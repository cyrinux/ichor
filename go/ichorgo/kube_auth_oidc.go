package ichorgo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// OIDC sign-in, as kubelogin (`kubectl oidc-login`) and the legacy `auth-provider: oidc` do
// it: the ID token is the bearer, renewed with the refresh token. The first sign-in opens the
// identity provider in the browser and comes back to a loopback address the app listens on
// (the redirect kubelogin registers, so no change on the provider), or uses a device code.

const (
	oidcHTTPTimeout = 20 * time.Second
	// oidcMaxBody bounds an identity provider's answer.
	oidcMaxBody = 1 << 20
)

// kubeloginListen are kubelogin's default redirect addresses.
var kubeloginListen = []string{"127.0.0.1:8000", "127.0.0.1:18000"}

type oidcMethod struct {
	// method is the name the sign-in is shown under (oidc, or azure for Entra ID).
	method                         string
	issuer, clientID, clientSecret string
	scopes                         []string
	deviceCode, useAccessToken     bool
	listen                         []string
	redirectHost                   string
	// seedRefresh is an auth-provider's refresh token, used until the app has its own.
	seedRefresh string
	tls         *tls.Config
	// google marks Google's sign-in whatever the issuer (a test's fake one).
	google bool
	// redirectURI, when set, is a custom-scheme redirect the app receives and hands over
	// (SignInRun.Complete): no loopback server then.
	redirectURI string
}

func (m *oidcMethod) name() string { return m.method }

// newOIDCMethod reads the OIDC settings of a kubelogin exec or an oidc auth-provider.
func newOIDCMethod(user *kubeStoreUser) (*oidcMethod, error) {
	m := &oidcMethod{method: authOIDC, redirectHost: "localhost", tls: baseTLS(nil, false)}

	var caData string

	if e := user.User.Exec; e != nil {
		args := e.Args
		m.issuer = flagValue(args, "--oidc-issuer-url")
		m.clientID = flagValue(args, "--oidc-client-id")
		m.clientSecret = flagValue(args, "--oidc-client-secret")
		m.scopes = splitScopes(flagValues(args, "--oidc-extra-scope"))
		m.deviceCode = flagValue(args, "--grant-type") == "device-code"
		m.useAccessToken = slices.Contains(args, "--oidc-use-access-token") || flagValue(args, "--oidc-use-access-token") == "true"
		m.listen = flagValues(args, "--listen-address")
		m.redirectHost = cmpOr(flagValue(args, "--oidc-redirect-url-hostname"), "localhost")
		caData = flagValue(args, "--certificate-authority-data")
		m.tls.InsecureSkipVerify = slices.Contains(args, "--insecure-skip-tls-verify") //nolint:gosec // the user's own kubeconfig asks for it
	} else {
		cfg, _ := user.User.AuthProvider["config"].(map[string]any)
		str := func(key string) string { s, _ := cfg[key].(string); return s }
		m.issuer, m.clientID, m.clientSecret = str("idp-issuer-url"), str("client-id"), str("client-secret")
		m.scopes = splitScopes([]string{str("extra-scopes")})
		m.seedRefresh = str("refresh-token")
		caData = str("idp-certificate-authority-data")
	}

	if len(m.listen) == 0 {
		m.listen = kubeloginListen
	}

	if caData != "" {
		pem, err := base64.StdEncoding.DecodeString(caData)
		if err != nil {
			return nil, fmt.Errorf("OIDC provider CA: %w", err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("OIDC provider CA: no certificate found")
		}

		m.tls.RootCAs = pool
	}

	issuer, err := url.Parse(m.issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || m.clientID == "" {
		return nil, errors.New("the kubeconfig names no OIDC issuer (https) and client ID")
	}

	return m, nil
}

// flagValues is every value of a repeatable flag.
func flagValues(args []string, name string) []string {
	var out []string

	for i, arg := range args {
		if v, ok := strings.CutPrefix(arg, name+"="); ok {
			out = append(out, v)
		} else if arg == name && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}

	return out
}

func splitScopes(values []string) []string {
	var out []string

	for _, v := range values {
		for _, s := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' }) {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}

	return out
}

func (m *oidcMethod) httpClient() *http.Client {
	// An identity provider may redirect its discovery document; never to plain http.
	return newHTTPClient(httpClientOpts{tls: m.tls, timeout: oidcHTTPTimeout, followRedirects: true})
}

// isGoogle: Google refuses offline_access and wants access_type=offline instead.
func (m *oidcMethod) isGoogle() bool {
	return m.google || strings.Contains(m.issuer, "accounts.google.com")
}

func (m *oidcMethod) scope() string {
	scopes := append([]string{"openid"}, m.scopes...)
	if !m.isGoogle() && !slices.Contains(scopes, "offline_access") {
		scopes = append(scopes, "offline_access")
	}

	return strings.Join(scopes, " ")
}

type oidcDiscovery struct {
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

func (m *oidcMethod) discover(ctx context.Context) (oidcDiscovery, error) {
	var d oidcDiscovery

	err := m.getJSON(ctx, strings.TrimSuffix(m.issuer, "/")+"/.well-known/openid-configuration", &d)
	if err != nil {
		return oidcDiscovery{}, fmt.Errorf("OIDC discovery: %w", err)
	}

	if d.TokenEndpoint == "" {
		return oidcDiscovery{}, errors.New("OIDC discovery: no token endpoint")
	}

	return d, nil
}

func (m *oidcMethod) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := m.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, oidcMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", u, resp.StatusCode)
	}

	return json.Unmarshal(data, out)
}

// oidcTokens is a token endpoint's answer, or its error.
type oidcTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
	Interval     int    `json:"interval"`
}

func (m *oidcMethod) postForm(ctx context.Context, endpoint string, form url.Values, out any) (int, error) {
	if m.clientSecret != "" {
		form.Set("client_secret", m.clientSecret)
	}

	form.Set("client_id", m.clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, oidcMaxBody))
	if err != nil {
		return resp.StatusCode, err
	}

	if err := json.Unmarshal(data, out); err != nil {
		return resp.StatusCode, fmt.Errorf("token endpoint answered %d", resp.StatusCode)
	}

	return resp.StatusCode, nil
}

