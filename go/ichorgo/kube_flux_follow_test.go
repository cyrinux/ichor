package ichorgo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	fluxFollowGroups = `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`
	fluxFollowPath   = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/shop"
	fluxRequest      = "2026-01-02T03:04:05.123Z"
)

// fastFluxFollow makes the follow poll every few milliseconds and end after timeout.
func fastFluxFollow(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, oldTimeout, step := fluxFollowInterval, fluxFollowTimeout, fluxDemoStep
	fluxFollowInterval, fluxFollowTimeout, fluxDemoStep = 5*time.Millisecond, timeout, time.Millisecond

	t.Cleanup(func() { fluxFollowInterval, fluxFollowTimeout, fluxDemoStep = interval, oldTimeout, step })
}

// fluxFollowKustomization is the Kustomization with the reconcile request fluxRequest, the
// last request the controller handled, and its Ready condition.
func fluxFollowKustomization(handled, ready, message string) string {
	return fmt.Sprintf(`{"metadata":{"name":"shop","namespace":"flux-system","generation":3,"annotations":{"reconcile.fluxcd.io/requestedAt":%q}},
	  "spec":{"suspend":false},
	  "status":{"observedGeneration":3,"lastHandledReconcileAt":%q,"conditions":[{"type":"Ready","status":%q,"message":%q,"lastTransitionTime":"2026-01-02T03:04:00Z"}]}}`,
		fluxRequest, handled, ready, message)
}

// fluxPhases is the phase and message of every progress event, in order.
func fluxPhases(t *testing.T, r *followRecorder) []string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.progress))

	for _, js := range r.progress {
		var p map[string]any
		if err := json.Unmarshal([]byte(js), &p); err != nil {
			t.Fatal(err)
		}

		if len(p) != 3 || p["at"] == nil {
			t.Fatalf("progress keys: %s", js)
		}

		out = append(out, fmt.Sprintf("%s: %s", p["phase"], p["message"]))
	}

	return out
}

func newFluxFollowAPI(t *testing.T, seq []string) string {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{"GET /apis": fluxFollowGroups})
	serveSequence(f, fluxFollowPath, seq)

	return f.kubeconfigFor(f.URL)
}

func TestFluxReconcileFollowPhaseSequence(t *testing.T) {
	fastFluxFollow(t, time.Minute)

	cfg := newFluxFollowAPI(t, []string{
		// The previous reconcile's Ready, the request not handled yet.
		fluxFollowKustomization("2026-01-01T00:00:00Z", "True", "Applied revision: main@sha1:1111111"),
		fluxFollowKustomization("2026-01-01T00:00:00Z", "Unknown", "Reconciliation in progress"),
		fluxFollowKustomization("2026-01-01T00:00:00Z", "Unknown", "Reconciliation in progress"),
		fluxFollowKustomization(fluxRequest, "True", "Applied revision: main@sha1:2222222"),
	})

	rec := newFollowRecorder()
	StartFluxReconcileFollow(cfg, "admin@test", "", "Kustomization", "flux-system", "shop", rec)

	if done := rec.wait(t); done != [2]string{fluxFollowReady, ""} {
		t.Fatalf("done %q", done)
	}

	want := []string{
		"reconciling: Applied revision: main@sha1:1111111",
		"reconciling: Reconciliation in progress",
		"ready: Applied revision: main@sha1:2222222",
	}
	if got := fluxPhases(t, rec); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("progress %v", got)
	}
}

func TestFluxReconcileFollowFailed(t *testing.T) {
	fastFluxFollow(t, time.Minute)

	cfg := newFluxFollowAPI(t, []string{fluxFollowKustomization(fluxRequest, "False", "kustomize build failed")})

	rec := newFollowRecorder()
	StartFluxReconcileFollow(cfg, "admin@test", "", "Kustomization", "flux-system", "shop", rec)

	if done := rec.wait(t); done != [2]string{fluxFollowFailed, ""} {
		t.Fatalf("done %q", done)
	}

	if got := fluxPhases(t, rec); len(got) != 1 || got[0] != "failed: kustomize build failed" {
		t.Fatalf("progress %v", got)
	}
}

