package ichorgo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"golang.org/x/net/websocket"
)

// certAPI is a Kubernetes API server that wants a client certificate, as the Teleport proxy's
// Kubernetes endpoint does: it notes the NotAfter of each certificate it is shown.
type certAPI struct {
	*httptest.Server

	mu   sync.Mutex
	seen []time.Time
	// refuse is the Status the list answers with ("" answers it).
	refuse string
	// rejectAtHandshake refuses every client certificate, as the proxy does an expired one.
	rejectAtHandshake atomic.Bool
	// refuseVersion answers /version as Teleport 18 refuses it: plain text, HTTP 500.
	refuseVersion atomic.Bool
}

func newCertAPI(t *testing.T) *certAPI {
	t.Helper()

	a := &certAPI{}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		a.note(r)

		if a.refuseVersion.Load() {
			http.Error(w, "[00] access denied", http.StatusInternalServerError)

			return
		}

		_, _ = io.WriteString(w, `{"gitVersion":"v1.31.0"}`)
	})
	mux.HandleFunc("/api/v1/nodes", func(w http.ResponseWriter, r *http.Request) {
		a.note(r)

		a.mu.Lock()
		refuse := a.refuse
		a.mu.Unlock()

		if refuse != "" {
			code := http.StatusForbidden
			if strings.Contains(refuse, "not found") {
				code = http.StatusNotFound
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = fmt.Fprintf(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":%q,"code":%d}`, refuse, code)

			return
		}

		_, _ = io.WriteString(w, `{"items":[]}`)
	})
	mux.Handle("/stream", websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			a.note(r)
			cfg.Protocol = cfg.Protocol[:1]

			return nil
		},
		Handler: func(ws *websocket.Conn) { _, _ = io.Copy(ws, ws) },
	})

	a.Server = httptest.NewUnstartedServer(mux)
	a.TLS = &tls.Config{
		ClientAuth: tls.RequireAnyClientCert,
		VerifyPeerCertificate: func([][]byte, [][]*x509.Certificate) error {
			if a.rejectAtHandshake.Load() {
				return errors.New("certificate expired")
			}

			return nil
		},
	}
	a.StartTLS()
	t.Cleanup(a.Close)

	return a
}

func (a *certAPI) note(r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		a.seen = append(a.seen, r.TLS.PeerCertificates[0].NotAfter)
	}
}

// last is the NotAfter of the last certificate shown; zero when none was.
func (a *certAPI) last() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.seen) == 0 {
		return time.Time{}
	}

	return a.seen[len(a.seen)-1]
}

func (a *certAPI) refuseWith(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.refuse = message
}

// teleportKubeconfigAt is teleportKubeconfig with api as the Kubernetes endpoint, through the
// proxy at proxyHost when set.
func teleportKubeconfigAt(f *fakeTeleport, api *httptest.Server, proxyHost string) string {
	cfg := execKubeconfig(&fakeKubeAPI{Server: api}, "tsh", "kube", "credentials", "--kube-cluster=sample-kube",
		"--teleport-cluster=teleport.example.org", "--proxy="+strings.TrimPrefix(f.URL, "https://"))
	if proxyHost != "" {
		cfg = strings.Replace(cfg, "    server: "+api.URL+"\n", "    server: "+api.URL+"\n    proxy-url: http://"+proxyHost+"\n", 1)
	}

	return cfg
}

func listNodes(cfg string) error {
	_, err := withKube(kubeTarget{config: cfg, context: teleportTestContext}, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, k.get(ctx, "/api/v1/nodes", nil)
	})

	return err
}

