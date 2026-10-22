package plugin

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// generateFixture loads testdata/crds/<file> and generates its draft.
func generateFixture(t *testing.T, file string, o GenerateOptions) (*CRDManifest, []byte) {
	t.Helper()

	src, err := os.ReadFile(filepath.Join("testdata", "crds", file))
	if err != nil {
		t.Fatal(err)
	}

	crd, err := LoadCRD(src)
	if err != nil {
		t.Fatal(err)
	}

	out, err := Generate(crd, o)
	if err != nil {
		t.Fatal(err)
	}

	return crd, out
}

// draftWarnings are the warnings a draft may keep: no translations, and the rules without a
// sample (listed in its samples comment).
func unexpectedWarnings(problems []Problem) []string {
	var out []string

	for _, p := range problems {
		msg := p.String()
		if strings.Contains(msg, "i18n") || strings.Contains(msg, "translat") || strings.Contains(msg, "no sample triggers") {
			continue
		}

		out = append(out, p.Severity+" "+msg)
	}

	return out
}

func TestGenerate(t *testing.T) {
	cases := map[string]GenerateOptions{
		"kube-green.com_sleepinfos.yaml":           {},
		"external-secrets.io_externalsecrets.yaml": {Website: "https://external-secrets.io", Category: "security"},
		"widgets.demo.io_widgetpools.yaml":         {Name: "Widgets"},
		"gadgets.demo.io_gadgets.yaml":             {},
	}

	for file, o := range cases {
		t.Run(file, func(t *testing.T) {
			crd, out := generateFixture(t, file, o)

			p, err := Load(file, out)
			if err != nil {
				t.Fatalf("the draft does not load: %v\n%s", err, out)
			}

			if problems := Validate(p); HasErrors(problems) {
				t.Fatalf("the draft does not validate: %v\n%s", problems, out)
			}

			if _, err := Compile(p); err != nil {
				t.Fatalf("the draft does not compile: %v\n%s", err, out)
			}

			if w := unexpectedWarnings(Lint(p, crd)); len(w) > 0 {
				t.Errorf("lint against its CRD:\n%s\n%s", strings.Join(w, "\n"), out)
			}

			// The same CRD gives the same bytes.
			if _, again := generateFixture(t, file, o); !bytes.Equal(out, again) {
				t.Error("two runs differ")
			}

			golden := filepath.Join("testdata", "generate", strings.TrimSuffix(file, ".yaml")+".yaml")
			if *update {
				if err := os.WriteFile(golden, out, 0o644); err != nil {
					t.Fatal(err)
				}

				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}

			if !bytes.Equal(out, want) {
				t.Errorf("draft differs from %s (run with -update):\n%s", golden, out)
			}
		})
	}
}

func TestGenerateClusterScopedWithPhaseAndScale(t *testing.T) {
	crd, out := generateFixture(t, "widgets.demo.io_widgetpools.yaml", GenerateOptions{})

	p, err := Load("widgets.yaml", out)
	if err != nil {
		t.Fatal(err)
	}

	res := p.Resources["widgetpool"]
	if res.Scope != "Cluster" || res.Version != "v1" || res.Kind != "WidgetPool" {
		t.Errorf("resource %+v (the storage version, cluster-scoped)", res)
	}

	if p.ID != "widgets" {
		t.Errorf("id %q, want widgets (from widgets.demo.io)", p.ID)
	}

	row := p.Cards["list"].Row
	if row == nil || !row.Subtitle.IsZero() {
		t.Errorf("a cluster-scoped row has no subtitle: %+v", row)
	}

	if a, ok := p.Actions["scale"]; !ok || a.Verb != "subresource" || a.Subresource != "scale" || a.RBAC.Resource != "widgetpools/scale" {
		t.Errorf("scale action %+v", a)
	}

	rules := p.Health["widgetpool"]
	if len(rules) == 0 || rules[0].Health != "critical" || rules[len(rules)-1].Health != "ok" {
		t.Errorf("rules: critical first, ok last: %+v", rules)
	}

	// The second sample is the failing one: its phase is the first critical value.
	samples := p.Samples["widgetpool"]
	if len(samples) != 2 {
		t.Fatalf("%d samples", len(samples))
	}

	status, _ := samples[1]["status"].(map[string]any)
	if status["phase"] != "Failed" {
		t.Errorf("second sample status %+v, want phase Failed", status)
	}

	if spec, _ := samples[0]["spec"].(map[string]any); spec["replicas"] != 1 || spec["size"] != "small" {
		t.Errorf("first sample spec %+v: required properties by type", spec)
	}

	if w := unexpectedWarnings(Lint(p, crd)); len(w) > 0 {
		t.Errorf("lint: %v", w)
	}
}

func TestGenerateConditionsAndFilterColumn(t *testing.T) {
	_, out := generateFixture(t, "gadgets.demo.io_gadgets.yaml", GenerateOptions{})
	text := string(out)

	for _, want := range []string{
		`condition("Ready").status`,           // the filter column, read by name
		"conditions.Synced == false",          // a rule per condition type
		"type: conditions",                    // the conditions section
		"$.spec.endpoint.port | default(443)", // a depth-2 field with its default
		"# Where the gadget connects.",        // the description kept
		"subtitle: $.metadata.namespace",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
}

func TestGeneratedID(t *testing.T) {
	cases := map[string]string{
		"kube-green.com":      "kube-green",
		"external-secrets.io": "external-secrets",
		"widgets.demo.io":     "widgets",
		"Acme_Things.dev":     "acme-things",
		"x.org":               "x",
	}

	for group, want := range cases {
		if got := generatedID("", group); got != want {
			t.Errorf("generatedID(%q) = %q, want %q", group, got, want)
		}
	}

	if got := generatedID("chosen", "a.io"); got != "chosen" {
		t.Errorf("a given id is kept: %q", got)
	}
}

func TestGenerateRefusals(t *testing.T) {
	if _, err := Generate(nil, GenerateOptions{}); err == nil {
		t.Error("no CRD")
	}

	if _, err := Generate(&CRDManifest{Spec: CRDSpec{Group: "a.io"}}, GenerateOptions{}); err == nil {
		t.Error("a CRD without versions or names")
	}
}
