package helm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"

	"github.com/cyrinux/ichor/go/kube/kubetest"
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
func helmHooksAPI(t *testing.T, postPolicies ...string) *kubetest.FakeAPI {
	t.Helper()

	interval := HookPollInterval
	HookPollInterval = time.Millisecond

	t.Cleanup(func() { HookPollInterval = interval })

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

	f.AnswerWith("GET /apis/batch/v1", 200, `{"groupVersion":"batch/v1","resources":[{"name":"jobs","kind":"Job","namespaced":true,"verbs":["get","create","delete"]}]}`)
	f.AnswerWith("POST "+helmTestJobs, 200, `{}`)
	f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobRunning)
	f.AnswerWith("GET "+helmTestPostJob, 200, helmTestJobComplete)
	f.AnswerWith("DELETE "+helmTestPostJob, 200, `{}`)

	// The pre-rollback Job runs once read, then completes.
	f.BeforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == helmTestPreJob {
			f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
		}
	}

	return f
}

func TestHelmRollbackPlansHooks(t *testing.T) {
	f := helmHooksAPI(t, HookSucceeded)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{})
	if err != nil {
		t.Fatal(err)
	}

	want := []Hook{
		{Name: "site-migrate", Kind: "Job", Event: HookPreRollback, Weight: -5, DeletePolicies: []string{HookBeforeCreation}},
		{Name: "site-notify", Kind: "Job", Event: HookPostRollback, DeletePolicies: []string{HookSucceeded}},
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

	for _, r := range f.Recorded() {
		if r.Method != "GET" && !strings.Contains(r.Query, "dryRun=All") {
			t.Fatalf("plan wrote: %+v", r)
		}

		if strings.HasPrefix(r.Path, helmTestJobs) {
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
	f := helmHooksAPI(t, HookSucceeded)

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

	if b := writes[2].Body; !strings.Contains(b, `"name":"site-migrate"`) || strings.Contains(b, "meta.helm.sh/release-name") || strings.Contains(writes[2].Query, "dryRun") {
		t.Errorf("hook created as %s?%s", b, writes[2].Query)
	}

	// The pre-rollback Job was read until it completed, before any change.
	reads := 0

	for _, r := range f.Recorded() {
		if r.Method == "GET" && r.Path == helmTestPreJob {
			reads++
		}

		if r.Method == "PATCH" && reads < 2 {
			t.Fatal("a change ran before the pre-rollback hook completed")
		}
	}

	if !strings.Contains(writes[len(writes)-1].Body, `"status":"deployed"`) {
		t.Errorf("v3 %s", writes[len(writes)-1].Body)
	}
}

func TestHelmRollbackPreHookFails(t *testing.T) {
	f := helmHooksAPI(t, HookSucceeded)
	f.BeforeAnswer = nil
	f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobFailed)
	f.AnswerWith("DELETE "+helmTestSecrets+"/sh.helm.release.v1.site.v1", 200, `{}`)

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

	v3 := helmSecretPayload(t, writes[3].Body, "data", "release")["info"].(map[string]any)
	if v3["status"] != StatusFailed || !strings.Contains(v3["description"].(string), "pre-rollback hook Job site-migrate failed") {
		t.Errorf("v3 %v", v3)
	}
}

func TestHelmRollbackPostHookFails(t *testing.T) {
	f := helmHooksAPI(t, HookSucceeded, HookFailed)
	f.AnswerWith("GET "+helmTestPostJob, 200, helmTestJobFailed)
	f.AnswerWith("DELETE "+helmTestSecrets+"/sh.helm.release.v1.site.v1", 200, `{}`)
	f.AnswerWith("PUT "+helmTestSecrets+"/sh.helm.release.v1.site.v2", 200, `{}`)

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
	if !strings.Contains(writes[len(writes)-1].Body, `"status":"failed"`) || !strings.Contains(writes[len(writes)-2].Body, `"status":"superseded"`) {
		t.Errorf("statuses %s / %s", writes[len(writes)-2].Body, writes[len(writes)-1].Body)
	}
}

func TestHelmRollbackRefusedHookBlocks(t *testing.T) {
	f := helmHooksAPI(t)
	f.AnswerWith("POST "+helmTestJobs, 403, `{"kind":"Status","status":"Failure","reason":"Forbidden","code":403,"message":"jobs.batch is forbidden"}`)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rb.plan.Hooks) != 2 || !strings.Contains(rb.plan.Hooks[0].Error, "forbidden") || len(rb.plan.Blockers) != 1 ||
		!strings.Contains(rb.plan.Blockers[0], "2 of the hooks") || !reflect.DeepEqual(rb.plan.Hooks[1].DeletePolicies, []string{HookBeforeCreation}) {
		t.Fatalf("plan hooks %+v blockers %v", rb.plan.Hooks, rb.plan.Blockers)
	}

	f.ForgetRequests()

	if _, err := runRollback(context.Background(), k, rb, time.Now()); err == nil {
		t.Fatal("ran with a refused hook")
	}

	for _, r := range f.Recorded() {
		if r.Method != "GET" {
			t.Fatalf("wrote %s %s", r.Method, r.Path)
		}
	}
}

