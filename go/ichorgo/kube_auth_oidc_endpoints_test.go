package ichorgo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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

const (
	endpointTestRefresh = "sample-refresh-value"
	endpointTestSecret  = "sample-client-secret"
)

// credentialSink records every request it gets: nothing credential-bearing may reach it.
type credentialSink struct {
	mu     sync.Mutex
	bodies []string
}

func (s *credentialSink) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	s.bodies = append(s.bodies, r.Method+" "+r.URL.Path+" "+string(body))
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id_token":%q,"access_token":"at","expires_in":3600}`, idToken(map[string]any{"sub": "u1", "exp": time.Now().Add(time.Hour).Unix()}))
}

func (s *credentialSink) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.bodies...)
}

// newPlainSink is a sink on plain http, newTLSSink one on https: another origin than the
// issuer, trusted through the same test certificate.
func newPlainSink(t *testing.T) (*credentialSink, *httptest.Server) {
	t.Helper()

	sink := &credentialSink{}
	srv := httptest.NewServer(sink)
	t.Cleanup(srv.Close)

	return sink, srv
}

func newTLSSink(t *testing.T) (*credentialSink, *httptest.Server) {
	t.Helper()

	sink := &credentialSink{}
	srv := httptest.NewTLSServer(sink)
	t.Cleanup(srv.Close)

	return sink, srv
}

// discoveryIssuer is an https issuer whose discovery document is built from its own URL.
func discoveryIssuer(t *testing.T, document func(issuer string) string) (*httptest.Server, *tls.Config) {
	t.Helper()

	var srv *httptest.Server

	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, document(srv.URL))
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	return srv, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func refreshState() kubeAuthState {
	return kubeAuthState{Method: authOIDC}.withSession(map[string]string{"refresh": endpointTestRefresh})
}

func TestCheckOIDCEndpoint(t *testing.T) {
	cases := []struct {
		raw  string
		want string // "" accepts
	}{
		{"https://idp.example.com/token", ""},
		{"https://idp.example.com:8443/realms/a/token?tenant=b", ""},
		{"HTTPS://idp.example.com/token", ""},
		{"http://idp.example.com/token", "not an https address"},
		{"ftp://idp.example.com/token", "not an https address"},
		{"/token", "not an https address"},
		{"idp.example.com/token", "not an https address"},
		{"https:idp.example.com/token", "not an https address"},
		{"https:///token", "not an https address"},
		{"https://:8443/token", "not an https address"},
		{"https://user:pass@idp.example.com/token", "user name or password"},
		{"https://user@idp.example.com/token", "user name or password"},
		{"https://idp.example.com/\x7f", "not an https address"},
		{"", "not an https address"},
	}

	for _, c := range cases {
		err := checkOIDCEndpoint("token endpoint", c.raw)

		switch {
		case c.want == "" && err != nil:
			t.Errorf("%q: %v", c.raw, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%q: err %v, want %q", c.raw, err, c.want)
		case err != nil && strings.Contains(err.Error(), "example.com"):
			t.Errorf("%q: the error repeats the address: %v", c.raw, err)
		}
	}
}

// Every way an endpoint can be bad, for each endpoint, from discovery and from m.endpoints:
// no refresh token, code or secret leaves, and the stored session is kept.
func TestOIDCRefusesInsecureEndpoints(t *testing.T) {
	sink, plain := newPlainSink(t)
	ok := func(issuer, path string) string { return issuer + path }

	cases := []struct {
		name                string
		auth, token, device func(issuer string) string
		want                string
	}{
		{name: "http token", token: func(string) string { return plain.URL + "/token" }, want: "token endpoint is not an https address"},
		{name: "http device", device: func(string) string { return plain.URL + "/device" }, want: "device authorization endpoint is not an https address"},
		{name: "http authorization", auth: func(string) string { return plain.URL + "/auth" }, want: "authorization endpoint is not an https address"},
		{name: "relative token", token: func(string) string { return "/token" }, want: "token endpoint is not an https address"},
		{name: "userinfo token", token: func(issuer string) string {
			return strings.Replace(issuer, "https://", "https://a:b@", 1) + "/token"
		}, want: "token endpoint must not hold a user name or password"},
	}

	for _, c := range cases {
		endpoint := func(f func(string) string, issuer, path string) string {
			if f != nil {
				return f(issuer)
			}

			return ok(issuer, path)
		}

		document := func(issuer string) string {
			return fmt.Sprintf(`{"authorization_endpoint":%q,"token_endpoint":%q,"device_authorization_endpoint":%q}`,
				endpoint(c.auth, issuer, "/auth"), endpoint(c.token, issuer, "/token"), endpoint(c.device, issuer, "/device"))
		}

		issuer, tlsConfig := discoveryIssuer(t, document)
		discovered := func() *oidcMethod {
			return &oidcMethod{method: authOIDC, issuer: issuer.URL, clientID: "kube", clientSecret: endpointTestSecret, tls: tlsConfig, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}
		}
		configured := func() *oidcMethod {
			m := discovered()
			m.endpoints = &oidcDiscovery{AuthorizationEndpoint: endpoint(c.auth, issuer.URL, "/auth"), TokenEndpoint: endpoint(c.token, issuer.URL, "/token"), DeviceAuthorizationEndpoint: endpoint(c.device, issuer.URL, "/device")}

			return m
		}

		for source, build := range map[string]func() *oidcMethod{"discovery": discovered, "configured": configured} {
			t.Run(c.name+"/"+source+"/refresh", func(t *testing.T) {
				_, _, state, err := build().mint(context.Background(), refreshState())
				if err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("err %v, want %q", err, c.want)
				}

				if state.session("refresh") != endpointTestRefresh {
					t.Errorf("the stored session was dropped: %+v", state)
				}
			})

			for _, device := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/signIn device=%v", c.name, source, device), func(t *testing.T) {
					m := build()
					m.deviceCode = device
					prompted := false

					_, err := m.signIn(context.Background(), kubeAuthState{}, func(signInPrompt) { prompted = true }, nil)
					if err == nil || !strings.Contains(err.Error(), c.want) {
						t.Fatalf("err %v, want %q", err, c.want)
					}

					if prompted {
						t.Error("a sign-in was presented for an insecure endpoint")
					}
				})
			}
		}
	}

	if got := sink.received(); len(got) != 0 {
		t.Fatalf("requests reached the insecure endpoint: %q", got)
	}
}

