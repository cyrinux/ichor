package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const restartAnywhere = `schemaVersion: ichor.dev/runbook/v1alpha1
name: Restart an app in any namespace
params:
  - {name: ns, prompt: Namespace, kind: namespace}
  - {name: app, prompt: Workload, kind: workload, default: ingest-api}
  - {name: count, kind: int, default: 2}
steps:
  - id: scale
    action: scale
    target: {namespace: "${ns}", name: "${app}"}
    params: {replicas: "${count}"}
  - id: restart
    action: rolloutRestart
    target:
      namespace: ${ns}
      name: ${app}
`

func TestRunbookValuesAreSubstituted(t *testing.T) {
	rb, err := loadRunbookWith(restartAnywhere, map[string]string{"ns": "demo-shop", "count": "3"})
	if err != nil {
		t.Fatal(err)
	}

	first, second := rb.Steps[0], rb.Steps[1]
	if first.Target.Namespace != "demo-shop" || first.Target.Name != "ingest-api" || first.Target.Kind != "Deployment" {
		t.Errorf("target = %+v", first.Target)
	}

	if first.Params.Replicas == nil || *first.Params.Replicas != 3 {
		t.Errorf("replicas = %v, want the int parameter", first.Params.Replicas)
	}

	if second.Target.Namespace != "demo-shop" || second.Target.Name != "ingest-api" {
		t.Errorf("a plain ${x} in a block mapping: %+v", second.Target)
	}
}

func TestRunbookValueCannotAddYAML(t *testing.T) {
	rb, err := loadRunbookWith(restartAnywhere, map[string]string{"ns": "a: {b: c}", "app": "[x]"})
	if err != nil {
		t.Fatal(err)
	}

	if got := rb.Steps[0].Target; got.Namespace != "a: {b: c}" || got.Name != "[x]" {
		t.Errorf("a value is a scalar, whatever it holds: %+v", got)
	}
}

