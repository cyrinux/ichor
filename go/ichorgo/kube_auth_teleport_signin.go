package ichorgo

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// teleportConsoleRequest is Teleport's SSOLoginConsoleReq. RouteToCluster and
// KubernetesCluster have no JSON tag upstream, hence their case; []byte fields are base64.
type teleportConsoleRequest struct {
	RedirectURL       string        `json:"redirect_url"`
	TLSPubKey         []byte        `json:"tls_pub_key"`
	CertTTL           time.Duration `json:"cert_ttl"`
	ConnectorID       string        `json:"connector_id"`
	KubernetesCluster string        `json:"KubernetesCluster"`        //nolint:tagliatelle // Teleport's own field
	RouteToCluster    string        `json:"RouteToCluster,omitempty"` //nolint:tagliatelle // Teleport's own field
	ClientVersion     string        `json:"client_version,omitempty"`
}

// teleportConsoleAnswer is where to send the browser: a URL, or (SAML with the HTTP-POST
// binding) a form to post from the loopback page.
type teleportConsoleAnswer struct {
	RedirectURL string `json:"redirect_url"`
	PostForm    string `json:"post_form"`
}

// teleportSealed is the callback's response: AES-256-GCM with the secret key, no extra data.
type teleportSealed struct {
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce"`
}

// teleportLogin is the part of Teleport's CLILoginResponse the app keeps (not the SSH one).
type teleportLogin struct {
	Username string `json:"username"`
	TLSCert  []byte `json:"tls_cert"`
}

// teleportCallbackPath is the only path the proxy redirects to.
const teleportCallbackPath = "/callback"

func (m *teleportMethod) signIn(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	ctx, cancel := context.WithTimeout(ctx, teleportCallbackTimeout)
	defer cancel()

	p, err := m.ping(ctx, state.secret(teleportConnector))
	if err != nil {
		return state, err
	}

	kind, connector, err := p.connector()
	if err != nil {
		return state, err
	}

	key, pubPEM, err := teleportKey(p.useRSA())
	if err != nil {
		return state, err
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return state, err
	}

	answers := make(chan url.Values, 1)

	lb, err := serveTeleportLoopback(ctx, answers)
	if err != nil {
		return state, err
	}
	defer lb.stop()

	var console teleportConsoleAnswer

	req := teleportConsoleRequest{
		RedirectURL: lb.base + teleportCallbackPath + "?" + url.Values{"secret_key": {hex.EncodeToString(secret)}}.Encode(),
		TLSPubKey:   pubPEM, CertTTL: teleportCertTTL, ConnectorID: connector,
		KubernetesCluster: m.kubeCluster, RouteToCluster: m.teleportCluster, ClientVersion: "ichor",
	}
	if err := m.call(ctx, http.MethodPost, "/webapi/"+kind+"/login/console", req, &console); err != nil {
		return state, err
	}

	browserURL, err := lb.browserURL(console)
	if err != nil {
		return state, err
	}

	prompt(signInPrompt{Kind: "browser", URL: browserURL, RedirectPrefix: lb.base + teleportCallbackPath})

	var answer url.Values

	select {
	case <-ctx.Done():
		return state, ctx.Err()
	case answer = <-answers:
	case cb := <-callbacks:
		u, err := url.Parse(cb)
		if err != nil {
			return state, fmt.Errorf("sign-in callback: %w", err)
		}

		answer = u.Query()
	}

	login, err := openTeleportResponse(secret, answer.Get("response"))
	if err != nil {
		return state, err
	}

	return teleportSignedIn(state, login, key, connector)
}

// teleportKey is a new key pair and its public half as PEM (PKIX): ECDSA P-256, or RSA 2048
// for an older proxy.
func teleportKey(useRSA bool) (crypto.Signer, []byte, error) {
	var (
		key crypto.Signer
		err error
	)

	if useRSA {
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	} else {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}

	if err != nil {
		return nil, nil, err
	}

	der, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return nil, nil, err
	}

	return key, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// openTeleportResponse opens the sealed callback response with the sign-in's secret key.