// postForm is the last gate whatever path led to it, before it touches the form.
func TestOIDCPostFormRefusesInsecureEndpoint(t *testing.T) {
	sink, plain := newPlainSink(t)
	m := &oidcMethod{method: authOIDC, clientID: "kube", clientSecret: endpointTestSecret}

	var out oidcTokens

	if _, err := m.postForm(context.Background(), plain.URL+"/token", url.Values{"refresh_token": {endpointTestRefresh}}, &out); err == nil {
		t.Fatal("posted to an http endpoint")
	}

	if got := sink.received(); len(got) != 0 {
		t.Fatalf("credentials reached it: %q", got)
	}
}

// A securely discovered endpoint on another origin is fine (some providers publish them).
func TestOIDCAcceptsCrossOriginDiscoveredEndpoint(t *testing.T) {
	sink, other := newTLSSink(t)
	issuer, tlsConfig := discoveryIssuer(t, func(string) string {
		return fmt.Sprintf(`{"token_endpoint":%q}`, other.URL+"/token")
	})
	m := &oidcMethod{method: authOIDC, issuer: issuer.URL, clientID: "kube", tls: tlsConfig}

	if _, _, _, err := m.mint(context.Background(), refreshState()); err != nil {
		t.Fatal(err)
	}

	if got := sink.received(); len(got) != 1 || !strings.Contains(got[0], endpointTestRefresh) {
		t.Fatalf("token endpoint got %q", got)
	}
}

