package ichorgo

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRunbookWaits makes a wait step's seconds milliseconds.
func fastRunbookWaits(t *testing.T) {
	t.Helper()

	saved := runbookSecond
	runbookSecond = time.Millisecond

	t.Cleanup(func() { runbookSecond = saved })
}

const conditionHeader = "schemaVersion: ichor.dev/runbook/v1alpha1\nname: Conditions\nsteps:\n"

// conditionKube is an API server with demo-shop/ingest-api (3 replicas wanted, available
// counting up one per read from availableFrom), two running pods, and a node.
func conditionKube(t *testing.T, availableFrom int32, cordoned bool) (*fakeKubeAPI, string) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/demo-shop/pods": `{"items":[
			{"metadata":{"name":"a"},"status":{"phase":"Running"}},
			{"metadata":{"name":"b"},"status":{"phase":"Running"}},
			{"metadata":{"name":"c","deletionTimestamp":"2026-01-01T00:00:00Z"},"status":{"phase":"Running"}},
			{"metadata":{"name":"d"},"status":{"phase":"Pending"}}]}`,
		"GET /api/v1/nodes": `{"items":[{"metadata":{"name":"worker-1"},"spec":{"unschedulable":` + map[bool]string{true: "true", false: "false"}[cordoned] + `},
			"status":{"conditions":[{"type":"Ready","status":"True"}],"addresses":[{"type":"InternalIP","address":"10.5.0.5"}]}}]}`,
	})

	var reads atomic.Int32

	f.answerFunc = func(method, path, body string) (string, bool) {
		switch {
		case path == ssarPath:
			return `{"status":{"allowed":true}}`, true
		case method == "GET" && path == "/apis/apps/v1/namespaces/demo-shop/deployments/ingest-api":
			available := max(min(availableFrom+reads.Add(1)-1, 3), 0)

			return `{"metadata":{"generation":2},"spec":{"replicas":3,"selector":{"matchLabels":{"app":"ingest"}}},
				"status":{"observedGeneration":2,"availableReplicas":` + itoa32(available) + `}}`, true
		}

		return "", false
	}

	return f, f.kubeconfigFor(f.URL)
}

func itoa32(n int32) string {
	b, _ := json.Marshal(n)

	return string(b)
}

func TestRunbookConditionLoadErrors(t *testing.T) {
	cases := map[string]struct{ step, want string }{
		"no until":            {`{id: w, action: wait, target: {namespace: a, name: b}}`, "wait needs params.until"},
		"unknown until":       {`{id: w, action: wait, target: {namespace: a, name: b}, params: {until: green}}`, "wait needs params.until"},
		"timeout too long":    {`{id: w, action: wait, target: {namespace: a, name: b}, params: {until: available, timeoutSeconds: 3600}}`, "timeoutSeconds"},
		"interval too short":  {`{id: w, action: wait, target: {namespace: a, name: b}, params: {until: available, intervalSeconds: 1}}`, "intervalSeconds"},
		"minReplicas alone":   {`{id: w, action: assert, target: {namespace: a, name: b}, params: {until: available, minReplicas: 2}}`, "minReplicas goes with"},
		"minReplicas zero":    {`{id: w, action: assert, target: {namespace: a, name: b}, params: {until: podsRunning, minReplicas: 0}}`, "at least 1"},
		"timeout on assert":   {`{id: w, action: assert, target: {namespace: a, name: b}, params: {until: available, timeoutSeconds: 10}}`, "assert takes no parameter \"timeoutSeconds\""},
		"node for a workload": {`{id: w, action: wait, target: {node: 10.5.0.5}, params: {until: available}}`, "takes no target.node"},
		"workload for a node": {`{id: w, action: wait, target: {namespace: a, name: b}, params: {until: nodeReady}}`, "no target node"},
		"named cluster":       {`{id: w, action: assert, target: {kind: Cluster, name: x}, params: {until: etcdHealthy}}`, "target.kind Cluster only"},
		"bad kind":            {`{id: w, action: wait, target: {kind: CronJob, namespace: a, name: b}, params: {until: available}}`, "target.kind \"CronJob\""},
	}

	for name, c := range cases {
		_, err := loadRunbook(conditionHeader + "  - " + c.step + "\n")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
}

func TestRunbookConditionPlan(t *testing.T) {
	_, cfg := conditionKube(t, 3, false)

	src := conditionHeader + `  - {id: wait-up, action: wait, target: {namespace: demo-shop, name: ingest-api}, params: {until: available}}
  - {id: rolled, action: wait, target: {kind: StatefulSet, namespace: demo-shop, name: db}, params: {until: rolloutDone, timeoutSeconds: 90}}
  - {id: pods, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: podsRunning, minReplicas: 2}}
  - {id: node, action: wait, target: {node: 10.5.0.5}, params: {until: nodeSchedulable, timeoutSeconds: 600}}
  - {id: etcd, action: assert, target: {kind: Cluster}, params: {until: etcdHealthy}}
  - {id: restart, action: rolloutRestart, target: {namespace: demo-shop, name: ingest-api}}
