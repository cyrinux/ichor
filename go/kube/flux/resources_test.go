package flux

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubetest"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("testdata/flux/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func fixtureList[T any](t *testing.T, name string) []T {
	t.Helper()

	var list kube.List[T]
	if err := json.Unmarshal([]byte(readFixture(t, name)), &list); err != nil {
		t.Fatal(err)
	}

	return list.Items
}

func fixture(t *testing.T) Status {
	t.Helper()

	var srcs []SourceObject

	for _, f := range []struct{ kind, file string }{{"GitRepository", "gitrepositories.json"}, {"HelmRepository", "helmrepositories.json"}} {
		for _, o := range fixtureList[SourceObject](t, f.file) {
			o.Kind = f.kind
			srcs = append(srcs, o)
		}
	}

	return mapStatus(fixtureList[KustomizationObject](t, "kustomizations.json"), fixtureList[HelmReleaseObject](t, "helmreleases.json"), srcs)
}

func appByName(t *testing.T, s Status, name string) App {
	t.Helper()

	for _, a := range s.Apps {
		if a.Name == name {
			return a
		}
	}

	t.Fatalf("no app %q", name)

	return App{}
}

func same(a, b any) bool { return reflect.DeepEqual(a, b) }

func TestMapStatusSortsAndLevels(t *testing.T) {
	s := fixture(t)

	var apps, sources []string
	for _, a := range s.Apps {
		apps = append(apps, a.Kind+"/"+a.Name+":"+a.Level)
	}

	for _, src := range s.Sources {
		sources = append(sources, src.Kind+"/"+src.Name+":"+src.Level)
	}

	wantApps := []string{"Kustomization/apps:critical", "HelmRelease/podinfo:critical", "HelmRelease/cilium:warning", "Kustomization/flux-system:ok", "Kustomization/infra:idle"}
	wantSources := []string{"HelmRepository/cilium:critical", "HelmRepository/bitnami:ok", "GitRepository/flux-system:ok"}

	if !same(apps, wantApps) || !same(sources, wantSources) {
		t.Fatalf("order differs: %v %v", apps, sources)
	}
}

func TestMapKustomization(t *testing.T) {
	s := fixture(t)

	root := appByName(t, s, "flux-system")
	if root.Pending || root.Owner != nil || root.SourceURL != "ssh://git@git.example.com/fleet.git" || !root.Prune {
		t.Fatalf("root %+v", root)
	}

	// The malformed inventory entry is skipped; cluster-scoped entries have no namespace.
	want := []Resource{{Kind: "Namespace", Name: "flux-system"}, {Group: "apps", Kind: "Deployment", Namespace: "flux-system", Name: "source-controller"}}
	if !same(root.Resources, want) {
		t.Fatalf("inventory %+v", root.Resources)
	}

	apps := appByName(t, s, "apps")
	if !apps.Pending || apps.Owner == nil || apps.Owner.Name != "flux-system" || !same(apps.DependsOn, []string{"flux-system/infra"}) {
		t.Fatalf("apps %+v", apps)
	}

	if apps.Revision != "main@sha1:9999" || apps.AttemptedRevision != "main@sha1:aaaa" || !same(apps.namespaces, []string{"web"}) {
		t.Fatalf("apps revision %+v", apps)
	}

	if infra := appByName(t, s, "infra"); !infra.Suspended {
		t.Fatalf("infra %+v", infra)
	}
}

func TestMapHelmRelease(t *testing.T) {
	s := fixture(t)

	cilium := appByName(t, s, "cilium")
	if !cilium.Reconciling || cilium.Chart != "cilium" || cilium.ChartVersion != "1.18.x" || cilium.Revision != "1.18.1" || cilium.AttemptedRevision != "1.18.2" {
		t.Fatalf("cilium %+v", cilium)
	}

	if cilium.Source == nil || cilium.Source.Name != "cilium" || cilium.SourceURL != "https://helm.cilium.io" {
		t.Fatalf("cilium source %+v", cilium.Source)
	}

	if len(cilium.History) != 2 || cilium.History[0].Version != 3 || cilium.History[0].DeployedAt == 0 {
		t.Fatalf("history %+v", cilium.History)
	}

	// Drift as the helm-controller found it; the release's health stays its own.
	if cilium.DriftMode != "warn" || !cilium.Drifted || !strings.HasPrefix(cilium.DriftMessage, "DaemonSet/kube-system/cilium changed") ||
		cilium.DriftAt != time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC).UnixMilli() || cilium.Level != kube.HealthWarning {
		t.Fatalf("cilium drift %+v", cilium)
	}

	podinfo := appByName(t, s, "podinfo")
	if podinfo.DriftMode != "" || podinfo.Drifted || podinfo.DriftAt != 0 {
		t.Fatalf("podinfo drift %+v", podinfo)
	}

	if !podinfo.Stalled || podinfo.Failures != 3 || podinfo.Chart != "podinfo" || podinfo.Source.Kind != "OCIRepository" || !same(podinfo.namespaces, []string{"web"}) {
		t.Fatalf("podinfo %+v", podinfo)
	}

	for _, src := range s.Sources {
		if src.Kind == "GitRepository" && (src.Apps != 3 || src.Ref != "main" || src.Revision != "main@sha1:aaaa" || src.FetchedAt == 0) {
			t.Fatalf("git %+v", src)
		}
	}
}

