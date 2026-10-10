package ichorgo

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// withGoogleSignInClient registers a build's Google client for the test.
func withGoogleSignInClient(t *testing.T, platform, clientID string) {
	t.Helper()

	SetGoogleSignInClient(platform, clientID)
	t.Cleanup(func() { SetGoogleSignInClient("", "") })
}

func signInOptions(t *testing.T, stored string) [][]string {
	t.Helper()

	raw, err := KubeSignInInfo(stored, "admin@test")
	if err != nil {
		t.Fatal(err)
	}

	var info kubeSignInKind
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}

	return info.Options
}

func hasOption(options [][]string, field string) bool {
	for _, set := range options {
		if len(set) == 1 && set[0] == field {
			return true
		}
	}

	return false
}

func TestGKENativeOptionOnlyWithAClient(t *testing.T) {
	withAuthStore(t)

	stored, _ := gkeUserCluster(t)

	if options := signInOptions(t, stored); len(options) != 3 || hasOption(options, gcpFieldGoogleSignIn) {
		t.Fatalf("without a client: %v", options)
	}

	withGoogleSignInClient(t, googleSignInAndroid, "")

	if options := signInOptions(t, stored); len(options) != 4 || !hasOption(options, gcpFieldGoogleSignIn) {
		t.Fatalf("with a client: %v", options)
	}

	if got := reversedClientScheme(sampleGoogleClientID); got != "com.googleusercontent.apps.123-abc" {
		t.Errorf("reversed scheme %q", got)
	}
}

// iOS: the browser comes back to the reversed-client-ID scheme, which the app hands over.
func TestGKENativeIOS(t *testing.T) {
	store := withAuthStore(t)
	idp := withFakeGoogleIssuer(t)
	withGoogleSignInClient(t, googleSignInIOS, sampleGoogleClientID)

	stored, api := gkeUserCluster(t)

	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldGoogleSignIn, googleSignInIOS)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	run := StartKubeSignIn(stored, "admin@test", rec)

	var p signInPrompt

	select {
	case p = <-rec.prompts:
	case <-time.After(15 * time.Second):
		t.Fatal("no prompt")
	}

	const redirect = "com.googleusercontent.apps.123-abc:/oauth2redirect"

	u, err := url.Parse(p.URL)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	if p.Kind != "browser" || p.RedirectPrefix != redirect || q.Get("redirect_uri") != redirect || q.Get("client_id") != sampleGoogleClientID ||
		q.Get("access_type") != "offline" || strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("prompt %+v, query %v", p, q)
	}

	run.Complete(redirect + "?code=good-code&state=" + url.QueryEscape(q.Get("state")))

	select {
	case msg := <-rec.done:
		if msg != "" {
			t.Fatalf("sign-in: %s", msg)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// Expired: the refresh token, no browser.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if g := idp.grantList(); len(g) != 2 || g[1] != "refresh_token" {
		t.Errorf("grants %v", g)
	}

	// A build without the client (a restored backup elsewhere): sign in again, another way.
	SetGoogleSignInClient("", "")
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.Contains(err.Error(), "not available in this build") {
		t.Errorf("without the client: %v", err)
	}
}

// Android: the app holds the token; Go asks for a new one once it expires.
func TestGKENativeAndroid(t *testing.T) {
	store := withAuthStore(t)
	withGoogleSignInClient(t, googleSignInAndroid, "")

	// Nothing may call Google.
	withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected call to %s", r.URL)
		w.WriteHeader(http.StatusTeapot)
	})

	stored, api := gkeUserCluster(t)

	secrets, _ := json.Marshal(map[string]string{
		gcpFieldGoogleSignIn: googleSignInAndroid, gcpFieldAccessToken: "ya29.app-held",
		gcpFieldAccessTokenExpiry: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), gcpFieldAccount: "dev@example.com",
	})
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.app-held" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "dev@example.com") {
		t.Errorf("the account is not shown: %s", info)
	}

	// Backups keep the marker, never the app-held token.
	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, gcpFieldGoogleSignIn) || strings.Contains(backup, "ya29.app-held") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), gcpNativeRenewReason) {
		t.Fatalf("after expiry: %v", err)
	}

	// The browser is never the way for Android's sign-in.
	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "admin@test", rec)

	if msg := <-rec.done; !strings.Contains(msg, "renews this sign-in itself") {
		t.Errorf("browser sign-in: %q", msg)
	}
}

func TestGKENativeRefusals(t *testing.T) {
	withAuthStore(t)
	withGoogleSignInClient(t, googleSignInAndroid, "")

	stored, _ := gkeUserCluster(t)

	for _, tc := range []struct {
		name    string
		secrets map[string]string
		want    string
	}{
		{"unknown platform", map[string]string{gcpFieldGoogleSignIn: "web"}, "unknown Google sign-in platform"},
		{"no token", map[string]string{gcpFieldGoogleSignIn: googleSignInAndroid}, "no access token"},
		{"bad expiry", map[string]string{gcpFieldGoogleSignIn: googleSignInAndroid, gcpFieldAccessToken: "t", gcpFieldAccessTokenExpiry: "soon"}, "no access token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.secrets)
			if err := KubeSetCredentials(stored, "admin@test", string(raw)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}
