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
	xpManagedPath   = "/apis/nop.example.org/v1alpha1/nopresources/demo-nop"
	xpCompositePath = "/apis/platform.example.org/v1/namespaces/team-a/xdatabases/orders"
)

var xpActionNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func crossplaneActionAPI(t *testing.T, ssar string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"POST " + ssarPath:         ssar,
		"PATCH " + xpManagedPath:   `{}`,
		"PATCH " + xpCompositePath: `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func TestCrossplaneActionPatches(t *testing.T) {
	f, k := crossplaneActionAPI(t, `{"status":{"allowed":true}}`)
	managed := resourceRef{"nop.example.org", "v1alpha1", "nopresources"}

	for _, c := range []struct{ action, want string }{
		{xpActionPause, `{"metadata":{"annotations":{"crossplane.io/paused":"true"}}}`},
		// null removes the key: a "false" left behind reads as paused to some tooling.
		{xpActionResume, `{"metadata":{"annotations":{"crossplane.io/paused":null}}}`},
		{xpActionReconcile, `{"metadata":{"annotations":{"crossplane.io/reconcile-requested-at":"2026-10-16T12:00:00Z"}}}`},
	} {
		if err := crossplaneAction(context.Background(), k, managed, "", "demo-nop", c.action, xpActionNow); err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}

		last := lastRequest(f)
		if last.method != http.MethodPatch || last.path != xpManagedPath || last.contentType != "application/merge-patch+json" || last.body != c.want {
			t.Errorf("%s: patch %+v\nwant %s", c.action, last, c.want)
		}
	}

	// A namespaced composite: the namespace is in the path.
	composite := resourceRef{"platform.example.org", "v1", "xdatabases"}
	if err := crossplaneAction(context.Background(), k, composite, "team-a", "orders", xpActionPause, xpActionNow); err != nil {
		t.Fatal(err)
	}

	if last := lastRequest(f); last.path != xpCompositePath {
		t.Errorf("composite path %s", last.path)
	}
}

func TestCrossplaneActionRefusedByRBAC(t *testing.T) {
	f, k := crossplaneActionAPI(t, `{"status":{"allowed":false,"reason":"no RBAC policy matched"}}`)

	err := crossplaneAction(context.Background(), k, resourceRef{"nop.example.org", "v1alpha1", "nopresources"}, "", "demo-nop", xpActionPause, xpActionNow)

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != 403 || !strings.Contains(err.Error(), "no RBAC policy matched") {
		t.Fatalf("got %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			t.Fatal("a refused action must not patch")
		}
	}
}

func TestKubeCrossplaneActionRefusals(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeCrossplaneAction(demo, "", "", "nop.example.org", "v1alpha1", "nopresources", "", "demo-nop", "delete"); err == nil || !strings.Contains(err.Error(), "pause, resume or reconcile") {
		t.Errorf("unknown action: %v", err)
	}

	if err := KubeCrossplaneAction(demo, "", "", "", "v1alpha1", "nopresources", "", "demo-nop", xpActionPause); err == nil || errors.Is(err, errDemoUnavailable) {
		t.Errorf("no group: %v", err)
	}

	if err := KubeCrossplaneAction(demo, "", "", "nop.example.org", "v1alpha1", "nopresources", "", "demo-nop", xpActionPause); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	entries := readAudit(t, "", "")
	if len(entries) != 3 || entries[0].Object != "nopresources.nop.example.org/demo-nop" {
		t.Fatalf("audit %+v", entries)
	}
}