func openTeleportResponse(secret []byte, response string) (teleportLogin, error) {
	var sealed teleportSealed
	if response == "" || json.Unmarshal([]byte(response), &sealed) != nil {
		return teleportLogin{}, errors.New("the Teleport sign-in came back without a certificate")
	}

	block, err := aes.NewCipher(secret)
	if err != nil {
		return teleportLogin{}, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return teleportLogin{}, err
	}

	if len(sealed.Nonce) != gcm.NonceSize() {
		return teleportLogin{}, errors.New("the Teleport sign-in answer is not for this sign-in")
	}

	plain, err := gcm.Open(nil, sealed.Nonce, sealed.Ciphertext, nil)
	if err != nil {
		return teleportLogin{}, errors.New("the Teleport sign-in answer is not for this sign-in")
	}

	var login teleportLogin
	if err := json.Unmarshal(plain, &login); err != nil {
		return teleportLogin{}, fmt.Errorf("teleport sign-in answer: %w", err)
	}

	return login, nil
}

// teleportSignedIn is state with the issued certificate, which must be for key (a certificate
// for any other key is useless, and a sign of a mixed-up answer) and still valid.
func teleportSignedIn(state kubeAuthState, login teleportLogin, key crypto.Signer, connector string) (kubeAuthState, error) {
	block, _ := pem.Decode(login.TLSCert)
	if block == nil {
		return state, errors.New("the Teleport sign-in answer holds no certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return state, fmt.Errorf("teleport certificate: %w", err)
	}

	pub, ok := cert.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	if !ok || !pub.Equal(key.Public()) {
		return state, errors.New("the Teleport certificate is not for this sign-in's key")
	}

	if time.Until(cert.NotAfter) <= tokenRefreshMargin {
		return state, errors.New("the Teleport proxy issued a certificate that has already expired")
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return state, err
	}

	out := state.withSession(map[string]string{
		teleportSessionCert: string(pem.EncodeToMemory(block)),
		teleportSessionKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		teleportSessionUser: login.Username,
	})
	out.Secrets = map[string]string{teleportConnector: connector}
	out.User = login.Username
	out.Expires, out.SessionExpires = cert.NotAfter.Unix(), cert.NotAfter.Unix()

	return out, nil
}

// teleportLoopback is the loopback server the browser comes back to.
type teleportLoopback struct {
	base string
	stop func()

	mu       sync.Mutex
	postForm string
}

// serveTeleportLoopback serves the callback (handing its query to answers) and, for a SAML
// HTTP-POST connector, the page that posts the proxy's form.
func serveTeleportLoopback(ctx context.Context, answers chan<- url.Values) (*teleportLoopback, error) {
	ln, err := listenFirst(ctx, teleportListen)
	if err != nil {
		return nil, err
	}

	lb := &teleportLoopback{base: "http://" + ln.Addr().String()}

	mux := http.NewServeMux()
	mux.HandleFunc("GET "+teleportCallbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("response") == "" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, signInDonePage)

		select {
		case answers <- r.URL.Query():
		default:
		}
	})
	mux.HandleFunc("GET /saml", func(w http.ResponseWriter, r *http.Request) {
		lb.mu.Lock()
		form := lb.postForm
		lb.mu.Unlock()

		if form == "" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, form)
	})

	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: mux}

	go func() { _ = srv.Serve(ln) }() //nolint:errcheck

	lb.stop = stopLoopback(srv, ln)

	return lb, nil
}

// browserURL is where the browser goes: the proxy's redirect, or the loopback page that
// posts its SAML form.
func (lb *teleportLoopback) browserURL(a teleportConsoleAnswer) (string, error) {
	if a.PostForm != "" {
		lb.mu.Lock()
		lb.postForm = a.PostForm
		lb.mu.Unlock()

		return lb.base + "/saml", nil
	}

	if err := checkOIDCEndpoint("sign-in page", a.RedirectURL); err != nil {
		return "", errors.New("the Teleport proxy's sign-in page is not an https address")
	}

	return a.RedirectURL, nil
}
