package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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

// fakeIdP is an OIDC provider: discovery, token endpoint (code, refresh, device code).
type fakeIdP struct {
	*httptest.Server

	mu        sync.Mutex
	grants    []string
	refresh   string // the valid refresh token
	idExpiry  time.Duration
	nonce     string
	pending   int // device polls answered authorization_pending first
	issued    int
	challenge string
}

func idToken(claims map[string]any) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(claims)

	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".c2ln"
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()

	idp := &fakeIdP{refresh: "", idExpiry: time.Hour}
	idp.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		defer idp.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			fmt.Fprintf(w, `{"authorization_endpoint":"%[1]s/auth","token_endpoint":"%[1]s/token","device_authorization_endpoint":"%[1]s/device"}`, idp.URL)
		case "/device":
			_ = r.ParseForm()
			_, _ = io.WriteString(w, `{"device_code":"dev-1","user_code":"ABCD-EFGH","verification_uri":"https://idp/activate","expires_in":60,"interval":0}`)
		case "/token":
			_ = r.ParseForm()
			grant := r.Form.Get("grant_type")
			idp.grants = append(idp.grants, grant)

			switch grant {
			case "authorization_code":
				if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
					_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)

					return
				}
			case "refresh_token":
				if r.Form.Get("refresh_token") != idp.refresh {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Token is not active"}`)

					return
				}
			case "urn:ietf:params:oauth:grant-type:device_code":
				if idp.pending > 0 {
					idp.pending--
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)

					return
				}
			}

			idp.issued++
			idp.refresh = fmt.Sprintf("refresh-%d", idp.issued)
			id := idToken(map[string]any{"sub": "u1", "email": "alice@example.org", "exp": time.Now().Add(idp.idExpiry).Unix(), "nonce": idp.nonce})
			fmt.Fprintf(w, `{"id_token":%q,"access_token":"at","refresh_token":%q,"expires_in":3600}`, id, idp.refresh)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(idp.Close)

	return idp
}

func (idp *fakeIdP) caData() string {
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: idp.Certificate().Raw}))
}

func (idp *fakeIdP) grantList() []string {
	idp.mu.Lock()
	defer idp.mu.Unlock()

	return append([]string(nil), idp.grants...)
}

// oidcKubeconfig is a kubelogin kubeconfig for the API server f signing in with idp.
func oidcKubeconfig(f *fakeKubeAPI, idp *fakeIdP, extra ...string) string {
	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.Certificate().Raw}))

	args := []string{"oidc-login", "get-token", "--oidc-issuer-url=" + idp.URL, "--oidc-client-id=kube", "--oidc-extra-scope=email",
		"--certificate-authority-data=" + idp.caData(), "--listen-address=127.0.0.1:0", "--oidc-redirect-url-hostname=127.0.0.1"}
	args = append(args, extra...)

	quoted, _ := json.Marshal(args)

	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster: {server: %q, certificate-authority-data: %s}
users:
- name: u
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: kubectl
      args: %s
contexts:
- name: sso
  context: {cluster: c, user: u}
