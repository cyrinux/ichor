package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Invented refresh tokens: the one the sign-in gave, and the one a refresh rotates to.
const (
	gcpTestRefresh = "sample-google-refresh-one"
	gcpTestRotated = "sample-google-refresh-two"
	gcpTestUser    = "someone@example.org"
)

// fakeGCEUser is Google's refresh grant for a signed-in account, gcloud's client.
type fakeGCEUser struct {
	refresh   string // the refresh token accepted
	rotate    string // sent back as the new one when set
	denyQuota bool   // the account may not bill calls to the project
	grants    []map[string]string
}

// token answers the refresh grant; false leaves the request to the key's JWT bearer grant.
func (u *fakeGCEUser) token(w http.ResponseWriter, r *http.Request) bool {
	_ = r.ParseForm()

	if r.PostForm.Get("grant_type") != "refresh_token" {
		return false
	}

	u.grants = append(u.grants, map[string]string{
		"refresh_token": r.PostForm.Get("refresh_token"), "client_id": r.PostForm.Get("client_id"), "client_secret": r.PostForm.Get("client_secret"),
	})

	if r.PostForm.Get("refresh_token") != u.refresh {
		gceJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "Token has been expired or revoked. " + r.PostForm.Get("refresh_token")})

		return true
	}

	answer := map[string]any{"access_token": gcpTestToken, "expires_in": 3599}
	if u.rotate != "" {
		answer["refresh_token"], u.refresh = u.rotate, u.rotate
	}

	gceJSON(w, http.StatusOK, answer)

	return true
}

func gceUserProjectDenied(w http.ResponseWriter) {
	gceJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{
		"code": 403, "message": "Caller does not have required permission to use project " + gcpTestProject + ".",
		"errors": []any{map[string]string{"domain": "global", "reason": "forbidden", "message": "denied"}},
		"status": "PERMISSION_DENIED",
		"details": []any{map[string]any{
			"@type": "type.googleapis.com/google.rpc.ErrorInfo", "reason": gcpReasonUserProjectDenied, "domain": "googleapis.com",
		}},
	}})
}

// gcpTestUserState is gcloud's sign-in as the GKE sign-in stores it: the user credentials.
func gcpTestUserState() kubeAuthState {
	return kubeAuthState{Method: authGKE, User: gcpTestUser, Secrets: map[string]string{
		gcpFieldGcloudSignIn: "1", gcpFieldUserCredentials: adcJSON("authorized_user", gcpTestRefresh),
	}}
}

func gcpTestUserCred() string {
	state := gcpTestUserState()

	return gcpCredential{GoogleSignIn: &state, Account: gcpTestUser, Project: gcpTestProject, Zone: gcpTestZone}.encode()
}

// A signed-in account mints with the refresh grant (gcloud's client, no key), the Compute calls
// carry the quota project, and the rotated refresh token comes back to seal again.
func TestGCPUserCredentialTests(t *testing.T) {
	f := &fakeGCE{user: &fakeGCEUser{refresh: gcpTestRefresh, rotate: gcpTestRotated}}
	startFakeGCE(t, f)

	out, err := GCPTest(gcpTestUserCred())
	test := decodeJSON[gcpTestResult](t, out, err)

	if test.Account != gcpTestUser || test.Project != gcpTestProject || !test.Checked || !test.CanCreate || test.RenewHint == "" {
		t.Fatalf("test %+v", test)
	}

	if len(f.user.grants) != 1 || f.user.grants[0]["refresh_token"] != gcpTestRefresh || f.user.grants[0]["client_id"] != "gcloud-client.apps.example.com" {
		t.Fatalf("grants %v", f.user.grants)
	}

	if f.noQuotaHeader != 0 || f.unauthorized != 0 {
		t.Fatalf("quota header missing %d, unauthorized %d", f.noQuotaHeader, f.unauthorized)
	}

	var sealed gcpCredential
	if err := json.Unmarshal([]byte(test.Credential), &sealed); err != nil || sealed.GoogleSignIn == nil || sealed.Project != gcpTestProject {
		t.Fatalf("credential %s %v", test.Credential, err)
	}

	creds, _ := parseUserCredentials(sealed.GoogleSignIn.secret(gcpFieldUserCredentials))
	if creds.RefreshToken != gcpTestRotated {
		t.Fatalf("not rotated: %+v", creds)
	}

	// The options of the same account: the rotation was taken, nothing more to seal.
	f.user.rotate = ""
	out, err = GCPOptions("", "", test.Credential)
	options := decodeJSON[gcpOptions](t, out, err)

	if options.Credential != "" || options.Project != gcpTestProject {
		t.Fatalf("options credential %q project %q", options.Credential, options.Project)
	}
}

