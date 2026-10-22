// Named dragonflydb, not dragonfly: a _dragonfly suffix is a GOOS build constraint.

package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Dragonfly built-in plugin against what the Data services reader it replaced said about
// the same objects (its tests' fixtures and expectations): the same health and first reason
// per instance, the same ready pods and master, the master first among its pods, and findings
// where the monitor alerted.

// Shaped like the Dragonfly operator's objects on a real cluster: one CR per instance, its pods
// labelled app.kubernetes.io/name=dragonfly, app=<instance> and role=master|replica.
const dragonflyFixture = `{"items":[
  {"metadata":{"name":"cache","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"queue","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"down","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"orphan","namespace":"app"},"spec":{"replicas":1},"status":{"phase":"Configuring-replication"}},
  {"metadata":{"name":"rolling","namespace":"app"},"spec":{"replicas":1},"status":{"phase":"Rolling-update"}}]}`

func dragonflyPods() []dsPod {
	pod := func(instance, name, node, role string, ready bool) dsPod {
		labels := map[string]string{"app.kubernetes.io/name": "dragonfly", "app": instance, "role": role}
		p := fakePod("app", name, node, ready, labels, "dragonfly", "docker.dragonflydb.io/dragonflydb/dragonfly:v2.0.0")
		if !ready {
			p.Status.Phase, p.Spec.NodeName = "Pending", ""
		}

		return p
	}

	return []dsPod{
		pod("cache", "cache-1", "node-2", "replica", true),
		pod("cache", "cache-0", "node-1", "master", true),
		pod("queue", "queue-0", "node-1", "master", true),
		pod("queue", "queue-1", "", "replica", false),
		pod("down", "down-0", "", "master", false),
		pod("down", "down-1", "", "replica", false),
		pod("orphan", "orphan-0", "node-3", "replica", true),
		pod("rolling", "rolling-0", "node-3", "master", true),
	}
}

const dragonflyGroups = `{"groups":[{"name":"dragonflydb.io","preferredVersion":{"version":"v1alpha1"}}]}`

// dragonflyPluginAPI serves the Dragonfly operator with instances and pods; the pods are
// filtered by the list's label selector, as the API server does.
func dragonflyPluginAPI(t *testing.T, instances string, pods []dsPod) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                     dragonflyGroups,
		"GET /apis/dragonflydb.io/v1alpha1":             `{"resources":[{"name":"dragonflies","namespaced":true,"kind":"Dragonfly"}]}`,
		"GET /apis/dragonflydb.io/v1alpha1/dragonflies": instances,
	})
	f.answerQuery = podsBySelector(t, pods)

	useFakeKube(t, f)

	return f
}

// podsBySelector answers a pod list (of every namespace or of one) with the pods its label
// selector matches.
func podsBySelector(t *testing.T, pods []dsPod) func(path, query string) (string, bool) {
	return func(path, query string) (string, bool) {
		namespace := ""
		if path != "/api/v1/pods" {
			ns, prefixed := strings.CutPrefix(path, "/api/v1/namespaces/")
			ns, suffixed := strings.CutSuffix(ns, "/pods")

			if !prefixed || !suffixed {
				return "", false
			}

			namespace = ns
		}

		q, _ := url.ParseQuery(query)
		matched := slices.DeleteFunc(slices.Clone(pods), func(p dsPod) bool {
			return (namespace != "" && p.Metadata.Namespace != namespace) || !labelsMatch(p.Metadata.Labels, q.Get("labelSelector"))
		})

		out, err := json.Marshal(map[string]any{"metadata": map[string]any{}, "items": matched})
		if err != nil {
			t.Error(err)
		}

		return string(out), true
	}
}

// podSelectors are the label selectors pods were listed with, sorted.
func podSelectors(f *fakeKubeAPI) []string {
	var out []string

	for _, r := range f.recorded() {
		if r.path == "/api/v1/pods" {
			q, _ := url.ParseQuery(r.query)
			out = append(out, q.Get("labelSelector"))
		}
	}

	slices.Sort(out)

	return out
}

