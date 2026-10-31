package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

// The logic's own tests are in go/kube/flux; these go through what the facade adds to it.

func readFluxFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile("../kube/flux/testdata/flux/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
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

	if cilium := fluxAppByName(t, s, "cilium"); cilium.Icon != "cilium" {
		t.Fatalf("icon %q", cilium.Icon)
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

	if err := KubeFluxAction(cfg, "", "", "Kustomization", "flux-system", "apps", fluxActionReconcile); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}
