package ichorgo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const teleportTestContext = "admin@test"

// fakeTeleport plays a Teleport proxy's web API and the identity provider behind it.
type fakeTeleport struct {
	*httptest.Server

	t    *testing.T
	ping string

	mu      sync.Mutex
	request map[string]any
	pubKey  any
	// postForm makes the console answer a SAML form instead of a redirect URL.
	postForm bool
}

func newFakeTeleport(t *testing.T, ping string) *fakeTeleport {
	t.Helper()

	f := &fakeTeleport{t: t, ping: ping}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /webapi/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, f.ping) })
	mux.HandleFunc("POST /webapi/{kind}/login/console", f.console)
	f.Server = httptest.NewTLSServer(mux)
	t.Cleanup(f.Close)

	client := teleportHTTPClient
	teleportHTTPClient = func(*tls.Config) *http.Client {
		c := f.Client()
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

		return c
	}

	t.Cleanup(func() { teleportHTTPClient = client })

	return f
}

var teleportCallbackPattern = regexp.MustCompile(`^http://127\.0\.0\.1:\d+/callback\?secret_key=[0-9a-f]{64}$`)

func (f *fakeTeleport) console(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	_ = json.NewDecoder(r.Body).Decode(&req)

	var typed teleportConsoleRequest

	raw, _ := json.Marshal(req)
	_ = json.Unmarshal(raw, &typed)

	block, _ := pem.Decode(typed.TLSPubKey)
	if block == nil || block.Type != "PUBLIC KEY" {
		f.t.Errorf("tls_pub_key %q", typed.TLSPubKey)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		f.t.Error(err)
	}

	if !teleportCallbackPattern.MatchString(typed.RedirectURL) || typed.CertTTL != teleportCertTTL ||
		req["KubernetesCluster"] != "sample-kube" || req["RouteToCluster"] != "teleport.example.org" ||
		typed.ConnectorID != "sample-github" || r.PathValue("kind") != "github" {
		f.t.Errorf("console request %s on %s", raw, r.URL.Path)
	}

	f.mu.Lock()
	f.request, f.pubKey = req, pub
	f.mu.Unlock()

	if f.postForm {
		_ = json.NewEncoder(w).Encode(map[string]string{"post_form": "<form>sample</form>"})

		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"redirect_url": f.URL + "/idp"})
}

// issue is the certificate the proxy would issue for pub, valid until notAfter.
func (f *fakeTeleport) issue(pub any, notAfter time.Time) []byte {
	ca, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "sample-user"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: notAfter,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, ca)
	if err != nil {
		f.t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// teleportSeal is Teleport's secret.Key.Seal: AES-256-GCM, the key hex in the callback URL.
func teleportSeal(t *testing.T, keyHex string, plain []byte) string {
	t.Helper()

	key, _ := hex.DecodeString(keyHex)
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)

	out, _ := json.Marshal(teleportSealed{Ciphertext: gcm.Seal(nil, nonce, plain, nil), Nonce: nonce})

	return string(out)
}

// teleportBrowser plays the browser: the proxy redirects it to the callback with the login,
// sealed with the secret key (or another one, wrongKey) and for pub (nil: the request's).
type teleportBrowser struct {
	notAfter time.Time
	wrongKey bool
	otherKey bool
}

func (b teleportBrowser) follow(t *testing.T, f *fakeTeleport) func(string) {
	return func(page string) {
		if strings.HasSuffix(page, "/saml") {
			resp, err := http.Get(page) //nolint:noctx
			if err != nil {
				t.Error(err)

				return
			}

			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if string(body) != "<form>sample</form>" {
				t.Errorf("saml page %q", body)
			}
		} else if page != f.URL+"/idp" {
			t.Errorf("browser sent to %q", page)
		}

		f.mu.Lock()
		redirect, _ := f.request["redirect_url"].(string)
		pub := f.pubKey
		f.mu.Unlock()

		if b.otherKey {
			other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			pub = &other.PublicKey
		}

		u, _ := url.Parse(redirect)
		key := u.Query().Get("secret_key")

		if b.wrongKey {
			key = strings.Repeat("ab", 32)
		}

		login, _ := json.Marshal(map[string]any{"username": "sample-user", "tls_cert": f.issue(pub, b.notAfter), "cert": []byte("ssh")})

		resp, err := http.Get(redirect + "&response=" + url.QueryEscape(teleportSeal(t, key, login))) //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_ = resp.Body.Close()
	}
}

const teleportGitHubPing = `{"auth":{"type":"github","github":{"name":"sample-github"}},"server_version":"18.1.0","cluster_name":"teleport.example.org"}`

