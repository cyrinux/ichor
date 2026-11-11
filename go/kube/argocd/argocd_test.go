package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubetest"
)

func readArgoFixture[T any](t *testing.T, name string) []T {
	t.Helper()

	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}

	var list kube.List[T]
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}

	return list.Items
}

func argoFixture(t *testing.T) Status {
	t.Helper()

	return Map(
		readArgoFixture[Object](t, "applications.json"),
		readArgoFixture[argoAppSetObject](t, "applicationsets.json"),
		readArgoFixture[ProjectObject](t, "appprojects.json"),
		argoTestNow,
	)
}

func argoAppByName(t *testing.T, s Status, name string) App {
	t.Helper()

	for _, a := range s.Apps {
		if a.Name == name {
			return a
		}
	}

	t.Fatalf("no app %q", name)

	return App{}
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

	cases := map[string]*Owner{
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

	if !a.AutoSync.Enabled || !a.AutoSync.Prune || a.AutoSync.SelfHeal {
		t.Fatalf("auto %+v", a.AutoSync)
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
	apps := []App{
		{Name: "web", Level: kube.HealthCritical, Destination: Dest{Namespace: "web"}},
		{Name: "ok", Level: kube.HealthOK, Destination: Dest{Namespace: "web"}},
	}

	attachUnhealthyPods(apps, []kube.Pod{
		{Namespace: "web", Name: "web-1", Status: "CrashLoopBackOff", Node: "node-3"},
		{Namespace: "web", Name: "web-2", Status: "Running", Healthy: true},
		{Namespace: "db", Name: "db-0", Status: "Pending"},
	})

	if len(apps[0].UnhealthyPods) != 1 || apps[0].UnhealthyPods[0].Node != "node-3" || apps[1].UnhealthyPods != nil {
		t.Fatalf("apps %+v", apps)
	}
}

func argoObjectByName(t *testing.T, name string) Object {
	t.Helper()

	for _, o := range readArgoFixture[Object](t, "applications.json") {
		if o.Metadata.Name == name {
			return o
		}
	}

	t.Fatalf("no app %q", name)

	return Object{}
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

	p, err := argoPatch(argoObjectByName(t, "apps"), nil, argoActionHardRefresh, SyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"argocd.argoproj.io/refresh":"hard"}}}` {
		t.Fatalf("got %s %v", patchJSON(t, p), err)
	}
}

func TestArgoPatchSync(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	// A running operation is never overwritten.
	if _, err := argoPatch(argoObjectByName(t, "cilium"), nil, ActionSync, SyncOptions{}, now); !errors.Is(err, errArgoBusy) {
		t.Fatalf("got %v", err)
	}

	opts := SyncOptions{Prune: true, ServerSideApply: true, ApplyOutOfSyncOnly: true,
		Resources: []argoResourceRef{{Group: "apps", Kind: "Deployment", Namespace: "web", Name: "web"}}}

	p, err := argoPatch(argoObjectByName(t, "web"), nil, ActionSync, opts, now)
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
	p, _ = argoPatch(argoObjectByName(t, "legacy"), nil, ActionSync, SyncOptions{}, now)
	if got := patchJSON(t, p); !strings.Contains(got, `"revision":"main"`) || strings.Contains(got, "syncOptions") {
		t.Errorf("legacy patch %s", got)
	}

	// A requested operation not picked up yet counts as running.
	pending := argoObjectByName(t, "legacy")
	pending.Operation = json.RawMessage(`{"sync":{}}`)

	if _, err := argoPatch(pending, nil, ActionSync, SyncOptions{}, now); !errors.Is(err, errArgoBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoPatchTerminate(t *testing.T) {
	now := time.Now()

	p, err := argoPatch(argoObjectByName(t, "cilium"), nil, ActionTerminate, SyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"status":{"operationState":{"phase":"Terminating"}}}` {
		t.Fatalf("got %v %v", p, err)
	}

	if _, err := argoPatch(argoObjectByName(t, "apps"), nil, ActionTerminate, SyncOptions{}, now); !errors.Is(err, errArgoNotBusy) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoPatchAutoSync(t *testing.T) {
	now := time.Now()

	// Owned apps keep their spec: the owner would revert it.
	for _, name := range []string{"cilium", "web", "legacy"} {
		o := argoObjectByName(t, name)
		if _, err := argoPatch(o, argoOwnerOf(o, map[string]bool{"argocd/apps": true}), argoActionAutoSyncOff, SyncOptions{}, now); err == nil || !strings.Contains(err.Error(), "managed by") {
			t.Fatalf("%s: got %v", name, err)
		}
	}

	app := argoObjectByName(t, "apps")

	p, err := argoPatch(app, nil, argoActionAutoSyncOff, SyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"ichor.levis.name/paused-automated":"{\"prune\":true,\"selfHeal\":true}"}},"spec":{"syncPolicy":{"automated":null}}}` {
		t.Fatalf("off: %s %v", patchJSON(t, p), err)
	}

	// Resuming restores what was paused.
	app.Spec.SyncPolicy.Automated = nil
	app.Metadata.Annotations = map[string]string{argoPausedAnnotation: `{"prune":true,"selfHeal":true}`}

	p, err = argoPatch(app, nil, argoActionAutoSyncOn, SyncOptions{}, now)
	if err != nil || patchJSON(t, p) != `{"metadata":{"annotations":{"ichor.levis.name/paused-automated":null}},"spec":{"syncPolicy":{"automated":{"prune":true,"selfHeal":true}}}}` {
		t.Fatalf("on: %s %v", patchJSON(t, p), err)
	}
}

func TestArgoPatchRollback(t *testing.T) {
	now := time.Now()

	app := argoObjectByName(t, "apps")
	if _, err := argoPatch(app, nil, argoActionRollback, SyncOptions{HistoryID: 1}, now); !errors.Is(err, errArgoAutoSync) {
		t.Fatalf("auto-sync on: got %v", err)
	}

	app.Spec.SyncPolicy = nil
	app.Status.OperationState = nil
	app.Status.History = []argoHistoryObject{{ID: 4, Revision: "abc", Source: json.RawMessage(`{"repoURL":"https://git.example.com/gitops.git","path":"apps","targetRevision":"main"}`)}}

	if _, err := argoPatch(app, nil, argoActionRollback, SyncOptions{HistoryID: 9}, now); !errors.Is(err, errArgoHistory) {
		t.Fatalf("unknown id: got %v", err)
	}

	p, err := argoPatch(app, nil, argoActionRollback, SyncOptions{HistoryID: 4, Resources: []argoResourceRef{{Kind: "x"}}}, now)
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

	apps, err := os.ReadFile("testdata/applications.json")
	if err != nil {
		t.Fatal(err)
	}

	var list struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(apps, &list); err != nil {
		t.Fatal(err)
	}

	f := kubetest.New(t, map[string]string{
		"GET /apis":     `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET " + path:   string(list.Items[3]),
		"PATCH " + path: `{}`,
	})

	k := f.KubeClient(t)

	if err := Act(context.Background(), k, "argocd", "legacy", argoActionRefresh, SyncOptions{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	reqs := f.Recorded()
	last := reqs[len(reqs)-1]

	if last.Method != "PATCH" || last.ContentType != "application/merge-patch+json" || !strings.Contains(last.Body, `"resourceVersion":"400"`) {
		t.Fatalf("patch %+v", last)
	}
}

func TestReadArgoCDNotInstalled(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`})

	k := f.KubeClient(t)

	s, err := Read(context.Background(), Env{Client: k})
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
	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`})

	k := f.KubeClient(t)

	if s, err := Read(context.Background(), Env{Client: k}); err != nil || s.Installed {
		t.Fatalf("got %+v %v", s, err)
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

		f := kubetest.New(t, answers)

		k := f.KubeClient(t)

		err := Act(context.Background(), k, "argocd", "legacy", argoActionAutoSyncOff, SyncOptions{}, time.Now())

		switch {
		case parentExists && (err == nil || !strings.Contains(err.Error(), "managed by Application apps")):
			t.Fatalf("parent exists: got %v", err)
		case !parentExists && err != nil:
			t.Fatalf("no parent (a Helm release name): got %v", err)
		}
	}
}

func TestArgoOpSelectiveAndOutOfSyncOnly(t *testing.T) {
	resources := []Resource{
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
