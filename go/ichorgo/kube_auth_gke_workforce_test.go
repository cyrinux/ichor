package ichorgo

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSTS is Google STS for workforce identity sessions: the refresh token grant with the
// client in a Basic header (never in the form).
type fakeSTS struct {
	mu      sync.Mutex
	refresh string
	rotate  string
	grants  int
}

func (s *fakeSTS) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = r.ParseForm()
	user, pass, basic := r.BasicAuth()

	switch {
	case r.URL.Path != "/v1/oauthtoken":
		http.NotFound(w, r)
	case !basic || user != "wf-client" || pass != "wf-secret" || r.Form.Get("client_id") != "" || r.Form.Get("client_secret") != "":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
	case r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != s.refresh:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"The refresh token is expired."}`)
	default:
		s.grants++
		answer := map[string]any{"access_token": "ya29.workforce", "expires_in": 3600}

		if s.rotate != "" {
			answer["refresh_token"] = s.rotate
			s.refresh, s.rotate = s.rotate, ""
		}

		_ = json.NewEncoder(w).Encode(answer)
	}
}

func workforceJSON(tokenURL, refresh string) string {
	raw, _ := json.Marshal(map[string]string{
		"type":           "external_account_authorized_user",
		"audience":       "//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp",
		"client_id":      "wf-client",
		"client_secret":  "wf-secret",
		"refresh_token":  refresh,
		"token_url":      tokenURL,
		"token_info_url": "https://sts.googleapis.com/v1/introspect",
	})

	return string(raw)
}

func TestGKEWorkforceSession(t *testing.T) {
	store := withAuthStore(t)
	sts := &fakeSTS{refresh: "wf-rt-1", rotate: "wf-rt-2"}
	srv := withCloudServer(t, sts.handle)

	stored, api := gkeUserCluster(t)

	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldUserCredentials, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-1"))); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
			t.Fatal(err)
		}
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.workforce" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// The sign-in check only: the token is reused while it lasts.
	if sts.grants != 1 {
		t.Errorf("%d grants", sts.grants)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "sample-pool (workforce identity)") {
		t.Errorf("the pool is not shown: %s", info)
	}

	// The rotated refresh token is the one stored, sent next and kept by backups.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, "wf-rt-2") || strings.Contains(backup, "ya29.workforce") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Expired at STS: sign in again, with the login-config command.
	sts.mu.Lock()
	sts.refresh = "something-else"
	sts.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "--login-config") {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestGKEWorkforceRefusals(t *testing.T) {
	withAuthStore(t)
	sts := &fakeSTS{refresh: "wf-rt-1"}
	srv := withCloudServer(t, sts.handle)

	stored, _ := gkeUserCluster(t)

	external, _ := json.Marshal(map[string]any{
		"type": "external_account", "audience": "//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp",
		"credential_source": map[string]string{"file": "/tmp/token"},
	})

	for _, tc := range []struct{ name, field, raw, want string }{
		{"no token_url", gcpFieldUserCredentials, workforceJSON("", "wf-rt-1"), "not a workforce identity session"},
		{"plain http token_url", gcpFieldUserCredentials, workforceJSON("http://sts.example.com/v1/oauthtoken", "wf-rt-1"), "not https"},
		{"login config", gcpFieldUserCredentials, string(external), "--login-config"},
		{"session as service account", gcpFieldServiceAccount, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-1"), "choose gcloud user credentials"},
		{"expired at sign-in", gcpFieldUserCredentials, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-old"), "--login-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := KubeSetCredentials(stored, "admin@test", userSecrets(tc.field, tc.raw)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkforceLabel(t *testing.T) {
	for audience, want := range map[string]string{
		"//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp": "sample-pool (workforce identity)",
		"": "Workforce identity",
		"//iam.googleapis.com/locations/global/workforcePools/": "Workforce identity",
	} {
		if got := workforceLabel(audience); got != want {
			t.Errorf("%q: %q, want %q", audience, got, want)
		}
	}
}

const samplePoolAudience = "//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp"

// fakeWorkforce is Google STS for a login config's sign-in: the authorization code and then
// the refresh token grants, each with the client in a Basic header (never in the form).
type fakeWorkforce struct {
	mu             sync.Mutex
	client, secret string
	refresh        string
	rotate         string
	grants         []string
}

func (s *fakeWorkforce) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = r.ParseForm()
	user, pass, basic := r.BasicAuth()
	grant := r.Form.Get("grant_type")

	switch {
	case r.URL.Path != "/v1/oauthtoken":
		http.NotFound(w, r)
	case !basic || user != s.client || pass != s.secret || r.Form.Has("client_id") || r.Form.Has("client_secret"):
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
	case grant == "authorization_code" && r.Form.Get("code") == "good-code" && r.Form.Get("code_verifier") != "" &&
		strings.HasPrefix(r.Form.Get("redirect_uri"), "http://localhost:"):
		s.grants = append(s.grants, grant)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29.wf-code", "refresh_token": s.refresh, "expires_in": 3600})
	case grant == "refresh_token" && r.Form.Get("refresh_token") == s.refresh:
		s.grants = append(s.grants, grant)
		answer := map[string]any{"access_token": "ya29.wf-refresh", "expires_in": 3600}

		if s.rotate != "" {
			answer["refresh_token"] = s.rotate
			s.refresh, s.rotate = s.rotate, ""
		}

		_ = json.NewEncoder(w).Encode(answer)
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"The refresh token is expired."}`)
	}
}

