package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const (
	actionsDeployment = `{"metadata":{"name":"web","namespace":"shop","annotations":{"deployment.kubernetes.io/revision":"3"}},
 "spec":{"selector":{"matchLabels":{"app":"web"}}}}`
	actionsReplicaSets = `{"items":[
 {"metadata":{"name":"web-a","creationTimestamp":"2026-10-01T10:00:00Z","annotations":{"deployment.kubernetes.io/revision":"1"},
   "ownerReferences":[{"kind":"Deployment","name":"web","controller":true}]},
  "spec":{"template":{"metadata":{"labels":{"app":"web","pod-template-hash":"aaa"}},"spec":{"containers":[{"name":"web","image":"web:1"}]}}}},
 {"metadata":{"name":"web-c","creationTimestamp":"2026-10-03T10:00:00Z","annotations":{"deployment.kubernetes.io/revision":"3","kubernetes.io/change-cause":"v3"},
   "ownerReferences":[{"kind":"Deployment","name":"web","controller":true}]},
  "spec":{"template":{"metadata":{"labels":{"app":"web","pod-template-hash":"ccc"}},"spec":{"containers":[{"name":"web","image":"web:3"}]}}},
  "status":{"replicas":2}},
 {"metadata":{"name":"other-x","annotations":{"deployment.kubernetes.io/revision":"9"},
   "ownerReferences":[{"kind":"Deployment","name":"other","controller":true}]},"spec":{"template":{}}}
]}`
)

func actionsKube(t *testing.T, answers map[string]string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func lastActionRequest(f *fakeKubeAPI, method string) fakeKubeRequest {
	reqs := f.recorded()
	for i := len(reqs) - 1; i >= 0; i-- {
		if reqs[i].method == method {
			return reqs[i]
		}
	}

	return fakeKubeRequest{}
}

func TestScaleAndAutoscalerWarning(t *testing.T) {
	f, k := actionsKube(t, map[string]string{
		"PATCH /apis/apps/v1/namespaces/shop/deployments/web/scale": `{}`,
		"GET /apis/autoscaling/v2/namespaces/shop/horizontalpodautoscalers": `{"items":[
			{"metadata":{"name":"web-hpa"},"spec":{"scaleTargetRef":{"kind":"Deployment","name":"web"},"minReplicas":2,"maxReplicas":8}}]}`,
	})

	err := k.patch(context.Background(), appsPath(workloadKinds[0], "shop", "web")+"/scale", "application/merge-patch+json",
		map[string]any{"spec": map[string]any{"replicas": 5}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if req := lastActionRequest(f, http.MethodPatch); req.body != `{"spec":{"replicas":5}}` || req.contentType != "application/merge-patch+json" {
		t.Errorf("scale request = %+v", req)
	}

	if w := autoscalerWarning(context.Background(), k, "Deployment", "shop", "web"); !strings.Contains(w, "web-hpa") || !strings.Contains(w, "2 to 8") {
		t.Errorf("warning = %q", w)
	}

	if w := autoscalerWarning(context.Background(), k, "StatefulSet", "shop", "web"); w != "" {
		t.Errorf("other kind warned: %q", w)
	}
}

func TestScaleRefusals(t *testing.T) {
	cases := map[string]struct {
		kind     string
		replicas int
		want     string
	}{
		"daemonset": {"DaemonSet", 2, "cannot be scaled"},
		"negative":  {"Deployment", -1, "between 0 and"},
		"too many":  {"Deployment", maxScaleReplicas + 1, "between 0 and"},
		"unknown":   {"Job", 1, "unsupported workload kind"},
	}

	for name, c := range cases {
		if _, err := KubeScale("", "", "", c.kind, "shop", "web", c.replicas); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestDeploymentRevisions(t *testing.T) {
	_, k := actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/web": actionsDeployment,
		"GET /apis/apps/v1/namespaces/shop/replicasets":     actionsReplicaSets,
	})

	revs, byRevision, err := deploymentRevisions(context.Background(), k, "shop", "web")
	if err != nil {
		t.Fatal(err)
	}

	if len(revs) != 2 || revs[0].Revision != 3 || !revs[0].Current || revs[1].Revision != 1 || revs[1].Current {
		t.Fatalf("revisions = %+v", revs)
	}

	if revs[0].ChangeCause != "v3" || revs[0].Images[0] != "web:3" || revs[0].Replicas != 2 {
		t.Errorf("newest = %+v", revs[0])
	}

	if _, ok := byRevision[9]; ok {
		t.Error("another Deployment's ReplicaSet was kept")
	}
}

func TestRollbackReplacesTheTemplate(t *testing.T) {
	f, k := actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/web":   actionsDeployment,
		"GET /apis/apps/v1/namespaces/shop/replicasets":       actionsReplicaSets,
		"PATCH /apis/apps/v1/namespaces/shop/deployments/web": `{}`,
	})

	if err := rollbackDeployment(context.Background(), k, "shop", "web", 1); err != nil {
		t.Fatal(err)
	}

	req := lastActionRequest(f, http.MethodPatch)
	if req.contentType != "application/json-patch+json" {
		t.Errorf("content type %q", req.contentType)
	}

	var patch []struct {
		Op, Path string
		Value    json.RawMessage
	}

	if err := json.Unmarshal([]byte(req.body), &patch); err != nil || len(patch) != 2 {
		t.Fatalf("patch %q: %v", req.body, err)
	}

	var template struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			Containers []struct {
				Image string `json:"image"`
			} `json:"containers"`
		} `json:"spec"`
	}

	_ = json.Unmarshal(patch[0].Value, &template)

	// The annotations come from the revision (revision 1 has none of its own), keeping the
	// Deployment's controller-owned revision annotation.
	if patch[1].Path != "/metadata/annotations" || string(patch[1].Value) != `{"deployment.kubernetes.io/revision":"3"}` {
		t.Errorf("annotations op = %s %s", patch[1].Path, patch[1].Value)
	}

	if patch[0].Op != "replace" || patch[0].Path != "/spec/template" || len(template.Spec.Containers) != 1 || template.Spec.Containers[0].Image != "web:1" {
		t.Errorf("template op = %s %s %+v", patch[0].Op, patch[0].Path, template)
	}

	if _, ok := template.Metadata.Labels[podTemplateHashLabel]; ok || template.Metadata.Labels["app"] != "web" {
		t.Errorf("labels = %v", template.Metadata.Labels)
	}

	for rev, want := range map[int]string{3: "already the current one", 2: "not kept anymore"} {
		if err := rollbackDeployment(context.Background(), k, "shop", "web", rev); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("revision %d: err = %v", rev, err)
		}
	}
}

