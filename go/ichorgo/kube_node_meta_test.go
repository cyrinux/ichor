package ichorgo

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const nodeMetaJSON = `{
  "metadata": {"name": "w1", "resourceVersion": "42", "labels": {
    "kubernetes.io/hostname": "w1", "topology.kubernetes.io/zone": "zone-a",
    "node-role.kubernetes.io/worker": "", "example.com/pool": "blue"}},
  "spec": {"unschedulable": true, "taints": [
    {"key": "maintenance", "value": "true", "effect": "NoSchedule", "timeAdded": "2026-10-01T10:00:00Z"},
    {"key": "node.kubernetes.io/unschedulable", "effect": "NoSchedule", "timeAdded": "2026-10-02T10:00:00Z"},
    {"key": "example.com/gpu", "value": "a", "effect": "NoSchedule"}]}}`

const nodeMetaPodsJSON = `{"items": [
  {"metadata": {"name": "web-1", "namespace": "apps", "ownerReferences": [{"kind": "ReplicaSet", "name": "web-5d8f", "controller": true}]}},
  {"metadata": {"name": "db-0", "namespace": "apps", "ownerReferences": [{"kind": "StatefulSet", "name": "db", "controller": true}]},
   "spec": {"tolerations": [{"key": "dedicated", "operator": "Exists", "effect": "NoExecute"}]}},
  {"metadata": {"name": "any-0", "namespace": "ops"}, "spec": {"tolerations": [{"operator": "Exists"}]}},
  {"metadata": {"name": "other-0", "namespace": "ops"}, "spec": {"tolerations": [{"key": "dedicated", "value": "batch", "effect": "NoExecute"}]}},
  {"metadata": {"name": "agent-x", "namespace": "kube-system", "ownerReferences": [{"kind": "DaemonSet", "name": "agent", "controller": true}]}}]}`

// nodeMetaAPI is a fake API server with the node w1, its pods, and the patches it gets.
type nodeMetaAPI struct {
	mu       sync.Mutex
	node     string
	dryRun   int    // the dry run's HTTP status, 200 when 0
	dryMsg   string // its Status message
	patchErr int    // the real patch's status, 200 when 0
	patches  []string
	dryRuns  []string
	podLists int
}

func useNodeMetaAPI(t *testing.T) *nodeMetaAPI {
	t.Helper()

	a := &nodeMetaAPI{node: nodeMetaJSON}
	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(a.handler)
	useFakeKube(t, f)

	return a
}

func (a *nodeMetaAPI) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	a.mu.Lock()
	defer a.mu.Unlock()

	status := func(code int, reason, message string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "Status", "code": code, "reason": reason, "message": message})
	}

	switch {
	case r.URL.Path == "/version":
		_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/nodes/w1":
		_, _ = io.WriteString(w, a.node)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pods":
		a.podLists++
		_, _ = io.WriteString(w, nodeMetaPodsJSON)
	case r.Method == http.MethodGet && r.URL.Path == "/apis/policy/v1/poddisruptionbudgets":
		_, _ = io.WriteString(w, `{"items":[]}`)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/nodes/w1":
		if r.Header.Get("Content-Type") != "application/merge-patch+json" {
			status(http.StatusUnsupportedMediaType, "UnsupportedMediaType", "bad patch type")

			return
		}

		if r.URL.Query().Get("dryRun") == "All" {
			a.dryRuns = append(a.dryRuns, string(body))

			if a.dryRun != 0 {
				status(a.dryRun, "Invalid", a.dryMsg)

				return
			}
		} else {
			a.patches = append(a.patches, string(body))

			if a.patchErr != 0 {
				status(a.patchErr, "Conflict", "the object has been modified")

				return
			}
		}

		_, _ = io.WriteString(w, a.node)
	default:
		status(http.StatusNotFound, "NotFound", "not found")
	}
}

