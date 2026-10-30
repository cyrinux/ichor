package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	revisionsStatefulSet = `{"metadata":{"name":"db","namespace":"demo-shop"},
 "spec":{"selector":{"matchLabels":{"app":"db"}},"updateStrategy":{"type":"RollingUpdate"},
  "template":{"metadata":{"labels":{"app":"db"}},"spec":{"containers":[{"name":"db","image":"postgres:17.2","env":[{"name":"MODE","value":"fast"}]}]}}},
 "status":{"updateRevision":"db-7f8c9","currentRevision":"db-7f8c9"}}`
	revisionsControllerRevisions = `{"items":[
 {"metadata":{"name":"db-5a1b2","creationTimestamp":"2026-10-01T10:00:00Z","ownerReferences":[{"kind":"StatefulSet","name":"db","controller":true}]},
  "revision":5,"data":{"spec":{"template":{"$patch":"replace","metadata":{"labels":{"app":"db"}},"spec":{"containers":[{"name":"db","image":"postgres:16.4"}]}}}}},
 {"metadata":{"name":"db-7f8c9","creationTimestamp":"2026-10-03T10:00:00Z","annotations":{"kubernetes.io/change-cause":"to 17.2"},"ownerReferences":[{"kind":"StatefulSet","name":"db","controller":true}]},
  "revision":7,"data":{"spec":{"template":{"$patch":"replace","metadata":{"labels":{"app":"db"}},"spec":{"containers":[{"name":"db","image":"postgres:17.2","env":[{"name":"MODE","value":"fast"}]}]}}}}},
 {"metadata":{"name":"db-6c3d4","creationTimestamp":"2026-10-02T10:00:00Z","ownerReferences":[{"kind":"StatefulSet","name":"db","controller":true}]},
  "revision":6,"data":{"spec":{"template":{"$patch":"replace","metadata":{"labels":{"app":"db"}},"spec":{"containers":[{"name":"db","image":"postgres:17.0"}]}}}}},
 {"metadata":{"name":"cache-9e9e9","ownerReferences":[{"kind":"StatefulSet","name":"cache","controller":true}]},"revision":9,"data":{}}
]}`
)

func revisionsKube(t *testing.T) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	return actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/demo-shop/statefulsets/db":     revisionsStatefulSet,
		"GET /apis/apps/v1/namespaces/demo-shop/controllerrevisions": revisionsControllerRevisions,
		"PATCH /apis/apps/v1/namespaces/demo-shop/statefulsets/db":   `{}`,
		"PATCH /apis/apps/v1/namespaces/demo-shop/deployments/web":   `{}`,
	})
}

func TestControllerRevisionHistoryOrderAndCurrent(t *testing.T) {
	_, k := revisionsKube(t)

	h, err := readWorkloadHistory(context.Background(), k, workloadKinds[1], "demo-shop", "db")
	if err != nil {
		t.Fatal(err)
	}

	list := h.list()
	if list.Kind != "StatefulSet" || list.Manual || len(list.Revisions) != 3 {
		t.Fatalf("list = %+v", list)
	}

	got := []int{list.Revisions[0].Revision, list.Revisions[1].Revision, list.Revisions[2].Revision}
	if got[0] != 7 || got[1] != 6 || got[2] != 5 {
		t.Errorf("order = %v", got)
	}

	newest := list.Revisions[0]
	if !newest.Current || list.Revisions[1].Current || newest.ReplicaSet != "db-7f8c9" || newest.ChangeCause != "to 17.2" ||
		len(newest.Images) != 1 || newest.Images[0] != "postgres:17.2" || newest.Created == 0 {
		t.Errorf("newest = %+v", newest)
	}

	if _, ok := h.find(5); !ok {
		t.Error("revision 5 not found")
	}

	if tmpl, _ := h.find(5); tmpl.template["$patch"] != nil {
		t.Error("the $patch directive was kept")
	}
}

