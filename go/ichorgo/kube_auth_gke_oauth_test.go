package ichorgo

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

const sampleGoogleClientID = "123-abc.apps.googleusercontent.com"

// withFakeGoogleIssuer points Google's sign-in at a fake OpenID provider.
func withFakeGoogleIssuer(t *testing.T) *fakeIdP {
	t.Helper()

	idp := newFakeIdP(t)
	issuer, tlsConfig := gcpIssuer, gcpOAuthTLS
	gcpIssuer = func() string { return idp.URL }
	gcpOAuthTLS = func() *tls.Config {
		return &tls.Config{RootCAs: idp.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	}

	t.Cleanup(func() { gcpIssuer, gcpOAuthTLS = issuer, tlsConfig })

	return idp
}

// googleBrowserFollow plays Google's sign-in page: it checks Google's extras and redirects
// to redirect_uri with code.
func googleBrowserFollow(t *testing.T, code string) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)

			return
		}

		q := u.Query()
		scope := q.Get("scope")

		if q.Get("client_id") != sampleGoogleClientID || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" ||
			q.Get("prompt") != "consent" || !strings.Contains(scope, "cloud-platform") || !strings.Contains(scope, "email") ||
			strings.Contains(scope, "offline_access") || !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
			t.Errorf("auth request %v", q)
		}

		resp, err := http.Get(q.Get("redirect_uri") + "/?code=" + code + "&state=" + url.QueryEscape(q.Get("state"))) //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_ = resp.Body.Close()
	}
}

func TestGKEOAuthClientBrowser(t *testing.T) {
	store := withAuthStore(t)
	idp := withFakeGoogleIssuer(t)

	stored, api := gkeUserCluster(t)

	info, _ := KubeSignInInfo(stored, "admin@test")
	if !strings.Contains(info, `"kind":"credentials"`) || !strings.Contains(info, gcpFieldOAuthClientID) {
		t.Fatalf("info %s", info)
	}

	secrets, _ := json.Marshal(map[string]string{gcpFieldOAuthClientID: sampleGoogleClientID, gcpFieldOAuthClientSecret: "desktop-secret"})

	// The client is kept and the browser sign-in asked for.
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1), onURL: googleBrowserFollow(t, "good-code")}
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

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	// GKE takes the access token.
	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	info, _ = KubeSignInInfo(stored, "admin@test")
	if !strings.Contains(info, `"signedIn":true`) || !strings.Contains(info, "alice@example.org") || !strings.Contains(info, sampleGoogleClientID) {
		t.Errorf("info after sign-in %s", info)
	}

	// Expired: renewed with the refresh token, no browser.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if g := idp.grantList(); len(g) != 2 || g[0] != "authorization_code" || g[1] != "refresh_token" {
		t.Fatalf("grants %v", g)
	}

	// Backups keep the client, not the tokens.
	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, "desktop-secret") || strings.Contains(backup, "refresh-") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Revoked: sign in again, the client still prefilled.
	idp.mu.Lock()
	idp.refresh = "revoked"
	idp.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("after revocation: %v", err)
	}

	if info, _ = KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, sampleGoogleClientID) {
		t.Errorf("client not prefilled: %s", info)
	}
}

func TestGKEOAuthClientRefusals(t *testing.T) {
	withAuthStore(t)
	withFakeGoogleIssuer(t)

	stored, _ := gkeUserCluster(t)

	for _, tc := range []struct{ name, id, secret, want string }{
		{"not a Google client", "my-client", "s", "not a Google OAuth client ID"},
		{"suffix only", gcpClientIDSuffix[1:], "s", "not a Google OAuth client ID"},
		{"no secret", sampleGoogleClientID, "", "enter the OAuth client secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secrets, _ := json.Marshal(map[string]string{gcpFieldOAuthClientID: tc.id, gcpFieldOAuthClientSecret: tc.secret})
			if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}

	// A browser sign-in without a client entered is refused.
	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "admin@test", rec)

	select {
	case msg := <-rec.done:
		if !strings.Contains(msg, "OAuth client ID and secret first") {
			t.Errorf("sign-in without a client: %q", msg)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}
}