`, f.URL, ca, quoted)
}

type recSignIn struct {
	prompts chan signInPrompt
	done    chan string
	onURL   func(string)
}

func (r recSignIn) OnPrompt(js string) {
	var p signInPrompt
	_ = json.Unmarshal([]byte(js), &p)

	if r.onURL != nil {
		go r.onURL(p.URL)
	}

	r.prompts <- p
}

func (r recSignIn) OnDone(err string) { r.done <- err }

// browserFollow plays the identity provider's login page: it redirects to redirect_uri.
func browserFollow(t *testing.T, code string) func(string) {
	return func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Error(err)

			return
		}

		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || !strings.Contains(q.Get("scope"), "openid") || !strings.Contains(q.Get("scope"), "offline_access") {
			t.Errorf("auth request %v", q)
		}

		back := q.Get("redirect_uri") + "/?code=" + code + "&state=" + url.QueryEscape(q.Get("state"))

		resp, err := http.Get(back) //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_ = resp.Body.Close()
	}
}

func withAuthStore(t *testing.T) *memAuthStore {
	t.Helper()

	store := newMemAuthStore()
	SetAuthStore(store)
	t.Cleanup(func() { SetAuthStore(nil) })

	return store
}

func TestOIDCSignInAndRefresh(t *testing.T) {
	store := withAuthStore(t)
	idp := newFakeIdP(t)

	var bearers []string

	var mu sync.Mutex

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})
	stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp), "")
	if err != nil {
		t.Fatal(err)
	}

	// Before any sign-in: the app is told to sign in, not that the server is unreachable.
	if _, err := KubeNodes(stored, "sso", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Fatalf("before sign-in: %v", err)
	}

	info, err := KubeSignInInfo(stored, "sso")
	if err != nil || !strings.Contains(info, `"kind":"browser"`) || !strings.Contains(info, `"signedIn":false`) {
		t.Fatalf("info %s, %v", info, err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1), onURL: browserFollow(t, "good-code")}
	StartKubeSignIn(stored, "sso", rec)

	select {
	case msg := <-rec.done:
		if msg != "" {
			t.Fatalf("sign-in: %s", msg)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("sign-in did not end")
	}

	if p := <-rec.prompts; p.Kind != "browser" || !strings.HasPrefix(p.RedirectPrefix, "http://127.0.0.1:") {
		t.Errorf("prompt %+v", p)
	}

	if _, err := KubeNodes(stored, "sso", ""); err != nil {
		t.Fatal(err)
	}

	for _, r := range api.recorded() {
		mu.Lock()
		bearers = append(bearers, r.auth)
		mu.Unlock()
	}

	if last := bearers[len(bearers)-1]; !strings.HasPrefix(last, "Bearer ey") {
		t.Fatalf("bearer %q", last)
	}

	info, _ = KubeSignInInfo(stored, "sso")
	if !strings.Contains(info, `"signedIn":true`) || !strings.Contains(info, "alice@example.org") {
		t.Errorf("info after sign-in %s", info)
	}

	// The refresh token is stored as session, never as a secret (backups leave it out).
	var state kubeAuthState

	store.mu.Lock()
	for _, raw := range store.states {
		_ = json.Unmarshal([]byte(raw), &state)
	}
	store.mu.Unlock()

	if state.session("refresh") != "refresh-1" || len(state.Secrets) != 0 {
		t.Fatalf("state %+v", state)
	}

	if backup, _ := KubeAuthForBackup(mustJSON(t, state)); backup != "" {
		t.Errorf("a session-only state is backed up: %s", backup)
	}

	// An ID token about to expire is renewed with the refresh token, which rotates.
	idp.mu.Lock()
	idp.idExpiry = 10 * time.Second
	idp.mu.Unlock()

	kubeAuth.mu.Lock()
	for _, s := range kubeAuth.sources {
		s.invalidate()
	}
	kubeAuth.mu.Unlock()

	expireStoredBearer(t, store)

	if _, err := KubeNodes(stored, "sso", ""); err != nil {
		t.Fatal(err)
	}

	grants := idp.grantList()
	if len(grants) < 2 || grants[0] != "authorization_code" || grants[1] != "refresh_token" {
		t.Fatalf("grants %v", grants)
	}

	// A revoked refresh token: back to "sign in".
	idp.mu.Lock()
	idp.refresh = "revoked"
	idp.mu.Unlock()

	expireStoredBearer(t, store)

	kubeAuth.mu.Lock()
	for _, s := range kubeAuth.sources {
		s.invalidate()
	}
	kubeAuth.mu.Unlock()

	kubeClients.forgetConfig(stored, "sso")

	if _, err := KubeNodes(stored, "sso", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "Token is not active") {
		t.Fatalf("after revocation: %v", err)
	}

	if err := KubeSignOut(stored, "sso"); err != nil {
		t.Fatal(err)
	}

	if left := store.Load(stateKeyOf(t, stored, "sso")); left != "" {
		t.Errorf("sign-out left %s", left)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(out)
}

// expireStoredBearer makes every stored bearer look expired.
func expireStoredBearer(t *testing.T, store *memAuthStore) {
	t.Helper()

	store.mu.Lock()
	defer store.mu.Unlock()

	for key, raw := range store.states {
		var s kubeAuthState
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			t.Fatal(err)
		}

		s.Session["bearerExpiry"] = "1"
		store.states[key] = mustJSON(t, s)
	}
}

func TestOIDCDeviceCode(t *testing.T) {
	withAuthStore(t)

	old := deviceMinInterval
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = old })

	idp := newFakeIdP(t)
	idp.pending = 2

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp, "--grant-type=device-code"), "")
	if err != nil {
		t.Fatal(err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "sso", rec)

	p := <-rec.prompts
	if p.Kind != "device" || p.UserCode != "ABCD-EFGH" || p.VerificationURL != "https://idp/activate" {
		t.Fatalf("prompt %+v", p)
	}

	if msg := <-rec.done; msg != "" {
		t.Fatalf("device sign-in: %s", msg)
	}

	if _, err := KubeNodes(stored, "sso", ""); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCBrowserRefusesWrongState(t *testing.T) {
	withAuthStore(t)

	idp := newFakeIdP(t)
	api := newFakeKubeAPI(t, nil)

	stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp), "")
	if err != nil {
		t.Fatal(err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	rec.onURL = func(authURL string) {
		u, _ := url.Parse(authURL)
		resp, err := http.Get(u.Query().Get("redirect_uri") + "/?code=good-code&state=forged") //nolint:noctx
		if err == nil {
			_ = resp.Body.Close()
		}
	}

	StartKubeSignIn(stored, "sso", rec)

	if msg := <-rec.done; !strings.Contains(msg, "state") {
		t.Fatalf("got %q", msg)
	}
}

func TestOIDCSignInCancel(t *testing.T) {
	withAuthStore(t)

	idp := newFakeIdP(t)
	api := newFakeKubeAPI(t, nil)

	stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp), "")
	if err != nil {
		t.Fatal(err)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	run := StartKubeSignIn(stored, "sso", rec)

	<-rec.prompts
	run.Cancel()

	if msg := <-rec.done; msg != "" {
		t.Fatalf("cancel reported %q", msg)
	}
}

func stateKeyOf(t *testing.T, stored, name string) string {
	t.Helper()

	sc, err := signInContext(stored, name)
	if err != nil {
		t.Fatal(err)
	}

	return sc.key
}
