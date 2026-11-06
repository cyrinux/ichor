package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Invented Identity Center samples.
const (
	awsTestStartURL = "https://example.awsapps.com/start"
	awsTestRole     = "IchorNodeCreator"
)

// fakeSSO is IAM Identity Center's OIDC and portal for the EC2 sign-in: it hands out the sample
// keys of fakeEC2 as role credentials, so the calls that follow are signed with them.
type fakeSSO struct {
	mu sync.Mutex

	pending   int    // device polls answered authorization_pending
	deviceErr string // the error the device poll answers instead of a token
	refreshed int    // refresh_token grants
	refuse    string // the error a refresh answers (invalid_grant: the session is over)
	creds     int    // role credential calls
}

func startFakeSSO(t *testing.T, s *fakeSSO) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(srv.Close)

	oidc, portal, client, interval := awsSSOOIDCEndpoint, awsSSOPortalEndpoint, awsHTTPClient, deviceMinInterval
	awsSSOOIDCEndpoint = func(string) string { return srv.URL + "/oidc" }
	awsSSOPortalEndpoint = func(string) string { return srv.URL + "/portal" }
	awsHTTPClient = func() *http.Client { return srv.Client() }
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() {
		awsSSOOIDCEndpoint, awsSSOPortalEndpoint, awsHTTPClient, deviceMinInterval = oidc, portal, client, interval
	})
}

func (s *fakeSSO) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, _ := io.ReadAll(r.Body)

	var req map[string]string

	_ = json.Unmarshal(body, &req)

	switch r.URL.Path {
	case "/oidc/client/register":
		_, _ = io.WriteString(w, `{"clientId":"cid","clientSecret":"csecret","clientSecretExpiresAt":4102444800}`)
	case "/oidc/device_authorization":
		if req["startUrl"] != awsTestStartURL {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		_, _ = io.WriteString(w, `{"deviceCode":"dc","userCode":"WXYZ-1234","verificationUri":"https://device.sso/","verificationUriComplete":"https://device.sso/?user_code=WXYZ-1234","expiresIn":60,"interval":0}`)
	case "/oidc/token":
		s.token(w, req)
	case "/portal/assignment/accounts":
		if !s.authorized(w, r) {
			return
		}

		fmt.Fprintf(w, `{"accountList":[{"accountId":%q,"accountName":"Sandbox"}]}`, awsTestAccount)
	case "/portal/assignment/roles":
		if !s.authorized(w, r) {
			return
		}

		fmt.Fprintf(w, `{"roleList":[{"roleName":%q},{"roleName":"ReadOnly"}]}`, awsTestRole)
	case "/portal/federation/credentials":
		if !s.authorized(w, r) {
			return
		}

		s.creds++
		fmt.Fprintf(w, `{"roleCredentials":{"accessKeyId":%q,"secretAccessKey":%q,"sessionToken":%q,"expiration":%d}}`,
			awsTestKeyID, awsTestSecret, awsTestToken, time.Now().Add(time.Hour).UnixMilli())
	default:
		http.NotFound(w, r)
	}
}

func (s *fakeSSO) token(w http.ResponseWriter, req map[string]string) {
	if req["clientId"] != "cid" || req["clientSecret"] != "csecret" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_client"}`)

		return
	}

	switch req["grantType"] {
	case "urn:ietf:params:oauth:grant-type:device_code":
		switch {
		case s.pending > 0:
			s.pending--
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"AuthorizationPendingException"}`)
		case s.deviceErr != "":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q}`, s.deviceErr)
		default:
			_, _ = io.WriteString(w, `{"accessToken":"sso-access-0","refreshToken":"sso-refresh-0","expiresIn":3600}`)
		}
	case "refresh_token":
		if s.refuse != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":%q,"error_description":"the session ended"}`, s.refuse)

			return
		}

		s.refreshed++
		fmt.Fprintf(w, `{"accessToken":"sso-access-%d","expiresIn":3600}`, s.refreshed)
	}
}

// authorized refuses an access token the fake did not hand out, or one marked revoked.
func (s *fakeSSO) authorized(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Amz-Sso_bearer_token")
	if strings.HasPrefix(token, "sso-access-") && token != "sso-access-revoked" {
		return true
	}

	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, `{"message":"Session token not found or invalid"}`)

	return false
}

// awsSignedIn is a credential signed in and picked, its access token and role keys given.
func awsSignedIn(accessToken string, tokenLeft, roleLeft time.Duration) awsCredentialFile {
	f := awsCredentialFile{
		Region: "eu-west-1", SSOStartURL: awsTestStartURL, SSORegion: "eu-west-1", AccountID: awsTestAccount, RoleName: awsTestRole,
		SSOToken: &awsStoredSSOToken{AccessToken: accessToken, RefreshToken: "sso-refresh-0", ExpiresAt: time.Now().Add(tokenLeft).Unix(), ClientID: "cid", ClientSecret: "csecret"},
	}

	if roleLeft != 0 {
		f.RoleCredentials = &awsStoredRole{awsTestKeyID, awsTestSecret, awsTestToken, time.Now().Add(roleLeft).Unix()}
	}

	return f
}

