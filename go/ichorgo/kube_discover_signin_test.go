package ichorgo

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// oauthDiscoverySecrets is the organisation's OAuth client as the discovery sheet sends it.
func oauthDiscoverySecrets() map[string]string {
	return map[string]string{gcpFieldOAuthClientID: sampleGoogleClientID, gcpFieldOAuthClientSecret: "desktop-secret", gcpFieldProjects: ""}
}

// withDiscoveryCloud serves Google's sign-in (a fake OpenID provider, access token "at") and
// the projects and clusters that token reads.
func withDiscoveryCloud(t *testing.T) (*fakeGoogleCloud, *fakeIdP) {
	t.Helper()

	idp := withFakeGoogleIssuer(t)
	f := newFakeGoogleCloud()
	f.bearer = "at"
	withFakeGoogleCloud(t, f)
	t.Cleanup(ForgetDiscoverSignIn)

	return f, idp
}

// discoverSignIn runs StartDiscoverSignIn with secrets; follow plays the browser (nil: no
// prompt expected) and the error message it ends with is returned.
func discoverSignIn(t *testing.T, secrets map[string]string, follow func(signInPrompt, *SignInRun)) string {
	t.Helper()

	raw, _ := json.Marshal(secrets)
	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	run := StartDiscoverSignIn(discoverGKE, string(raw), rec)

	select {
	case p := <-rec.prompts:
		if follow == nil {
			t.Fatalf("unexpected prompt %+v", p)
		}

		follow(p, run)
	case msg := <-rec.done:
		if follow != nil {
			t.Fatalf("no prompt, done with %q", msg)
		}

		return msg
	case <-time.After(15 * time.Second):
		t.Fatal("no prompt")
	}

	select {
	case msg := <-rec.done:
		return msg
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	return ""
}

// followGoogle plays Google's page for a Desktop client: back to the loopback redirect.
func followGoogle(t *testing.T) func(signInPrompt, *SignInRun) {
	return func(p signInPrompt, _ *SignInRun) { googleBrowserFollow(t, "good-code")(p.URL) }
}

func discoverErr(secrets map[string]string) error {
	raw, _ := json.Marshal(secrets)
	_, err := DiscoverClusters(discoverGKE, string(raw))

	return err
}

func contextNames(s kubeconfigSummary) string {
	names := make([]string, 0, len(s.Contexts))
	for _, c := range s.Contexts {
		names = append(names, c.Name)
	}

	return strings.Join(names, ",")
}

// The OAuth client signs in in the browser once, then the account's projects are read.
func TestDiscoverGKEWithOAuthClient(t *testing.T) {
	f, _ := withDiscoveryCloud(t)

	if msg := discoverSignIn(t, oauthDiscoverySecrets(), followGoogle(t)); msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	s := discoverWith(t, discoverGKE, oauthDiscoverySecrets())
	if got := contextNames(s); got != "sample-proj-a-gke.europe-west1.gke,sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %s", got)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Both pages of projects read; no quota project, which only gcloud's credentials carry.
	if f.crmCalls != 2 {
		t.Errorf("projects listed in %d calls", f.crmCalls)
	}

	for _, h := range f.headers {
		if h != "" {
			t.Errorf("quota project header %q", h)
		}
	}
}

// Without the browser sign-in first, discovery says to sign in.
func TestDiscoverGKENeedsSignInFirst(t *testing.T) {
	withDiscoveryCloud(t)

	err := discoverErr(oauthDiscoverySecrets())
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "before finding clusters") {
		t.Fatalf("without a sign-in: %v", err)
	}

	// A sign-in for another client is not used.
	if msg := discoverSignIn(t, oauthDiscoverySecrets(), followGoogle(t)); msg != "" {
		t.Fatal(msg)
	}

	other := oauthDiscoverySecrets()
	other[gcpFieldOAuthClientSecret] = "other-secret"

	if err := discoverErr(other); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Errorf("another client: %v", err)
	}
}

func TestForgetDiscoverSignIn(t *testing.T) {
	withDiscoveryCloud(t)

	if msg := discoverSignIn(t, oauthDiscoverySecrets(), followGoogle(t)); msg != "" {
		t.Fatal(msg)
	}

	ForgetDiscoverSignIn()

	if err := discoverErr(oauthDiscoverySecrets()); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("after forget: %v", err)
	}
}

// The clusters imported after the discovery sign in with its session: no second browser round.
func TestImportedClustersReuseTheDiscoverySignIn(t *testing.T) {
	store := withAuthStore(t)
	_, idp := withDiscoveryCloud(t)

	if msg := discoverSignIn(t, oauthDiscoverySecrets(), followGoogle(t)); msg != "" {
		t.Fatal(msg)
	}

	discoverWith(t, discoverGKE, oauthDiscoverySecrets())

	stored, api := gkeUserCluster(t)

	secrets, _ := json.Marshal(oauthDiscoverySecrets())
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err != nil {
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

	// Stored under the context, with the refresh token: it renews like any OAuth sign-in.
	if len(store.states) != 1 {
		t.Fatalf("stored states %d", len(store.states))
	}

	for _, raw := range store.states {
		var state kubeAuthState
		if err := json.Unmarshal([]byte(raw), &state); err != nil || state.Method != authGKE || state.session("refresh") == "" ||
			state.secret(gcpFieldOAuthClientID) != sampleGoogleClientID {
			t.Errorf("stored %s (%v)", raw, err)
		}
	}
}

// iOS: the build's Google client, back to the reversed-client-ID scheme the app hands over.
func TestDiscoverGKEWithGoogleSignInIOS(t *testing.T) {
	withDiscoveryCloud(t)
	withGoogleSignInClient(t, googleSignInIOS, sampleGoogleClientID)

	secrets := map[string]string{gcpFieldGoogleSignIn: googleSignInIOS, gcpFieldProjects: "sample-proj-c"}

	const redirect = "com.googleusercontent.apps.123-abc:/oauth2redirect"

	msg := discoverSignIn(t, secrets, func(p signInPrompt, run *SignInRun) {
		u, err := url.Parse(p.URL)
		if err != nil {
			t.Fatal(err)
		}

		q := u.Query()
		if p.Kind != "browser" || q.Get("redirect_uri") != redirect || q.Get("client_id") != sampleGoogleClientID {
			t.Fatalf("prompt %+v, query %v", p, q)
		}

		run.Complete(redirect + "?code=good-code&state=" + url.QueryEscape(q.Get("state")))
	})
	if msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	if got := contextNames(discoverWith(t, discoverGKE, secrets)); got != "sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %s", got)
	}
}

// Android: the app holds the token, so no sign-in run is needed.
func TestDiscoverGKEWithGoogleSignInAndroid(t *testing.T) {
	f := newFakeGoogleCloud()
	withFakeGoogleCloud(t, f)
	withGoogleSignInClient(t, googleSignInAndroid, "")

	secrets := map[string]string{
		gcpFieldGoogleSignIn: googleSignInAndroid, gcpFieldAccessToken: "ya29.user",
		gcpFieldAccessTokenExpiry: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), gcpFieldAccount: "dev@example.com",
	}

	if msg := discoverSignIn(t, secrets, nil); msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	if got := contextNames(discoverWith(t, discoverGKE, secrets)); got != "sample-proj-a-gke.europe-west1.gke,sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %s", got)
	}

	// Expired: the app renews it, discovery says so.
	secrets[gcpFieldAccessTokenExpiry] = strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	if err := discoverErr(secrets); err == nil || !strings.Contains(err.Error(), gcpNativeRenewReason) {
		t.Errorf("expired token: %v", err)
	}
}