func teleportKubeconfig(t *testing.T, f *fakeTeleport, extra ...string) string {
	t.Helper()

	api := newFakeKubeAPI(t, nil)
	args := append([]string{"kube", "credentials", "--kube-cluster=sample-kube", "--teleport-cluster=teleport.example.org",
		"--proxy=" + strings.TrimPrefix(f.URL, "https://")}, extra...)

	return execKubeconfig(api, "tsh", args...)
}

// teleportSignIn runs the sign-in to the end and returns OnDone's message.
func teleportSignIn(t *testing.T, cfg string, b teleportBrowser, f *fakeTeleport) string {
	t.Helper()

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1), onURL: b.follow(t, f)}
	StartKubeSignIn(cfg, teleportTestContext, rec)

	select {
	case err := <-rec.done:
		return err
	case <-time.After(20 * time.Second):
		t.Fatal("sign-in did not finish")
	}

	return ""
}

func TestTeleportSignIn(t *testing.T) {
	cases := map[string]struct {
		ping     string
		postForm bool
		browser  teleportBrowser
		wantErr  string
	}{
		"github":           {ping: teleportGitHubPing, browser: teleportBrowser{notAfter: time.Now().Add(12 * time.Hour)}},
		"saml post form":   {ping: teleportGitHubPing, postForm: true, browser: teleportBrowser{notAfter: time.Now().Add(time.Hour)}},
		"local users only": {ping: `{"auth":{"type":"local","local":{"name":""}}}`, wantErr: "tctl auth sign"},
		"wrong secret key": {ping: teleportGitHubPing, browser: teleportBrowser{notAfter: time.Now().Add(time.Hour), wrongKey: true}, wantErr: "not for this sign-in"},
		"another key":      {ping: teleportGitHubPing, browser: teleportBrowser{notAfter: time.Now().Add(time.Hour), otherKey: true}, wantErr: "not for this sign-in's key"},
		"expired":          {ping: teleportGitHubPing, browser: teleportBrowser{notAfter: time.Now().Add(30 * time.Second)}, wantErr: "already expired"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := withAuthStore(t)
			f := newFakeTeleport(t, c.ping)
			f.postForm = c.postForm
			cfg := teleportKubeconfig(t, f)

			err := teleportSignIn(t, cfg, c.browser, f)
			if c.wantErr != "" {
				if !strings.Contains(err, c.wantErr) {
					t.Fatalf("error %q, want %q", err, c.wantErr)
				}

				if len(store.states) != 0 {
					t.Errorf("a failed sign-in stored %v", store.states)
				}

				return
			}

			if err != "" {
				t.Fatal(err)
			}

			checkTeleportSignedIn(t, cfg, c.browser.notAfter)
		})
	}
}

