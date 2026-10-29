package ichorgo

import (
	"encoding/json"
	"testing"
	"time"
)

func idle(kind, namespace, name string, desired int32, reason string, images ...string) idleWorkload {
	return idleWorkload{inventoryWorkload{Kind: kind, Namespace: namespace, Name: name, Desired: desired, Reason: reason}, images}
}

func appsByID(apps []inventoryApp) map[string]inventoryApp {
	ids := map[string]inventoryApp{}
	for _, a := range apps {
		ids[a.ID] = a
	}

	return ids
}

func TestIdleWorkloadsAddAppsThatRunNothing(t *testing.T) {
	inv := buildInventory(1, 1, []nodeContainers{{Node: "worker-1", Containers: []containerInfo{
		{PodNamespace: "monitoring", Pod: "grafana-0", Name: "grafana", Image: "grafana/grafana:12.2.0", Status: containerRunning},
	}}})

	got := withIdleWorkloads(inv, []idleWorkload{
		idle("Deployment", "docs", "paperless", 1, "volume is faulted", "paperlessngx/paperless-ngx:3.3.4"),
		idle("Deployment", "docs", "paperless-ai", 0, "", "clusterzx/paperless-ai:3.0.9"),
		// Its app runs: a stopped replica of it changes nothing.
		idle("Deployment", "monitoring", "grafana-canary", 0, "", "grafana/grafana:12.3.0"),
	})

	ids := appsByID(got.Apps)
	if len(got.Apps) != 3 {
		t.Fatalf("apps %+v, want grafana, paperless-ngx and paperless-ai", got.Apps)
	}

	if a := ids["grafana"]; a.State != "" || a.Running != 1 || len(a.Workloads) != 0 {
		t.Errorf("grafana %+v, want unchanged", a)
	}

	p := ids["paperless-ngx"]
	if p.State != appNotStarting || p.Reason != "volume is faulted" || !p.Known || p.Category != "productivity" ||
		p.Version != "3.3.4" || p.Icon != "paperless-ngx" || p.Containers != 0 || p.Running != 0 || len(p.Pods) != 0 || len(p.Nodes) != 0 {
		t.Errorf("paperless-ngx %+v, want a known app not starting, without containers", p)
	}

	if len(p.Workloads) != 1 || p.Workloads[0] != (inventoryWorkload{Kind: "Deployment", Namespace: "docs", Name: "paperless", Desired: 1, Reason: "volume is faulted"}) {
		t.Errorf("paperless-ngx workloads %+v", p.Workloads)
	}

	if img := p.Images; len(img) != 1 || img[0].Containers != 0 || img[0].Tag != "3.3.4" {
		t.Errorf("paperless-ngx images %+v, want its template's, no container", img)
	}

	if a := ids["paperless-ai"]; a.State != appStopped || a.Reason != "" {
		t.Errorf("paperless-ai %+v, want stopped", a)
	}

	// Sorted by name like the rest.
	if got.Apps[0].ID != "grafana" || got.Apps[1].ID != "paperless-ai" || got.Apps[2].ID != "paperless-ngx" {
		t.Errorf("order %v", []string{got.Apps[0].ID, got.Apps[1].ID, got.Apps[2].ID})
	}

	if len(inv.Apps) != 1 {
		t.Errorf("the inventory given changed: %+v", inv.Apps)
	}
}

