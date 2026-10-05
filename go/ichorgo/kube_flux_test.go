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

func readFluxFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/flux/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func fluxFixtureList[T any](t *testing.T, name string) []T {
	t.Helper()

	var list kubeList[T]
	if err := json.Unmarshal([]byte(readFluxFixture(t, name)), &list); err != nil {
		t.Fatal(err)
	}

	return list.Items
}

func fluxFixture(t *testing.T) fluxStatus {
	t.Helper()

	var srcs []fluxSourceObject

	for _, f := range []struct{ kind, file string }{{"GitRepository", "gitrepositories.json"}, {"HelmRepository", "helmrepositories.json"}} {
		for _, o := range fluxFixtureList[fluxSourceObject](t, f.file) {
			o.Kind = f.kind
			srcs = append(srcs, o)
		}
	}

	return mapFlux(fluxFixtureList[fluxKustomizationObject](t, "kustomizations.json"), fluxFixtureList[fluxHelmReleaseObject](t, "helmreleases.json"), srcs)
}

func fluxAppByName(t *testing.T, s fluxStatus, name string) fluxApp {
	t.Helper()

	for _, a := range s.Apps {
		if a.Name == name {
			return a
		}
	}

	t.Fatalf("no app %q", name)

	return fluxApp{}
}

func TestMapFluxSortsAndLevels(t *testing.T) {
	s := fluxFixture(t)

	var apps, sources []string
	for _, a := range s.Apps {
		apps = append(apps, a.Kind+"/"+a.Name+":"+a.Level)
	}

	for _, src := range s.Sources {
		sources = append(sources, src.Kind+"/"+src.Name+":"+src.Level)
	}

	wantApps := []string{"Kustomization/apps:critical", "HelmRelease/podinfo:critical", "HelmRelease/cilium:warning", "Kustomization/flux-system:ok", "Kustomization/infra:idle"}
	wantSources := []string{"HelmRepository/cilium:critical", "HelmRepository/bitnami:ok", "GitRepository/flux-system:ok"}

	if !equalJSON(t, apps, wantApps) || !equalJSON(t, sources, wantSources) {
		t.Fatal("order differs")
	}
}

func TestMapFluxKustomization(t *testing.T) {
	s := fluxFixture(t)

	root := fluxAppByName(t, s, "flux-system")
	if root.Pending || root.Owner != nil || root.SourceURL != "ssh://git@git.example.com/fleet.git" || !root.Prune {
		t.Fatalf("root %+v", root)
	}

	// The malformed inventory entry is skipped; cluster-scoped entries have no namespace.
	want := []fluxResource{{Kind: "Namespace", Name: "flux-system"}, {Group: "apps", Kind: "Deployment", Namespace: "flux-system", Name: "source-controller"}}
	if !equalJSON(t, root.Resources, want) {
		t.Fatal("inventory")
	}

	apps := fluxAppByName(t, s, "apps")
	if !apps.Pending || apps.Owner == nil || apps.Owner.Name != "flux-system" || !equalJSON(t, apps.DependsOn, []string{"flux-system/infra"}) {
		t.Fatalf("apps %+v", apps)
	}

	if apps.Revision != "main@sha1:9999" || apps.AttemptedRevision != "main@sha1:aaaa" || !equalJSON(t, apps.namespaces, []string{"web"}) {
		t.Fatalf("apps revision %+v", apps)
	}

	if infra := fluxAppByName(t, s, "infra"); !infra.Suspended {
		t.Fatalf("infra %+v", infra)
	}
}

func TestMapFluxHelmRelease(t *testing.T) {
	s := fluxFixture(t)

	cilium := fluxAppByName(t, s, "cilium")
	if !cilium.Reconciling || cilium.Chart != "cilium" || cilium.ChartVersion != "1.18.x" || cilium.Revision != "1.18.1" || cilium.AttemptedRevision != "1.18.2" {
		t.Fatalf("cilium %+v", cilium)
	}

	if cilium.Source == nil || cilium.Source.Name != "cilium" || cilium.SourceURL != "https://helm.cilium.io" || cilium.Icon != "cilium" {
		t.Fatalf("cilium source %+v %q", cilium.Source, cilium.Icon)
	}

	if len(cilium.History) != 2 || cilium.History[0].Version != 3 || cilium.History[0].DeployedAt == 0 {
		t.Fatalf("history %+v", cilium.History)
	}

	podinfo := fluxAppByName(t, s, "podinfo")
	if !podinfo.Stalled || podinfo.Failures != 3 || podinfo.Chart != "podinfo" || podinfo.Source.Kind != "OCIRepository" || !equalJSON(t, podinfo.namespaces, []string{"web"}) {
		t.Fatalf("podinfo %+v", podinfo)
	}

	for _, src := range s.Sources {
		if src.Kind == "GitRepository" && (src.Apps != 3 || src.Ref != "main" || src.Revision != "main@sha1:aaaa" || src.FetchedAt == 0) {
			t.Fatalf("git %+v", src)
		}
	}
}

