package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Dragonfly plugin's Fail over now action (failOver: delete the master pod, as the
// operator documents it), the syncing replica and the single-pod finding, over raw pods that
// carry a uid and the replication readiness gate's condition.

const dfPodsPath = "/api/v1/namespaces/app/pods"

// dfInstances: ha has a ready replica, lone has one pod, gated's replica still syncs behind
// the readiness gate, tired's replica is down.
const dfInstances = `{"items":[
  {"metadata":{"name":"ha","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}},
  {"metadata":{"name":"lone","namespace":"app"},"spec":{"replicas":1},"status":{"phase":"Ready"}},
  {"metadata":{"name":"gated","namespace":"app"},"spec":{"replicas":2,"enableReplicationReadinessGate":true},"status":{"phase":"Ready"}},
  {"metadata":{"name":"tired","namespace":"app"},"spec":{"replicas":2},"status":{"phase":"Ready"}}]}`

// dfRawPod is a running pod of instance as the API server returns it; syncing adds the
// readiness gate's condition, not true yet.
func dfRawPod(instance, name, role string, ready, syncing bool) map[string]any {
	status := map[string]any{
		"phase":             "Running",
		"containerStatuses": []any{map[string]any{"name": "dragonfly", "ready": ready}},
	}
	if syncing {
		status["conditions"] = []any{map[string]any{"type": "dragonflydb.io/replication-ready", "status": "False"}}
	}

	return map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name": name, "namespace": "app", "uid": "uid-" + name, "resourceVersion": "41",
			"labels": map[string]any{"app.kubernetes.io/name": "dragonfly", "app": instance, "role": role},
		},
		"spec":   map[string]any{"nodeName": "node-1"},
		"status": status,
	}
}

func dfRawPods() []map[string]any {
	return []map[string]any{
		dfRawPod("ha", "ha-0", "master", true, false),
		dfRawPod("ha", "ha-1", "replica", true, false),
		dfRawPod("lone", "lone-0", "master", true, false),
		dfRawPod("gated", "gated-0", "master", true, false),
		dfRawPod("gated", "gated-1", "replica", true, true),
		dfRawPod("tired", "tired-0", "master", true, false),
		dfRawPod("tired", "tired-1", "replica", false, false),
	}
}

// dfActionAPI serves the instances and the pods (listed by label selector, or read one by
// one), and accepts the delete of any pod.
func dfActionAPI(t *testing.T, pods []map[string]any) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                                    dragonflyGroups,
		"GET /apis/dragonflydb.io/v1alpha1":                            `{"resources":[{"name":"dragonflies","namespaced":true,"kind":"Dragonfly"}]}`,
		"GET /apis/dragonflydb.io/v1alpha1/dragonflies":                dfInstances,
		"GET /apis/dragonflydb.io/v1alpha1/namespaces/app/dragonflies": dfInstances,
	})
	f.answerQuery = rawPodsBySelector(t, pods)
	f.answerFunc = func(method, path, _ string) (string, bool) {
		return `{"kind":"Pod"}`, method == http.MethodDelete && strings.HasPrefix(path, dfPodsPath+"/")
	}

	useFakeKube(t, f)

	return f
}

// rawPodsBySelector answers a pod list with the pods its label selector matches, and a pod
// read with that pod.
func rawPodsBySelector(t *testing.T, pods []map[string]any) func(path, query string) (string, bool) {
	return func(path, query string) (string, bool) {
		if name, ok := strings.CutPrefix(path, dfPodsPath+"/"); ok {
			i := slices.IndexFunc(pods, func(p map[string]any) bool { return mapAt(p, "metadata")["name"] == name })
			if i < 0 {
				return `{"kind":"Status","status":"Failure","code":404,"reason":"NotFound"}`, true
			}

			out, _ := json.Marshal(pods[i])

			return string(out), true
		}

		if path != "/api/v1/pods" && path != dfPodsPath {
			return "", false
		}

		q, _ := url.ParseQuery(query)
		matched := slices.DeleteFunc(slices.Clone(pods), func(p map[string]any) bool {
			labels := map[string]string{}
			for k, v := range mapAt(mapAt(p, "metadata"), "labels") {
				labels[k], _ = v.(string)
			}

			return !labelsMatch(labels, q.Get("labelSelector"))
		})

		out, err := json.Marshal(map[string]any{"metadata": map[string]any{}, "items": matched})
		if err != nil {
			t.Error(err)
		}

		return string(out), true
	}
}

// failOverButton is the pod row's Fail over now button, by its label.
func failOverButton(t *testing.T, tree plugin.Tree, pod, label string) map[string]any {
	t.Helper()

	for _, row := range rookCard(t, tree, "pods").Rows {
		if row.Title == pod {
			return forceSyncButton(t, row, label)
		}
	}

	t.Fatalf("no pod row %s", pod)

	return nil
}

