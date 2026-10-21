package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The rolling cluster upgrade run end to end: StartClusterUpgrade against a fake Talos API
// of four nodes and a fake Kubernetes API holding the cluster upgrade lock (a Lease).

const (
	rollCP1    = "10.5.0.1" // leads etcd: upgraded last of the control planes
	rollCP2    = "10.5.0.2"
	rollCP3    = "10.5.0.3"
	rollWorker = "10.5.0.11"
)

// rollKube is the Kubernetes API of the roll: the lock's Lease (create, get, replace,
// delete with preconditions) and what a drain reads and patches.
type rollKube struct {
	mu      sync.Mutex
	lease   *kubeLease
	rv      int
	creates int
	renewed []map[string]string // the annotations of each replace, in order
	deletes int
	patches []string // "node body" of each node patch
	// stealOn takes the lock over for another run on the first request whose "METHOD path"
	// starts with it.
	stealOn string
	// failLeaseGets answers every read of the Lease with an error.
	failLeaseGets bool
}

func (k *rollKube) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	k.mu.Lock()
	defer k.mu.Unlock()

	reply := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	status := func(code int, reason string) {
		reply(code, map[string]any{"kind": "Status", "reason": reason, "message": reason, "code": code})
	}

	leasePath := leasesPath + "/" + upgradeLockName
	key := r.Method + " " + r.URL.Path

	defer func() {
		if k.stealOn != "" && strings.HasPrefix(key, k.stealOn) {
			k.stealOn = ""
			k.steal()
		}
	}()

	switch {
	case r.URL.Path == "/version":
		reply(http.StatusOK, map[string]string{"gitVersion": "v1.34.0"})
	case key == "POST "+leasesPath:
		if k.lease != nil {
			status(http.StatusConflict, "AlreadyExists")

			return
		}

		var l kubeLease
		_ = json.Unmarshal(body, &l)
		k.creates++
		k.store(l)
		reply(http.StatusCreated, k.lease)
	case key == "GET "+leasePath && k.failLeaseGets:
		status(http.StatusInternalServerError, "InternalError")
	case key == "GET "+leasePath:
		if k.lease == nil {
			status(http.StatusNotFound, "NotFound")

			return
		}

		reply(http.StatusOK, k.lease)
	case key == "PUT "+leasePath:
		var l kubeLease
		_ = json.Unmarshal(body, &l)

		if k.lease == nil || l.Metadata.ResourceVersion != k.lease.Metadata.ResourceVersion {
			status(http.StatusConflict, "Conflict")

			return
		}

		k.renewed = append(k.renewed, l.Metadata.Annotations)
		k.store(l)
		reply(http.StatusOK, k.lease)
	case key == "DELETE "+leasePath:
		var opts struct {
			Preconditions struct{ UID, ResourceVersion string } `json:"preconditions"`
		}
		_ = json.Unmarshal(body, &opts)

		if k.lease == nil || opts.Preconditions.ResourceVersion != k.lease.Metadata.ResourceVersion {
			status(http.StatusConflict, "Conflict")

			return
		}

		k.deletes++
		k.lease = nil
		reply(http.StatusOK, map[string]string{"kind": "Status", "status": "Success"})
	case key == "GET /api/v1/pods" || key == "GET /apis/policy/v1/poddisruptionbudgets":
		reply(http.StatusOK, map[string]any{"items": []any{}})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/nodes/"):
		k.patches = append(k.patches, strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")+" "+string(body))
		reply(http.StatusOK, map[string]any{})
	default:
		status(http.StatusNotFound, "NotFound")
	}
}

// store keeps l as the Lease with a new resourceVersion (k.mu held).
func (k *rollKube) store(l kubeLease) {
	k.rv++
	l.Metadata.UID = "lease-uid"
	l.Metadata.ResourceVersion = fmt.Sprint(k.rv)
	k.lease = &l
}

// steal gives the lock to another run (k.mu held).
func (k *rollKube) steal() {
	other := upgradeLockRequest{holder: "ichor/other", hostname: "cluster", to: "v1.11.1", run: lockRunCluster, duration: clusterLockDuration}
	k.store(other.lease(time.Now(), k.lease))
}