func checkTeleportSignedIn(t *testing.T, cfg string, notAfter time.Time) {
	t.Helper()

	sc, err := signInContext(cfg, teleportTestContext)
	if err != nil {
		t.Fatal(err)
	}

	state := kubeAuth.load(sc.key)
	if state.Method != authTeleport || state.User != "sample-user" || state.Expires != notAfter.Unix() ||
		state.SessionExpires != notAfter.Unix() || state.secret(teleportConnector) != "sample-github" {
		t.Errorf("state method %q user %q expires %d/%d, want %d", state.Method, state.User, state.Expires, state.SessionExpires, notAfter.Unix())
	}

	bundle, expiry, _, err := sc.method.mint(t.Context(), state)
	if err != nil || expiry.Unix() != notAfter.Unix() {
		t.Fatalf("mint: expiry %v, %v", expiry, err)
	}

	if _, err := tls.X509KeyPair([]byte(bundle), []byte(bundle)); err != nil {
		t.Errorf("the bundle is no certificate and key: %v", err)
	}

	info, err := KubeSignInInfo(cfg, teleportTestContext)
	if err != nil || !strings.Contains(info, `"method":"teleport"`) || !strings.Contains(info, `"kind":"browser"`) ||
		!strings.Contains(info, `"signedIn":true`) || !strings.Contains(info, `"kubeCluster":"sample-kube"`) ||
		!strings.Contains(info, `"teleportConnector":"sample-github"`) {
		t.Errorf("info %s, %v", info, err)
	}

	raw, _ := json.Marshal(state)

	backup, err := KubeAuthForBackup(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	for _, out := range []string{info, backup} {
		if strings.Contains(out, "BEGIN") || strings.Contains(out, state.session(teleportSessionKey)[40:80]) {
			t.Errorf("certificate or key material in %s", out)
		}
	}

	if err := KubeSignOut(cfg, teleportTestContext); err != nil {
		t.Fatal(err)
	}

	if left := kubeAuth.load(sc.key); left.Method != "" {
		t.Errorf("signed out, still stored: %+v", left)
	}
}

func TestTeleportMintAndRenewal(t *testing.T) {
	withAuthStore(t)

	f := newFakeTeleport(t, teleportGitHubPing)
	cfg := teleportKubeconfig(t, f)

	if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: time.Now().Add(time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	report, err := KubeRenewSignIns(cfg, "")
	if err != nil || !strings.Contains(report, `"status":"valid"`) || strings.Contains(report, "BEGIN") {
		t.Fatalf("fresh certificate: %s, %v", report, err)
	}

	// The certificate ends: the stored one is past its margin.
	sc, _ := signInContext(cfg, teleportTestContext)
	state := kubeAuth.load(sc.key)

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	expired := state.withSession(map[string]string{
		teleportSessionCert: string(f.issue(&key.PublicKey, time.Now().Add(30*time.Second))),
		teleportSessionKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	kubeAuth.forget(sc.key)
	kubeAuth.save(sc.key, expired)

	report, err = KubeRenewSignIns(cfg, "")
	if err != nil || !strings.Contains(report, `"status":"signInRequired"`) || !strings.Contains(report, "Teleport certificate expired") ||
		strings.Contains(report, "BEGIN") {
		t.Fatalf("expired certificate: %s, %v", report, err)
	}
}

func TestTeleportNeverReachesKubernetesYet(t *testing.T) {
	withAuthStore(t)

	f := newFakeTeleport(t, teleportGitHubPing)
	cfg := teleportKubeconfig(t, f)

	if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: time.Now().Add(time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	// The certificate and key are not a bearer token: the client refuses rather than send them.
	_, err := KubeNodes(cfg, teleportTestContext, "")
	if err == nil || !strings.Contains(err.Error(), "later version") || strings.Contains(err.Error(), "BEGIN") {
		t.Fatalf("nodes: %v", err)
	}
}

func TestTeleportImportWaitsForTheClient(t *testing.T) {
	f := newFakeTeleport(t, teleportGitHubPing)

	s := parseKubeSummary(t, teleportKubeconfig(t, f, "--insecure"))
	for _, c := range s.Contexts {
		if c.Name != teleportTestContext {
			continue
		}

		if c.Auth != authTeleport || c.AuthDetail != "sample-kube" || c.Problem != KubeProblemSignInLater || c.ExecHint != "" || !c.Insecure {
			t.Errorf("tsh context %+v", c)
		}
	}
}

func TestTeleportProxyAddr(t *testing.T) {
	cases := map[string]string{
		"proxy.example.org":          "proxy.example.org:443",
		"proxy.example.org:3080":     "proxy.example.org:3080",
		"proxy.example.org:443,3023": "proxy.example.org:443",
		"https://proxy.example.org/": "proxy.example.org:443",
		"":                           "",
		"proxy.example.org/path":     "",
		"user@proxy.example.org":     "",
	}

	for in, want := range cases {
		got, err := teleportProxyAddr(in)
		if got != want || (want == "") != (err != nil) {
			t.Errorf("teleportProxyAddr(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestTeleportPingConnector(t *testing.T) {
	cases := map[string]struct{ ping, kind, name string }{
		"github":  {`{"auth":{"type":"github","github":{"name":"gh"}}}`, "github", "gh"},
		"oidc":    {`{"auth":{"type":"oidc","oidc":{"name":"okta"}}}`, "oidc", "okta"},
		"saml":    {`{"auth":{"type":"saml","saml":{"name":"adfs"}}}`, "saml", "adfs"},
		"local":   {`{"auth":{"type":"local"}}`, "", ""},
		"unnamed": {`{"auth":{"type":"oidc"}}`, "", ""},
	}

	for name, c := range cases {
		var p teleportPingAnswer
		_ = json.Unmarshal([]byte(c.ping), &p)

		kind, connector, err := p.connector()
		if kind != c.kind || connector != c.name || (c.kind == "") != (err != nil) {
			t.Errorf("%s: %q %q %v", name, kind, connector, err)
		}
	}

	for version, rsa := range map[string]bool{"16.4.2": true, "v17.0.0": false, "18.1.0": false, "": false} {
		if got := (teleportPingAnswer{ServerVersion: version}).useRSA(); got != rsa {
			t.Errorf("useRSA(%q) = %v", version, got)
		}
	}
}
