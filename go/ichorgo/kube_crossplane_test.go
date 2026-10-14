package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func crossplaneAPI(t *testing.T) (*fakeKubeAPI, *kubeClient, map[string]string) {
	t.Helper()
	answers := map[string]string{}
	for path, file := range map[string]string{
		"/apis":                                "groups.json",
		"/apis/platform.example.org/v1beta1":   "platform.example.org.json",
		"/apis/storage.example.org/v1":         "storage.example.org.json",
		"/apis/sql.example.org/v1":             "sql.example.org.json",
		"/apis/pkg.crossplane.io/v1/providers": "providers.json",
		"/apis/pkg.crossplane.io/v1/functions": "functions.json",
		"/apis/apiextensions.crossplane.io/v1/compositeresourcedefinitions": "definitions.json",
		"/apis/apiextensions.crossplane.io/v1/compositions":                 "compositions.json",
		"/apis/platform.example.org/v1beta1/xbuckets":                       "composites-v1.json",
		"/apis/platform.example.org/v1beta1/xdatabases":                     "composites-v2.json",
		"/apis/platform.example.org/v1beta1/bucketclaims":                   "claims.json",
		"/apis/storage.example.org/v1/buckets":                              "buckets.json",
		"/apis/sql.example.org/v1/databases":                                "databases.json",
	} {
		answers["GET "+path] = string(readFixture(t, "crossplane/"+file))
	}
	answers["GET /apis/pkg.crossplane.io/v1/configurations"] = `{"items":[]}`
	answers["GET /apis/pkg.crossplane.io/v1"] = `{"groupVersion":"pkg.crossplane.io/v1","resources":[]}`
	answers["GET /apis/apiextensions.crossplane.io/v2"] = `{"groupVersion":"apiextensions.crossplane.io/v2","resources":[]}`
	f, k := actionsKube(t, answers)
	groups, err := readAPIGroups(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	return f, k, groups
}

func TestReadCrossplane(t *testing.T) {
	f, k, groups := crossplaneAPI(t)
	out, err := readCrossplane(context.Background(), k, groups, fixtureNow)
	if err != nil || !out.Installed || out.Error != "" {
		t.Fatalf("status: %+v %v", out, err)
	}
	if len(out.Packages) != 3 || out.Packages[0].Name != "provider-broken" || out.Packages[0].Health != healthCritical || out.Packages[0].Installed.Message != "cannot pull package" {
		t.Fatalf("packages: %+v", out.Packages)
	}
	if len(out.Definitions) != 3 || out.Definitions[0].Health != healthWarning {
		t.Fatalf("definitions: %+v", out.Definitions)
	}
	for _, d := range out.Definitions {
		if d.CompositeKind == "XBucket" && (d.Scope != "LegacyCluster" || d.ClaimKind != "BucketClaim" || d.Compositions != 2 || d.Composites != 1) {
			t.Errorf("legacy definition: %+v", d)
		}
		if d.Scope == "Namespaced" && (d.ClaimKind != "" || d.Compositions != 1 || d.Composites != 1) {
			t.Errorf("v2 definition: %+v", d)
		}
		if d.Group != groupCrossplaneAPIExt || d.Resource != "compositeresourcedefinitions" {
			t.Errorf("definition ref: %+v", d)
		}
	}
	if len(out.Composites) != 2 || len(out.Claims) != 1 || out.Claims[0].Composite != "assets" {
		t.Fatalf("XR/claims: %+v %+v", out.Composites, out.Claims)
	}
	for _, x := range out.Composites {
		if x.Name == "app-db" && (x.Namespace != "apps" || x.Composition != "database-standard" || x.Resources != 2) {
			t.Errorf("v2 XR: %+v", x)
		}
		if x.Name == "assets" && (x.Claim != "assets-claim" || x.Resources != 1) {
			t.Errorf("v1 XR: %+v", x)
		}
	}
	if out.ManagedTotal != 5 || len(out.Managed) != 5 || out.Truncated || out.Managed[0].Synced.Reason != "ReconcileError" || out.Managed[0].LastTransition != fixtureNow.Add(-time.Hour).UnixMilli() {
		t.Fatalf("managed: %+v", out)
	}
	for _, m := range out.Managed {
		if m.Name == "assets-bucket" && (m.ProviderConfig != "storage-auth" || m.ExternalName != "assets-external" || m.Composite != "assets" || len(m.ManagementPolicies) != 2) {
			t.Errorf("managed fields: %+v", m)
		}
		if m.Name == "paused-db" && (!m.Paused || m.Health != healthWarning || slices.Contains(m.Reasons, xpReasonSyncFailed)) {
			t.Errorf("pause: %+v", m)
		}
	}
	for _, r := range f.recorded() {
		if strings.Contains(r.path, "notlistable") || strings.Contains(r.path, "ignoredclaims") || strings.Contains(r.path, "/xbroken") || strings.HasSuffix(r.path, "/status") {
			t.Errorf("unexpected listing: %s", r.path)
		}
	}
}

func TestReadCrossplanePartialErrors(t *testing.T) {
	for _, section := range []string{"managed", "definitions", "package", "discovery"} {
		t.Run(section, func(t *testing.T) {
			f, k, groups := crossplaneAPI(t)
			path := map[string]string{"managed": "/apis/sql.example.org/v1/databases", "definitions": "/apis/apiextensions.crossplane.io/v1/compositeresourcedefinitions", "package": "/apis/pkg.crossplane.io/v1/providers", "discovery": "/apis/sql.example.org/v1"}[section]
			f.answerWith("GET "+path, 403, `{"kind":"Status","reason":"Forbidden","message":"listing denied"}`)
			out, err := readCrossplane(context.Background(), k, groups, fixtureNow)
			if err != nil || !out.Installed || !strings.Contains(out.Error, path) || len(out.Managed) == 0 {
				t.Fatalf("partial status: %+v %v", out, err)
			}
		})
	}
}

func TestReadCrossplaneAbsent(t *testing.T) {
	out, err := readCrossplane(context.Background(), nil, map[string]string{}, fixtureNow)
	if err != nil || out.Installed || out.Managed == nil || out.Packages == nil || out.Definitions == nil || out.Composites == nil || out.Claims == nil {
		t.Fatalf("absent: %+v %v", out, err)
	}
}

func TestCrossplaneHealth(t *testing.T) {
	for _, tc := range []struct {
		name, status, reason, at string
		paused                   bool
		health, why              string
	}{
		{"sync error", "False", "ReconcileError", "", false, healthCritical, xpReasonSyncFailed},
		{"pause condition", "False", "ReconcilePaused", "", false, healthWarning, xpReasonPaused},
		{"pause annotation", "True", "", "", true, healthWarning, xpReasonPaused},
		{"old creating", "False", "Creating", "2026-10-03T11:00:00Z", false, healthWarning, xpReasonNotReady},
		{"young creating", "False", "Creating", "2026-10-03T11:58:00Z", false, healthOK, ""},
		{"unknown age", "False", "Creating", "invalid", false, healthOK, ""},
		{"unavailable", "False", "Unavailable", "", false, healthWarning, xpReasonNotReady},
		{"deleting", "False", "Deleting", "", false, healthWarning, xpReasonDeleting},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var obj xpResourceObject
			conditionType := "Ready"
			if strings.HasPrefix(tc.name, "sync") || tc.name == "pause condition" {
				conditionType = "Synced"
			}
			obj.Status.Conditions = []kubeCondition{{Type: conditionType, Status: tc.status, Reason: tc.reason, LastTransitionTime: tc.at}}
			if tc.paused {
				obj.Metadata.Annotations = map[string]string{"crossplane.io/paused": "true"}
			}
			out := mapCrossplaneObject(obj, xpObjectRef{}, fixtureNow)
			if out.Health != tc.health || (tc.why != "" && !slices.Contains(out.Reasons, tc.why)) {
				t.Fatalf("health: %+v", out)
			}
		})
	}
}

