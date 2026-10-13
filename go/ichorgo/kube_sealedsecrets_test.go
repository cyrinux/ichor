package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSealedSecretsHealth(t *testing.T) {
	for _, tt := range []struct {
		name, status, reason, scope, health string
		generation, observed                int64
		reasons                             []string
	}{
		{"synced", "True", "", "strict", healthOK, 1, 1, []string{}},
		{"unseal", "False", "UnsealFailed", "cluster-wide", healthCritical, 2, 1, []string{sealedReasonNotSynced, sealedReasonStale}},
		{"unmanaged", "False", "Unmanaged", "namespace-wide", healthWarning, 1, 1, []string{sealedReasonUnmanaged}},
		{"unknown", "Unknown", "", "strict", healthOK, 1, 1, []string{}},
		{"absent", "", "", "strict", healthOK, 1, 1, []string{}},
		{"stale", "True", "", "strict", healthWarning, 3, 2, []string{sealedReasonStale}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := `{"metadata":{"name":"credentials","namespace":"shop","generation":1,"creationTimestamp":"2026-10-01T00:00:00Z"},"spec":{"encryptedData":{"password":"ciphertext-sentinel","username":"ciphertext-sentinel"},"template":{"type":"Opaque","immutable":true}}}`
			var obj sealedSecretObject
			if err := json.Unmarshal([]byte(raw), &obj); err != nil {
				t.Fatal(err)
			}
			obj.Metadata.Generation = tt.generation
			obj.Status.ObservedGeneration = tt.observed
			obj.Metadata.Annotations = map[string]string{"sealedsecrets.bitnami.com/" + tt.scope: "true"}
			if tt.status != "" {
				obj.Status.Conditions = []kubeCondition{{Type: "Synced", Status: tt.status, Reason: tt.reason, Message: "controller detail", LastTransitionTime: "2026-10-02T00:00:00Z"}}
			}
			got := mapSealedSecret(obj)
			if got.Health != tt.health || got.Scope != tt.scope || !slices.Equal(got.Reasons, tt.reasons) {
				t.Fatalf("got %+v", got)
			}
			if got.KeyCount != 2 || !slices.Equal(got.Keys, []string{"password", "username"}) || !got.Immutable || got.Type != "Opaque" || got.Age != unixMilli("2026-10-01T00:00:00Z") {
				t.Fatalf("fields: %+v", got)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "ciphertext-sentinel") {
				t.Fatal("ciphertext leaked")
			}
		})
	}
}

