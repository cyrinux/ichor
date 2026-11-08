package plugin

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// actionInput is the fixture's samples: alpha (not paused, with a uid), beta (paused), the
// pool shared.
func actionInput(t *testing.T) (*Compiled, Input) {
	t.Helper()

	c := compileFixture(t, "actions.yaml")

	return c, SamplesInput(c, "", nil, renderNow)
}

func alpha() ActionTarget { return ActionTarget{Version: "v1", Namespace: "demo-shop", Name: "alpha"} }

// TestActionBodyPerVerb: each verb's method, path, content type and body.
func TestActionBodyPerVerb(t *testing.T) {
	c, in := actionInput(t)
	now := renderNow.UnixMilli()
	widget := "/apis/actions.example.com/v1/namespaces/demo-shop/widgets"

	tests := []struct {
		action      string
		target      ActionTarget
		params      map[string]any
		method      string
		path        string
		contentType string
		body        string
	}{
		{"pause", alpha(), map[string]any{"reason": "maintenance"}, "PATCH", widget + "/alpha", contentMergePatch,
			`{"metadata":{"resourceVersion":"41"},"spec":{"by":"demo-shop/alpha","paused":true,"reason":"maintenance"}}`},
		{"resize", alpha(), map[string]any{"size": json.Number("7")}, "PATCH", widget + "/alpha", contentJSONPatch,
			`[{"op":"test","path":"/metadata/resourceVersion","value":"41"},{"op":"test","path":"/spec/size","value":3},{"op":"replace","path":"/spec/size","value":7}]`},
		{"restart", alpha(), nil, "PUT", widget + "/alpha/restart", contentJSON, `{}`},
		{"scale", alpha(), map[string]any{"replicas": json.Number("4")}, "PATCH", widget + "/alpha/scale", contentMergePatch,
			`{"metadata":{"resourceVersion":"41"},"spec":{"replicas":4}}`},
		{"remove", alpha(), nil, "DELETE", widget + "/alpha", contentJSON,
			`{"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":"1b0e7c52-0000-4000-8000-000000000001"},"propagationPolicy":"Foreground"}`},
		{"tag", alpha(), map[string]any{"mode": "slow"}, "PATCH", widget + "/alpha", contentMergePatch,
			`{"metadata":{"annotations":{"actions.example.com/mode":"slow","actions.example.com/size.v1":"3","actions.example.com/stale":null,"actions.example.com/touched-at":"` + itoa(now) + `"}}}`},
		{"clone", alpha(), map[string]any{"paused": true, "copies": json.Number("2")}, "POST", widget, contentJSON,
			`{"apiVersion":"actions.example.com/v1","kind":"Widget","metadata":{"labels":{"actions.example.com/copies.v1":"2","actions.example.com/copy-of":"alpha"},"name":"alpha-copy","namespace":"demo-shop"},"spec":{"paused":true,"size":3}}`},
		{"rebalance", alpha(), nil, "PATCH", widget + "/alpha", contentMergePatch, `{"metadata":{"resourceVersion":"41"},"spec":{"rebalance":true}}`},
		{"retire", ActionTarget{Version: "v1", Name: "shared"}, nil, "DELETE", "/apis/actions.example.com/v1/pools/shared", contentJSON,
			`{"apiVersion":"v1","kind":"DeleteOptions"}`},
	}

	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			params, err := c.ActionParams(tc.action, tc.params)
			if err != nil {
				t.Fatal(err)
			}

			req, err := c.ActionBody(tc.action, tc.target, in, params, renderNow)
			if err != nil {
				t.Fatal(err)
			}

			if req.Method != tc.method || req.Path != tc.path || req.ContentType != tc.contentType || string(req.Body) != tc.body {
				t.Errorf("got %s %s %s\n%s\nwant %s %s %s\n%s", req.Method, req.Path, req.ContentType, req.Body, tc.method, tc.path, tc.contentType, tc.body)
			}
		})
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// TestActionDefaultsFillIn: an omitted parameter takes its default, typed.
func TestActionDefaultsFillIn(t *testing.T) {
	c, in := actionInput(t)

	for action, want := range map[string]string{
		"pause": `"reason":"manual"`,
		"scale": `"spec":{"replicas":1}`,
		"tag":   `"actions.example.com/mode":"fast"`,
		"clone": `"paused":false`,
	} {
		params, err := c.ActionParams(action, nil)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		req, err := c.ActionBody(action, alpha(), in, params, renderNow)
		if err != nil || !strings.Contains(string(req.Body), want) {
			t.Errorf("%s: %v %s, want %s", action, err, req.Body, want)
		}
	}
}

