package ichorgo

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The External Secrets built-in plugin over an httptest API serving
// testdata/externalsecrets: one ExternalSecret per health it gives, the store joins by kind
// and namespace, and the optional stores and target Secrets that a kubeconfig may not list.

// esNow is the fixtures' clock: Wednesday noon UTC.
var esNow = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

const (
	esGroups    = `{"groups":[{"name":"external-secrets.io","preferredVersion":{"version":"v1"}}]}`
	esResources = `{"resources":[{"name":"externalsecrets","namespaced":true,"kind":"ExternalSecret"},` +
		`{"name":"secretstores","namespaced":true,"kind":"SecretStore"},{"name":"clustersecretstores","namespaced":false,"kind":"ClusterSecretStore"}]}`
)

// useBuiltinPlugin makes the built-in plugin id the only one the cluster calls know, at now.
func useBuiltinPlugin(t *testing.T, id string, now time.Time) *plugin.Compiled {
	t.Helper()

	var c *plugin.Compiled

	for _, b := range plugin.Builtins() {
		if b.Plugin.ID == id {
			c = b
		}
	}

	if c == nil {
		t.Fatalf("no %s built-in plugin", id)
	}

	savedPlugins, savedNow := knownPlugins, pluginNow
	knownPlugins = func() []*plugin.Compiled { return []*plugin.Compiled{c} }
	pluginNow = func() time.Time { return now }

	t.Cleanup(func() { knownPlugins, pluginNow = savedPlugins, savedNow })

	return c
}

// esAPI serves the fixtures; storesStatus and secretsStatus answer the SecretStore and the
// Secret lists (200: the fixture, 403: a kubeconfig that may not list them, else a failure).
func esAPI(t *testing.T, storesStatus, secretsStatus int) *fakeKubeAPI {
	t.Helper()

	const base = "GET /apis/external-secrets.io/v1/"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                        esGroups,
		"GET /apis/external-secrets.io/v1": esResources,
		base + "externalsecrets":           string(readFixture(t, "externalsecrets/externalsecrets.json")),
		base + "clustersecretstores":       string(readFixture(t, "externalsecrets/clustersecretstores.json")),
		base + "secretstores":              string(readFixture(t, "externalsecrets/secretstores.json")),
		"GET /api/v1/secrets":              string(readFixture(t, "externalsecrets/secrets.json")),
	})

	for key, status := range map[string]int{base + "secretstores": storesStatus, "GET /api/v1/secrets": secretsStatus} {
		switch status {
		case http.StatusOK:
		case http.StatusForbidden:
			f.answerWith(key, status, forbiddenStatus)
		default:
			f.answerWith(key, status, fmt.Sprintf(`{"kind":"Status","status":"Failure","message":"etcd is down","code":%d}`, status))
		}
	}

	useFakeKube(t, f)

	return f
}

// esWant is what one ExternalSecret's row says: its health tone and reason, its status text,
// and the store and target Secret it links when they can be read.
type esWant struct {
	tone, reason, status, store, target string
}

// esRows are the fixtures' rows when every list can be read.
var esRows = map[string]esWant{
	"apps/synced":                   {"ok", "", "Synced", "vault", "synced"},
	"apps/sync-error":               {"warning", "notSynced", "Not synced", "vault", ""},
	"apps/deleted":                  {"critical", "targetMissing", "Deleted", "vault", ""},
	"apps/missing":                  {"critical", "targetMissing", "Missing", "vault", ""},
	"apps/in-sync-window":           {"ok", "", "Synced", "vault", ""},
	"apps/stale":                    {"warning", "stale", "Synced", "vault", ""},
	"apps/within-grace":             {"ok", "", "Synced", "vault", ""},
	"apps/created-once":             {"ok", "", "Synced", "vault", ""},
	"apps/never-refreshes":          {"ok", "", "Synced", "vault", ""},
	"apps/pending":                  {"idle", "neverSynced", "Pending", "fresh", ""},
	"apps/on-unvalidated-store":     {"ok", "", "Synced", "fresh", "db-password"},
	"tools/on-broken-cluster-store": {"warning", "storeNotReady", "Synced", "broken-kms", ""},
	"tools/on-cluster-store":        {"ok", "", "Synced", "shared", ""},
	"tools/on-broken-store":         {"warning", "storeNotReady", "Synced", "old", ""},
	"tools/store-elsewhere":         {"ok", "", "Synced", "", ""},
}

