package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func readArgoFixture[T any](t *testing.T, name string) []T {
	t.Helper()

	data, err := os.ReadFile("testdata/argocd/" + name)
	if err != nil {
		t.Fatal(err)
	}

	var list kubeList[T]
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}

	return list.Items
}

func argoFixture(t *testing.T) argoStatus {
	t.Helper()

	return mapArgoCD(
		readArgoFixture[argoObject](t, "applications.json"),
		readArgoFixture[argoAppSetObject](t, "applicationsets.json"),
		readArgoFixture[argoProjectObject](t, "appprojects.json"),
	)
}

func argoAppByName(t *testing.T, s argoStatus, name string) argoApp {
	t.Helper()

	for _, a := range s.Apps {
		if a.Name == name {
			return a
		}
	}

	t.Fatalf("no app %q", name)

	return argoApp{}
}

func TestMapArgoCDSortsAndLevels(t *testing.T) {
	s := argoFixture(t)

	var got []string
	for _, a := range s.Apps {
		got = append(got, a.Name+":"+a.Level)
	}

	want := []string{"web:critical", "cilium:warning", "legacy:warning", "apps:ok"}
	if !equalJSON(t, got, want) {
		t.Fatal("apps differ")
	}
}

func TestMapArgoAppOwners(t *testing.T) {
	s := argoFixture(t)

	cases := map[string]*argoOwner{
		"cilium": {Kind: "ApplicationSet", Name: "infra"},
		"web":    {Kind: "Application", Name: "apps"}, // tracking id "argocd_apps"
		"legacy": {Kind: "Application", Name: "apps"}, // instance label
		"apps":   nil,
	}

	for name, want := range cases {
		if got := argoAppByName(t, s, name).Owner; !equalJSON(t, got, want) {
			t.Errorf("%s owner", name)
		}
	}
}

func TestMapArgoAppRunningOperation(t *testing.T) {
	a := argoAppByName(t, argoFixture(t), "cilium")

	op := a.Operation
	if op == nil || op.Phase != "Running" || op.InitiatedBy != "automated" || op.Revision != "1.18.2" {
		t.Fatalf("operation %+v", op)
	}

	// 4 resources plus the PreSync hook; the CRD, the ConfigMap and the hook are done; wave 1 runs.
	if op.Done != 3 || op.Total != 5 || op.Wave != 1 || !equalJSON(t, op.Waves, []int{-1, 0, 1}) {
		t.Fatalf("progress %d/%d wave %d waves %v", op.Done, op.Total, op.Wave, op.Waves)
	}

	if a.Resources[0].Wave != -1 || a.Resources[0].SyncResult != "Synced" || a.Resources[3].Health != "Progressing" && a.Resources[2].Health != "Progressing" {
		t.Fatalf("resources %+v", a.Resources)
	}

	if a.Icon != "cilium" || !a.AutoSync.Enabled || !a.AutoSync.Prune || a.AutoSync.SelfHeal {
		t.Fatalf("icon %q auto %+v", a.Icon, a.AutoSync)
	}

	if len(a.History) != 1 || a.History[0].Chart != "cilium" || a.History[0].TargetRevision != "1.18.1" || a.History[0].InitiatedBy != "automated" {
		t.Fatalf("history %+v", a.History)
	}
}

func TestMapArgoAppFailedMultiSource(t *testing.T) {
	a := argoAppByName(t, argoFixture(t), "web")

	if a.Revision != "bbbbbbb2222222" || len(a.Sources) != 2 || a.Sources[1].Chart != "redis" || a.AutoSync.Enabled {
		t.Fatalf("app %+v", a)
	}

	op := a.Operation
	if op.Phase != "Failed" || op.InitiatedBy != "alice" || op.RetryCount != 2 || len(op.Failed) != 1 || op.Failed[0].Message != "image: Required value" {
		t.Fatalf("operation %+v", op)
	}

	// Newest first, the first source's revision.
	if len(a.History) != 2 || a.History[0].ID != 2 || a.History[0].Revision != "0000002" {
		t.Fatalf("history %+v", a.History)
	}
}

func TestMapArgoAppPausedAndPrune(t *testing.T) {
	a := argoAppByName(t, argoFixture(t), "legacy")

	if a.AutoSync.Enabled || !a.AutoSync.Prune || a.Operation != nil || !a.Resources[0].Prune {
		t.Fatalf("app %+v", a)
	}
}

