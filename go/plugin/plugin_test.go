package plugin

import (
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	src, err := os.ReadFile("testdata/plugins/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return src
}

// problems loads, validates and compiles a fixture and returns every problem it has.
func problems(t *testing.T, name string) []Problem {
	t.Helper()

	p, err := Load(name, fixture(t, name))

	var le *LoadError
	if errors.As(err, &le) {
		return le.Problems
	}

	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	found := Validate(p)

	_, err = Compile(p)
	if HasErrors(found) != (err != nil) {
		t.Fatalf("%s: Compile error %v with problems %v", name, err, found)
	}

	return found
}

type want struct {
	line int
	msg  string // a substring of Path + ": " + Msg
}

func TestFixtures(t *testing.T) {
	tests := []struct {
		file string
		want []want
	}{
		{"valid.yaml", nil},
		{"unknown-field.yaml", []want{
			{13, "unknown key colour"},
			{17, "unknown key subtitle"},
		}},
		{"bad-expr.yaml", []want{
			{15, "fields.widget.size: column 25: unexpected end of expression"},
			{17, "fields.widget.later: column 19: unknown fields.after"},
			{18, `fields.widget.after: column 25: unknown function "nope"`},
			{23, "health.widget[1].when: column 14: a condition must be a bool, this is a string"},
			{26, `health.widget[2].health: "green" is not one of ok, idle, info, warning, critical, unknown`},
			{31, `cards.summary.title: unexpected "1"`},
			{37, `cards.list.row.status.tone: column 34: expected ":"`},
		}},
		{"missing-sample.yaml", []want{
			{23, `cards.list.from: "gizmo" is neither a resource (gadget, widget) nor a data service (byScope)`},
			{32, "cards.groups.row.detail[0].items[0]: a row of a data service has no object to act on"},
			{39, "actions.restart.rbac: is missing"},
			{39, "actions.restart.audit: is missing: name the audit entry (demo-widgets.restart)"},
			{42, `actions.restart.verb: "restart" is not one of mergePatch, jsonPatch, subresource, delete, annotate, create`},
			{43, "samples: gadget has no sample"},
			{46, `samples.gizmo: "gizmo" is not a declared resource`},
		}},
		{"dup-id.yaml", []want{
			{2, `id: "Demo_Widgets": use lowercase letters, digits and dashes`},
			{5, "detect[0]: set exactly one of group, pods or service"},
			{22, "actions.resize.body.spec.owner: unknown params.owner"},
			{27, `actions.resize.params[1].name: "size" is declared twice`},
			{27, `actions.resize.params[1].type: "number" is not one of string, int, bool, enum`},
			{34, `findings[1].id: "too-big" is declared twice`},
			{39, `findings[1].opens: "details" is not a card (summary)`},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			got := problems(t, tc.file)
			if len(got) != len(tc.want) {
				t.Fatalf("%d problems, want %d:\n%s", len(got), len(tc.want), listProblems(got))
			}

			for i, w := range tc.want {
				text := got[i].Path + ": " + got[i].Msg
				if got[i].Line != w.line || !strings.Contains(text, w.msg) {
					t.Errorf("problem %d = %d: %s, want %d: %s", i, got[i].Line, text, w.line, w.msg)
				}
			}
		})
	}
}

func listProblems(problems []Problem) string {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = p.String()
	}

	return strings.Join(lines, "\n")
}

