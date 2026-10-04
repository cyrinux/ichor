package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRolloutStatusDeployment(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/web": `{
			"metadata":{"name":"web","namespace":"shop","generation":4,"annotations":{"deployment.kubernetes.io/revision":"7"}},
			"spec":{"replicas":2,"selector":{"matchLabels":{"app":"web"}}},
			"status":{"observedGeneration":4,"replicas":3,"readyReplicas":2,"updatedReplicas":1,"availableReplicas":2}}`,
		"GET /apis/apps/v1/namespaces/shop/replicasets": `{"items":[
			{"metadata":{"name":"web-old","labels":{"pod-template-hash":"old"},"annotations":{"deployment.kubernetes.io/revision":"6"},
				"ownerReferences":[{"kind":"Deployment","name":"web"}]}},
			{"metadata":{"name":"web-new","labels":{"pod-template-hash":"new"},"annotations":{"deployment.kubernetes.io/revision":"7"},
				"ownerReferences":[{"kind":"Deployment","name":"web"}]}},
			{"metadata":{"name":"webhook-x","labels":{"pod-template-hash":"new"},"annotations":{"deployment.kubernetes.io/revision":"7"},
				"ownerReferences":[{"kind":"Deployment","name":"webhook"}]}}]}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[
			` + fakeRolloutPod("web-old-a", "ReplicaSet", "web-old", "pod-template-hash", "old", "2026-10-01T10:00:00Z", true) + `,
			` + fakeRolloutPod("web-old-b", "ReplicaSet", "web-old", "pod-template-hash", "old", "2026-10-01T10:00:00Z", true) + `,
			` + fakeRolloutPod("web-new-a", "ReplicaSet", "web-new", "pod-template-hash", "new", "2026-10-04T10:00:00Z", false) + `,
			` + fakeRolloutPod("webhook-x-a", "ReplicaSet", "webhook-x", "pod-template-hash", "new", "2026-10-04T10:00:00Z", true) + `]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	st, err := rolloutStatus(context.Background(), k, workloadKinds[0], "shop", "web")
	if err != nil {
		t.Fatal(err)
	}

	if st.Done || st.Failed || st.Workload.State != workloadProgressing || st.Workload.Updated != 1 {
		t.Fatalf("unexpected status %+v", st)
	}

	if got, want := rolloutPodsLine(st), "web-new-a:new:Running:0/1 web-old-a:old:Running:1/1 web-old-b:old:Running:1/1"; got != want {
		t.Fatalf("pods %q, want %q", got, want)
	}

	for _, r := range f.recorded() {
		if r.method != "GET" {
			t.Fatalf("a status read sent %s %s", r.method, r.path)
		}
	}
}

func TestRolloutStatusStatefulSetAndDaemonSet(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/statefulsets/db": `{
			"metadata":{"name":"db","namespace":"shop","generation":2},
			"spec":{"replicas":2,"selector":{"matchLabels":{"app":"db"}}},
			"status":{"observedGeneration":2,"replicas":2,"readyReplicas":2,"updatedReplicas":2,"updateRevision":"db-2"}}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[
			` + fakeRolloutPod("db-0", "StatefulSet", "db", "controller-revision-hash", "db-2", "2026-10-04T10:00:00Z", true) + `,
			` + fakeRolloutPod("db-1", "StatefulSet", "db", "controller-revision-hash", "db-2", "2026-10-04T10:01:00Z", true) + `]}`,
		"GET /apis/apps/v1/namespaces/kube-system/daemonsets/proxy": `{
			"metadata":{"name":"proxy","namespace":"kube-system","generation":5,"annotations":{"deprecated.daemonset.template.generation":"3"}},
			"spec":{"selector":{"matchLabels":{"k8s-app":"proxy"}}},
			"status":{"observedGeneration":5,"desiredNumberScheduled":2,"numberReady":2,"updatedNumberScheduled":1,"numberAvailable":2}}`,
		"GET /api/v1/namespaces/kube-system/pods": `{"items":[
			` + fakeRolloutPod("proxy-a", "DaemonSet", "proxy", "pod-template-generation", "3", "2026-10-04T10:00:00Z", true) + `,
			` + fakeRolloutPod("proxy-b", "DaemonSet", "proxy", "pod-template-generation", "2", "2026-10-01T10:00:00Z", true) + `]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	db, err := rolloutStatus(context.Background(), k, workloadKinds[1], "shop", "db")
	if err != nil {
		t.Fatal(err)
	}

	if !db.Done || rolloutPodsLine(db) != "db-1:new:Running:1/1 db-0:new:Running:1/1" {
		t.Fatalf("statefulset %+v", db)
	}

	proxy, err := rolloutStatus(context.Background(), k, workloadKinds[2], "kube-system", "proxy")
	if err != nil {
		t.Fatal(err)
	}

	if proxy.Done || rolloutPodsLine(proxy) != "proxy-a:new:Running:1/1 proxy-b:old:Running:1/1" {
		t.Fatalf("daemonset %+v", proxy)
	}
}

