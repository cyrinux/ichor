package ichorgo

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func readRunbookFixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile("testdata/runbooks/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// runbookKube is an API server where demo-shop/ingest-api can be scaled and restarted, and
// the access reviews answer allowed(attributes).
func runbookKube(t *testing.T, allowed func(ssarAttributes) bool) (*fakeKubeAPI, string) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"PATCH /apis/apps/v1/namespaces/demo-shop/deployments/ingest-api/scale": `{}`,
		"GET /apis/apps/v1/namespaces/demo-shop/deployments/ingest-api":         `{"spec":{}}`,
		"PATCH /apis/apps/v1/namespaces/demo-shop/deployments/ingest-api":       `{}`,
		"GET /apis": `{"groups":[]}`,
		"GET /apis/autoscaling/v2/horizontalpodautoscalers": `{"items":[]}`,
	})
	f.answerFunc = func(method, path, body string) (string, bool) {
		if path != ssarPath {
			return "", false
		}

		var review ssarRequest
		_ = json.Unmarshal([]byte(body), &review)

		ok := allowed == nil || allowed(review.Spec.ResourceAttributes)

		return `{"status":{"allowed":` + map[bool]string{true: "true", false: "false"}[ok] + `,"reason":"RBAC: no rule"}}`, true
	}

	return f, f.kubeconfigFor(f.URL)
}

type runbookRecorder struct {
	steps  chan runbookStepState
	done   chan runbookResult
	failed chan string
}

func newRunbookRecorder() *runbookRecorder {
	return &runbookRecorder{steps: make(chan runbookStepState, 100), done: make(chan runbookResult, 1), failed: make(chan string, 1)}
}

func (r *runbookRecorder) OnStep(_ int, js string) {
	var s runbookStepState
	_ = json.Unmarshal([]byte(js), &s)
	r.steps <- s
}

func (r *runbookRecorder) OnDone(js string) {
	var res runbookResult
	_ = json.Unmarshal([]byte(js), &res)
	r.done <- res
}

func (r *runbookRecorder) OnError(msg string) { r.failed <- msg }

func (r *runbookRecorder) wait(t *testing.T) (runbookResult, string) {
	t.Helper()

	select {
	case res := <-r.done:
		return res, ""
	case msg := <-r.failed:
		return runbookResult{}, msg
	case <-time.After(10 * time.Second):
		t.Fatal("the runbook did not end")
	}

	return runbookResult{}, ""
}

func planOf(t *testing.T, cfg, cluster, src string) runbookPlanResult {
	t.Helper()

	out, err := RunbookPlan(cfg, cluster, "", src)
	if err != nil {
		t.Fatal(err)
	}

	var p runbookPlanResult
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return p
}

func TestRunbookPlanListsStepsWithAccess(t *testing.T) {
	_, cfg := runbookKube(t, func(a ssarAttributes) bool { return a.Subresource != "scale" })

	p := planOf(t, cfg, "admin@test", readRunbookFixture(t, "scale-and-restart.yaml"))

	if p.Name != "Restart ingestion" || len(p.Steps) != 2 || len(p.Errors) != 0 {
		t.Fatalf("plan = %+v", p)
	}

	scale, restart := p.Steps[0], p.Steps[1]
	if scale.ID != "scale-down" || !scale.Destructive || scale.Role != adminRole || scale.Allowed || !strings.Contains(scale.Denied, "deployments") {
		t.Errorf("scale step = %+v", scale)
	}

	if restart.Target.Kind != "Deployment" || !restart.Allowed || restart.Denied != "" {
		t.Errorf("restart step = %+v (the kind defaults to Deployment)", restart)
	}
}

