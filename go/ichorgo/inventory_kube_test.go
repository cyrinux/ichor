package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const kubeInventoryNodesJSON = `{"items":[{"metadata":{"name":"node-a"}},{"metadata":{"name":"node-b"}}]}`

// Two scheduled pods (Grafana with its sidecar, a finished backup Job) and one pending pod.
const kubeInventoryPodsJSON = `{"items":[
 {"metadata":{"name":"grafana-0","namespace":"monitoring"},"spec":{"nodeName":"node-a",
   "containers":[{"name":"grafana","image":"grafana/grafana:12.2.0"},{"name":"sidecar","image":"quay.io/kiwigrid/k8s-sidecar:1.30"}]},
  "status":{"phase":"Running","containerStatuses":[
   {"name":"grafana","ready":true,"state":{"running":{"startedAt":"2026-10-08T09:00:00Z"}},"containerID":"containerd://3f1"},
   {"name":"sidecar","ready":true,"state":{"running":{"startedAt":"2026-10-08T09:00:00Z"}},"containerID":"containerd://3f2"}]}},
 {"metadata":{"name":"nightly-backup-29","namespace":"tools","ownerReferences":[{"kind":"Job","name":"nightly-backup-29"}]},"spec":{"nodeName":"node-b",
   "containers":[{"name":"backup","image":"registry.example.com/internal/nightly-backup:1"}]},
  "status":{"phase":"Succeeded","containerStatuses":[{"name":"backup","ready":false,"state":{"terminated":{"reason":"Completed","exitCode":0}},"containerID":"containerd://9a"}]}},
 {"metadata":{"name":"pending-0","namespace":"tools"},"spec":{"containers":[{"name":"c","image":"registry.example.com/internal/pending:1"}]},
  "status":{"phase":"Pending"}}]}`

func decodeInventory(t *testing.T, out string) inventory {
	t.Helper()

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatalf("%v in %s", err, out)
	}

	return inv
}

// A cluster added from a kubeconfig gets its apps from the pod list, end to end through the
// stored kubeconfig, by KubeInventory and by ClusterInventory alike.
func TestKubeInventoryFromKubeconfig(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /version":      `{"gitVersion":"v1.34.1"}`,
		"GET /api/v1/nodes": kubeInventoryNodesJSON,
		"GET /api/v1/pods":  kubeInventoryPodsJSON,
	})

	stored, err := MergeKubeconfig("", "", strings.Replace(f.kubeconfigFor(f.URL), "server: https://other.invalid:6443", "server: "+f.URL, 1), "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeInventory(stored, "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	inv := decodeInventory(t, out)
	if inv.Nodes != 2 || inv.Answered != 2 || inv.Truncated || inv.Forbidden || len(inv.Apps) != 2 {
		t.Fatalf("inventory = %+v", inv)
	}

	grafana := appByID(t, inv, "grafana")
	if grafana.Containers != 2 || grafana.Running != 2 || !grafana.Known || len(grafana.Pods) != 1 || grafana.Pods[0].Node != "node-a" || grafana.Version != "12.2.0" {
		t.Errorf("grafana = %+v", grafana)
	}

	var backup inventoryApp

	for _, app := range inv.Apps {
		if app.ID != "grafana" {
			backup = app
		}
	}

	if backup.Containers != 1 || backup.Running != 0 || backup.Known || len(backup.Nodes) != 1 || backup.Nodes[0] != "node-b" {
		t.Errorf("backup = %+v", backup)
	}

	same, err := ClusterInventory(stored, "admin@test")
	if err != nil {
		t.Fatal(err)
	}

	if again := decodeInventory(t, same); len(again.Apps) != 2 || again.Nodes != 2 {
		t.Errorf("ClusterInventory on a kubeconfig = %+v", again)
	}
}

// pagedPodsAPI serves /api/v1/pods in three pages of one pod each, counting the pages asked.
type pagedPodsAPI struct {
	mu            sync.Mutex
	pages         int
	nodesStatus   int
	podsForbidden bool
}

