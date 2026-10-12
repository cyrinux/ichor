package ichorgo

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const (
	joinTemplateNode = "192.0.2.11"
	joinNewNode      = "192.0.2.20"
	joinPollInterval = 10 * time.Millisecond
	// joinStepDelay is how long the fake node stays in each state: several polls.
	joinStepDelay = 80 * time.Millisecond
)

type joinRecorder struct {
	events chan joinProgress
	done   chan [2]string
}

func newJoinRecorder() *joinRecorder {
	return &joinRecorder{events: make(chan joinProgress, 256), done: make(chan [2]string, 1)}
}

func (r *joinRecorder) OnProgress(js string) {
	var p joinProgress
	if err := json.Unmarshal([]byte(js), &p); err == nil {
		r.events <- p
	}
}

func (r *joinRecorder) OnDone(nodeAddress, errMessage string) {
	r.done <- [2]string{nodeAddress, errMessage}
}

// wait returns the run's end and every event before it.
func (r *joinRecorder) wait(t *testing.T) (address, errMessage string, events []joinProgress) {
	t.Helper()

	select {
	case d := <-r.done:
		close(r.events)

		for e := range r.events {
			events = append(events, e)
		}

		return d[0], d[1], events
	case <-time.After(10 * time.Second):
		t.Fatal("the join did not end")
	}

	return "", "", nil
}

func joinPhasesOf(events []joinProgress) []string {
	var out []string
	for _, e := range events {
		if len(out) == 0 || out[len(out)-1] != e.Phase {
			out = append(out, e.Phase)
		}
	}

	return out
}

// joinCluster is a cluster with a template member of role, and a node in maintenance mode
// behind the same fake endpoint (it answers the calls that name no node, as the maintenance
// API does). It returns the fake, the talosconfig and the maintenance address.
func joinCluster(t *testing.T, role machine.Type) (*fakeTalos, string, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, joinTemplateNode, "v1.14.2", role)

	provider, err := configloader.NewFromBytes([]byte(joinTemplate(t, role)))
	if err != nil {
		t.Fatal(err)
	}

	f.put(joinTemplateNode, config.NewMachineConfigWithID(provider, config.ActiveID))
	f.setVersion("", "v1.14.2")

	cfg := f.start(t, joinTemplateNode)

	endpoint := strings.TrimSpace(strings.SplitN(strings.SplitN(cfg, "endpoints:\n", 2)[1], "\n", 2)[0])

	return f, cfg, strings.TrimPrefix(endpoint, "- ")
}

// installAfterApply plays the install once the config arrives: the maintenance API stops,
// nothing answers, then the node boots with the cluster's identity, becomes ready, and its
// Kubernetes node Ready.
func installAfterApply(t *testing.T, f *fakeTalos, typ machine.Type) {
	t.Helper()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(joinStepDelay)
			f.addNode(t, joinNewNode, "v1.14.2", typ)
			f.setStage(joinNewNode, runtime.MachineStageBooting, false)

			time.Sleep(joinStepDelay)
			f.setStage(joinNewNode, runtime.MachineStageRunning, true)

			time.Sleep(joinStepDelay)

			status := k8s.NewNodeStatus(k8s.NamespaceName, "worker-3")
			status.TypedSpec().Nodename, status.TypedSpec().NodeReady = "worker-3", true
			f.put(joinNewNode, status)
		}()
	}
}

func startJoin(cfg, address, role string, rec *joinRecorder, timeout time.Duration) *JoinRun {
	j := &nodeJoin{
		config: cfg, context: "fake", address: address, listener: maskedJoinListener{rec}, interval: joinPollInterval,
		req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "worker-3", installDisk: "/dev/sda", staticAddress: joinNewNode + "/24"},
	}

	return j.start(timeout)
}

func TestStartNodeJoinWorkerPhases(t *testing.T) {
	f, cfg, address := joinCluster(t, machine.TypeWorker)
	installAfterApply(t, f, machine.TypeWorker)

	rec := newJoinRecorder()
	startJoin(cfg, address, joinRoleWorker, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("join failed: %s (events %+v)", errMessage, events)
	}

	if node != joinNewNode {
		t.Errorf("node = %q, want the static address %s", node, joinNewNode)
	}

	want := []string{joinPhaseApplying, joinPhaseInstalling, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	f.mu.Lock()
	applies := slices.Clone(f.applies)
	f.mu.Unlock()

	if len(applies) != 1 || applies[0].node != "" || applies[0].dryRun || applies[0].mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Errorf("applies = %+v, want one real AUTO apply to the maintenance API", applies)
	}

	for _, e := range events {
		if strings.Contains(e.Message, "BEGIN") || strings.Contains(e.Message, "secret") {
			t.Errorf("a progress line carries config: %q", e.Message)
		}
	}
}

