package ichorgo

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The secrets section leaves Sealed Secrets to its plugin (the plugins section): with both
// operators, only External Secrets reports here.
func TestCheckupSecretsLeavesSealedSecretsToItsPlugin(t *testing.T) {
	routes := sealedSecretsRoutes()
	routes["GET /apis/bitnami.com/v1alpha1/sealedsecrets"] = `{"items":[{"metadata":{"name":"legacy-cert","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},` +
		`"status":{"conditions":[{"type":"Synced","status":"False","reason":"UnsealFailed"}]}}]}`
	routes["GET /apis/external-secrets.io/v1/externalsecrets"] = `{"items":[{"metadata":{"name":"db","namespace":"shop"},"spec":{"secretStoreRef":{"name":"vault"}},` +
		`"status":{"conditions":[{"type":"Ready","status":"False","reason":"SecretSyncedError","message":"denied"}]}}]}`
	routes["GET /apis/external-secrets.io/v1/secretstores"] = `{"items":[]}`
	routes["GET /apis/external-secrets.io/v1/clustersecretstores"] = `{"items":[]}`

	f := newFakeKubeAPI(t, routes)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	groups := map[string]string{"bitnami.com": "v1alpha1", groupExternalSecrets: "v1"}

	got := checkupSecrets(context.Background(), k, checkupInput{now: fixtureNow, groups: groups})
	if kinds := findingKinds(got); !reflect.DeepEqual(kinds, []string{"externalSecretFailed:warning:db"}) || got.Status != healthWarning {
		t.Fatalf("findings %v, status %q (error %q)", kinds, got.Status, got.Error)
	}

	for _, r := range f.recorded() {
		if strings.HasPrefix(r.path, "/apis/bitnami.com") {
			t.Errorf("the secrets section read %s", r.path)
		}
	}
}

// The demo's secrets section: a SOPS decryption that failed and an ExternalSecret that does not
// sync (Sealed Secrets reports through its plugin).
func TestCheckupSealedSecretsDemo(t *testing.T) {
	secrets := sectionByID(t, demoCheckup(checkupNow, ""), checkSecrets)

	want := []string{
		"sopsDecryptionFailed:critical:apps",
		"externalSecretFailed:warning:database-credentials",
	}
	if got := findingKinds(secrets); !reflect.DeepEqual(got, want) {
		t.Fatalf("secrets %v, want %v", got, want)
	}
}