func TestKubeNodeMetaMarksProtectedKeys(t *testing.T) {
	useNodeMetaAPI(t)

	out, err := KubeNodeMeta("cfg", "ctx", "", "w1")
	if err != nil {
		t.Fatal(err)
	}

	var m nodeMeta
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}

	if m.Node != "w1" || m.ResourceVersion != "42" || len(m.Labels) != 4 || len(m.Taints) != 3 {
		t.Fatalf("meta = %+v", m)
	}

	labels := map[string]bool{}
	for _, l := range m.Labels {
		labels[l.Key] = l.Protected
	}

	want := map[string]bool{"example.com/pool": false, "kubernetes.io/hostname": true, "node-role.kubernetes.io/worker": true, "topology.kubernetes.io/zone": true}
	for key, protected := range want {
		if p, ok := labels[key]; !ok || p != protected {
			t.Errorf("label %s protected = %v, want %v", key, p, protected)
		}
	}

	if m.Labels[0].Key != "example.com/pool" {
		t.Errorf("labels not sorted: %+v", m.Labels)
	}

	keys := []string{m.Taints[0].Key, m.Taints[1].Key, m.Taints[2].Key}
	if strings.Join(keys, ",") != "example.com/gpu,maintenance,node.kubernetes.io/unschedulable" {
		t.Errorf("taints not sorted: %v", keys)
	}

	if m.Taints[0].Protected || m.Taints[1].Protected || !m.Taints[2].Protected {
		t.Errorf("taint protection: %+v", m.Taints)
	}

	if m.Taints[1].TimeAdded != time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC).Unix() || m.Taints[0].TimeAdded != 0 {
		t.Errorf("timeAdded: %+v", m.Taints)
	}
}

func planFor(t *testing.T, edits string) nodeMetaPlan {
	t.Helper()

	out, err := KubeNodeMetaPlan("cfg", "ctx", "", "w1", edits)
	if err != nil {
		t.Fatal(err)
	}

	var plan nodeMetaPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	return plan
}