// oidcClaims is what the app reads of an ID token (the API server checks its signature).
type oidcClaims struct {
	Subject  string `json:"sub"`
	Email    string `json:"email"`
	Username string `json:"preferred_username"`
	Expires  int64  `json:"exp"`
	Nonce    string `json:"nonce"`
}

func readIDToken(token string) (oidcClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return oidcClaims{}, errors.New("the ID token is not a JWT")
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return oidcClaims{}, fmt.Errorf("ID token: %w", err)
	}

	var c oidcClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return oidcClaims{}, fmt.Errorf("ID token: %w", err)
	}

	return c, nil
}

// bearer is the token the API server gets and its expiry.
func (m *oidcMethod) bearer(t oidcTokens) (string, time.Time, oidcClaims, error) {
	if m.useAccessToken {
		expiry := time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 300)) * time.Second)

		claims, _ := readIDToken(t.IDToken)

		return t.AccessToken, expiry, claims, nil
	}

	if t.IDToken == "" {
		return "", time.Time{}, oidcClaims{}, errors.New("the identity provider sent no ID token")
	}

	claims, err := readIDToken(t.IDToken)
	if err != nil {
		return "", time.Time{}, oidcClaims{}, err
	}

	return t.IDToken, time.Unix(claims.Expires, 0), claims, nil
}

func cmpOrInt(v, fallback int64) int64 {
	if v > 0 {
		return v
	}

	return fallback
}

// session is the state a token answer leaves: refresh token kept unless a new one came.
func (m *oidcMethod) session(state kubeAuthState, t oidcTokens, bearer string, expiry time.Time, claims oidcClaims) kubeAuthState {
	refresh := cmpOr(t.RefreshToken, state.session("refresh"))
	out := state.withSession(map[string]string{"refresh": refresh, "bearer": bearer, "bearerExpiry": strconv.FormatInt(expiry.Unix(), 10)})
	out.User = cmpOr(claims.Email, claims.Username, claims.Subject, state.User)

	return out
}

