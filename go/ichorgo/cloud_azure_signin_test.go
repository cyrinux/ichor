package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Invented tokens: the refresh token the device flow gives, and the one a refresh rotates to.
const (
	azureTestRefresh  = "sample-refresh-token-one"
	azureTestRotated  = "sample-refresh-token-two"
	azureTestUserCode = "SAMPLE123"
	azureTestAccount  = "someone@example.org"
)

// fakeAzureUser is Entra ID's device-code and refresh side for a Microsoft account.
type fakeAzureUser struct {
	pending       int    // authorization_pending answers before the tokens
	deviceError   string // the device-code endpoint's error_description, "" to answer
	refuseRefresh bool   // the refresh token is refused (invalid_grant)
	noRotate      bool   // a refresh sends no new refresh token

	deviceScope string
	refreshes   int
	refreshForm map[string]string
}

func azureTestIDToken() string {
	claims, _ := json.Marshal(map[string]string{"preferred_username": azureTestAccount, "tid": azureTestTenant})

	return "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

func (u *fakeAzureUser) deviceCode(_ *fakeARM, w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	u.deviceScope = r.PostForm.Get("scope")

	if u.deviceError != "" {
		armJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": u.deviceError})

		return
	}

	armJSON(w, http.StatusOK, map[string]any{
		"device_code": "sample-device-code", "user_code": azureTestUserCode, "verification_uri": "https://microsoft.com/devicelogin",
		"expires_in": 900, "interval": 0,
	})
}

// handlesToken answers the device-code and refresh grants; false leaves the request to the
// service principal's client credentials.
func (u *fakeAzureUser) handlesToken(f *fakeARM, w http.ResponseWriter, r *http.Request) bool {
	if err := r.ParseForm(); err != nil {
		return false
	}

	switch r.PostForm.Get("grant_type") {
	case "urn:ietf:params:oauth:grant-type:device_code":
		f.tokenForms = append(f.tokenForms, r.PostForm)

		if u.pending > 0 {
			u.pending--
			armJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending", "error_description": "AADSTS70016: pending"})

			return true
		}

		armJSON(w, http.StatusOK, map[string]any{
			"access_token": azureTestToken, "refresh_token": azureTestRefresh, "id_token": azureTestIDToken(), "expires_in": 3599,
		})
	case "refresh_token":
		f.tokenForms = append(f.tokenForms, r.PostForm)
		u.refreshes++
		u.refreshForm = map[string]string{"refresh_token": r.PostForm.Get("refresh_token"), "scope": r.PostForm.Get("scope"),
			"client_secret": r.PostForm.Get("client_secret"), "client_id": r.PostForm.Get("client_id")}

		if u.refuseRefresh {
			armJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid_grant", "error_description": "AADSTS700082: The refresh token has expired due to inactivity. " + r.PostForm.Get("refresh_token"),
			})

			return true
		}

		answer := map[string]any{"access_token": azureTestToken, "expires_in": 3599}
		if !u.noRotate {
			answer["refresh_token"] = azureTestRotated
		}

		armJSON(w, http.StatusOK, answer)
	default:
		return false
	}

	return true
}

// azureTestUserCred is a signed-in account's credential, bound to the scratch subscription.
func azureTestUserCred() string {
	return azureCredential{
		TenantID: azureTestTenant, ClientID: azureCLIClientID, SubscriptionID: azureTestSub, Location: "westeurope",
		azureUserCredential: azureUserCredential{Kind: azureKindUser, RefreshToken: azureTestRefresh, Account: azureTestAccount},
	}.encode()
}

func fastDevicePolls(t *testing.T) {
	t.Helper()

	saved := deviceMinInterval
	deviceMinInterval = 5 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = saved })
}

func decodeAzure[T any](t *testing.T, out string) T {
	t.Helper()

	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%s: %v", out, err)
	}

	return v
}

// Start shows the code, Finish waits through two pending polls and lists the subscriptions,
// Choose binds the credential to one and to its tenant.
func TestAzureSignInDeviceFlow(t *testing.T) {
	fastDevicePolls(t)

	f := &fakeARM{user: &fakeAzureUser{pending: 2}}
	startFakeARM(t, f)

	out, err := AzureSignInStart(`{"tenantId":"","clientId":""}`)
	if err != nil {
		t.Fatal(err)
	}

	start := decodeAzure[azureSignInStartResult](t, out)
	if start.UserCode != azureTestUserCode || start.VerificationURI != "https://microsoft.com/devicelogin" {
		t.Fatalf("start %+v", start)
	}

	if f.user.deviceScope != "https://management.azure.com/.default offline_access openid profile" {
		t.Fatalf("scope %q", f.user.deviceScope)
	}

	if !strings.Contains(start.Credential, `"tenantId":"organizations"`) || !strings.Contains(start.Credential, azureCLIClientID) {
		t.Fatalf("defaults %s", start.Credential)
	}

	out, err = AzureSignInFinish(start.Credential)
	if err != nil {
		t.Fatal(err)
	}

	finish := decodeAzure[azureSignInFinishResult](t, out)
	if finish.Account != azureTestAccount || len(finish.Subscriptions) != 1 || finish.Subscriptions[0].TenantID != azureTestTenant {
		t.Fatalf("finish %+v", finish)
	}

	if _, err := AzureSignInChoose(finish.Credential, "00000000-0000-0000-0000-00000000ffff"); err == nil {
		t.Fatal("a subscription the sign-in did not list was taken")
	}

	out, err = AzureSignInChoose(finish.Credential, azureTestSub)
	if err != nil {
		t.Fatal(err)
	}

	chose := decodeAzure[azureSignInChooseResult](t, out)

	cred, err := parseAzureCredential(chose.Credential)
	if err != nil || chose.SubscriptionName != "Scratch" || cred.SubscriptionID != azureTestSub || cred.TenantID != azureTestTenant ||
		cred.RefreshToken != azureTestRotated || cred.Choices != nil || cred.Device != nil {
		t.Fatalf("choose %+v %+v %v", chose, cred, err)
	}
}

