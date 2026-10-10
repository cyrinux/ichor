package ichorgo

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func TestOrderClusterNodes(t *testing.T) {
	nodes := []clusterPlanNode{
		{Node: "10.0.0.9", Hostname: "w-2", Role: "worker"},
		{Node: "10.0.0.1", Hostname: "cp-1", Role: "controlplane", Leader: true},
		{Node: "10.0.0.8", Hostname: "w-1", Role: "worker"},
		{Node: "10.0.0.3", Hostname: "cp-3", Role: "controlplane"},
		{Node: "10.0.0.2", Hostname: "cp-2", Role: "controlplane"},
	}

	var got []string
	for _, n := range orderClusterNodes(nodes) {
		got = append(got, n.Hostname)
	}

	if want := []string{"cp-2", "cp-3", "cp-1", "w-1", "w-2"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// stubCluster records the roll's steps; gate failures are scripted per node.
type stubCluster struct {
	calls     []string
	gateFails map[string]int   // node -> how many gates fail before one passes
	upFail    map[string]error // node -> its upgrade's error
	onUpgrade func(node string)
	onGate    func(node string, passed bool)
}

func (s *stubCluster) upgrade(_ context.Context, n clusterPlanNode, emit func(phase, message string)) error {
	s.calls = append(s.calls, "upgrade "+n.Hostname)
	emit(phaseInstall, "installing")

	if s.onUpgrade != nil {
		s.onUpgrade(n.Hostname)
	}

	return s.upFail[n.Hostname]
}

func (s *stubCluster) gate(_ context.Context, n clusterPlanNode) error {
	s.calls = append(s.calls, "gate "+n.Hostname)

	failed := s.gateFails[n.Hostname] > 0
	if failed {
		s.gateFails[n.Hostname]--
	}

	if s.onGate != nil {
		s.onGate(n.Hostname, !failed)
	}

	if failed {
		return errors.New("etcd on " + n.Node + " is not healthy")
	}

	return nil
}

func clusterNodes(done ...string) []clusterPlanNode {
	nodes := []clusterPlanNode{
		{Node: "10.0.0.2", Hostname: "cp-2", Role: "controlplane", State: clusterNodePending},
		{Node: "10.0.0.1", Hostname: "cp-1", Role: "controlplane", Leader: true, State: clusterNodePending},
		{Node: "10.0.0.8", Hostname: "w-1", Role: "worker", State: clusterNodePending},
	}

	for i := range nodes {
		if slices.Contains(done, nodes[i].Hostname) {
			nodes[i].State = clusterNodeDone
		}
	}

	return nodes
}

// clusterHarness runs runClusterUpgrade with an instant settle time.
type clusterHarness struct {
	commands chan string
	progress []clusterProgress
}

func newClusterHarness() *clusterHarness { return &clusterHarness{commands: make(chan string, 8)} }

func (h *clusterHarness) run(steps clusterSteps, nodes []clusterPlanNode) error {
	return runClusterUpgrade(context.Background(), steps, nodes, clusterControl{
		commands: h.commands,
		emit:     func(p clusterProgress) { h.progress = append(h.progress, p) },
		settle:   time.Minute,
		after: func(time.Duration) <-chan time.Time {
			fired := make(chan time.Time, 1)
			fired <- time.Time{}

			return fired
		},
	})
}

func (h *clusterHarness) phases() []string {
	var out []string

	for _, p := range h.progress {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}

	return out
}

func TestClusterUpgradeSkipsDoneNodesAndGatesBetween(t *testing.T) {
	steps := &stubCluster{}
	h := newClusterHarness()

	if err := h.run(steps, clusterNodes("cp-2")); err != nil {
		t.Fatal(err)
	}

	want := []string{"upgrade cp-1", "gate cp-1", "upgrade w-1"}
	if !slices.Equal(steps.calls, want) {
		t.Fatalf("calls = %v, want %v", steps.calls, want)
	}

	last := h.progress[len(h.progress)-1]
	if last.Phase != clusterPhaseDone || last.Total != 3 || slices.ContainsFunc(last.Nodes, func(n clusterPlanNode) bool { return n.State != clusterNodeDone }) {
		t.Errorf("last = %+v", last)
	}

	// The node's own phases are forwarded.
	if !slices.ContainsFunc(h.progress, func(p clusterProgress) bool { return p.NodePhase == phaseInstall && p.Hostname == "cp-1" }) {
		t.Error("the node's phases must be reported")
	}
}

func TestClusterUpgradeGateFailurePausesUntilResume(t *testing.T) {
	h := newClusterHarness()
	steps := &stubCluster{gateFails: map[string]int{"cp-2": 1}, onGate: func(_ string, passed bool) {
		if !passed {
			h.commands <- clusterCommandResume // the user retries once paused
		}
	}}

	if err := h.run(steps, clusterNodes()); err != nil {
		t.Fatal(err)
	}

	want := []string{"upgrade cp-2", "gate cp-2", "gate cp-2", "upgrade cp-1", "gate cp-1", "upgrade w-1"}
	if !slices.Equal(steps.calls, want) {
		t.Fatalf("calls = %v", steps.calls)
	}

	paused := slices.IndexFunc(h.progress, func(p clusterProgress) bool { return p.Phase == clusterPhasePaused })
	if paused < 0 || !strings.Contains(h.progress[paused].Message, "health check failed") {
		t.Errorf("progress = %v", h.phases())
	}
}

func TestClusterUpgradeAbortWhilePaused(t *testing.T) {
	h := newClusterHarness()
	steps := &stubCluster{gateFails: map[string]int{"cp-2": 5}, onGate: func(string, bool) { h.commands <- clusterCommandAbort }}

	if err := h.run(steps, clusterNodes()); !errors.Is(err, errClusterAborted) {
		t.Fatalf("err = %v", err)
	}

	if !slices.Equal(steps.calls, []string{"upgrade cp-2", "gate cp-2"}) {
		t.Errorf("the other nodes must not be touched: %v", steps.calls)
	}
}

func TestClusterUpgradeAbortDuringANodeStopsAfterIt(t *testing.T) {
	h := newClusterHarness()
	steps := &stubCluster{onUpgrade: func(host string) {
		if host == "cp-2" {
			h.commands <- clusterCommandAbort // tapped while cp-2 upgrades
		}
	}}

	if err := h.run(steps, clusterNodes()); !errors.Is(err, errClusterAborted) {
		t.Fatalf("err = %v", err)
	}

	// cp-2 finished; the abort skipped the settle time, before cp-1 was touched.
	if !slices.Equal(steps.calls, []string{"upgrade cp-2", "gate cp-2"}) {
		t.Errorf("calls = %v", steps.calls)
	}
}

func TestClusterUpgradePauseBetweenNodes(t *testing.T) {
	h := newClusterHarness()
	steps := &stubCluster{onUpgrade: func(host string) {
		if host == "cp-1" {
			h.commands <- clusterCommandPause
			h.commands <- clusterCommandResume
		}
	}}

	if err := h.run(steps, clusterNodes("cp-2")); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(h.phases(), clusterPhasePaused) || len(steps.calls) != 3 {
		t.Errorf("phases = %v, calls = %v", h.phases(), steps.calls)
	}
}

func TestClusterUpgradeNodeFailureStops(t *testing.T) {
	steps := &stubCluster{upFail: map[string]error{"cp-1": errors.New("the installer failed")}}
	h := newClusterHarness()

	err := h.run(steps, clusterNodes())
	if err == nil || !strings.Contains(err.Error(), "cp-1: the installer failed") {
		t.Fatalf("err = %v", err)
	}

	if slices.Contains(steps.calls, "upgrade w-1") {
		t.Errorf("w-1 must not be touched: %v", steps.calls)
	}

	nodes := h.progress[len(h.progress)-1].Nodes
	if nodes[0].State != clusterNodeDone || nodes[1].State != clusterNodeFailed || nodes[2].State != clusterNodePending {
		t.Errorf("states = %+v", nodes)
	}
}

func TestEtcdGate(t *testing.T) {
	healthy := etcdOverview{Statuses: []etcdNodeStatus{{Node: "10.0.0.1"}, {Node: "10.0.0.2"}}}
	if err := etcdGate(healthy); err != nil {
		t.Errorf("healthy = %v", err)
	}

	alarm := healthy
	alarm.Alarms = []etcdAlarm{{MemberID: "a1", Alarm: "NOSPACE"}}

	if err := etcdGate(alarm); err == nil || !strings.Contains(err.Error(), "NOSPACE") {
		t.Errorf("alarm = %v", err)
	}

	down := etcdOverview{Statuses: []etcdNodeStatus{{Node: "10.0.0.1", Error: "unreachable"}}}
	if err := etcdGate(down); err == nil {
		t.Error("a member down must fail the gate")
	}
}

func TestClusterLockRenewal(t *testing.T) {
	store := &fakeLeases{}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	r := upgradeLockRequest{holder: "ichor/a", hostname: "cluster", to: "v1.12.0", run: lockRunCluster, duration: clusterLockDuration}

	res, err := acquireUpgradeLock(context.Background(), store, r, func(*upgradeLockInfo) bool { return false }, now)
	if err != nil {
		t.Fatal(err)
	}

	info := heldLock(res.lease, now)
	if info == nil || info.run != lockRunCluster || !strings.Contains(info.describe(), "cluster upgrade (to v1.12.0") {
		t.Fatalf("info = %+v", info)
	}

	// Expired after clusterLockDuration unless renewed.
	if heldLock(res.lease, now.Add(clusterLockDuration+time.Second)) != nil {
		t.Error("a cluster lock lasts clusterLockDuration")
	}

	renewed, err := renewLease(context.Background(), store, res.lease, map[string]string{annotationHostname: "cp-2"}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	held := heldLock(renewed, now.Add(clusterLockDuration+time.Second))
	if held == nil || held.hostname != "cp-2" || held.run != lockRunCluster || held.to != "v1.12.0" {
		t.Fatalf("renewed = %+v", held)
	}

	// A lease taken over meanwhile is not renewed.
	if _, err := renewLease(context.Background(), store, res.lease, nil, now); !errors.Is(err, errLeaseConflict) {
		t.Errorf("stale renew = %v", err)
	}
}

// clusterFake is three control planes (192.0.2.51 leads etcd) and a worker; .52 already
// runs the target version.
func clusterFake(t *testing.T) string {
	t.Helper()

	f := newFakeTalos()
	f.members = map[string]uint64{"192.0.2.51": 0xa1, "192.0.2.52": 0xa2, "192.0.2.53": 0xa3}
	f.leader = 0xa1

	f.addNode(t, "192.0.2.51", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.52", "v1.11.1", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.53", "v1.11.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.61", "v1.11.0", machine.TypeWorker)

	return f.start(t, "192.0.2.51", "192.0.2.52", "192.0.2.53", "192.0.2.61")
}

func TestClusterUpgradePlanFake(t *testing.T) {
	cfg := clusterFake(t)

	out, err := ClusterUpgradePlan(cfg, "fake", "", "1.11.1")
	plan := decodeJSON[clusterUpgradePlan](t, out, err)

	var order, states []string
	for _, n := range plan.Nodes {
		order = append(order, n.Node)
		states = append(states, n.State)
	}

	if want := []string{"192.0.2.52", "192.0.2.53", "192.0.2.51", "192.0.2.61"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v (control planes, the leader last, then workers)", order, want)
	}

	if want := []string{"done", "pending", "pending", "pending"}; !slices.Equal(states, want) {
		t.Errorf("states = %v", states)
	}

	if plan.Version != "v1.11.1" || !strings.HasSuffix(plan.Image, ":v1.11.1") || !plan.Nodes[2].Leader || plan.Nodes[3].Role != "worker" {
		t.Errorf("plan = %s", out)
	}

	if _, err := ClusterUpgradePlan(cfg, "fake", "", "latest"); err == nil {
		t.Error("a bad version must be refused")
	}
}

func TestClusterUpgradeDemo(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := ClusterUpgradePlan(demo, "", "", "v1.99.0")
	plan := decodeJSON[clusterUpgradePlan](t, out, err)

	if len(plan.Nodes) != 5 || plan.Nodes[2].Hostname != "demo-cp-1" || !plan.Nodes[2].Leader || plan.Nodes[3].Role != "worker" {
		t.Fatalf("plan = %s", out)
	}

	rec := doneRecorder{done: make(chan string, 1)}
	StartClusterUpgrade(demo, "", "", "v1.99.0", true, false, rec)

	if got := <-rec.done; got != errDemoUnavailable.Error() {
		t.Fatalf("demo = %q", got)
	}

	if entries := readAudit(t, "", "cluster-upgrade"); len(entries) != 1 || !strings.Contains(entries[0].Params, "version=v1.99.0 drain=true") {
		t.Errorf("audit = %+v", entries)
	}
}
