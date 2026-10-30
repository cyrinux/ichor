package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube/helm"
)

const helmFollowManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: site
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: site
spec:
  replicas: 2
`

// fastHelmFollow makes the wait poll every few milliseconds and end after timeout.
func fastHelmFollow(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, oldTimeout, step := helm.WaitInterval, helm.WaitTimeout, helmDemoStep
	helm.WaitInterval, helm.WaitTimeout, helmDemoStep = 5*time.Millisecond, timeout, time.Millisecond

	t.Cleanup(func() { helm.WaitInterval, helm.WaitTimeout, helmDemoStep = interval, oldTimeout, step })
}

// helmFollowDeployment is the release's Deployment with ready of 2 pods ready (all of them
// updated), and the progress deadline exceeded when failed.
func helmFollowDeployment(ready int, failed bool) string {
	conditions := `[]`
	if failed {
		conditions = `[{"type":"Progressing","status":"False","reason":"ProgressDeadlineExceeded"}]`
	}

	return fmt.Sprintf(`{"metadata":{"name":"site","namespace":"web","generation":2},"spec":{"replicas":2},
	  "status":{"observedGeneration":2,"replicas":2,"updatedReplicas":2,"readyReplicas":%d,"availableReplicas":%d,"conditions":%s}}`,
		ready, ready, conditions)
}

// newHelmFollowAPI serves release site in web, its latest revision with status, and the
// Deployment reads of seq in turn.
func newHelmFollowAPI(t *testing.T, status string, seq []string) string {
	t.Helper()

	f := newHelmFollowFake(t, status, seq)

	return f.kubeconfigFor(f.URL)
}

// newHelmFollowFake is newHelmFollowAPI's server, for a test that hooks its reads.
func newHelmFollowFake(t *testing.T, status string, seq []string) *fakeKubeAPI {
	t.Helper()

	rel := helmSecretData(t, helmTestRelease(3, status, "1.0.0", helmFollowManifest, nil))
	old := helmSecretData(t, helmTestRelease(2, "superseded", "1.0.0", helmFollowManifest, nil))

	f := newFakeKubeAPI(t, map[string]string{
		"GET " + helmTestSecrets: `{"kind":"Table","apiVersion":"meta.k8s.io/v1","columnDefinitions":[],"rows":[` +
			helmRow("sh.helm.release.v1.site.v2", "site", 2, "superseded") + "," + helmRow("sh.helm.release.v1.site.v3", "site", 3, status) + `]}`,
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v2": `{"data":{"release":"` + old + `"}}`,
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v3": `{"data":{"release":"` + rel + `"}}`,
		"GET /api/v1":       helmTestDiscover,
		"GET /apis/apps/v1": `{"groupVersion":"apps/v1","resources":[{"name":"deployments","kind":"Deployment","namespaced":true,"verbs":["get","list"]}]}`,
	})
	serveSequence(f, helmTestDeploy, seq)

	return f
}

// helmFollowSteps is every progress event as "phase done/total name ready/desired ...".
func helmFollowSteps(t *testing.T, r *followRecorder) []string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.progress))

	for _, js := range r.progress {
		var raw map[string]any
		if err := json.Unmarshal([]byte(js), &raw); err != nil {
			t.Fatal(err)
		}

		if len(raw) != 5 || raw["at"] == nil || raw["workloads"] == nil {
			t.Fatalf("progress keys: %s", js)
		}

		var p helm.Progress
		if err := json.Unmarshal([]byte(js), &p); err != nil {
			t.Fatal(err)
		}

		step := fmt.Sprintf("%s %d/%d", p.Phase, p.Done, p.Total)
		for _, w := range p.Workloads {
			step += fmt.Sprintf(" %s/%s/%s %d/%d", w.Kind, w.Namespace, w.Name, w.Ready, w.Desired)
		}

		out = append(out, step)
	}

	return out
}

func TestHelmRollbackFollow(t *testing.T) {
	fastHelmFollow(t, time.Minute)

	cfg := newHelmFollowAPI(t, "deployed", []string{helmFollowDeployment(1, false), helmFollowDeployment(1, false), helmFollowDeployment(2, false)})

	rec := newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	if done := rec.wait(t); done != [2]string{helm.WaitSucceeded, ""} {
		t.Fatalf("done %q", done)
	}

	want := []string{
		"Running 0/1 Deployment/web/site 1/2",
		"Succeeded 1/1 Deployment/web/site 2/2",
	}
	if got := helmFollowSteps(t, rec); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("progress %v", got)
	}
}

func TestHelmRollbackFollowFailed(t *testing.T) {
	fastHelmFollow(t, time.Minute)

	cfg := newHelmFollowAPI(t, "deployed", []string{helmFollowDeployment(1, true)})

	rec := newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	if done := rec.wait(t); done != [2]string{helm.WaitFailed, ""} {
		t.Fatalf("done %q", done)
	}

	if got := helmFollowSteps(t, rec); len(got) != 1 || got[0] != "Failed 0/1 Deployment/web/site 1/2" {
		t.Fatalf("progress %v", got)
	}
}

// A progress deadline exceeded that the controller reported before seeing the rollback's
// spec is the previous rollout's: it waits, as `kubectl rollout status`.
func TestHelmRollbackFollowStaleNotFailed(t *testing.T) {
	fastHelmFollow(t, time.Minute)

	stale := strings.Replace(helmFollowDeployment(1, true), `"generation":2`, `"generation":3`, 1)
	cfg := newHelmFollowAPI(t, "deployed", []string{stale, helmFollowDeployment(2, false)})

	rec := newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	if done := rec.wait(t); done != [2]string{helm.WaitSucceeded, ""} {
		t.Fatalf("done %q", done)
	}

	want := []string{
		"Running 0/1 Deployment/web/site 1/2",
		"Succeeded 1/1 Deployment/web/site 2/2",
	}
	if got := helmFollowSteps(t, rec); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("progress %v", got)
	}
}

// The wait budget leaves room for the race detector's slower round trips; the progress dedup
// keeps the steps at two however many polls fit in it.
const helmFollowTestWait = 300 * time.Millisecond

func TestHelmRollbackFollowTimeout(t *testing.T) {
	fastHelmFollow(t, helmFollowTestWait)

	cfg := newHelmFollowAPI(t, "deployed", []string{helmFollowDeployment(1, false)})

	rec := newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	assertHelmFollowTimedOut(t, rec)
}

// A listing slower than the whole wait still ends with Timeout, not an error: the wait starts
// once the workloads are listed.
func TestHelmRollbackFollowTimeoutAfterSlowListing(t *testing.T) {
	fastHelmFollow(t, helmFollowTestWait)

	f := newHelmFollowFake(t, "deployed", []string{helmFollowDeployment(1, false)})
	// The release's three secret reads (the list and two revisions) take longer than the wait.
	next := f.beforeAnswer
	f.beforeAnswer = func(r *http.Request) {
		if strings.HasPrefix(r.URL.Path, helmTestSecrets) {
			time.Sleep(helmFollowTestWait / 2)
		}

		next(r)
	}

	rec := newFollowRecorder()
	StartHelmRollbackFollow(f.kubeconfigFor(f.URL), "admin@test", "", "web", "site", rec)

	assertHelmFollowTimedOut(t, rec)
}

func assertHelmFollowTimedOut(t *testing.T, rec *followRecorder) {
	t.Helper()

	if done := rec.wait(t); done != [2]string{helm.WaitTimedOut, ""} {
		t.Fatalf("done %q", done)
	}

	got := helmFollowSteps(t, rec)
	if len(got) != 2 || got[0] != "Running 0/1 Deployment/web/site 1/2" || got[1] != "Timeout 0/1 Deployment/web/site 1/2" {
		t.Fatalf("progress %v", got)
	}
}

func TestHelmRollbackFollowRefusesPending(t *testing.T) {
	fastHelmFollow(t, time.Minute)

	cfg := newHelmFollowAPI(t, "pending-rollback", []string{helmFollowDeployment(2, false)})

	rec := newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	if done := rec.wait(t); done[0] != "" || !strings.Contains(done[1], "not deployed") {
		t.Fatalf("done %q", done)
	}
}

func TestHelmRollbackFollowCancelAndInvalid(t *testing.T) {
	fastHelmFollow(t, time.Minute)

	cfg := newHelmFollowAPI(t, "deployed", []string{helmFollowDeployment(0, false)})

	rec := newFollowRecorder()
	run := StartHelmRollbackFollow(cfg, "admin@test", "", "web", "site", rec)

	time.Sleep(20 * time.Millisecond)
	run.Cancel()

	if done := rec.wait(t); done != [2]string{"", ""} {
		t.Fatalf("done %q", done)
	}

	rec = newFollowRecorder()
	StartHelmRollbackFollow(cfg, "admin@test", "", "web", "../site", rec)

	if done := rec.wait(t); !strings.Contains(done[1], "invalid") {
		t.Fatalf("done %q", done)
	}
}

func TestHelmRollbackFollowDemo(t *testing.T) {
	fastHelmFollow(t, time.Minute)
	noKubeDial(t)

	rec := newFollowRecorder()
	StartHelmRollbackFollow(demoKubeconfigForTest(t), "", "", "ingress-nginx", "ingress-nginx", rec)

	if done := rec.wait(t); done != [2]string{helm.WaitSucceeded, ""} {
		t.Fatalf("done %q", done)
	}

	got := helmFollowSteps(t, rec)
	if len(got) != 4 || !strings.HasPrefix(got[0], "Running 0/2") || got[3] != "Succeeded 2/2 Deployment/ingress-nginx/ingress-nginx 2/2 StatefulSet/ingress-nginx/ingress-nginx-cache 1/1" {
		t.Fatalf("progress %v", got)
	}
}
