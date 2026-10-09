package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func decodeSummary(t *testing.T, read func() (string, error)) kubeObjectSummary {
	t.Helper()

	out, err := read()
	if err != nil {
		t.Fatal(err)
	}

	var s kubeObjectSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("%v in %s", err, out)
	}

	return s
}

const summaryPod = `{"kind":"Pod","apiVersion":"v1",
  "metadata":{"name":"web-7d9c5-abcde","namespace":"shop","creationTimestamp":"2026-01-01T00:00:00Z",
    "labels":{"app":"web","argocd.argoproj.io/instance":"web"},
    "annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{...}","note":"` + "%s" + `"},
    "finalizers":["example.com/keep"],
    "ownerReferences":[{"apiVersion":"v1","kind":"Node","name":"n1"},
                       {"apiVersion":"apps/v1","kind":"ReplicaSet","name":"web-7d9c5","controller":true}]},
  "spec":{"nodeName":"n1","containers":[{"name":"a","image":"web:1"},{"name":"b","image":"web:1"},{"name":"c","image":"sidecar:2"}]},
  "status":{"phase":"Running","conditions":[
    {"type":"PodScheduled","status":"True","lastTransitionTime":"2026-01-01T00:00:00Z"},
    {"type":"Ready","status":"False","reason":"ContainersNotReady","message":"containers with unready status: [a]","lastTransitionTime":"2026-01-02T00:00:00Z"}]}}`

func TestKubeObjectSummaryPod(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods/web-7d9c5-abcde": strings.Replace(summaryPod, "%s", strings.Repeat("x", 300), 1),
		"GET /api/v1/namespaces/shop/events": `{"items":[{"metadata":{"creationTimestamp":"2026-01-02T00:00:00Z"},
		  "involvedObject":{"kind":"Pod","namespace":"shop","name":"web-7d9c5-abcde"},"type":"Warning","reason":"BackOff","message":"restarting","count":3}]}`,
		"GET /apis/apps/v1": `{"groupVersion":"apps/v1","resources":[{"name":"replicasets","kind":"ReplicaSet","namespaced":true,"verbs":["get","list","update"]},
		  {"name":"replicasets/scale","kind":"Scale","namespaced":true,"verbs":["get","patch"]}]}`,
		"GET /api/v1":           `{"groupVersion":"v1","resources":[{"name":"nodes","kind":"Node","namespaced":false,"verbs":["get","list"]}]}`,
		"GET /apis/argoproj.io": `{"name":"argoproj.io","preferredVersion":{"groupVersion":"argoproj.io/v1alpha1","version":"v1alpha1"}}`,
		"GET /apis/argoproj.io/v1alpha1": `{"groupVersion":"argoproj.io/v1alpha1","resources":[
		  {"name":"applications","kind":"Application","namespaced":true,"verbs":["get","list"]}]}`,
		"GET /apis/argoproj.io/v1alpha1/applications": `{"items":[{"metadata":{"name":"web","namespace":"argocd"}}]}`,
	})

	s := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(kubeStoreFor(t, f), "admin@test", "", "", "v1", "pods", "shop", "web-7d9c5-abcde")
	})

	if s.Kind != "Pod" || s.Phase != "Running" || s.Health != toneBad || s.HealthReason != "Ready: ContainersNotReady" {
		t.Errorf("health %+v", s)
	}

	if len(s.Conditions) != 2 || s.Conditions[0].Type != "Ready" || s.Conditions[0].Tone != toneBad || s.Conditions[0].LastTransition == 0 {
		t.Errorf("conditions %+v", s.Conditions)
	}

	if len(s.Owners) != 3 {
		t.Fatalf("owners %+v", s.Owners)
	}

	rs, node, app := s.Owners[0], s.Owners[1], s.Owners[2]
	if !rs.Controller || rs.Resource != "replicasets" || rs.Group != "apps" || rs.Namespace != "shop" || !slices.Contains(rs.Verbs, "update") || !rs.Scalable {
		t.Errorf("replica set %+v", rs)
	}

	if node.Resource != "nodes" || node.Namespace != "" || node.Namespaced {
		t.Errorf("node %+v", node)
	}

	if app.Via != ownerViaArgo || app.Resource != "applications" || app.Namespace != "argocd" || app.Name != "web" {
		t.Errorf("application %+v", app)
	}

	if _, ok := s.Annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Error("last-applied-configuration kept")
	}

	if note := s.Annotations["note"]; len(note) > annotationValueLimit+len("…") || !strings.HasSuffix(note, "…") {
		t.Errorf("note not cut: %d", len(note))
	}

	if len(s.Finalizers) != 1 || s.Created == 0 || s.Deleting != 0 || s.Labels["app"] != "web" {
		t.Errorf("metadata %+v", s)
	}

	want := []kubeHighlight{{highlightNode, "n1"}, {highlightImage, "web:1"}, {highlightImage, "sidecar:2"}}
	if !slices.Equal(s.Highlights, want) {
		t.Errorf("highlights %+v", s.Highlights)
	}

	if len(s.Events) != 1 || s.Events[0].Reason != "BackOff" || s.EventsError != "" {
		t.Errorf("events %+v %q", s.Events, s.EventsError)
	}

	for _, r := range f.recorded() {
		if r.path == "/api/v1/namespaces/shop/events" && !strings.Contains(r.query, "involvedObject.kind%3DPod") {
			t.Errorf("events query %q", r.query)
		}
	}
}