func TestRunbookRunsStepsAndRecordsThem(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f, cfg := runbookKube(t, nil)
	rec := newRunbookRecorder()
	StartRunbook(cfg, "admin@test", "", readRunbookFixture(t, "scale-and-restart.yaml"), rec)

	res, msg := rec.wait(t)
	if msg != "" || !res.OK || len(res.Steps) != 2 || res.Steps[0].State != runbookOK || res.Steps[1].State != runbookOK {
		t.Fatalf("result = %+v, error %q", res, msg)
	}

	if req := lastActionRequest(f, http.MethodPatch); !strings.Contains(req.body, "restartedAt") {
		t.Errorf("last patch = %+v, want the restart", req)
	}

	actions := map[string]auditEntry{}
	for _, e := range readAudit(t, "", "") {
		actions[e.Action] = e
	}

	if len(actions) != 3 || actions["scale"].Outcome != auditOK || actions["rollout-restart"].Outcome != auditOK {
		t.Errorf("audit = %+v", actions)
	}

	if e := actions["runbook"]; e.Object != "Restart ingestion" || e.Params != "steps=2 ok=2" || e.Outcome != auditOK {
		t.Errorf("runbook entry = %+v", e)
	}
}

func TestRunbookStopsAtTheFirstFailure(t *testing.T) {
	cases := map[string]struct {
		continueOnError bool
		third           string
	}{
		"stops":             {false, runbookSkipped},
		"continue on error": {true, runbookOK},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			withDataDir(t)

			_, cfg := runbookKube(t, nil)
			src := readRunbookFixture(t, "three-steps.yaml")

			if c.continueOnError {
				src = strings.Replace(src, "name: missing-api}", "name: missing-api}\n    continueOnError: true", 1)
			}

			rec := newRunbookRecorder()
			StartRunbook(cfg, "admin@test", "", src, rec)

			res, msg := rec.wait(t)
			if msg != "" || res.OK || len(res.Steps) != 3 {
				t.Fatalf("result = %+v, error %q", res, msg)
			}

			if res.Steps[0].State != runbookOK || res.Steps[1].State != runbookFailed || res.Steps[1].Message == "" || res.Steps[2].State != c.third {
				t.Errorf("steps = %+v", res.Steps)
			}

			entries := readAudit(t, "", "runbook")
			if len(entries) != 1 || entries[0].Outcome != auditFailed {
				t.Errorf("runbook entry = %+v", entries)
			}
		})
	}
}