func sealedSecretsRoutes() map[string]string {
	return map[string]string{
		"GET /apis":                                    `{"groups":[{"name":"bitnami.com","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /apis/bitnami.com/v1alpha1":               `{"resources":[{"name":"sealedsecrets"}]}`,
		"GET /apis/bitnami.com/v1alpha1/sealedsecrets": `{"items":[]}`,
		"GET /api/v1/secrets":                          `{"items":[{"metadata":{"name":"key-new","namespace":"kube-system","creationTimestamp":"2026-10-01T00:00:00Z","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"active"}},"data":{"tls.key":"private-key-sentinel"}}]}`,
		"GET /apis/apps/v1/deployments":                `{"items":[{"metadata":{"name":"sealed-secrets-controller","namespace":"kube-system"},"spec":{"replicas":1,"template":{"spec":{"containers":[{"image":"ghcr.io/bitnami/sealed-secrets-controller:v0.32.0"}]}}},"status":{"readyReplicas":1}}]}`,
	}
}

func TestReadDataServicesSealedSecrets(t *testing.T) {
	for _, tt := range []struct {
		name, route, body, health, reason string
		absent                            bool
	}{
		{name: "installed", health: healthOK},
		{name: "no group", route: "GET /apis", body: `{"groups":[]}`, absent: true},
		{name: "shared group", route: "GET /apis/bitnami.com/v1alpha1", body: `{"resources":[{"name":"other"}]}`, absent: true},
		{name: "no keys", route: "GET /api/v1/secrets", body: `{"items":[]}`, health: healthCritical, reason: sealedReasonNoKeys},
		{name: "compromised", route: "GET /api/v1/secrets", body: `{"items":[{"metadata":{"name":"old-key","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"compromised"}}}]}`, health: healthCritical, reason: sealedReasonKeyCompromised},
		{name: "key stale", route: "GET /api/v1/secrets", body: `{"items":[{"metadata":{"name":"old-key","creationTimestamp":"2026-08-01T00:00:00Z","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"active"}}}]}`, health: healthWarning, reason: sealedReasonKeyStale},
		{name: "controller down", route: "GET /apis/apps/v1/deployments", body: `{"items":[]}`, health: healthCritical, reason: sealedReasonControllerDown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := sealedSecretsRoutes()
			if tt.route != "" {
				routes[tt.route] = tt.body
			}
			f := newFakeKubeAPI(t, routes)
			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}
			ds, err := readDataServices(context.Background(), k, nil, parseHints("sealed-secrets"), fixtureNow)
			if err != nil {
				t.Fatal(err)
			}
			if tt.absent {
				if ds.SealedSecrets != nil {
					t.Fatal("uninstalled section returned")
				}
				return
			}
			got := ds.SealedSecrets
			if got == nil || got.Error != "" || got.Health != tt.health {
				t.Fatalf("section: %+v", got)
			}
			if tt.reason != "" && !slices.Contains(got.Reasons, tt.reason) {
				t.Fatalf("reasons: %+v", got)
			}
			encoded, _ := json.Marshal(ds)
			if strings.Contains(string(encoded), "private-key-sentinel") {
				t.Fatal("private key leaked")
			}
		})
	}
}

func TestSealedSecretsDemo(t *testing.T) {
	got := demoDataServices(fixtureNow).SealedSecrets
	if got == nil || len(got.Items) != 3 || len(got.Keys) != 2 || got.Controller == nil || !got.Controller.Ready {
		t.Fatalf("demo: %+v", got)
	}
	if got.Items[0].Health != healthCritical || !got.Items[0].Stale || got.Items[0].Reason != "UnsealFailed" {
		t.Fatalf("problem first: %+v", got.Items)
	}
	if got.Keys[0].CreatedAt <= fixtureNow.Add(-30*24*time.Hour).UnixMilli() {
		t.Fatal("demo keys are stale")
	}
}

func TestSealedSecretsFixtureOutput(t *testing.T) {
	routes := sealedSecretsRoutes()
	routes["GET /apis/bitnami.com/v1alpha1/sealedsecrets"] = string(readFixture(t, "sealedsecrets/items.json"))
	f := newFakeKubeAPI(t, routes)
	raw, err := KubeDataServices(f.kubeconfigFor(f.URL), "admin@test", "", "sealed-secrets")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "ciphertext-sentinel") || strings.Contains(raw, "private-key-sentinel") {
		t.Fatal("secret value leaked")
	}
	var ds dataServices
	if err := json.Unmarshal([]byte(raw), &ds); err != nil {
		t.Fatal(err)
	}
	got := ds.SealedSecrets
	if got == nil || got.SecretCount != 3 || got.Items[0].Name != "legacy-cert" || got.Items[0].Scope != "cluster-wide" || got.Items[0].LastTransitionTime != unixMilli("2026-10-02T00:00:00Z") {
		t.Fatalf("fixture: %+v", got)
	}
	for _, r := range f.recorded() {
		if r.path == "/api/v1/secrets" {
			q, _ := url.ParseQuery(r.query)
			if q.Get("labelSelector") != sealedSecretsKeyLabel {
				t.Fatalf("keys not filtered: %s", r.query)
			}
		}
	}
}

func TestSealedSecretsPartialErrors(t *testing.T) {
	for _, path := range []string{"/apis/bitnami.com/v1alpha1/sealedsecrets", "/api/v1/secrets", "/apis/apps/v1/deployments"} {
		t.Run(path, func(t *testing.T) {
			f := newFakeKubeAPI(t, sealedSecretsRoutes())
			f.answerWith("GET "+path, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"read refused"}`)
			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}
			got := readSealedSecrets(context.Background(), k, fixtureNow)
			if got == nil || !strings.Contains(got.Error, "refused") {
				t.Fatalf("missing refusal: %+v", got)
			}
			if slices.Contains(got.Reasons, sealedReasonNoKeys) || slices.Contains(got.Reasons, sealedReasonControllerDown) {
				t.Fatalf("false problem from inaccessible data: %+v", got)
			}
			if path != "/api/v1/secrets" && len(got.Keys) != 1 {
				t.Fatalf("lost keys: %+v", got)
			}
			if path != "/apis/apps/v1/deployments" && got.Controller == nil {
				t.Fatalf("lost controller: %+v", got)
			}
		})
	}
}

func TestSealedSecretsDiscoveryRefusal(t *testing.T) {
	f := newFakeKubeAPI(t, sealedSecretsRoutes())
	f.answerWith("GET /apis/bitnami.com/v1alpha1", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"discovery refused"}`)
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := readSealedSecrets(context.Background(), k, fixtureNow)
	if got == nil || !strings.Contains(got.Error, "discovery refused") {
		t.Fatalf("discovery refusal lost: %+v", got)
	}
	if len(f.recorded()) != 2 {
		t.Fatalf("asked for data after discovery refusal: %+v", f.recorded())
	}
}

func TestSealedSecretsPagedCapAndRestart(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			firstReads := 0
			expiredOnce := false
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" {
					fmt.Fprint(w, `{"gitVersion":"v1.34.0"}`)
					return
				}
				if r.URL.Path != "/apis/bitnami.com/v1alpha1/sealedsecrets" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Query().Get("limit") != "500" {
					t.Errorf("page limit: %s", r.URL.RawQuery)
				}
				if r.URL.Query().Get("continue") == "next" {
					if expired && !expiredOnce {
						expiredOnce = true
						w.WriteHeader(http.StatusGone)
						fmt.Fprint(w, `{"reason":"Expired","message":"expired list"}`)
						return
					}
					fmt.Fprint(w, `{"items":[{"metadata":{"name":"last","namespace":"shop"}}]}`)
					return
				}
				firstReads++
				items := make([]map[string]any, 500)
				for i := range items {
					items[i] = map[string]any{"metadata": map[string]string{"name": fmt.Sprintf("secret-%03d", i), "namespace": "shop"}}
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"continue": "next"}, "items": items}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			f := &fakeKubeAPI{Server: server}
			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}
			out := &sealedSecretsStatus{Items: []sealedSecret{}}
			if err := readSealedSecretItems(context.Background(), k, out); err != nil {
				t.Fatal(err)
			}
			if out.SecretCount != 501 || len(out.Items) != 500 || !out.Truncated {
				t.Fatalf("cap/restart: %+v", out)
			}
			expected := 1
			if expired {
				expected = 2
			}
			if firstReads != expected {
				t.Fatalf("first reads %d", firstReads)
			}
		})
	}
}