func TestCompileResolvesInFileOrder(t *testing.T) {
	p, err := Load("valid.yaml", fixture(t, "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	c, err := Compile(p)
	if err != nil {
		t.Fatal(err)
	}

	var fields []string
	for _, f := range c.Fields["sleepinfo"] {
		fields = append(fields, f.Name)
	}

	if got := strings.Join(fields, ","); !strings.HasPrefix(got, "weekdays,sleepAt,wakeUpAt,timeZone,") || !strings.HasSuffix(got, ",nextWakeUpAt,state") {
		t.Errorf("fields in order = %s", got)
	}

	if len(c.DataServices) != 2 || c.DataServices[0].Name != "summary" || c.DataServices[1].Name != "byNamespace" {
		t.Errorf("data services = %+v", c.DataServices)
	}

	cards := c.Cards
	if len(cards) != 3 || cards[0].Name != "summary" || cards[1].Name != "list" || cards[1].Resource != "sleepinfo" ||
		cards[2].Name != "namespaces" || cards[2].DataService != "byNamespace" {
		t.Errorf("cards = %+v", cards)
	}

	if c.Hash == "" || len(c.Warnings) != 0 {
		t.Errorf("hash %q, warnings %v", c.Hash, c.Warnings)
	}

	// The same name and bytes compile once; Compile itself does not cache (p is the caller's).
	s1, err := CompileSource("valid.yaml", fixture(t, "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if s2, _ := CompileSource("valid.yaml", fixture(t, "valid.yaml")); s2 != s1 {
		t.Error("the same source compiled twice")
	}

	if s3, _ := CompileSource("other.yaml", fixture(t, "valid.yaml")); s3 == s1 || s3.Plugin.FileName() != "other.yaml" {
		t.Error("another file name shared the cache entry")
	}

	p.Resources = nil
	if _, err := Compile(p); err == nil {
		t.Error("a plugin changed after a compile still compiles")
	}

	// Expressions keep their file line.
	if e := c.Fields["sleepinfo"][0].Expr; e.Line != 38 || e.Node() == nil {
		t.Errorf("weekdays at line %d, node %v", e.Line, e.Node())
	}
}

func TestCacheEvictsLeastRecentlyUsed(t *testing.T) {
	c := newCache(2)

	c.put("a", &Compiled{})
	c.put("b", &Compiled{})
	c.get("a") // a is now the most recent
	c.put("d", &Compiled{})

	if _, ok := c.get("b"); ok {
		t.Error("b survived")
	}

	if _, ok := c.get("a"); !ok {
		t.Error("a was evicted")
	}

	if c.put("a", &Compiled{}); c.order.Len() != 2 {
		t.Error("putting a key again added an entry")
	}
}

const v1 = "schemaVersion: ichor.dev/plugin/v1alpha1\n"

func TestLoadRefuses(t *testing.T) {
	tests := []struct {
		name, src, want string
		line            int
	}{
		{"empty", "", "the file is empty", 1},
		{"not a map", "- a\n- b\n", "a plugin is a map", 1},
		{"no version", "id: x\n", "schemaVersion is missing", 1},
		{"other major", "id: x\nschemaVersion: ichor.dev/plugin/v2\n", `schemaVersion "ichor.dev/plugin/v2" is not supported`, 2},
		{"bad yaml", "schemaVersion: ichor.dev/plugin/v1alpha1\nid: [x\n", "", 0},
		{"expression is a map", "schemaVersion: ichor.dev/plugin/v1alpha1\nfields:\n  a:\n    b: {c: d}\n", "an expression is a single value", 4},
		{"too large", "#" + strings.Repeat("x", MaxSize), "larger than 1 MiB", 0},
		{"too many flow collections", "#" + strings.Repeat("{", maxFlow+1), "[ or {", 0},
		{"too deep", v1 + "samples:\n  x:\n    - " + strings.Repeat("{a: ", 70) + "1" + strings.Repeat("}", 70) + "\n", "nested deeper than 64", 4},
		{"number key", v1 + "samples:\n  x:\n    - {1: one}\n", `a key is text: quote "1"`, 4},
		{"merge key", v1 + "base: &b {a: 1}\nsamples:\n  x:\n    - {<<: *b}\n", "merge keys (<<) are not supported", 5},
		{"empty list item", v1 + "sort:\n  -\n  - $.a\n", "an empty list item", 3},
		{"NaN", v1 + "samples:\n  x:\n    - {n: .nan}\n", ".nan is not a number JSON can carry", 4},
		{"number tone", v1 + "cards:\n  list:\n    row:\n      status: {tone: 3}\n", "a tone is a word", 5},
	}

	for _, tc := range tests {
		_, err := Load(tc.name, []byte(tc.src))

		var le *LoadError
		if !errors.As(err, &le) || len(le.Problems) == 0 {
			t.Fatalf("%s: error %v", tc.name, err)
		}

		if p := le.Problems[0]; !strings.Contains(p.Msg, tc.want) || (tc.line != 0 && p.Line != tc.line) {
			t.Errorf("%s: %d: %s, want %d: %s", tc.name, p.Line, p.Msg, tc.line, tc.want)
		}
	}
}

// An alias bomb and deep nesting are refused or bounded, never hang or panic.
func TestLoadHostileYAML(t *testing.T) {
	bomb := "schemaVersion: ichor.dev/plugin/v1alpha1\na: &a [x, x, x, x, x, x, x, x, x]\n"
	for i, prev := 0, "a"; i < 9; i++ {
		name := string(rune('b' + i))
		bomb += name + ": &" + name + " [*" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + "]\n"
		prev = name
	}

	deep := "schemaVersion: ichor.dev/plugin/v1alpha1\nsamples:\n  x:\n    - " + strings.Repeat("[", 10_000) + strings.Repeat("]", 10_000) + "\n"

	for name, src := range map[string]string{"alias bomb": bomb, "deep nesting": deep} {
		if _, err := Load(name, []byte(src)); err == nil {
			t.Errorf("%s loads", name)
		}
	}
}

func TestBuiltinsLoad(t *testing.T) {
	for _, c := range Builtins() {
		if c.Plugin.ID == "" {
			t.Errorf("%s has no id", c.Plugin.FileName())
		}
	}
}

func TestLoadDirRefusesDuplicateIDs(t *testing.T) {
	valid := fixture(t, "valid.yaml")
	fsys := fstest.MapFS{
		"dir/a.yaml": {Data: valid},
		"dir/b.yaml": {Data: append([]byte("# a copy\n"), valid...)},
	}

	if _, err := loadDir(fsys, "dir"); err == nil || !strings.Contains(err.Error(), `id "kube-green" is already used by dir/a.yaml`) {
		t.Fatalf("error %v", err)
	}

	got, err := loadDir(fstest.MapFS{"dir/a.yaml": {Data: valid}}, "dir")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v, %v", got, err)
	}
}