func TestAttachFluxUnhealthyPods(t *testing.T) {
	apps := []fluxApp{
		{Name: "a", Level: healthCritical, UnhealthyPods: []kubePod{}, namespaces: []string{"web"}},
		{Name: "b", Level: healthOK, UnhealthyPods: []kubePod{}, namespaces: []string{"web"}},
	}

	attachFluxUnhealthyPods(apps, []kubePod{{Namespace: "web", Name: "web-1"}, {Namespace: "web", Name: "web-2", Healthy: true}})

	if len(apps[0].UnhealthyPods) != 1 || apps[0].UnhealthyPods[0].Name != "web-1" || len(apps[1].UnhealthyPods) != 0 {
		t.Fatalf("pods %+v", apps)
	}
}

func TestFluxPatch(t *testing.T) {
	const stamp = "2026-10-05T10:00:00Z"

	var active, suspended fluxTarget
	suspended.Spec.Suspend = true

	cases := []struct {
		kind, action string
		obj          fluxTarget
		want         string
		err          error
	}{
		{"Kustomization", fluxActionReconcile, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"GitRepository", fluxActionReconcile, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"Kustomization", fluxActionReconcile, suspended, "", errFluxSuspended},
		{"Kustomization", fluxActionSuspend, active, `{"spec":{"suspend":true}}`, nil},
		{"HelmRelease", fluxActionResume, suspended, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}},"spec":{"suspend":null}}`, nil},
		{"HelmRelease", fluxActionForce, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/forceAt":"` + stamp + `","reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"HelmRelease", fluxActionReset, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `","reconcile.fluxcd.io/resetAt":"` + stamp + `"}}}`, nil},
		{"Kustomization", fluxActionForce, active, "", errFluxHelmOnly},
	}

	for _, c := range cases {
		p, err := fluxPatch(c.kind, c.obj, c.action, stamp)
		if !errors.Is(err, c.err) {
			t.Errorf("%s %s: err %v", c.kind, c.action, err)

			continue
		}

		if c.err != nil {
			continue
		}

		if got, _ := json.Marshal(p); string(got) != c.want {
			t.Errorf("%s %s: %s", c.kind, c.action, got)
		}
	}
}