func (a *pagedPodsAPI) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch r.URL.Path {
	case "/version":
		_, _ = io.WriteString(w, `{"gitVersion":"v1.34.1"}`)
	case "/api/v1/nodes":
		if a.nodesStatus != 0 {
			w.WriteHeader(a.nodesStatus)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"nodes is forbidden"}`)

			return
		}

		_, _ = io.WriteString(w, `{"items":[{"metadata":{"name":"n1"}}]}`)
	case "/api/v1/pods":
		if a.podsForbidden {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"pods is forbidden"}`)

			return
		}

		a.pages++

		next, n := "", 1
		switch r.URL.Query().Get("continue") {
		case "":
			next = "t1"
		case "t1":
			next, n = "t2", 2
		default:
			n = 3
		}

		fmt.Fprintf(w, `{"metadata":{"continue":%q},"items":[{"metadata":{"name":"p%d","namespace":"apps"},"spec":{"nodeName":"n1",
		  "containers":[{"name":"c","image":"registry.example.com/team/app%d:1"}]},
		  "status":{"phase":"Running","containerStatuses":[{"name":"c","ready":true,"state":{"running":{}}}]}}]}`, next, n, n)
	default:
		http.NotFound(w, r)
	}
}

func usePagedPodsAPI(t *testing.T) *pagedPodsAPI {
	t.Helper()

	a := &pagedPodsAPI{}
	f := newFakeKubeAPI(t, nil)
	f.Config.Handler = http.HandlerFunc(a.handler)
	useFakeKube(t, f)

	return a
}

func TestKubeInventoryPagesAndTruncates(t *testing.T) {
	a := usePagedPodsAPI(t)

	out, err := KubeInventory("cfg", "ctx", "")
	if err != nil {
		t.Fatal(err)
	}

	if inv := decodeInventory(t, out); inv.Truncated || len(inv.Apps) != 3 || a.pages != 3 {
		t.Fatalf("every page: %+v after %d pages", inv, a.pages)
	}

	saved := inventoryPodPages
	inventoryPodPages = 2

	t.Cleanup(func() { inventoryPodPages = saved })

	a.pages = 0

	out, err = KubeInventory("cfg", "ctx", "")
	if err != nil {
		t.Fatal(err)
	}

	if inv := decodeInventory(t, out); !inv.Truncated || len(inv.Apps) != 2 || a.pages != 2 || inv.Nodes != 1 || inv.Answered != 1 {
		t.Fatalf("capped: %+v after %d pages", inv, a.pages)
	}
}

func TestKubeInventoryForbidden(t *testing.T) {
	a := usePagedPodsAPI(t)
	a.podsForbidden = true

	out, err := KubeInventory("cfg", "ctx", "")
	if err != nil {
		t.Fatal(err)
	}

	if inv := decodeInventory(t, out); !inv.Forbidden || len(inv.Apps) != 0 || inv.Nodes != 1 {
		t.Fatalf("pods forbidden: %+v", inv)
	}

	a.podsForbidden, a.nodesStatus = false, http.StatusForbidden

	out, err = KubeInventory("cfg", "ctx", "")
	if err != nil {
		t.Fatal(err)
	}

	// The nodes are those the pods run on.
	if inv := decodeInventory(t, out); inv.Forbidden || len(inv.Apps) != 3 || inv.Nodes != 1 || inv.Answered != 1 {
		t.Fatalf("nodes forbidden: %+v", inv)
	}

	a.nodesStatus = http.StatusInternalServerError

	if _, err := KubeInventory("cfg", "ctx", ""); err == nil {
		t.Fatal("a broken node list was not an error")
	}
}

func TestPodContainersStates(t *testing.T) {
	var obj podObject
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"p","namespace":"ns"},"spec":{"nodeName":"n",
	  "initContainers":[{"name":"init","image":"busybox:1.37"}],
	  "containers":[{"name":"run","image":"a/b:1"},{"name":"wait","image":"a/c:1"},{"name":"new","image":"a/d:1"}]},
	  "status":{"initContainerStatuses":[{"name":"init","state":{"terminated":{"reason":"Completed","exitCode":0}},"containerID":"containerd://i1"}],
	   "containerStatuses":[{"name":"run","state":{"running":{}},"containerID":"containerd://r1"},{"name":"wait","state":{"waiting":{"reason":"ImagePullBackOff"}}}]}}`), &obj); err != nil {
		t.Fatal(err)
	}

	got := podContainers(obj)
	want := []containerInfo{
		{ID: "i1", PodNamespace: "ns", Pod: "p", Name: "init", Image: "busybox:1.37", Status: containerExited},
		{ID: "r1", PodNamespace: "ns", Pod: "p", Name: "run", Image: "a/b:1", Status: containerRunning},
		{ID: "ns/p/wait", PodNamespace: "ns", Pod: "p", Name: "wait", Image: "a/c:1", Status: "CONTAINER_CREATED"},
		{ID: "ns/p/new", PodNamespace: "ns", Pod: "p", Name: "new", Image: "a/d:1", Status: "CONTAINER_CREATED"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("container %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