func TestStartNodeJoinControlPlaneWaitsForEtcd(t *testing.T) {
	f, cfg, address := joinCluster(t, machine.TypeControlPlane)
	installAfterApply(t, f, machine.TypeControlPlane)

	f.mu.Lock()
	f.etcdDown = map[string]bool{joinNewNode: true}
	f.mu.Unlock()

	go func() {
		time.Sleep(5 * joinStepDelay)
		f.mu.Lock()
		delete(f.etcdDown, joinNewNode)
		f.mu.Unlock()
	}()

	rec := newJoinRecorder()
	startJoin(cfg, address, joinRoleControlPlane, rec, time.Minute)

	_, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("join failed: %s", errMessage)
	}

	waited := slices.ContainsFunc(events, func(e joinProgress) bool { return strings.Contains(e.Message, "etcd member") })
	if !waited {
		t.Errorf("no etcd wait in %+v", events)
	}

	if len(f.called("EtcdStatus "+joinNewNode)) == 0 {
		t.Error("the new member's etcd was never asked")
	}
}

func TestStartNodeJoinTimeout(t *testing.T) {
	f, cfg, address := joinCluster(t, machine.TypeWorker)

	// The node takes the config and never comes back.
	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()
	}

	rec := newJoinRecorder()
	startJoin(cfg, address, joinRoleWorker, rec, 2*time.Second)

	node, errMessage, _ := rec.wait(t)
	if node != "" || !strings.Contains(errMessage, "did not join within") || !strings.Contains(errMessage, "console") {
		t.Errorf("got %q, %q; want a timeout naming what to check", node, errMessage)
	}
}

// Cancel after the apply only stops following: the config was sent once, nothing else.
func TestStartNodeJoinCancelAfterApply(t *testing.T) {
	f, cfg, address := joinCluster(t, machine.TypeWorker)

	applied := make(chan struct{})
	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()
		close(applied)
	}

	rec := newJoinRecorder()
	run := startJoin(cfg, address, joinRoleWorker, rec, time.Minute)

	select {
	case <-applied:
	case <-time.After(10 * time.Second):
		t.Fatal("the config was never applied")
	}

	run.Cancel()

	_, errMessage, _ := rec.wait(t)
	if errMessage != errJoinStopped.Error() {
		t.Errorf("got %q, want %q", errMessage, errJoinStopped)
	}

	f.mu.Lock()
	n := len(f.applies)
	f.mu.Unlock()

	if n != 1 {
		t.Errorf("%d applies, want exactly one", n)
	}

	if len(f.called("Reset")) != 0 || len(f.called("Reboot")) != 0 {
		t.Error("cancel touched the node")
	}
}

// An installed node (the fake answers without a node only in maintenance mode) is refused.
func TestStartNodeJoinRefusals(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	j := &nodeJoin{config: demo, context: "", address: "10.0.0.5", req: joinConfigRequest{template: "192.0.2.10", role: joinRoleWorker}}
	if err := j.run(t.Context(), time.Minute); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: got %v", err)
	}

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	for _, tc := range []struct{ address, role, want string }{
		{"8.8.8.8", joinRoleWorker, "public"},
		{"10.0.0.5", "etcd", "unknown role"},
	} {
		j := &nodeJoin{config: cfg, context: "fake", address: tc.address,
			req: joinConfigRequest{template: joinTemplateNode, role: tc.role, hostname: "worker-3", installDisk: "/dev/sda"}}
		if err := j.run(t.Context(), time.Minute); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s/%s: got %v, want %q", tc.address, tc.role, err, tc.want)
		}
	}
}

// A hostname a member already has is refused before anything is sent: following it by
// hostname would end on that member.
func TestStartNodeJoinRefusesATakenHostname(t *testing.T) {
	f, cfg, address := joinCluster(t, machine.TypeWorker)

	m := cluster.NewMember(cluster.NamespaceName, "worker-3")
	m.TypedSpec().Hostname = "worker-3"
	f.put(joinTemplateNode, m)

	rec := newJoinRecorder()
	startJoin(cfg, address, joinRoleWorker, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "already named worker-3") {
		t.Errorf("got %q, want the taken hostname refused", errMessage)
	}

	f.mu.Lock()
	n := len(f.applies)
	f.mu.Unlock()

	if n != 0 {
		t.Errorf("%d applies, want none", n)
	}
}

func TestNodeJoinWarnings(t *testing.T) {
	worker, err := NodeJoinWarnings("worker")
	if err != nil || !strings.Contains(worker, "secrets") || !strings.Contains(worker, "without authentication") || strings.Contains(worker, "etcd") {
		t.Errorf("worker: %q, %v", worker, err)
	}

	cp, err := NodeJoinWarnings("controlplane")
	if err != nil || !strings.Contains(cp, "etcd") {
		t.Errorf("controlplane: %q, %v", cp, err)
	}

	if _, err := NodeJoinWarnings("init"); err == nil {
		t.Error("an unknown role was accepted")
	}
}

func TestJoinNodeAddress(t *testing.T) {
	for _, tc := range []struct{ address, static, want string }{
		{"10.0.0.5", "", "10.0.0.5"},
		{"10.0.0.5:50000", "", "10.0.0.5"},
		{"[fd00::5]:50000", "", "fd00::5"},
		{"10.0.0.5", "192.0.2.20/24", "192.0.2.20"},
	} {
		if got := joinNodeAddress(tc.address, tc.static); got != tc.want {
			t.Errorf("%q %q: got %q, want %q", tc.address, tc.static, got, tc.want)
		}
	}
}