// esRowsByKey are a secrets card's rows by namespace/name.
func esRowsByKey(t *testing.T, tree plugin.Tree, card string) map[string]plugin.TreeRow {
	t.Helper()

	for _, c := range tree.Cards {
		if c.ID != card {
			continue
		}

		out := map[string]plugin.TreeRow{}
		for _, row := range c.Rows {
			out[row.Ref.Namespace+"/"+row.Ref.Name] = row
		}

		return out
	}

	t.Fatalf("no %s card in %+v", card, tree.Cards)

	return nil
}

// relationName is the name of the object a row links under label, "" without one.
func relationName(row plugin.TreeRow, label string) string {
	ref, _ := rowItems(row, "relations")[label]["ref"].(map[string]any)
	name, _ := ref["name"].(string)

	return name
}

func assertESRow(t *testing.T, key string, row plugin.TreeRow, want esWant) {
	t.Helper()

	if row.Health == nil || row.Health.Tone != want.tone || row.Health.Reason != want.reason {
		t.Errorf("%s: health %+v, want %s %q", key, row.Health, want.tone, want.reason)
	}

	if row.Status == nil || row.Status.Text != want.status {
		t.Errorf("%s: status %+v, want %q", key, row.Status, want.status)
	}

	if got := relationName(row, "Store"); got != want.store {
		t.Errorf("%s: store relation %q, want %q", key, got, want.store)
	}

	if got := relationName(row, "Target Secret"); got != want.target {
		t.Errorf("%s: target relation %q, want %q", key, got, want.target)
	}
}

// Every ExternalSecret's health, status and links, the summary and the findings, with every
// list readable.
func TestExternalSecretsPluginRows(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	f := esAPI(t, http.StatusOK, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "")
	if !tree.Active || tree.APIVersion != "v1" || len(tree.Errors) != 0 {
		t.Fatalf("tree active %v, version %q, errors %+v", tree.Active, tree.APIVersion, tree.Errors)
	}

	rows := esRowsByKey(t, tree, "secrets")
	if len(rows) != len(esRows) {
		t.Fatalf("%d rows, want %d", len(rows), len(esRows))
	}

	for key, want := range esRows {
		assertESRow(t, key, rows[key], want)
	}

	if msg := rows["apps/sync-error"].Health.Message; msg != "does not sync: could not get secret data from provider: permission denied" {
		t.Errorf("sync-error message %q", msg)
	}

	values := rowItems(rows["apps/synced"], "keyValues")
	for label, want := range map[string]string{
		"Store": "SecretStore/vault", "Refresh interval": "1h0m0s", "Refresh policy": "Periodic",
		"Target Secret": "synced", "Creation policy": "Owner", "Last refresh": "20m ago",
	} {
		if got := values[label]["value"]; got != want {
			t.Errorf("synced: %s = %v, want %q", label, got, want)
		}
	}

	badges := map[string]string{}
	for _, b := range tree.Summary.Badges {
		badges[b.Label] = b.Value
	}

	// Not synced: sync-error (deleted and missing are Ready); stale: stale (in-sync-window is
	// held back on purpose); stores down: old, broken-kms.
	if tree.Summary.Title != "15 external secrets" || badges["not synced"] != "1" || badges["stale"] != "1" ||
		badges["stores down"] != "2" || tree.Summary.Health != "critical" {
		t.Errorf("summary %+v", tree.Summary)
	}

	findings := map[string]int{}
	for _, fd := range tree.Findings {
		findings[fd.ID]++
	}

	if fmt.Sprint(findings) != fmt.Sprint(map[string]int{"secret-warning": 4, "target-missing": 2, "store-not-ready": 1, "cluster-store-not-ready": 1}) {
		t.Errorf("findings %v", findings)
	}

	for _, r := range f.recorded() {
		if r.path == "/api/v1/secrets" && !strings.Contains(r.query, "labelSelector=reconcile.external-secrets.io%2Fmanaged%3Dtrue") {
			t.Errorf("secrets listed without the managed label: %s", r.query)
		}
	}
}

// The store cards: health by the Ready condition, a cluster store named by its kind.
func TestExternalSecretsPluginStores(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusOK, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "")
	got := map[string]string{}

	for _, card := range []string{"stores", "clusterStores"} {
		for key, row := range esRowsByKey(t, tree, card) {
			got[key] = row.Health.Tone + " " + row.Status.Text + " " + row.Subtitle
		}
	}

	want := map[string]string{
		"apps/vault": "ok Ready apps", "apps/fresh": "unknown Pending apps", "tools/old": "critical Not ready tools",
		"/broken-kms": "critical Not ready ClusterSecretStore", "/shared": "ok Ready ClusterSecretStore",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("stores %v, want %v", got, want)
	}
}