func TestRunbookLoadErrorsNameTheStep(t *testing.T) {
	good := readRunbookFixture(t, "scale-and-restart.yaml")

	cases := map[string]struct {
		src, want string
	}{
		"unknown action": {strings.Replace(good, "action: rolloutRestart", "action: deleteEverything", 1), "step restart: unknown action"},
		"unknown field":  {strings.Replace(good, "params: {replicas: 1}", "params: {replicas: 1, force: true}", 1), "step scale-down:"},
		"step field":     {strings.Replace(good, "action: rolloutRestart", "action: rolloutRestart\n    when: always", 1), "step restart:"},
		"no target":      {strings.Replace(good, "target: {namespace: demo-shop, name: ingest-api}", "target: {namespace: demo-shop}", 1), "step restart: no target"},
		"foreign param":  {strings.Replace(good, "target: {namespace: demo-shop, name: ingest-api}\n", "target: {namespace: demo-shop, name: ingest-api}\n    params: {replicas: 3}\n", 1), "step restart: rolloutRestart takes no parameter"},
		"no replicas":    {strings.Replace(good, "params: {replicas: 1}", "", 1), "step scale-down: scale needs params.replicas"},
		"bad kind":       {strings.Replace(good, "kind: Deployment", "kind: CronJob", 1), "step scale-down: target.kind"},
		"node on kube":   {strings.Replace(good, "target: {namespace: demo-shop, name: ingest-api}", "target: {namespace: demo-shop, name: ingest-api, node: 10.0.0.2}", 1), "step restart: a Kubernetes action takes no target.node"},
		"schema":         {strings.Replace(good, "v1alpha1", "v9", 1), "schemaVersion"},
		"duplicate id":   {strings.Replace(good, "id: restart", "id: scale-down", 1), "step scale-down: the id is used twice"},
		"no id":          {strings.Replace(good, "id: restart", "id: ''", 1), "step #2: no id"},
	}

	for name, c := range cases {
		_, err := RunbookPlan("", "", "", c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func TestRunbookNodeSteps(t *testing.T) {
	src := `schemaVersion: ichor.dev/runbook/v1alpha1
name: Nodes
steps:
  - {id: snap, action: etcdSnapshot, target: {node: 10.0.0.2}}
  - {id: reboot, action: nodeReboot, target: {node: 10.0.0.3}}
  - {id: drain, action: nodeMaintenance, target: {node: 10.0.0.4}, params: {action: none}}
`

	rb, err := loadRunbook(src)
	if err != nil {
		t.Fatal(err)
	}

	if rb.Steps[1].Params.Mode != "default" || rb.Steps[2].Params.Action != maintenanceNone {
		t.Errorf("defaults = %+v", rb.Steps)
	}

	reader := &runbookGate{roles: []string{"os:reader"}, rolesKnown: true, access: map[string]kubeActionAccess{}}
	operator := &runbookGate{roles: []string{"os:operator"}, rolesKnown: true, access: map[string]kubeActionAccess{}}
	omni := &runbookGate{access: map[string]kubeActionAccess{}}
	kube := &runbookGate{kube: true, access: map[string]kubeActionAccess{}}

	for _, s := range rb.Steps {
		a := runbookActions[s.Action]

		if d := reader.denied(s, a); !strings.Contains(d, "role") {
			t.Errorf("%s as os:reader: %q", s.ID, d)
		}

		if d := kube.denied(s, a); !strings.Contains(d, "talosconfig") {
			t.Errorf("%s on a kubeconfig: %q", s.ID, d)
		}
	}

	if d := omni.denied(rb.Steps[1], runbookActions["nodeReboot"]); d != "" {
		t.Errorf("unknown roles (Omni) refused a step: %q", d)
	}

	if d := operator.denied(rb.Steps[0], runbookActions["etcdSnapshot"]); d != "" {
		t.Errorf("os:operator snapshot refused: %q", d)
	}

	if d := operator.denied(rb.Steps[2], runbookActions["nodeMaintenance"]); !strings.Contains(d, adminRole) {
		t.Errorf("os:operator maintenance (drains through Kubernetes): %q", d)
	}

	if _, err := loadRunbook(strings.Replace(src, "mode", "x", 1) + "  - {id: bad, action: nodeReboot, target: {node: n}, params: {mode: gentle}}\n"); err == nil {
		t.Error("an unknown reboot mode loaded")
	}
}

func TestRunbookDemoIsPlannedNotRun(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)
	src := readRunbookFixture(t, "scale-and-restart.yaml")

	p := planOf(t, cfg, "", src)
	for _, s := range p.Steps {
		if !s.Allowed {
			t.Errorf("demo step refused: %+v", s)
		}
	}

	rec := newRunbookRecorder()
	StartRunbook(cfg, "", "", src, rec)

	if _, msg := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo run: %q, want the demo refusal", msg)
	}

	if len(rec.steps) != 0 {
		t.Errorf("a demo step ran")
	}
}

func TestRunbookStopSkipsTheRest(t *testing.T) {
	withDataDir(t)

	f, cfg := runbookKube(t, nil)
	// Stop while the first step's scale is answered.
	release := make(chan struct{})
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodPatch {
			<-release
		}
	}

	rec := newRunbookRecorder()
	run := StartRunbook(cfg, "admin@test", "", readRunbookFixture(t, "three-steps.yaml"), rec)

	if s := <-rec.steps; s.State != runbookRunning {
		t.Fatalf("first state = %+v", s)
	}

	run.Stop()
	close(release)

	res, _ := rec.wait(t)
	if res.OK || res.Steps[1].State != runbookSkipped || res.Steps[2].State != runbookSkipped {
		t.Errorf("after Stop: %+v", res.Steps)
	}
}
