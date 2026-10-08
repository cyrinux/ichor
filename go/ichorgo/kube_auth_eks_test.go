package ichorgo

import (
	"encoding/base64"
	"encoding/json"
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
		case r.URL.Path == "/portal/federation/credentials":
			if !strings.HasPrefix(r.Header.Get("X-Amz-Sso_bearer_token"), "sso-access-") || r.URL.Query().Get("account_id") != "123456789012" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"Session token not found or invalid"}`)

				return
			}

			fmt.Fprintf(w, `{"roleCredentials":{"accessKeyId":"ASIASSO","secretAccessKey":"x","sessionToken":"y","expiration":%d}}`, time.Now().Add(time.Hour).UnixMilli())
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
