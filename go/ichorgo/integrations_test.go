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

	// The catalog's own icon names: the id, another slug, or none.
	if icons["longhorn"] != "longhorn" || icons["cloudnative-pg"] != "postgresql" || icons["dragonfly"] != "" || icons["gateway-api"] != "" {
		t.Errorf("icons: %v", icons)
	}
}

func TestReadIntegrations(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[
			{"name":"apps","preferredVersion":{"version":"v1"}},
			{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}},
			{"name":"postgresql.cnpg.io","preferredVersion":{"version":"v1"}},
			{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}},
			{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}}]}`,
		// Argo Rollouts alone: argoproj.io without Applications.
		"GET /apis/argoproj.io/v1alpha1": `{"resources":[{"name":"rollouts"},{"name":"analysisruns"}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readIntegrations(context.Background(), k, parseHints("garage,velero"))
	if err != nil {
		t.Fatal(err)
	}

	var detected []string
	versions := map[string]string{}
	for _, it := range res.Items {
		if it.Detected {
			detected = append(detected, it.ID)
			versions[it.ID] = it.Version
		}
	}

	// Flux is found by its first group (kustomize) only; a hint alone never detects an operator.
	want := []string{"cloudnative-pg", "longhorn", "garage"}
	if !res.Checked || !slices.Equal(detected, want) {
		t.Fatalf("detected %v, want %v", detected, want)
	}

	if versions["longhorn"] != "v1beta2" || versions["garage"] != "" {
		t.Errorf("versions: %v", versions)
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

	n := 0
	for _, it := range res.Items {
		if it.Detected {
			n++
		}
	}

	if n == 0 {
		t.Error("the demo cluster runs nothing")
	}
}
