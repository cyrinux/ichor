package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAWS answers STS AssumeRole, IAM Identity Center OIDC and the SSO portal.
type fakeAWS struct {
	*httptest.Server

	mu       sync.Mutex
	pending  int
	refreshN int
	grants   []string
	// devices counts the device codes asked for; singleRole lists one account with one role.
	devices    int
	singleRole bool
	// perRoleKeys hands each role its own access key, ASIA<ROLE>, instead of ASIASSO.
	perRoleKeys bool
}

// The accounts and roles the fake portal lists (invented), the first page ending with a
// nextToken.
var fakeSSOAccounts = []struct{ id, name string }{{"210987654321", "Staging"}, {"123456789012", "Production"}}

var fakeSSORoles = map[string][]string{"123456789012": {"ReadOnly", "Admin"}, "210987654321": {"ReadOnly"}}

// portal answers the account and role listings.
func (a *fakeAWS) portal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if !strings.HasPrefix(r.Header.Get("X-Amz-Sso_bearer_token"), "sso-access-") || q.Get("max_result") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Session token not found or invalid"}`)

		return
	}

	accounts := fakeSSOAccounts
	if a.singleRole {
		accounts = accounts[1:]
	}

	if r.URL.Path == "/portal/assignment/accounts" {
		page, next := accounts[:1], `"p2"`
		if q.Get("next_token") == "p2" || len(accounts) == 1 {
			page, next = accounts[len(accounts)-1:], "null"
		}

		list := make([]string, 0, len(page))
		for _, acc := range page {
			list = append(list, fmt.Sprintf(`{"accountId":%q,"accountName":%q,"emailAddress":"aws@example.com"}`, acc.id, acc.name))
		}

		fmt.Fprintf(w, `{"accountList":[%s],"nextToken":%s}`, strings.Join(list, ","), next)

		return
	}

	roles := fakeSSORoles[q.Get("account_id")]
	if a.singleRole {
		roles = roles[:1]
	}

	list := make([]string, 0, len(roles))
	for _, role := range roles {
		list = append(list, fmt.Sprintf(`{"accountId":%q,"roleName":%q}`, q.Get("account_id"), role))
	}

	fmt.Fprintf(w, `{"roleList":[%s]}`, strings.Join(list, ","))
}