func TestRollbackAnnotationsLikeKubectl(t *testing.T) {
	got := rollbackAnnotations(
		map[string]string{deploymentRevisionKey: "7", changeCauseAnnotation: "v7", "team": "web"},
		map[string]string{deploymentRevisionKey: "5", changeCauseAnnotation: "v5", "owner": "ops"},
	)

	want := map[string]string{deploymentRevisionKey: "7", changeCauseAnnotation: "v5", "owner": "ops"}
	if len(got) != len(want) {
		t.Fatalf("annotations = %v", got)
	}

	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestNewestBytesKeepsTheEnd(t *testing.T) {
	log := "old line\nmiddle line\nnewest crash\n"

	if got := newestBytes(log, 100); got != log {
		t.Errorf("short log changed: %q", got)
	}

	if got := newestBytes(log, 20); got != "newest crash\n" {
		t.Errorf("trimmed = %q", got)
	}
}

func TestRollbackRefusesAPausedDeployment(t *testing.T) {
	_, k := actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/web": `{"spec":{"paused":true}}`,
	})

	if err := rollbackDeployment(context.Background(), k, "shop", "web", 1); err != errPausedDeployment {
		t.Errorf("err = %v", err)
	}
}

func TestPodLogsQuery(t *testing.T) {
	var (
		mu    sync.Mutex
		query string
	)

	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"gitVersion":"v1.34.0"}`))

			return
		}

		mu.Lock()
		query = r.URL.Path + "?" + r.URL.RawQuery
		mu.Unlock()

		_, _ = w.Write([]byte("line 1\nline 2\n"))
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := k.getText(context.Background(), podPath("shop", "web-1")+"/log?container=app&limitBytes=2097152&previous=true&tailLines=200")
	if err != nil || out != "line 1\nline 2\n" {
		t.Fatalf("out %q, err %v", out, err)
	}

	mu.Lock()
	defer mu.Unlock()

	if !strings.Contains(query, "/api/v1/namespaces/shop/pods/web-1/log?") || !strings.Contains(query, "previous=true") {
		t.Errorf("query = %q", query)
	}
}

func TestPodLogsValidationAndDemo(t *testing.T) {
	if _, err := KubePodLogs("", "", "", "shop", "web-1", "BAD NAME", false, 10); err == nil {
		t.Error("invalid container accepted")
	}

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubePodLogs(demo, "", "", "shop", "web-1", "", true, 0)
	if err != nil || !strings.Contains(out, "panic:") {
		t.Errorf("demo previous log = %q, %v", out, err)
	}

	revs, err := KubeDeploymentRevisions(demo, "", "", "shop", "frontend")
	if err != nil || !strings.Contains(revs, `"current":true`) {
		t.Errorf("demo revisions = %q, %v", revs, err)
	}

	if err := KubeSuspendCronJob(demo, "", "", "shop", "backup", true); err != demoUnavailable {
		t.Errorf("demo suspend: %v", err)
	}
}
