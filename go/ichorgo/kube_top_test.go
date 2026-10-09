package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const (
	topNodesBody = `{"items":[
		{"metadata":{"name":"w1"},"status":{"allocatable":{"cpu":"4","memory":"8Gi"}}},
		{"metadata":{"name":"w2"},"status":{"allocatable":{"cpu":"2000m","memory":"4Gi"}}}]}`
	topNodeMetricsBody = `{"items":[
		{"metadata":{"name":"w1"},"usage":{"cpu":"1500000000n","memory":"2Gi"}},
		{"metadata":{"name":"w2"},"usage":{"cpu":"250m","memory":"1048576Ki"}}]}`
	topPodsBody = `{"items":[
		{"metadata":{"name":"api","namespace":"web"},"spec":{"nodeName":"w1","containers":[
			{"name":"app","resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"cpu":"1","memory":"512Mi"}}},
			{"name":"proxy","resources":{"requests":{"cpu":"50m","memory":"64Mi"},"limits":{"cpu":"100m","memory":"128Mi"}}}]}},
		{"metadata":{"name":"batch","namespace":"web"},"spec":{"nodeName":"w2","containers":[
			{"name":"job","resources":{"requests":{"cpu":"100m"},"limits":{"memory":"1Gi"}}},
			{"name":"side"}]}}]}`
	topPodMetricsBody = `{"items":[
		{"metadata":{"name":"api","namespace":"web"},"containers":[
			{"name":"app","usage":{"cpu":"120m","memory":"200Mi"}},
			{"name":"proxy","usage":{"cpu":"5m","memory":"20Mi"}}]},
		{"metadata":{"name":"batch","namespace":"web"},"containers":[{"name":"job","usage":{"cpu":"900m","memory":"700Mi"}}]}]}`
)

func openFakeKube(t *testing.T, f *fakeKubeAPI) *kubeClient {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestTopNodesJoinsUsageWithAllocatable(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/nodes":                      topNodesBody,
		"GET /apis/metrics.k8s.io/v1beta1/nodes": topNodeMetricsBody,
	})

	top, err := readTopNodes(context.Background(), openFakeKube(t, f))
	if err != nil {
		t.Fatal(err)
	}

	if !top.Available || top.Forbidden || len(top.Nodes) != 2 {
		t.Fatalf("unexpected %+v", top)
	}

	w1 := top.Nodes[0]
	if w1.Name != "w1" || !near(w1.CPU, 1.5) || w1.CPUAllocatable != 4 || w1.CPUPercent != 37.5 || w1.MemoryPercent != 25 {
		t.Errorf("w1 %+v", w1)
	}

	if w2 := top.Nodes[1]; !near(w2.CPU, 0.25) || w2.Memory != 1<<30 || w2.MemoryPercent != 25 {
		t.Errorf("w2 %+v", w2)
	}
}

func TestTopPodsJoinsUsageWithRequestsAndLimits(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/pods":                      topPodsBody,
		"GET /apis/metrics.k8s.io/v1beta1/namespaces/web/pods": topPodMetricsBody,
	})

	top, err := readTopPods(context.Background(), openFakeKube(t, f), "web", "app=api,tier!=db")
	if err != nil {
		t.Fatal(err)
	}

	if !top.Available || !top.BoundsRead || len(top.Pods) != 2 {
		t.Fatalf("unexpected %+v", top)
	}

	api, batch := top.Pods[0], top.Pods[1]
	if api.Name != "api" || api.Node != "w1" || !near(api.CPU, 0.125) || api.Memory != 220<<20 {
		t.Errorf("api usage %+v", api)
	}

	if !near(api.CPURequest, 0.3) || !near(api.CPULimit, 1.1) || api.MemoryRequest != 320<<20 || api.MemoryLimit != 640<<20 {
		t.Errorf("api resources %+v", api)
	}

	// A container without a limit leaves the pod unlimited, as kubectl describe shows it.
	if batch.Name != "batch" || !near(batch.CPURequest, 0.1) || batch.CPULimit != 0 || batch.MemoryLimit != 0 {
		t.Errorf("batch %+v", batch)
	}

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/pods") && !strings.Contains(r.query, "labelSelector=app%3Dapi%2Ctier%21%3Ddb") {
			t.Errorf("%s sent query %q", r.path, r.query)
		}
	}
}

func TestTopWithoutMetricsServer(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		forbidden bool
	}{
		{"not installed", http.StatusNotFound, false},
		{"aggregated API down", http.StatusServiceUnavailable, false},
		{"not allowed", http.StatusForbidden, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": topNodesBody})
			if tc.status != http.StatusNotFound {
				f.answerWith("GET /apis/metrics.k8s.io/v1beta1/nodes", tc.status, `{"kind":"Status","code":`+strconv.Itoa(tc.status)+`}`)
			}

			top, err := readTopNodes(context.Background(), openFakeKube(t, f))
			if err != nil {
				t.Fatal(err)
			}

			if top.Available || top.Forbidden != tc.forbidden || len(top.Nodes) != 0 {
				t.Errorf("got %+v", top)
			}
		})
	}
}