func TestSealedSecretsControllerStopped(t *testing.T) {
	routes := sealedSecretsRoutes()
	routes["GET /apis/apps/v1/deployments"] = `{"items":[{"metadata":{"name":"controller","namespace":"security"},"spec":{"replicas":0,"template":{"spec":{"containers":[{"image":"docker.io/bitnami/sealed-secrets-controller:0.31.0"}]}}},"status":{"readyReplicas":0}}]}`
	f := newFakeKubeAPI(t, routes)
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := readSealedSecrets(context.Background(), k, fixtureNow)
	if got.Controller == nil || got.Controller.Ready || got.Controller.Namespace != "security" || got.Health != healthCritical {
		t.Fatalf("stopped controller: %+v", got)
	}
}

func TestSealedSecretsSupportedIntegration(t *testing.T) {
	demo := demoSupportedIntegrations()
	found := false
	for _, s := range demo.Items {
		if s.ID == "sealed-secrets" {
			found = s.Detected && s.Via == detectedByAPI
		}
	}
	if !found {
		t.Fatal("Sealed Secrets not detected in demo integrations")
	}
	cfg := demoConfigForTest(t)
	raw, err := KubeDataServices(cfg, "", "", "sealed-secrets")
	if err != nil {
		t.Fatal(err)
	}
	var ds dataServices
	if err := json.Unmarshal([]byte(raw), &ds); err != nil {
		t.Fatal(err)
	}
	if ds.SealedSecrets == nil {
		t.Fatal("demo public API misses Sealed Secrets")
	}
}

func TestSealedSecretsKeysRequestMetadataOnly(t *testing.T) {
	routes := sealedSecretsRoutes()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/secrets" && r.Header.Get("Accept") != "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1" {
			t.Error("sealing keys requested with their payloads")
			http.Error(w, "metadata only required", http.StatusNotAcceptable)
			return
		}
		answer, ok := routes[r.Method+" "+r.URL.Path]
		if r.URL.Path == "/version" {
			answer, ok = `{"gitVersion":"v1.34.0"}`, true
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, answer)
	}))
	defer server.Close()
	f := &fakeKubeAPI{Server: server}
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := readSealedSecrets(context.Background(), k, fixtureNow)
	if got == nil || got.Error != "" || len(got.Keys) != 1 {
		t.Fatalf("metadata response: %+v", got)
	}
}
