package ichorgo

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Every API group constant a reader uses (groupXxx = "a.b") must be listed in Settings.
func TestIntegrationsCoverEveryGroup(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	listed := map[string]bool{}
	for _, s := range integrationSpecs {
		for _, g := range s.Groups {
			listed[g] = true
		}
	}

	found := 0

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		{
			ast.Inspect(file, func(n ast.Node) bool {
				spec, ok := n.(*ast.ValueSpec)
				if !ok {
					return true
				}

				for i, id := range spec.Names {
					if !strings.HasPrefix(id.Name, "group") || i >= len(spec.Values) {
						continue
					}

					lit, ok := spec.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}

					group, _ := strconv.Unquote(lit.Value)
					if !strings.Contains(group, ".") {
						continue
					}

					found++
					if !listed[group] {
						t.Errorf("%s: %s = %q is not in integrationSpecs", name, id.Name, group)
					}
				}

				return true
			})
		}
	}

	if found < 10 {
		t.Fatalf("found only %d group constants: the scan is broken", found)
	}
}

func TestIntegrationSpecs(t *testing.T) {
	catalog := loadAppCatalog()
	seen := map[string]bool{}

	for _, s := range integrationSpecs {
		if seen[s.ID] {
			t.Errorf("%s listed twice", s.ID)
		}
		seen[s.ID] = true

		if !strings.HasPrefix(s.Website, "https://") || s.Name == "" {
			t.Errorf("%s: name %q website %q", s.ID, s.Name, s.Website)
		}

		// The id names the catalog app (its icon, the inventory hint); Gateway API has no image.
		if app := catalog.byName[s.ID]; s.ID != "gateway-api" && (app == nil || app.ID != s.ID) {
			t.Errorf("%s is not a catalog app id", s.ID)
		}
	}
}

func TestIntegrationsStatic(t *testing.T) {
	out, err := Integrations()
	if err != nil {
		t.Fatal(err)
	}

	var res integrations
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Checked || len(res.Items) != len(integrationSpecs) {
		t.Fatalf("got %+v", res)
	}

	icons := map[string]string{}
	for _, it := range res.Items {
		if it.Detected {
			t.Errorf("%s detected without a cluster", it.ID)
		}
		icons[it.ID] = it.Icon
	}

	// Bundled icons are named after the catalog id (as in the inventory); none for Dragonfly.
	if icons["longhorn"] != "longhorn" || icons["cloudnative-pg"] != "cloudnative-pg" || icons["dragonfly"] != "" || icons["gateway-api"] != "" {
		t.Errorf("icons: %v", icons)
	}
}

func TestReadIntegrations(t *testing.T) {
	stopped := fakePod("old", "garage-old", "node-1", false, nil, "garage", "dxflrs/garage:v1.0.0")
	stopped.Status.Phase = "Succeeded"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[
			{"name":"apps","preferredVersion":{"version":"v1"}},
			{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}},
			{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}},
			{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}},
			{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}}]}`,
		// Argo Rollouts alone: argoproj.io without Applications.
		"GET /apis/argoproj.io/v1alpha1": `{"resources":[{"name":"rollouts"},{"name":"analysisruns"}]}`,
		"GET /api/v1/pods": podListJSON(t, stopped,
			fakePod("storage", "garage-0", "node-1", true, nil, "garage", "dxflrs/garage:v2.3.0"),
			// Cilium's image alone never detects it: it has an API group.
			fakePod("kube-system", "cilium-x", "node-1", true, nil, "cilium-agent", "quay.io/cilium/cilium:v1.18.2")),
		"GET /api/v1/services": `{"items":[{"metadata":{"name":"thanos-query","namespace":"metrics"},"spec":{"ports":[{"name":"http","port":9090}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// The hints are only a fallback: velero is hinted but neither served nor running.
	res, err := readIntegrations(context.Background(), k, parseHints("garage,velero"))
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]integration{}
	var detected []string
	for _, it := range res.Items {
		if it.Detected {
			detected = append(detected, it.ID)
			got[it.ID] = it
		}
	}

	// Flux is found by its first group (kustomize) only.
	want := []string{"cloudnative-pg", "longhorn", "garage", "thanos"}
	if !res.Checked || !slices.Equal(detected, want) {
		t.Fatalf("detected %v, want %v", detected, want)
	}

	if g := got["garage"]; g.Via != detectedByPods || g.Version != "v2.3.0" || g.Namespace != "storage" {
		t.Errorf("garage: %+v", g)
	}

	if l := got["longhorn"]; l.Via != detectedByAPI || l.Version != "v1beta2" || l.Namespace != "" {
		t.Errorf("longhorn: %+v", l)
	}

	if th := got["thanos"]; th.Via != detectedByServices || th.Namespace != "metrics" {
		t.Errorf("thanos: %+v", th)
	}
}

func TestReadIntegrationsPodsForbidden(t *testing.T) {
	// Only /apis answers: the pods cannot be listed, the inventory's hint stands in.
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readIntegrations(context.Background(), k, parseHints("garage,longhorn"))
	if err != nil {
		t.Fatal(err)
	}

	for _, it := range res.Items {
		switch {
		case it.ID == "garage" && (!it.Detected || it.Via != detectedByInventory):
			t.Errorf("garage: %+v", it)
		case it.ID != "garage" && it.Detected:
			t.Errorf("%s detected: %+v", it.ID, it)
		}
	}
}

func TestReadIntegrationsArgoCD(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                      `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/argoproj.io/v1alpha1": `{"resources":[{"name":"applications"},{"name":"appprojects"}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readIntegrations(context.Background(), k, parseHints(""))
	if err != nil {
		t.Fatal(err)
	}

	if !res.Items[0].Detected || res.Items[0].ID != "argo-cd" || res.Items[0].Version != "v1alpha1" {
		t.Errorf("argo-cd: %+v", res.Items[0])
	}
}

func TestDemoIntegrations(t *testing.T) {
	res := demoIntegrations()
	if !res.Checked {
		t.Fatal("demo not checked")
	}

	detected := map[string]bool{}
	for _, it := range res.Items {
		detected[it.ID] = it.Detected
	}

	if !detected["argo-cd"] || !detected["garage"] || !detected["prometheus"] {
		t.Errorf("demo: %v", detected)
	}
}