func TestDaemonSetHistoryMarksTheNewestAndOnDelete(t *testing.T) {
	_, k := actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/demo-shop/daemonsets/agent": `{"metadata":{"name":"agent"},"spec":{"selector":{"matchLabels":{"app":"agent"}},"updateStrategy":{"type":"OnDelete"}}}`,
		"GET /apis/apps/v1/namespaces/demo-shop/controllerrevisions": `{"items":[
			{"metadata":{"name":"agent-1","ownerReferences":[{"kind":"DaemonSet","name":"agent","controller":true}]},"revision":1,"data":{"spec":{"template":{"spec":{"containers":[{"image":"agent:1"}]}}}}},
			{"metadata":{"name":"agent-2","ownerReferences":[{"kind":"DaemonSet","name":"agent","controller":true}]},"revision":2,"data":{"spec":{"template":{"spec":{"containers":[{"image":"agent:2"}]}}}}},
			{"metadata":{"name":"agent-x","ownerReferences":[{"kind":"StatefulSet","name":"agent","controller":true}]},"revision":3,"data":{}}]}`,
	})

	h, err := readWorkloadHistory(context.Background(), k, workloadKinds[2], "demo-shop", "agent")
	if err != nil {
		t.Fatal(err)
	}

	list := h.list()
	if !list.Manual || len(list.Revisions) != 2 || !list.Revisions[0].Current || list.Revisions[0].Revision != 2 || list.Revisions[1].Current {
		t.Errorf("list = %+v", list)
	}
}

func TestRevisionDiffShowsTheTemplateChange(t *testing.T) {
	_, k := revisionsKube(t)

	h, err := readWorkloadHistory(context.Background(), k, workloadKinds[1], "demo-shop", "db")
	if err != nil {
		t.Fatal(err)
	}

	to, _ := h.find(5)

	d, err := buildRevisionDiff(h.current(), h.live, to)
	if err != nil {
		t.Fatal(err)
	}

	if d.From != "revision 7 (current)" || d.To != "revision 5" || d.FromRevision != 7 || d.ToRevision != 5 {
		t.Errorf("names = %q %q %d %d", d.From, d.To, d.FromRevision, d.ToRevision)
	}

	for _, want := range []string{"--- revision 7 (current)\n+++ revision 5\n", "-          image: postgres:17.2\n", "+        - image: postgres:16.4\n", "-            - name: MODE\n"} {
		if !strings.Contains(d.Diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, d.Diff)
		}
	}

	if strings.Contains(d.Diff, "$patch") {
		t.Errorf("diff shows the patch directive:\n%s", d.Diff)
	}

	if d.Images.From[0] != "postgres:17.2" || d.Images.To[0] != "postgres:16.4" {
		t.Errorf("images = %+v", d.Images)
	}

	if same, _ := buildRevisionDiff(7, h.live, h.revs[0]); same.Diff != "" {
		t.Errorf("the current revision differs from itself:\n%s", same.Diff)
	}
}

func TestRollbackControllerRevisionPatchesTheTemplate(t *testing.T) {
	f, k := revisionsKube(t)

	if err := rollbackControllerRevision(context.Background(), k, workloadKinds[1], "demo-shop", "db", 5); err != nil {
		t.Fatal(err)
	}

	req := lastActionRequest(f, http.MethodPatch)
	if req.contentType != "application/json-patch+json" || req.path != "/apis/apps/v1/namespaces/demo-shop/statefulsets/db" {
		t.Errorf("request = %+v", req)
	}

	var patch []struct {
		Op, Path string
		Value    map[string]any
	}

	if err := json.Unmarshal([]byte(req.body), &patch); err != nil || len(patch) != 1 {
		t.Fatalf("patch %q: %v", req.body, err)
	}

	if patch[0].Op != "replace" || patch[0].Path != "/spec/template" || patch[0].Value["$patch"] != nil {
		t.Errorf("patch = %+v", patch[0])
	}

	if images := mapTemplateImages(patch[0].Value); len(images) != 1 || images[0] != "postgres:16.4" {
		t.Errorf("images = %v", images)
	}

	for rev, want := range map[int]string{7: "already the current one", 4: "not kept anymore"} {
		if err := rollbackControllerRevision(context.Background(), k, workloadKinds[1], "demo-shop", "db", rev); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("revision %d: err = %v", rev, err)
		}
	}
}

