package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	helmTestJobs    = "/apis/batch/v1/namespaces/web/jobs"
	helmTestPreJob  = helmTestJobs + "/site-migrate"
	helmTestPostJob = helmTestJobs + "/site-notify"

	helmTestJobRunning  = `{"kind":"Job","status":{"active":1}}`
	helmTestJobComplete = `{"kind":"Job","status":{"conditions":[{"type":"Complete","status":"True"}]}}`
	helmTestJobFailed   = `{"kind":"Job","status":{"conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded","message":"Job has reached the specified backoff limit"}]}}`
)

func helmTestHookJob(name string) string {
	return "apiVersion: batch/v1\nkind: Job\nmetadata:\n  name: " + name + "\n  annotations:\n    helm.sh/hook: x\n" +
		"spec:\n  template:\n    spec:\n      restartPolicy: Never\n      containers:\n      - name: run\n        image: site:v1\n"
}

// helmHooksAPI is helmRollbackAPI whose target revision (1) has a pre-rollback Job (weight
// -5, no delete policy: before-hook-creation) and a post-rollback Job with postPolicies.
func helmHooksAPI(t *testing.T, postPolicies ...string) *fakeKubeAPI {
	t.Helper()

	interval := helmHookPollInterval
	helmHookPollInterval = time.Millisecond

	t.Cleanup(func() { helmHookPollInterval = interval })

	post := []any{}
	for _, p := range postPolicies {
		post = append(post, p)
	}

	f := helmRollbackAPI(t, map[string]any{"hooks": []any{
		map[string]any{"name": "site-notify", "kind": "Job", "path": "site/templates/notify.yaml", "manifest": helmTestHookJob("site-notify"),
			"events": []any{"post-rollback"}, "delete_policies": post, "last_run": map[string]any{"phase": "Succeeded"}},
		map[string]any{"name": "site-migrate", "kind": "Job", "manifest": helmTestHookJob("site-migrate"), "events": []any{"pre-rollback", "pre-upgrade"}, "weight": -5},
		map[string]any{"name": "site-test", "kind": "Pod", "manifest": "kind: Pod", "events": []any{"test"}},
	}})

	f.answerWith("GET /apis/batch/v1", 200, `{"groupVersion":"batch/v1","resources":[{"name":"jobs","kind":"Job","namespaced":true,"verbs":["get","create","delete"]}]}`)
	f.answerWith("POST "+helmTestJobs, 200, `{}`)
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobRunning)
	f.answerWith("GET "+helmTestPostJob, 200, helmTestJobComplete)
	f.answerWith("DELETE "+helmTestPostJob, 200, `{}`)

	// The pre-rollback Job runs once read, then completes.
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == helmTestPreJob {
			f.answerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
		}
	}

	return f
}

func TestHelmRollbackPlansHooks(t *testing.T) {
	f := helmHooksAPI(t, helmHookSucceeded)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 0, helmRollbackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	want := []helmHook{
		{Name: "site-migrate", Kind: "Job", Event: helmHookPreRollback, Weight: -5, DeletePolicies: []string{helmHookBeforeCreation}},
		{Name: "site-notify", Kind: "Job", Event: helmHookPostRollback, DeletePolicies: []string{helmHookSucceeded}},
	}
	if !reflect.DeepEqual(rb.plan.Hooks, want) || len(rb.plan.Blockers) != 0 {
		t.Fatalf("hooks %+v blockers %v", rb.plan.Hooks, rb.plan.Blockers)
	}

	// Hook objects are no change of the release.
	for _, c := range rb.plan.Changes {
		if c.Kind == "Job" {
			t.Fatalf("a hook is a change: %+v", c)
		}
	}

	for _, s := range rb.steps {
		if s.obj.Kind == "Job" {
			t.Fatalf("a hook is a step: %+v", s.obj)
		}
	}

	dryRuns := 0

	for _, r := range f.recorded() {
		if r.method != "GET" && !strings.Contains(r.query, "dryRun=All") {
			t.Fatalf("plan wrote: %+v", r)
		}

		if strings.HasPrefix(r.path, helmTestJobs) {
			dryRuns++
		}
	}

	if dryRuns != 4 { // a create and a delete per hook
		t.Fatalf("%d hook dry runs", dryRuns)
	}

	out, err := json.Marshal(rb.plan)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(out), `"hooks":[{"name":"site-migrate","kind":"Job","event":"pre-rollback","weight":-5,"deletePolicies":["before-hook-creation"],"error":""}`) {
		t.Errorf("plan JSON %s", out)
	}
}