// TestActionDisabledRefuses: enabledWhen is checked on the object, with the reason.
func TestActionDisabledRefuses(t *testing.T) {
	c, in := actionInput(t)
	params, _ := c.ActionParams("pause", nil)

	_, err := c.ActionBody("pause", ActionTarget{Version: "v1", Namespace: "demo-shop", Name: "beta"}, in, params, renderNow)

	var disabled *ActionDisabledError
	if !errors.As(err, &disabled) || disabled.Reason != "beta is already paused" {
		t.Fatalf("got %v", err)
	}

	// The data service sees one widget only: rebalance is off, with its reason.
	one := in
	one.Objects = map[string][]map[string]any{"widget": in.Objects["widget"][:1]}

	_, err = c.ActionBody("rebalance", alpha(), one, nil, renderNow)
	if !errors.As(err, &disabled) || disabled.Reason != "only one widget" {
		t.Fatalf("rebalance: %v", err)
	}
}

// TestActionParamErrors: each error names the parameter.
func TestActionParamErrors(t *testing.T) {
	c, _ := actionInput(t)

	tests := []struct {
		action string
		params map[string]any
		param  string
		msg    string
	}{
		{"resize", nil, "size", "is required"},
		{"resize", map[string]any{"size": "7"}, "size", "want an integer"},
		{"resize", map[string]any{"size": json.Number("7.5")}, "size", "want an integer"},
		{"resize", map[string]any{"size": json.Number("2"), "path": "/x"}, "path", "declares no such parameter"},
		{"tag", map[string]any{"mode": "turbo"}, "mode", "want one of fast, slow"},
		{"clone", map[string]any{"paused": "yes"}, "paused", "want true or false"},
		{"pause", map[string]any{"reason": 3.0}, "reason", "want a string"},
		{"pause", map[string]any{"reason": strings.Repeat("x", maxParamLen+1)}, "reason", "longer than"},
		{"restart", map[string]any{"force": true}, "force", "declares no such parameter"},
	}

	for _, tc := range tests {
		_, err := c.ActionParams(tc.action, tc.params)

		var pe *ParamError
		if !errors.As(err, &pe) || pe.Param != tc.param || !strings.Contains(err.Error(), tc.msg) || !strings.Contains(err.Error(), `"`+tc.param+`"`) {
			t.Errorf("%s %v: got %v, want %s: %s", tc.action, tc.params, err, tc.param, tc.msg)
		}
	}

	if _, err := c.ActionParams("nope", nil); err == nil || !strings.Contains(err.Error(), `no action "nope"`) {
		t.Errorf("unknown action: %v", err)
	}
}

// TestActionPathComesFromTheResource: a parameter or a template cannot reach the path, and
// a target that would add a segment is refused.
func TestActionPathComesFromTheResource(t *testing.T) {
	c, in := actionInput(t)

	params, err := c.ActionParams("pause", map[string]any{"reason": "../../../api/v1/namespaces/kube-system/secrets/x?dryRun=All#"})
	if err != nil {
		t.Fatal(err)
	}

	req, err := c.ActionBody("pause", alpha(), in, params, renderNow)
	if err != nil {
		t.Fatal(err)
	}

	if req.Path != "/apis/actions.example.com/v1/namespaces/demo-shop/widgets/alpha" || !strings.Contains(string(req.Body), "../../../api/v1") {
		t.Errorf("path %s body %s", req.Path, req.Body)
	}

	for _, target := range []ActionTarget{
		{Version: "v1", Namespace: "demo-shop", Name: "alpha/../../secrets/x"},
		{Version: "v1", Namespace: "demo-shop", Name: "alpha/restart"},
		{Version: "v1", Namespace: "demo-shop", Name: ".."},
		{Version: "v1", Namespace: "demo-shop", Name: "a..b"},
		{Version: "v1", Namespace: "demo-shop", Name: "alpha?dryRun=All"},
		{Version: "v1", Namespace: "demo-shop", Name: "Alpha"},
		{Version: "v1", Namespace: "demo-shop", Name: ""},
		{Version: "v1", Namespace: "../kube-system", Name: "alpha"},
		{Version: "v1", Namespace: "", Name: "alpha"},
		{Version: "v1/../../api", Namespace: "demo-shop", Name: "alpha"},
	} {
		if req, err := c.ActionBody("pause", target, in, params, renderNow); err == nil {
			t.Errorf("%+v: sent %s %s", target, req.Method, req.Path)
		}
	}

	// A cluster-scoped resource takes no namespace.
	if _, err := c.ActionBody("retire", ActionTarget{Version: "v1", Namespace: "demo-shop", Name: "shared"}, in, nil, renderNow); err == nil || !strings.Contains(err.Error(), "cluster-scoped") {
		t.Errorf("retire in a namespace: %v", err)
	}

	// An object that was not read is not acted on.
	if _, err := c.ActionBody("restart", ActionTarget{Version: "v1", Namespace: "demo-shop", Name: "gamma"}, in, nil, renderNow); err == nil || !strings.Contains(err.Error(), "was not read") {
		t.Errorf("unread object: %v", err)
	}
}

