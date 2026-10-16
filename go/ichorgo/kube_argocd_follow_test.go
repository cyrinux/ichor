package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	argoFollowGroups = `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`
	argoFollowPath   = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/shop"
)

// followRecorder records a followed operation's progress and its end.
type followRecorder struct {
	mu       sync.Mutex
	progress []string
	done     chan [2]string
}

func newFollowRecorder() *followRecorder { return &followRecorder{done: make(chan [2]string, 1)} }

func (r *followRecorder) OnProgress(js string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.progress = append(r.progress, js)
}

func (r *followRecorder) OnDone(result, errMessage string) { r.done <- [2]string{result, errMessage} }

// wait is OnDone's arguments, failing the test when they do not come.
func (r *followRecorder) wait(t *testing.T) [2]string {
	t.Helper()

	select {
	case d := <-r.done:
		return d
	case <-time.After(10 * time.Second):
		t.Fatal("the follow did not end")

		return [2]string{}
	}
}

// phases is the field key of every progress event, in order.
func (r *followRecorder) phases(t *testing.T) []string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.progress))

	for _, js := range r.progress {
		var p argoSyncProgress
		if err := json.Unmarshal([]byte(js), &p); err != nil {
			t.Fatal(err)
		}

		out = append(out, fmt.Sprintf("%s %d/%d %d/%d", p.Phase, p.Wave, p.Waves, p.Done, p.Total))
	}

	return out
}

// fastArgoFollow makes the follow poll every few milliseconds.
func fastArgoFollow(t *testing.T, grace int) {
	t.Helper()

	interval, oldGrace, step := argoFollowInterval, argoFollowGrace, argoDemoStep
	argoFollowInterval, argoFollowGrace, argoDemoStep = 5*time.Millisecond, grace, time.Millisecond

	t.Cleanup(func() { argoFollowInterval, argoFollowGrace, argoDemoStep = interval, oldGrace, step })
}

// serveSequence answers GET path with each of seq in turn, the last one from then on.
func serveSequence(f *fakeKubeAPI, path string, seq []string) {
	var reads atomic.Int32

	f.answerWith("GET "+path, http.StatusOK, seq[0])
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == path {
			i := min(int(reads.Add(1)), len(seq)-1)
			f.answerWith("GET "+path, http.StatusOK, seq[i])
		}
	}
}

// argoFollowApp is the app with two waves of two resources, synced of them done, its
// operation phase started at started (none when phase is ""), and pending when a sync was
// requested and not picked up yet.
func argoFollowApp(phase string, started time.Time, synced int, pending bool, message string) string {
	resources := []string{
		`{"kind":"ConfigMap","namespace":"shop","name":"settings","status":"OutOfSync","syncWave":0}`,
		`{"kind":"Secret","namespace":"shop","name":"keys","status":"OutOfSync","syncWave":0}`,
		`{"group":"apps","kind":"Deployment","namespace":"shop","name":"front","status":"OutOfSync","syncWave":1}`,
		`{"group":"apps","kind":"Deployment","namespace":"shop","name":"back","status":"OutOfSync","syncWave":1}`,
	}
	results := []string{
		`{"kind":"ConfigMap","namespace":"shop","name":"settings","status":"Synced"}`,
		`{"kind":"Secret","namespace":"shop","name":"keys","status":"Synced"}`,
		`{"group":"apps","kind":"Deployment","namespace":"shop","name":"front","status":"Synced"}`,
		`{"group":"apps","kind":"Deployment","namespace":"shop","name":"back","status":"Synced"}`,
	}

	op := ""
	if phase != "" {
		op = fmt.Sprintf(`,"operationState":{"phase":%q,"message":%q,"startedAt":%q,"operation":{"sync":{"revision":"abc"}},"syncResult":{"revision":"abc","resources":[%s]}}`,
			phase, message, started.UTC().Format(time.RFC3339), strings.Join(results[:synced], ","))
	}

	operation := ""
	if pending {
		operation = `"operation":{"sync":{"revision":"abc"}},`
	}

	return fmt.Sprintf(`{"metadata":{"name":"shop","namespace":"argocd","resourceVersion":"7"},%s"spec":{"project":"default"},"status":{"resources":[%s]%s}}`,
		operation, strings.Join(resources, ","), op)
}