func decodeAWSCredential(t *testing.T, raw string) awsCredentialFile {
	t.Helper()

	f, err := parseAWSCredential(raw)
	if err != nil {
		t.Fatalf("credential %q: %v", raw, err)
	}

	return f
}

func TestAWSSignInStartFinishChoose(t *testing.T) {
	startFakeEC2(t, &fakeEC2{})

	sso := &fakeSSO{pending: 1}
	startFakeSSO(t, sso)

	out, err := AWSSignInStart(`{"ssoStartUrl":"` + awsTestStartURL + `","ssoRegion":"eu-west-1"}`)
	if err != nil {
		t.Fatal(err)
	}

	var start awsSignInStartResult
	if err := json.Unmarshal([]byte(out), &start); err != nil || start.UserCode != "WXYZ-1234" || !strings.Contains(start.VerificationURIComplete, "user_code=") {
		t.Fatalf("start = %s, %v", out, err)
	}

	if f := decodeAWSCredential(t, start.Credential); f.Region != "eu-west-1" || f.SSODevice == nil || f.SSOToken.ClientID != "cid" {
		t.Errorf("start credential = %s", start.Credential)
	}

	out, err = AWSSignInFinish(start.Credential)
	if err != nil {
		t.Fatal(err)
	}

	var finish awsSignInFinishResult
	if err := json.Unmarshal([]byte(out), &finish); err != nil || len(finish.Roles) != 2 || finish.Roles[0].RoleName != awsTestRole || finish.Roles[0].AccountName != "Sandbox" {
		t.Fatalf("finish = %s, %v", out, err)
	}

	if f := decodeAWSCredential(t, finish.Credential); f.SSODevice != nil || f.SSOToken.AccessToken != "sso-access-0" || f.SSOToken.RefreshToken != "sso-refresh-0" {
		t.Errorf("finish credential = %s", finish.Credential)
	}

	out, err = AWSSignInChoose(finish.Credential, awsTestAccount, awsTestRole)
	if err != nil {
		t.Fatal(err)
	}

	var chosen awsSignInChooseResult
	if err := json.Unmarshal([]byte(out), &chosen); err != nil || chosen.Account != awsTestAccount {
		t.Fatalf("choose = %s, %v", out, err)
	}

	f := decodeAWSCredential(t, chosen.Credential)
	if f.AccountName != "Sandbox" || f.RoleName != awsTestRole || f.SSORoles != nil || f.RoleCredentials == nil || f.RoleCredentials.AccessKeyID != awsTestKeyID {
		t.Errorf("chosen credential = %s", chosen.Credential)
	}

	// The role's keys sign the calls as access keys would; nothing to renew yet.
	out, err = AWSTest(chosen.Credential)
	if err != nil || !strings.Contains(out, `"account":"`+awsTestAccount+`"`) || strings.Contains(out, `"credential"`) {
		t.Errorf("test = %s, %v", out, err)
	}

	if sso.creds != 1 || sso.refreshed != 0 {
		t.Errorf("role credential calls = %d, refreshes = %d", sso.creds, sso.refreshed)
	}
}

func TestAWSSignInRefusals(t *testing.T) {
	sso := &fakeSSO{deviceErr: "expired_token"}
	startFakeSSO(t, sso)

	for _, bad := range []string{`{"ssoStartUrl":"http://example.awsapps.com/start","ssoRegion":"eu-west-1"}`, `{"ssoStartUrl":"` + awsTestStartURL + `","ssoRegion":"nowhere"}`, `{`} {
		if _, err := AWSSignInStart(bad); err == nil {
			t.Errorf("start %s: no error", bad)
		}
	}

	out, err := AWSSignInStart(`{"ssoStartUrl":"` + awsTestStartURL + `","ssoRegion":"eu-west-1"}`)
	if err != nil {
		t.Fatal(err)
	}

	var start awsSignInStartResult
	_ = json.Unmarshal([]byte(out), &start)

	if _, err := AWSSignInFinish(start.Credential); err == nil || !strings.Contains(err.Error(), "expired_token") {
		t.Errorf("expired device code: %v", err)
	}

	if _, err := AWSSignInFinish(`{"region":"eu-west-1"}`); err == nil {
		t.Error("finish without a start: no error")
	}

	if _, err := AWSSignInChoose(awsTestCred, awsTestAccount, awsTestRole); err == nil {
		t.Error("choose on access keys: no error")
	}
}