// A redirect from an endpoint that takes credentials is refused, whatever its status and
// whether it stays on the origin, and the POST is not replayed.
func TestOIDCRefusesRedirectedCredentials(t *testing.T) {
	sink, other := newTLSSink(t)

	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, kind := range []string{"refresh", "device", "code"} {
			t.Run(fmt.Sprintf("%d/%s", status, kind), func(t *testing.T) {
				var posts []string

				var mu sync.Mutex

				var srv *httptest.Server

				srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.URL.Path == "/.well-known/openid-configuration":
						fmt.Fprintf(w, `{"authorization_endpoint":"%[1]s/auth","token_endpoint":"%[1]s/moved","device_authorization_endpoint":"%[1]s/moved"}`, srv.URL)
					case r.URL.Path == "/moved":
						mu.Lock()
						posts = append(posts, r.Method)
						mu.Unlock()

						// A JSON body, so a client reading the 3xx as an answer would not fail.
						w.Header().Set("Location", other.URL+"/stolen")
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(status)
						_, _ = io.WriteString(w, `{}`)
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(srv.Close)

				pool := x509.NewCertPool()
				pool.AddCert(srv.Certificate())

				m := &oidcMethod{method: authOIDC, issuer: srv.URL, clientID: "kube", clientSecret: endpointTestSecret, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1", tls: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}

				var err error

				switch kind {
				case "refresh":
					var state kubeAuthState

					_, _, state, err = m.mint(context.Background(), refreshState())
					if state.session("refresh") != endpointTestRefresh {
						t.Errorf("the stored session was dropped: %+v", state)
					}
				case "device":
					m.deviceCode = true
					_, err = m.signIn(context.Background(), kubeAuthState{}, func(signInPrompt) { t.Error("device code presented") }, nil)
				case "code":
					callbacks := make(chan string, 1)
					_, err = m.signIn(context.Background(), kubeAuthState{}, func(p signInPrompt) {
						u, perr := url.Parse(p.URL)
						if perr != nil {
							t.Error(perr)

							return
						}

						callbacks <- "https://back.example.com/?code=sample-code&state=" + url.QueryEscape(u.Query().Get("state"))
					}, callbacks)
				}

				if err == nil || !strings.Contains(err.Error(), "redirect") {
					t.Fatalf("err %v, want a refused redirect", err)
				}

				mu.Lock()
				defer mu.Unlock()

				if len(posts) != 1 || posts[0] != http.MethodPost {
					t.Errorf("endpoint saw %v, want one POST", posts)
				}
			})
		}
	}

	if got := sink.received(); len(got) != 0 {
		t.Fatalf("credentials reached the redirect's target: %q", got)
	}
}

// Discovery itself carries no credential: an https redirect of the document still works.
func TestOIDCDiscoveryStillFollowsHTTPSRedirects(t *testing.T) {
	target, tlsConfig := discoveryIssuer(t, func(issuer string) string {
		return fmt.Sprintf(`{"token_endpoint":%q}`, issuer+"/token")
	})

	front := httptest.NewTLSServer(http.RedirectHandler(target.URL+"/.well-known/openid-configuration", http.StatusFound))
	t.Cleanup(front.Close)

	m := &oidcMethod{method: authOIDC, issuer: front.URL, clientID: "kube", tls: tlsConfig}

	d, err := m.discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if d.TokenEndpoint != target.URL+"/token" {
		t.Fatalf("discovered %+v", d)
	}
}

// The Omni sign-in reads the same document: no authorization request, no code exchange.
func TestOmniOIDCRefusesInsecureEndpoints(t *testing.T) {
	sink, plain := newPlainSink(t)

	cases := []struct {
		name     string
		document func(issuer string) string
		want     string
	}{
		{"http token", func(issuer string) string {
			return fmt.Sprintf(`{"authorization_endpoint":%q,"token_endpoint":%q}`, issuer+"/authorize", plain.URL+"/token")
		}, "token endpoint is not an https address"},
		{"http authorization", func(issuer string) string {
			return fmt.Sprintf(`{"authorization_endpoint":%q,"token_endpoint":%q}`, plain.URL+"/authorize", issuer+"/token")
		}, "authorization endpoint is not an https address"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issuer, tlsConfig := discoveryIssuer(t, c.document)
			omniTestRoots = tlsConfig.RootCAs

			t.Cleanup(func() { omniTestRoots = nil })

			cfgCtx, _ := omniInstanceContext("https://acme.omni.example.com", testUserIdentity)

			_, _, err := omniOIDCToken(context.Background(), cfgCtx, omniSigner{}, issuer.URL, "demo")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err %v, want %q", err, c.want)
			}
		})
	}

	if got := sink.received(); len(got) != 0 {
		t.Fatalf("requests reached the insecure endpoint: %q", got)
	}
}
