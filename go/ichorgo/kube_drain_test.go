package ichorgo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const drainPodsJSON = `{"items":[
 {"metadata":{"name":"web-1","namespace":"web","uid":"u1","labels":{"app":"web"},
   "ownerReferences":[{"kind":"ReplicaSet","name":"web-5d","controller":true}]},
  "spec":{"nodeName":"w1","volumes":[{"emptyDir":{}}]}},
 {"metadata":{"name":"pg-1","namespace":"db","uid":"u2","labels":{"app":"pg","role":"primary"},
   "ownerReferences":[{"kind":"Cluster","name":"pg","controller":true}]},
  "spec":{"nodeName":"w1"}},
 {"metadata":{"name":"cilium-x","namespace":"kube-system","uid":"u3",
   "ownerReferences":[{"kind":"DaemonSet","name":"cilium","controller":true}]},"spec":{"nodeName":"w1"}},
 {"metadata":{"name":"kube-apiserver-w1","namespace":"kube-system","uid":"u4",
   "annotations":{"kubernetes.io/config.mirror":"abc"},
   "ownerReferences":[{"kind":"Node","name":"w1","controller":true}]},"spec":{"nodeName":"w1"}},
 {"metadata":{"name":"debug","namespace":"default","uid":"u5"},"spec":{"nodeName":"w1"}}
]}`

const drainPDBsJSON = `{"items":[
 {"metadata":{"name":"pg-primary","namespace":"db"},"spec":{"selector":{"matchLabels":{"role":"primary"}}},"status":{"disruptionsAllowed":0}},
 {"metadata":{"name":"other-ns","namespace":"web2"},"spec":{"selector":{}},"status":{"disruptionsAllowed":5}}
]}`

// drainAPI is a fake API server: pg-1's eviction is refused (429) blockFor times, then every
// evicted pod is gone; it records evictions and node patches.
type drainAPI struct {
	mu        sync.Mutex
	blockFor  int
	forbid    bool // every eviction is refused (403)
	evicted   map[string]bool
	evictions []string
	patches   []string
}

func (d *drainAPI) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	d.mu.Lock()
	defer d.mu.Unlock()

	notFound := func() {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"not found"}`)
	}

	switch {
	case r.URL.Path == "/version":
		_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pods":
		if r.URL.Query().Get("fieldSelector") != "spec.nodeName=w1" {
			http.Error(w, "bad selector", http.StatusBadRequest)

			return
		}

		_, _ = io.WriteString(w, drainPodsJSON)
	case r.Method == http.MethodGet && r.URL.Path == "/apis/policy/v1/poddisruptionbudgets":
		_, _ = io.WriteString(w, drainPDBsJSON)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/nodes/w1":
		_, _ = io.WriteString(w, `{"metadata":{"name":"w1"},"spec":{"unschedulable":true}}`)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/nodes/w1":
		d.patches = append(d.patches, r.Header.Get("Content-Type")+" "+string(body))
		_, _ = io.WriteString(w, `{}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/eviction"):
		pod := strings.TrimSuffix(r.URL.Path, "/eviction")
		d.evictions = append(d.evictions, pod)

		if d.forbid {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"evictions are forbidden"}`)

			return
		}

		if strings.HasSuffix(pod, "/pg-1") && d.blockFor > 0 {
			d.blockFor--
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"TooManyRequests","message":"Cannot evict pod as it would violate the pod's disruption budget."}`)

			return
		}

		d.evicted[pod] = true
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/"):
		if d.evicted[r.URL.Path] {
			notFound()

			return
		}

		_, _ = io.WriteString(w, `{"metadata":{"uid":"same"}}`)
	default:
		notFound()
	}
}

func newDrainAPI(t *testing.T, blockFor int) (*drainAPI, *kubeClient) {
	t.Helper()

	d := &drainAPI{blockFor: blockFor, evicted: map[string]bool{}}
	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(d.handler)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return d, k
}

