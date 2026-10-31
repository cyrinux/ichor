package ichorgo

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

const pluginStepHeader = "schemaVersion: ichor.dev/runbook/v1alpha1\nname: Plugin steps\n"

func pluginStepRunbook(steps string) string {
	return pluginStepHeader + "steps:\n" + steps
}

func TestRunbookPluginStepLoads(t *testing.T) {
	usePlugins(t, "actions.yaml")

	cases := map[string]struct {
		step, want string
	}{
		"resolves":               {`{id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}}`, ""},
		"kind given":             {`{id: s, action: plugin/demo-actions/restart, target: {kind: Widget, namespace: demo-shop, name: alpha}}`, ""},
		"typed params":           {`{id: s, action: plugin/demo-actions/resize, target: {namespace: demo-shop, name: alpha}, params: {size: 4}}`, ""},
		"cluster scope":          {`{id: s, action: plugin/demo-actions/retire, target: {name: shared}}`, ""},
		"unknown plugin":         {`{id: s, action: plugin/nope/restart, target: {namespace: demo-shop, name: alpha}}`, `step s: unknown plugin "nope"`},
		"unknown action":         {`{id: s, action: plugin/demo-actions/nope, target: {namespace: demo-shop, name: alpha}}`, `step s: plugin demo-actions has no action "nope"`},
		"bad form":               {`{id: s, action: plugin/demo-actions, target: {namespace: demo-shop, name: alpha}}`, "step s: a plugin action is written plugin/<id>/<action>"},
		"too deep":               {`{id: s, action: plugin/demo-actions/restart/more, target: {namespace: demo-shop, name: alpha}}`, "step s: a plugin action is written plugin/<id>/<action>"},
		"wrong kind":             {`{id: s, action: plugin/demo-actions/restart, target: {kind: Pool, namespace: demo-shop, name: alpha}}`, `step s: target.kind "Pool" is not Widget`},
		"node target":            {`{id: s, action: plugin/demo-actions/restart, target: {node: 10.5.0.2}}`, "step s: a plugin action takes no target.node"},
		"no name":                {`{id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop}}`, "step s: no target name"},
		"no namespace":           {`{id: s, action: plugin/demo-actions/restart, target: {name: alpha}}`, "step s:"},
		"cluster with namespace": {`{id: s, action: plugin/demo-actions/retire, target: {namespace: demo-shop, name: shared}}`, "step s: Pool is cluster-scoped"},
		"undeclared param":       {`{id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}, params: {force: true}}`, `step s: parameter "force"`},
		"missing param":          {`{id: s, action: plugin/demo-actions/resize, target: {namespace: demo-shop, name: alpha}}`, `step s: parameter "size": is required`},
		"wrong type":             {`{id: s, action: plugin/demo-actions/resize, target: {namespace: demo-shop, name: alpha}, params: {size: big}}`, `step s: parameter "size"`},
		"not an enum value":      {`{id: s, action: plugin/demo-actions/tag, target: {namespace: demo-shop, name: alpha}, params: {mode: odd}}`, `step s: parameter "mode"`},
		"params not a map":       {`{id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}, params: [a]}`, "step s:"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadRunbook(pluginStepRunbook("  - " + c.step + "\n"))

			switch {
			case c.want == "" && err != nil:
				t.Fatalf("err = %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// The catalogue stays closed: a plugin step is a second lookup, never an entry of the map.
func TestRunbookPluginStepsStayOutOfTheCatalogue(t *testing.T) {
	usePlugins(t, "actions.yaml")

	if _, err := loadRunbook(pluginStepRunbook("  - {id: s, action: pluginFoo, target: {namespace: a, name: b}}\n")); err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("err = %v", err)
	}

	for name := range runbookActions {
		if strings.HasPrefix(name, runbookPluginPrefix) {
			t.Errorf("%s is in the fixed catalogue", name)
		}
	}
}

func TestRunbookPluginStepDestructiveFollowsConfirm(t *testing.T) {
	usePlugins(t, "actions.yaml")

	cases := map[string]struct {
		action      string
		destructive bool
		confirm     string
	}{
		"typedName":       {"restart", true, "typedName"},
		"plain":           {"pause", true, "plain"},
		"none":            {"tag", false, "none"},
		"plain by absent": {"rebalance", true, "plain"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			src := pluginStepRunbook("  - {id: s, action: plugin/demo-actions/" + c.action + ", target: {namespace: demo-shop, name: alpha}}\n")

			out, err := RunbookSummary(src)
			if err != nil {
				t.Fatal(err)
			}

			var sum runbookSummary
			if err := json.Unmarshal([]byte(out), &sum); err != nil {
				t.Fatal(err)
			}

			if (sum.Destructive == 1) != c.destructive {
				t.Errorf("summary destructive = %d", sum.Destructive)
			}

			f := newFakeKubeAPI(t, map[string]string{})
			p := planOf(t, f.kubeconfigFor(f.URL), "admin@test", src)

			s := p.Steps[0]
			if s.Destructive != c.destructive || s.Confirm != c.confirm {
				t.Errorf("step = %+v", s)
			}
		})
	}
}

func TestRunbookPluginPlanSaysThePluginAndAsksTheCluster(t *testing.T) {
	// An imported plugin: its can-I rows are not in the shared table, the gate still asks them.
	withPluginStore(t)

	src, err := os.ReadFile("../plugin/testdata/plugins/actions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	importUserPlugin(t, string(src))

	_, cfg := runbookKube(t, func(a ssarAttributes) bool { return a.Subresource != "restart" })

	p := planOf(t, cfg, "admin@test", pluginStepRunbook(`  - {id: refused, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}}
  - {id: fine, action: plugin/demo-actions/tag, target: {namespace: demo-shop, name: alpha}}
`))

	refused, fine := p.Steps[0], p.Steps[1]
	if refused.Allowed || !strings.Contains(refused.Denied, "widgets") || refused.Role != adminRole {
		t.Errorf("refused step = %+v", refused)
	}

	if refused.PluginID != "demo-actions" || refused.PluginName != "Demo Actions" || refused.Label != "Restart" || refused.Target.Kind != "Widget" {
		t.Errorf("what the row shows = %+v", refused)
	}

	if !fine.Allowed || fine.Label != "Tag" {
		t.Errorf("fine step = %+v", fine)
	}
}

func TestRunbookPluginPlanLabelIsLocalised(t *testing.T) {
	noKubeDial(t)

	src := pluginStepRunbook("  - {id: s, action: plugin/external-secrets/forceSync, target: {namespace: demo-shop, name: db-creds}}\n")

	label := func(locale string) string {
		out, err := RunbookPlan(demoKubeconfigForTest(t), "", "", src, locale)
		if err != nil {
			t.Fatal(err)
		}

		var p runbookPlanResult
		if err := json.Unmarshal([]byte(out), &p); err != nil {
			t.Fatal(err)
		}

		return p.Steps[0].Label
	}

	if got := label(""); got != "Refresh now" {
		t.Errorf("English label = %q", got)
	}

	if got := label("fr"); got == "Refresh now" || got == "" {
		t.Errorf("French label = %q", got)
	}
}

// A step runs through KubePluginAction: the request the button sends, and its audit entry.
func TestRunbookPluginStepRunsAndIsAudited(t *testing.T) {
	withDataDir(t)

	f, cfg := actionCluster(t)
	rec := newRunbookRecorder()

	startConfirmed(t, cfg, "admin@test", pluginStepRunbook(`  - {id: resize, action: plugin/demo-actions/resize, target: {namespace: demo-shop, name: alpha}, params: {size: 4}}
`), "", rec)

	res, msg := rec.wait(t)
	if msg != "" || !res.OK || res.Steps[0].State != runbookOK {
		t.Fatalf("result = %+v, error %q", res, msg)
	}

	if req := mutation(t, f); req.method != http.MethodPatch || req.path != actAlpha || !strings.Contains(req.body, `"value":4`) {
		t.Errorf("request = %+v", req)
	}

	entries := readAudit(t, "", pluginActionKey("demo-actions", "resize"))
	if len(entries) != 1 || entries[0].Outcome != auditOK || entries[0].Object != "Widget/alpha" || entries[0].Namespace != "demo-shop" || entries[0].Params != `{"size":4}` {
		t.Errorf("audit = %+v", entries)
	}
}

func TestRunbookPluginStepWaitsForItsConfirmation(t *testing.T) {
	withDataDir(t)

	f, cfg := actionCluster(t)
	rec := newRunbookRecorder()
	run := StartRunbook(cfg, "admin@test", "", pluginStepRunbook("  - {id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}}\n"), "", rec)

	if s := <-rec.steps; s.State != runbookWaiting {
		t.Fatalf("first state = %+v, want waiting", s)
	}

	run.Stop()

	if res, _ := rec.wait(t); res.OK || res.Steps[0].State != runbookSkipped {
		t.Fatalf("after Stop: %+v", res)
	}

	if got := f.recorded(); slices.ContainsFunc(got, func(r fakeKubeRequest) bool { return r.method != http.MethodGet }) {
		t.Errorf("the action ran before its confirmation: %+v", got)
	}
}

func TestRunbookPluginStepTakesRunbookParameters(t *testing.T) {
	withDataDir(t)

	f, cfg := actionCluster(t)
	src := pluginStepHeader + `params:
  - {name: ns, kind: namespace}
  - {name: widget}
  - {name: wanted, kind: int}
steps:
  - id: resize
    action: plugin/demo-actions/resize
    target: {namespace: "${ns}", name: "${widget}"}
    params: {size: "${wanted}"}
`

	// The draft takes them as they are written.
	if _, err := loadRunbook(src); err != nil {
		t.Fatalf("draft: %v", err)
	}

	// A wrong value is refused once it is known, by the action's declaration.
	if _, err := loadRunbookWith(strings.Replace(src, "kind: int", "kind: string", 1), map[string]string{"ns": "demo-shop", "widget": "alpha", "wanted": "big"}); err == nil || !strings.Contains(err.Error(), `parameter "size"`) {
		t.Errorf("wrong value: %v", err)
	}

	rec := newRunbookRecorder()
	run := StartRunbookWith(cfg, "admin@test", "", src, "", `{"ns":"demo-shop","widget":"alpha","wanted":"7"}`, rec)

	if s := <-rec.steps; s.State != runbookWaiting {
		t.Fatalf("first state = %+v", s)
	}

	run.Confirm(0)

	res, msg := rec.wait(t)
	if msg != "" || !res.OK {
		t.Fatalf("result = %+v, error %q", res, msg)
	}

	if req := mutation(t, f); req.path != actAlpha || !strings.Contains(req.body, `"value":7`) {
		t.Errorf("request = %+v", req)
	}
}

// A text parameter stays text when a value reads as a number.
func TestRunbookPluginStringParamKeepsItsText(t *testing.T) {
	usePlugins(t, "actions.yaml")

	src := pluginStepHeader + `params:
  - {name: why}
steps:
  - {id: s, action: plugin/demo-actions/pause, target: {namespace: demo-shop, name: alpha}, params: {reason: "${why}"}}
`

	rb, err := loadRunbookWith(src, map[string]string{"why": "42"})
	if err != nil {
		t.Fatal(err)
	}

	js, err := rb.Steps[0].pluginParamsJSON()
	if err != nil || js != `{"reason":"42"}` {
		t.Errorf("params = %s (%v)", js, err)
	}
}

func TestRunbookPluginStepOnADisabledUserPlugin(t *testing.T) {
	withPluginStore(t)

	src, err := os.ReadFile("../plugin/testdata/plugins/actions.yaml")
	if err != nil {
		t.Fatal(err)
	}

	importUserPlugin(t, string(src))

	step := pluginStepRunbook("  - {id: s, action: plugin/demo-actions/restart, target: {namespace: demo-shop, name: alpha}}\n")
	if _, err := loadRunbook(step); err != nil {
		t.Fatalf("enabled: %v", err)
	}

	if err := PluginUserSetEnabled("demo-actions", false); err != nil {
		t.Fatal(err)
	}

	if _, err := loadRunbook(step); err == nil || !strings.Contains(err.Error(), `step s: unknown plugin "demo-actions"`) {
		t.Errorf("disabled: %v", err)
	}
}

func TestRunbookPluginStepDemoPlansAndRefusesTheRun(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)
	src := readRunbookFixture(t, "plugin-step.yaml")

	p := planOf(t, cfg, "", src)
	if len(p.Steps) != 1 || !p.Steps[0].Allowed || !p.Steps[0].Destructive || p.Steps[0].Label != "Refresh now" || p.Steps[0].PluginIcon == "" {
		t.Errorf("demo plan = %+v", p)
	}

	rec := newRunbookRecorder()
	StartRunbook(cfg, "", "", src, "", rec)

	if _, msg := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo run: %q", msg)
	}

	if len(rec.steps) != 0 {
		t.Error("a demo step ran")
	}
}