// A refused refresh token asks to sign in again with the hint, and quotes no token; a project the
// account may not bill to is named.
func TestGCPUserCredentialRefusals(t *testing.T) {
	f := &fakeGCE{user: &fakeGCEUser{refresh: "another-refresh"}}
	startFakeGCE(t, f)

	_, err := GCPTest(gcpTestUserCred())
	if err == nil || !strings.Contains(err.Error(), "Google sign-in: it has ended") || !strings.Contains(err.Error(), "sign in") ||
		strings.Contains(err.Error(), gcpTestRefresh) {
		t.Fatalf("refused refresh: %v", err)
	}

	f.user.refresh, f.user.denyQuota = gcpTestRefresh, true

	_, err = GCPTest(gcpTestUserCred())
	if err == nil || !strings.Contains(err.Error(), "may not bill calls to project "+gcpTestProject) {
		t.Fatalf("quota project: %v", err)
	}

	for _, bad := range []gcpCredential{
		{GoogleSignIn: &kubeAuthState{}, Project: gcpTestProject},
		{GoogleSignIn: func() *kubeAuthState { s := gcpTestUserState(); return &s }()},
	} {
		if _, err := newGCPClient(bad.encode()); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

// A key still works as before.
func TestGCPKeyUnchangedBySignIn(t *testing.T) {
	startFakeGCE(t, &fakeGCE{})

	out, err := GCPTest(gcpTestCred(t))
	test := decodeJSON[gcpTestResult](t, out, err)
	if test.Account != gcpTestEmail || test.Credential != "" || test.RenewHint != "" {
		t.Fatalf("test %+v", test)
	}
}

// After a sign-in run: its session becomes the credential (no project yet) and the account's
// projects are listed for the pick; taken once.
func TestGCPSignInCredential(t *testing.T) {
	f := &fakeGCE{user: &fakeGCEUser{refresh: gcpTestRefresh}}
	startFakeGCE(t, f)

	secrets, err := gcpSignInSecrets(gcpSignInGcloud, "")
	if err != nil {
		t.Fatal(err)
	}

	signed := gcpTestUserState()
	gcpSignInRun.mu.Lock()
	gcpSignInRun.secrets = secrets
	gcpSignInRun.mu.Unlock()

	discoverySignIn.mu.Lock()
	discoverySignIn.state = &signed
	discoverySignIn.mu.Unlock()

	t.Cleanup(ForgetDiscoverSignIn)

	out, err := GCPSignInCredential()
	result := decodeJSON[gcpSignInResult](t, out, err)

	if result.Account != gcpTestUser || len(result.Projects) != 2 || result.Projects[1].ID != gcpTestProject {
		t.Fatalf("result %+v", result)
	}

	var cred gcpCredential
	if err := json.Unmarshal([]byte(result.Credential), &cred); err != nil || cred.GoogleSignIn == nil || cred.Project != "" {
		t.Fatalf("credential %s", result.Credential)
	}

	if _, err := GCPSignInCredential(); !errors.Is(err, errGCPSignInFirst) {
		t.Fatalf("second take: %v", err)
	}
}

// The kinds and their fields; a kind that cannot run ends the listener with the reason.
func TestGCPSignInKinds(t *testing.T) {
	tests := []struct {
		kind, secrets, field string
		ok                   bool
	}{
		{gcpSignInGcloud, "", gcpFieldGcloudSignIn, true},
		{gcpSignInOAuth, `{"gcpOAuthClientId":"sample.apps.example.com","gcpOAuthClientSecret":"s","other":"x"}`, gcpFieldOAuthClientID, true},
		{gcpSignInOAuth, `{}`, "", false},
		{gcpSignInWorkforce, `{"gcpLoginConfigJson":"{}"}`, gcpFieldLoginConfig, true},
		{gcpSignInWorkforce, ``, "", false},
		{"telepathy", ``, "", false},
	}

	for _, tt := range tests {
		secrets, err := gcpSignInSecrets(tt.kind, tt.secrets)
		if (err == nil) != tt.ok || (tt.ok && secrets[tt.field] == "") || secrets["other"] != "" {
			t.Errorf("%s %s: %v %v", tt.kind, tt.secrets, secrets, err)
		}
	}

	if choices, err := GCPSignInChoices(); err != nil || !strings.Contains(choices, `"gcloud"`) {
		t.Errorf("choices %s %v", choices, err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	GCPSignInStart("telepathy", "", rec)

	select {
	case msg := <-rec.done:
		if !strings.Contains(msg, "not a Google sign-in") {
			t.Fatalf("done %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no end")
	}
}