func TestAWSSignInCancel(t *testing.T) {
	sso := &fakeSSO{pending: 1 << 20}
	startFakeSSO(t, sso)

	out, err := AWSSignInStart(`{"ssoStartUrl":"` + awsTestStartURL + `","ssoRegion":"eu-west-1"}`)
	if err != nil {
		t.Fatal(err)
	}

	var start awsSignInStartResult
	_ = json.Unmarshal([]byte(out), &start)

	go func() {
		time.Sleep(50 * time.Millisecond)
		AWSSignInCancel()
	}()

	if _, err := AWSSignInFinish(start.Credential); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("cancelled sign-in: %v", err)
	}
}

func TestAWSSSORenewsRoleCredentials(t *testing.T) {
	startFakeEC2(t, &fakeEC2{})

	sso := &fakeSSO{}
	startFakeSSO(t, sso)

	// Role credentials run out, the access token still good: new role credentials, no refresh.
	out, err := AWSPrice(awsSignedIn("sso-access-0", time.Hour, time.Minute).encode(), "t3.small")
	if err != nil {
		t.Fatal(err)
	}

	var price awsPriceResult
	_ = json.Unmarshal([]byte(out), &price)

	if f := decodeAWSCredential(t, price.Credential); price.Hourly == "" || f.RoleCredentials == nil || time.Until(time.Unix(f.RoleCredentials.ExpiresAt, 0)) < 50*time.Minute || sso.refreshed != 0 {
		t.Errorf("renewed role = %s (refreshes %d)", out, sso.refreshed)
	}

	// The access token over too: renewed with the refresh token, which is kept.
	out, err = AWSTest(awsSignedIn("sso-access-0", 0, 0).encode())
	if err != nil {
		t.Fatal(err)
	}

	var test awsTestResult
	_ = json.Unmarshal([]byte(out), &test)

	if f := decodeAWSCredential(t, test.Credential); f.SSOToken.AccessToken != "sso-access-1" || f.SSOToken.RefreshToken != "sso-refresh-0" || sso.refreshed != 1 {
		t.Errorf("renewed token = %s", test.Credential)
	}

	// A cached access token AWS no longer takes: renewed once, then the role.
	_, next, changed, err := awsResolve(context.Background(), awsSignedIn("sso-access-revoked", time.Hour, 0), true, awsRoleMinLeft)
	if err != nil || !changed || next.SSOToken.AccessToken != "sso-access-2" || next.RoleCredentials == nil {
		t.Errorf("revoked token: %v, changed %v, %+v", err, changed, next.SSOToken)
	}
}

func TestAWSSSOSessionOver(t *testing.T) {
	startFakeEC2(t, &fakeEC2{})

	sso := &fakeSSO{refuse: "invalid_grant"}
	startFakeSSO(t, sso)

	signedIn := awsSignedIn("sso-access-0", 0, 0)

	_, next, changed, err := awsResolve(context.Background(), signedIn, true, awsRoleMinLeft)
	if !errors.Is(err, errAWSSignInAgain) || !changed || next.SSOToken != nil || next.RoleCredentials != nil || next.SSOStartURL != awsTestStartURL || next.RoleName != awsTestRole {
		t.Errorf("session over: %v, changed %v, %+v", err, changed, next)
	}

	_, err = AWSTest(signedIn.encode())
	if err == nil || !strings.Contains(err.Error(), "sign in to IAM Identity Center again") {
		t.Errorf("test after the session: %v", err)
	}

	for _, secret := range []string{"sso-refresh-0", "csecret", awsTestSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error carries %s: %v", secret, err)
		}
	}
}

func TestAWSRunsUseWhatTheyAreGiven(t *testing.T) {
	startFakeEC2(t, &fakeEC2{})

	sso := &fakeSSO{}
	startFakeSSO(t, sso)

	if _, err := newAWSClient(awsSignedIn("sso-access-0", time.Hour, 2*time.Minute).encode()); !errors.Is(err, errAWSRenewFirst) {
		t.Errorf("run with role credentials running out: %v", err)
	}

	if c, err := newAWSClient(awsSignedIn("sso-access-0", time.Hour, time.Hour).encode()); err != nil || c.creds.accessKeyID != awsTestKeyID || c.region != "eu-west-1" {
		t.Errorf("run with fresh role credentials: %v", err)
	}

	if sso.creds != 0 {
		t.Errorf("a run fetched role credentials %d times", sso.creds)
	}

	// AWSRenew before a run: 30 minutes at least.
	out, err := AWSRenew(awsSignedIn("sso-access-0", time.Hour, 10*time.Minute).encode())
	if err != nil {
		t.Fatal(err)
	}

	var renewed awsCredentialResult
	_ = json.Unmarshal([]byte(out), &renewed)

	if _, err := newAWSClient(renewed.Credential); err != nil {
		t.Errorf("run after AWSRenew: %v (%s)", err, out)
	}

	// Access keys pass as they are.
	if out, err := AWSRenew(awsTestCred); err != nil || out != `{}` {
		t.Errorf("renew access keys = %s, %v", out, err)
	}
}