func TestRolloutPausePatchesSpecPaused(t *testing.T) {
	f, k := revisionsKube(t)

	for _, paused := range []bool{true, false} {
		if err := pauseRollout(context.Background(), k, "demo-shop", "web", paused); err != nil {
			t.Fatal(err)
		}

		want := `{"spec":{"paused":false}}`
		if paused {
			want = `{"spec":{"paused":true}}`
		}

		if req := lastActionRequest(f, http.MethodPatch); req.body != want || req.contentType != "application/merge-patch+json" {
			t.Errorf("paused=%v: request = %+v", paused, req)
		}
	}
}

func TestRolloutStatusReportsPaused(t *testing.T) {
	_, k := actionsKube(t, map[string]string{
		"GET /apis/apps/v1/namespaces/demo-shop/deployments/web": `{"metadata":{"name":"web","generation":2},"spec":{"paused":true,"selector":{"matchLabels":{"app":"web"}}},"status":{"observedGeneration":2}}`,
		"GET /apis/apps/v1/namespaces/demo-shop/replicasets":     `{"items":[]}`,
		"GET /api/v1/namespaces/demo-shop/pods":                  `{"items":[]}`,
	})

	st, err := rolloutStatus(context.Background(), k, workloadKinds[0], "demo-shop", "web")
	if err != nil {
		t.Fatal(err)
	}

	if !st.Paused || st.Workload.State != workloadPaused {
		t.Errorf("status = %+v", st)
	}
}

func TestWorkloadRevisionsInTheDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeWorkloadRevisions(demo, "", "", "StatefulSet", "demo", "postgres")
	if err != nil {
		t.Fatal(err)
	}

	var list workloadRevisions
	if err := json.Unmarshal([]byte(out), &list); err != nil || list.Kind != "StatefulSet" || len(list.Revisions) != 3 ||
		!list.Revisions[0].Current || list.Revisions[0].Images[0] != "postgres:17" || list.Revisions[1].Images[0] != "postgres:16" {
		t.Fatalf("demo revisions = %s, %v", out, err)
	}

	diff, err := KubeRevisionDiff(demo, "", "", "StatefulSet", "demo", "postgres", list.Revisions[2].Revision)
	if err != nil || !strings.Contains(diff, "postgres:15") || !strings.Contains(diff, "LOG_LEVEL") {
		t.Errorf("demo diff = %s, %v", diff, err)
	}

	if _, err := KubeRevisionDiff(demo, "", "", "StatefulSet", "demo", "postgres", 1); err == nil {
		t.Error("a revision the demo does not keep has a diff")
	}

	if err := KubeRollbackWorkload(demo, "", "", "DaemonSet", "kube-system", "kube-proxy", 6); err != errDemoUnavailable {
		t.Errorf("demo rollback: %v", err)
	}

	if err := KubeRolloutPause(demo, "", "", "demo", "hello-ichor", true); err != errDemoUnavailable {
		t.Errorf("demo pause: %v", err)
	}
}

func TestWorkloadRevisionsRefusals(t *testing.T) {
	if _, err := KubeWorkloadRevisions("", "", "", "CronJob", "demo-shop", "db"); err == nil || !strings.Contains(err.Error(), "unsupported workload kind") {
		t.Errorf("kind: %v", err)
	}

	if err := KubeRollbackWorkload("", "", "", "StatefulSet", "demo-shop", "Bad Name", 1); err == nil {
		t.Error("an invalid name was accepted")
	}
}

func TestOlderImage(t *testing.T) {
	cases := map[string]string{
		"nginx:1.27":                         "nginx:1.25",
		"registry.k8s.io/kube-proxy:v1.34.0": "registry.k8s.io/kube-proxy:v1.32.0",
		"postgres:17":                        "postgres:15",
		"localhost:5000/app":                 "localhost:5000/app",
		"ghcr.io/example/web:2.4.1":          "ghcr.io/example/web:2.2.1",
	}

	for image, want := range cases {
		if got := olderImage(image, 2); got != want {
			t.Errorf("olderImage(%q) = %q, want %q", image, got, want)
		}
	}
}