// A user credential mints ARM tokens with the refresh grant (no secret), and the rotated refresh
// token comes back for the app to seal again; with no rotation nothing comes back.
func TestAzureUserCredentialRefreshes(t *testing.T) {
	f := &fakeARM{user: &fakeAzureUser{}}
	startFakeARM(t, f)

	out, err := AzureTest(azureTestUserCred())
	if err != nil {
		t.Fatal(err)
	}

	test := decodeAzure[azureTestResult](t, out)
	if !test.CanCreate || test.Account != azureTestAccount {
		t.Fatalf("test %+v", test)
	}

	form := f.user.refreshForm
	if form["refresh_token"] != azureTestRefresh || form["scope"] != "https://management.azure.com/.default offline_access" ||
		form["client_secret"] != "" || form["client_id"] != azureCLIClientID {
		t.Fatalf("refresh form %v", form)
	}

	if cred, _ := parseAzureCredential(test.Credential); cred.RefreshToken != azureTestRotated || cred.SubscriptionID != azureTestSub {
		t.Fatalf("credential %s", test.Credential)
	}

	f.user.noRotate = true

	out, err = AzureOptions("", "", azureTestUserCred())
	if err != nil {
		t.Fatal(err)
	}

	if options := decodeAzure[azureOptions](t, out); options.Credential != "" || len(options.ResourceGroups) == 0 {
		t.Fatalf("options credential %q groups %v", options.Credential, options.ResourceGroups)
	}
}

// A refused refresh token asks to sign in again, and never quotes the token.
func TestAzureUserRefreshRefused(t *testing.T) {
	f := &fakeARM{user: &fakeAzureUser{refuseRefresh: true}}
	startFakeARM(t, f)

	_, err := AzureTest(azureTestUserCred())
	if !errors.Is(err, errAzureSignInAgain) && (err == nil || err.Error() != errAzureSignInAgain.Error()) {
		t.Fatalf("error %v", err)
	}

	if strings.Contains(err.Error(), azureTestRefresh) {
		t.Fatalf("the refresh token leaked: %v", err)
	}
}

// The device code's refusals name what to fix.
func TestAzureSignInRefusals(t *testing.T) {
	tests := []struct {
		name, description, want string
	}{
		{"unknown tenant", "AADSTS90002: Tenant 'x' not found.", "enter the tenant id"},
		{"not a public client", "AADSTS7000218: The request body must contain client_assertion or client_secret.", "allow public client flows"},
		{"blocked client", "AADSTS53003: Access has been blocked by Conditional Access policies.", "client id of an app registration"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startFakeARM(t, &fakeARM{user: &fakeAzureUser{deviceError: tt.description}})

			_, err := AzureSignInStart(`{"tenantId":"` + azureTestTenant + `"}`)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v", err)
			}
		})
	}

	// A code that only starts like AADSTS70002 is not the public-client refusal; pending keeps polling.
	for _, d := range []string{"AADSTS700025: Client is public.", "AADSTS70016: pending"} {
		if err := azureSignInRefusal(oidcTokens{Error: "invalid_request", Description: d}); err != nil {
			t.Errorf("%s: %v", d, err)
		}
	}

	if !azureTenantPattern.MatchString(strings.ToLower("Example.OnMicrosoft.com")) {
		t.Error("a domain tenant refused")
	}

	for _, bad := range []string{`{"tenantId":"not a tenant"}`, `{"clientId":"not-a-guid"}`} {
		if _, err := AzureSignInStart(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	if _, err := AzureSignInFinish(`{"kind":"user"}`); err == nil {
		t.Error("finish without a start accepted")
	}

	if _, err := AzureSignInChoose(`{"kind":"user"}`, azureTestSub); !errors.Is(err, errAzureNoUser) {
		t.Errorf("choose without a sign-in: %v", err)
	}
}

// Cancel ends a Finish that is still waiting for the browser.
func TestAzureSignInCancel(t *testing.T) {
	fastDevicePolls(t)
	startFakeARM(t, &fakeARM{user: &fakeAzureUser{pending: 1 << 30}})

	out, err := AzureSignInStart("")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() {
		_, err := AzureSignInFinish(decodeAzure[azureSignInStartResult](t, out).Credential)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	AzureSignInCancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Fatalf("error %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not stop the wait")
	}
}

// A user credential without its refresh token, or not bound to a subscription, is refused before
// any call; the refresh token is scrubbed from messages.
func TestAzureUserClientChecks(t *testing.T) {
	signedOut := azureCredential{TenantID: azureTestTenant, SubscriptionID: azureTestSub, azureUserCredential: azureUserCredential{Kind: azureKindUser}}
	if _, err := newAzureClient(signedOut.encode()); !errors.Is(err, errAzureSignInAgain) {
		t.Errorf("signed out: %v", err)
	}

	unbound := azureCredential{TenantID: azureTestTenant, azureUserCredential: azureUserCredential{Kind: azureKindUser, RefreshToken: azureTestRefresh}}
	if _, err := newAzureClient(unbound.encode()); err == nil {
		t.Error("no subscription accepted")
	}

	c, err := newAzureClient(azureTestUserCred())
	if err != nil {
		t.Fatal(err)
	}

	if got := c.scrub("bad token " + azureTestRefresh); strings.Contains(got, azureTestRefresh) {
		t.Fatalf("scrub %q", got)
	}

	if c.updated() != "" || c.account() != azureTestAccount {
		t.Fatalf("updated %q account %q", c.updated(), c.account())
	}
}