func newArgoFollowAPI(t *testing.T, seq []string) (*fakeKubeAPI, string) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{"GET /apis": argoFollowGroups})
	serveSequence(f, argoFollowPath, seq)

	return f, f.kubeconfigFor(f.URL)
}

func TestArgoSyncFollowPhaseSequence(t *testing.T) {
	fastArgoFollow(t, 10)

	now := time.Now()
	_, cfg := newArgoFollowAPI(t, []string{
		// The last sync, finished an hour ago, while the phone's is requested.
		argoFollowApp("Succeeded", now.Add(-time.Hour), 4, true, "old"),
		argoFollowApp("Running", now, 0, false, "wave 0"),
		argoFollowApp("Running", now, 0, false, "wave 0"),
		argoFollowApp("Running", now, 2, false, "wave 1"),
		argoFollowApp("Succeeded", now, 4, false, "successfully synced"),
	})

	rec := newFollowRecorder()
	StartArgoSyncFollow(cfg, "admin@test", "", "argocd", "shop", rec)

	if done := rec.wait(t); done != [2]string{argoSyncSucceeded, ""} {
		t.Fatalf("done %q", done)
	}

	want := []string{"Pending 0/0 0/0", "Running 1/2 0/4", "Running 2/2 2/4", "Succeeded 2/2 4/4"}
	if got := rec.phases(t); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("progress %v, want %v", got, want)
	}

	var p map[string]any
	if err := json.Unmarshal([]byte(rec.progress[1]), &p); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"phase", "message", "wave", "waves", "done", "total", "at"} {
		if _, ok := p[key]; !ok {
			t.Errorf("progress lacks %q: %s", key, rec.progress[1])
		}
	}
}

func TestArgoSyncFollowIgnoresAnOlderOperation(t *testing.T) {
	fastArgoFollow(t, 3)

	now := time.Now()

	// Nothing new shows up: after the grace reads, the last finished sync is reported.
	_, cfg := newArgoFollowAPI(t, []string{argoFollowApp("Failed", now.Add(-time.Hour), 1, false, "old failure")})

	rec := newFollowRecorder()
	StartArgoSyncFollow(cfg, "admin@test", "", "argocd", "shop", rec)

	if done := rec.wait(t); done != [2]string{argoSyncFailed, ""} {
		t.Fatalf("done %q", done)
	}

	if got := rec.phases(t); len(got) != 2 || got[0] != "Pending 0/0 0/0" || !strings.HasPrefix(got[1], "Failed") {
		t.Fatalf("progress %v", got)
	}

	// A sync that finished just before the follow, while the phone's is requested, is not it:
	// its successor is followed.
	_, cfg = newArgoFollowAPI(t, []string{
		argoFollowApp("Succeeded", now, 4, true, "previous"),
		argoFollowApp("Succeeded", now, 4, true, "previous"),
		argoFollowApp("Failed", now.Add(2*time.Second), 1, false, "one or more objects failed to apply"),
	})

	rec = newFollowRecorder()
	StartArgoSyncFollow(cfg, "admin@test", "", "argocd", "shop", rec)

	if done := rec.wait(t); done != [2]string{argoSyncFailed, ""} {
		t.Fatalf("done %q", done)
	}

	if got := rec.phases(t); len(got) != 2 || got[0] != "Pending 0/0 0/0" || got[1] != "Failed 1/2 1/4" {
		t.Fatalf("progress %v", got)
	}

	// Requested and never picked up: the follow says so.
	_, cfg = newArgoFollowAPI(t, []string{argoFollowApp("Succeeded", now.Add(-time.Hour), 4, true, "old")})

	rec = newFollowRecorder()
	StartArgoSyncFollow(cfg, "admin@test", "", "argocd", "shop", rec)

	if done := rec.wait(t); done[0] != "" || done[1] != errArgoSyncNotStarted.Error() {
		t.Fatalf("done %q", done)
	}
}