func (s *fakeWorkforce) grantList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.grants...)
}

// withFakeWorkforce serves f as STS and trusts it for the sign-in; it returns its token URL.
func withFakeWorkforce(t *testing.T, f *fakeWorkforce) string {
	t.Helper()

	srv := withCloudServer(t, f.handle)

	tlsConfig := gcpOAuthTLS
	gcpOAuthTLS = func() *tls.Config {
		return &tls.Config{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	}

	t.Cleanup(func() { gcpOAuthTLS = tlsConfig })

	return srv.URL + "/v1/oauthtoken"
}

// withGcloudCLIClient replaces gcloud's CLI client by a test one.
func withGcloudCLIClient(t *testing.T) {
	t.Helper()

	client := gcloudCLIClient
	gcloudCLIClient = func() (string, string) { return "cli-client.apps.example.com", "cli-secret" }

	t.Cleanup(func() { gcloudCLIClient = client })
}

func loginConfigJSON(tokenURL string, extra map[string]string) string {
	cfg := map[string]string{
		"type": gcpTypeLoginConfig, "audience": samplePoolAudience, "universe_domain": "googleapis.com",
		"auth_url": "https://auth.example.com/authorize", "token_url": tokenURL, "token_info_url": "https://sts.example.com/v1/introspect",
	}
	maps.Copy(cfg, extra)

	for k, v := range cfg {
		if v == "-" {
			delete(cfg, k)
		}
	}

	raw, _ := json.Marshal(cfg)

	return string(raw)
}

// workforceBrowserFollow plays Google's workforce page: it checks the request is gcloud's
// and goes back to the loopback redirect with a good code.
func workforceBrowserFollow(t *testing.T, client string) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)

			return
		}

		q := u.Query()
		if u.Host != "auth.example.com" || q.Get("client_id") != client || q.Get("scope") != gcpScope || q.Has("nonce") || q.Has("prompt") ||
			q.Get("access_type") != "offline" || q.Get("provider_name") != "locations/global/workforcePools/sample-pool/providers/sample-idp" ||
			q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || !strings.HasPrefix(q.Get("redirect_uri"), "http://localhost:") {
			t.Errorf("auth request %s", authURL)
		}

		resp, err := http.Get(q.Get("redirect_uri") + "?code=good-code&state=" + url.QueryEscape(q.Get("state"))) //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_ = resp.Body.Close()
	}
}

func workforceSignIn(t *testing.T, stored, client string) {
	t.Helper()

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1), onURL: workforceBrowserFollow(t, client)}
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
}

type signInInfoValues struct {
	Options  [][]string        `json:"options"`
	Values   map[string]string `json:"values"`
	User     string            `json:"user"`
	SignedIn bool              `json:"signedIn"`
}

func readSignInInfo(t *testing.T, stored string) signInInfoValues {
	t.Helper()

	raw, err := KubeSignInInfo(stored, "admin@test")
	if err != nil {
		t.Fatal(err)
	}

	var info signInInfoValues
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}

	return info
}