func TestIdleWorkloadsMarkAListedAppThatRunsNothing(t *testing.T) {
	// A kubeconfig cluster lists the pending pod; a Talos one the exited containers of a crash loop.
	inv := buildInventory(1, 1, []nodeContainers{{Node: "", Containers: []containerInfo{
		{PodNamespace: "docs", Pod: "paperless-5d8f7c9b6-k2m8p", Name: "paperless", Image: "paperlessngx/paperless-ngx:3.3.4", Status: "CONTAINER_UNKNOWN"},
	}}})

	got := withIdleWorkloads(inv, []idleWorkload{
		idle("StatefulSet", "docs", "paperless-cache", 0, "", "paperlessngx/paperless-ngx:3.3.4"),
		idle("Deployment", "docs", "paperless", 1, "0/3 nodes are available", "paperlessngx/paperless-ngx:3.3.4"),
	})

	if len(got.Apps) != 1 {
		t.Fatalf("apps %+v, want one", got.Apps)
	}

	a := got.Apps[0]
	if a.State != appNotStarting || a.Reason != "0/3 nodes are available" || a.Containers != 1 || len(a.Pods) != 1 || len(a.Workloads) != 2 {
		t.Errorf("%+v, want its pod kept, not starting (over stopped) with both workloads", a)
	}
}

func TestIdleAppOfBaseImagesIsNamedByItsWorkload(t *testing.T) {
	a, ok := idleApp(idle("Deployment", "tools", "backup-runner", 0, "", "alpine:3.22"))
	if !ok || a.ID != podKeyPrefix+"tools/backup-runner" || a.Name != "Backup Runner" || a.State != appStopped {
		t.Errorf("%+v %v, want the workload's name", a, ok)
	}

	if _, ok := idleApp(idle("Deployment", "tools", "empty", 0, "")); ok {
		t.Error("a template without images names no app")
	}
}

func TestPodOfWorkload(t *testing.T) {
	deploy := inventoryWorkload{Kind: "Deployment", Namespace: "docs", Name: "paperless"}
	sts := inventoryWorkload{Kind: "StatefulSet", Namespace: "docs", Name: "paperless"}

	for _, tc := range []struct {
		pod  kubePod
		w    inventoryWorkload
		want bool
	}{
		{kubePod{Namespace: "docs", Owner: "ReplicaSet/paperless-54755bd44f"}, deploy, true},
		{kubePod{Namespace: "docs", Owner: "ReplicaSet/paperless-ai-6c9d8b7f5"}, deploy, false},
		{kubePod{Namespace: "other", Owner: "ReplicaSet/paperless-54755bd44f"}, deploy, false},
		{kubePod{Namespace: "docs", Owner: "StatefulSet/paperless"}, sts, true},
		{kubePod{Namespace: "docs", Owner: "StatefulSet/paperless"}, deploy, false},
		{kubePod{Namespace: "docs", Owner: ""}, deploy, false},
	} {
		if got := podOf(tc.pod, tc.w); got != tc.want {
			t.Errorf("podOf(%+v, %+v) = %v, want %v", tc.pod, tc.w, got, tc.want)
		}
	}
}