func (k *rollKube) state() (lease *kubeLease, creates, deletes int, renewed []map[string]string, patches []string) {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.lease, k.creates, k.deletes, slices.Clone(k.renewed), slices.Clone(k.patches)
}

// useRollKube serves k as the Kubernetes API of every context.
func useRollKube(t *testing.T, k *rollKube) {
	t.Helper()

	api := newFakeKubeAPI(t, nil)
	api.Config.Handler = http.HandlerFunc(k.handler)
	useFakeKube(t, api)
}

// rollHosts are the hostnames of the roll's nodes.
var rollHosts = map[string]string{rollCP1: "cp-1", rollCP2: "cp-2", rollCP3: "cp-3", rollWorker: "worker-1"}

// rollFake is cp-1 (the etcd leader), cp-2 and cp-3, control planes (two members would
// lose quorum), and worker-1, all on from;
// an upgrade request boots the requested version at once.
func rollFake(t *testing.T, from string) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.members = map[string]uint64{rollCP1: 0xa1, rollCP2: 0xa2, rollCP3: 0xa3}
	f.leader = 0xa1

	f.addNode(t, rollCP1, from, machine.TypeControlPlane)
	f.addNode(t, rollCP2, from, machine.TypeControlPlane)
	f.addNode(t, rollCP3, from, machine.TypeControlPlane)
	f.addNode(t, rollWorker, from, machine.TypeWorker)

	for node, host := range rollHosts {
		hs := network.NewHostnameStatus(network.NamespaceName, network.HostnameID)
		hs.TypedSpec().Hostname = host
		f.put(node, hs)
		setKubeReady(f, node, true)
	}

	f.upgrade = func(node string, req *machineapi.UpgradeRequest) error {
		_, tag := splitImageRef(req.GetImage())
		f.setVersion(node, tag)

		return nil
	}

	return f, f.start(t, rollCP1, rollCP2, rollCP3, rollWorker)
}

// setKubeReady sets whether node's Kubernetes node is Ready.
func setKubeReady(f *fakeTalos, node string, ready bool) {
	status := k8s.NewNodeStatus(k8s.NamespaceName, rollHosts[node])
	status.TypedSpec().Nodename, status.TypedSpec().NodeReady = rollHosts[node], ready
	f.put(node, status)
}

// rollRecorder is a ClusterUpgradeListener handing every event to the test.
type rollRecorder struct {
	events chan clusterProgress
	done   chan string
}

func newRollRecorder() *rollRecorder {
	return &rollRecorder{events: make(chan clusterProgress, 256), done: make(chan string, 1)}
}

func (r *rollRecorder) OnProgress(s string) {
	var p clusterProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.events <- p
}

func (r *rollRecorder) OnDone(errMessage string) { r.done <- errMessage }

// follow passes every event to react until OnDone, and returns the events and the error.
func (r *rollRecorder) follow(t *testing.T, react func(clusterProgress)) ([]clusterProgress, string) {
	t.Helper()

	var got []clusterProgress

	timeout := time.After(30 * time.Second)

	for {
		select {
		case p := <-r.events:
			got = append(got, p)

			if react != nil {
				react(p)
			}
		case msg := <-r.done:
			for {
				select {
				case p := <-r.events:
					got = append(got, p)
				default:
					return got, msg
				}
			}
		case <-timeout:
			t.Fatal("OnDone not called")

			return nil, ""
		}
	}
}

// skipSettle resumes as soon as a node passed its gate, instead of waiting clusterSettle.
func skipSettle(run *ClusterUpgradeRun) func(clusterProgress) {
	return func(p clusterProgress) {
		if p.Phase == clusterPhaseGate && strings.HasPrefix(p.Message, "healthy:") {
			run.Resume()
		}
	}
}

func upgradeRequests(f *fakeTalos) []string {
	var out []string
	for _, c := range f.called("Upgrade ") {
		out = append(out, rollHosts[strings.TrimPrefix(c, "Upgrade ")])
	}

	return out
}

func hasProgress(events []clusterProgress, phase, message string) bool {
	return slices.ContainsFunc(events, func(p clusterProgress) bool {
		return p.Phase == phase && strings.Contains(p.Message, message)
	})
}