func TestGKEWorkforceLoginConfig(t *testing.T) {
	store := withAuthStore(t)
	sts := &fakeWorkforce{client: "wf-client", secret: "wf-secret", refresh: "wf-rt-1", rotate: "wf-rt-2"}
	tokenURL := withFakeWorkforce(t, sts)
	config := loginConfigJSON(tokenURL, map[string]string{"client_id": "wf-client", "client_secret": "wf-secret"})

	stored, api := gkeUserCluster(t)

	if info := readSignInInfo(t, stored); !slices.ContainsFunc(info.Options, func(set []string) bool { return slices.Equal(set, []string{gcpFieldLoginConfig}) }) {
		t.Fatalf("options %v", info.Options)
	}

	// The config alone asks for the browser.
	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldLoginConfig, config)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("set credentials: %v", err)
	}

	workforceSignIn(t, stored, "wf-client")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.wf-code" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// Stored as a workforce identity session, the shape an imported one has.
	state, creds := storedGcloudState(t, store)
	if creds.Type != gcpTypeWorkforceUser || creds.Audience != samplePoolAudience || creds.ClientID != "wf-client" || creds.ClientSecret != "wf-secret" ||
		creds.RefreshToken != "wf-rt-1" || creds.TokenURL != tokenURL || creds.TokenInfoURL != "https://sts.example.com/v1/introspect" ||
		state.session("refresh") != "" || state.secret(gcpFieldLoginConfig) != config {
		t.Fatalf("stored %+v %+v", state, creds)
	}

	if info := readSignInInfo(t, stored); !info.SignedIn || info.User != "sample-pool (workforce identity)" || info.Values[gcpFieldLoginConfig] != config {
		t.Errorf("info after sign-in %+v", info)
	}

	// Expired: the STS refresh renews it, no browser; the rotated refresh token is kept.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.wf-refresh" {
		t.Fatalf("bearer after refresh %q", r[len(r)-1].auth)
	}

	if _, creds = storedGcloudState(t, store); creds.RefreshToken != "wf-rt-2" || !slices.Equal(sts.grantList(), []string{"authorization_code", "refresh_token"}) {
		t.Fatalf("after refresh %+v, grants %v", creds, sts.grantList())
	}

	// Backups keep the config and the refresh token, not the access token.
	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, gcpFieldLoginConfig) || !strings.Contains(backup, "wf-rt-2") || strings.Contains(backup, "ya29.") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Refused at STS: sign in again in the browser, no gcloud command; the config is prefilled.
	sts.mu.Lock()
	sts.refresh = "revoked"
	sts.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	_, err := KubeNodes(stored, "admin@test", "")
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), gcpWorkforceSignInHint) || strings.Contains(err.Error(), "gcloud") {
		t.Fatalf("after revocation: %v", err)
	}

	info := readSignInInfo(t, stored)
	if info.Values[gcpFieldLoginConfig] != config {
		t.Fatalf("info after revocation %+v", info)
	}

	sts.mu.Lock()
	sts.refresh = "wf-rt-3"
	sts.mu.Unlock()

	values, _ := json.Marshal(info.Values)
	if err := KubeSetCredentials(stored, "admin@test", string(values)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("sign in again: %v", err)
	}

	workforceSignIn(t, stored, "wf-client")

	if _, creds = storedGcloudState(t, store); creds.RefreshToken != "wf-rt-3" {
		t.Errorf("after a new sign-in %+v", creds)
	}
}

// A login config names no client (gcloud's own do not): gcloud's CLI client signs in; one
// with a client and no secret sends the client with an empty password.
func TestGKEWorkforceLoginConfigClients(t *testing.T) {
	for _, tc := range []struct {
		name, id, secret, wantID, wantSecret string
	}{
		{"gcloud's client", "-", "-", "cli-client.apps.example.com", "cli-secret"},
		{"client without secret", "org-client", "-", "org-client", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := withAuthStore(t)
			withGcloudCLIClient(t)

			sts := &fakeWorkforce{client: tc.wantID, secret: tc.wantSecret, refresh: "wf-rt-1"}
			tokenURL := withFakeWorkforce(t, sts)
			stored, api := gkeUserCluster(t)

			config := loginConfigJSON(tokenURL, map[string]string{"client_id": tc.id, "client_secret": tc.secret})
			if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldLoginConfig, config)); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
				t.Fatalf("set credentials: %v", err)
			}

			workforceSignIn(t, stored, tc.wantID)

			if _, creds := storedGcloudState(t, store); creds.ClientID != tc.wantID || creds.ClientSecret != tc.wantSecret {
				t.Fatalf("stored %+v", creds)
			}

			forgetTokens(t, store, stored, "admin@test")

			if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
				t.Fatal(err)
			}

			if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.wf-refresh" {
				t.Fatalf("bearer after refresh %q", r[len(r)-1].auth)
			}
		})
	}
}