func TestDrainPodsClassifiesLikeKubectl(t *testing.T) {
	_, k := newDrainAPI(t, 0)

	pods, err := drainPods(context.Background(), k, "w1")
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]drainPod{}
	for _, p := range pods {
		got[p.Namespace+"/"+p.Name] = p
	}

	checks := map[string]string{
		"web/web-1": drainEvict, "db/pg-1": drainEvict, "default/debug": drainBare,
		"kube-system/cilium-x": drainDaemonSet, "kube-system/kube-apiserver-w1": drainStatic,
	}
	for name, kind := range checks {
		if got[name].Kind != kind {
			t.Errorf("%s: kind %q, want %q", name, got[name].Kind, kind)
		}
	}

	if pg := got["db/pg-1"]; pg.PDB != "pg-primary" || pg.PDBAllowed != 0 || pg.Owner != "Cluster/pg" {
		t.Errorf("pg-1 = %+v", pg)
	}

	if web := got["web/web-1"]; !web.EmptyDir || web.PDB != "" || web.PDBAllowed != -1 {
		t.Errorf("web-1 = %+v", web)
	}

	// Pods a drain evicts come first.
	if pods[len(pods)-1].Kind != drainDaemonSet && pods[len(pods)-1].Kind != drainStatic {
		t.Errorf("order: last is %+v", pods[len(pods)-1])
	}

	if n := len(toEvict(pods, false)); n != 2 {
		t.Errorf("toEvict without bare = %d, want 2", n)
	}

	if n := len(toEvict(pods, true)); n != 3 {
		t.Errorf("toEvict with bare = %d, want 3", n)
	}
}

func TestDrainWaitsForBudgetThenFinishes(t *testing.T) {
	d, k := newDrainAPI(t, 2)

	pods, err := drainPods(context.Background(), k, "w1")
	if err != nil {
		t.Fatal(err)
	}

	evict := toEvict(pods, false)

	var sawBlocked bool

	report := func(p []drainPod) {
		for _, pod := range p {
			if pod.State == podBlocked && strings.Contains(pod.Reason, "pg-primary") {
				sawBlocked = true
			}
		}
	}

	if err := drain(context.Background(), k, evict, report, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	for _, p := range evict {
		if p.State != podGone {
			t.Errorf("%s: state %s", p.Name, p.State)
		}
	}

	if !sawBlocked {
		t.Error("the PDB block was never reported")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if pg := countSuffix(d.evictions, "/pg-1"); pg != 3 {
		t.Errorf("pg-1 evictions = %d, want 3 (2 refused, 1 accepted)", pg)
	}

	for _, e := range d.evictions {
		if strings.Contains(e, "cilium") || strings.Contains(e, "apiserver") || strings.Contains(e, "debug") {
			t.Errorf("evicted %s", e)
		}
	}
}

func TestDrainStopsWhenCancelled(t *testing.T) {
	_, k := newDrainAPI(t, 1_000_000)

	pods, _ := drainPods(context.Background(), k, "w1")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)

	defer cancel()

	if err := drain(ctx, k, toEvict(pods, false), func([]drainPod) {}, 5*time.Millisecond); err != errDrainStopped {
		t.Errorf("err = %v, want errDrainStopped", err)
	}
}

func TestCordonPatchesTheNode(t *testing.T) {
	d, k := newDrainAPI(t, 0)

	if err := setUnschedulable(context.Background(), k, "w1", true); err != nil {
		t.Fatal(err)
	}

	if err := setUnschedulable(context.Background(), k, "w1", false); err != nil {
		t.Fatal(err)
	}

	want := []string{
		`application/merge-patch+json {"spec":{"unschedulable":true}}`,
		`application/merge-patch+json {"spec":{"unschedulable":false}}`,
	}
	if strings.Join(d.patches, "\n") != strings.Join(want, "\n") {
		t.Errorf("patches = %q", d.patches)
	}

	if err := setUnschedulable(context.Background(), k, "missing", true); err == nil || !strings.Contains(err.Error(), "cordon missing") {
		t.Errorf("missing node: %v", err)
	}
}

func TestWaitBackNeedsADownThenReady(t *testing.T) {
	steps := []backObservation{
		{reachable: true, running: true, kubeReady: true}, // not rebooted yet
		{},                               // down
		{reachable: true},                // booting
		{reachable: true, running: true}, // kubelet not Ready yet
		{reachable: true, running: true, kubeReady: true},
	}

	i := 0
	observe := func(context.Context) backObservation {
		o := steps[min(i, len(steps)-1)]
		i++

		return o
	}

	var msgs []string

	if err := waitBack(context.Background(), observe, func(m string) { msgs = append(msgs, m) }, time.Millisecond); err != nil {
		t.Fatal(err)
	}

	if i != len(steps) || msgs[len(msgs)-1] != "the node is back and Ready" {
		t.Errorf("polls %d, messages %q", i, msgs)
	}
}

func TestMaintenanceRefusal(t *testing.T) {
	plan := maintenancePlan{Blockers: []string{"node is unreachable"}, Acknowledge: []string{"only endpoint"}}

	if err := (maintenance{action: maintenanceNone}).refusal(plan); err != nil {
		t.Errorf("drain only refused: %v", err)
	}

	if err := (maintenance{action: maintenanceReboot}).refusal(plan); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("blockers: %v", err)
	}

	plan.Blockers = nil

	if err := (maintenance{action: maintenanceReboot}).refusal(plan); err == nil || !strings.Contains(err.Error(), "until confirmed") {
		t.Errorf("acknowledge: %v", err)
	}

	if err := (maintenance{action: maintenanceReboot, acknowledged: true}).refusal(plan); err != nil {
		t.Errorf("acknowledged: %v", err)
	}
}