func TestKubeNodeMetaPlanDiff(t *testing.T) {
	cases := []struct {
		name, edits string
		check       func(t *testing.T, p nodeMetaPlan)
	}{
		{"add and change labels", `{"setLabels":{"example.com/rack":"r1","example.com/pool":"green"}}`, func(t *testing.T, p nodeMetaPlan) {
			if len(p.Labels.Added) != 1 || p.Labels.Added[0] != (nodeMetaKV{"example.com/rack", "r1"}) {
				t.Errorf("added = %+v", p.Labels.Added)
			}

			if len(p.Labels.Changed) != 1 || p.Labels.Changed[0] != (nodeMetaLabelChange{"example.com/pool", "blue", "green"}) {
				t.Errorf("changed = %+v", p.Labels.Changed)
			}
		}},
		{"remove a label", `{"removeLabels":["example.com/pool","example.com/absent"]}`, func(t *testing.T, p nodeMetaPlan) {
			if len(p.Labels.Removed) != 1 || p.Labels.Removed[0] != (nodeMetaKV{"example.com/pool", "blue"}) {
				t.Errorf("removed = %+v", p.Labels.Removed)
			}
		}},
		{"remove the stray taint", `{"taints":[{"key":"example.com/gpu","value":"a","effect":"NoSchedule"}]}`, func(t *testing.T, p nodeMetaPlan) {
			if len(p.Taints.Removed) != 1 || p.Taints.Removed[0].Key != "maintenance" || len(p.Taints.Added)+len(p.Taints.Changed) != 0 {
				t.Errorf("taints = %+v", p.Taints)
			}
		}},
		{"change a taint value", `{"taints":[{"key":"maintenance","value":"true","effect":"NoSchedule"},{"key":"example.com/gpu","value":"b","effect":"NoSchedule"}]}`, func(t *testing.T, p nodeMetaPlan) {
			if len(p.Taints.Changed) != 1 || p.Taints.Changed[0] != (nodeMetaTaintChange{"example.com/gpu", "NoSchedule", "a", "b"}) {
				t.Errorf("changed = %+v", p.Taints.Changed)
			}
		}},
		{"same as the node", `{"setLabels":{"example.com/pool":"blue"},"taints":[{"key":"maintenance","value":"true","effect":"NoSchedule"},{"key":"example.com/gpu","value":"a","effect":"NoSchedule"}]}`, func(t *testing.T, p nodeMetaPlan) {
			if !p.NoChange || len(p.Blockers)+len(p.Warnings) != 0 {
				t.Errorf("plan = %+v", p)
			}
		}},
		{"remove a role label", `{"removeLabels":["node-role.kubernetes.io/worker"],"force":true}`, func(t *testing.T, p nodeMetaPlan) {
			if len(p.Blockers) != 0 || !strings.Contains(strings.Join(p.Warnings, "|"), "leaves its role worker") {
				t.Errorf("plan = %+v", p)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := useNodeMetaAPI(t)
			p := planFor(t, tc.edits)

			if p.Node != "w1" || p.Evicts == nil || p.Blockers == nil || p.Warnings == nil {
				t.Errorf("plan = %+v", p)
			}

			if !p.NoChange && len(p.Blockers) != 0 {
				t.Errorf("blockers = %v", p.Blockers)
			}

			if p.NoChange != (len(a.dryRuns) == 0) {
				t.Errorf("dry runs = %q for noChange %v", a.dryRuns, p.NoChange)
			}

			if len(a.patches) != 0 {
				t.Errorf("a plan patched: %q", a.patches)
			}

			tc.check(t, p)
		})
	}
}

func TestKubeNodeMetaPlanDryRunRefusalIsABlocker(t *testing.T) {
	a := useNodeMetaAPI(t)
	a.dryRun, a.dryMsg = http.StatusUnprocessableEntity, `Node "w1" is invalid: metadata.labels: Invalid value: "example.kubernetes.io/x"`

	p := planFor(t, `{"setLabels":{"example.com/x":"1"}}`)

	if len(p.Blockers) != 1 || p.Blockers[0] != a.dryMsg {
		t.Errorf("blockers = %q", p.Blockers)
	}

	var sent map[string]map[string]any
	if err := json.Unmarshal([]byte(a.dryRuns[0]), &sent); err != nil {
		t.Fatal(err)
	}

	if sent["metadata"]["resourceVersion"] != "42" || sent["spec"] != nil {
		t.Errorf("dry run body = %s", a.dryRuns[0])
	}
}

func TestKubeNodeMetaPlanNoExecuteEvictions(t *testing.T) {
	a := useNodeMetaAPI(t)

	p := planFor(t, `{"taints":[{"key":"maintenance","value":"true","effect":"NoSchedule"},{"key":"example.com/gpu","value":"a","effect":"NoSchedule"},{"key":"dedicated","value":"gpu","effect":"NoExecute"}]}`)

	// web-1 tolerates nothing, other-0 another value; db-0 and any-0 tolerate it, the
	// DaemonSet pod stays.
	got := make([]string, len(p.Evicts))
	for i, e := range p.Evicts {
		got[i] = e.Namespace + "/" + e.Name + "@" + e.Owner
	}

	if strings.Join(got, ",") != "apps/web-1@ReplicaSet/web-5d8f,ops/other-0@" {
		t.Errorf("evicts = %v", got)
	}

	if len(p.Warnings) != 1 || p.Warnings[0] != "2 pods will be evicted at once" {
		t.Errorf("warnings = %q", p.Warnings)
	}

	if a.podLists != 1 {
		t.Errorf("pods listed %d times", a.podLists)
	}

	if q := planFor(t, `{"setLabels":{"example.com/rack":"r1"}}`); len(q.Evicts) != 0 || a.podLists != 1 {
		t.Errorf("a label edit lists pods: %+v", q)
	}
}

func TestKubeNodeMetaPlanProtectedNeedsForce(t *testing.T) {
	useNodeMetaAPI(t)

	p := planFor(t, `{"removeLabels":["topology.kubernetes.io/zone"]}`)
	if len(p.Blockers) != 1 || !strings.Contains(p.Blockers[0], "protected label topology.kubernetes.io/zone") {
		t.Errorf("blockers = %q", p.Blockers)
	}

	p = planFor(t, `{"removeLabels":["topology.kubernetes.io/zone"],"force":true}`)
	if len(p.Blockers) != 0 || len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0], "removes the protected label") {
		t.Errorf("forced plan = %+v", p)
	}
}

func TestKubeNodeMetaPlanStaleNode(t *testing.T) {
	useNodeMetaAPI(t)

	p := planFor(t, `{"setLabels":{"example.com/rack":"r1"},"resourceVersion":"41"}`)
	if len(p.Blockers) != 1 || p.Blockers[0] != nodeMetaChanged || len(p.Warnings) != 1 || p.Warnings[0] != nodeMetaChanged {
		t.Errorf("plan = %+v", p)
	}
}