// A kubeconfig that may not list Secrets sees every row without a target link, and no error.
func TestExternalSecretsPluginSecretsForbidden(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusOK, http.StatusForbidden)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "")
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	rows := esRowsByKey(t, tree, "secrets")
	if len(rows) != len(esRows) {
		t.Fatalf("%d rows, want %d", len(rows), len(esRows))
	}

	for key, want := range esRows {
		want.target = ""
		assertESRow(t, key, rows[key], want)
	}
}

// A kubeconfig that may not list SecretStores: their readiness is unknown, so an
// ExternalSecret on a broken one is not warned about, and there is no error.
func TestExternalSecretsPluginStoresForbidden(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusForbidden, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "")
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	rows := esRowsByKey(t, tree, "secrets")
	for key, want := range esRows {
		if want.store != "broken-kms" && want.store != "shared" {
			want.store = ""
		}

		if key == "tools/on-broken-store" {
			want.tone, want.reason = "ok", ""
		}

		assertESRow(t, key, rows[key], want)
	}

	if n := len(esRowsByKey(t, tree, "stores")); n != 0 {
		t.Errorf("%d SecretStore rows", n)
	}
}

// An optional resource is silent when forbidden, not when the server fails.
func TestExternalSecretsPluginSecretsFailure(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusOK, http.StatusInternalServerError)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "")
	if len(tree.Errors) != 1 || tree.Errors[0].Resource != "secrets" || len(esRowsByKey(t, tree, "secrets")) != len(esRows) {
		t.Fatalf("errors %+v", tree.Errors)
	}
}

// The French tree translates the plugin's words; the controller's message stays as written.
func TestExternalSecretsPluginFrench(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	esAPI(t, http.StatusOK, http.StatusOK)

	tree := pluginData(t, "cfg", "ctx", "", "external-secrets", "", "fr")
	if tree.Summary.Title != "15 secrets externes" || tree.Cards[0].ID != "secrets" || tree.Cards[0].Empty != "Aucun secret externe." {
		t.Fatalf("summary %+v, cards %+v", tree.Summary, tree.Cards)
	}

	rows := esRowsByKey(t, tree, "secrets")
	if r := rows["apps/sync-error"]; r.Health.Message != "ne se synchronise pas : could not get secret data from provider: permission denied" || r.Status.Text != "Non synchronisé" {
		t.Errorf("sync-error in French: %+v %+v", r.Health, r.Status)
	}

	if _, ok := rowItems(rows["apps/synced"], "keyValues")["Intervalle de rafraîchissement"]; !ok {
		t.Errorf("synced labels in French: %+v", rowItems(rows["apps/synced"], "keyValues"))
	}
}

// The demo renders the samples: one row per health the plugin gives an ExternalSecret.
func TestExternalSecretsPluginDemo(t *testing.T) {
	useBuiltinPlugin(t, "external-secrets", esNow)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "external-secrets", "", "")

	got := map[string]string{}
	for key, row := range esRowsByKey(t, tree, "secrets") {
		got[key] = row.Health.Tone + " " + row.Health.Reason
	}

	want := map[string]string{
		"demo-shop/api-credentials": "ok ", "demo-shop/payment-keys": "warning notSynced", "demo-shop/smtp-relay": "warning stale",
		"demo-lab/registry-pull": "critical targetMissing", "demo-lab/feature-flags": "idle neverSynced",
		"demo-lab/tls-bundle": "warning storeNotReady",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) || len(tree.Errors) != 0 {
		t.Fatalf("demo rows %v, errors %+v", got, tree.Errors)
	}
}

// Settings lists External Secrets once, from the plugin, whose detect decides.
func TestExternalSecretsIntegrationFromThePlugin(t *testing.T) {
	var rows []integrationSpec

	for _, s := range integrationSpecs() {
		if s.ID == "external-secrets" {
			rows = append(rows, s)
		}
	}

	if len(rows) != 1 || rows[0].fromPlugin == nil || rows[0].fromPlugin.Plugin.ID != "external-secrets" ||
		fmt.Sprint(rows[0].Groups) != fmt.Sprint([]string{groupExternalSecrets}) {
		t.Fatalf("external-secrets rows %+v", rows)
	}
}