func (m *oidcMethod) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	if bearer := state.session("bearer"); bearer != "" {
		exp, _ := strconv.ParseInt(state.session("bearerExpiry"), 10, 64)
		if time.Until(time.Unix(exp, 0)) > tokenRefreshMargin {
			return bearer, time.Unix(exp, 0), state, nil
		}
	}

	refresh := cmpOr(state.session("refresh"), m.seedRefresh)
	if refresh == "" {
		return "", time.Time{}, state, signInRequired(m.method, "")
	}

	d, err := m.discover(ctx)
	if err != nil {
		return "", time.Time{}, state, err
	}

	var t oidcTokens

	if _, err := m.postForm(ctx, d.TokenEndpoint, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}, &t); err != nil {
		return "", time.Time{}, state, err
	}

	if t.Error != "" {
		// invalid_grant: the refresh token expired or was revoked.
		return "", time.Time{}, state.withSession(map[string]string{"refresh": "", "bearer": ""}), signInRequired(m.method, cmpOr(t.Description, t.Error))
	}

	bearer, expiry, claims, err := m.bearer(t)
	if err != nil {
		return "", time.Time{}, state, signInRequired(m.method, err.Error())
	}

	return bearer, expiry, m.session(state, t, bearer, expiry, claims), nil
}

// signInPrompt is what the app shows during an interactive sign-in.
type signInPrompt struct {
	// Kind is "browser" (open URL; it comes back by itself) or "device" (show UserCode and
	// open VerificationURL, the complete one when given).
	Kind            string `json:"kind"`
	URL             string `json:"url"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	// RedirectPrefix is where the browser comes back: the app closes its browser sheet on it.
	RedirectPrefix string `json:"redirectPrefix,omitempty"`
	ExpiresIn      int    `json:"expiresIn,omitempty"`
}

func (m *oidcMethod) signIn(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	d, err := m.discover(ctx)
	if err != nil {
		return state, err
	}

	if m.deviceCode {
		return m.deviceSignIn(ctx, state, d, prompt)
	}

	return m.browserSignIn(ctx, state, d, prompt, callbacks)
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)

	return base64.RawURLEncoding.EncodeToString(b)
}

func (m *oidcMethod) browserSignIn(ctx context.Context, state kubeAuthState, d oidcDiscovery, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	if d.AuthorizationEndpoint == "" {
		return state, errors.New("the identity provider has no authorization endpoint")
	}

	// The answer comes to the loopback server, or (a custom-scheme redirect the app receives)
	// only through callbacks.
	codes := make(chan url.Values, 1)
	redirect := m.redirectURI

	if redirect == "" {
		loopback, stop, err := m.serveLoopback(ctx, codes)
		if err != nil {
			return state, err
		}
		defer stop()

		redirect = loopback
	}

	verifier, oauthState, nonce := randomToken(), randomToken(), randomToken()
	challenge := sha256.Sum256([]byte(verifier))

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {m.clientID},
		"redirect_uri":          {redirect},
		"scope":                 {m.scope()},
		"state":                 {oauthState},
		"nonce":                 {nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}

	if m.isGoogle() {
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
	}

	sep := "?"
	if strings.Contains(d.AuthorizationEndpoint, "?") {
		sep = "&"
	}

	prompt(signInPrompt{Kind: "browser", URL: d.AuthorizationEndpoint + sep + q.Encode(), RedirectPrefix: redirect})

	var answer url.Values

	select {
	case <-ctx.Done():
		return state, ctx.Err()
	case answer = <-codes:
	case cb := <-callbacks:
		u, err := url.Parse(cb)
		if err != nil {
			return state, fmt.Errorf("sign-in callback: %w", err)
		}

		answer = u.Query()
	}

	if e := answer.Get("error"); e != "" {
		return state, fmt.Errorf("sign-in refused: %s", cmpOr(answer.Get("error_description"), e))
	}

	if answer.Get("state") != oauthState {
		return state, errors.New("sign-in answer does not match the request (state)")
	}

	var t oidcTokens

	form := url.Values{"grant_type": {"authorization_code"}, "code": {answer.Get("code")}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	if _, err := m.postForm(ctx, d.TokenEndpoint, form, &t); err != nil {
		return state, err
	}

	return m.finish(state, t, nonce)
}

// serveLoopback listens on the first free m.listen address and hands each answer with a
// state to codes; it returns the redirect URI and how to stop.
func (m *oidcMethod) serveLoopback(ctx context.Context, codes chan<- url.Values) (string, func(), error) {
	ln, err := listenFirst(ctx, m.listen)
	if err != nil {
		return "", nil, err
	}

	_, port, _ := net.SplitHostPort(ln.Addr().String())

	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") == "" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, signInDonePage)

		select {
		case codes <- r.URL.Query():
		default:
		}
	})}

	go func() { _ = srv.Serve(ln) }() //nolint:errcheck

	stop := func() {
		_ = srv.Close() //nolint:errcheck
		_ = ln.Close()  //nolint:errcheck
	}

	return "http://" + net.JoinHostPort(m.redirectHost, port), stop, nil
}

func (m *oidcMethod) finish(state kubeAuthState, t oidcTokens, nonce string) (kubeAuthState, error) {
	if t.Error != "" {
		return state, fmt.Errorf("sign-in refused: %s", cmpOr(t.Description, t.Error))
	}

	bearer, expiry, claims, err := m.bearer(t)
	if err != nil {
		return state, err
	}

	if nonce != "" && claims.Nonce != "" && claims.Nonce != nonce {
		return state, errors.New("sign-in answer does not match the request (nonce)")
	}

	return m.session(state, t, bearer, expiry, claims), nil
}

// listenFirst listens on the first free address of addrs (kubelogin tries them in order).
func listenFirst(ctx context.Context, addrs []string) (net.Listener, error) {
	var errs []error

	for _, addr := range addrs {
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
		if err == nil {
			return ln, nil
		}

		errs = append(errs, err)
	}

	return nil, fmt.Errorf("no free sign-in address: %w", errors.Join(errs...))
}

const signInDonePage = `<!doctype html><meta name="viewport" content="width=device-width">` +
	`<body style="font-family:sans-serif;text-align:center;padding-top:20vh">` +
	`<h1>Signed in</h1><p>You can go back to Ichor.</p>`

type oidcDeviceAnswer struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	// Entra ID names it verification_url.
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Error           string `json:"error"`
	Description     string `json:"error_description"`
}

func (m *oidcMethod) deviceSignIn(ctx context.Context, state kubeAuthState, d oidcDiscovery, prompt func(signInPrompt)) (kubeAuthState, error) {
	if d.DeviceAuthorizationEndpoint == "" {
		return state, errors.New("the identity provider does not offer device codes")
	}

	var a oidcDeviceAnswer
	if _, err := m.postForm(ctx, d.DeviceAuthorizationEndpoint, url.Values{"scope": {m.scope()}}, &a); err != nil {
		return state, err
	}

	if a.Error != "" || a.DeviceCode == "" {
		return state, fmt.Errorf("device code refused: %s", cmpOr(a.Description, a.Error, "no code"))
	}

	verification := cmpOr(a.VerificationURI, a.VerificationURL)
	prompt(signInPrompt{Kind: "device", URL: cmpOr(a.VerificationURIComplete, verification), UserCode: a.UserCode, VerificationURL: verification, ExpiresIn: a.ExpiresIn})

	t, err := pollDeviceToken(ctx, a, func(ctx context.Context) (oidcTokens, error) {
		var t oidcTokens

		_, err := m.postForm(ctx, d.TokenEndpoint, url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {a.DeviceCode}}, &t)

		return t, err
	})
	if err != nil {
		return state, err
	}

	return m.finish(state, t, "")
}

// deviceMinInterval is the shortest wait between two device-code polls (RFC 8628 default).
var deviceMinInterval = 5 * time.Second

// pollDeviceToken asks for the token every interval until the user approved, refused, or
// the code expired (RFC 8628).
func pollDeviceToken(ctx context.Context, a oidcDeviceAnswer, poll func(context.Context) (oidcTokens, error)) (oidcTokens, error) {
	interval := max(time.Duration(a.Interval)*time.Second, deviceMinInterval)
	deadline := time.Now().Add(time.Duration(cmpOrInt(int64(a.ExpiresIn), 900)) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return oidcTokens{}, ctx.Err()
		case <-time.After(interval):
		}

		t, err := poll(ctx)
		if err != nil {
			return oidcTokens{}, err
		}

		switch t.Error {
		case "":
			return t, nil
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		default:
			return oidcTokens{}, fmt.Errorf("sign-in refused: %s", cmpOr(t.Description, t.Error))
		}
	}

	return oidcTokens{}, errors.New("the device code expired before it was approved")
}
