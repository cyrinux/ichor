package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// updateEditTestEnv is a cluster with three apps: the Flux Kustomization apps (path apps/,
// a kustomize images: override for api), the Argo CD Application shop (two sources: a chart
// and a values ref) and the parent Application root that declares shop.
func updateEditTestEnv() updateEditEnv {
	files := map[string][]byte{
		"apps/web/deployment.yaml":    []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          image: ghcr.io/example-org/web:1.2.0\n"),
		"apps/api/deployment.yaml":    []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          image: ghcr.io/example-org/api:2.0.0\n"),
		"apps/api/kustomization.yaml": []byte("resources:\n  - deployment.yaml\nimages:\n  - name: ghcr.io/example-org/api\n    newTag: 2.0.0\n"),
		"apps/releases/app.yaml":      []byte("apiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: app\n  namespace: demo\nspec:\n  chart:\n    spec:\n      chart: app\n      version: 1.0.0\n"),
		"root/shop.yaml":              []byte("apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: shop\n  namespace: argocd\nspec:\n  source:\n    chart: shop\n    targetRevision: 1.0.0\n"),
	}
	git := gitopsSource{RepoURL: "https://git.example.test/example-org/fleet.git", Path: "apps", Kind: gitopsKindKustomize, Browsable: true}
	root := gitopsSource{RepoURL: git.RepoURL, Path: "root", Kind: gitopsKindDirectory, Browsable: true}

	return updateEditEnv{
		sources: func(_ context.Context, app updateEditApp) (gitopsSources, error) {
			switch app.Name {
			case "apps":
				return gitopsSources{Sources: []gitopsSource{git}}, nil
			case "root":
				return gitopsSources{Sources: []gitopsSource{root}}, nil
			case "shop":
				return gitopsSources{Sources: []gitopsSource{
					{RepoURL: "https://charts.example.test", Kind: gitopsKindChart, Chart: "shop", ValueFiles: []string{"$values/shop/values.yaml"}},
					{RepoURL: git.RepoURL, Kind: gitopsKindValues, Browsable: true},
				}}, nil
			}

			return gitopsSources{}, errors.New("not found")
		},
		files: func(_ context.Context, src gitopsSource) (sourceFiles, error) {
			return treeFilesUnder(files, src.Path), nil
		},
		meta: func(_ context.Context, tool, _, namespace, name string) (fluxMeta, error) {
			if tool == "argocd" {
				return fluxMeta{Name: name, Namespace: namespace, Annotations: map[string]string{argoTrackingID: "root:argoproj.io/Application:argocd/" + name}}, nil
			}

			if name == "lost" {
				return fluxMeta{Name: name, Namespace: namespace}, nil
			}

			return fluxMeta{Name: name, Namespace: namespace, Labels: map[string]string{fluxKustomizeNameLabel: "apps", fluxKustomizeNsLabel: "flux-system"}}, nil
		},
	}
}