func TestFluxSourceOf(t *testing.T) {
	var hr fluxTarget
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"cilium","namespace":"kube-system"},"spec":{"chart":{"spec":{"sourceRef":{"kind":"HelmRepository","name":"cilium","namespace":"flux-system"}}}}}`), &hr); err != nil {
		t.Fatal(err)
	}

	if got, err := fluxSourceOf("HelmRelease", hr); err != nil || got != (fluxRef{"HelmChart", "flux-system", "kube-system-cilium"}) {
		t.Fatalf("got %+v %v", got, err)
	}

	if _, err := fluxSourceOf("GitRepository", hr); !errors.Is(err, errFluxNoSource) {
		t.Fatalf("got %v", err)
	}
}

func TestFluxActionWithSourcePatchesBoth(t *testing.T) {
	const (
		ks  = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps"
		git = "/apis/source.toolkit.fluxcd.io/v1/namespaces/flux-system/gitrepositories/flux-system"
	)

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":    `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + ks:    `{"metadata":{"name":"apps","namespace":"flux-system","resourceVersion":"101"},"spec":{"sourceRef":{"kind":"GitRepository","name":"flux-system"}}}`,
		"GET " + git:   `{"metadata":{"name":"flux-system","namespace":"flux-system","resourceVersion":"300"},"spec":{}}`,
		"PATCH " + ks:  `{}`,
		"PATCH " + git: `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := fluxAction(context.Background(), k, "Kustomization", "flux-system", "apps", fluxActionReconcileWithSource, time.Now()); err != nil {
		t.Fatal(err)
	}

	var patches []string
	for _, r := range f.recorded() {
		if r.method == "PATCH" {
			if r.contentType != "application/merge-patch+json" || !strings.Contains(r.body, "requestedAt") {
				t.Fatalf("patch %+v", r)
			}

			patches = append(patches, r.path)
		}
	}

	if !equalJSON(t, patches, []string{git, ks}) {
		t.Fatal("source first, then the Kustomization")
	}
}

func TestReadFlux(t *testing.T) {
	const base = "/apis/source.toolkit.fluxcd.io/v1/"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/kustomize.toolkit.fluxcd.io/v1/kustomizations": readFluxFixture(t, "kustomizations.json"),
		"GET /apis/helm.toolkit.fluxcd.io/v2/helmreleases":        readFluxFixture(t, "helmreleases.json"),
		"GET " + base + "gitrepositories":                         readFluxFixture(t, "gitrepositories.json"),
		"GET " + base + "helmrepositories":                        readFluxFixture(t, "helmrepositories.json"),
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"kustomize-controller-1","namespace":"flux-system","labels":{"app":"kustomize-controller","app.kubernetes.io/version":"v2.7.0"}},
		   "spec":{"nodeName":"node-1","containers":[{"name":"manager","image":"ghcr.io/fluxcd/kustomize-controller:v1.7.0"}]},"status":{"phase":"Running"}}]}`,
		// Only the namespaces of apps in trouble are read, one by one.
		"GET /api/v1/namespaces/web/pods": `{"items":[
		  {"metadata":{"name":"web-1","namespace":"web"},"spec":{"nodeName":"node-3","containers":[{"name":"web","image":"web:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s, err := readFlux(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	// OCIRepositories and Buckets answer 404: none, no error.
	if !s.Installed || s.Version != "v2.7.0" || len(s.Apps) != 5 || len(s.Sources) != 3 || s.SourcesError != "" || s.HelmError != "" {
		t.Fatalf("status %+v", s)
	}

	if apps := fluxAppByName(t, s, "apps"); len(apps.UnhealthyPods) != 1 || apps.UnhealthyPods[0].Node != "node-3" {
		t.Fatalf("unhealthy pods %+v", apps.UnhealthyPods)
	}
}

func TestReadFluxNotInstalled(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	s, err := readFlux(context.Background(), k)
	if err != nil || s.Installed {
		t.Fatalf("got %+v %v", s, err)
	}

	out, _ := json.Marshal(s)
	if string(out) != `{"installed":false,"version":"","helmError":"","sourcesError":"","apps":[],"sources":[]}` {
		t.Fatalf("json %s", out)
	}
}

func TestKubeFluxActionValidates(t *testing.T) {
	if err := KubeFluxAction("", "", "", "Kustomization", "flux-system", "apps", "delete"); err == nil || !strings.Contains(err.Error(), "unsupported Flux action") {
		t.Fatalf("got %v", err)
	}

	if err := KubeFluxAction("", "", "", "Secret", "flux-system", "apps", fluxActionReconcile); err == nil || !strings.Contains(err.Error(), "unsupported Flux kind") {
		t.Fatalf("got %v", err)
	}

	if err := KubeFluxAction("", "", "", "Kustomization", "flux-system", "../x", fluxActionReconcile); err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestKubeFluxDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeFlux(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s fluxStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil || !s.Installed || len(s.Apps) < 5 || len(s.Sources) < 3 {
		t.Fatalf("demo: %v %s", err, out)
	}

	var reconciling, stalled, suspended, crashing bool
	for _, a := range s.Apps {
		reconciling = reconciling || a.Reconciling
		stalled = stalled || a.Stalled
		suspended = suspended || a.Suspended
		crashing = crashing || len(a.UnhealthyPods) > 0
	}

	if !reconciling || !stalled || !suspended || !crashing {
		t.Fatalf("demo states: reconciling %v stalled %v suspended %v crashing %v", reconciling, stalled, suspended, crashing)
	}

	if err := KubeFluxAction(cfg, "", "", "Kustomization", "flux-system", "apps", fluxActionReconcile); !errors.Is(err, demoUnavailable) {
		t.Fatalf("got %v", err)
	}
}
