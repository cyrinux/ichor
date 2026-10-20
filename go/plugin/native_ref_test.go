package plugin

import (
	"strings"
	"testing"
)

// A resource with ref opens the object its expression names: its rows, findings, relations
// and select rows carry that object's group, version, resource, kind, namespace and name.
func TestRenderRefOverride(t *testing.T) {
	c := compileFixture(t, "native-ref.yaml")

	tree, errs := Render(c, SamplesInput(c, "fr", stubHeuristics{}, renderNow), renderNow)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	ingress := Ref{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespace: "demo-shop", Name: "shop"}
	service := Ref{Group: "", Version: "v1", Resource: "services", Kind: "Service", Namespace: "demo-lab", Name: "gateway"}

	records := tree.Cards[0]
	if records.ID != "records" || len(records.Rows) != 2 {
		t.Fatalf("records card %+v", records)
	}

	// Sorted by hostname: lab, then shop.
	for i, want := range []Ref{service, ingress} {
		row := records.Rows[i]
		if row.Ref == nil || *row.Ref != want {
			t.Errorf("row %d ref %+v, want %+v", i, row.Ref, want)
		}

		items, _ := row.Detail[0].Items.([]TreeItem)
		if len(items) != 1 || items[0].Ref == nil || *items[0].Ref != want {
			t.Errorf("row %d relation %+v, want %+v", i, items, want)
		}
	}

	hosts := tree.Cards[1]
	if len(hosts.Rows) != 2 || hosts.Rows[0].Ref == nil || *hosts.Rows[0].Ref != ingress {
		t.Errorf("select rows %+v", hosts.Rows)
	}

	if len(tree.Findings) != 1 {
		t.Fatalf("findings %+v", tree.Findings)
	}

	// The finding opens the source object; its title names the record.
	f := tree.Findings[0]
	if f.Ref == nil || *f.Ref != service || f.Title != "lab-a n'a pas de cible" {
		t.Errorf("finding %+v (ref %+v)", f, f.Ref)
	}

	if len(tree.Tools) != 1 || tree.Tools[0] != (TreeTool{ID: "check", Label: "Vérifier les noms", Hint: "Résoudre chaque nom d'hôte"}) {
		t.Errorf("tools %+v", tree.Tools)
	}
}

// A ref that gives no object leaves the row without a ref, as an object the plugin did not
// list; the finding still renders.
func TestRenderRefOverrideNoObject(t *testing.T) {
	c := compileFixture(t, "native-ref.yaml")
	in := SamplesInput(c, "", stubHeuristics{}, renderNow)
	in.Objects["record"] = []map[string]any{
		{"metadata": map[string]any{"name": "bare", "namespace": "demo-lab"}, "spec": map[string]any{"hostname": "bare.example.com"}},
		{"metadata": map[string]any{"name": "half", "namespace": "demo-lab"}, "source": map[string]any{"resource": "services"}},
	}

	tree, _ := Render(c, in, renderNow)

	for _, row := range tree.Cards[0].Rows {
		if row.Ref != nil {
			t.Errorf("row %s ref %+v, want none", row.Title, row.Ref)
		}
	}

	if len(tree.Findings) != 2 || tree.Findings[0].Ref != nil || tree.Findings[0].Title != "bare has no target" {
		t.Errorf("findings %+v", tree.Findings)
	}
}

func TestValidateRefAndTools(t *testing.T) {
	src := string(fixture(t, "native-ref.yaml"))

	for _, tt := range []struct{ name, from, to, want string }{
		{"ref without source", "    source: native:demo-records\n", "", "resources.record.ref: needs source"},
		{"ref out of scope", "ref: $.source", "ref: dataServices.summary", "resources.record.ref: column 10: dataServices is not available here"},
		{"run not native", "run: native:demo-check", "run: demo-check", `tools.check.run: "demo-check": use native:<id>`},
		{"run missing", "    run: native:demo-check\n", "", "tools.check.run: is missing"},
		{"label missing", `    label: "Check names"` + "\n", "", "tools.check.label: is missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Load("native-ref.yaml", []byte(strings.Replace(src, tt.from, tt.to, 1)))
			if err != nil {
				t.Fatal(err)
			}

			got := listProblems(Validate(p))
			if !strings.Contains(got, tt.want) {
				t.Errorf("problems:\n%s\nwant %q", got, tt.want)
			}
		})
	}
}

func TestLintRefGivesNoObject(t *testing.T) {
	src := string(fixture(t, "native-ref.yaml"))

	p, err := Load("native-ref.yaml", fixture(t, "native-ref.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if got := listProblems(Lint(p, nil)); strings.Contains(got, ".ref") {
		t.Errorf("the fixture's refs are fine, got:\n%s", got)
	}

	broken := strings.Replace(src, "name: gateway }", "name: \"\" }", 1)

	p, err = Load("native-ref.yaml", []byte(broken))
	if err != nil {
		t.Fatal(err)
	}

	got := listProblems(Lint(p, nil))
	if !strings.Contains(got, "resources.record.ref: gives no object on sample 1 (samples.record[1])") {
		t.Errorf("lint:\n%s", got)
	}
}