func TestKubeInventoryListsIdleWorkloads(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"paperless-54755bd44f-jqsg5","namespace":"docs","ownerReferences":[{"kind":"ReplicaSet","name":"paperless-54755bd44f"}]},
		   "spec":{"nodeName":"worker-1","containers":[{"name":"paperless","image":"paperlessngx/paperless-ngx:3.3.4"}]},
		   "status":{"phase":"Pending","containerStatuses":[{"name":"paperless","ready":false,"state":{"waiting":{"reason":"ContainerCreating"}}}]}}
		]}`,
		"GET /api/v1/namespaces/docs/pods": `{"items":[
		  {"metadata":{"name":"paperless-54755bd44f-jqsg5","namespace":"docs","ownerReferences":[{"kind":"ReplicaSet","name":"paperless-54755bd44f"}]},
		   "spec":{"nodeName":"worker-1","containers":[{"name":"paperless","image":"paperlessngx/paperless-ngx:3.3.4"}]},
		   "status":{"phase":"Pending","containerStatuses":[{"name":"paperless","ready":false,"state":{"waiting":{"reason":"ContainerCreating"}}}]}}
		]}`,
		"GET /api/v1/namespaces/docs/events": `{"items":[
		  {"type":"Normal","reason":"Scheduled","message":"Successfully assigned","involvedObject":{"kind":"Pod","name":"paperless-54755bd44f-jqsg5","namespace":"docs"},"lastTimestamp":"2026-10-29T08:00:00Z"},
		  {"type":"Warning","reason":"FailedAttachVolume","message":"AttachVolume.Attach failed: volume is faulted","involvedObject":{"kind":"Pod","name":"paperless-54755bd44f-jqsg5","namespace":"docs"},"lastTimestamp":"2026-10-29T09:00:00Z"}
		]}`,
		"GET /apis/apps/v1/deployments": `{"items":[
		  {"metadata":{"name":"paperless","namespace":"docs"},"spec":{"replicas":1,"template":{"spec":{"containers":[{"image":"paperlessngx/paperless-ngx:3.3.4"}]}}},"status":{"replicas":1}},
		  {"metadata":{"name":"paperless-ai","namespace":"docs"},"spec":{"replicas":0,"template":{"spec":{"containers":[{"image":"clusterzx/paperless-ai:3.0.9"}]}}},"status":{}}
		]}`,
		"GET /apis/apps/v1/statefulsets": `{"items":[]}`,
	})

	out, err := KubeInventory(f.kubeconfigFor(f.URL), "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}

	ids := appsByID(inv.Apps)

	if a := ids["paperless-ngx"]; a.State != appNotStarting || a.Reason != "AttachVolume.Attach failed: volume is faulted" {
		t.Errorf("paperless-ngx %+v, want not starting with the pod's warning", a)
	}

	if a := ids["paperless-ai"]; a.State != appStopped || len(a.Workloads) != 1 || a.Workloads[0].Name != "paperless-ai" {
		t.Errorf("paperless-ai %+v, want stopped with its Deployment", a)
	}
}

func TestStuckPodPrefersTheReplacement(t *testing.T) {
	w := inventoryWorkload{Kind: "Deployment", Namespace: "chat", Name: "server"}
	old := kubePod{Namespace: "chat", Name: "server-647bc76564-4rvc4", Owner: "ReplicaSet/server-647bc76564", Status: "Terminating"}
	repl := kubePod{Namespace: "chat", Name: "server-647bc76564-jwbsx", Owner: "ReplicaSet/server-647bc76564", Status: "Pending"}

	if got := stuckPodOf([]kubePod{old, repl}, w); got != 1 {
		t.Errorf("stuckPodOf = %d, want the pending replacement", got)
	}

	if got := stuckPodOf([]kubePod{old}, w); got != 0 {
		t.Errorf("stuckPodOf = %d, want the terminating pod when it is alone", got)
	}

	if got := stuckPodOf([]kubePod{{Namespace: "chat", Owner: "ReplicaSet/other-5d8f"}}, w); got != -1 {
		t.Errorf("stuckPodOf = %d, want none", got)
	}
}

func TestIdleReadSurvivesAPanic(t *testing.T) {
	start := time.Now()

	if got := inBackground(func() []idleWorkload { panic("boom") })(); got != nil {
		t.Errorf("got %+v, want nil", got)
	}

	if time.Since(start) >= idleGrace {
		t.Error("a panic waited out the grace")
	}
}

func TestKubeInventoryKeepsTheKindItMayList(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/pods": `{"items":[]}`,
		"GET /apis/apps/v1/deployments": `{"items":[
		  {"metadata":{"name":"paperless-ai","namespace":"docs"},"spec":{"replicas":0,"template":{"spec":{"containers":[{"image":"clusterzx/paperless-ai:3.0.9"}]}}},"status":{}}
		]}`,
	}) // statefulsets: not found, as for a role that may not list them

	out, err := KubeInventory(f.kubeconfigFor(f.URL), "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	var inv inventory
	if err := json.Unmarshal([]byte(out), &inv); err != nil {
		t.Fatal(err)
	}

	if a := appsByID(inv.Apps)["paperless-ai"]; a.State != appStopped {
		t.Errorf("apps %+v, want paperless-ai stopped", inv.Apps)
	}
}