func newFakeAWS(t *testing.T) *fakeAWS {
	t.Helper()

	a := &fakeAWS{pending: 1}
	a.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()

		body, _ := io.ReadAll(r.Body)

		switch {
		case strings.HasPrefix(r.URL.Path, "/sts"):
			if !strings.Contains(string(body), "Action=AssumeRole") || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIDBASE/") {
				w.WriteHeader(http.StatusForbidden)

				return
			}

			fmt.Fprintf(w, `<AssumeRoleResponse><AssumeRoleResult><Credentials><AccessKeyId>ASIAROLE</AccessKeyId><SecretAccessKey>s</SecretAccessKey><SessionToken>tok</SessionToken><Expiration>%s</Expiration></Credentials></AssumeRoleResult></AssumeRoleResponse>`, time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		case r.URL.Path == "/oidc/client/register":
			_, _ = io.WriteString(w, `{"clientId":"cid","clientSecret":"csecret"}`)
		case r.URL.Path == "/oidc/device_authorization":
			a.devices++
			_, _ = io.WriteString(w, `{"deviceCode":"dc","userCode":"WXYZ-1234","verificationUri":"https://device.sso/","verificationUriComplete":"https://device.sso/?user_code=WXYZ-1234","expiresIn":60,"interval":0}`)
		case r.URL.Path == "/oidc/token":
			var req map[string]string
			_ = json.Unmarshal(body, &req)
			a.grants = append(a.grants, req["grantType"])

			if req["grantType"] == "urn:ietf:params:oauth:grant-type:device_code" && a.pending > 0 {
				a.pending--
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)

				return
			}

			a.refreshN++
			fmt.Fprintf(w, `{"accessToken":"sso-access-%d","refreshToken":"sso-refresh-%d","expiresIn":28800}`, a.refreshN, a.refreshN)
		case strings.HasPrefix(r.URL.Path, "/portal/assignment/"):
			a.portal(w, r)
		case r.URL.Path == "/portal/federation/credentials":
			if !strings.HasPrefix(r.Header.Get("X-Amz-Sso_bearer_token"), "sso-access-") || r.URL.Query().Get("account_id") != "123456789012" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"Session token not found or invalid"}`)

				return
			}

			key := "ASIASSO"
			if a.perRoleKeys {
				key = "ASIA" + strings.ToUpper(r.URL.Query().Get("role_name"))
			}

			fmt.Fprintf(w, `{"roleCredentials":{"accessKeyId":%q,"secretAccessKey":"x","sessionToken":"y","expiration":%d}}`, key, time.Now().Add(time.Hour).UnixMilli())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(a.Close)

	sts, oidc, portal, client := awsSTSEndpoint, awsSSOOIDCEndpoint, awsSSOPortalEndpoint, awsHTTPClient
	awsSTSEndpoint = func(string) string { return a.URL + "/sts" }
	awsSSOOIDCEndpoint = func(string) string { return a.URL + "/oidc" }
	awsSSOPortalEndpoint = func(string) string { return a.URL + "/portal" }
	awsHTTPClient = func() *http.Client { return a.Client() }

	t.Cleanup(func() {
		awsSTSEndpoint, awsSSOOIDCEndpoint, awsSSOPortalEndpoint, awsHTTPClient = sts, oidc, portal, client
	})

	return a
}

func eksKubeconfig(f *fakeKubeAPI, args ...string) string {
	quoted, _ := json.Marshal(append([]string{"--region", "eu-west-3", "eks", "get-token", "--cluster-name", "prod"}, args...))

	return strings.Replace(f.kubeconfigFor(f.URL), "    token: secret-token\n", "    exec:\n      command: aws\n      args: "+string(quoted)+"\n", 1)
}

// presignedFrom reads the URL inside the bearer the API server got last.
func presignedFrom(t *testing.T, f *fakeKubeAPI) *url.URL {
	t.Helper()

	r := f.recorded()
	auth := r[len(r)-1].auth

	raw, ok := strings.CutPrefix(auth, "Bearer "+eksTokenPrefix)
	if !ok {
		t.Fatalf("bearer %q", auth)
	}

	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(string(data))
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func TestEKSWithKeysAndRole(t *testing.T) {
	withAuthStore(t)
	newFakeAWS(t)

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api, "--role-arn", "arn:aws:iam::123456789012:role/viewer"), "")
	if err != nil {
		t.Fatal(err)
	}

	info, _ := KubeSignInInfo(stored, "admin@test")
	if !strings.Contains(info, `"kind":"credentials"`) || !strings.Contains(info, awsFieldAccessKey) {
		t.Fatalf("info %s", info)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"awsAccessKeyId":"AKIDBASE","awsSecretAccessKey":"secret"}`); err != nil {
		t.Fatal(err)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); strings.Contains(info, `"values"`) || strings.Contains(info, "AKIDBASE") {
		t.Fatalf("access keys shown again: %s", info)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	u := presignedFrom(t, api)
	q := u.Query()

	if q.Get("Action") != "GetCallerIdentity" || !strings.HasPrefix(q.Get("X-Amz-Credential"), "ASIAROLE/") ||
		q.Get("X-Amz-SignedHeaders") != "host;x-k8s-aws-id" || q.Get("X-Amz-Expires") != "60" || q.Get("X-Amz-Security-Token") != "tok" {
		t.Fatalf("presigned %s", u)
	}
}

func TestEKSWithIdentityCenter(t *testing.T) {
	store := withAuthStore(t)
	aws := newFakeAWS(t)

	old := deviceMinInterval
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = old })

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api), "")
	if err != nil {
		t.Fatal(err)
	}

	err = KubeSetCredentials(stored, "admin@test", `{"awsSsoStartUrl":"https://acme.awsapps.com/start","awsSsoRegion":"eu-west-1","awsAccountId":"123456789012","awsRoleName":"ReadOnly"}`)
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	// The fields entered come back to fill the form; signing in again only needs confirming.
	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, `"values":{"awsAccountId":"123456789012","awsRoleName":"ReadOnly","awsSsoRegion":"eu-west-1","awsSsoStartUrl":"https://acme.awsapps.com/start"}`) {
		t.Fatalf("info %s", info)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "admin@test", rec)

	if p := <-rec.prompts; p.Kind != "device" || p.UserCode != "WXYZ-1234" {
		t.Fatalf("prompt %+v", p)
	}

	if msg := <-rec.done; msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if u := presignedFrom(t, api); !strings.HasPrefix(u.Query().Get("X-Amz-Credential"), "ASIASSO/") {
		t.Fatalf("presigned %s", u)
	}

	// The SSO access token and role credentials expire: the refresh token renews them.
	key := stateKeyOf(t, stored, "admin@test")

	var state kubeAuthState
	_ = json.Unmarshal([]byte(store.Load(key)), &state)

	state = state.withSession(map[string]string{"ssoAccessExpiry": "1", "roleExpiry": "1"})
	store.Save(key, mustJSON(t, state))

	kubeAuth.mu.Lock()
	for _, s := range kubeAuth.sources {
		s.invalidate()
	}
	kubeAuth.mu.Unlock()

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	aws.mu.Lock()
	grants := append([]string(nil), aws.grants...)
	aws.mu.Unlock()

	if grants[len(grants)-1] != "refresh_token" {
		t.Fatalf("grants %v", grants)
	}

	if backup, _ := KubeAuthForBackup(store.Load(key)); !strings.Contains(backup, "acme.awsapps.com") || strings.Contains(backup, "sso-refresh") {
		t.Errorf("backup %s", backup)
	}
}

