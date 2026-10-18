package ichorgo

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

// The secrets section, Sealed Secrets side: a SealedSecret that did not unseal, and the
// sealing keys the controller cannot work with.
func TestCheckupSealedSecrets(t *testing.T) {
	const (
		empty = `{"items":[]}`
		// fixtureNow is 2026-10-03T12:00:00Z.
		notSynced   = `{"items":[{"metadata":{"name":"legacy-cert","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"UnsealFailed","message":"failed to decrypt: no key could decrypt secret"}]}}]}`
		unmanaged   = `{"items":[{"metadata":{"name":"cert-manager","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"Unmanaged"}]}}]}`
		fresh       = `{"items":[{"metadata":{"name":"just-applied","namespace":"shop","creationTimestamp":"2026-10-03T11:59:30Z"},"status":{"conditions":[{"type":"Synced","status":"False","reason":"UnsealFailed"}]}}]}`
		synced      = `{"items":[{"metadata":{"name":"api-token","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"status":{"conditions":[{"type":"Synced","status":"True"}]}}]}`
		compromised = `{"items":[{"metadata":{"name":"old-key","namespace":"kube-system","labels":{"sealedsecrets.bitnami.com/sealed-secrets-key":"compromised"}}}]}`
	)

	sealed := map[string]string{groupSealedSecrets: sealedSecretsVersion}
	both := map[string]string{groupSealedSecrets: sealedSecretsVersion, groupExternalSecrets: "v1"}

	for _, tt := range []struct {
		name   string
		groups map[string]string
		routes map[string]string
		refuse string
		want   []string
		status string
	}{
		{name: "healthy", groups: sealed, want: []string{}, status: healthOK},
		{name: "synced", groups: sealed, routes: map[string]string{"GET /apis/bitnami.com/v1alpha1/sealedsecrets": synced}, want: []string{}, status: healthOK},
		{name: "not synced", groups: sealed, routes: map[string]string{"GET /apis/bitnami.com/v1alpha1/sealedsecrets": notSynced}, want: []string{"sealedSecretNotSynced:critical:legacy-cert"}, status: healthCritical},
		{name: "not synced, no hard failure", groups: sealed, routes: map[string]string{"GET /apis/bitnami.com/v1alpha1/sealedsecrets": unmanaged}, want: []string{"sealedSecretNotSynced:warning:cert-manager"}, status: healthWarning},
		{name: "fresh", groups: sealed, routes: map[string]string{"GET /apis/bitnami.com/v1alpha1/sealedsecrets": fresh}, want: []string{}, status: healthOK},
		{name: "no active key", groups: sealed, routes: map[string]string{"GET /api/v1/secrets": compromised}, want: []string{"sealingKeyMissing:critical:sealed-secrets-controller", "sealingKeyCompromised:warning:old-key"}, status: healthCritical},
		{name: "no keys", groups: sealed, routes: map[string]string{"GET /api/v1/secrets": empty}, want: []string{"sealingKeyMissing:critical:sealed-secrets-controller"}, status: healthCritical},
		{name: "keys refused", groups: sealed, refuse: "GET /api/v1/secrets", want: []string{}, status: checkUnknown},
		{name: "operator absent", groups: map[string]string{}, want: []string{}, status: checkAbsent},
		{
			name:   "external secrets too",
			groups: both,
			routes: map[string]string{
				"GET /apis/external-secrets.io/v1/externalsecrets":     `{"items":[{"metadata":{"name":"db","namespace":"shop"},"spec":{"secretStoreRef":{"name":"vault"}},"status":{"conditions":[{"type":"Ready","status":"False","reason":"SecretSyncedError","message":"denied"}]}}]}`,
				"GET /apis/external-secrets.io/v1/secretstores":        empty,
				"GET /apis/external-secrets.io/v1/clustersecretstores": empty,
				"GET /apis/bitnami.com/v1alpha1/sealedsecrets":         notSynced,
			},
			want:   []string{"sealedSecretNotSynced:critical:legacy-cert", "externalSecretFailed:warning:db"},
			status: healthCritical,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			routes := sealedSecretsRoutes()
			for path, body := range tt.routes {
				routes[path] = body
			}

			f := newFakeKubeAPI(t, routes)
			if tt.refuse != "" {
				f.answerWith(tt.refuse, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"read refused"}`)
			}

			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}

			got := checkupSecrets(context.Background(), k, checkupInput{now: fixtureNow, groups: tt.groups})
			if got.Status != tt.status {
				t.Fatalf("status %q (error %q), want %q", got.Status, got.Error, tt.status)
			}

			if kinds := findingKinds(got); !reflect.DeepEqual(kinds, tt.want) {
				t.Fatalf("findings %v, want %v (error %q)", kinds, tt.want, got.Error)
			}
		})
	}
}

// The secrets section names a SealedSecret that failed to unseal and a compromised sealing key.
func TestCheckupSealedSecretsDemo(t *testing.T) {
	secrets := sectionByID(t, demoCheckup(checkupNow), checkSecrets)

	want := []string{
		"sealedSecretNotSynced:critical:legacy-cert",
		"sopsDecryptionFailed:critical:apps",
		"externalSecretFailed:warning:database-credentials",
		"sealingKeyCompromised:warning:sealed-secrets-key-rotated",
	}
	if got := findingKinds(secrets); !reflect.DeepEqual(got, want) {
		t.Fatalf("secrets %v, want %v", got, want)
	}
}