func TestStartClusterUpgradeRollsEveryNode(t *testing.T) {
	withDataDir(t)

	kube := &rollKube{}
	useRollKube(t, kube)

	f, cfg := rollFake(t, "v1.11.0")

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "1.11.1", false, true, rec)

	events, msg := rec.follow(t, skipSettle(run))
	if msg != "" {
		t.Fatalf("OnDone = %q", msg)
	}

	// Control planes first, the etcd leader last of them, then the worker.
	if got := upgradeRequests(f); !slices.Equal(got, []string{"cp-2", "cp-3", "cp-1", "worker-1"}) {
		t.Fatalf("upgrades = %v", got)
	}

	if got := f.called("EtcdForfeitLeadership"); !slices.Equal(got, []string{"EtcdForfeitLeadership " + rollCP1}) {
		t.Errorf("only the leader forfeits its leadership: %v", got)
	}

	last := events[len(events)-1]
	if last.Phase != clusterPhaseDone || last.Total != 4 || slices.ContainsFunc(last.Nodes, func(n clusterPlanNode) bool { return n.State != clusterNodeDone }) {
		t.Errorf("last = %+v", last)
	}

	// Each node passed its gate, and the node's own phases were forwarded.
	for _, host := range []string{"cp-2", "cp-3", "cp-1"} {
		if !hasProgress(events, clusterPhaseGate, "checking "+host) {
			t.Errorf("no gate after %s", host)
		}
	}

	if !slices.ContainsFunc(events, func(p clusterProgress) bool { return p.Hostname == "worker-1" && p.NodePhase == phaseDone }) {
		t.Error("the node's phases must be reported")
	}

	// The lock was taken once, renewed before each node with its name, then freed.
	lease, creates, deletes, renewed, _ := kube.state()
	if lease != nil || creates != 1 || deletes != 1 {
		t.Errorf("lease = %+v, creates %d, deletes %d", lease, creates, deletes)
	}

	var hosts []string
	for _, a := range renewed {
		hosts = append(hosts, a[annotationHostname])

		if a[annotationRun] != lockRunCluster || a[annotationTo] != "v1.11.1" {
			t.Errorf("renewed annotations = %v", a)
		}
	}

	if !slices.Equal(hosts, []string{"cp-2", "cp-3", "cp-1", "worker-1"}) {
		t.Errorf("renewals = %v", hosts)
	}

	if entries := readAudit(t, "fake", "cluster-upgrade"); len(entries) != 1 || !strings.Contains(entries[0].Params, "version=1.11.1 drain=false nodes=cp-2,cp-3,cp-1,worker-1") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartClusterUpgradePauseResume(t *testing.T) {
	withDataDir(t)
	useRollKube(t, &rollKube{})

	f, cfg := rollFake(t, "v1.11.0")

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.11.1", false, true, rec)

	paused := false
	events, msg := rec.follow(t, func(p clusterProgress) {
		switch {
		case p.Phase == clusterPhaseGate && strings.HasPrefix(p.Message, "healthy:") && !paused:
			paused = true

			run.Pause() // tapped while the cluster settles after cp-2
		case p.Phase == clusterPhasePaused:
			if got := upgradeRequests(f); !slices.Equal(got, []string{"cp-2"}) {
				t.Errorf("paused after %v", got)
			}

			run.Resume()
		default:
			skipSettle(run)(p)
		}
	})
	if msg != "" {
		t.Fatalf("OnDone = %q", msg)
	}

	if !hasProgress(events, clusterPhasePaused, "paused after cp-2") {
		t.Errorf("no pause after cp-2")
	}

	if got := upgradeRequests(f); len(got) != 4 {
		t.Errorf("upgrades after the resume = %v", got)
	}
}