func TestAttachUnhealthyPods(t *testing.T) {
	apps := []App{
		{Name: "a", Level: kube.HealthCritical, UnhealthyPods: []kube.Pod{}, namespaces: []string{"web"}},
		{Name: "b", Level: kube.HealthOK, UnhealthyPods: []kube.Pod{}, namespaces: []string{"web"}},
	}

	attachUnhealthyPods(apps, []kube.Pod{{Namespace: "web", Name: "web-1"}, {Namespace: "web", Name: "web-2", Healthy: true}})

	if len(apps[0].UnhealthyPods) != 1 || apps[0].UnhealthyPods[0].Name != "web-1" || len(apps[1].UnhealthyPods) != 0 {
		t.Fatalf("pods %+v", apps)
	}
}

func TestRead(t *testing.T) {
	const base = "/apis/source.toolkit.fluxcd.io/v1/"

	f := kubetest.New(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/kustomize.toolkit.fluxcd.io/v1/kustomizations": readFixture(t, "kustomizations.json"),
		"GET /apis/helm.toolkit.fluxcd.io/v2/helmreleases":        readFixture(t, "helmreleases.json"),
		"GET " + base + "gitrepositories":                         readFixture(t, "gitrepositories.json"),
		"GET " + base + "helmrepositories":                        readFixture(t, "helmrepositories.json"),
	})

	var asked []string

	s, err := Read(context.Background(), Env{
		Client:  f.KubeClient(t),
		Version: func(context.Context) string { return "v2.7.0" },
		UnhealthyPods: func(_ context.Context, namespaces []string) ([]kube.Pod, error) {
			asked = namespaces

			return []kube.Pod{{Namespace: "web", Name: "web-1", Node: "node-3"}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// OCIRepositories and Buckets answer 404: none, no error.
	if !s.Installed || s.Version != "v2.7.0" || len(s.Apps) != 5 || len(s.Sources) != 3 || s.SourcesError != "" || s.HelmError != "" {
		t.Fatalf("status %+v", s)
	}

	if apps := appByName(t, s, "apps"); len(apps.UnhealthyPods) != 1 || apps.UnhealthyPods[0].Node != "node-3" {
		t.Fatalf("unhealthy pods %+v", apps.UnhealthyPods)
	}

	if len(asked) == 0 {
		t.Fatal("the namespaces of the apps in trouble were not read")
	}
}

func TestReadASectionThatFailsOnlySaysSo(t *testing.T) {
	f := kubetest.New(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /apis/kustomize.toolkit.fluxcd.io/v1/kustomizations": `{"items":[]}`,
	})
	f.AnswerWith("GET /apis/helm.toolkit.fluxcd.io/v2/helmreleases", 403, `{"kind":"Status","reason":"Forbidden","message":"no"}`)

	s, err := Read(context.Background(), Env{Client: f.KubeClient(t), Error: func(error) error { return errors.New("sign in") }})
	if err != nil {
		t.Fatal(err)
	}

	if !s.Installed || s.HelmError != "sign in" {
		t.Fatalf("status %+v", s)
	}
}

func TestReadNotInstalled(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`})

	s, err := Read(context.Background(), Env{Client: f.KubeClient(t)})
	if err != nil || s.Installed {
		t.Fatalf("got %+v %v", s, err)
	}

	out, _ := json.Marshal(s)
	if string(out) != `{"installed":false,"version":"","helmError":"","sourcesError":"","apps":[],"sources":[]}` {
		t.Fatalf("json %s", out)
	}
}