// Every connection presents the certificate: the list, an exec or port-forward stream and the
// VM console dial, directly and through a proxy-url.
func TestTeleportReachesKubernetesWithTheCertificate(t *testing.T) {
	for _, proxied := range []bool{false, true} {
		t.Run(fmt.Sprintf("proxied=%v", proxied), func(t *testing.T) {
			withAuthStore(t)

			f := newFakeTeleport(t, teleportGitHubPing)
			api := newCertAPI(t)

			proxyHost := ""
			proxy := &recordingProxy{}

			if proxied {
				front := httptest.NewServer(proxy)
				t.Cleanup(front.Close)

				proxyHost = strings.TrimPrefix(front.URL, "http://")
			}

			cfg := teleportKubeconfigAt(f, api.Server, proxyHost)
			t.Cleanup(func() { kubeClients.forgetConfig(cfg, teleportTestContext) })

			notAfter := time.Now().Add(time.Hour).Truncate(time.Second)
			if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: notAfter}, f); err != "" {
				t.Fatal(err)
			}

			if err := listNodes(cfg); err != nil {
				t.Fatalf("list: %v", err)
			}

			if !api.last().Equal(notAfter) {
				t.Fatalf("the list showed a certificate until %v, want %v", api.last(), notAfter)
			}

			k, _, err := kubeClients.get(kubeTarget{config: cfg, context: teleportTestContext})
			if err != nil {
				t.Fatal(err)
			}

			if k.token != "" || k.tokens != nil || k.certs == nil {
				t.Errorf("a certificate cluster has a bearer: token %q, tokens %v", k.token, k.tokens)
			}

			checkStreamsPresentTheCertificate(t, k, api, notAfter)

			if proxied && proxy.count() < 3 {
				t.Errorf("the proxy saw only %v", proxy.seen)
			}
		})
	}
}

func checkStreamsPresentTheCertificate(t *testing.T, k *kubeClient, api *certAPI, notAfter time.Time) {
	t.Helper()

	before := len(api.seen)

	ws, err := k.dialStream(t.Context(), "/stream")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	_ = ws.Close()

	cfg, err := k.streamConfig(t.Context(), "/stream", nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Header.Get("Authorization") != "" {
		t.Error("the stream sends a bearer token")
	}

	conn, err := k.dialAPIServer(t.Context(), cfg)
	if err != nil {
		t.Fatalf("console dial: %v", err)
	}

	// The console dial is a bare TLS connection: its handshake is what shows the certificate.
	if state := conn.(*tls.Conn).ConnectionState(); !state.HandshakeComplete {
		t.Error("console dial without a handshake")
	}

	_ = conn.Close()

	api.mu.Lock()
	streamed := api.seen[before:]
	api.mu.Unlock()

	if len(streamed) == 0 || !streamed[0].Equal(notAfter) {
		t.Errorf("the stream showed %v, want %v", streamed, notAfter)
	}
}

// A new sign-in's certificate is used by the next call: the old client and its sessions go.
func TestTeleportRenewedCertificateIsUsedNext(t *testing.T) {
	withAuthStore(t)

	f := newFakeTeleport(t, teleportGitHubPing)
	api := newCertAPI(t)
	cfg := teleportKubeconfigAt(f, api.Server, "")
	t.Cleanup(func() { kubeClients.forgetConfig(cfg, teleportTestContext) })

	for _, ttl := range []time.Duration{time.Hour, 2 * time.Hour} {
		notAfter := time.Now().Add(ttl).Truncate(time.Second)
		if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: notAfter}, f); err != "" {
			t.Fatal(err)
		}

		if err := listNodes(cfg); err != nil {
			t.Fatalf("list: %v", err)
		}

		if !api.last().Equal(notAfter) {
			t.Errorf("certificate until %v shown, want %v", api.last(), notAfter)
		}
	}
}

// The proxy's refusals each read as their own sentence; a certificate refused at the
// handshake asks for a sign-in.
func TestTeleportRefusalsReadAsSentences(t *testing.T) {
	withAuthStore(t)

	f := newFakeTeleport(t, teleportGitHubPing)
	api := newCertAPI(t)
	cfg := teleportKubeconfigAt(f, api.Server, "")
	t.Cleanup(func() { kubeClients.forgetConfig(cfg, teleportTestContext) })

	if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: time.Now().Add(time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	for refusal, want := range map[string]string{
		`MFA is required to access Kubernetes cluster "sample-kube"`: "requires per-session MFA",
		`kubernetes cluster "sample-kube" not found`:                 `no Kubernetes cluster named sample-kube`,
		`[00] access denied`: "roles grant no access",
	} {
		api.refuseWith(refusal)

		err := listNodes(cfg)
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "BEGIN") {
			t.Errorf("%s: %v", refusal, err)
		}
	}

	api.refuseWith("")
	api.rejectAtHandshake.Store(true)
	kubeClients.forgetConfig(cfg, teleportTestContext)

	err := listNodes(cfg)
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "Teleport certificate expired") {
		t.Fatalf("refused at the handshake: %v", err)
	}

	// Once the client is open, a stream refused at the handshake says the same.
	api.rejectAtHandshake.Store(false)

	if err := listNodes(cfg); err != nil {
		t.Fatal(err)
	}

	api.rejectAtHandshake.Store(true)

	_, err = withKube(kubeTarget{config: cfg, context: teleportTestContext}, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		_, err := k.dialStream(ctx, "/stream")

		return struct{}{}, err
	})
	if err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) {
		t.Errorf("stream refused at the handshake: %v", err)
	}
}