func TestParseNodeMetaEditsRefuses(t *testing.T) {
	cases := map[string]string{
		"bad JSON":         `{`,
		"label key":        `{"setLabels":{"bad key":"x"}}`,
		"label prefix":     `{"setLabels":{"Example.com/x":"x"}}`,
		"long name":        `{"setLabels":{"` + strings.Repeat("a", 64) + `":"x"}}`,
		"label value":      `{"setLabels":{"a":"-x"}}`,
		"long value":       `{"setLabels":{"a":"` + strings.Repeat("v", 64) + `"}}`,
		"set and removed":  `{"setLabels":{"a":"x"},"removeLabels":["a"]}`,
		"effect":           `{"taints":[{"key":"a","effect":"Evict"}]}`,
		"duplicate taint":  `{"taints":[{"key":"a","effect":"NoSchedule"},{"key":"a","value":"b","effect":"NoSchedule"}]}`,
		"cordon taint":     `{"taints":[{"key":"node.kubernetes.io/unschedulable","effect":"NoSchedule"}]}`,
		"taint key prefix": `{"taints":[{"key":"/a","effect":"NoSchedule"}]}`,
	}

	for name, edits := range cases {
		if _, err := parseNodeMetaEdits(edits); err == nil {
			t.Errorf("%s: %s accepted", name, edits)
		}
	}

	ok := `{"setLabels":{"example.com/pool":"","a.b/c_d.e":"V-1"},"taints":[{"key":"a","effect":"NoSchedule"},{"key":"a","effect":"NoExecute"}]}`
	if _, err := parseNodeMetaEdits(ok); err != nil {
		t.Errorf("%s refused: %v", ok, err)
	}
}

func TestKubeNodeMetaApplyPatch(t *testing.T) {
	withDataDir(t)

	a := useNodeMetaAPI(t)

	edits := `{"removeLabels":["example.com/pool"],"setLabels":{"example.com/rack":"r1"},"taints":[{"key":"example.com/gpu","value":"a","effect":"NoSchedule"}],"resourceVersion":"42"}`
	if err := KubeNodeMetaApply("cfg", "ctx", "", "w1", edits, false); err != nil {
		t.Fatal(err)
	}

	if len(a.patches) != 1 {
		t.Fatalf("patches = %q", a.patches)
	}

	var sent struct {
		Metadata struct {
			ResourceVersion string             `json:"resourceVersion"`
			Labels          map[string]*string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Taints []nodeTaintObject `json:"taints"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(a.patches[0]), &sent); err != nil {
		t.Fatal(err)
	}

	if sent.Metadata.ResourceVersion != "42" || sent.Metadata.Labels["example.com/pool"] != nil || *sent.Metadata.Labels["example.com/rack"] != "r1" || len(sent.Metadata.Labels) != 2 {
		t.Errorf("metadata = %s", a.patches[0])
	}

	if !strings.Contains(a.patches[0], `"example.com/pool":null`) {
		t.Errorf("the removed label is not null: %s", a.patches[0])
	}

	// The cordon taint is kept as the server has it, the stray one dropped.
	taints := sent.Spec.Taints
	if len(taints) != 2 || taints[0].Key != taintUnschedulable || taints[0].TimeAdded != "2026-10-02T10:00:00Z" || taints[1].Key != "example.com/gpu" {
		t.Errorf("taints = %+v", taints)
	}

	entries := readAudit(t, "ctx", "node-meta")
	if len(entries) != 1 || entries[0].Node != "w1" || entries[0].Outcome != auditOK {
		t.Fatalf("audit = %+v", entries)
	}

	if want := "+1 label (example.com/rack=r1), -1 label (example.com/pool=blue), -1 taint (maintenance=true:NoSchedule)"; entries[0].Params != want {
		t.Errorf("params = %q, want %q", entries[0].Params, want)
	}
}

func TestKubeNodeMetaApplyLabelsKeepTaints(t *testing.T) {
	a := useNodeMetaAPI(t)

	if err := KubeNodeMetaApply("cfg", "ctx", "", "w1", `{"setLabels":{"example.com/rack":"r1"}}`, false); err != nil {
		t.Fatal(err)
	}

	if len(a.patches) != 1 || strings.Contains(a.patches[0], "taints") {
		t.Errorf("a label edit sends the taints: %q", a.patches)
	}
}

func TestKubeNodeMetaApplyRefusals(t *testing.T) {
	cases := []struct {
		name, edits string
		force       bool
		patchErr    int
		want        error
		wantText    string
		patches     int
	}{
		{name: "conflict", edits: `{"setLabels":{"example.com/rack":"r1"}}`, patchErr: http.StatusConflict, want: errNodeMetaChanged, patches: 1},
		{name: "stale read", edits: `{"setLabels":{"example.com/rack":"r1"},"resourceVersion":"7"}`, want: errNodeMetaChanged},
		{name: "protected", edits: `{"removeLabels":["topology.kubernetes.io/zone"]}`, wantText: "allow protected changes"},
		{name: "protected forced", edits: `{"removeLabels":["topology.kubernetes.io/zone"]}`, force: true, patches: 1},
		{name: "protected forced by edits", edits: `{"removeLabels":["topology.kubernetes.io/zone"],"force":true}`, patches: 1},
		{name: "invalid", edits: `{"taints":[{"key":"a","effect":"Never"}]}`, wantText: "effect"},
		{name: "no change", edits: `{"setLabels":{"example.com/pool":"blue"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := useNodeMetaAPI(t)
			a.patchErr = tc.patchErr

			err := KubeNodeMetaApply("cfg", "ctx", "", "w1", tc.edits, tc.force)

			switch {
			case tc.want != nil:
				if err == nil || err.Error() != tc.want.Error() {
					t.Errorf("err = %v, want %v", err, tc.want)
				}
			case tc.wantText != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantText) {
					t.Errorf("err = %v, want %q", err, tc.wantText)
				}
			case err != nil:
				t.Errorf("err = %v", err)
			}

			if len(a.patches) != tc.patches {
				t.Errorf("patches = %q, want %d", a.patches, tc.patches)
			}
		})
	}
}