func TestUpdateEditTarget(t *testing.T) {
	apps := &updateOwner{Tool: "flux", Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}
	workload := func(name string) []updateWorkloadRef {
		return []updateWorkloadRef{{Kind: "Deployment", Namespace: "demo", Name: name, Containers: []string{name}}}
	}

	cases := []struct {
		name string
		item updateEditItem
		want string // path:line edit, or the note
	}{
		{"workload image", updateEditItem{
			Repo: "ghcr.io/example-org/web", Workloads: workload("web"), Owner: apps,
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "web", Container: "web", Image: "ghcr.io/example-org/web:1.2.1"},
		}, `apps/web/deployment.yaml:10 {"op":"image","documentIndex":0,"kind":"Deployment","name":"web","container":"web","image":"ghcr.io/example-org/web:1.2.1"}`},
		{"the chip picked", updateEditItem{
			Repo: "ghcr.io/example-org/web", Workloads: workload("web"), Owner: apps, Version: "2.0.0",
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "web", Container: "web", Image: "ghcr.io/example-org/web:1.2.1"},
		}, `apps/web/deployment.yaml:10 {"op":"image","documentIndex":0,"kind":"Deployment","name":"web","container":"web","image":"ghcr.io/example-org/web:2.0.0"}`},
		{"kustomize images override", updateEditItem{
			Repo: "ghcr.io/example-org/api", Workloads: workload("api"), Owner: apps,
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "api", Container: "api", Image: "ghcr.io/example-org/api:2.1.0"},
		}, `apps/api/kustomization.yaml:5 {"op":"image","documentIndex":0,"kustomizeImage":"ghcr.io/example-org/api","image":"2.1.0"}`},
		{"workload not in Git", updateEditItem{
			Repo: "ghcr.io/example-org/gone", Workloads: workload("gone"), Owner: apps,
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "gone", Container: "gone", Image: "ghcr.io/example-org/gone:1.0.1"},
		}, "no file under apps declares Deployment gone"},
		{"flux chart version", updateEditItem{
			Name: "app", Owner: &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "demo", Name: "app"},
			Edit: &updateEdit{Op: "path", Path: "spec.chart.spec.version", Value: "1.0.1"},
		}, `apps/releases/app.yaml:10 {"op":"path","documentIndex":0,"kind":"HelmRelease","name":"app","path":"spec.chart.spec.version","value":"1.0.1"}`},
		{"argo chart version, parent app", updateEditItem{
			Name: "shop", Owner: &updateOwner{Tool: "argocd", Kind: "Application", Namespace: "argocd", Name: "shop"}, Version: "2.0.0",
			Edit: &updateEdit{Op: "path", Path: "spec.source.targetRevision", Value: "1.0.1"},
		}, `root/shop.yaml:9 {"op":"path","documentIndex":0,"kind":"Application","name":"shop","path":"spec.source.targetRevision","value":"2.0.0"}`},
		{"no declaring app", updateEditItem{
			Name: "lost", Owner: &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "demo", Name: "lost"},
			Edit: &updateEdit{Op: "path", Path: "spec.chart.spec.version", Value: "1.0.1"},
		}, "no Argo CD or Flux app declares HelmRelease lost in Git"},
		{"rendered by a chart", updateEditItem{
			Repo: "ghcr.io/example-org/shop", Workloads: workload("shop"), Owner: &updateOwner{Tool: "argocd", Kind: "Application", Namespace: "argocd", Name: "shop"},
			Edit: &updateEdit{Op: "path", Hint: updateRenderedHint},
		}, "shop/values.yaml rendered by a chart [$values/shop/values.yaml] source 1"},
		{"helm by hand", updateEditItem{
			Repo: "registry.example.test/coredns", Workloads: workload("coredns"), Owner: &updateOwner{Tool: "helm", Kind: "Release", Namespace: "kube-system", Name: "coredns"},
			Edit: &updateEdit{Op: "path", Hint: updateRenderedHint},
		}, updateEditByHandNote},
	}

	for _, c := range cases {
		res, err := resolveUpdateEdit(context.Background(), updateEditTestEnv(), c.item)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)

			continue
		}

		var got string

		switch {
		case res.Edit != nil:
			b, _ := json.Marshal(res.Edit) //nolint:errcheck
			got = res.Path + ":" + itoa(res.Line) + " " + string(b)
		case len(res.ValueFiles) > 0:
			got = res.Path + " " + res.Note + " [" + strings.Join(res.ValueFiles, ",") + "] source " + itoa(res.SourceIndex)
		default:
			got = res.Note
		}

		if !strings.HasPrefix(got, c.want) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}

	if _, err := resolveUpdateEdit(context.Background(), updateEditTestEnv(), updateEditItem{Owner: apps}); !errors.Is(err, errUpdateNoEdit) {
		t.Errorf("no edit: %v", err)
	}
}

func TestUpdateEditTargetDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	var updates struct {
		Images []json.RawMessage `json:"images"`
	}

	out, err := ClusterUpdates(cfg, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(out), &updates); err != nil || len(updates.Images) == 0 {
		t.Fatalf("%s %v", out, err)
	}

	// The first image is podinfo: its patch chip opens deployment.yaml at the image line.
	item := strings.Replace(string(updates.Images[0]), `{`, `{"version":"6.7.1",`, 1)

	got, err := UpdateEditTarget(cfg, "", "", "", item)
	if err != nil {
		t.Fatal(err)
	}

	var res updateEditTarget
	if err := json.Unmarshal([]byte(got), &res); err != nil {
		t.Fatal(err)
	}

	if res.Path != "apps/podinfo/deployment.yaml" || res.Line != 17 || res.Edit == nil || res.Edit.Image != "ghcr.io/stefanprodan/podinfo:6.7.1" || res.App == nil || res.App.Tool != "flux" {
		t.Errorf("podinfo: %s", got)
	}

	if _, err := UpdateEditTarget(cfg, "", "", "", "not json"); err == nil {
		t.Error("a bad item was accepted")
	}
}
