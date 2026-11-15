package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The one-pod read lists each container's usage against its own bounds, as
// `kubectl top pod NAME --containers` does, in the pod spec's order.
func TestTopPodListsItsContainers(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		// metrics-server's order is not the spec's; "warmup" has no metrics yet.
		"GET /apis/metrics.k8s.io/v1beta1/namespaces/web/pods/api": `{"metadata":{"name":"api","namespace":"web"},"containers":[
			{"name":"mesh","usage":{"cpu":"15m","memory":"30Mi"}},
			{"name":"proxy","usage":{"cpu":"5m","memory":"20Mi"}},
			{"name":"app","usage":{"cpu":"120m","memory":"200Mi"}}]}`,
		// "mesh" is a native sidecar (an init container that always restarts); "migrate" is an
		// ordinary init container, done before the pod ran, so it is no row.
		"GET /api/v1/namespaces/web/pods/api": `{"metadata":{"name":"api","namespace":"web"},"spec":{"nodeName":"w1",
			"initContainers":[
				{"name":"migrate","resources":{"limits":{"cpu":"2"}}},
				{"name":"mesh","restartPolicy":"Always","resources":{"requests":{"cpu":"10m","memory":"32Mi"},"limits":{"memory":"64Mi"}}}],
			"containers":[
				{"name":"app","resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"cpu":"1"}}},
				{"name":"proxy","resources":{"requests":{"cpu":"50m"},"limits":{"cpu":"100m","memory":"128Mi"}}},
				{"name":"warmup","resources":{}}]}}`,
	})

	top, err := readTopPod(context.Background(), openFakeKube(t, f), "web", "api")
	if err != nil {
		t.Fatal(err)
	}

	if len(top.Pods) != 1 {
		t.Fatalf("unexpected %+v", top)
	}

	p := top.Pods[0]

	// The pod's own figures are unchanged: usage over every reported container, bounds over
	// spec.containers, memory unbounded because app has no memory limit.
	if !near(p.CPU, 0.14) || p.Memory != 250<<20 || !near(p.CPURequest, 0.3) || p.CPULimit != 0 || p.MemoryLimit != 0 {
		t.Errorf("pod %+v", p)
	}

	want := []kubeTopContainer{
		{Name: "app", CPU: 0.12, Memory: 200 << 20, CPURequest: 0.25, CPULimit: 1, MemoryRequest: 256 << 20},
		{Name: "proxy", CPU: 0.005, Memory: 20 << 20, CPURequest: 0.05, CPULimit: 0.1, MemoryLimit: 128 << 20},
		{Name: "warmup"},
		{Name: "mesh", CPU: 0.015, Memory: 30 << 20, CPURequest: 0.01, MemoryRequest: 32 << 20, MemoryLimit: 64 << 20},
	}

	if len(p.Containers) != len(want) {
		t.Fatalf("containers %+v", p.Containers)
	}

	for i, c := range p.Containers {
		w := want[i]
		if c.Name != w.Name || !near(c.CPU, w.CPU) || c.Memory != w.Memory || !near(c.CPURequest, w.CPURequest) ||
			!near(c.CPULimit, w.CPULimit) || c.MemoryRequest != w.MemoryRequest || c.MemoryLimit != w.MemoryLimit {
			t.Errorf("container %d: %+v, want %+v", i, c, w)
		}
	}
}

// The namespace read stays as small as before (CYR-112): no per-container rows.
func TestTopPodsOfANamespaceListNoContainers(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/metrics.k8s.io/v1beta1/namespaces/web/pods": `{"items":[{"metadata":{"name":"api","namespace":"web"},"containers":[
			{"name":"app","usage":{"cpu":"120m","memory":"200Mi"}},{"name":"proxy","usage":{"cpu":"5m","memory":"20Mi"}}]}]}`,
		"GET /api/v1/namespaces/web/pods": `{"items":[{"metadata":{"name":"api","namespace":"web"},"spec":{"containers":[
			{"name":"app","resources":{}},{"name":"proxy","resources":{}}]}}]}`,
	})

	top, err := readTopPods(context.Background(), openFakeKube(t, f), "web", "")
	if err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}

	if len(top.Pods) != 1 || strings.Contains(string(raw), `"containers"`) {
		t.Errorf("the list carries containers: %s", raw)
	}
}

func TestKubeTopPodDemoContainers(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		pod   string
		names []string
	}{
		{"postgres-0", []string{"postgres", "metrics"}},
		{"hello-ichor-7d9c5-abcde", []string{"hello-ichor"}},
	} {
		out, err := KubeTopPod(cfg, "", "", "demo", tc.pod)
		if err != nil {
			t.Fatal(err)
		}

		var top kubeTopPods
		if err := json.Unmarshal([]byte(out), &top); err != nil || len(top.Pods) != 1 {
			t.Fatalf("%s: %v %s", tc.pod, err, out)
		}

		p := top.Pods[0]
		if len(p.Containers) != len(tc.names) {
			t.Fatalf("%s: containers %+v", tc.pod, p.Containers)
		}

		var cpu, memory float64

		for i, c := range p.Containers {
			if c.Name != tc.names[i] {
				t.Errorf("%s: container %d is %q, want %q", tc.pod, i, c.Name, tc.names[i])
			}

			cpu, memory = cpu+c.CPU, memory+c.Memory
		}

		// The containers add up to the pod, as metrics-server's would.
		if !near(cpu, p.CPU) || !near(memory, p.Memory) {
			t.Errorf("%s: containers sum to %v CPU, %v memory; pod %v, %v", tc.pod, cpu, memory, p.CPU, p.Memory)
		}
	}

	// The demo's pod list says postgres-0 runs two containers, both ready.
	for _, p := range demoPods() {
		if p.Name == "postgres-0" && (p.Containers != 2 || p.Ready != 2 || !p.Healthy || len(p.Images) != 2) {
			t.Errorf("demo postgres-0: %+v", p)
		}
	}
}
