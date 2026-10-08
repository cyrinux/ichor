package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// useDrainAPI points the exported Kubernetes calls at a drainAPI.
func useDrainAPI(t *testing.T, blockFor int) *drainAPI {
	t.Helper()

	d := &drainAPI{blockFor: blockFor, evicted: map[string]bool{}}
	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(d.handler)
	useFakeKube(t, f)

	return d
}

func TestKubeNodeCordonNeedsNoTalos(t *testing.T) {
	d := useDrainAPI(t, 0)

	if err := KubeNodeCordon("cfg", "ctx", "", "w1", true); err != nil {
		t.Fatal(err)
	}

	if want := `application/merge-patch+json {"spec":{"unschedulable":true}}`; len(d.patches) != 1 || d.patches[0] != want {
		t.Errorf("patches = %q", d.patches)
	}
}

func TestKubeDrainPlan(t *testing.T) {
	useDrainAPI(t, 0)

	out, err := KubeDrainPlan("cfg", "ctx", "", "w1")
	if err != nil {
		t.Fatal(err)
	}

	var plan maintenancePlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if plan.KubeNode != "w1" || plan.Hostname != "w1" || !plan.Cordoned || len(plan.Pods) != 5 {
		t.Errorf("plan = %+v", plan)
	}

	if len(plan.Blockers)+len(plan.Acknowledge) != 0 {
		t.Errorf("a kube drain has no Talos checks: %+v", plan)
	}

	// The drain's own warnings, as the Talos maintenance plan gives them.
	want := []string{
		"PodDisruptionBudget db/pg-primary allows no disruption now: the drain waits for it",
		"default/debug has no controller: once evicted it is not recreated",
	}

	if len(plan.Warnings) != len(want) {
		t.Fatalf("warnings = %q", plan.Warnings)
	}

	for _, w := range want {
		if !slices.Contains(plan.Warnings, w) {
			t.Errorf("warnings %q lack %q", plan.Warnings, w)
		}
	}
}

// A kube node is named by its hostname: the demo plan still knows its role and static pods.
func TestDemoKubeDrainPlanByHostname(t *testing.T) {
	cp := demoKubeDrainPlan("demo-cp-1")
	if !cp.ControlPlane || cp.Node != "demo-cp-1" || cp.KubeNode != "demo-cp-1" {
		t.Errorf("control plane plan = %+v", cp)
	}

	if !slices.ContainsFunc(cp.Pods, func(p drainPod) bool { return p.Kind == drainStatic && p.Name == "kube-apiserver-demo-cp-1" }) {
		t.Errorf("no static pod in %+v", cp.Pods)
	}

	if len(cp.Blockers)+len(cp.Acknowledge) != 0 || len(cp.Warnings) != 1 {
		t.Errorf("a kube drain keeps the drain warnings only: %+v", cp)
	}

	if w := demoKubeDrainPlan("demo-worker-1"); w.ControlPlane {
		t.Errorf("worker plan = %+v", w)
	}
}

// recDrain records the phases and the end of a drain run.
type recDrain struct {
	mu     sync.Mutex
	phases []string
	done   chan string
}

func (r *recDrain) OnProgress(js string) {
	var p maintenanceProgress
	_ = json.Unmarshal([]byte(js), &p)

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.phases) == 0 || r.phases[len(r.phases)-1] != p.Phase {
		r.phases = append(r.phases, p.Phase)
	}
}

func (r *recDrain) OnDone(errMessage string) { r.done <- errMessage }

func (r *recDrain) wait(t *testing.T, within time.Duration) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(within):
		t.Fatal("the drain did not end")

		return ""
	}
}

func TestStartKubeDrainCordonsThenEvicts(t *testing.T) {
	if testing.Short() {
		t.Skip("waits a drain poll")
	}

	d := useDrainAPI(t, 0)
	rec := &recDrain{done: make(chan string, 1)}

	StartKubeDrain("cfg", "ctx", "", "w1", false, rec)

	if msg := rec.wait(t, 30*time.Second); msg != "" {
		t.Fatalf("drain failed: %s", msg)
	}

	if got := strings.Join(rec.phases, ","); got != "cordon,drain" {
		t.Errorf("phases = %s", got)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// The ReplicaSet and the Cluster pods; not the DaemonSet, static or bare one.
	if len(d.evictions) != 2 || len(d.patches) != 1 || !strings.Contains(d.patches[0], `"unschedulable":true`) {
		t.Errorf("evictions = %q, patches = %q", d.evictions, d.patches)
	}
}

func TestStartKubeDrainStopsWhenCancelled(t *testing.T) {
	useDrainAPI(t, 1<<20) // pg-1 is never evictable
	rec := &recDrain{done: make(chan string, 1)}

	StartKubeDrain("cfg", "ctx", "", "w1", false, rec).Cancel()

	if msg := rec.wait(t, 10*time.Second); msg == "" {
		t.Fatal("a cancelled drain reports success")
	}
}
