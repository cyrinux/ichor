package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeGoogle is a token endpoint for gcloud user credentials (and a userinfo one): it answers
// the refresh token it expects with an access token, and records each grant's form.
type fakeGoogle struct {
	mu sync.Mutex
	// refresh is the refresh token accepted; rotate, when set, is sent back as the new one.
	refresh, rotate string
	// email goes in an ID token; "" sends none, so userinfo is asked.
	email  string
	grants []map[string]string
}

func (g *fakeGoogle) handle(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if r.URL.Path == "/userinfo" {
		if r.Header.Get("Authorization") != "Bearer ya29.user" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		_, _ = io.WriteString(w, `{"email":"dev@example.com"}`)

		return
	}

	_ = r.ParseForm()
	form := map[string]string{}

	for k := range r.Form {
		form[k] = r.Form.Get(k)
	}

	g.grants = append(g.grants, form)

	if form["grant_type"] != "refresh_token" || form["refresh_token"] != g.refresh || form["client_id"] != "gcloud-client.apps.example.com" || form["client_secret"] != "gcloud-secret" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)

		return
	}

	answer := map[string]any{"access_token": "ya29.user", "expires_in": 3599}
	if g.email != "" {
		claims, _ := json.Marshal(map[string]string{"email": g.email})
		answer["id_token"] = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	}

	if g.rotate != "" {
		answer["refresh_token"] = g.rotate
		g.refresh = g.rotate
	}

	_ = json.NewEncoder(w).Encode(answer)
}

func (g *fakeGoogle) grantList() []map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]map[string]string(nil), g.grants...)
}

func withFakeGoogle(t *testing.T, g *fakeGoogle) {
	t.Helper()

	srv := withCloudServer(t, g.handle)

	token, info := gcpTokenTarget, gcpUserInfoTarget
	gcpTokenTarget = func(string) string { return srv.URL + "/token" }
	gcpUserInfoTarget = func() string { return srv.URL + "/userinfo" }

	t.Cleanup(func() { gcpTokenTarget, gcpUserInfoTarget = token, info })
}

func adcJSON(credentialType, refresh string) string {
	raw, _ := json.Marshal(map[string]string{
		"type": credentialType, "client_id": "gcloud-client.apps.example.com", "client_secret": "gcloud-secret",
		"refresh_token": refresh, "quota_project_id": "sample-proj",
	})

	return string(raw)
}

func userSecrets(field, raw string) string {
	s, _ := json.Marshal(map[string]string{field: raw})

	return string(s)
}

// forgetTokens drops the tokens held in memory and makes the stored ones look expired: the
// next call mints again.
func forgetTokens(t *testing.T, store *memAuthStore, stored, contextName string) {
	t.Helper()

	kubeAuth.mu.Lock()
	for _, s := range kubeAuth.sources {
		s.invalidate()
	}
	kubeAuth.mu.Unlock()

	expireStoredBearer(t, store)
	kubeClients.forgetConfig(stored, contextName)
}

func gkeUserCluster(t *testing.T) (string, *fakeKubeAPI) {
	t.Helper()

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", execKubeconfig(api, "gke-gcloud-auth-plugin"), "")
	if err != nil {
		t.Fatal(err)
	}

	return stored, api
}

func TestGKEUserCredentials(t *testing.T) {
	store := withAuthStore(t)
	google := &fakeGoogle{refresh: "rt-1", rotate: "rt-2", email: "dev@example.com"}
	withFakeGoogle(t, google)

	stored, api := gkeUserCluster(t)

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, gcpFieldUserCredentials) || !strings.Contains(info, gcpFieldServiceAccount) {
		t.Fatalf("both options are not offered: %s", info)
	}

	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldUserCredentials, adcJSON("authorized_user", "rt-1"))); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	r := api.recorded()
	if r[len(r)-1].auth != "Bearer ya29.user" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// One grant for the sign-in check: the token is reused while it lasts.
	if grants := google.grantList(); len(grants) != 1 || grants[0]["refresh_token"] != "rt-1" {
		t.Fatalf("grants %v", grants)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "dev@example.com") {
		t.Errorf("the account is not shown: %s", info)
	}

	// The refresh token Google rotated is the one stored, sent next and kept by backups.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if grants := google.grantList(); len(grants) != 2 || grants[1]["refresh_token"] != "rt-2" {
		t.Fatalf("after rotation, grants %v", grants)
	}

	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, "rt-2") || strings.Contains(backup, "ya29.user") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Revoked, or expired by the session control: sign in again, with how.
	google.mu.Lock()
	google.refresh = "something-else"
	google.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "application-default login") {
		t.Fatalf("after revocation: %v", err)
	}
}

func TestGKEUserCredentialsAccountFromUserInfo(t *testing.T) {
	withAuthStore(t)
	withFakeGoogle(t, &fakeGoogle{refresh: "rt-1"})

	stored, _ := gkeUserCluster(t)

	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldUserCredentials, adcJSON("authorized_user", "rt-1"))); err != nil {
		t.Fatal(err)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "dev@example.com") {
		t.Errorf("the account is not shown: %s", info)
	}
}

func TestGKEUserCredentialsRefusals(t *testing.T) {
	withAuthStore(t)
	withFakeGoogle(t, &fakeGoogle{refresh: "rt-1"})

	stored, _ := gkeUserCluster(t)

	for _, tc := range []struct {
		name, field, raw, want string
	}{
		{"workforce login config", gcpFieldUserCredentials, adcJSON("external_account", "rt-1"), "workforce identity"},
		{"service account key as user", gcpFieldUserCredentials, `{"type":"service_account","client_email":"a@b"}`, "choose Service account key"},
		{"user credentials as service account", gcpFieldServiceAccount, adcJSON("authorized_user", "rt-1"), "choose gcloud user credentials"},
		{"not JSON", gcpFieldUserCredentials, "refresh-token", "not JSON"},
		{"no refresh token", gcpFieldUserCredentials, adcJSON("authorized_user", ""), "not a gcloud user credential"},
		{"revoked at sign-in", gcpFieldUserCredentials, adcJSON("authorized_user", "rt-old"), "application-default login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := KubeSetCredentials(stored, "admin@test", userSecrets(tc.field, tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}