func TestKubeNodeMetaDemo(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	out, err := KubeNodeMeta(yaml, "", "", "demo-cp-1")
	if err != nil {
		t.Fatal(err)
	}

	var m nodeMeta
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}

	if len(m.Labels) != 3 || len(m.Taints) != 2 || m.Taints[0].Key != "maintenance" || m.Taints[0].TimeAdded == 0 || !m.Taints[1].Protected {
		t.Errorf("demo meta = %+v", m)
	}

	out, err = KubeNodeMetaPlan(yaml, "", "", "demo-cp-1", `{"taints":[{"key":"node-role.kubernetes.io/control-plane","effect":"NoSchedule"}],"resourceVersion":"`+m.ResourceVersion+`"}`)
	if err != nil {
		t.Fatal(err)
	}

	var plan nodeMetaPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if len(plan.Taints.Removed) != 1 || plan.Taints.Removed[0].Key != "maintenance" || len(plan.Blockers) != 0 {
		t.Errorf("demo plan = %+v", plan)
	}

	out, err = KubeNodeMetaPlan(yaml, "", "", "demo-cp-1", `{"taints":[{"key":"node-role.kubernetes.io/control-plane","effect":"NoSchedule"},{"key":"maintenance","value":"true","effect":"NoSchedule"},{"key":"x","effect":"NoExecute"}]}`)
	if err != nil {
		t.Fatal(err)
	}

	plan = nodeMetaPlan{}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatal(err)
	}

	if len(plan.Evicts) == 0 {
		t.Errorf("the demo NoExecute taint evicts nothing: %+v", plan)
	}

	if err := KubeNodeMetaApply(yaml, "", "", "demo-cp-1", `{"taints":[]}`, false); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo apply: %v", err)
	}
}

func TestNodeMetaTalosDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	out, err := NodeMeta(yaml, "", "", "192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"node":"demo-cp-1"`) {
		t.Errorf("talos demo meta = %s", out)
	}

	if _, err := NodeMetaPlan(yaml, "", "", "192.0.2.10", `{"removeLabels":["example.com/pool"]}`); err != nil {
		t.Errorf("talos demo plan: %v", err)
	}

	if err := NodeMetaApply(yaml, "", "", "192.0.2.10", `{"taints":[]}`, false); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("talos demo apply: %v", err)
	}
}