func TestMaintenanceLockDescription(t *testing.T) {
	info := upgradeLockInfo{hostname: "w1", to: maintenanceLockTo, since: time.Now(), expires: time.Now()}
	if d := info.describe(); !strings.Contains(d, "node maintenance (w1,") {
		t.Errorf("describe = %q", d)
	}
}

func TestDemoMaintenancePlan(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := NodeMaintenancePlan(demo, "", "", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"controlPlane":true`) || !strings.Contains(out, "postgres-primary") {
		t.Errorf("plan = %s", out)
	}

	if err := KubeCordon(demo, "", "", "192.0.2.10", true); err != errDemoUnavailable {
		t.Errorf("demo cordon: %v", err)
	}
}

func TestNodeReadyNeedsAFreshHeartbeat(t *testing.T) {
	rebootAt := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	node := func(status string, heartbeat time.Time) string {
		return `{"status":{"conditions":[{"type":"MemoryPressure","status":"False"},` +
			`{"type":"Ready","status":"` + status + `","lastHeartbeatTime":"` + heartbeat.Format(time.RFC3339) + `"}]}}`
	}

	cases := map[string]struct {
		body string
		want bool
	}{
		"stale Ready from before the reboot": {node("True", rebootAt.Add(-time.Second)), false},
		"fresh Ready":                        {node("True", rebootAt.Add(time.Minute)), true},
		"fresh NotReady":                     {node("False", rebootAt.Add(time.Minute)), false},
	}

	for name, c := range cases {
		f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes/w1": c.body})

		k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
		if err != nil {
			t.Fatal(err)
		}

		if got := kubeNodeReadySince(context.Background(), k, "w1", rebootAt); got != c.want {
			t.Errorf("%s: ready = %v", name, got)
		}
	}
}

func TestLockStillHeld(t *testing.T) {
	lease := func(holder string, renew time.Time) string {
		stamp := renew.UTC().Format(leaseTimeFormat)

		return `{"spec":{"holderIdentity":"` + holder + `","leaseDurationSeconds":600,"acquireTime":"` + stamp + `","renewTime":"` + stamp + `"}}`
	}
	path := "GET " + leasesPath + "/" + upgradeLockName

	mine := &upgradeLock{lease: &kubeLease{Spec: leaseSpec{HolderIdentity: "ichor/me"}}}

	cases := map[string]struct {
		answer string
		ok     bool
	}{
		"still mine":    {lease("ichor/me", time.Now()), true},
		"taken":         {lease("ichor/other", time.Now()), false},
		"expired":       {lease("ichor/me", time.Now().Add(-time.Hour)), false},
		"deleted (404)": {"", false},
	}

	for name, c := range cases {
		answers := map[string]string{}
		if c.answer != "" {
			answers[path] = c.answer
		}

		f := newFakeKubeAPI(t, answers)

		k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
		if err != nil {
			t.Fatal(err)
		}

		if err := mine.stillHeld(context.Background(), k); (err == nil) != c.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}

	if err := (&upgradeLock{}).stillHeld(context.Background(), nil); err != nil {
		t.Errorf("no lock taken: %v", err)
	}
}

func countSuffix(items []string, suffix string) int {
	n := 0

	for _, s := range items {
		if strings.HasSuffix(s, suffix) {
			n++
		}
	}

	return n
}
