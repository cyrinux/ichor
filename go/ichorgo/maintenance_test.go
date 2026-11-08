package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maintNode     = "192.0.2.11"
	maintPeer     = "192.0.2.12"
	maintKubeNode = "worker-1"
)

// Pods of worker-1: web-1 is evicted, pg-1 is evicted under a budget, cilium-x is left to
// its DaemonSet, debug has no controller.
const (
	maintWebPod    = `{"metadata":{"name":"web-1","namespace":"web","uid":"u1","ownerReferences":[{"kind":"ReplicaSet","name":"web-5d","controller":true}]},"spec":{"nodeName":"worker-1"}}`
	maintPgPod     = `{"metadata":{"name":"pg-1","namespace":"db","uid":"u2","labels":{"role":"primary"},"ownerReferences":[{"kind":"Cluster","name":"pg","controller":true}]},"spec":{"nodeName":"worker-1"}}`
	maintCiliumPod = `{"metadata":{"name":"cilium-x","namespace":"kube-system","uid":"u3","ownerReferences":[{"kind":"DaemonSet","name":"cilium","controller":true}]},"spec":{"nodeName":"worker-1"}}`
	maintBarePod   = `{"metadata":{"name":"debug","namespace":"default","uid":"u4"},"spec":{"nodeName":"worker-1"}}`
	maintPDBs      = `{"items":[{"metadata":{"name":"pg-primary","namespace":"db"},"spec":{"selector":{"matchLabels":{"role":"primary"}}},"status":{"disruptionsAllowed":0}}]}`
)

// maintKubeAPI is the API server of the maintenance tests: worker-1's pods, a budget that
// refuses pg-1's eviction (429), the upgrade lock Lease, and the node's cordon patches.
// Evicted pods answer NotFound right away (gone before the next drain round, which only
// comes after drainPollInterval).
type maintKubeAPI struct {
	mu      sync.Mutex
	pods    []string
	lease   *kubeLease
	version int
	cordons []bool // the unschedulable value of each node patch, in order
	// failAfterCordon answers the pod listing with a 500 once the node was patched.
	failAfterCordon bool
}

func (a *maintKubeAPI) cordoned() []bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.cordons)
}