func TestActionNeedsContext(t *testing.T) {
	c, _ := actionInput(t)

	for action, want := range map[string]bool{"pause": false, "resize": false, "rebalance": true, "retire": false} {
		if got := c.ActionNeedsContext(action); got != want {
			t.Errorf("%s: %v, want %v", action, got, want)
		}
	}

	// A field that joins another object needs the resources too, for every action of its resource.
	src := strings.Replace(string(fixture(t, "actions.yaml")), "    size: $.spec.size | default(0)\n",
		"    size: $.spec.size | default(0)\n    pool: join(pool, $.metadata.name == \"shared\")\n", 1)

	joined, err := CompileSource("joined.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	if !joined.ActionNeedsContext("pause") || joined.ActionNeedsContext("retire") {
		t.Errorf("a join: pause %v, retire %v", joined.ActionNeedsContext("pause"), joined.ActionNeedsContext("retire"))
	}
}

// TestTreeSetAllowed: the render tree's buttons take the can-I answers.
func TestTreeSetAllowed(t *testing.T) {
	c, in := actionInput(t)

	tree, _ := Render(c, in, renderNow)
	if !tree.HasActions() {
		t.Fatal("no actions in the tree")
	}

	no := false
	tree.SetAllowed(func(id string) *bool {
		if id == "remove" {
			return &no
		}

		return nil
	})

	seen := 0

	for _, card := range tree.Cards {
		for _, row := range card.Rows {
			for _, s := range row.Detail {
				items, _ := s.Items.([]TreeAction)
				for _, a := range items {
					seen++

					if (a.ID == "remove") != (a.Allowed != nil && !*a.Allowed) {
						t.Errorf("%s allowed %v", a.ID, a.Allowed)
					}
				}
			}
		}
	}

	if seen != 2*8+1 {
		t.Errorf("%d buttons", seen)
	}
}

// TestValidateActionShapes: what a request is built from is checked when the plugin loads.
func TestValidateActionShapes(t *testing.T) {
	src := string(fixture(t, "actions.yaml"))

	tests := []struct {
		from, to, want string
	}{
		{"    subresource: restart\n", "    subresource: restart/../../x\n", "actions.restart.subresource"},
		{"    method: PATCH\n", "    method: GET\n", `actions.scale.method: "GET" is not one of PUT, POST, PATCH`},
		{"    verb: delete\n    propagationPolicy: Foreground\n", "    verb: mergePatch\n    body: { a: 1 }\n    propagationPolicy: Foreground\n", "actions.remove.propagationPolicy: only applies with verb delete"},
		{"    verb: delete\n    rbac: { verbs: [delete], group: actions.example.com, resource: pools }\n", "    verb: delete\n    body: { a: 1 }\n    rbac: { verbs: [delete], group: actions.example.com, resource: pools }\n", "actions.retire.body: verb delete takes no body"},
		{"      - { op: test, path: /spec/size", "      - { op: drop, path: spec/size", `actions.resize.body[0].op: "drop" is not one of`},
		{`body: { spec: { rebalance: true } }`, `body: [1]`, "actions.rebalance.body: verb mergePatch takes a map"},
		{`"actions.example.com/stale": null`, `"actions.example.com/stale": 3`, "an annotation's value is a string"},
		{"type: int, default: 1 }", `type: int, default: "one" }`, `actions.scale.params[0].default: want an integer`},
		{"values: [fast, slow], default: fast }", "values: [fast, slow], default: turbo }", "actions.tag.params[0].default: want one of fast, slow"},
		{"resource: widgets/scale }", "resource: widgets/scale/x }", "actions.scale.rbac.resource"},
		{"op: replace, path: /spec/size,", `op: replace, path: "/spec/{{ params.size }}",`, "actions.resize.body[1].path: is literal"},
		{"op: replace, path: /spec/size,", `op: "{{ params.size }}", path: /spec/size,`, "actions.resize.body[1].op"},
		{"op: replace, path: /spec/size,", "op: move, path: /spec/size,", "actions.resize.body[1].from: a literal JSON pointer"},
		{"op: replace, path: /spec/size,", `op: copy, from: "/spec/{{ params.size }}", path: /spec/size,`, "actions.resize.body[1].from: is literal"},
		{"op: replace, path: /spec/size,", "op: replace, path: /spec/~2size,", "actions.resize.body[1].path: a literal JSON pointer"},
		{"rbac: { verbs: [update], group: actions.example.com, resource: widgets/restart }", "rbac: { verbs: [patch], group: actions.example.com, resource: widgets }",
			"actions.restart.rbac: the request needs update on widgets/restart"},
		{"rbac: { verbs: [delete], group: actions.example.com, resource: pools }", "rbac: { verbs: [delete], group: other.example.com, resource: pools }",
			`actions.retire.rbac: the request needs delete on pools (group "actions.example.com")`},
	}

	for _, tc := range tests {
		if !strings.Contains(src, tc.from) {
			t.Fatalf("fixture lacks %q", tc.from)
		}

		p, err := Load("actions.yaml", []byte(strings.Replace(src, tc.from, tc.to, 1)))
		if err != nil {
			t.Fatalf("%s: %v", tc.want, err)
		}

		got := Validate(p)
		if !strings.Contains(listProblems(got), tc.want) {
			t.Errorf("want %q in\n%s", tc.want, listProblems(got))
		}
	}
}