func TestArgoSyncFollowTerminate(t *testing.T) {
	fastArgoFollow(t, 10)

	now := time.Now()
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":               argoFollowGroups,
		"GET " + argoFollowPath:   argoFollowApp("Running", now, 1, false, "wave 0"),
		"PATCH " + argoFollowPath: `{}`,
	})
	// The controller ends the operation once asked to.
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodPatch {
			f.answerWith("GET "+argoFollowPath, http.StatusOK, argoFollowApp("Failed", now, 1, false, "Operation terminated"))
		}
	}

	rec := newFollowRecorder()
	run := StartArgoSyncFollow(f.kubeconfigFor(f.URL), "admin@test", "", "argocd", "shop", rec)

	// Terminate once the follow saw the running sync.
	deadline := time.Now().Add(5 * time.Second)
	for len(rec.phases(t)) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if err := run.Terminate(); err != nil {
		t.Fatal(err)
	}

	if done := rec.wait(t); done != [2]string{argoSyncTerminated, ""} {
		t.Fatalf("done %q", done)
	}

	if got := rec.phases(t); len(got) != 2 || got[0] != "Running 1/2 1/4" || got[1] != "Terminated 1/2 1/4" {
		t.Fatalf("progress %v", got)
	}

	var patched bool

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch && strings.Contains(r.body, `"phase":"Terminating"`) && strings.Contains(r.body, `"resourceVersion":"7"`) {
			patched = true
		}
	}

	if !patched {
		t.Fatalf("no terminate patch in %+v", f.recorded())
	}
}

func TestArgoSyncFollowCancel(t *testing.T) {
	fastArgoFollow(t, 10)

	_, cfg := newArgoFollowAPI(t, []string{argoFollowApp("Running", time.Now(), 0, false, "")})

	rec := newFollowRecorder()
	run := StartArgoSyncFollow(cfg, "admin@test", "", "argocd", "shop", rec)

	time.Sleep(20 * time.Millisecond)
	run.Cancel()

	if done := rec.wait(t); done != [2]string{"", ""} {
		t.Fatalf("done %q", done)
	}
}

func TestArgoSyncFollowGoneAndInvalid(t *testing.T) {
	fastArgoFollow(t, 10)

	f := newFakeKubeAPI(t, map[string]string{"GET /apis": argoFollowGroups})

	rec := newFollowRecorder()
	StartArgoSyncFollow(f.kubeconfigFor(f.URL), "admin@test", "", "argocd", "shop", rec)

	if done := rec.wait(t); done[0] != "" || !strings.Contains(done[1], "not found") {
		t.Fatalf("done %q", done)
	}

	rec = newFollowRecorder()
	StartArgoSyncFollow(f.kubeconfigFor(f.URL), "admin@test", "", "argocd", "../shop", rec)

	if done := rec.wait(t); done[1] == "" {
		t.Fatalf("an invalid name was followed: %q", done)
	}
}

func TestArgoSyncFollowDemo(t *testing.T) {
	fastArgoFollow(t, 10)
	noKubeDial(t)

	for name, cfg := range map[string]string{"talos": mustDemoConfig(t), "kube": demoKubeconfigForTest(t)} {
		rec := newFollowRecorder()
		run := StartArgoSyncFollow(cfg, "", "", "argocd", "demo-web", rec)

		if done := rec.wait(t); done != [2]string{argoSyncSucceeded, ""} {
			t.Fatalf("%s: done %q", name, done)
		}

		want := []string{"Pending 0/0 0/0", "Running 1/4 0/12", "Running 2/4 3/12", "Running 3/4 6/12", "Running 4/4 9/12", "Succeeded 4/4 12/12"}
		if got := rec.phases(t); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("%s: progress %v", name, got)
		}

		if err := run.Terminate(); !errors.Is(err, errDemoUnavailable) {
			t.Fatalf("%s: terminate %v", name, err)
		}
	}
}