func (a *maintKubeAPI) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	a.mu.Lock()
	defer a.mu.Unlock()

	answer := func(code int, s string) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, s)
	}
	notFound := func() { answer(http.StatusNotFound, `{"kind":"Status","reason":"NotFound","message":"not found"}`) }

	lease := leasesPath + "/" + upgradeLockName

	switch {
	case r.URL.Path == "/version":
		answer(http.StatusOK, `{"gitVersion":"v1.34.0"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pods" && a.failAfterCordon && len(a.cordons) > 0:
		answer(http.StatusInternalServerError, `{"kind":"Status","reason":"InternalError","message":"etcdserver: request timed out"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pods":
		answer(http.StatusOK, `{"items":[`+strings.Join(a.pods, ",")+`]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/apis/policy/v1/poddisruptionbudgets":
		answer(http.StatusOK, maintPDBs)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/nodes/"+maintKubeNode:
		// The restarted kubelet posts a fresh heartbeat.
		answer(http.StatusOK, fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":"True","lastHeartbeatTime":%q}]}}`,
			maintKubeNode, time.Now().UTC().Format(time.RFC3339Nano)))
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/nodes/"+maintKubeNode:
		var patch struct {
			Spec struct {
				Unschedulable bool `json:"unschedulable"`
			} `json:"spec"`
		}
		_ = json.Unmarshal(body, &patch)
		a.cordons = append(a.cordons, patch.Spec.Unschedulable)
		answer(http.StatusOK, `{}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pg-1/eviction"):
		answer(http.StatusTooManyRequests, `{"kind":"Status","reason":"TooManyRequests","message":"Cannot evict pod as it would violate the pod's disruption budget."}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eviction"):
		notFound()
	case r.Method == http.MethodPost && r.URL.Path == leasesPath:
		if a.lease != nil {
			answer(http.StatusConflict, `{"kind":"Status","reason":"AlreadyExists","message":"exists"}`)

			return
		}

		a.storeLease(w, body)
	case r.Method == http.MethodPut && r.URL.Path == lease:
		a.storeLease(w, body)
	case r.Method == http.MethodGet && r.URL.Path == lease && a.lease != nil:
		out, _ := json.Marshal(a.lease)
		answer(http.StatusOK, string(out))
	case r.Method == http.MethodDelete && r.URL.Path == lease:
		a.lease = nil
		answer(http.StatusOK, `{}`)
	default:
		notFound()
	}
}

// storeLease keeps the Lease in body as the current one, with a new resourceVersion.
func (a *maintKubeAPI) storeLease(w http.ResponseWriter, body []byte) {
	var l kubeLease
	if err := json.Unmarshal(body, &l); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	a.version++
	l.Metadata.UID, l.Metadata.ResourceVersion = "lease-uid", fmt.Sprint(a.version)
	a.lease = &l

	out, _ := json.Marshal(l)
	_, _ = w.Write(out)
}

// holdLock makes another run hold the upgrade lock for an hour.
func (a *maintKubeAPI) holdLock(holder string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	r := upgradeLockRequest{holder: holder, node: maintPeer, hostname: "worker-2", to: maintenanceLockTo, duration: time.Hour}
	l := r.lease(time.Now(), nil)
	a.version++
	l.Metadata.UID, l.Metadata.ResourceVersion = "lease-uid", fmt.Sprint(a.version)
	a.lease = &l
}

// maintCluster is worker-1 (registered as worker-1, cordoned or not) next to a second
// worker, behind the fake Talos endpoint, with the Kubernetes API of pods: every client the
// code opens goes to it.
func maintCluster(t *testing.T, cordoned bool, pods ...string) (*fakeTalos, *maintKubeAPI, string) {
	t.Helper()

	withDataDir(t)

	f := newFakeTalos()
	f.addNode(t, maintNode, "v1.11.0", machine.TypeWorker)
	f.addNode(t, maintPeer, "v1.11.0", machine.TypeWorker)

	ns := k8s.NewNodeStatus(k8s.NamespaceName, maintKubeNode)
	ns.TypedSpec().Nodename, ns.TypedSpec().NodeReady, ns.TypedSpec().Unschedulable = maintKubeNode, true, cordoned
	f.put(maintNode, ns)

	api := &maintKubeAPI{pods: pods}
	server := newFakeKubeAPI(t, nil)
	server.Config.Handler = http.HandlerFunc(api.handler)

	saved := kubeClients
	kubeClients = newKubeClientCache(func(kubeTarget) (*kubeClient, error) {
		return openKubeClient(context.Background(), server.kubeconfigFor(server.URL), nil, "")
	})
	t.Cleanup(func() { kubeClients = saved })

	return f, api, f.start(t, maintNode, maintPeer)
}

// maintRecorder is a MaintenanceListener keeping every event; on, when set, sees each one
// as it comes (on the run's goroutine).
type maintRecorder struct {
	mu     sync.Mutex
	events []maintenanceProgress
	on     func(maintenanceProgress)
	done   chan string
}

func newMaintRecorder(on func(maintenanceProgress)) *maintRecorder {
	return &maintRecorder{on: on, done: make(chan string, 1)}
}

func (r *maintRecorder) OnProgress(s string) {
	var p maintenanceProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.mu.Lock()
	r.events = append(r.events, p)
	r.mu.Unlock()

	if r.on != nil {
		r.on(p)
	}
}

func (r *maintRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *maintRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

// phases are the phases seen, consecutive repeats folded.
func (r *maintRecorder) phases() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, p := range r.events {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}

	return out
}

func (r *maintRecorder) last() maintenanceProgress {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.events[len(r.events)-1]
}

func TestNodeMaintenancePlanFake(t *testing.T) {
	_, _, cfg := maintCluster(t, false, maintWebPod, maintPgPod, maintCiliumPod, maintBarePod)

	out, err := NodeMaintenancePlan(cfg, "fake", "", maintNode)
	plan := decodeJSON[maintenancePlan](t, out, err)

	if plan.Node != maintNode || plan.KubeNode != maintKubeNode || plan.Hostname != "host-192-0-2-11" || plan.Cordoned || plan.ControlPlane {
		t.Errorf("plan = %s", out)
	}

	if len(plan.Blockers) != 0 {
		t.Errorf("blockers = %v", plan.Blockers)
	}

	kinds := map[string]string{}
	for _, p := range plan.Pods {
		kinds[p.Namespace+"/"+p.Name] = p.Kind
	}

	want := map[string]string{"web/web-1": drainEvict, "db/pg-1": drainEvict, "kube-system/cilium-x": drainDaemonSet, "default/debug": drainBare}
	for name, kind := range want {
		if kinds[name] != kind {
			t.Errorf("%s: kind %q, want %q", name, kinds[name], kind)
		}
	}

	for _, w := range []string{
		"PodDisruptionBudget db/pg-primary allows no disruption now: the drain waits for it",
		"default/debug has no controller: once evicted it is not recreated",
	} {
		if !slices.Contains(plan.Warnings, w) {
			t.Errorf("no %q in %v", w, plan.Warnings)
		}
	}
}

func TestNodeMaintenancePlanFakeRefusals(t *testing.T) {
	tests := []struct {
		name    string
		node    string
		setup   func(f *fakeTalos)
		wantErr string
	}{
		{name: "node outside the context", node: "192.0.2.19", wantErr: "not part of this context"},
		{name: "no kubelet", node: maintPeer, wantErr: errKubeNodeUnknown.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, cfg := maintCluster(t, false)

			if _, err := NodeMaintenancePlan(cfg, "fake", "", tt.node); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestNodeMaintenancePlanFakeCordoned(t *testing.T) {
	_, _, cfg := maintCluster(t, true, maintCiliumPod)

	out, err := NodeMaintenancePlan(cfg, "fake", "", maintNode)
	if plan := decodeJSON[maintenancePlan](t, out, err); !plan.Cordoned || len(plan.Pods) != 1 {
		t.Errorf("plan = %s", out)
	}
}

func TestKubeCordonFake(t *testing.T) {
	_, api, cfg := maintCluster(t, false)

	for _, on := range []bool{true, false} {
		if err := KubeCordon(cfg, "fake", "", maintNode, on); err != nil {
			t.Fatalf("cordon %t: %v", on, err)
		}
	}

	if got := api.cordoned(); !slices.Equal(got, []bool{true, false}) {
		t.Errorf("patches = %v", got)
	}

	if err := KubeCordon(cfg, "fake", "", maintPeer, true); err == nil || !strings.Contains(err.Error(), "Kubernetes name is unknown") {
		t.Errorf("node without a kubelet = %v", err)
	}

	if entries := readAudit(t, "fake", "cordon"); len(entries) != 2 {
		t.Errorf("cordon audit = %+v", entries)
	}
}

// A reboot run to its end: cordon, drain, reboot, the node goes down and comes back Ready,
// uncordon. The listener drives the node through its reboot. It waits two polls of
// upgradePollInterval (the node seen booting, then back).
func TestStartNodeMaintenanceRebootFake(t *testing.T) {
	f, api, cfg := maintCluster(t, false, maintWebPod, maintCiliumPod)

	r := newMaintRecorder(func(p maintenanceProgress) {
		switch {
		case p.Phase == phaseReboot:
			f.setStage(maintNode, runtime.MachineStageBooting, false)
		case p.Phase == phaseBack && p.Message == "the node is booting":
			f.setStage(maintNode, runtime.MachineStageRunning, true)
		}
	})

	StartNodeMaintenance(cfg, "fake", "", maintNode, " Reboot ", false, false, r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("OnDone = %q", msg)
	}

	if got, want := r.phases(), []string{phaseCordon, phaseDrain, phaseReboot, phaseBack, phaseUncordon}; !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if got := api.cordoned(); !slices.Equal(got, []bool{true, false}) {
		t.Errorf("patches = %v", got)
	}

	if len(f.called("Reboot "+maintNode)) != 1 {
		t.Errorf("reboots = %v", f.called("Reboot"))
	}

	api.mu.Lock()
	lease := api.lease
	api.mu.Unlock()

	if lease != nil {
		t.Errorf("the lock was not released: %+v", lease)
	}

	if entries := readAudit(t, "fake", "maintenance-reboot"); len(entries) != 1 || entries[0].Outcome != auditOK || entries[0].Params != "include-bare=false" {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartNodeMaintenanceFakeStops(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		setup    func(f *fakeTalos, api *maintKubeAPI)
		wantErr  string // "" for success
		wantLast string // the last message on success
		patches  []bool
		reboots  int
	}{
		{
			name: "drain only", action: maintenanceNone,
			wantLast: "worker-1 is drained and stays cordoned", patches: []bool{true},
		},
		{
			name: "shutdown refused", action: maintenanceShutdown,
			wantErr: "worker-1 stays cordoned", patches: []bool{true},
		},
		{
			name: "reboot refused", action: maintenanceReboot,
			setup: func(f *fakeTalos, _ *maintKubeAPI) {
				f.rebootErr = status.Error(codes.PermissionDenied, "not authorized")
			},
			wantErr: "worker-1 stays cordoned", patches: []bool{true}, reboots: 1,
		},
		{
			name: "lock held by another run", action: maintenanceReboot,
			setup:   func(_ *fakeTalos, api *maintKubeAPI) { api.holdLock("ichor/other") },
			wantErr: "another Ichor node maintenance (worker-2",
		},
		{
			name: "peer down", action: maintenanceReboot,
			setup:   func(f *fakeTalos, _ *maintKubeAPI) { f.setStage(maintPeer, runtime.MachineStageBooting, false) },
			wantErr: "maintenance refused: host-192-0-2-12 is booting",
		},
		{name: "unknown action", action: "format", wantErr: `unknown maintenance action "format"`},
		{
			// A Kubernetes API failure after the cordon still says the node stays cordoned.
			name: "API failure after the cordon", action: maintenanceNone,
			setup:   func(_ *fakeTalos, api *maintKubeAPI) { api.failAfterCordon = true },
			wantErr: "worker-1 stays cordoned", patches: []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, api, cfg := maintCluster(t, false, maintWebPod, maintCiliumPod)
			if tt.setup != nil {
				tt.setup(f, api)
			}

			r := newMaintRecorder(nil)
			StartNodeMaintenance(cfg, "fake", "", maintNode, tt.action, false, false, r)

			msg := r.wait(t)
			if tt.wantErr == "" && msg != "" || !strings.Contains(msg, tt.wantErr) {
				t.Fatalf("OnDone = %q, want %q", msg, tt.wantErr)
			}

			if tt.wantLast != "" && r.last().Message != tt.wantLast {
				t.Errorf("last event = %+v", r.last())
			}

			if got := api.cordoned(); !slices.Equal(got, tt.patches) {
				t.Errorf("patches = %v, want %v", got, tt.patches)
			}

			if got := len(f.called("Reboot")); got != tt.reboots {
				t.Errorf("reboots = %d, want %d", got, tt.reboots)
			}
		})
	}
}

// A budget that never allows pg-1's eviction holds the drain until the run is stopped: the
// node stays cordoned, nothing is rebooted, and the listener gets why.
func TestStartNodeMaintenanceFakeBlockedDrain(t *testing.T) {
	f, api, cfg := maintCluster(t, false, maintWebPod, maintPgPod)

	blocked := make(chan struct{})

	var once sync.Once

	r := newMaintRecorder(func(p maintenanceProgress) {
		if slices.ContainsFunc(p.Pods, func(d drainPod) bool { return d.State == podBlocked }) {
			once.Do(func() { close(blocked) })
		}
	})

	run := StartNodeMaintenance(cfg, "fake", "", maintNode, maintenanceReboot, false, false, r)

	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("the drain never reported the blocked pod")
	}

	run.Cancel()

	msg := r.wait(t)
	if !strings.Contains(msg, errDrainStopped.Error()) || !strings.Contains(msg, "worker-1 stays cordoned") {
		t.Fatalf("OnDone = %q", msg)
	}

	if got := r.last(); got.Phase != phaseDrain || !strings.Contains(got.Message, "1 waiting for a PodDisruptionBudget") {
		t.Errorf("last event = %+v", got)
	}

	if got := api.cordoned(); !slices.Equal(got, []bool{true}) {
		t.Errorf("patches = %v", got)
	}

	if len(f.called("Reboot")) != 0 {
		t.Error("a node whose drain did not finish was rebooted")
	}
}

// A drain that runs out of time leaves the node cordoned and names the cause.
func TestCordonAndDrainTimesOut(t *testing.T) {
	_, api, _ := maintCluster(t, false, maintPgPod)

	k, err := kubeClients.open(kubeTarget{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	var phases []string

	_, err = cordonAndDrain(ctx, k, maintKubeNode, false, func(phase, msg string, _ []drainPod) { phases = append(phases, phase+": "+msg) })
	if !errors.Is(err, errDrainStopped) || !strings.Contains(err.Error(), "worker-1 stays cordoned") {
		t.Fatalf("err = %v", err)
	}

	want := []string{"cordon: cordoning worker-1", "drain: evicting 1 pods", "drain: 0 of 1 pods evicted, 1 waiting for a PodDisruptionBudget"}
	if !slices.Equal(phases, want) {
		t.Errorf("phases = %v, want %v", phases, want)
	}

	if got := api.cordoned(); !slices.Equal(got, []bool{true}) {
		t.Errorf("patches = %v", got)
	}
}

// recheck right before the reboot: it passes while nothing changed, and refuses a node not
// back from a reboot yet and a lock another run took meanwhile.
func TestMaintenanceRecheckFake(t *testing.T) {
	const holder = "ichor/this-run"

	tests := []struct {
		name    string
		setup   func(f *fakeTalos, api *maintKubeAPI)
		wantErr string
	}{
		{name: "nothing changed"},
		{
			name:    "node still booting after a reboot",
			setup:   func(f *fakeTalos, _ *maintKubeAPI) { f.setStage(maintNode, runtime.MachineStageBooting, false) },
			wantErr: "not rebooting: node is booting, not running",
		},
		{
			name:    "lock taken by another run",
			setup:   func(_ *fakeTalos, api *maintKubeAPI) { api.holdLock("ichor/other") },
			wantErr: "the cluster upgrade lock expired or was taken by another run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, api, cfg := maintCluster(t, false)
			api.holdLock(holder)

			s, release, err := acquireNode(cfg, "fake", maintNode)
			if err != nil {
				t.Fatal(err)
			}
			defer release()

			k, err := kubeClients.open(kubeTarget{})
			if err != nil {
				t.Fatal(err)
			}

			api.mu.Lock()
			lease := *api.lease
			api.mu.Unlock()

			if tt.setup != nil {
				tt.setup(f, api)
			}

			m := maintenance{node: maintNode, action: maintenanceReboot}
			lock := &upgradeLock{lease: &lease}

			err = m.recheck(t.Context(), s, k, lock, maintenancePlan{KubeNode: maintKubeNode, Acknowledge: []string{}})
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("recheck = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
