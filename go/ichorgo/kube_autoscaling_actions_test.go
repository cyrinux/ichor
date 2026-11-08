package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	soPath  = "/apis/keda.sh/v1alpha1/namespaces/shop/scaledobjects/worker"
	hpaPath = "/apis/autoscaling/v2/namespaces/shop/horizontalpodautoscalers/web"
)

func TestScaledObjectPausePatches(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"PATCH " + soPath: `{}`})
	k := openFakeKube(t, f)

	for _, c := range []struct {
		replicas int
		want     string
	}{
		{2, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":null,"autoscaling.keda.sh/paused-replicas":"2"}}}`},
		{0, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":null,"autoscaling.keda.sh/paused-replicas":"0"}}}`},
		// Hold where it is.
		{scaledObjectHold, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":"true","autoscaling.keda.sh/paused-replicas":null}}}`},
		// Resume removes both: KEDA reads paused-replicas first.
		{scaledObjectResume, `{"metadata":{"annotations":{"autoscaling.keda.sh/paused":null,"autoscaling.keda.sh/paused-replicas":null}}}`},
	} {
		if err := k.patch(context.Background(), soPath, "application/merge-patch+json", scaledObjectPausePatch(c.replicas), nil); err != nil {
			t.Fatal(err)
		}

		if last := lastRequest(f); last.method != http.MethodPatch || last.contentType != "application/merge-patch+json" || last.body != c.want {
			t.Errorf("replicas %d: %+v\nwant %s", c.replicas, last, c.want)
		}
	}
}

func TestSetHPABounds(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + hpaPath:   `{"metadata":{"name":"web","namespace":"shop"},"spec":{"minReplicas":2,"maxReplicas":5}}`,
		"PATCH " + hpaPath: `{}`,
	})

	if err := setHPABounds(context.Background(), openFakeKube(t, f), "shop", "web", 2, 10); err != nil {
		t.Fatal(err)
	}

	if last := lastRequest(f); last.method != http.MethodPatch || last.body != `{"spec":{"maxReplicas":10,"minReplicas":2}}` {
		t.Errorf("patch %+v", last)
	}
}

func TestSetHPABoundsRefusesAKEDAOwnedHPA(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET " + hpaPath: `{"metadata":{"name":"keda-hpa-worker","namespace":"shop","ownerReferences":[{"kind":"ScaledObject","name":"worker"}]}}`,
	})

	if err := setHPABounds(context.Background(), openFakeKube(t, f), "shop", "web", 2, 10); !errors.Is(err, errKEDAOwnsHPA) {
		t.Fatalf("got %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			t.Fatal("a KEDA-owned HPA must not be patched")
		}
	}
}

func TestSetHPABoundsSurfacesTheAPIRefusal(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET " + hpaPath: `{"metadata":{"name":"web"}}`})
	f.answerWith("PATCH "+hpaPath, http.StatusForbidden,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","message":"horizontalpodautoscalers.autoscaling \"web\" is forbidden","code":403}`)

	err := setHPABounds(context.Background(), openFakeKube(t, f), "shop", "web", 1, 3)
	if err == nil || !strings.Contains(kubeError(err).Error(), "forbidden") {
		t.Fatalf("got %v", err)
	}
}

func TestAutoscalingActionsRefusals(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ min, max int }{{0, 3}, {4, 3}, {1, maxScaleReplicas + 1}} {
		if err := KubeSetHPABounds(demo, "", "", "shop", "web", c.min, c.max); err == nil || errors.Is(err, errDemoUnavailable) {
			t.Errorf("bounds %+v: %v", c, err)
		}
	}

	if err := KubePauseScaledObject(demo, "", "", "shop", "worker", -3); err == nil || errors.Is(err, errDemoUnavailable) {
		t.Errorf("replicas -3: %v", err)
	}

	if err := KubePauseScaledObject(demo, "", "", "shop", "worker", 2); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo pause: %v", err)
	}

	if err := KubeSetHPABounds(demo, "", "", "shop", "web", 2, 6); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo bounds: %v", err)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 6 {
		t.Fatalf("%d audit entries", len(entries))
	}

	actions := map[string]bool{}
	for _, e := range entries {
		actions[e.Action] = true
	}

	if !actions["hpa-bounds"] || !actions["pause-scaledobject"] {
		t.Errorf("audit actions %v", actions)
	}
}