func TestMapArgoAppSetsAndProjects(t *testing.T) {
	s := argoFixture(t)

	got := []string{}
	for _, set := range s.AppSets {
		got = append(got, set.Name+":"+set.Level)
	}

	if !equalJSON(t, got, []string{"broken:critical", "infra:warning"}) || s.AppSets[1].Apps != 1 {
		t.Fatalf("appsets %+v", s.AppSets)
	}

	if len(s.Projects) != 2 || s.Projects[1].Name != "infra" || s.Projects[1].SyncWindows != 1 {
		t.Fatalf("projects %+v", s.Projects)
	}
}

func TestAttachUnhealthyPods(t *testing.T) {
	apps := []argoApp{
		{Name: "web", Level: healthCritical, Destination: argoDest{Namespace: "web"}},
		{Name: "ok", Level: healthOK, Destination: argoDest{Namespace: "web"}},
	}

	attachUnhealthyPods(apps, []kubePod{
		{Namespace: "web", Name: "web-1", Status: "CrashLoopBackOff", Node: "node-3"},
		{Namespace: "web", Name: "web-2", Status: "Running", Healthy: true},
		{Namespace: "db", Name: "db-0", Status: "Pending"},
	})

	if len(apps[0].UnhealthyPods) != 1 || apps[0].UnhealthyPods[0].Node != "node-3" || apps[1].UnhealthyPods != nil {
		t.Fatalf("apps %+v", apps)
	}
}

func argoObjectByName(t *testing.T, name string) argoObject {
	t.Helper()

	for _, o := range readArgoFixture[argoObject](t, "applications.json") {
		if o.Metadata.Name == name {
			return o
		}
	}

	t.Fatalf("no app %q", name)

	return argoObject{}
}

