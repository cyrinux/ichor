package talosmobile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func decodePod(t *testing.T, js string) podObject {
	t.Helper()

	var obj podObject
	if err := json.Unmarshal([]byte(js), &obj); err != nil {
		t.Fatal(err)
	}

	return obj
}

func TestPodStatusLikeKubectl(t *testing.T) {
	cases := map[string]string{
		"Running":               `{"status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}}`,
		"Pending":               `{"status":{"phase":"Pending"}}`,
		"CrashLoopBackOff":      `{"status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}},{"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`,
		"Completed":             `{"status":{"phase":"Succeeded","containerStatuses":[{"state":{"terminated":{"reason":"Completed"}}}]}}`,
		"ExitCode:3":            `{"status":{"phase":"Failed","containerStatuses":[{"state":{"terminated":{"exitCode":3}}}]}}`,
		"Signal:9":              `{"status":{"phase":"Failed","containerStatuses":[{"state":{"terminated":{"signal":9}}}]}}`,
		"Init:Error":            `{"spec":{"initContainers":[{},{}]},"status":{"phase":"Pending","initContainerStatuses":[{"state":{"terminated":{"exitCode":0}}},{"state":{"terminated":{"reason":"Error","exitCode":1}}}]}}`,
		"Init:1/2":              `{"spec":{"initContainers":[{},{}]},"status":{"phase":"Pending","initContainerStatuses":[{"state":{"terminated":{"exitCode":0}}},{"state":{"running":{}}}]}}`,
		"Init:ImagePullBackOff": `{"spec":{"initContainers":[{}]},"status":{"phase":"Pending","initContainerStatuses":[{"state":{"waiting":{"reason":"ImagePullBackOff"}}}]}}`,
		"Terminating":           `{"metadata":{"deletionTimestamp":"2026-10-03T10:00:00Z"},"status":{"phase":"Running"}}`,
		"Unknown":               `{"metadata":{"deletionTimestamp":"2026-10-03T10:00:00Z"},"status":{"phase":"Running","reason":"NodeLost"}}`,
		"Evicted":               `{"status":{"phase":"Failed","reason":"Evicted"}}`,
	}

	for want, js := range cases {
		if got := podStatus(decodePod(t, js)); got != want {
			t.Errorf("%s: got %s", want, got)
		}
	}
}

func TestMapPod(t *testing.T) {
	p := mapPod(decodePod(t, `{"metadata":{"name":"web-1","namespace":"shop","creationTimestamp":"2026-01-02T03:04:05Z",
		"ownerReferences":[{"kind":"ReplicaSet","name":"web-5d8f"}]},
		"spec":{"nodeName":"w1","containers":[{"image":"nginx"},{"image":"envoy"}]},
		"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":2,"state":{"running":{}}},{"ready":false,"restartCount":1,"state":{"running":{}}}]}}`))

	if p.Status != "Running" || p.Healthy || p.Ready != 1 || p.Containers != 2 || p.Restarts != 3 ||
		p.Node != "w1" || p.Owner != "ReplicaSet/web-5d8f" || strings.Join(p.Images, ",") != "nginx,envoy" {
		t.Fatalf("unexpected pod %+v", p)
	}
}

func TestListAndDeletePods(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/pods": `{"items":[{"metadata":{"name":"b","namespace":"z"},"spec":{"containers":[{}]},"status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}},
			{"metadata":{"name":"a","namespace":"z"},"status":{"phase":"Succeeded"}}]}`,
		"DELETE /api/v1/namespaces/z/pods/a b": `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil)
	if err != nil {
		t.Fatal(err)
	}

	list, err := listPods(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if len(list.Pods) != 2 || list.Pods[0].Name != "a" || !list.Pods[1].Healthy {
		t.Fatalf("unexpected pods %+v", list.Pods)
	}

	if err := k.do(context.Background(), http.MethodDelete, podPath("z", "a b"), "", nil, nil); err != nil {
		t.Fatal(err)
	}

	if r := f.recorded(); r[len(r)-1].method != http.MethodDelete || r[len(r)-1].path != "/api/v1/namespaces/z/pods/a b" {
		t.Fatalf("unexpected request %+v", r[len(r)-1])
	}
}

func TestKubePodsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubePods(cfg, "")
	if err != nil || !strings.Contains(out, "CrashLoopBackOff") {
		t.Fatalf("demo pods: %v %s", err, out)
	}

	if err := KubeDeletePod(cfg, "", "demo", "worker-6f4b8-uvwxy"); !errors.Is(err, demoUnavailable) {
		t.Fatalf("got %v", err)
	}

	if err := KubeDeletePod(cfg, "", "", "x"); err == nil {
		t.Fatal("expected a validation error")
	}
}
