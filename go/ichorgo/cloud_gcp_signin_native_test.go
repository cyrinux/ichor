package ichorgo

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gcpNativeSecrets is what the Android app hands over after Google's sign-in: the token valid
// until expiry, for an invented account.
func gcpNativeSecrets(t *testing.T, expiry time.Time, fields map[string]string) string {
	t.Helper()

	secrets := map[string]string{
		gcpFieldGoogleSignIn: googleSignInAndroid, gcpFieldAccessToken: gcpTestToken,
		gcpFieldAccessTokenExpiry: strconv.FormatInt(expiry.Unix(), 10), gcpFieldAccount: gcpTestUser,
	}
	for k, v := range fields {
		secrets[k] = v
	}

	raw, err := json.Marshal(secrets)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// gcpNativeCred is the credential GCPSignInNative gives, with the quota project picked.
func gcpNativeCred(t *testing.T, expiry time.Time) string {
	t.Helper()

	out, err := GCPSignInNative(gcpNativeSecrets(t, expiry, nil), false)
	result := decodeJSON[gcpSignInResult](t, out, err)

	var cred gcpCredential
	if err := json.Unmarshal([]byte(result.Credential), &cred); err != nil {
		t.Fatal(err)
	}

	cred.Project, cred.Zone = gcpTestProject, gcpTestZone

	return cred.encode()
}

// On Android's Play build, "Sign in with Google" is offered first, the app's token becomes the
// credential without a browser run, the projects are listed for the pick, and the test passes.
func TestGCPSignInNativeAndroid(t *testing.T) {
	withGoogleSignInClient(t, googleSignInAndroid, "")

	f := &fakeGCE{}
	startFakeGCE(t, f)

	choices, err := GCPSignInChoices()
	if err != nil || !strings.HasPrefix(choices, `["native",`) {
		t.Fatalf("choices %s %v", choices, err)
	}

	out, err := GCPSignInNative(gcpNativeSecrets(t, time.Now().Add(time.Hour), map[string]string{"other": "x"}), true)
	result := decodeJSON[gcpSignInResult](t, out, err)

	if result.Account != gcpTestUser || len(result.Projects) != 2 || result.Projects[1].ID != gcpTestProject {
		t.Fatalf("result %+v", result)
	}

	var cred gcpCredential
	if err := json.Unmarshal([]byte(result.Credential), &cred); err != nil || cred.GoogleSignIn == nil || cred.Project != "" ||
		cred.GoogleSignIn.secret(gcpFieldGoogleSignIn) != googleSignInAndroid || cred.GoogleSignIn.secret("other") != "" {
		t.Fatalf("credential %s %v", result.Credential, err)
	}

	// Without the project list (a silent renewal): the credential only, no call to Google.
	f.unauthorized = 0

	out, err = GCPSignInNative(gcpNativeSecrets(t, time.Now().Add(time.Hour), nil), false)
	if renewed := decodeJSON[gcpSignInResult](t, out, err); renewed.Credential == "" || len(renewed.Projects) != 0 {
		t.Fatalf("renewal %+v", renewed)
	}

	out, err = GCPTest(gcpNativeCred(t, time.Now().Add(time.Hour)))
	test := decodeJSON[gcpTestResult](t, out, err)

	if test.Account != gcpTestUser || test.Project != gcpTestProject || !test.Checked || !test.CanCreate || f.unauthorized != 0 {
		t.Fatalf("test %+v, unauthorized %d", test, f.unauthorized)
	}
}

// The native kind on Android: refused without a token, and an expired one answers the
// sign-in-required reason the app renews on (the runs mint the same way as the test and the options),
// without quoting the token.
func TestGCPSignInNativeAndroidRefusals(t *testing.T) {
	withGoogleSignInClient(t, googleSignInAndroid, "")
	startFakeGCE(t, &fakeGCE{})

	for _, bad := range []string{`{}`, `{"gcpAccessToken":"ya29.x"}`, `{"gcpAccessTokenExpiry":"1"}`, `not json`} {
		if _, err := GCPSignInNative(bad, true); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	expired := gcpNativeCred(t, time.Now().Add(time.Hour))

	var cred gcpCredential
	if err := json.Unmarshal([]byte(expired), &cred); err != nil {
		t.Fatal(err)
	}

	cred.GoogleSignIn.Session["bearerExpiry"] = strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expired = cred.encode()

	_, testErr := GCPTest(expired)
	_, optionsErr := GCPOptions("", "", expired)

	for name, err := range map[string]error{"test": testErr, "options": optionsErr} {
		if err == nil {
			t.Errorf("%s: expired token accepted", name)

			continue
		}

		msg := err.Error()
		if !strings.HasSuffix(msg, KubeSignInRequired+": sign in to this cluster (gke): "+gcpNativeRenewReason) ||
			!strings.Contains(msg, "it has ended") || strings.Contains(msg, gcpTestToken) {
			t.Errorf("%s: %q", name, msg)
		}
	}
}

// iOS keeps its browser flow (the app has no token to hand over), and a build without a Google
// client offers no native sign-in at all.
func TestGCPSignInNativeOtherBuilds(t *testing.T) {
	withGoogleSignInClient(t, googleSignInIOS, "123-sample.apps.googleusercontent.com")

	if secrets, err := gcpSignInSecrets(gcpSignInNative, ""); err != nil || secrets[gcpFieldGoogleSignIn] != googleSignInIOS {
		t.Fatalf("ios secrets %v %v", secrets, err)
	}

	if _, err := GCPSignInNative(gcpNativeSecrets(t, time.Now().Add(time.Hour), nil), true); err == nil {
		t.Fatal("ios took an app-held token")
	}

	withGoogleSignInClient(t, "", "")

	if choices, err := GCPSignInChoices(); err != nil || strings.Contains(choices, gcpSignInNative) {
		t.Fatalf("choices without a client %s %v", choices, err)
	}

	if _, err := GCPSignInNative(gcpNativeSecrets(t, time.Now().Add(time.Hour), nil), true); err == nil {
		t.Fatal("a build without a Google client took a token")
	}
}
