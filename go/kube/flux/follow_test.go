package flux

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/kubetest"
)

const followPath = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/shop"

func fastFollow(t *testing.T, timeout time.Duration) {
	t.Helper()

	interval, oldTimeout := FollowInterval, FollowTimeout
	FollowInterval, FollowTimeout = 5*time.Millisecond, timeout

	t.Cleanup(func() { FollowInterval, FollowTimeout = interval, oldTimeout })
}

func followAnswer(handled, ready, message string) string {
	return fmt.Sprintf(`{"metadata":{"generation":3,"annotations":{"reconcile.fluxcd.io/requestedAt":"req"}},
	  "status":{"observedGeneration":3,"lastHandledReconcileAt":%q,"conditions":[{"type":"Ready","status":%q,"message":%q}]}}`, handled, ready, message)
}

// followAPI answers the object with seq, one read after the other, the last one for good.
func followAPI(t *testing.T, seq ...string) With {
	t.Helper()

	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`})

	var read atomic.Int32

	f.AnswerQuery = func(path, _ string) (string, bool) {
		if path != followPath {
			return "", false
		}

		return seq[min(int(read.Add(1))-1, len(seq)-1)], true
	}

	client := f.KubeClient(t)

	return func(ctx context.Context, fn func(context.Context, kube.Client) error) error { return fn(ctx, client) }
}

func TestFollowUntilHandled(t *testing.T) {
	fastFollow(t, time.Minute)

	with := followAPI(t, followAnswer("old", "True", "previous"), followAnswer("old", "Unknown", "running"), followAnswer("req", "True", "applied"))

	var phases []string

	got, err := Follow(context.Background(), with, kube.FollowFatal, "Kustomization", "flux-system", "shop", func(p Progress) {
		phases = append(phases, p.Phase+": "+p.Message)
	})
	if err != nil || got != FollowReady {
		t.Fatalf("got %q %v", got, err)
	}

	want := "reconciling: previous|reconciling: running|ready: applied"
	if strings.Join(phases, "|") != want {
		t.Fatalf("phases %v", phases)
	}
}

func TestFollowFailed(t *testing.T) {
	fastFollow(t, time.Minute)

	with := followAPI(t, followAnswer("req", "False", "kustomize build failed"))

	got, err := Follow(context.Background(), with, kube.FollowFatal, "Kustomization", "flux-system", "shop", func(Progress) {})
	if err != nil || got != FollowFailed {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestFollowOutlivedIsNotAnError(t *testing.T) {
	fastFollow(t, 50*time.Millisecond)

	with := followAPI(t, followAnswer("old", "Unknown", "running"))

	got, err := Follow(context.Background(), with, kube.FollowFatal, "Kustomization", "flux-system", "shop", func(Progress) {})
	if err != nil || got != FollowStill {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestFollowTakesTheCallersContext(t *testing.T) {
	fastFollow(t, time.Minute)

	with := followAPI(t, followAnswer("old", "Unknown", "running"))

	ctx, cancel := context.WithCancel(context.Background())
	got, err := Follow(ctx, with, kube.FollowFatal, "Kustomization", "flux-system", "shop", func(Progress) { cancel() })

	if got == FollowStill || err == nil {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestProgressOf(t *testing.T) {
	parse := func(js string) followObject {
		var o followObject
		if err := json.Unmarshal([]byte(js), &o); err != nil {
			t.Fatal(err)
		}

		return o
	}

	// No request annotation: handled once the controller observed the generation.
	o := parse(`{"metadata":{"generation":4},"status":{"observedGeneration":3,"conditions":[{"type":"Ready","status":"True","message":"old"},{"type":"Reconciling","status":"True","message":"building"}]}}`)
	if p := progressOf(o); p.Phase != FollowReconciling || p.Message != "building" {
		t.Fatalf("got %+v", p)
	}

	o.Status.ObservedGeneration = 4
	if p := progressOf(o); p.Phase != FollowReady {
		t.Fatalf("got %+v", p)
	}

	o.Spec.Suspend = true
	if p := progressOf(o); p.Phase != FollowFailed || !strings.Contains(p.Message, "suspended") {
		t.Fatalf("got %+v", p)
	}
}