func TestHelmRollbackRunsHooks(t *testing.T) {
	f := helmHooksAPI(t, helmHookSucceeded)

	_, writes, err := helmRollbackWrites(t, f, "")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST " + helmTestSecrets,
		"DELETE " + helmTestPreJob, // before-hook-creation: nothing there
		"POST " + helmTestJobs,
		"POST /api/v1/namespaces/web/serviceaccounts",
		"PATCH " + helmTestCM,
		"PATCH " + helmTestDeploy,
		"DELETE " + helmTestService,
		"POST " + helmTestJobs,
		"DELETE " + helmTestPostJob, // hook-succeeded
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v1",
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3",
	}
	if got := writeKeys(writes); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes:\n got  %v\n want %v", got, want)
	}

	if b := writes[2].body; !strings.Contains(b, `"name":"site-migrate"`) || strings.Contains(b, "meta.helm.sh/release-name") || strings.Contains(writes[2].query, "dryRun") {
		t.Errorf("hook created as %s?%s", b, writes[2].query)
	}

	// The pre-rollback Job was read until it completed, before any change.
	reads := 0

	for _, r := range f.recorded() {
		if r.method == "GET" && r.path == helmTestPreJob {
			reads++
		}

		if r.method == "PATCH" && reads < 2 {
			t.Fatal("a change ran before the pre-rollback hook completed")
		}
	}

	if !strings.Contains(writes[len(writes)-1].body, `"status":"deployed"`) {
		t.Errorf("v3 %s", writes[len(writes)-1].body)
	}
}

func TestHelmRollbackPreHookFails(t *testing.T) {
	f := helmHooksAPI(t, helmHookSucceeded)
	f.beforeAnswer = nil
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobFailed)
	f.answerWith("DELETE "+helmTestSecrets+"/sh.helm.release.v1.site.v1", 200, `{}`)

	_, writes, err := helmRollbackWrites(t, f, `{"historyMax":2}`)
	if err == nil || !strings.Contains(err.Error(), "pre-rollback hook Job site-migrate failed: Job failed: BackoffLimitExceeded") ||
		!strings.Contains(err.Error(), "before any change, revision 3 recorded as failed") {
		t.Fatalf("err %v", err)
	}

	want := []string{
		"POST " + helmTestSecrets,
		"DELETE " + helmTestPreJob,
		"POST " + helmTestJobs,
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3",
	}
	if got := writeKeys(writes); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes:\n got  %v\n want %v", got, want)
	}

	v3 := helmSecretPayload(t, writes[3].body, "data", "release")["info"].(map[string]any)
	if v3["status"] != helmStatusFailed || !strings.Contains(v3["description"].(string), "pre-rollback hook Job site-migrate failed") {
		t.Errorf("v3 %v", v3)
	}
}

func TestHelmRollbackPostHookFails(t *testing.T) {
	f := helmHooksAPI(t, helmHookSucceeded, helmHookFailed)
	f.answerWith("GET "+helmTestPostJob, 200, helmTestJobFailed)
	f.answerWith("DELETE "+helmTestSecrets+"/sh.helm.release.v1.site.v1", 200, `{}`)
	f.answerWith("PUT "+helmTestSecrets+"/sh.helm.release.v1.site.v2", 200, `{}`)

	_, writes, err := helmRollbackWrites(t, f, `{"historyMax":2}`)
	if err == nil || !strings.Contains(err.Error(), "revision 3 recorded as failed: post-rollback hook Job site-notify failed") || strings.Contains(err.Error(), "site.v2") {
		t.Fatalf("err %v", err)
	}

	got := writeKeys(writes)
	tail := []string{
		"DELETE " + helmTestService,
		"POST " + helmTestJobs,
		"DELETE " + helmTestPostJob, // hook-failed
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v2",
		"PUT " + helmTestSecrets + "/sh.helm.release.v1.site.v3",
	}

	if len(got) < len(tail) || !reflect.DeepEqual(got[len(got)-len(tail):], tail) {
		t.Fatalf("writes %v", got)
	}

	// The changes stay, the history is not pruned, v3 failed and the current v2 superseded.
	if !strings.Contains(writes[len(writes)-1].body, `"status":"failed"`) || !strings.Contains(writes[len(writes)-2].body, `"status":"superseded"`) {
		t.Errorf("statuses %s / %s", writes[len(writes)-2].body, writes[len(writes)-1].body)
	}
}

// Through the exported call, the hook's failure keeps its message past the Kubernetes call
// wrapper.
func TestKubeHelmRollbackHookFailureNamesTheHook(t *testing.T) {
	f := helmHooksAPI(t)
	f.beforeAnswer = nil
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobFailed)

	err := KubeHelmRollback(f.kubeconfigFor(f.URL), "admin@test", "", "web", "site", 0, "")
	if err == nil || !strings.HasPrefix(err.Error(), "rollback stopped before any change") || !strings.Contains(err.Error(), "Job site-migrate failed") {
		t.Fatalf("err %v", err)
	}
}

func TestHelmRollbackRefusedHookBlocks(t *testing.T) {
	f := helmHooksAPI(t)
	f.answerWith("POST "+helmTestJobs, 403, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"jobs.batch is forbidden"}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 0, helmRollbackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rb.plan.Hooks) != 2 || !strings.Contains(rb.plan.Hooks[0].Error, "forbidden") || len(rb.plan.Blockers) != 1 ||
		!strings.Contains(rb.plan.Blockers[0], "2 of the hooks") || !reflect.DeepEqual(rb.plan.Hooks[1].DeletePolicies, []string{helmHookBeforeCreation}) {
		t.Fatalf("plan hooks %+v blockers %v", rb.plan.Hooks, rb.plan.Blockers)
	}

	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()

	if _, err := runHelmRollback(context.Background(), k, rb, time.Now()); err == nil {
		t.Fatal("ran with a refused hook")
	}

	for _, r := range f.recorded() {
		if r.method != "GET" {
			t.Fatalf("wrote %s %s", r.method, r.path)
		}
	}
}