`

	p := planOf(t, cfg, "admin@test", src)
	if len(p.Steps) != 6 {
		t.Fatalf("plan = %+v", p)
	}

	want := []struct {
		detail  string
		timeout int
	}{
		{"wait up to 5 min for ingest-api to be available", 300},
		{"wait up to 1 min 30 s for the rollout of db to finish", 90},
		{"check that at least 2 pods of ingest-api run", 0},
		{"wait up to 10 min for node 10.5.0.5 to be Ready and schedulable", 600},
		{"check that every etcd member is healthy", 0},
		{"", 0},
	}

	for i, w := range want {
		s := p.Steps[i]
		if s.Detail != w.detail || s.TimeoutSeconds != w.timeout || s.Destructive != (s.Action == "rolloutRestart") {
			t.Errorf("step %s = %+v, want %q / %d", s.ID, s, w.detail, w.timeout)
		}
	}

	if p.Steps[0].Target.Kind != "Deployment" || p.Steps[4].Target.Kind != clusterKind {
		t.Errorf("default kinds: %+v / %+v", p.Steps[0].Target, p.Steps[4].Target)
	}

	// On a kubeconfig cluster the Kubernetes reads stay allowed, etcd needs the talosconfig.
	if p.Steps[3].Denied != "" || !strings.Contains(p.Steps[4].Denied, "talosconfig") {
		t.Errorf("kubeconfig gate: node %q, etcd %q", p.Steps[3].Denied, p.Steps[4].Denied)
	}
}

func TestRunbookConditionRoles(t *testing.T) {
	rb, err := loadRunbook(conditionHeader + `  - {id: etcd, action: assert, target: {}, params: {until: etcdHealthy}}
  - {id: node, action: assert, target: {node: 10.5.0.5}, params: {until: nodeReady}}
`)
	if err != nil {
		t.Fatal(err)
	}

	reader := &runbookGate{roles: []string{"os:reader"}, rolesKnown: true, access: map[string]kubeActionAccess{}}

	etcd, node := rb.Steps[0], rb.Steps[1]
	if d := reader.denied(etcd, runbookActions["assert"].of(etcd)); d != "" {
		t.Errorf("os:reader etcd check refused: %q", d)
	}

	if d := reader.denied(node, runbookActions["assert"].of(node)); !strings.Contains(d, adminRole) {
		t.Errorf("os:reader node check through the Talos kubeconfig: %q", d)
	}
}

func TestRunbookConditionReadsRefused(t *testing.T) {
	f, cfg := conditionKube(t, 3, false)
	f.answerFunc = func(_, path, body string) (string, bool) {
		if path != ssarPath {
			return "", false
		}

		var review ssarRequest
		_ = json.Unmarshal([]byte(body), &review)

		allowed := review.Spec.ResourceAttributes.Resource != "pods"

		return `{"status":{"allowed":` + map[bool]string{true: "true", false: "false"}[allowed] + `,"reason":"RBAC: no rule"}}`, true
	}

	p := planOf(t, cfg, "admin@test", conditionHeader+`  - {id: a, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: available}}
  - {id: b, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: podsRunning}}
`)

	if !p.Steps[0].Allowed || p.Steps[1].Allowed || !strings.Contains(p.Steps[1].Denied, "list pods in namespace demo-shop") {
		t.Errorf("steps = %+v", p.Steps)
	}
}

func TestRunbookWaitPollsUntilTheConditionHolds(t *testing.T) {
	withDataDir(t)
	fastRunbookWaits(t)

	_, cfg := conditionKube(t, 1, false)
	rec := newRunbookRecorder()
	startConfirmed(t, cfg, "admin@test", conditionHeader+`  - {id: up, action: wait, target: {namespace: demo-shop, name: ingest-api}, params: {until: available, timeoutSeconds: 60}}