// TestActionPinsTheCheckedObject: with an enabledWhen, the patches carry the checked
// object's resourceVersion (Pinned); without one, or for a PUT or a create, nothing is added.
func TestActionPinsTheCheckedObject(t *testing.T) {
	c, in := actionInput(t)

	given := map[string]map[string]any{"resize": {"size": json.Number("1")}}

	for action, want := range map[string]bool{"pause": true, "resize": true, "scale": true, "rebalance": true, "tag": false, "restart": false, "clone": false, "remove": false} {
		params, err := c.ActionParams(action, given[action])
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		req, err := c.ActionBody(action, alpha(), in, params, renderNow)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		if req.Pinned != want || strings.Contains(string(req.Body), `"41"`) != want {
			t.Errorf("%s: pinned %v %s, want %v", action, req.Pinned, req.Body, want)
		}
	}
}

// TestActionNullIsLiteralOnly: a hole that reads null is an error, never a null sent (which
// would delete a field); a literal null of the YAML is sent.
func TestActionNullIsLiteralOnly(t *testing.T) {
	src := string(fixture(t, "actions.yaml"))

	for _, tc := range []struct{ from, to, action string }{
		{`reason: "{{ params.reason }}"`, `reason: "{{ $.spec.nope }}"`, "pause"},
		{`"actions.example.com/mode": "{{ params.mode }}"`, `"actions.example.com/mode": "{{ $.spec.nope }}"`, "tag"},
		{`size: "{{ fields.size }}", paused`, `size: "{{ $.metadata.labels.nope }}", paused`, "clone"},
	} {
		if !strings.Contains(src, tc.from) {
			t.Fatalf("fixture lacks %q", tc.from)
		}

		c, err := CompileSource(tc.action+"-null.yaml", []byte(strings.Replace(src, tc.from, tc.to, 1)))
		if err != nil {
			t.Fatal(err)
		}

		params, err := c.ActionParams(tc.action, nil)
		if err != nil {
			t.Fatal(err)
		}

		req, err := c.ActionBody(tc.action, alpha(), SamplesInput(c, "", nil, renderNow), params, renderNow)
		if err == nil || !strings.Contains(err.Error(), "is null") {
			t.Errorf("%s: %v %s", tc.action, err, req.Body)
		}
	}
}

// TestActionCreateMatchesTheTarget: a created object's apiVersion, kind and namespace are the
// resource's and the target's; absent apiVersion and kind are filled in.
func TestActionCreateMatchesTheTarget(t *testing.T) {
	src := string(fixture(t, "actions.yaml"))

	for _, tc := range []struct{ from, to, want string }{
		{"      kind: Widget\n", "      kind: Secret\n", "kind is Secret"},
		{"      apiVersion: actions.example.com/v1\n", "      apiVersion: v1\n", "apiVersion is v1"},
		{`namespace: "{{ namespace }}", labels`, `namespace: kube-system, labels`, "metadata.namespace is kube-system"},
		{"      apiVersion: actions.example.com/v1\n      kind: Widget\n", "", ""},
	} {
		if !strings.Contains(src, tc.from) {
			t.Fatalf("fixture lacks %q", tc.from)
		}

		c, err := CompileSource("create.yaml", []byte(strings.Replace(src, tc.from, tc.to, 1)))
		if err != nil {
			t.Fatal(err)
		}

		params, _ := c.ActionParams("clone", nil)

		req, err := c.ActionBody("clone", alpha(), SamplesInput(c, "", nil, renderNow), params, renderNow)

		switch {
		case tc.want == "" && (err != nil || !strings.Contains(string(req.Body), `"apiVersion":"actions.example.com/v1","kind":"Widget"`)):
			t.Errorf("filled in: %v %s", err, req.Body)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: %v %s", tc.want, err, req.Body)
		}
	}
}
