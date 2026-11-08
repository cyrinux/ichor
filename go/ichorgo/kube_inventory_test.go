package ichorgo

import (
	"encoding/json"
	"testing"
)

func TestKubeInventoryFromPods(t *testing.T) {
	answers := map[string]string{
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"argocd-server-7d9f","namespace":"argocd"},
		   "spec":{"nodeName":"worker-1","containers":[{"name":"server","image":"quay.io/argoproj/argocd:v3.1.0"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"server","ready":true,"state":{}}]}},
		  {"metadata":{"name":"grafana-0","namespace":"monitoring"},
		   "spec":{"nodeName":"worker-2","containers":[{"name":"grafana","image":"grafana/grafana:12.2.0"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"grafana","ready":true,"state":{}}]}}
		]}`,
	}

	f := newFakeKubeAPI(t, answers)

	out, err := KubeInventory(f.kubeconfigFor(f.URL), "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}

	if inv.Nodes != 2 || inv.Answered != 2 {
		t.Fatalf("nodes %d answered %d, want 2 and 2", inv.Nodes, inv.Answered)
	}

	ids := map[string]inventoryApp{}
	for _, a := range inv.Apps {
		ids[a.ID] = a
	}

	for _, id := range []string{"argo-cd", "grafana"} {
		if a, ok := ids[id]; !ok || a.Running != 1 {
			t.Errorf("app %q: %+v (found %v), want one running container; apps %+v", id, a, ok, inv.Apps)
		}
	}
}

func TestPodContainerStatus(t *testing.T) {
	for status, want := range map[string]string{
		"Running":          containerRunning,
		"Completed":        containerExited,
		"CrashLoopBackOff": "CONTAINER_UNKNOWN",
		"Pending":          "CONTAINER_UNKNOWN",
	} {
		if got := podContainerStatus(status); got != want {
			t.Errorf("podContainerStatus(%q) = %q, want %q", status, got, want)
		}
	}
}