func TestGKEWorkforceLoginConfigRefusals(t *testing.T) {
	withAuthStore(t)

	stored, _ := gkeUserCluster(t)
	tokenURL := "https://sts.example.com/v1/oauthtoken"

	for _, tc := range []struct{ name, field, raw, want string }{
		{"not JSON", gcpFieldLoginConfig, "{", "not JSON"},
		{"wrong type", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{"type": "service_account"}), "not a workforce login config"},
		{"a session", gcpFieldLoginConfig, workforceJSON(tokenURL, "rt"), "choose gcloud user credentials"},
		{"no audience", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{"audience": "-"}), "no workforce pool provider"},
		{"workload pool", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{
			"audience": "//iam.googleapis.com/projects/1/locations/global/workloadIdentityPools/sample-pool/providers/sample-idp",
		}), "no workforce pool provider"},
		{"no auth_url", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{"auth_url": "-"}), "auth_url is not an https"},
		{"plain http auth_url", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{"auth_url": "http://auth.example.com/authorize"}), "auth_url is not an https"},
		{"no token_url", gcpFieldLoginConfig, loginConfigJSON("", nil), "token_url is not an https"},
		{"secret without client", gcpFieldLoginConfig, loginConfigJSON(tokenURL, map[string]string{"client_secret": "s"}), "client_secret without its client_id"},
		{"config as user credentials", gcpFieldUserCredentials, loginConfigJSON(tokenURL, nil), "choose Workforce identity (login config)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := KubeSetCredentials(stored, "admin@test", userSecrets(tc.field, tc.raw)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGKEWorkforceLoginConfigState(t *testing.T) {
	config := loginConfigJSON("https://sts.example.com/v1/oauthtoken", nil)
	session := workforceJSON("https://sts.example.com/v1/oauthtoken", "rt")

	other, _ := json.Marshal(map[string]string{
		"type": gcpTypeWorkforceUser, "audience": "//iam.googleapis.com/locations/global/workforcePools/other-pool/providers/sample-idp",
		"client_id": "c", "refresh_token": "rt", "token_url": "https://sts.example.com/v1/oauthtoken",
	})

	for _, tc := range []struct {
		name    string
		secrets map[string]string
		keeps   bool
	}{
		{"config only", map[string]string{gcpFieldLoginConfig: config}, false},
		{"with its session", map[string]string{gcpFieldLoginConfig: config, gcpFieldUserCredentials: session}, true},
		{"another pool's session", map[string]string{gcpFieldLoginConfig: config, gcpFieldUserCredentials: string(other)}, false},
		{"a Google account's credentials", map[string]string{gcpFieldLoginConfig: config, gcpFieldUserCredentials: adcJSON(gcpTypeAuthorizedUser, "rt")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := gkeMethod{}.fromSecrets(tc.secrets)
			if err != nil || state.secret(gcpFieldLoginConfig) != config || (state.secret(gcpFieldUserCredentials) != "") != tc.keeps {
				t.Fatalf("state %+v (%v)", state, err)
			}
		})
	}

	// Discovery signs in in the browser first and hands its session to the same config.
	if !gkeNeedsBrowser(map[string]string{gcpFieldLoginConfig: config}) {
		t.Error("discovery does not sign in first")
	}

	signed := kubeAuthState{Secrets: map[string]string{gcpFieldLoginConfig: config, gcpFieldUserCredentials: session}}
	entered := kubeAuthState{Secrets: map[string]string{gcpFieldLoginConfig: config}}
	another := kubeAuthState{Secrets: map[string]string{gcpFieldLoginConfig: loginConfigJSON("https://sts.example.com/v2", nil)}}

	if !gcloudSignedFor(signed, entered) || gcloudSignedFor(signed, another) || gcloudSignedFor(entered, entered) {
		t.Error("discovery session matching")
	}
}
