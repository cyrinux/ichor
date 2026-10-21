package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Sealed Secrets built-in plugin against what the Data services reader, the checkup and
// the monitor it replaced said about the same objects (their tests' routes and fixtures):
// each SealedSecret's health and first reason, the sealing keys and the controller, the
// section's health, and findings where the checkup and the monitor reported.

func sealedSecretsRoutes() map[string]string {
	return map[string]string{
		"GET /apis":                                    `{"groups":[{"name":"bitnami.com","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/bitnami.com/v1alpha1":               `{"resources":[{"name":"sealedsecrets"}]}`,
		"GET /apis/bitnami.com/v1alpha1/sealedsecrets": `{"items":[]}`,
		"GET /api/v1/secrets":                          `{"items":[{"metadata":{"name":"key-new","namespace":"kube-system","creationTimestamp":"2026-10-01T00:00:00Z","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"active"}},"data":{"tls.key":"private-key-sentinel"}}]}`,
		"GET /apis/apps/v1/deployments":                `{"items":[{"metadata":{"name":"sealed-secrets-controller","namespace":"kube-system"},"spec":{"replicas":1,"template":{"spec":{"containers":[{"image":"ghcr.io/bitnami/sealed-secrets-controller:v0.32.0"}]}}},"status":{"readyReplicas":1}}]}`,
	}
}

// sealedSecretsTree renders the plugin, alone and at fixtureNow, over routes; refused
// answers 403 to those requests.
func sealedSecretsTree(t *testing.T, routes map[string]string, refused ...string) (plugin.Tree, *fakeKubeAPI) {
	t.Helper()

	useBuiltinPlugin(t, "sealed-secrets", fixtureNow)

	f := newFakeKubeAPI(t, routes)
	for _, r := range refused {
		f.answerWith(r, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"read refused"}`)
	}

	useFakeKube(t, f)

	return pluginData(t, "cfg", "ctx", "", "sealed-secrets", "", ""), f
}

func findingsOf(tree plugin.Tree) []string {
	out := []string{}
	for _, f := range tree.Findings {
		out = append(out, f.Severity+" "+f.ID+" "+f.Title)
	}

	slices.Sort(out)

	return out
}

// Each SealedSecret's health, scope and values count, as the reader mapped them.
func TestSealedSecretsPluginItems(t *testing.T) {
	cases := []struct {
		name, status, reason, scope, health, why string
		generation, observed                     int64
	}{
		{"synced", "True", "", "strict", healthOK, "", 1, 1},
		{"unseal", "False", "UnsealFailed", "cluster-wide", healthCritical, "notSynced", 2, 1},
		{"unmanaged", "False", "Unmanaged", "namespace-wide", healthWarning, "unmanaged", 1, 1},
		{"unknown", "Unknown", "", "strict", healthOK, "", 1, 1},
		{"absent", "", "", "strict", healthOK, "", 1, 1},
		{"stale", "True", "", "strict", healthWarning, "stale", 3, 2},
	}

	var items []string

	for _, tt := range cases {
		obj := map[string]any{
			"metadata": map[string]any{"name": "credentials", "namespace": tt.name, "generation": tt.generation, "creationTimestamp": "2026-10-01T00:00:00Z",
				"annotations": map[string]string{"sealedsecrets.bitnami.com/" + tt.scope: "true"}},
			"spec":   map[string]any{"encryptedData": map[string]string{"password": "ciphertext-sentinel", "username": "ciphertext-sentinel"}, "template": map[string]any{"type": "Opaque", "immutable": true}},
			"status": map[string]any{"observedGeneration": tt.observed},
		}
		if tt.status != "" {
			obj["status"].(map[string]any)["conditions"] = []map[string]string{{"type": "Synced", "status": tt.status, "reason": tt.reason, "message": "controller detail"}}
		}

		raw, err := json.Marshal(obj)
		if err != nil {
			t.Fatal(err)
		}

		items = append(items, string(raw))
	}

	routes := sealedSecretsRoutes()
	routes["GET /apis/bitnami.com/v1alpha1/sealedsecrets"] = `{"items":[` + strings.Join(items, ",") + `]}`
	tree, _ := sealedSecretsTree(t, routes)

	rows := map[string]plugin.TreeRow{}
	for _, row := range rookCard(t, tree, "secrets").Rows {
		rows[row.Subtitle] = row
	}

	for _, tt := range cases {
		row := rows[tt.name]
		if row.Health == nil || row.Health.Tone != tt.health || row.Health.Reason != tt.why {
			t.Errorf("%s: health %+v, want %s %s", tt.name, row.Health, tt.health, tt.why)
		}

		if scope := rowItems(row, "keyValues")["Scope"]["value"]; scope != tt.scope {
			t.Errorf("%s: scope %v, want %s", tt.name, scope, tt.scope)
		}

		kv := rowItems(row, "keyValues")
		if kv["Keys"]["value"] != "2" || kv["Type"]["value"] != "Opaque" || rowItems(row, "badges")["immutable"]["tone"] != "ok" {
			t.Errorf("%s: details %v %v", tt.name, kv, rowItems(row, "badges"))
		}
	}
}

// The section as a whole: the keys and the controller decide it, as in the reader.
func TestSealedSecretsPluginSection(t *testing.T) {
	for _, tt := range []struct {
		name, route, body, health string
		inactive                  bool
		findings                  []string
	}{
		{name: "installed", health: healthOK, findings: []string{}},
		{name: "no group", route: "GET /apis", body: `{"groups":[]}`, inactive: true},
		{name: "shared group", route: "GET /apis/bitnami.com/v1alpha1", body: `{"resources":[{"name":"other"}]}`, inactive: true},
		{name: "no keys", route: "GET /api/v1/secrets", body: `{"items":[]}`, health: healthCritical,
			findings: []string{"critical no-active-key kube-system/sealed-secrets-controller"}},
		{name: "compromised", route: "GET /api/v1/secrets", body: `{"items":[{"metadata":{"name":"old-key","namespace":"kube-system","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"compromised"}}}]}`,
			health: healthCritical, findings: []string{"critical no-active-key kube-system/sealed-secrets-controller", "warning key-compromised kube-system/old-key"}},
		{name: "key stale", route: "GET /api/v1/secrets", body: `{"items":[{"metadata":{"name":"old-key","namespace":"kube-system","creationTimestamp":"2026-08-01T00:00:00Z","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"active"}}}]}`,
			health: healthWarning, findings: []string{}},
		{name: "controller missing", route: "GET /apis/apps/v1/deployments", body: `{"items":[]}`, health: healthCritical, findings: []string{}},
		{name: "controller stopped", route: "GET /apis/apps/v1/deployments",
			body:   `{"items":[{"metadata":{"name":"controller","namespace":"security"},"spec":{"replicas":0,"template":{"spec":{"containers":[{"image":"docker.io/bitnami/sealed-secrets-controller:0.31.0"}]}}},"status":{"readyReplicas":0}}]}`,
			health: healthCritical, findings: []string{"critical controller-down security/controller"}},
		// Another workload: not the controller, which is then missing.
		{name: "other deployment", route: "GET /apis/apps/v1/deployments",
			body:   `{"items":[{"metadata":{"name":"web","namespace":"shop"},"spec":{"replicas":1,"template":{"spec":{"containers":[{"image":"docker.io/library/nginx:1.27"}]}}},"status":{"readyReplicas":1}}]}`,
			health: healthCritical, findings: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := sealedSecretsRoutes()
			if tt.route != "" {
				routes[tt.route] = tt.body
			}

			tree, _ := sealedSecretsTree(t, routes)
			if tt.inactive {
				if tree.Active {
					t.Fatal("active without Sealed Secrets")
				}

				return
			}

			if !tree.Active || len(tree.Errors) != 0 || tree.Summary.Health != tt.health {
				t.Fatalf("active %v, summary %+v, errors %+v", tree.Active, tree.Summary, tree.Errors)
			}

			if got := findingsOf(tree); !slices.Equal(got, tt.findings) {
				t.Errorf("findings %v, want %v", got, tt.findings)
			}
		})
	}
}

// A stale newest key says so on the controller's row.
func TestSealedSecretsPluginStaleKeyNote(t *testing.T) {
	routes := sealedSecretsRoutes()
	routes["GET /api/v1/secrets"] = `{"items":[{"metadata":{"name":"old-key","namespace":"kube-system","creationTimestamp":"2026-08-01T00:00:00Z","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"active"}}}]}`
	tree, _ := sealedSecretsTree(t, routes)

	row := rookCard(t, tree, "controller").Rows[0]
	if note := rowSection(row, "text"); note == nil || !strings.Contains(note.Text, "over 30 days old") || note.Tone != healthWarning {
		t.Fatalf("controller row %+v", row.Detail)
	}
}

// The reader's fixture: problems first, the scope the annotations give, and not one byte of
// a value or a key in the tree.
func TestSealedSecretsPluginFixture(t *testing.T) {
	useBuiltinPlugin(t, "sealed-secrets", fixtureNow)

	routes := sealedSecretsRoutes()
	routes["GET /apis/bitnami.com/v1alpha1/sealedsecrets"] = string(readFixture(t, "sealedsecrets/items.json"))
	f := newFakeKubeAPI(t, routes)
	useFakeKube(t, f)

	raw, err := KubePluginData("cfg", "ctx", "", "sealed-secrets", "", "")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(raw, "ciphertext-sentinel") || strings.Contains(raw, "private-key-sentinel") {
		t.Fatal("a secret value is in the tree")
	}

	tree := pluginTree(t, raw, nil)

	first := rookCard(t, tree, "secrets").Rows[0]
	if tree.Summary.Title != "3 SealedSecrets" || first.Title != "legacy-cert" || first.Trailing.Text != "cluster-wide" || first.Status.Text != "UnsealFailed" {
		t.Fatalf("summary %+v, first %+v", tree.Summary, first)
	}

	// The sealing keys are read as metadata: the private keys never leave the API server.
	for _, r := range f.recorded() {
		if r.path == "/api/v1/secrets" && !strings.Contains(r.query, "labelSelector=sealedsecrets.bitnami.com%2Fsealed-secrets-key") {
			t.Errorf("keys listed without their label: %s", r.query)
		}
	}
}

func TestSealedSecretsPluginKeysAsMetadata(t *testing.T) {
	useBuiltinPlugin(t, "sealed-secrets", fixtureNow)

	f := newFakeKubeAPI(t, sealedSecretsRoutes())
	f.beforeAnswer = func(r *http.Request) {
		if r.URL.Path == "/api/v1/secrets" && r.Header.Get("Accept") != "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1" {
			t.Errorf("sealing keys requested with their payloads: %s", r.Header.Get("Accept"))
		}
	}
	useFakeKube(t, f)

	if keys := rookCard(t, pluginData(t, "cfg", "ctx", "", "sealed-secrets", "", ""), "keys").Rows; len(keys) != 1 {
		t.Fatalf("keys %+v", keys)
	}
}

// A listing the account may not read is an error of the tree, never a false alarm: no keys
// visible is not no keys, no Deployment visible is not no controller.
func TestSealedSecretsPluginRefusedReads(t *testing.T) {
	for _, path := range []string{"/apis/bitnami.com/v1alpha1/sealedsecrets", "/api/v1/secrets", "/apis/apps/v1/deployments"} {
		t.Run(path, func(t *testing.T) {
			tree, _ := sealedSecretsTree(t, sealedSecretsRoutes(), "GET "+path)

			if len(tree.Errors) != 1 || !strings.Contains(tree.Errors[0].Message, "refused") {
				t.Fatalf("errors %+v", tree.Errors)
			}

			if tree.Summary.Health == healthCritical || len(tree.Findings) != 0 {
				t.Fatalf("false alarm: summary %+v, findings %v", tree.Summary, findingsOf(tree))
			}

			if keys := rookCard(t, tree, "keys").Rows; path != "/api/v1/secrets" && len(keys) != 1 {
				t.Errorf("lost keys: %+v", keys)
			}

			if controller := rookCard(t, tree, "controller").Rows; path != "/apis/apps/v1/deployments" && len(controller) != 1 {
				t.Errorf("lost controller: %+v", controller)
			}
		})
	}
}

// What the monitor and the checkup read (KubePluginFindings): a SealedSecret that failed to
// unseal is critical once the controller had its turn; a fresh one waits; one a Secret it
// does not own blocks is shown (info), not notified.
func TestSealedSecretsPluginFindingsForTheMonitor(t *testing.T) {
	const (
		notSynced = `{"metadata":{"name":"legacy-cert","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"UnsealFailed","message":"failed to decrypt: no key could decrypt secret"}]}}`
		unmanaged = `{"metadata":{"name":"cert-manager","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"Unmanaged"}]}}`
		// fixtureNow is 2026-10-03T12:00:00Z.
		fresh = `{"metadata":{"name":"just-applied","namespace":"shop","creationTimestamp":"2026-10-03T11:59:30Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"UnsealFailed"}]}}`
	)

	useBuiltinPlugin(t, "sealed-secrets", fixtureNow)

	routes := sealedSecretsRoutes()
	routes["GET /apis/bitnami.com/v1alpha1/sealedsecrets"] = `{"items":[` + notSynced + "," + unmanaged + "," + fresh + `]}`
	useFakeKube(t, newFakeKubeAPI(t, routes))

	raw, err := KubePluginFindings("cfg", "ctx", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var report pluginFindingsReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, f := range report.Findings {
		got = append(got, f.Severity+" "+f.Key)
	}

	want := []string{"critical plugin:sealed-secrets:unseal-failed:shop/legacy-cert", "info plugin:sealed-secrets:unmanaged:shop/cert-manager"}
	if !slices.Equal(got, want) || len(report.Errors) != 0 {
		t.Fatalf("findings %v, errors %+v", got, report.Errors)
	}
}

// Data services no longer carry Sealed Secrets: the plugin does.
func TestDataServicesHaveNoSealedSecrets(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "sealedSecrets") {
		t.Error("the Data services JSON still has a sealedSecrets key")
	}
}

// The demo renders the plugin's samples, as the former demo: two active keys and a
// compromised one, a SealedSecret that cannot be decrypted.
func TestSealedSecretsPluginDemo(t *testing.T) {
	useBuiltinPlugin(t, "sealed-secrets", fixtureNow)
	noKubeDial(t)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "sealed-secrets", "", "")
	if tree.Summary.Health != healthCritical || len(tree.Errors) != 0 {
		t.Fatalf("summary %+v, errors %+v", tree.Summary, tree.Errors)
	}

	want := []string{"critical unseal-failed demo-shop/legacy-cert", "info unmanaged demo-lab/shared-config", "warning key-compromised kube-system/sealed-secrets-key-rotated"}
	if got := findingsOf(tree); !slices.Equal(got, want) {
		t.Fatalf("findings %v", got)
	}
}