// signInEKS runs the device sign-in of the context and waits for it.
func signInEKS(t *testing.T, stored string) {
	t.Helper()

	old := deviceMinInterval
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = old })

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "admin@test", rec)

	if p := <-rec.prompts; p.Kind != "device" {
		t.Fatalf("prompt %+v", p)
	}

	if msg := <-rec.done; msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}
}

func TestEKSIdentityCenterPicksTheRoleAfterSignIn(t *testing.T) {
	store := withAuthStore(t)
	aws := newFakeAWS(t)

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api), "")
	if err != nil {
		t.Fatal(err)
	}

	start := `"awsSsoStartUrl":"https://example.awsapps.com/start","awsSsoRegion":"eu-west-1"`

	if err := KubeSetCredentials(stored, "admin@test", "{"+start+"}"); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	signInEKS(t, stored)

	var info kubeSignInKind

	raw, _ := KubeSignInInfo(stored, "admin@test")
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}

	want := []awsSSORole{
		{AccountID: "123456789012", AccountName: "Production", RoleName: "Admin"},
		{AccountID: "123456789012", AccountName: "Production", RoleName: "ReadOnly"},
		{AccountID: "210987654321", AccountName: "Staging", RoleName: "ReadOnly"},
	}
	if !info.SignedIn || info.User != "" || fmt.Sprint(info.Choices) != fmt.Sprint(want) {
		t.Fatalf("info %s", raw)
	}

	// Without a role the cluster asks for the sheet again, not for the browser.
	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.Contains(err.Error(), "choose an account and role") {
		t.Fatalf("nodes before the choice: %v", err)
	}

	if err := KubeSetCredentials(stored, "admin@test", "{"+start+`,"awsAccountId":"123456789012","awsRoleName":"ReadOnly"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	aws.mu.Lock()
	devices := aws.devices
	aws.mu.Unlock()

	if devices != 1 {
		t.Errorf("device codes asked: %d", devices)
	}

	info = kubeSignInKind{}

	raw, _ = KubeSignInInfo(stored, "admin@test")
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}

	if info.User != "ReadOnly @ Production (123456789012)" || len(info.Choices) != 0 {
		t.Fatalf("info after the choice %s", raw)
	}

	if backup, _ := KubeAuthForBackup(store.Load(stateKeyOf(t, stored, "admin@test"))); strings.Contains(backup, "Staging") {
		t.Errorf("backup keeps the role list: %s", backup)
	}
}

func TestEKSIdentityCenterSingleRolePicksItself(t *testing.T) {
	withAuthStore(t)
	aws := newFakeAWS(t)
	aws.singleRole = true

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api), "")
	if err != nil {
		t.Fatal(err)
	}

	_ = KubeSetCredentials(stored, "admin@test", `{"awsSsoStartUrl":"https://example.awsapps.com/start","awsSsoRegion":"eu-west-1"}`)
	signInEKS(t, stored)

	raw, _ := KubeSignInInfo(stored, "admin@test")
	if !strings.Contains(raw, `"user":"ReadOnly @ Production (123456789012)"`) || strings.Contains(raw, `"choices"`) ||
		!strings.Contains(raw, `"awsAccountId":"123456789012"`) {
		t.Fatalf("info %s", raw)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}
}

func TestEKSIdentityCenterNeedsStartURLAndRegion(t *testing.T) {
	m := &eksMethod{}

	tests := []struct {
		name    string
		secrets map[string]string
		wantErr string
	}{
		{"start URL and region", map[string]string{awsFieldSSOStartURL: "https://example.awsapps.com/start", awsFieldSSORegion: "eu-west-1"}, ""},
		{"all four", map[string]string{awsFieldSSOStartURL: "https://example.awsapps.com/start", awsFieldSSORegion: "eu-west-1", awsFieldAccountID: "123456789012", awsFieldRoleName: "ReadOnly"}, ""},
		{"no region", map[string]string{awsFieldSSOStartURL: "https://example.awsapps.com/start"}, "awsSsoRegion"},
		{"http", map[string]string{awsFieldSSOStartURL: "http://example.awsapps.com/start", awsFieldSSORegion: "eu-west-1"}, "https"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := m.fromSecrets(tt.secrets)
			if tt.wantErr == "" {
				if err != nil || len(state.Secrets) != len(tt.secrets) {
					t.Fatalf("state %+v, err %v", state, err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestEKSSSORolesExpiredToken(t *testing.T) {
	newFakeAWS(t)

	m := &eksMethod{http: awsHTTPClient()}

	_, err := m.ssoRoles(t.Context(), "expired", "eu-west-1")

	var need *errSignInRequired
	if !errors.As(err, &need) {
		t.Fatalf("err %v", err)
	}
}