// An object already named as the hook is fine when before-hook-creation deletes it first;
// the run waits for it to be gone before creating the hook.
func TestHelmRollbackHookReplacesLeftover(t *testing.T) {
	f := helmHooksAPI(t, HookSucceeded)
	f.AnswerWith("POST "+helmTestJobs, 409, `{"kind":"Status","status":"Failure","reason":"AlreadyExists","code":409,"message":"jobs.batch \"site-migrate\" already exists"}`)
	f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
	f.AnswerWith("DELETE "+helmTestPreJob, 200, `{}`)

	k := Env{Client: f.KubeClient(t)}

	rb, err := prepareRollback(context.Background(), k, "web", "site", 0, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// site-notify has no before-hook-creation: its conflict is an error.
	if rb.plan.Hooks[0].Error != "" || !strings.Contains(rb.plan.Hooks[1].Error, "already exists") {
		t.Fatalf("hooks %+v", rb.plan.Hooks)
	}

	// The leftover goes once deleted; then the create succeeds.
	deleted := false
	f.BeforeAnswer = func(r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == helmTestPreJob && !strings.Contains(r.URL.RawQuery, "dryRun"):
			deleted = true
			f.Mu.Lock()
			delete(f.Answers, "GET "+helmTestPreJob)
			f.Mu.Unlock()
		case r.Method == http.MethodGet && r.URL.Path == helmTestPreJob && deleted:
			f.AnswerWith("POST "+helmTestJobs, 200, `{}`)
			f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobComplete)
		}
	}

	f.ForgetRequests()

	if err := runHooks(context.Background(), k, rb.preHooks); err != nil {
		t.Fatal(err)
	}

	var seq []string

	for _, r := range f.Recorded() {
		if strings.HasPrefix(r.Path, helmTestJobs) {
			seq = append(seq, r.Method+" "+r.Path)
		}
	}

	want := []string{"DELETE " + helmTestPreJob, "GET " + helmTestPreJob, "POST " + helmTestJobs, "GET " + helmTestPreJob}
	if !reflect.DeepEqual(seq, want) {
		t.Fatalf("sequence:\n got  %v\n want %v", seq, want)
	}
}

