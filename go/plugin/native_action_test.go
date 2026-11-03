package plugin

import (
	"errors"
	"strings"
	"testing"
)

// The native-action fixture: an action run by a Go function (verb native) on a resource read
// by a Go reader whose rows open another object (ref).

func TestValidateNativeAction(t *testing.T) {
	src := string(fixture(t, "native-action.yaml"))

	if p, err := Load("native-action.yaml", []byte(src)); err != nil {
		t.Fatal(err)
	} else if got := listProblems(Validate(p)); got != "" {
		t.Fatalf("the fixture is valid, got:\n%s", got)
	}

	for _, tt := range []struct{ name, from, to, want string }{
		{"run missing", "    run: native:demo-evict\n", "", "actions.evict.run: is missing: verb native names the Go function"},
		{"run not native", "run: native:demo-evict", "run: demo-evict", `actions.evict.run: "demo-evict": use native:<id>`},
		{"run with another verb", "    verb: native\n", "    verb: mergePatch\n    body: { spec: { a: 1 } }\n", "actions.evict.run: only applies with verb native"},
		{"body", "    verb: native\n", "    verb: native\n    body: { a: 1 }\n", "actions.evict.body: verb native takes no body"},
		{"rbac missing", "    rbac: { verbs: [create], group: \"\", resource: services/proxy }\n", "", "actions.evict.rbac: is missing"},
		{"rbac resource", "resource: services/proxy }", "resource: services/proxy/x }", "actions.evict.rbac.resource"},
		{"unknown verb", "    verb: native\n", "    verb: exec\n", `actions.evict.verb: "exec" is not one of`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(src, tt.from) {
				t.Fatalf("fixture lacks %q", tt.from)
			}

			p, err := Load("native-action.yaml", []byte(strings.Replace(src, tt.from, tt.to, 1)))
			if err != nil {
				t.Fatal(err)
			}

			if got := listProblems(Validate(p)); !strings.Contains(got, tt.want) {
				t.Errorf("problems:\n%s\nwant %q", got, tt.want)
			}
		})
	}
}

// A button on a row whose ref opens another object targets the row's own object; the row's
// ref still opens the Deployment.
func TestNativeActionTargetInTree(t *testing.T) {
	c := compileFixture(t, "native-action.yaml")

	tree, errs := Render(c, SamplesInput(c, "fr", stubHeuristics{}, renderNow), renderNow)
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	// The apps' decoding tests read it too.
	golden(t, "native-action-fr.json", tree)

	rows := tree.Cards[0].Rows
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}

	for _, tt := range []struct {
		name    string
		enabled bool
		reason  string
	}{
		{"carts:v1", false, "pas en mémoire"},
		{"pages:v2", true, ""},
	} {
		row := rows[slicesIndexTitle(rows, tt.name)]
		if row.Ref == nil || row.Ref.Kind != "Deployment" || row.Ref.Name != "cache-a" {
			t.Errorf("%s: row ref %+v", tt.name, row.Ref)
		}

		buttons, _ := row.Detail[0].Items.([]TreeAction)
		if len(buttons) != 1 {
			t.Fatalf("%s: buttons %+v", tt.name, row.Detail)
		}

		b := buttons[0]
		want := Ref{Group: "ichor.dev", Version: "v1alpha1", Resource: "democacheentries", Kind: "DemoCacheEntry", Namespace: "demo-shop", Name: tt.name}

		if b.Target == nil || *b.Target != want || b.Enabled != tt.enabled || b.DisabledReason != tt.reason || b.Label != "Évincer" || b.Confirm != "typedName" {
			t.Errorf("%s: button %+v (target %+v)", tt.name, b, b.Target)
		}
	}
}

func slicesIndexTitle(rows []TreeRow, title string) int {
	for i, r := range rows {
		if r.Title == title {
			return i
		}
	}

	return -1
}

// NativeActionObject finds the object as the plugin read it, checks enabledWhen, and refuses a
// name two objects carry; ActionBody has no request for a native action.
func TestNativeActionObject(t *testing.T) {
	c := compileFixture(t, "native-action.yaml")
	in := SamplesInput(c, "", stubHeuristics{}, renderNow)

	obj, err := c.NativeActionObject("evict", "demo-shop", "pages:v2", in, renderNow)
	if err != nil {
		t.Fatal(err)
	}

	if spec, _ := obj["spec"].(map[string]any); spec["server"] != "cache-a" {
		t.Errorf("object %+v", obj)
	}

	var disabled *ActionDisabledError
	if _, err := c.NativeActionObject("evict", "demo-shop", "carts:v1", in, renderNow); !errors.As(err, &disabled) || disabled.Reason != "not in memory" {
		t.Errorf("cold entry: %v", err)
	}

	if _, err := c.NativeActionObject("evict", "demo-shop", "gone:v1", in, renderNow); err == nil || !strings.Contains(err.Error(), "was not read") {
		t.Errorf("missing entry: %v", err)
	}

	twice := in
	twice.Objects = map[string][]map[string]any{"entry": append(append([]map[string]any{}, in.Objects["entry"]...), in.Objects["entry"][0])}

	var ambiguous *ActionAmbiguousError
	if _, err := c.NativeActionObject("evict", "demo-shop", "pages:v2", twice, renderNow); !errors.As(err, &ambiguous) || ambiguous.Count != 2 {
		t.Errorf("same name twice: %v", err)
	}

	if _, err := c.ActionBody("evict", ActionTarget{Version: "v1alpha1", Namespace: "demo-shop", Name: "pages"}, in, nil, renderNow); err == nil {
		t.Error("ActionBody built a request for a native action")
	}
}