func TestFluxReconcileFollowTimeout(t *testing.T) {
	// Room for the race detector's slower round trips (no production change: the Flux follow
	// reads everything inside its wait, so a slow read ends as "still", never an error).
	fastFluxFollow(t, 300*time.Millisecond)

	cfg := newFluxFollowAPI(t, []string{fluxFollowKustomization("2026-01-01T00:00:00Z", "Unknown", "Reconciliation in progress")})

	rec := newFollowRecorder()
	StartFluxReconcileFollow(cfg, "admin@test", "", "Kustomization", "flux-system", "shop", rec)

	// Still running when the follow gives up: said so, not an error.
	if done := rec.wait(t); done != [2]string{fluxFollowStill, ""} {
		t.Fatalf("done %q", done)
	}

	if got := fluxPhases(t, rec); len(got) != 1 || !strings.HasPrefix(got[0], "reconciling") {
		t.Fatalf("progress %v", got)
	}
}

func TestFluxReconcileFollowCancelAndInvalid(t *testing.T) {
	fastFluxFollow(t, time.Minute)

	cfg := newFluxFollowAPI(t, []string{fluxFollowKustomization("", "Unknown", "")})

	rec := newFollowRecorder()
	run := StartFluxReconcileFollow(cfg, "admin@test", "", "Kustomization", "flux-system", "shop", rec)

	time.Sleep(20 * time.Millisecond)
	run.Cancel()

	if done := rec.wait(t); done != [2]string{"", ""} {
		t.Fatalf("done %q", done)
	}

	rec = newFollowRecorder()
	StartFluxReconcileFollow(cfg, "admin@test", "", "Deployment", "flux-system", "shop", rec)

	if done := rec.wait(t); !strings.Contains(done[1], "unsupported Flux kind") {
		t.Fatalf("done %q", done)
	}
}

func TestFluxReconcileFollowProgressOf(t *testing.T) {
	parse := func(js string) fluxFollowObject {
		var o fluxFollowObject
		if err := json.Unmarshal([]byte(js), &o); err != nil {
			t.Fatal(err)
		}

		return o
	}

	// No request annotation: handled once the controller observed the generation.
	o := parse(`{"metadata":{"generation":4},"status":{"observedGeneration":3,"conditions":[{"type":"Ready","status":"True","message":"old"},{"type":"Reconciling","status":"True","message":"building"}]}}`)
	if p := fluxReconcileProgressOf(o); p.Phase != fluxFollowReconciling || p.Message != "building" {
		t.Fatalf("got %+v", p)
	}

	o.Status.ObservedGeneration = 4
	if p := fluxReconcileProgressOf(o); p.Phase != fluxFollowReady {
		t.Fatalf("got %+v", p)
	}

	o.Spec.Suspend = true
	if p := fluxReconcileProgressOf(o); p.Phase != fluxFollowFailed || !strings.Contains(p.Message, "suspended") {
		t.Fatalf("got %+v", p)
	}
}

func TestFluxReconcileFollowDemo(t *testing.T) {
	fastFluxFollow(t, time.Minute)
	noKubeDial(t)

	rec := newFollowRecorder()
	StartFluxReconcileFollow(demoKubeconfigForTest(t), "", "", "Kustomization", "flux-system", "apps", rec)

	if done := rec.wait(t); done != [2]string{fluxFollowReady, ""} {
		t.Fatalf("done %q", done)
	}

	got := fluxPhases(t, rec)
	if len(got) != 3 || !strings.HasPrefix(got[0], "reconciling") || !strings.HasPrefix(got[2], "ready") {
		t.Fatalf("progress %v", got)
	}
}