func TestStartClusterUpgradeAbort(t *testing.T) {
	withDataDir(t)

	kube := &rollKube{}
	useRollKube(t, kube)

	f, cfg := rollFake(t, "v1.11.0")

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.11.1", false, true, rec)

	events, msg := rec.follow(t, func(p clusterProgress) {
		if p.Phase == clusterPhaseGate && strings.HasPrefix(p.Message, "healthy:") {
			run.Abort()
		}
	})
	if msg != errClusterAborted.Error() {
		t.Fatalf("OnDone = %q", msg)
	}

	if got := upgradeRequests(f); !slices.Equal(got, []string{"cp-2"}) {
		t.Errorf("upgrades = %v: the nodes after the abort must not be touched", got)
	}

	last := events[len(events)-1].Nodes
	if last[0].State != clusterNodeDone || slices.ContainsFunc(last[1:], func(n clusterPlanNode) bool { return n.State != clusterNodePending }) {
		t.Errorf("states = %+v", last)
	}

	if lease, _, deletes, _, _ := kube.state(); lease != nil || deletes != 1 {
		t.Errorf("an abort frees the lock: %+v", lease)
	}

	if entries := readAudit(t, "fake", "cluster-upgrade"); len(entries) != 1 || !strings.Contains(entries[0].Params, "nodes=cp-2") || strings.Contains(entries[0].Params, "cp-1") {
		t.Errorf("audit = %+v", entries)
	}
}

// A node that did not come back Ready in Kubernetes stops the roll at the gate until Resume
// retries it.
func TestStartClusterUpgradeGateWaitsForReady(t *testing.T) {
	withDataDir(t)
	useRollKube(t, &rollKube{})

	f, cfg := rollFake(t, "v1.11.0")
	upgrade := f.upgrade
	f.upgrade = func(node string, req *machineapi.UpgradeRequest) error {
		if node == rollCP2 {
			setKubeReady(f, node, false) // its kubelet did not register yet
		}

		return upgrade(node, req)
	}

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.11.1", false, true, rec)

	events, msg := rec.follow(t, func(p clusterProgress) {
		if p.Phase == clusterPhasePaused {
			if got := upgradeRequests(f); !slices.Equal(got, []string{"cp-2"}) {
				t.Errorf("the gate let the roll go on: %v", got)
			}

			setKubeReady(f, rollCP2, true)
			run.Resume()
		}

		skipSettle(run)(p)
	})
	if msg != "" {
		t.Fatalf("OnDone = %q", msg)
	}

	if !hasProgress(events, clusterPhasePaused, "health check failed: cp-2 is not Ready in Kubernetes yet") {
		t.Errorf("no failed gate: %+v", events)
	}

	if got := upgradeRequests(f); len(got) != 4 {
		t.Errorf("upgrades = %v", got)
	}
}

// Cancel while the roll waits on a failed gate stops following; the lock is freed.
func TestStartClusterUpgradeCancelWhilePaused(t *testing.T) {
	withDataDir(t)

	kube := &rollKube{}
	useRollKube(t, kube)

	f, cfg := rollFake(t, "v1.11.0")
	upgrade := f.upgrade
	f.upgrade = func(node string, req *machineapi.UpgradeRequest) error {
		f.mu.Lock()
		f.etcdErrors = map[string][]string{rollCP1: {"raft: not leader"}} // etcd did not recover
		f.mu.Unlock()

		return upgrade(node, req)
	}

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.11.1", false, true, rec)

	events, msg := rec.follow(t, func(p clusterProgress) {
		if p.Phase == clusterPhasePaused {
			run.Cancel()
		}
	})
	if msg != errStoppedFollowing.Error() {
		t.Fatalf("OnDone = %q", msg)
	}

	if !hasProgress(events, clusterPhasePaused, "etcd on "+rollCP1+" is not healthy") {
		t.Errorf("the gate must fail on etcd: %+v", events)
	}

	if got := upgradeRequests(f); !slices.Equal(got, []string{"cp-2"}) {
		t.Errorf("upgrades = %v", got)
	}

	if lease, _, _, _, _ := kube.state(); lease != nil {
		t.Errorf("the lock must be freed: %+v", lease)
	}
}

