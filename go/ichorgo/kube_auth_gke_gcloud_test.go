package ichorgo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// withGcloudClient replaces gcloud's client by the one fakeGoogle takes.
func withGcloudClient(t *testing.T) {
	t.Helper()

	client := gcloudClient
	gcloudClient = func() (string, string) { return "gcloud-client.apps.example.com", "gcloud-secret" }

	t.Cleanup(func() { gcloudClient = client })
}

// gcloudBrowserFollow plays Google's page for gcloud's client: it checks the request and goes
// back to the loopback redirect with a good code.
func gcloudBrowserFollow(t *testing.T) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)

			return
		}

		q := u.Query()
		if q.Get("client_id") != "gcloud-client.apps.example.com" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" ||
			q.Get("scope") != "openid email "+gcpScope || q.Get("code_challenge_method") != "S256" ||
			!strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
			t.Errorf("auth request %v", q)
		}

		resp, err := http.Get(q.Get("redirect_uri") + "/?code=good-code&state=" + url.QueryEscape(q.Get("state"))) //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_ = resp.Body.Close()
	}
}

// gcloudSignIn runs StartKubeSignIn for the stored context and waits for its end.
func gcloudSignIn(t *testing.T, stored string) {
	t.Helper()

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1), onURL: gcloudBrowserFollow(t)}
	StartKubeSignIn(stored, "admin@test", rec)

	select {
	case msg := <-rec.done:
		if msg != "" {
			t.Fatalf("sign-in: %s", msg)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	if p := <-rec.prompts; p.Kind != "browser" {
		t.Errorf("prompt %+v", p)
	}
}

// storedGcloudState is the one state the store holds and its user credentials.
func storedGcloudState(t *testing.T, store *memAuthStore) (kubeAuthState, gcpUserCredentials) {
	t.Helper()

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.states) != 1 {
		t.Fatalf("stored states %d", len(store.states))
	}

	var state kubeAuthState

	for _, raw := range store.states {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			t.Fatal(err)
		}
	}

	creds, err := parseUserCredentials(state.secret(gcpFieldUserCredentials))
	if err != nil {
		t.Fatalf("stored credentials: %v", err)
	}

	return state, creds
}

func gcloudMarker() string {
	raw, _ := json.Marshal(map[string]string{gcpFieldGcloudSignIn: "1"})

	return string(raw)
}

func TestGKEGcloudSignIn(t *testing.T) {
	store := withAuthStore(t)
	withFakeGoogleIssuer(t)
	withGcloudClient(t)
	withGoogleSignInClient(t, "", "")

	google := &fakeGoogle{refresh: "refresh-1", rotate: "rt-2"}
	withFakeGoogle(t, google)

	stored, api := gkeUserCluster(t)

	// Offered on every build, with no Google client of the build.
	info, _ := KubeSignInInfo(stored, "admin@test")
	if !strings.Contains(info, `["`+gcpFieldGcloudSignIn+`"]`) {
		t.Fatalf("info %s", info)
	}

	// The marker alone asks for the browser.
	if err := KubeSetCredentials(stored, "admin@test", gcloudMarker()); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	gcloudSignIn(t, stored)

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// Stored as gcloud user credentials: gcloud's client and the refresh token.
	state, creds := storedGcloudState(t, store)
	if creds.Type != gcpTypeAuthorizedUser || creds.ClientID != "gcloud-client.apps.example.com" || creds.ClientSecret != "gcloud-secret" ||
		creds.RefreshToken != "refresh-1" || state.session("refresh") != "" || state.secret(gcpFieldGcloudSignIn) != "1" {
		t.Fatalf("stored %+v %+v", state, creds)
	}

	if info, _ = KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, `"signedIn":true`) || !strings.Contains(info, "alice@example.org") {
		t.Errorf("info after sign-in %s", info)
	}

	// Expired: the refresh grant renews it, no browser; the rotated refresh token is kept.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.user" {
		t.Fatalf("bearer after refresh %q", r[len(r)-1].auth)
	}

	if _, creds = storedGcloudState(t, store); creds.RefreshToken != "rt-2" || len(google.grantList()) != 1 {
		t.Fatalf("after refresh %+v, grants %v", creds, google.grantList())
	}

	// Backups keep the session, as an imported credentials file.
	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, gcpFieldGcloudSignIn) || !strings.Contains(backup, "rt-2") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Revoked: sign in again in the browser, no gcloud command.
	google.mu.Lock()
	google.refresh = "revoked"
	google.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	_, err := KubeNodes(stored, "admin@test", "")
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), gcpGcloudRenewHint) || strings.Contains(err.Error(), "gcloud auth") {
		t.Fatalf("after revocation: %v", err)
	}

	if info, _ = KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, `"values":{"`+gcpFieldGcloudSignIn+`":"1"}`) {
		t.Errorf("info after revocation %s", info)
	}

	if err := KubeSetCredentials(stored, "admin@test", gcloudMarker()); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("sign in again: %v", err)
	}

	gcloudSignIn(t, stored)

	if _, creds = storedGcloudState(t, store); creds.RefreshToken != "refresh-2" {
		t.Errorf("after a new sign-in %+v", creds)
	}
}

func TestGKEGcloudSignInState(t *testing.T) {
	adc := adcJSON(gcpTypeAuthorizedUser, "rt")

	for _, tc := range []struct {
		name    string
		secrets map[string]string
		keeps   bool
	}{
		{"marker only", map[string]string{gcpFieldGcloudSignIn: "1"}, false},
		{"with a session", map[string]string{gcpFieldGcloudSignIn: "1", gcpFieldUserCredentials: adc}, true},
		{"broken session", map[string]string{gcpFieldGcloudSignIn: "1", gcpFieldUserCredentials: "{"}, false},
		{"workforce session", map[string]string{gcpFieldGcloudSignIn: "1", gcpFieldUserCredentials: adcJSON(gcpTypeWorkforceUser, "rt")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := gkeMethod{}.fromSecrets(tc.secrets)
			if err != nil || state.secret(gcpFieldGcloudSignIn) != "1" || (state.secret(gcpFieldUserCredentials) != "") != tc.keeps {
				t.Fatalf("state %+v (%v)", state, err)
			}
		})
	}

	if !gkeNeedsBrowser(map[string]string{gcpFieldGcloudSignIn: "1"}) {
		t.Error("discovery does not sign in first")
	}
}

// Discovery signs in with gcloud's client first; the cluster imported then takes that session.
func TestDiscoverGKEWithGcloudSignIn(t *testing.T) {
	store := withAuthStore(t)
	_, idp := withDiscoveryCloud(t)
	withGcloudClient(t)

	secrets := map[string]string{gcpFieldGcloudSignIn: "1", gcpFieldProjects: ""}

	if err := discoverErr(secrets); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("without a sign-in: %v", err)
	}

	follow := func(p signInPrompt, _ *SignInRun) { gcloudBrowserFollow(t)(p.URL) }
	if msg := discoverSignIn(t, secrets, follow); msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	s := discoverWith(t, discoverGKE, secrets)
	if got := contextNames(s); got != "sample-proj-a-gke.europe-west1.gke,sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %s", got)
	}

	stored, api := gkeUserCluster(t)
	if err := KubeSetCredentials(stored, "admin@test", gcloudMarker()); err != nil {
		t.Fatalf("set credentials: %v", err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	if g := idp.grantList(); len(g) != 1 || g[0] != "authorization_code" {
		t.Errorf("grants %v", g)
	}

	if _, creds := storedGcloudState(t, store); creds.RefreshToken != "refresh-1" {
		t.Errorf("stored %+v", creds)
	}
}