func TestRunbookValueErrors(t *testing.T) {
	head := "schemaVersion: ichor.dev/runbook/v1alpha1\nname: x\n"
	step := "steps:\n  - {id: s, action: rolloutRestart, target: {namespace: \"${ns}\", name: api}}\n"

	cases := map[string]struct {
		src    string
		values map[string]string
		want   string
	}{
		"missing lists the names": {head + "params:\n  - {name: ns}\n  - {name: other}\n" + step, nil, "missing a value for ns, other"},
		"an empty value is none":  {head + "params: [{name: ns}]\n" + step, map[string]string{"ns": " "}, "missing a value for ns"},
		"unknown names the step":  {head + step, nil, "step s: unknown parameter ${ns}"},
		"not a number":            {head + "params: [{name: ns, kind: int}]\n" + step, map[string]string{"ns": "two"}, "ns must be a whole number"},
		"not a bool":              {head + "params: [{name: ns, kind: bool}]\n" + step, map[string]string{"ns": "maybe"}, "ns must be true or false"},
		"bad default":             {head + "params: [{name: ns, kind: int, default: two}]\n" + step, nil, "ns must be a whole number"},
		"bad name":                {head + "params: [{name: 1ns}]\n" + step, nil, `parameter name "1ns"`},
		"bad kind":                {head + "params: [{name: ns, kind: secret}]\n" + step, nil, `parameter ns: unknown kind "secret"`},
		"twice":                   {head + "params: [{name: ns}, {name: ns}]\n" + step, nil, "parameter ns is declared twice"},
		"kind check sees the end": {head + "params: [{name: ns}]\nsteps:\n  - {id: s, action: nodeReboot, target: {node: n}, params: {mode: \"${ns}\"}}\n", map[string]string{"ns": "gently"}, "step s:"},
		"int into replicas fails": {head + "params: [{name: ns}]\nsteps:\n  - {id: s, action: scale, target: {namespace: a, name: b}, params: {replicas: \"${ns}\"}}\n", map[string]string{"ns": "many"}, "step s:"},
	}

	for name, c := range cases {
		_, err := loadRunbookWith(c.src, c.values)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func TestRunbookParamLimit(t *testing.T) {
	var params strings.Builder
	for i := range runbookMaxParams + 1 {
		params.WriteString("  - {name: p" + string(rune('a'+i)) + "}\n")
	}

	src := "schemaVersion: ichor.dev/runbook/v1alpha1\nname: x\nparams:\n" + params.String() +
		"steps:\n  - {id: s, action: rolloutRestart, target: {namespace: a, name: b}}\n"
	if _, err := loadRunbook(src); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Errorf("err = %v", err)
	}
}

func TestRunbookEscapeKeepsALiteral(t *testing.T) {
	src := "schemaVersion: ichor.dev/runbook/v1alpha1\nname: x\nsteps:\n" +
		"  - {id: s, action: rolloutRestart, target: {namespace: \"$${ns}\", name: api}}\n"

	rb, err := loadRunbookWith(src, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := rb.Steps[0].Target.Namespace; got != "${ns}" {
		t.Errorf("namespace = %q", got)
	}
}

// What the import shows: the file as written, nothing asked of the cluster about a placeholder.
func TestRunbookPlanWithoutValuesShowsThePlaceholders(t *testing.T) {
	_, cfg := runbookKube(t, func(ssarAttributes) bool { return false })

	p := planOf(t, cfg, "admin@test", restartAnywhere)
	if got := p.Steps[1].Target; got.Namespace != "${ns}" || got.Name != "ingest-api" {
		t.Errorf("target = %+v", got)
	}

	if !p.Steps[1].Allowed {
		t.Errorf("a step without its values is not refused yet: %+v", p.Steps[1])
	}

	if _, err := RunbookPlanWith(cfg, "admin@test", "", restartAnywhere, "", ""); err == nil || !strings.Contains(err.Error(), "missing a value for ns") {
		t.Errorf("planned without its values: %v", err)
	}
}

func TestRunbookPlanWithShowsTheTargetAndAsksTheCluster(t *testing.T) {
	_, cfg := runbookKube(t, func(ssarAttributes) bool { return false })

	out, err := RunbookPlanWith(cfg, "admin@test", "", restartAnywhere, `{"ns":"demo-shop"}`, "")
	if err != nil {
		t.Fatal(err)
	}

	var p runbookPlanResult
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if got := p.Steps[0].Target; got.Namespace != "demo-shop" || got.Name != "ingest-api" {
		t.Errorf("target = %+v", got)
	}

	if p.Steps[0].Allowed || p.Steps[0].Denied == "" {
		t.Errorf("the gate runs on the final target: %+v", p.Steps[0])
	}

	if _, err := RunbookPlanWith(cfg, "admin@test", "", restartAnywhere, "not json", ""); err == nil {
		t.Error("broken values were accepted")
	}
}

func TestRunbookSummaryListsTheParams(t *testing.T) {
	out, err := RunbookSummary(restartAnywhere)
	if err != nil {
		t.Fatal(err)
	}

	var sum runbookSummary
	if err := json.Unmarshal([]byte(out), &sum); err != nil {
		t.Fatal(err)
	}

	if len(sum.Params) != 3 {
		t.Fatalf("params = %+v", sum.Params)
	}

	if got := sum.Params[1]; got.Name != "app" || got.Prompt != "Workload" || got.Default != "ingest-api" || got.Kind != "workload" {
		t.Errorf("param = %+v", got)
	}

	if got := sum.Params[2]; got.Prompt != "count" || got.Default != "2" || got.Kind != "int" {
		t.Errorf("no prompt falls back to the name, a default keeps its text: %+v", got)
	}

	src := readRunbookFixture(t, "three-steps.yaml")
	if out, err = RunbookSummary(src); err != nil || !strings.Contains(out, `"params":[]`) {
		t.Errorf("no params is an empty list: %s %v", out, err)
	}
}

func TestStartRunbookNeverRunsAPlaceholder(t *testing.T) {
	_, cfg := runbookKube(t, nil)

	rec := newRunbookRecorder()
	StartRunbook(cfg, "admin@test", "", restartAnywhere, "", rec)

	if _, msg := rec.wait(t); !strings.Contains(msg, "missing a value for ns") {
		t.Errorf("err = %q", msg)
	}
}

func TestStartRunbookWithRunsTheValues(t *testing.T) {
	withDataDir(t)

	f, cfg := runbookKube(t, nil)
	rec := newRunbookRecorder()
	run := StartRunbookWith(cfg, "admin@test", "", restartAnywhere, "", `{"ns":"demo-shop","count":"1"}`, rec)
	stop := make(chan struct{})

	defer close(stop)

	go func() {
		for {
			select {
			case i := <-rec.waits:
				run.Confirm(i)
			case <-stop:
				return
			}
		}
	}()

	res, msg := rec.wait(t)
	if msg != "" || !res.OK {
		t.Fatalf("result = %+v %q", res, msg)
	}

	if req := lastActionRequest(f, http.MethodPatch); !strings.Contains(req.path, "demo-shop") || !strings.Contains(req.body, "restartedAt") {
		t.Errorf("last patch = %+v, want the restart in the namespace given", req)
	}
}

func TestRunbookDemoPlansWithValuesAndRefusesTheRun(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	out, err := RunbookPlanWith(cfg, "", "", restartAnywhere, `{"ns":"demo-shop"}`, "")
	if err != nil || !strings.Contains(out, `"namespace":"demo-shop"`) {
		t.Fatalf("plan = %s %v", out, err)
	}

	rec := newRunbookRecorder()
	StartRunbookWith(cfg, "", "", restartAnywhere, "", `{"ns":"demo-shop"}`, rec)

	if _, msg := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo run: %q", msg)
	}
}

// A parameter in a wait step: the file loads before its values are known, then they reach it.
func TestRunbookValuesReachAWaitStep(t *testing.T) {
	src := `schemaVersion: ichor.dev/runbook/v1alpha1
name: Wait for it
params:
  - {name: ns, kind: namespace}
  - {name: seconds, kind: int}
  - {name: every, kind: int}
  - {name: least, kind: int}
steps:
  - id: wait
    action: wait
    target: {namespace: "${ns}", name: api}
    params: {until: podsRunning, timeoutSeconds: "${seconds}", intervalSeconds: "${every}", minReplicas: "${least}"}
`

	if _, err := loadRunbook(src); err != nil {
		t.Fatalf("draft: %v", err)
	}

	rb, err := loadRunbookWith(src, map[string]string{"ns": "demo-shop", "seconds": "90", "every": "10", "least": "2"})
	if err != nil {
		t.Fatal(err)
	}

	p := rb.Steps[0].Params
	if rb.Steps[0].Target.Namespace != "demo-shop" || *p.TimeoutSeconds != 90 || *p.IntervalSeconds != 10 || *p.MinReplicas != 2 {
		t.Errorf("step = %+v %+v", rb.Steps[0].Target, p)
	}

	if _, err := loadRunbookWith(src, map[string]string{"ns": "a", "seconds": "0", "every": "10", "least": "2"}); err == nil ||
		!strings.Contains(err.Error(), "step wait: params.timeoutSeconds") {
		t.Errorf("a value is checked like the file: %v", err)
	}
}