func dragonflyBuiltin(t *testing.T) *plugin.Compiled {
	t.Helper()

	return useBuiltinPlugin(t, "dragonfly", fixtureNow)
}

// The instances, pods and findings of the reader's fixtures.
func TestDragonflyPluginTreeMatchesTheReader(t *testing.T) {
	dragonflyBuiltin(t)
	f := dragonflyPluginAPI(t, dragonflyFixture, dragonflyPods())

	tree := pluginData(t, "cfg", "ctx", "", "dragonfly", "", "")
	if !tree.Active || tree.DetectedVia != "api" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	type instance struct {
		Name, Health, Reason, Master, Ready string
	}

	var got []instance

	for _, row := range rookCard(t, tree, "instances").Rows {
		master, _ := rowItems(row, "keyValues")["Master"]["value"].(string)
		got = append(got, instance{row.Title, row.Health.Tone, row.Health.Reason, master, row.Trailing.Text})
	}

	// The reader's order (health, then namespace/name), its first reason, master and ready pods.
	want := []instance{
		{"down", healthCritical, "noReady", "down-0", "0/2"},
		{"orphan", healthCritical, "noMaster", "", "1/1"},
		{"queue", healthWarning, "pods", "queue-0", "1/2"},
		{"rolling", healthWarning, "notReady", "rolling-0", "1/1"},
		{"cache", healthOK, "", "cache-0", "2/2"},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("instances differ")
	}

	if h := rookCard(t, tree, "instances").Rows[3].Health; h.Message != "operator: Rolling-update" {
		t.Errorf("rolling: %+v", h)
	}

	// The master first under its instance, with the role label as the operator set it; a pod
	// not ready is critical.
	var pods []string
	for _, row := range rookCard(t, tree, "pods").Rows {
		pods = append(pods, row.Title+":"+row.Status.Text+"@"+row.Trailing.Text+":"+row.Health.Tone)
	}

	if want := []string{
		"cache-0:master@node-1:ok", "cache-1:replica@node-2:ok", "down-0:master@:critical", "down-1:replica@:critical",
		"orphan-0:replica@node-3:ok", "queue-0:master@node-1:ok", "queue-1:replica@:critical", "rolling-0:master@node-3:ok",
	}; !slices.Equal(pods, want) {
		t.Errorf("pods %v", pods)
	}

	// The monitor alerted on the critical instances and the replica down: not on a rolling update.
	var findings []string
	for _, f := range tree.Findings {
		findings = append(findings, f.Severity+" "+f.ID+" "+f.Title)
	}

	slices.Sort(findings)

	if want := []string{"critical instance-critical app/down", "critical instance-critical app/orphan", "warning instance-warning app/queue"}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}

	if tree.Summary.Title != "5 Dragonfly instances" || tree.Summary.Health != healthCritical {
		t.Errorf("summary %+v", tree.Summary)
	}

	// Its pods come from a label-selected listing, not the full one.
	if sel := podSelectors(f); !slices.Equal(sel, []string{"app.kubernetes.io/name=dragonfly"}) {
		t.Errorf("pods listed with %v", sel)
	}
}

// dragonflyPod is a pod of instance i in namespace ns.
func dragonflyPod(ns, name, role string, ready bool) dsPod {
	labels := map[string]string{"app.kubernetes.io/name": "dragonfly", "app": "i", "role": role}

	return fakePod(ns, name, "node-1", ready, labels, "dragonfly", "")
}