func TestKubeTopRefusesBadSelectors(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, selector := range []string{"app in (a,b)", "a=b&watch=true", strings.Repeat("a", 2000)} {
		if _, err := KubeTopPods(cfg, "", "", "", selector); err == nil {
			t.Errorf("selector %q accepted", selector)
		}
	}
}

func TestKubeTopDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeTopNodes(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var nodes kubeTopNodes
	if err := json.Unmarshal([]byte(out), &nodes); err != nil || !nodes.Available || len(nodes.Nodes) == 0 {
		t.Fatalf("demo nodes: %v %s", err, out)
	}

	for _, n := range nodes.Nodes {
		if n.CPU <= 0 || n.CPU > n.CPUAllocatable || n.Memory <= 0 || n.Memory > n.MemoryAllocatable {
			t.Errorf("implausible demo node %+v", n)
		}
	}

	out, err = KubeTopPods(cfg, "", "", "demo", "")
	if err != nil {
		t.Fatal(err)
	}

	var pods kubeTopPods
	if err := json.Unmarshal([]byte(out), &pods); err != nil || len(pods.Pods) == 0 {
		t.Fatalf("demo pods: %v %s", err, out)
	}

	for _, p := range pods.Pods {
		if p.Namespace != "demo" || p.CPU <= 0 {
			t.Errorf("demo pod %+v", p)
		}
	}
}

func TestTopPodsOfEveryNamespaceReadMetricsOnly(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis/metrics.k8s.io/v1beta1/pods": topPodMetricsBody})

	top, err := readTopPods(context.Background(), openFakeKube(t, f), "", "")
	if err != nil {
		t.Fatal(err)
	}

	if !top.Available || top.BoundsRead || len(top.Pods) != 2 {
		t.Fatalf("unexpected %+v", top)
	}

	if api := top.Pods[0]; !near(api.CPU, 0.125) || api.CPURequest != 0 || api.MemoryLimit != 0 {
		t.Errorf("api %+v", api)
	}

	for _, r := range f.recorded() {
		if r.path == "/api/v1/pods" {
			t.Error("every pod of the cluster was listed")
		}
	}
}

func TestTopPodReadsOnlyThatPod(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/metrics.k8s.io/v1beta1/namespaces/web/pods/api": `{"metadata":{"name":"api","namespace":"web"},"containers":[
			{"name":"app","usage":{"cpu":"120m","memory":"200Mi"}},{"name":"proxy","usage":{"cpu":"5m","memory":"20Mi"}}]}`,
		"GET /api/v1/namespaces/web/pods/api": `{"metadata":{"name":"api","namespace":"web"},"spec":{"nodeName":"w1","containers":[
			{"name":"app","resources":{"requests":{"cpu":"250m"},"limits":{"cpu":"1","memory":"512Mi"}}},
			{"name":"proxy","resources":{"requests":{"cpu":"50m"},"limits":{"cpu":"100m","memory":"128Mi"}}}]}}`,
	})

	top, err := readTopPod(context.Background(), openFakeKube(t, f), "web", "api")
	if err != nil {
		t.Fatal(err)
	}

	if !top.Available || !top.BoundsRead || len(top.Pods) != 1 {
		t.Fatalf("unexpected %+v", top)
	}

	if p := top.Pods[0]; p.Node != "w1" || !near(p.CPU, 0.125) || !near(p.CPULimit, 1.1) || p.MemoryLimit != 640<<20 {
		t.Errorf("pod %+v", p)
	}

	calls := 0

	for _, r := range f.recorded() {
		if r.path != "/version" {
			calls++
		}
	}

	if calls != 2 {
		t.Errorf("%d requests, want the pod's metrics and the pod", calls)
	}
}

func TestTopPodWithoutMetrics(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("GET /apis/metrics.k8s.io/v1beta1/namespaces/web/pods/api", http.StatusForbidden, `{"kind":"Status","code":403}`)

	top, err := readTopPod(context.Background(), openFakeKube(t, f), "web", "api")
	if err != nil {
		t.Fatal(err)
	}

	if top.Available || !top.Forbidden || len(top.Pods) != 0 {
		t.Errorf("got %+v", top)
	}
}

func TestKubeTopPodDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeTopPod(cfg, "", "", "demo", "postgres-0")
	if err != nil {
		t.Fatal(err)
	}

	var top kubeTopPods
	if err := json.Unmarshal([]byte(out), &top); err != nil || len(top.Pods) != 1 || top.Pods[0].MemoryLimit == 0 || !top.BoundsRead {
		t.Fatalf("demo pod: %v %s", err, out)
	}

	out, err = KubeTopPods(cfg, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(out), &top); err != nil || top.BoundsRead || top.Pods[0].CPURequest != 0 {
		t.Fatalf("demo of every namespace: %v %s", err, out)
	}

	if _, err := KubeTopPod(cfg, "", "", "demo", "Bad Name"); err == nil {
		t.Error("a bad pod name was accepted")
	}
}