func TestRolloutStatusStaleAndManual(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		// Just restarted: the controller still reports the old revision as the update one.
		"GET /apis/apps/v1/namespaces/shop/statefulsets/db": `{
			"metadata":{"name":"db","namespace":"shop","generation":3},
			"spec":{"replicas":1,"selector":{"matchLabels":{"app":"db"}},"updateStrategy":{"type":"OnDelete"}},
			"status":{"observedGeneration":2,"replicas":1,"readyReplicas":1,"updatedReplicas":1,"updateRevision":"db-1"}}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[
			` + fakeRolloutPod("db-0", "StatefulSet", "db", "controller-revision-hash", "db-1", "2026-10-01T10:00:00Z", true) + `]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	st, err := rolloutStatus(context.Background(), k, workloadKinds[1], "shop", "db")
	if err != nil {
		t.Fatal(err)
	}

	if st.Done || !st.Manual || rolloutPodsLine(st) != "db-0:old:Running:1/1" {
		t.Fatalf("unexpected status %+v", st)
	}
}

func TestRolloutStatusProgressDeadline(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/namespaces/shop/deployments/web": `{"metadata":{"name":"web"},"spec":{"replicas":1},
			"status":{"replicas":2,"updatedReplicas":1,"readyReplicas":1,
				"conditions":[{"type":"Progressing","status":"False","reason":"ProgressDeadlineExceeded"}]}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// No selector: the pods are not listed.
	st, err := rolloutStatus(context.Background(), k, workloadKinds[0], "shop", "web")
	if err != nil {
		t.Fatal(err)
	}

	if !st.Failed || st.Done || len(st.Pods) != 0 {
		t.Fatalf("unexpected status %+v", st)
	}
}

func TestKubeRolloutStatusValidatesAndDemo(t *testing.T) {
	if _, err := KubeRolloutStatus("", "", "", "Job", "ns", "x"); err == nil || !strings.Contains(err.Error(), "unsupported workload kind") {
		t.Fatalf("got %v", err)
	}

	if _, err := KubeRolloutStatus("", "", "", "Deployment", "ns", "../x"); err == nil || !strings.Contains(err.Error(), "invalid Kubernetes name") {
		t.Fatalf("got %v", err)
	}

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeRolloutStatus(cfg, "", "", "Deployment", "demo", "hello-ichor")
	if err != nil {
		t.Fatal(err)
	}

	var st kubeRolloutStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		t.Fatal(err)
	}

	if !st.Done || st.Workload.Name != "hello-ichor" || len(st.Pods) != 3 {
		t.Fatalf("demo status %s", out)
	}
}

func TestMatchLabelsSelector(t *testing.T) {
	if got := matchLabelsSelector(map[string]string{"b": "2", "a": "1"}); got != "a=1,b=2" {
		t.Fatalf("got %q", got)
	}

	if matchLabelsSelector(nil) != "" {
		t.Fatal("expected no selector")
	}
}

func TestRolloutStatusMissingWorkload(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var apiErr *kubeAPIError
	if _, err := rolloutStatus(context.Background(), k, workloadKinds[0], "shop", "gone"); !errors.As(err, &apiErr) || apiErr.Code != 404 {
		t.Fatalf("got %v", err)
	}
}

// fakeRolloutPod is a one-container pod owned by kind/owner, labelled key=value.
func fakeRolloutPod(name, kind, owner, key, value, created string, ready bool) string {
	return fmt.Sprintf(`{"metadata":{"name":%q,"creationTimestamp":%q,"labels":{%q:%q},"ownerReferences":[{"kind":%q,"name":%q}]},
		"spec":{"containers":[{"image":"x"}]},"status":{"phase":"Running","containerStatuses":[{"ready":%t,"state":{}}]}}`,
		name, created, key, value, kind, owner, ready)
}

func rolloutPodsLine(st kubeRolloutStatus) string {
	parts := make([]string, 0, len(st.Pods))
	for _, p := range st.Pods {
		rev := "old"
		if p.Updated {
			rev = "new"
		}

		parts = append(parts, fmt.Sprintf("%s:%s:%s:%d/%d", p.Name, rev, p.Status, p.Ready, p.Containers))
	}

	return strings.Join(parts, " ")
}