// Each rule of the reader's health on its own, two masters included; a missing replicas
// field means one.
func TestDragonflyPluginHealth(t *testing.T) {
	tests := []struct {
		name, spec, phase string
		pods              []dsPod
		health, reason    string
	}{
		{"two-masters", `{}`, "Ready", []dsPod{dragonflyPod("two-masters", "a", "master", true), dragonflyPod("two-masters", "b", "master", true)}, healthWarning, "masters"},
		{"no-pod", `{"replicas":1}`, "Ready", nil, healthCritical, "noReady"},
		{"stopped", `{"replicas":0}`, "Ready", nil, healthOK, ""},
		{"replica-down", `{"replicas":3}`, "Ready", []dsPod{dragonflyPod("replica-down", "a", "master", true), dragonflyPod("replica-down", "b", "replica", false)}, healthWarning, "pods"},
		{"lower-case", `{}`, "ready", []dsPod{dragonflyPod("lower-case", "a", "master", true)}, healthOK, ""},
		{"no-phase", `{}`, "", []dsPod{dragonflyPod("no-phase", "a", "master", true)}, healthOK, ""},
	}

	var (
		items []string
		pods  []dsPod
	)

	for _, tt := range tests {
		items = append(items, fmt.Sprintf(`{"metadata":{"name":"i","namespace":%q},"spec":%s,"status":{"phase":%q}}`, tt.name, tt.spec, tt.phase))
		pods = append(pods, tt.pods...)
	}

	dragonflyBuiltin(t)
	dragonflyPluginAPI(t, `{"items":[`+strings.Join(items, ",")+`]}`, pods)

	got := map[string]string{}
	for _, row := range rookCard(t, pluginData(t, "cfg", "ctx", "", "dragonfly", "", ""), "instances").Rows {
		got[row.Subtitle] = row.Health.Tone + " " + row.Health.Reason
	}

	for _, tt := range tests {
		if want := tt.health + " " + tt.reason; got[tt.name] != want {
			t.Errorf("%s: %q, want %q", tt.name, got[tt.name], want)
		}
	}
}

// The French tree takes the apps' former strings.
func TestDragonflyPluginFrench(t *testing.T) {
	dragonflyBuiltin(t)
	dragonflyPluginAPI(t, dragonflyFixture, dragonflyPods())

	tree := pluginData(t, "cfg", "ctx", "", "dragonfly", "", "fr")
	if tree.Summary.Title != "5 instances Dragonfly" || rookCard(t, tree, "instances").Empty != "Aucune instance Dragonfly." {
		t.Fatalf("summary %+v", tree.Summary)
	}

	if h := rookCard(t, tree, "instances").Rows[1].Health; h.Message != "aucun maître" {
		t.Errorf("orphan in French: %+v", h)
	}
}

// The demo renders the plugin's samples: the former demo's session cache with a replica
// pending next to a healthy queue cache, and one instance per other state.
func TestDragonflyPluginDemo(t *testing.T) {
	dragonflyBuiltin(t)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "dragonfly", "", "")

	health := map[string]string{}
	for _, row := range rookCard(t, tree, "instances").Rows {
		health[row.Title] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{
		"session-cache": "warning pods", "queue-cache": "ok ", "rate-limit": "warning notReady",
		"feature-flags": "warning masters", "leaderboard": "critical noMaster", "scratch": "critical noReady",
		"cart-cache": "ok ", "job-locks": "ok ",
	}
	if fmt.Sprint(health) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", health, tree.Errors)
	}

	// The cart cache's replica still syncs behind the readiness gate; the job locks are one pod.
	for _, row := range rookCard(t, tree, "pods").Rows {
		if row.Title == "cart-cache-1" && (row.Health.Tone != "info" || row.Health.Reason != "syncing") {
			t.Errorf("cart-cache-1: %+v", row.Health)
		}
	}

	if !slices.ContainsFunc(tree.Findings, func(f plugin.TreeFind) bool {
		return f.ID == "instance-single" && f.Severity == "info" && f.Title == "demo-lab/job-locks"
	}) {
		t.Errorf("no single-pod finding for the job locks: %+v", tree.Findings)
	}
}

// Data services no longer carry Dragonfly: the plugin does.
func TestDataServicesHaveNoDragonfly(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "dragonfly") {
		t.Error("the Data services JSON still has a dragonfly key")
	}
}