func TestCrossplaneManagedPaginationAndCap(t *testing.T) {
	var inFlight, maxFlight atomic.Int32
	f := &fakeKubeAPI{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"gitVersion":"v1.34.0"}`))
			return
		}
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := maxFlight.Load(); n > old && !maxFlight.CompareAndSwap(old, n); old = maxFlight.Load() {
		}
		time.Sleep(time.Millisecond)
		if r.URL.Query().Get("limit") != "500" {
			t.Errorf("page size: %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("continue")
		var items []map[string]any
		first := 0
		last := 500
		next := "second"
		if page == "second" {
			first = 500
			last = 505
			next = ""
		}
		for i := first; i < last; i++ {
			conditions := []map[string]string{{"type": "Ready", "status": "True"}, {"type": "Synced", "status": "True"}}
			if i == 504 {
				conditions[1] = map[string]string{"type": "Synced", "status": "False", "reason": "ReconcileError", "message": "quota exceeded"}
			}
			items = append(items, map[string]any{"metadata": map[string]string{"name": fmt.Sprintf("row-%03d", i)}, "status": map[string]any{"conditions": conditions}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"continue": next}, "items": items})
	}))
	t.Cleanup(f.Close)
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	resources := []kubeBrowserResource{}
	for i := range 10 {
		resources = append(resources, kubeBrowserResource{Group: fmt.Sprintf("g%d.example.org", i), Version: "v1", Resource: "buckets", Kind: "Bucket"})
	}
	out, total, counts, err := listCrossplaneManaged(context.Background(), k, resources, fixtureNow)
	if err != nil || total != 5050 || len(out) != 510 || counts.OK != 5040 || counts.Critical != 10 {
		t.Fatalf("cap: total=%d rows=%d counts=%+v err=%v", total, len(out), counts, err)
	}
	if maxFlight.Load() > 8 || maxFlight.Load() < 2 {
		t.Errorf("concurrency: %d", maxFlight.Load())
	}
	for _, x := range out[:10] {
		if x.Health != healthCritical {
			t.Errorf("problem lost beyond cap: %+v", x)
		}
	}
}

func TestReadIntegrationsCrossplaneGroupsAreSupported(t *testing.T) {
	_, k, _ := crossplaneAPI(t)
	out, err := readIntegrations(context.Background(), k)
	if err != nil || len(out.Families) != 0 || !slices.Contains(out.Supported, "crossplane") {
		t.Fatalf("integrations: %+v %v", out, err)
	}
	raw, err := SupportedIntegrations()
	if err != nil || !strings.Contains(raw, `"id":"crossplane"`) {
		t.Fatalf("supported: %s %v", raw, err)
	}
}

func TestKubeCrossplaneDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := KubeCrossplane(cfg, "Demo cluster", "")
	if err != nil {
		t.Fatal(err)
	}
	var out crossplaneStatus
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Installed || len(out.Packages) != 3 || len(out.Definitions) != 2 || len(out.Composites) != 2 || len(out.Claims) != 1 || out.ManagedTotal != 9 {
		t.Fatalf("demo: %+v", out)
	}
	reasons := []string{}
	for _, p := range out.Packages {
		reasons = append(reasons, p.Reasons...)
	}
	for _, d := range out.Definitions {
		reasons = append(reasons, d.Reasons...)
	}
	for _, m := range out.Managed {
		reasons = append(reasons, m.Reasons...)
	}
	for _, reason := range []string{xpReasonSyncFailed, xpReasonProviderUnhealthy, xpReasonNotReady, xpReasonDeleting, xpReasonPaused, xpReasonNotEstablished} {
		if !slices.Contains(reasons, reason) {
			t.Errorf("demo missing reason %s", reason)
		}
	}
	if out.Counts.OK == 0 || out.Counts.Warning == 0 || out.Counts.Critical == 0 {
		t.Errorf("demo counts: %+v", out.Counts)
	}
}

func TestCrossplaneManagedExpiredPageResetsCounts(t *testing.T) {
	var expired atomic.Bool
	f := &fakeKubeAPI{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"gitVersion":"v1.34.0"}`))
			return
		}
		if r.URL.Query().Get("continue") == "next" && expired.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"kind":"Status","reason":"Expired","message":"the list expired"}`))
			return
		}
		next, name := "next", "first"
		if r.URL.Query().Get("continue") == "next" {
			next, name = "", "second"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"continue": next}, "items": []any{map[string]any{"metadata": map[string]string{"name": name}}}})
	}))
	t.Cleanup(f.Close)
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	rows, total, counts, err := listCrossplaneManaged(context.Background(), k, []kubeBrowserResource{{Group: "storage.example.org", Version: "v1", Resource: "buckets", Kind: "Bucket"}}, fixtureNow)
	if err != nil || total != 2 || len(rows) != 2 || counts.OK != 2 || !expired.Load() {
		t.Fatalf("expired list: total=%d rows=%+v counts=%+v err=%v", total, rows, counts, err)
	}
}

func TestCrossplaneCompositionNamesAreCapped(t *testing.T) {
	f, k, groups := crossplaneAPI(t)
	items := []any{}
	for i := range 25 {
		items = append(items, map[string]any{"metadata": map[string]string{"name": fmt.Sprintf("composition-%02d", i)}, "spec": map[string]any{"compositeTypeRef": map[string]string{"kind": "XBucket", "apiVersion": "platform.example.org/v1beta1"}}})
	}
	raw, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}
	f.answerWith("GET /apis/apiextensions.crossplane.io/v1/compositions", 200, string(raw))
	out, err := readCrossplane(context.Background(), k, groups, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range out.Definitions {
		if d.CompositeKind == "XBucket" && (d.Compositions != 25 || len(d.CompositionNames) != 20 || !slices.IsSorted(d.CompositionNames)) {
			t.Fatalf("composition names: %+v", d)
		}
	}
}