func patchJSON(t *testing.T, patch map[string]any) string {
	t.Helper()

	data, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestArgoPatchRefresh(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	p, err := argoPatch(argoObjectByName(t, "apps"), nil, argoActionHardRefresh, argoSyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}` {
		t.Fatalf("got %s %v", patchJSON(t, p), err)
	}
}

func TestArgoPatchSync(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	// A running operation is never overwritten.
	if _, err := argoPatch(argoObjectByName(t, "cilium"), nil, argoActionSync, argoSyncOptions{}, now); !errors.Is(err, errArgoBusy) {
		t.Fatalf("got %v", err)
	}

	opts := argoSyncOptions{Prune: true, ServerSideApply: true, ApplyOutOfSyncOnly: true,
		Resources: []argoResourceRef{{Group: "apps", Kind: "Deployment", Namespace: "web", Name: "web"}}}

	p, err := argoPatch(argoObjectByName(t, "web"), nil, argoActionSync, opts, now)
	if err != nil {
		t.Fatal(err)
	}

	got := patchJSON(t, p)
	for _, want := range []string{
		`"revisions":["main","20.1.0"]`, `"prune":true`, `"dryRun":false`,
		`"syncOptions":["CreateNamespace=true","ApplyOutOfSyncOnly=true","ServerSideApply=true"]`,
		`"resources":[{"group":"apps","kind":"Deployment","namespace":"web","name":"web"}]`,
		`"initiatedBy":{"username":"ichor"}`, `"syncStrategy":{"hook":{"force":false}}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("patch lacks %s: %s", want, got)
		}
	}

	// A single source syncs to its target revision.
	p, _ = argoPatch(argoObjectByName(t, "legacy"), nil, argoActionSync, argoSyncOptions{}, now)
	if got := patchJSON(t, p); !strings.Contains(got, `"revision":"main"`) || strings.Contains(got, "syncOptions") {
		t.Errorf("legacy patch %s", got)
	}

	// A requested operation not picked up yet counts as running.
	pending := argoObjectByName(t, "legacy")
	pending.Operation = json.RawMessage(`{"sync":{}}`)

	if _, err := argoPatch(pending, nil, argoActionSync, argoSyncOptions{}, now); !errors.Is(err, errArgoBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoPatchTerminate(t *testing.T) {
	now := time.Now()

	p, err := argoPatch(argoObjectByName(t, "cilium"), nil, argoActionTerminate, argoSyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"status":{"operationState":{"phase":"Terminating"}}}` {
		t.Fatalf("got %v %v", p, err)
	}

	if _, err := argoPatch(argoObjectByName(t, "apps"), nil, argoActionTerminate, argoSyncOptions{}, now); !errors.Is(err, errArgoNotBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoPatchAutoSync(t *testing.T) {
	now := time.Now()

	// Owned apps keep their spec: the owner would revert it.
	for _, name := range []string{"cilium", "web", "legacy"} {
		o := argoObjectByName(t, name)
		if _, err := argoPatch(o, argoOwnerOf(o, map[string]bool{"argocd/apps": true}), argoActionAutoSyncOff, argoSyncOptions{}, now); err == nil || !strings.Contains(err.Error(), "managed by") {
			t.Fatalf("%s: got %v", name, err)
		}
	}

	app := argoObjectByName(t, "apps")

	p, err := argoPatch(app, nil, argoActionAutoSyncOff, argoSyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"ichor.levis.name/paused-automated":"{\"prune\":true,\"selfHeal\":true}"}},"spec":{"syncPolicy":{"automated":null}}}` {
		t.Fatalf("off: %s %v", patchJSON(t, p), err)
	}

	// Resuming restores what was paused.
	app.Spec.SyncPolicy.Automated = nil
	app.Metadata.Annotations = map[string]string{argoPausedAnnotation: `{"prune":true,"selfHeal":true}`}

	p, err = argoPatch(app, nil, argoActionAutoSyncOn, argoSyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"ichor.levis.name/paused-automated":null}},"spec":{"syncPolicy":{"automated":{"prune":true,"selfHeal":true}}}}` {
		t.Fatalf("on: %s %v", patchJSON(t, p), err)
	}
}

func TestArgoPatchRollback(t *testing.T) {
	now := time.Now()

	app := argoObjectByName(t, "apps")
	if _, err := argoPatch(app, nil, argoActionRollback, argoSyncOptions{HistoryID: 1}, now); !errors.Is(err, errArgoAutoSync) {
		t.Fatalf("auto-sync on: got %v", err)
	}

	app.Spec.SyncPolicy = nil
	app.Status.OperationState = nil
	app.Status.History = []argoHistoryObject{{ID: 4, Revision: "abc", Source: json.RawMessage(`{"repoURL":"https://git.example.com/gitops.git","path":"apps","targetRevision":"main"}`)}}

	if _, err := argoPatch(app, nil, argoActionRollback, argoSyncOptions{HistoryID: 9}, now); !errors.Is(err, errArgoHistory) {
		t.Fatalf("unknown id: got %v", err)
	}

	p, err := argoPatch(app, nil, argoActionRollback, argoSyncOptions{HistoryID: 4, Resources: []argoResourceRef{{Kind: "x"}}}, now)
	if err != nil {
		t.Fatal(err)
	}

	got := patchJSON(t, p)
	if !strings.Contains(got, `"revision":"abc"`) || !strings.Contains(got, `"source":{"repoURL":"https://git.example.com/gitops.git"`) || strings.Contains(got, `"resources"`) {
		t.Fatalf("rollback patch %s", got)
	}
}

func TestArgoActionPatchesWithResourceVersion(t *testing.T) {
	const path = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/legacy"

	apps, err := os.ReadFile("testdata/argocd/applications.json")
	if err != nil {
		t.Fatal(err)
	}

	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(apps, &list); err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":     `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET " + path:   string(list.Items[3]),
		"PATCH " + path: `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := argoAction(context.Background(), k, "argocd", "legacy", argoActionRefresh, argoSyncOptions{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	reqs := f.recorded()
	last := reqs[len(reqs)-1]

	if last.method != "PATCH" || last.contentType != "application/merge-patch+json" || !strings.Contains(last.body, `"resourceVersion":"400"`) {
		t.Fatalf("patch %+v", last)
	}
}

func TestReadArgoCD(t *testing.T) {
	apps, _ := os.ReadFile("testdata/argocd/applications.json")
	sets, _ := os.ReadFile("testdata/argocd/applicationsets.json")

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/argoproj.io/v1alpha1/applications":    string(apps),
		"GET /apis/argoproj.io/v1alpha1/applicationsets": string(sets),
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"argocd-application-controller-0","namespace":"argocd","labels":{"app.kubernetes.io/name":"argocd-application-controller"}},
		   "spec":{"nodeName":"node-1","containers":[{"name":"c","image":"quay.io/argoproj/argocd:v3.4.5"}]},"status":{"phase":"Running"}}]}`,
		// Only the namespaces of apps in trouble are read, one by one.
		"GET /api/v1/namespaces/web/pods": `{"items":[
		  {"metadata":{"name":"web-1","namespace":"web"},"spec":{"nodeName":"node-3","containers":[{"name":"web","image":"web:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s, err := readArgoCD(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	// The projects listing is missing (404): no error, just none.
	if !s.Installed || s.Version != "v3.4.5" || len(s.Apps) != 4 || len(s.AppSets) != 2 || s.AppSetsError != "" || len(s.Projects) != 0 {
		t.Fatalf("status %+v", s)
	}

	web := argoAppByName(t, s, "web")
	if len(web.UnhealthyPods) != 1 || web.UnhealthyPods[0].Status != "CrashLoopBackOff" || web.UnhealthyPods[0].Node != "node-3" {
		t.Fatalf("unhealthy pods %+v", web.UnhealthyPods)
	}
}

func TestReadArgoCDNotInstalled(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s, err := readArgoCD(context.Background(), k)
	if err != nil || s.Installed || s.Apps == nil {
		t.Fatalf("got %+v %v", s, err)
	}

	out, _ := json.Marshal(s)
	if string(out) != `{"installed":false,"version":"","appSetsError":"","apps":[],"appSets":[],"projects":[]}` {
		t.Fatalf("json %s", out)
	}
}

func TestReadArgoCDRolloutsOnly(t *testing.T) {
	// Argo Rollouts shares the argoproj.io group but serves no Applications.
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if s, err := readArgoCD(context.Background(), k); err != nil || s.Installed {
		t.Fatalf("got %+v %v", s, err)
	}
}

func TestKubeArgoActionValidates(t *testing.T) {
	if err := KubeArgoAction("", "", "", "argocd", "web", "delete", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}

	if err := KubeArgoAction("", "", "", "argocd", "web", argoActionSync, "{"); err == nil || !strings.Contains(err.Error(), "bad sync options") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeArgoDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoCD(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s argoStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil || !s.Installed || len(s.Apps) < 5 {
		t.Fatalf("demo: %v %s", err, out)
	}

	// The demo shows a running sync, a failure and a degraded app with its crashing pod.
	var running, failed, crashing bool
	for _, a := range s.Apps {
		running = running || a.Operation != nil && a.Operation.Phase == "Running"
		failed = failed || a.Operation != nil && a.Operation.Phase == "Failed"
		crashing = crashing || len(a.UnhealthyPods) > 0
	}

	if !running || !failed || !crashing {
		t.Fatalf("demo states: running %v failed %v crashing %v", running, failed, crashing)
	}

	if err := KubeArgoAction(cfg, "", "", "argocd", "cilium", argoActionSync, ""); !errors.Is(err, demoUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoActionOwnerNeedsAnExistingParent(t *testing.T) {
	const (
		base   = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/"
		groups = `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`
	)

	// legacy's instance label names "apps": owned while that Application exists.
	legacy := `{"metadata":{"name":"legacy","namespace":"argocd","resourceVersion":"400","labels":{"app.kubernetes.io/instance":"apps"}},
	  "spec":{"syncPolicy":{"automated":{"prune":true}}}}`

	for _, parentExists := range []bool{true, false} {
		answers := map[string]string{"GET /apis": groups, "GET " + base + "legacy": legacy, "PATCH " + base + "legacy": `{}`}
		if parentExists {
			answers["GET "+base+"apps"] = `{"metadata":{"name":"apps"}}`
		}

		f := newFakeKubeAPI(t, answers)

		k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
		if err != nil {
			t.Fatal(err)
		}

		err = argoAction(context.Background(), k, "argocd", "legacy", argoActionAutoSyncOff, argoSyncOptions{}, time.Now())

		switch {
		case parentExists && (err == nil || !strings.Contains(err.Error(), "managed by Application apps")):
			t.Fatalf("parent exists: got %v", err)
		case !parentExists && err != nil:
			t.Fatalf("no parent (a Helm release name): got %v", err)
		}
	}
}

func TestArgoOpSelectiveAndOutOfSyncOnly(t *testing.T) {
	resources := []argoResource{
		{Kind: "ConfigMap", Name: "a", Sync: "Synced"},                     // in sync, left alone
		{Kind: "ConfigMap", Name: "b", Sync: "OutOfSync", Wave: 1},         // not selected
		{Kind: "Deployment", Name: "c", Sync: "OutOfSync", Wave: 2},        // selected, running
		{Kind: "Service", Name: "d", Sync: "Synced", SyncResult: "Synced"}, // selected, done
	}

	var op argoOperationState
	if err := json.Unmarshal([]byte(`{"phase":"Running","operation":{"sync":{"resources":[
	  {"kind":"Deployment","name":"c"},{"kind":"Service","name":"d"}]}}}`), &op); err != nil {
		t.Fatal(err)
	}

	got := argoOp(&op, resources, map[argoKey]argoResultEntry{})
	if got.Done != 1 || got.Total != 2 || got.Wave != 2 {
		t.Fatalf("selective: %d/%d wave %d", got.Done, got.Total, got.Wave)
	}

	op.Operation.Sync.Resources = nil

	got = argoOp(&op, resources, map[argoKey]argoResultEntry{})
	if got.Done != 2 || got.Total != 4 || got.Wave != 1 {
		t.Fatalf("whole app: %d/%d wave %d", got.Done, got.Total, got.Wave)
	}
}
