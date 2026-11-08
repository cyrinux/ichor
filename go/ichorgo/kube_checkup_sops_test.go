package ichorgo

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The secrets section, Flux side: a Kustomization whose SOPS decryption fails, not one that
// fails its build for another reason.
func TestCheckupSopsDecryption(t *testing.T) {
	const path = "GET /apis/kustomize.toolkit.fluxcd.io/v1/kustomizations"

	ks := func(name, spec, reason, status, message string) string {
		return `{"metadata":{"name":"` + name + `","namespace":"flux-system"},"spec":` + spec +
			`,"status":{"conditions":[{"type":"Ready","status":"` + status + `","reason":"` + reason + `","message":"` + message + `"}]}}`
	}

	const (
		withKey   = `{"decryption":{"provider":"sops","secretRef":{"name":"sops-age"}}}`
		withKMS   = `{"decryption":{"provider":"sops"}}`
		noDecrypt = `{}`
		suspended = `{"suspend":true,"decryption":{"provider":"sops","secretRef":{"name":"sops-age"}}}`
		flagged   = "sopsDecryptionFailed:critical:apps"
	)

	long := strings.Repeat("x", 2*sopsMessageLimit)
	flux := map[string]string{groupFluxKustomize: "v1"}

	for _, tt := range []struct {
		name    string
		groups  map[string]string
		items   []string
		want    []string
		extra   string
		reason  string
		checked int
		status  string
	}{
		{
			name: "wrong key", groups: flux,
			items: []string{ks("apps", withKey, "BuildFailed", "False", "error decrypting sources: failed to decrypt: no key could decrypt the data")},
			want:  []string{flagged}, extra: "sops-age", reason: "BuildFailed", checked: 1, status: healthCritical,
		},
		{
			name: "resource decryption, long message", groups: flux,
			items: []string{ks("apps", withKey, "BuildFailed", "False", "decryption failed for 'Secret/db': "+long)},
			want:  []string{flagged}, extra: "sops-age", reason: "BuildFailed", checked: 1, status: healthCritical,
		},
		{
			name: "key Secret missing", groups: flux,
			items: []string{ks("apps", withKey, "BuildFailed", "False", `cannot get sops decryption Secret 'sops-age': secrets \"sops-age\" not found`)},
			want:  []string{flagged}, extra: "sops-age", reason: "BuildFailed", checked: 1, status: healthCritical,
		},
		{
			name: "cloud KMS, no secretRef", groups: flux,
			items: []string{ks("apps", withKMS, "BuildFailed", "False", "Error decrypting sources: failed to get the data key: AccessDenied")},
			want:  []string{flagged}, extra: "", reason: "BuildFailed", checked: 1, status: healthCritical,
		},
		{
			name: "kustomize error", groups: flux,
			items: []string{ks("apps", withKey, "BuildFailed", "False", "kustomize build failed: accumulating resources: missing file")},
			want:  []string{}, checked: 1, status: healthOK,
		},
		{
			name: "a path that says decrypt", groups: flux,
			items: []string{ks("apps", noDecrypt, "BuildFailed", "False", "kustomize build failed: accumulating resources: './decrypt-proxy': no such file or directory")},
			want:  []string{}, checked: 0, status: healthOK,
		},
		{
			name: "decryption not configured", groups: flux,
			items: []string{ks("apps", noDecrypt, "ReconciliationFailed", "False", "Secret/apps/db is SOPS encrypted, configuring decryption is required for this secret to be reconciled")},
			want:  []string{flagged}, extra: "", reason: "ReconciliationFailed", checked: 1, status: healthCritical,
		},
		{
			name: "suspended", groups: flux,
			items: []string{ks("apps", suspended, "BuildFailed", "False", "error decrypting sources: no key")},
			want:  []string{}, checked: 1, status: healthOK,
		},
		{
			name: "ready", groups: flux,
			items: []string{ks("apps", withKey, "ReconciliationSucceeded", "True", "Applied revision: main@sha1:abc")},
			want:  []string{}, checked: 1, status: healthOK,
		},
		{name: "flux absent", groups: map[string]string{}, want: []string{}, status: checkAbsent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeKubeAPI(t, map[string]string{path: `{"items":[` + strings.Join(tt.items, ",") + `]}`})

			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}

			in := checkupInput{now: fixtureNow, groups: tt.groups}

			part := checkupSopsDecryption(context.Background(), k, in)
			if part.served != (len(tt.groups) > 0) || part.checked != tt.checked {
				t.Fatalf("served %v, checked %d, want %d", part.served, part.checked, tt.checked)
			}

			got := checkupSecrets(context.Background(), k, in)
			if got.Status != tt.status {
				t.Fatalf("status %q (error %q), want %q", got.Status, got.Error, tt.status)
			}

			if kinds := findingKinds(got); !reflect.DeepEqual(kinds, tt.want) {
				t.Fatalf("findings %v, want %v", kinds, tt.want)
			}

			if len(got.Findings) == 1 {
				f := got.Findings[0]
				if f.Extra != tt.extra || f.Reason != tt.reason || f.Namespace != "flux-system" || len(f.Message) > sopsMessageLimit+len("…") {
					t.Fatalf("finding %+v", f)
				}
			}
		})
	}
}