// An object already named as the hook is fine when before-hook-creation deletes it first;
// the run waits for it to be gone before creating the hook.
func TestHelmRollbackHookReplacesLeftover(t *testing.T) {
	f := helmHooksAPI(t, helmHookSucceeded)
	f.answerWith("POST "+helmTestJobs, 409, `{"kind":"Status","status":"Failure","reason":"AlreadyExists","code":409,"message":"jobs.batch \"site-migrate\" already exists"}`)
	f.answerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
	f.answerWith("DELETE "+helmTestPreJob, 200, `{}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	rb, err := prepareHelmRollback(context.Background(), k, "web", "site", 0, helmRollbackOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// site-notify has no before-hook-creation: its conflict is an error.
	if rb.plan.Hooks[0].Error != "" || !strings.Contains(rb.plan.Hooks[1].Error, "already exists") {
		t.Fatalf("hooks %+v", rb.plan.Hooks)
	}

	// The leftover goes once deleted; then the create succeeds.
	deleted := false
	f.beforeAnswer = func(r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == helmTestPreJob && !strings.Contains(r.URL.RawQuery, "dryRun"):
			deleted = true
			f.mu.Lock()
			delete(f.answers, "GET "+helmTestPreJob)
			f.mu.Unlock()
		case r.Method == http.MethodGet && r.URL.Path == helmTestPreJob && deleted:
			f.answerWith("POST "+helmTestJobs, 200, `{}`)
			f.answerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
		}
	}

	f.mu.Lock()
	f.requests = nil
	f.mu.Unlock()

	if err := runHelmHooks(context.Background(), k, rb.preHooks); err != nil {
		t.Fatal(err)
	}

	var seq []string

	for _, r := range f.recorded() {
		if strings.HasPrefix(r.path, helmTestJobs) {
			seq = append(seq, r.method+" "+r.path)
		}
	}

	want := []string{"DELETE " + helmTestPreJob, "GET " + helmTestPreJob, "POST " + helmTestJobs, "GET " + helmTestPreJob}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("sequence:\n got  %v\n want %v", seq, want)
	}
}

func TestHelmEventHooksOrder(t *testing.T) {
	rel := helmRelease{Hooks: []helmReleaseHook{
		{Name: "c", Events: []string{helmHookPreRollback}, Weight: 1},
		{Name: "b", Events: []string{helmHookPreRollback}},
		{Name: "a", Events: []string{helmHookPreRollback}, Weight: 1},
		{Name: "z", Events: []string{helmHookPreRollback}, Weight: -3},
		{Name: "post", Events: []string{helmHookPostRollback}},
	}}

	var names []string
	for _, h := range helmEventHooks(rel, helmHookPreRollback) {
		names = append(names, h.Name)
	}

	if !reflect.DeepEqual(names, []string{"z", "b", "a", "c"}) {
		t.Fatalf("order %v", names)
	}
}

func TestHelmHookDone(t *testing.T) {
	for _, c := range []struct {
		kind, live string
		done, ok   bool
	}{
		{"Job", helmTestJobRunning, false, true},
		{"Job", helmTestJobComplete, true, true},
		{"Job", helmTestJobFailed, true, false},
		{"Job", `{"status":{"conditions":[{"type":"Failed","status":"False"}]}}`, false, true},
		{"Pod", `{"status":{"phase":"Running"}}`, false, true},
		{"Pod", `{"status":{"phase":"Succeeded"}}`, true, true},
		{"Pod", `{"status":{"phase":"Failed","reason":"Evicted"}}`, true, false},
	} {
		var live map[string]any
		if err := json.Unmarshal([]byte(c.live), &live); err != nil {
			t.Fatal(err)
		}

		done, err := helmHookDone(c.kind, live)
		if done != c.done || (err == nil) != c.ok {
			t.Errorf("%s %s: done %t err %v", c.kind, c.live, done, err)
		}
	}
}

// A release without hooks plans an empty list, never null.
func TestHelmRollbackWithoutHooks(t *testing.T) {
	f := helmRollbackAPI(t, nil)

	plan, _, err := helmRollbackWrites(t, f, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(out), `"hooks":[]`) {
		t.Fatalf("plan %s", out)
	}
}

func TestKubeHelmRollbackDemoHook(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeHelmRollbackPlan(cfg, "", "", "ingress-nginx", "ingress-nginx", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	var p helmRollbackPlan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if len(p.Hooks) != 1 || p.Hooks[0].Event != helmHookPreRollback || p.Hooks[0].Kind != "Job" || p.Hooks[0].Weight != -5 {
		t.Fatalf("demo hooks %+v", p.Hooks)
	}

	if err := KubeHelmRollback(cfg, "", "", "ingress-nginx", "ingress-nginx", 6, ""); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo rollback: %v", err)
	}
}