func TestHelmEventHooksOrder(t *testing.T) {
	rel := Release{Hooks: []ReleaseHook{
		{Name: "c", Events: []string{HookPreRollback}, Weight: 1},
		{Name: "b", Events: []string{HookPreRollback}},
		{Name: "a", Events: []string{HookPreRollback}, Weight: 1},
		{Name: "z", Events: []string{HookPreRollback}, Weight: -3},
		{Name: "post", Events: []string{HookPostRollback}},
	}}

	var names []string
	for _, h := range eventHooks(rel, HookPreRollback) {
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

		done, err := hookDone(c.kind, live)
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

// A hook Job deleted while waited for (ttlSecondsAfterFinished: 0) is done, as Helm's
// watcher takes watch.Deleted: the rollback goes on and is deployed.
func TestHelmRollbackHookDeletedWhileWaited(t *testing.T) {
	f := helmHooksAPI(t, HookSucceeded)
	f.AnswerWith("GET "+helmTestPreJob, 200, helmTestJobRunning)

	f.BeforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == helmTestPreJob {
			f.Mu.Lock()
			delete(f.Answers, "GET "+helmTestPreJob) // the next read is a 404
			f.Mu.Unlock()
		}
	}

	_, writes, err := helmRollbackWrites(t, f, "")
	if err != nil {
		t.Fatal(err)
	}

	reads := 0

	for _, r := range f.Recorded() {
		if r.Method == "GET" && r.Path == helmTestPreJob {
			reads++
		}
	}

	if reads != 2 || !strings.Contains(writes[len(writes)-1].Body, `"status":"deployed"`) {
		t.Fatalf("%d reads, last write %s", reads, writes[len(writes)-1].Body)
	}
}

func TestHelmRollbackTimeoutPerHook(t *testing.T) {
	for _, c := range []struct {
		hooks int
		want  time.Duration
	}{
		{0, PlanTimeout},
		{1, PlanTimeout + 10*time.Minute},
		{3, PlanTimeout + 30*time.Minute},
	} {
		if got := rollbackTimeout(c.hooks); got != c.want {
			t.Errorf("%d hooks: %s, want %s", c.hooks, got, c.want)
		}
	}
}

// before-hook-creation makes an existing object fine, except for a CustomResourceDefinition,
// which is never deleted.
func TestDryRunHelmHookConflict(t *testing.T) {
	const crds = "/apis/apiextensions.k8s.io/v1/customresourcedefinitions"

	conflict := `{"kind":"Status","status":"Failure","reason":"AlreadyExists","code":409,"message":"already exists"}`
	f := kubetest.New(t, map[string]string{})
	f.AnswerWith("POST "+helmTestJobs, 409, conflict)
	f.AnswerWith("POST "+crds, 409, conflict)
	f.AnswerWith("DELETE "+helmTestPreJob, 200, `{}`)
	f.AnswerWith("DELETE "+crds+"/widgets.example.com", 200, `{}`)

	k := Env{Client: f.KubeClient(t)}

	step := func(kind, path string) hookStep {
		return hookStep{
			hook: Hook{Kind: kind, DeletePolicies: []string{HookBeforeCreation}},
			obj:  object{Kind: kind, Path: path, Body: map[string]any{"kind": kind}},
		}
	}

	if err := dryRunHook(context.Background(), k, step("Job", helmTestPreJob)); err != nil {
		t.Errorf("Job: %v", err)
	}

	if err := dryRunHook(context.Background(), k, step(kindCRD, crds+"/widgets.example.com")); kube.Code(err) != http.StatusConflict {
		t.Errorf("CRD: %v", err)
	}
}

func TestHelmHookWaitError(t *testing.T) {
	expired := func(parent context.Context, cause error) context.Context {
		ctx, cancel := context.WithTimeoutCause(parent, 0, cause)
		t.Cleanup(cancel)

		return ctx
	}

	rollbackDeadline, cancel := context.WithTimeout(context.Background(), 0)
	t.Cleanup(cancel)

	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()

	for _, c := range []struct {
		ctx  context.Context
		want string
	}{
		{expired(context.Background(), errHookTimeout), "not done after 5m0s"},
		{expired(context.Background(), errRollbackTimeout), "the rollback ran out of time"},
		{expired(rollbackDeadline, errHookTimeout), "the rollback ran out of time"},
		{expired(cancelled, errHookTimeout), "stopped while waiting: context canceled"},
	} {
		<-c.ctx.Done()

		if err := hookWaitError(c.ctx); err == nil || err.Error() != c.want {
			t.Errorf("got %v, want %q", err, c.want)
		}
	}
}

// The rollback's deadline, not the hook's 5 minutes, is named when it ends a wait.
func TestPollHelmHookRollbackDeadline(t *testing.T) {
	f := helmHooksAPI(t)

	k := Env{Client: f.KubeClient(t)}

	ctx, cancel := context.WithTimeoutCause(context.Background(), 20*time.Millisecond, errRollbackTimeout)
	defer cancel()

	err := pollHook(ctx, k, helmTestPreJob, func(map[string]any, bool) (bool, error) { return false, nil })
	if !errors.Is(err, errRollbackTimeout) {
		t.Fatalf("err %v", err)
	}
}

// The release Secret's description is one line; the error returned keeps its lines.
func TestHelmFailureDescription(t *testing.T) {
	err := errors.Join(errors.New("pre-rollback hook Job site-migrate failed: Job failed"), errors.New("delete: forbidden\n"))

	got := failureDescription("site", err)
	if want := `Rollback "site" failed: pre-rollback hook Job site-migrate failed: Job failed; delete: forbidden`; got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
