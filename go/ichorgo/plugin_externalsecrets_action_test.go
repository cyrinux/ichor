package ichorgo

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The External Secrets plugin's Refresh now action (forceSync) over the httptest API of
// plugin_externalsecrets_test.go.

const esObjectPath = "/apis/external-secrets.io/v1/namespaces/apps/externalsecrets/"

// esObject is an ExternalSecret as GET returns it, at resourceVersion 812.
func esObject(name, policy string) string {
	return `{"apiVersion":"external-secrets.io/v1","kind":"ExternalSecret","metadata":{"name":"` + name +
		`","namespace":"apps","resourceVersion":"812"},"spec":{"refreshPolicy":"` + policy +
		`","secretStoreRef":{"name":"vault"}},"status":{"refreshTime":"2026-10-14T11:40:00Z"}}`
}

// forceSyncButton is the row's Refresh now button.
func forceSyncButton(t *testing.T, row plugin.TreeRow, label string) map[string]any {
	t.Helper()

	b, ok := rowItems(row, "actions")[label]
	if !ok {
		t.Fatalf("no %q button in %+v", label, row.Detail)
	}

	return b
}

// Every ExternalSecret row carries Refresh now; a CreatedOnce one has it off with the reason,
// since the controller never refreshes it again whatever the annotation says.
func TestExternalSecretsForceSyncInTheTree(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusOK, http.StatusOK)

	rows := esRowsByKey(t, pluginData(t, "cfg", "ctx", "", "external-secrets", "", ""), "secrets")
	for key, row := range rows {
		b := forceSyncButton(t, row, "Refresh now")

		wantEnabled := key != "apps/created-once"
		if b["id"] != "forceSync" || b["enabled"] != wantEnabled || b["confirm"] != "plain" {
			t.Errorf("%s: %+v, want enabled %v", key, b, wantEnabled)
		}

		if reason, _ := b["disabledReason"].(string); wantEnabled != (reason == "") {
			t.Errorf("%s: disabled reason %q", key, reason)
		}
	}

	if b := forceSyncButton(t, rows["apps/created-once"], "Refresh now"); b["disabledReason"] != "refreshed once by policy" {
		t.Errorf("created-once: %+v", b)
	}

	fr := esRowsByKey(t, pluginData(t, "cfg", "ctx", "", "external-secrets", "", "fr"), "secrets")
	if b := forceSyncButton(t, fr["apps/created-once"], "Actualiser maintenant"); b["disabledReason"] != "actualisé une seule fois, par sa politique" {
		t.Errorf("French button %+v", b)
	}
}

// Refresh now reads the ExternalSecret, then sends one merge patch of the force-sync
// annotation at the clock's unix milliseconds, pinned to the resourceVersion it checked the
// policy on, and records it in the audit log.
func TestExternalSecretsForceSyncPatches(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	useBuiltinPlugin(t, "external-secrets", esNow)

	f := esAPI(t, http.StatusOK, http.StatusOK)
	f.answerWith("GET "+esObjectPath+"synced", http.StatusOK, esObject("synced", "Periodic"))
	f.answerWith("PATCH "+esObjectPath+"synced", http.StatusOK, esObject("synced", "Periodic"))

	if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "external-secrets", "forceSync", "apps", "synced", ""); err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf(`{"metadata":{"annotations":{"external-secrets.io/force-sync":"%d"},"resourceVersion":"812"}}`, esNow.UnixMilli())
	if got := mutation(t, f); got.method != http.MethodPatch || got.path != esObjectPath+"synced" ||
		got.contentType != "application/merge-patch+json" || got.body != want {
		t.Errorf("got %s %s %s\n%s\nwant %s", got.method, got.path, got.contentType, got.body, want)
	}

	entries := readAudit(t, "", "plugin:external-secrets:forceSync")
	if len(entries) != 1 || entries[0].Outcome != auditOK || entries[0].Namespace != "apps" || entries[0].Object != "ExternalSecret/synced" {
		t.Errorf("audit %+v", entries)
	}
}

// A CreatedOnce ExternalSecret, as read now, is refused with the reason and nothing is sent.
func TestExternalSecretsForceSyncCreatedOnceRefused(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	useBuiltinPlugin(t, "external-secrets", esNow)

	f := esAPI(t, http.StatusOK, http.StatusOK)
	f.answerWith("GET "+esObjectPath+"created-once", http.StatusOK, esObject("created-once", "CreatedOnce"))

	err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "external-secrets", "forceSync", "apps", "created-once", "")

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusConflict || apiErr.Message != "refreshed once by policy" {
		t.Fatalf("got %v", err)
	}

	if slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method != http.MethodGet }) {
		t.Errorf("sent anyway: %+v", f.recorded())
	}

	if entries := readAudit(t, "", "plugin:external-secrets:forceSync"); len(entries) != 1 || entries[0].Outcome != auditFailed {
		t.Errorf("audit %+v", entries)
	}
}

// The demo shows the button but never runs it; the can-I row is the patch of the
// ExternalSecret and the get that reads it first.
func TestExternalSecretsForceSyncDemoAndAccess(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)

	rows := esRowsByKey(t, pluginData(t, demo, demoKubeContext, "", "external-secrets", "", ""), "secrets")
	if b := forceSyncButton(t, rows["demo-shop/api-credentials"], "Refresh now"); b["enabled"] != true {
		t.Errorf("demo button %+v", b)
	}

	if err := KubePluginAction(demo, demoKubeContext, "", "external-secrets", "forceSync", "demo-shop", "api-credentials", ""); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	want := []accessNeed{
		{ssarAttributes{Verb: "patch", Group: "external-secrets.io", Resource: "externalsecrets"}, true},
		{ssarAttributes{Verb: "get", Group: "external-secrets.io", Resource: "externalsecrets"}, true},
	}
	if got := kubeActionChecks["plugin:external-secrets:forceSync"]; !slices.Equal(got, want) {
		t.Errorf("can-I row %+v, want %+v", got, want)
	}
}