// Only a master with a replica ready to take over has the button on; a replica, a lone master,
// a master whose replica still syncs and one whose replica is down have it off, with the reason.
func TestDragonflyFailOverInTheTree(t *testing.T) {
	dragonflyBuiltin(t)
	dfActionAPI(t, dfRawPods())

	tree := pluginData(t, "cfg", "ctx", "", "dragonfly", "", "")
	for _, pod := range []string{"ha-0", "ha-1", "lone-0", "gated-0", "gated-1", "tired-0", "tired-1"} {
		b := failOverButton(t, tree, pod, "Fail over now")

		wantEnabled := pod == "ha-0"
		if b["id"] != "failOver" || b["enabled"] != wantEnabled || b["confirm"] != "typedName" {
			t.Errorf("%s: %+v, want enabled %v", pod, b, wantEnabled)
		}

		if reason, _ := b["disabledReason"].(string); wantEnabled != (reason == "") || (!wantEnabled && reason != "needs a master with a ready replica") {
			t.Errorf("%s: disabled reason %q", pod, reason)
		}
	}

	fr := pluginData(t, "cfg", "ctx", "", "dragonfly", "", "fr")
	if b := failOverButton(t, fr, "lone-0", "Basculer maintenant"); b["disabledReason"] != "il faut un maître avec une réplique prête" {
		t.Errorf("French button %+v", b)
	}
}

// A replica behind the readiness gate reads info "Syncing from the master", not critical,
// and is not a ready replica; a healthy one-pod instance is an info finding of the checkup.
func TestDragonflySyncingReplicaAndSinglePod(t *testing.T) {
	dragonflyBuiltin(t)
	dfActionAPI(t, dfRawPods())

	tree := pluginData(t, "cfg", "ctx", "", "dragonfly", "", "")

	got := map[string]string{}
	for _, row := range rookCard(t, tree, "pods").Rows {
		ready, _ := rowItems(row, "keyValues")["Replicas ready"]["value"].(string)
		got[row.Title] = row.Status.Text + " " + row.Health.Tone + " " + row.Health.Reason + " " + row.Health.Message + " " + ready
	}

	want := map[string]string{
		"ha-0":    "master ok   1",
		"ha-1":    "replica ok   1",
		"lone-0":  "master ok   0",
		"gated-0": "master ok   0",
		"gated-1": "replica info syncing Syncing from the master 0",
		"tired-0": "master ok   0",
		"tired-1": "replica critical notReady Not ready 0",
	}
	for pod, w := range want {
		if got[pod] != w {
			t.Errorf("%s: %q, want %q", pod, got[pod], w)
		}
	}

	var findings []string
	for _, f := range tree.Findings {
		findings = append(findings, f.Severity+" "+f.ID+" "+f.Title+" "+f.Detail)
	}

	slices.Sort(findings)

	if want := []string{"info instance-single app/lone one pod, no replica to fail over to", "warning instance-warning app/tired replicas missing"}; !slices.Equal(findings, want) {
		t.Errorf("findings %v", findings)
	}
}

// Fail over now reads the master pod and its instance's pods, then deletes the pod in the
// background, pinned to the uid it checked, and records it in the audit log.
func TestDragonflyFailOverDeletesTheMaster(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	dragonflyBuiltin(t)

	f := dfActionAPI(t, dfRawPods())

	if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "dragonfly", "failOver", "app", "ha-0", ""); err != nil {
		t.Fatal(err)
	}

	got := mutation(t, f)
	if got.method != http.MethodDelete || got.path != dfPodsPath+"/ha-0" {
		t.Fatalf("got %s %s", got.method, got.path)
	}

	var opts map[string]any
	if err := json.Unmarshal([]byte(got.body), &opts); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Background",
		"preconditions": map[string]any{"uid": "uid-ha-0"},
	}
	if !equalJSON(t, opts, want) {
		t.Errorf("delete options %s", got.body)
	}

	entries := readAudit(t, "", "plugin:dragonfly:failOver")
	if len(entries) != 1 || entries[0].Outcome != auditOK || entries[0].Namespace != "app" || entries[0].Object != "Pod/ha-0" {
		t.Errorf("audit %+v", entries)
	}
}

// A master whose only replica is down, as read now, is refused with the reason: nothing is
// deleted.
func TestDragonflyFailOverWithoutAReadyReplicaRefused(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	dragonflyBuiltin(t)

	f := dfActionAPI(t, dfRawPods())

	err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "dragonfly", "failOver", "app", "tired-0", "")

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusConflict || apiErr.Message != "needs a master with a ready replica" {
		t.Fatalf("got %v", err)
	}

	if slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method != http.MethodGet }) {
		t.Errorf("sent anyway: %+v", f.recorded())
	}

	if entries := readAudit(t, "", "plugin:dragonfly:failOver"); len(entries) != 1 || entries[0].Outcome != auditFailed {
		t.Errorf("audit %+v", entries)
	}
}

// The demo shows the button on its masters with a ready replica but never runs it; the can-I
// row is the delete of the pod and the get that reads it first.
func TestDragonflyFailOverDemoAndAccess(t *testing.T) {
	dragonflyBuiltin(t)
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)
	tree := pluginData(t, demo, demoKubeContext, "", "dragonfly", "", "")

	if b := failOverButton(t, tree, "queue-cache-1", "Fail over now"); b["enabled"] != true {
		t.Errorf("demo master: %+v", b)
	}

	if b := failOverButton(t, tree, "cart-cache-0", "Fail over now"); b["enabled"] != false {
		t.Errorf("demo master with a syncing replica: %+v", b)
	}

	if err := KubePluginAction(demo, demoKubeContext, "", "dragonfly", "failOver", "demo-shop", "queue-cache-1", ""); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	want := []accessNeed{
		{ssarAttributes{Verb: "delete", Group: "", Resource: "pods"}, true},
		{ssarAttributes{Verb: "get", Group: "", Resource: "pods"}, true},
	}
	if got := kubeActionChecks["plugin:dragonfly:failOver"]; !slices.Equal(got, want) {
		t.Errorf("can-I row %+v, want %+v", got, want)
	}
}