// A cluster-scoped object's events are read in "default"; events the role may not list
// leave the rest of the summary; a kind without conditions reads its phase.
func TestKubeObjectSummaryClusterScopedWithoutEvents(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/team": `{"kind":"Namespace","apiVersion":"v1","metadata":{"name":"team",
		  "deletionTimestamp":"2026-02-01T00:00:00Z"},"status":{"phase":"Terminating"}}`,
	})
	f.answerWith("GET /api/v1/namespaces/default/events", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"events is forbidden"}`)

	s := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(kubeStoreFor(t, f), "admin@test", "", "", "v1", "namespaces", "", "team")
	})

	if s.Health != toneWarn || s.HealthReason != "Terminating" || s.Deleting == 0 || len(s.Conditions) != 0 {
		t.Errorf("summary %+v", s)
	}

	if s.EventsError == "" || len(s.Events) != 0 {
		t.Errorf("events %+v %q", s.Events, s.EventsError)
	}
}

// Flux labels name the HelmRelease and Kustomization; the Helm release it rendered is then
// not listed apart. Without the Flux kinds served they stay untappable.
func TestKubeObjectSummaryFluxManagers(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/namespaces/web/deployments/site": `{"kind":"Deployment","apiVersion":"apps/v1",
		  "metadata":{"name":"site","namespace":"web",
		    "labels":{"helm.toolkit.fluxcd.io/name":"site","helm.toolkit.fluxcd.io/namespace":"flux-system",
		              "kustomize.toolkit.fluxcd.io/name":"apps","kustomize.toolkit.fluxcd.io/namespace":"flux-system"},
		    "annotations":{"meta.helm.sh/release-name":"site","meta.helm.sh/release-namespace":"web"}},
		  "spec":{"replicas":3,"selector":{"matchLabels":{"app":"site"},"matchExpressions":[{"key":"tier","operator":"In","values":["a","b"]}]},
		          "template":{"spec":{"containers":[{"image":"site:2"}]}}},
		  "status":{"replicas":3,"readyReplicas":2,"conditions":[{"type":"Available","status":"True"},{"type":"ReplicaFailure","status":"True","reason":"FailedCreate"}]}}`,
		"GET /apis/helm.toolkit.fluxcd.io": `{"preferredVersion":{"groupVersion":"helm.toolkit.fluxcd.io/v2"}}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2": `{"groupVersion":"helm.toolkit.fluxcd.io/v2","resources":[
		  {"name":"helmreleases","kind":"HelmRelease","namespaced":true,"verbs":["get","list"]}]}`,
	})

	s := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(kubeStoreFor(t, f), "admin@test", "", "apps", "v1", "deployments", "web", "site")
	})

	if len(s.Owners) != 2 {
		t.Fatalf("owners %+v", s.Owners)
	}

	if k := s.Owners[0]; k.Kind != "Kustomization" || k.Resource != "" || k.Via != ownerViaFlux {
		t.Errorf("kustomization %+v", k)
	}

	if h := s.Owners[1]; h.Kind != "HelmRelease" || h.Resource != "helmreleases" || h.Version != "v2" || h.Namespace != "flux-system" {
		t.Errorf("helm release %+v", h)
	}

	if s.Health != toneBad || s.Conditions[0].Type != "ReplicaFailure" {
		t.Errorf("health %s %+v", s.Health, s.Conditions)
	}

	want := []kubeHighlight{{highlightReplicas, "2/3"}, {highlightSelector, "app=site,tier in (a,b)"}, {highlightImage, "site:2"}}
	if !slices.Equal(s.Highlights, want) {
		t.Errorf("highlights %+v", s.Highlights)
	}
}

func TestKubeObjectSummaryHelmRelease(t *testing.T) {
	s := kubeObjectSummary{Annotations: map[string]string{"meta.helm.sh/release-name": "db", "meta.helm.sh/release-namespace": "data"}}

	got := managers(s)
	if len(got) != 1 || got[0].Via != ownerViaHelm || got[0].Name != "db" || got[0].Namespace != "data" {
		t.Errorf("managers %+v", got)
	}

	s = kubeObjectSummary{Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "team_web:apps/Deployment:shop/web"}}
	if got := managers(s); len(got) != 1 || got[0].Namespace != "team" || got[0].Name != "web" {
		t.Errorf("tracking id %+v", got)
	}
}

func TestConditionTone(t *testing.T) {
	cases := []struct {
		typ, status, reason, want string
	}{
		{"Ready", "True", "", toneOK},
		{"Ready", "False", "", toneBad},
		{"Ready", "False", "PodCompleted", toneOK},
		{"Ready", "Unknown", "", toneWarn},
		{"MemoryPressure", "False", "", toneOK},
		{"MemoryPressure", "True", "", toneBad},
		{"Stalled", "True", "", toneBad},
		{"Reconciling", "True", "", toneWarn},
		{"Reconciling", "False", "", toneOK},
		{"Issuing", "True", "", toneWarn},
		{"Ready", "", "", toneNone},
	}

	for _, c := range cases {
		if got := conditionTone(summaryCondition{Type: c.typ, Status: c.status, Reason: c.reason}); got != c.want {
			t.Errorf("%s=%s (%s): %s, want %s", c.typ, c.status, c.reason, got, c.want)
		}
	}
}

func TestObjectHealthWithoutConditions(t *testing.T) {
	for phase, want := range map[string]string{"Bound": toneOK, "Pending": toneWarn, "Failed": toneBad, "": toneNone, "Whatever": toneNone} {
		if got, _ := objectHealth(nil, phase, false); got != want {
			t.Errorf("%q: %s, want %s", phase, got, want)
		}
	}

	if got, reason := objectHealth(nil, "", true); got != toneWarn || reason != "Terminating" {
		t.Errorf("deleting: %s %s", got, reason)
	}
}

func TestSpecHighlightsShapes(t *testing.T) {
	cronJob := map[string]any{"spec": map[string]any{
		"schedule": "*/5 * * * *", "suspend": true,
		"jobTemplate": map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []any{map[string]any{"image": "job:1"}},
		}}}},
	}}

	want := []kubeHighlight{{highlightSchedule, "*/5 * * * *"}, {highlightSuspend, "true"}, {highlightImage, "job:1"}}
	if got := specHighlights(cronJob); !slices.Equal(got, want) {
		t.Errorf("cron job %+v", got)
	}

	service := map[string]any{"spec": map[string]any{"selector": map[string]any{"b": "2", "a": "1"}}}
	if got := specHighlights(service); len(got) != 1 || got[0].Value != "a=1,b=2" {
		t.Errorf("service %+v", got)
	}

	exists := map[string]any{"matchExpressions": []any{
		map[string]any{"key": "gpu", "operator": "Exists"}, map[string]any{"key": "spot", "operator": "DoesNotExist"},
	}}
	if got := selectorText(exists); got != "gpu,!spot" {
		t.Errorf("expressions %q", got)
	}

	if got := specHighlights(map[string]any{"spec": map[string]any{"replicas": float64(2)}}); got[0].Value != "2" {
		t.Errorf("replicas without status %+v", got)
	}
}

func TestKubeObjectSummaryValidatesAndDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	for _, bad := range [][5]string{
		{"", "v1", "pods", "demo", "a/b"},
		{"", "v1", "pods", "bad ns", "a"},
		{"", "1", "pods", "demo", "a"},
		{"Bad", "v1", "pods", "demo", "a"},
	} {
		if _, err := KubeObjectSummary(yaml, "", "", bad[0], bad[1], bad[2], bad[3], bad[4]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}

	pod := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(yaml, "", "", "", "v1", "pods", "demo", "worker-6f4b8-uvwxy")
	})
	if pod.Health != toneBad || pod.Owners[0].Resource != "replicasets" || pod.Owners[0].Name != "worker-6f4b8" || len(pod.Events) == 0 {
		t.Errorf("pod %+v", pod)
	}

	rs := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(yaml, "", "", "apps", "v1", "replicasets", "demo", "worker-6f4b8")
	})
	if rs.Owners[0].Kind != "Deployment" || rs.Owners[0].Name != "worker" {
		t.Errorf("replica set %+v", rs.Owners)
	}

	deploy := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(yaml, "", "", "apps", "v1", "deployments", "demo", "hello-ichor")
	})
	if deploy.Health != toneOK || deploy.Owners[0].Kind != "HelmRelease" || deploy.Owners[0].Resource == "" {
		t.Errorf("deployment %+v", deploy)
	}

	cm := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(yaml, "", "", "", "v1", "configmaps", "demo", "settings")
	})
	if cm.Kind != "ConfigMap" || cm.Health != toneNone || len(cm.Owners) != 0 {
		t.Errorf("config map %+v", cm)
	}
}

// An Application whose instance label names itself (app of apps) is not its own owner, and
// no lookup is made for it.
func TestKubeObjectSummaryArgoApplicationNotItsOwnOwner(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/argoproj.io/v1alpha1/namespaces/argocd/applications/root": `{"kind":"Application","apiVersion":"argoproj.io/v1alpha1",
		  "metadata":{"name":"root","namespace":"argocd","labels":{"argocd.argoproj.io/instance":"root"}}}`,
	})

	s := decodeSummary(t, func() (string, error) {
		return KubeObjectSummary(kubeStoreFor(t, f), "admin@test", "", "argoproj.io", "v1alpha1", "applications", "argocd", "root")
	})

	if len(s.Owners) != 0 {
		t.Errorf("owners %+v", s.Owners)
	}

	for _, r := range f.recorded() {
		if r.path == "/apis/argoproj.io/v1alpha1/applications" {
			t.Error("looked the Application up")
		}
	}
}

func TestTimeMilli(t *testing.T) {
	if got := timeMilli("2026-01-01T00:00:00Z"); got != time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("got %d", got)
	}

	if timeMilli("") != 0 || timeMilli("yesterday") != 0 {
		t.Error("bad timestamps read")
	}
}
