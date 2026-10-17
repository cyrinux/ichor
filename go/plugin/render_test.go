package plugin

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden render trees in testdata/render")

// renderNow is the reference time of the golden trees.
var renderNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func compileFixture(t *testing.T, name string) *Compiled {
	t.Helper()

	c, err := CompileSource(name, fixture(t, name))
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// stubHeuristics reads a Ready condition, as the Go core does in a simpler way.
type stubHeuristics struct{}

func (stubHeuristics) ObjectHealth(obj map[string]any) (string, string) {
	status, _ := obj["status"].(map[string]any)
	if phase, _ := status["phase"].(string); phase == "Failed" {
		return "critical", "Failed"
	}

	return "ok", ""
}

func (stubHeuristics) ConditionTone(c map[string]any) string {
	if c["status"] == "True" {
		return "ok"
	}

	return "critical"
}

// golden compares got with testdata/render/name, or rewrites it with -update.
func golden(t *testing.T, name string, got Tree) {
	t.Helper()

	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	data = append(data, '\n')
	path := filepath.Join("testdata", "render", name)

	if *update {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./plugin -run Render -update)", err)
	}

	if !bytes.Equal(want, data) {
		t.Errorf("%s differs from the render; got:\n%s", path, data)
	}
}

func TestRenderSamples(t *testing.T) {
	c := compileFixture(t, "valid.yaml")

	tree, errs := Render(c, SamplesInput(c, "", stubHeuristics{}), renderNow)
	if len(errs) > 0 {
		t.Fatalf("errors %v", errs)
	}

	golden(t, "kube-green-samples.json", tree)
}

func TestRenderWidgetsFrench(t *testing.T) {
	c := compileFixture(t, "widgets.yaml")
	in := SamplesInput(c, "fr-CA", stubHeuristics{})
	in.APIVersions = map[string]string{"widget": "v2"}
	in.Errors = []TreeError{{Resource: "gadgets", Message: "forbidden"}}

	tree, errs := Render(c, in, renderNow)

	// One failing field, reported once for the three widgets.
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "fields.widget.broken") {
		t.Fatalf("errors %v", errs)
	}

	if len(tree.Errors) != 2 || tree.Errors[0].Resource != "gadgets" || tree.Errors[1].Where != "fields.widget.broken" {
		t.Fatalf("tree errors %+v", tree.Errors)
	}

	golden(t, "widgets-fr.json", tree)
}

func TestRenderNoObjects(t *testing.T) {
	c := compileFixture(t, "widgets.yaml")

	tree, errs := Render(c, Input{Detected: Detection{Active: true, Via: "api"}}, renderNow)
	if len(errs) > 0 {
		t.Fatalf("errors %v", errs)
	}

	if tree.Summary.Title != "0 widgets" || tree.Health != "ok" || len(tree.Findings) != 0 {
		t.Fatalf("summary %+v findings %+v", tree.Summary, tree.Findings)
	}

	// A badge hidden at zero is gone; the replicas sum is 0, not null.
	if len(tree.Summary.Badges) != 1 || tree.Summary.Badges[0].Value != "0" {
		t.Fatalf("badges %+v", tree.Summary.Badges)
	}

	for _, card := range tree.Cards {
		if card.Rows == nil || len(card.Rows) != 0 {
			t.Fatalf("card %s rows %+v", card.ID, card.Rows)
		}
	}

	if tree.APIVersion != "" || tree.Findings == nil || tree.Errors == nil {
		t.Fatalf("tree %+v", tree)
	}
}

func TestAggregations(t *testing.T) {
	tests := []struct {
		name string
		vals []any
		want any
	}{
		{"count", []any{true, false, 1, "", nil}, int64(2)},
		{"sum", []any{int64(1), int64(2), "x"}, int64(3)},
		{"sum", []any{int64(1), 0.5}, 1.5},
		{"sum", nil, int64(0)},
		{"min", []any{int64(3), int64(-1)}, int64(-1)},
		{"max", []any{2.5, int64(7)}, 7.0},
		{"max", []any{"a"}, nil},
		{"worst", []any{"ok", "idle", "warning", "info"}, "warning"},
		{"worst", []any{"ok", "bogus"}, "unknown"},
		{"worst", nil, "ok"},
	}

	for _, tt := range tests {
		if got := fold(tt.name, tt.vals); got != tt.want {
			t.Errorf("%s(%v) = %v (%T), want %v", tt.name, tt.vals, got, got, tt.want)
		}
	}
}

func TestAggregationOverAValue(t *testing.T) {
	c := compileFixture(t, "widgets.yaml")
	r := newRenderer(c, SamplesInput(c, "", nil), renderNow)
	r.indexObjects()
	r.evalObjects()

	tests := []struct {
		src  string
		want any
	}{
		{`count([1, 0, 2])`, int64(2)},
		{`worst(["ok", "critical"])`, "critical"},
		{`sum(widget, fields.replicas)`, int64(6)},
		{`count(widget, health == "warning")`, int64(1)},
		{`worst(widget)`, "critical"},
	}

	env := r.dataServiceEnv()

	for _, tt := range tests {
		got := r.eval("test", NewExpr(tt.src, At{}), env)
		if got != tt.want {
			t.Errorf("%s = %v (%T), want %v", tt.src, got, got, tt.want)
		}
	}

	// sum names a resource and an expression; min over a field path is refused.
	for _, src := range []string{`sum(widget)`, `min(widget.size)`} {
		r.eval(src, NewExpr(src, At{}), env)

		if !r.failed[src] {
			t.Errorf("%s did not fail", src)
		}
	}
}
