package helm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubetest"
)

const followTestManifest = `apiVersion: v1
kind: ConfigMap
metadata:
  name: site
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: site
`

// followFake serves release site in web, its latest revision with status.
func followFake(t *testing.T, status string) *kubetest.FakeAPI {
	t.Helper()

	return kubetest.New(t, map[string]string{
		"GET " + helmTestSecrets:                                 refsTable("", helmRow("sh.helm.release.v1.site.v3", "site", 3, status)),
		"GET " + helmTestSecrets + "/sh.helm.release.v1.site.v3": `{"data":{"release":"` + helmSecretData(t, helmTestRelease(3, status, "1.0.0", followTestManifest, nil)) + `"}}`,
		"GET /api/v1":       helmTestDiscover,
		"GET /apis/apps/v1": `{"groupVersion":"apps/v1","resources":[{"name":"deployments","kind":"Deployment","namespaced":true}]}`,
	})
}

// fastFollow polls every few milliseconds and ends the wait after timeout.
func fastFollow(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, oldTimeout := WaitInterval, WaitTimeout
	WaitInterval, WaitTimeout = 5*time.Millisecond, timeout

	t.Cleanup(func() { WaitInterval, WaitTimeout = interval, oldTimeout })
}

// runFollow follows site with the rollout states of states in turn (the last one repeats) and
// returns its result, its error and the phase of each progress event.
func runFollow(t *testing.T, f *kubetest.FakeAPI, states ...RolloutState) (string, []string, error) {
	t.Helper()

	var reads atomic.Int32

	env := Env{Client: f.KubeClient(t), Rollout: func(_ context.Context, kind, namespace, name string) (RolloutState, error) {
		if kind != "Deployment" || namespace != "web" || name != "site" {
			return RolloutState{}, errors.New("unexpected workload " + kind + " " + namespace + "/" + name)
		}

		return states[min(int(reads.Add(1))-1, len(states)-1)], nil
	}}
	with := func(ctx context.Context, fn func(context.Context, Env) error) error { return fn(ctx, env) }
	phases := []string{}

	result, err := Follow(context.Background(), with, kube.FollowFatal, "web", "site", func(p Progress) {
		phases = append(phases, p.Phase)
	})

	return result, phases, err
}

func TestFollowSucceeds(t *testing.T) {
	fastFollow(t, time.Minute)

	result, phases, err := runFollow(t, followFake(t, "deployed"), RolloutState{Ready: 1, Desired: 2}, RolloutState{Ready: 2, Desired: 2, Done: true})
	if err != nil || result != WaitSucceeded || strings.Join(phases, " ") != "Running Succeeded" {
		t.Fatalf("%q %v %v", result, err, phases)
	}
}

// A progress deadline exceeded the controller reported before seeing the latest spec is the
// previous rollout's.
func TestFollowStaleIsNotFailed(t *testing.T) {
	fastFollow(t, time.Minute)

	result, phases, err := runFollow(t, followFake(t, "deployed"), RolloutState{Failed: true, Done: true, Stale: true}, RolloutState{Failed: true})
	if err != nil || result != WaitFailed || strings.Join(phases, " ") != "Running Failed" {
		t.Fatalf("%q %v %v", result, err, phases)
	}
}

// The wait starts once the workloads are listed: a listing slower than the whole wait still
// reads them, and ends with Timeout rather than an error.
func TestFollowTimeoutStartsAfterTheListing(t *testing.T) {
	const wait = 200 * time.Millisecond

	fastFollow(t, wait)

	f := followFake(t, "deployed")
	f.BeforeAnswer = func(r *http.Request) {
		if strings.HasPrefix(r.URL.Path, helmTestSecrets) {
			time.Sleep(wait)
		}
	}

	result, phases, err := runFollow(t, f, RolloutState{Ready: 1, Desired: 2})
	if err != nil || result != WaitTimedOut || strings.Join(phases, " ") != strings.TrimSpace(strings.Repeat("Running ", len(phases)-1))+" Timeout" {
		t.Fatalf("%q %v %v", result, err, phases)
	}
}

func TestFollowRefusesARevisionNotDeployed(t *testing.T) {
	fastFollow(t, time.Minute)

	_, _, err := runFollow(t, followFake(t, "pending-rollback"), RolloutState{})
	if err == nil || !strings.Contains(err.Error(), "not deployed") {
		t.Fatalf("got %v", err)
	}
}

func TestFollowNoWorkloadsIsDone(t *testing.T) {
	fastFollow(t, time.Minute)

	f := followFake(t, "deployed")
	f.AnswerWith("GET "+helmTestSecrets+"/sh.helm.release.v1.site.v3", http.StatusOK,
		`{"data":{"release":"`+helmSecretData(t, helmTestRelease(3, "deployed", "1.0.0", "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: site\n", nil))+`"}}`)

	result, phases, err := runFollow(t, f, RolloutState{})
	if err != nil || result != WaitSucceeded || strings.Join(phases, " ") != "Succeeded" {
		t.Fatalf("%q %v %v", result, err, phases)
	}
}

func TestHelmWaitPhase(t *testing.T) {
	ws := []Workload{{Done: true}, {Manual: true}, {}}

	if p := WithWaitPhase(Progress{Total: 3, Workloads: ws}); p.Phase != WaitRunning || p.Done != 2 {
		t.Fatalf("got %+v", p)
	}

	ws[2].Done = true
	if p := WithWaitPhase(Progress{Total: 3, Workloads: ws}); p.Phase != WaitSucceeded || p.Done != 3 {
		t.Fatalf("got %+v", p)
	}

	ws[0].Failed = true
	if p := WithWaitPhase(Progress{Total: 3, Workloads: ws}); p.Phase != WaitFailed {
		t.Fatalf("got %+v", p)
	}

	// No workload to wait for: done at once.
	if got := workloadObjects([]object{{APIVersion: "v1", Kind: "ConfigMap"}, {APIVersion: "apps/v1", Kind: "DaemonSet"}}); len(got) != 1 {
		t.Fatalf("workloads %+v", got)
	}
}
