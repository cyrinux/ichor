package ichorgo

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

const sampleRedirectPage = "https://pages.example.com/ichor/auth/google/"

// webClientSignIn starts the browser sign-in of a Web client and returns its run and the
// state the authorization request carries, after checking the request.
func webClientSignIn(t *testing.T, stored string) (*SignInRun, recSignIn, string) {
	t.Helper()

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	run := StartKubeSignIn(stored, "admin@test", rec)

	var p signInPrompt

	select {
	case p = <-rec.prompts:
	case <-time.After(15 * time.Second):
		t.Fatal("no prompt")
	}

	u, err := url.Parse(p.URL)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	if p.RedirectPrefix != sampleRedirectPage || q.Get("redirect_uri") != sampleRedirectPage || q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" {
		t.Fatalf("prompt %+v, query %v", p, q)
	}

	return run, rec, q.Get("state")
}

func waitDone(t *testing.T, rec recSignIn) string {
	t.Helper()

	select {
	case msg := <-rec.done:
		return msg
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	return ""
}

func TestGKEOAuthWebClient(t *testing.T) {
	withAuthStore(t)
	withFakeGoogleIssuer(t)

	stored, api := gkeUserCluster(t)

	secrets, _ := json.Marshal(map[string]string{
		gcpFieldOAuthClientID: sampleGoogleClientID, gcpFieldOAuthClientSecret: "web-secret", gcpFieldOAuthRedirectURL: sampleRedirectPage,
	})
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	// The page forwards to ichor://signin, which the app hands over.
	run, rec, state := webClientSignIn(t, stored)
	run.Complete("ichor://signin?code=good-code&state=" + url.QueryEscape(state))

	if msg := waitDone(t, rec); msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// The redirect URL is remembered with the client, so signing in again is one tap.
	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, sampleRedirectPage) {
		t.Errorf("redirect not remembered: %s", info)
	}

	// The browser did not come back to the app: the code the page shows, pasted.
	run, rec, state = webClientSignIn(t, stored)
	run.Complete("  " + state + signInCodeSeparator + "good-code\n")

	if msg := waitDone(t, rec); msg != "" {
		t.Fatalf("pasted code: %s", msg)
	}

	// A code from another sign-in is refused.
	run, rec, _ = webClientSignIn(t, stored)
	run.Complete("someone-elses-state" + signInCodeSeparator + "good-code")

	if msg := waitDone(t, rec); !strings.Contains(msg, "does not match") {
		t.Errorf("state mismatch: %q", msg)
	}
}

func TestGKEOAuthWebClientRefusals(t *testing.T) {
	withAuthStore(t)
	withFakeGoogleIssuer(t)

	stored, _ := gkeUserCluster(t)

	for _, redirect := range []string{"http://pages.example.com/auth/", "https://pages.example.com/auth/?x=1", "ichor://signin", "pages.example.com"} {
		secrets, _ := json.Marshal(map[string]string{
			gcpFieldOAuthClientID: sampleGoogleClientID, gcpFieldOAuthClientSecret: "s", gcpFieldOAuthRedirectURL: redirect,
		})
		if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err == nil || !strings.Contains(err.Error(), "https page") {
			t.Errorf("%q: %v", redirect, err)
		}
	}

	for _, pasted := range []string{"~code", "state~", "~"} {
		if _, err := callbackAnswer(pasted); err == nil || !strings.Contains(err.Error(), "incomplete") {
			t.Errorf("%q: %v", pasted, err)
		}
	}

	if q, err := callbackAnswer("com.googleusercontent.apps.123-abc:/oauth2redirect?code=c~1&state=s"); err != nil || q.Get("code") != "c~1" || q.Get("state") != "s" {
		t.Errorf("a callback URL is read as a URL: %v %v", q, err)
	}
}