// A lock taken over by another run (it expired while the phone slept) refuses the next
// node's upgrade request.
func TestStartClusterUpgradeLockLost(t *testing.T) {
	withDataDir(t)

	kube := &rollKube{stealOn: "PUT "}
	useRollKube(t, kube)

	f, cfg := rollFake(t, "v1.11.0")

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.11.1", false, true, rec)

	_, msg := rec.follow(t, skipSettle(run))
	if !strings.HasPrefix(msg, "cp-2: ") || !strings.Contains(msg, "the cluster upgrade lock expired or was taken by another run") {
		t.Fatalf("OnDone = %q", msg)
	}

	if got := upgradeRequests(f); len(got) != 0 {
		t.Errorf("no node may be upgraded without the lock: %v", got)
	}

	// The other run's lock is not released.
	if lease, _, deletes, _, _ := kube.state(); lease == nil || lease.Spec.HolderIdentity != "ichor/other" || deletes != 0 {
		t.Errorf("lease = %+v, deletes %d", lease, deletes)
	}
}

func TestStartClusterUpgradeRefusals(t *testing.T) {
	tests := []struct {
		name, context, version string
		held                   bool
		wantErr                string
	}{
		{name: "bad version", context: "fake", version: "latest", wantErr: `"latest" is not a Talos version`},
		{name: "unknown context", context: "nope", version: "v1.11.1", wantErr: "nope"},
		{name: "lock held", context: "fake", version: "v1.11.1", held: true, wantErr: "cluster upgrade refused: another Ichor cluster upgrade (to v1.11.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withDataDir(t)

			kube := &rollKube{}
			if tt.held {
				kube.steal()
			}

			useRollKube(t, kube)

			f, cfg := rollFake(t, "v1.11.0")

			rec := newRollRecorder()
			StartClusterUpgrade(cfg, tt.context, "", tt.version, false, true, rec)

			if _, msg := rec.follow(t, nil); !strings.Contains(msg, tt.wantErr) {
				t.Fatalf("OnDone = %q, want %q", msg, tt.wantErr)
			}

			if got := upgradeRequests(f); len(got) != 0 {
				t.Errorf("upgrades = %v", got)
			}
		})
	}
}

// With drain, a node whose upgrade does not drain it (Talos 1.18+) goes through the
// maintenance upgrade: cordoned and drained, then the checks run again under the lock; a
// lock that cannot be read stops it there, the node cordoned and not upgraded.
func TestStartClusterUpgradeDrained(t *testing.T) {
	withDataDir(t)

	kube := &rollKube{failLeaseGets: true} // the lock cannot be checked again
	useRollKube(t, kube)

	f, cfg := rollFake(t, "v1.18.0")

	rec := newRollRecorder()
	run := StartClusterUpgrade(cfg, "fake", "", "v1.18.1", true, true, rec)

	events, msg := rec.follow(t, skipSettle(run))
	// The Kubernetes API error replaces the rest of the message (see kubeError).
	if !strings.HasPrefix(msg, "cp-2: ") || !strings.Contains(msg, "InternalError") {
		t.Fatalf("OnDone = %q", msg)
	}

	if got := upgradeRequests(f); len(got) != 0 {
		t.Errorf("upgrades = %v", got)
	}

	if _, _, _, _, patches := kube.state(); len(patches) != 1 || !strings.HasPrefix(patches[0], "cp-2 ") || !strings.Contains(patches[0], `"unschedulable":true`) {
		t.Errorf("patches = %v", patches)
	}

	if !slices.ContainsFunc(events, func(p clusterProgress) bool { return p.Hostname == "cp-2" && p.NodePhase == phaseCordon }) {
		t.Errorf("the maintenance phases must be reported: %+v", events)
	}
}

func TestClusterUpgradeKeepLockStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan struct{})

	go func() {
		keepLockRenewed(ctx, &upgradeLock{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("keepLockRenewed did not stop")
	}

	steps := &talosClusterSteps{lock: &upgradeLock{}}
	steps.keepLock(ctx)

	if err := steps.stillHeld(ctx); err != nil {
		t.Errorf("no lock taken passes: %v", err)
	}
}

func TestClusterUpgradeMaintenanceEmitter(t *testing.T) {
	var got []string

	e := maintenanceEmitter(func(phase, message string) { got = append(got, phase+": "+message) })
	e.OnProgress(`{"phase":"drain","message":"evicting 2 pods"}`)
	e.OnProgress("not json")
	e.OnDone("ignored: the cluster run reports the node's outcome")

	if !slices.Equal(got, []string{"drain: evicting 2 pods"}) {
		t.Errorf("forwarded = %v", got)
	}
}