`, "", rec)

	res, msg := rec.wait(t)
	if msg != "" || !res.OK || res.Steps[0].Message != "3/3 available" {
		t.Fatalf("result = %+v, error %q", res, msg)
	}

	var observed []string

	for len(rec.steps) > 0 {
		if s := <-rec.steps; s.State == runbookRunning && s.Message != "" {
			observed = append(observed, s.Message)
		}
	}

	if strings.Join(observed, "|") != "1/3 available|2/3 available" {
		t.Errorf("progress = %q", observed)
	}
}

func TestRunbookWaitAndAssertFailures(t *testing.T) {
	cases := map[string]struct {
		step, want string
	}{
		"timeout": {`{id: s, action: wait, target: {namespace: demo-shop, name: ingest-api}, params: {until: available, timeoutSeconds: 10}}`,
			"timed out after 10 s: 0/3 available"},
		"assert false": {`{id: s, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: podsRunning, minReplicas: 99}}`,
			"check failed: 2 running, want 99"},
		"missing workload": {`{id: s, action: wait, target: {namespace: demo-shop, name: gone}, params: {until: available}}`,
			"Deployment demo-shop/gone does not exist"},
		"cordoned": {`{id: s, action: assert, target: {node: 10.5.0.5}, params: {until: nodeSchedulable}}`,
			"check failed: Ready, cordoned"},
		"unknown node": {`{id: s, action: assert, target: {node: worker-9}, params: {until: nodeReady}}`,
			"check failed: node worker-9 is not registered"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			withDataDir(t)
			fastRunbookWaits(t)

			// availableFrom -100: never more than 0 available within the test.
			_, cfg := conditionKube(t, -100, true)
			rec := newRunbookRecorder()
			startConfirmed(t, cfg, "admin@test", conditionHeader+"  - "+c.step+"\n  - {id: next, action: assert, target: {node: worker-1}, params: {until: nodeReady}}\n", "", rec)

			res, msg := rec.wait(t)
			if msg != "" || res.OK || len(res.Steps) != 2 {
				t.Fatalf("result = %+v, error %q", res, msg)
			}

			if first := res.Steps[0]; first.State != runbookFailed || first.Message != c.want {
				t.Errorf("step = %+v, want %q", first, c.want)
			}

			if res.Steps[1].State != runbookSkipped {
				t.Errorf("the step after a failed check ran: %+v", res.Steps[1])
			}
		})
	}
}

func TestRunbookAssertContinueOnError(t *testing.T) {
	withDataDir(t)

	_, cfg := conditionKube(t, 3, false)
	rec := newRunbookRecorder()
	startConfirmed(t, cfg, "admin@test", conditionHeader+`  - {id: pods, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: podsRunning, minReplicas: 99}, continueOnError: true}
  - {id: node, action: assert, target: {node: worker-1}, params: {until: nodeReady}}
  - {id: all, action: assert, target: {namespace: demo-shop, name: ingest-api}, params: {until: podsRunning, minReplicas: 2}}
`, "", rec)

	res, _ := rec.wait(t)
	if res.OK || res.Steps[0].State != runbookFailed || res.Steps[1].State != runbookOK || res.Steps[1].Message != "Ready" ||
		res.Steps[2].State != runbookOK || res.Steps[2].Message != "2 running, want 2" {
		t.Errorf("steps = %+v", res.Steps)
	}
}

func TestRunbookWaitStops(t *testing.T) {
	withDataDir(t)

	_, cfg := conditionKube(t, -100, false)
	rec := newRunbookRecorder()
	run := startConfirmed(t, cfg, "admin@test", conditionHeader+`  - {id: up, action: wait, target: {namespace: demo-shop, name: ingest-api}, params: {until: available}}
`, "", rec)

	// The first check reports, then the wait sleeps its 5 s interval: Stop ends it.
	for s := range rec.steps {
		if s.Message != "" {
			break
		}
	}

	run.Stop()

	res, _ := rec.wait(t)
	if res.OK || res.Steps[0].State != runbookFailed || !strings.HasPrefix(res.Steps[0].Message, "stopped while waiting: 0/3 available") {
		t.Errorf("steps = %+v", res.Steps)
	}
}

func TestEtcdAllHealthy(t *testing.T) {
	healthy := etcdNodeStatus{Node: "a"}
	broken := etcdNodeStatus{Node: "b", Errors: []string{"slow"}}

	cases := []struct {
		ov       etcdOverview
		ok       bool
		observed string
	}{
		{etcdOverview{Statuses: []etcdNodeStatus{healthy, healthy}}, true, "2/2 members healthy"},
		{etcdOverview{Statuses: []etcdNodeStatus{healthy, broken}}, false, "1/2 members healthy"},
		{etcdOverview{Statuses: []etcdNodeStatus{healthy}, Alarms: []etcdAlarm{{Alarm: "NOSPACE"}}}, false, "1/1 members healthy, alarm NOSPACE"},
		{etcdOverview{}, false, "0/0 members healthy"},
	}

	for _, c := range cases {
		if ok, observed := etcdAllHealthy(c.ov); ok != c.ok || observed != c.observed {
			t.Errorf("%+v: %v %q, want %v %q", c.ov, ok, observed, c.ok, c.observed)
		}
	}
}

func TestWaitDuration(t *testing.T) {
	for seconds, want := range map[int]string{45: "45 s", 60: "1 min", 90: "1 min 30 s", 1800: "30 min"} {
		if got := waitDuration(seconds); got != want {
			t.Errorf("%d: %q, want %q", seconds, got, want)
		}
	}
}

func TestRunbookRestartWaitFixtureDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)
	src := readRunbookFixture(t, "restart-wait.yaml")

	p := planOf(t, cfg, "", src)
	if len(p.Steps) != 3 || p.Steps[1].Detail != "wait up to 5 min for the rollout of ingest-api to finish" || p.Steps[2].Detail != "check that ingest-api is available" {
		t.Fatalf("plan = %+v", p)
	}

	for _, s := range p.Steps {
		if !s.Allowed {
			t.Errorf("demo step refused: %+v", s)
		}
	}

	rec := newRunbookRecorder()
	StartRunbook(cfg, "", "", src, "", rec)

	if _, msg := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo run: %q, want the demo refusal", msg)
	}
}