// The first call of a new client (its probe) is refused in the same words as the later ones:
// Teleport 18 refuses /version with a plain-text 500, per-session MFA included.
func TestTeleportRefusalWhenTheClientOpens(t *testing.T) {
	withAuthStore(t)

	f := newFakeTeleport(t, teleportGitHubPing)
	api := newCertAPI(t)
	cfg := teleportKubeconfigAt(f, api.Server, "")
	t.Cleanup(func() { kubeClients.forgetConfig(cfg, teleportTestContext) })

	if err := teleportSignIn(t, cfg, teleportBrowser{notAfter: time.Now().Add(time.Hour)}, f); err != "" {
		t.Fatal(err)
	}

	api.refuseVersion.Store(true)

	err := listNodes(cfg)
	if err == nil || !strings.Contains(err.Error(), "per-session MFA") || strings.Contains(err.Error(), "500") {
		t.Fatalf("refused when opening: %v", err)
	}
}

func TestTeleportRefusalMapping(t *testing.T) {
	m := &teleportMethod{proxy: "proxy.example.org:443", kubeCluster: "sample-kube"}
	alpn := "kube-teleport-proxy-alpn.proxy.example.org"

	unknownCA := &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}

	streamCfg, err := websocket.NewConfig("wss://proxy.example.org/stream", "https://proxy.example.org")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, serverName string
		err              error
		want             string
	}{
		{"mfa", "", &kube.APIError{Code: 403, Message: `MFA is required to access Kubernetes cluster "x"`}, "per-session MFA"},
		{"not found", "", &kube.APIError{Code: 404, Message: `kubernetes cluster "x" not found`}, "no Kubernetes cluster named x"},
		{"roles", "", &kube.APIError{Code: 403, Message: "[00] access denied"}, "roles grant no access"},
		{"no roles", "", &kube.APIError{Code: 403, Message: "no roles grant access to it"}, "roles grant no access"},
		// What Teleport 18 sends, per-session MFA included (seen against the fixture): a 403
		// Status, and a plain-text 500 for /version.
		{"real proxy", "", &kube.APIError{Code: 403, Reason: "Forbidden", Message: "[00] access denied"}, "per-session MFA"},
		{"real proxy /version", "", &kube.APIError{Code: 500, Reason: "Internal Server Error", Message: "[00] access denied"}, "per-session MFA"},
		{"other 500 stays", "", &kube.APIError{Code: 500, Reason: "Internal Server Error", Message: "etcdserver: request timed out"}, "500"},
		{"kube rbac stays", "", &kube.APIError{Code: 403, Message: `nodes is forbidden: User "x" cannot list`}, "forbidden"},
		{"expired", "", tls.AlertError(42), KubeSignInRequired},
		{"expired stream", "", &websocket.DialError{Config: streamCfg, Err: errors.New("remote error: tls: expired certificate")}, KubeSignInRequired},
		{"load balancer", alpn, unknownCA, "did not answer as a Kubernetes endpoint"},
		{"own address, bad CA", "proxy.example.org", unknownCA, "certificate"},
	}

	for _, c := range cases {
		got := m.refusal(c.err, c.serverName)
		if got == nil || !strings.Contains(got.Error(), c.want) {
			t.Errorf("%s: %v", c.name, got)
		}
	}

	// A mapped refusal keeps the API error under it.
	var apiErr *kube.APIError
	if !errors.As(m.refusal(cases[0].err, ""), &apiErr) || apiErr.Code != 403 {
		t.Error("the MFA refusal lost its API error")
	}
}
