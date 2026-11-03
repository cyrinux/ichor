package ichorgo

import (
	"bytes"
	"context"
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

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// A kubeconfig written by `tsh kube login` runs `tsh kube credentials`: the app does what
// `tsh login` does over HTTPS instead. It asks the Teleport proxy for an SSO sign-in in the
// browser (GitHub, OIDC or SAML), which comes back to a loopback address with a short-lived
// X.509 client certificate for one Kubernetes cluster, sealed with a key only this sign-in
// knows. There is no refresh token: once the certificate ends, the user signs in again.

const (
	// teleportCertTTL is the length asked for; the proxy caps it to the role's max_session_ttl.
	teleportCertTTL = 12 * time.Hour
	// teleportCallbackTimeout is how long the proxy keeps an SSO request open (tsh's own).
	teleportCallbackTimeout = 180 * time.Second
	// teleportRSABefore is the first Teleport major version whose proxy takes ECDSA keys.
	teleportRSABefore = 17

	teleportSessionCert = "tlsCert"
	teleportSessionKey  = "tlsKey"
	teleportSessionUser = "username"
	// teleportConnector is the SSO connector last signed in with (a name, not a secret).
	teleportConnector = "teleportConnector"
)

var errTeleportLocalOnly = errors.New("this Teleport proxy only signs local users in with a password, which the app " +
	"does not ask for; ask your Teleport administrator for a kubeconfig with a certificate (tctl auth sign --format=kubernetes)")

// teleportListen is where the sign-in comes back to: any free loopback port, as tsh does.
var teleportListen = []string{"127.0.0.1:0"}

// teleportHTTPClient is the client of the proxy's web API; tests replace it.
var teleportHTTPClient = func(tlsConfig *tls.Config) *http.Client {
	return httpx.New(httpx.Opts{TLS: tlsConfig, Timeout: oidcHTTPTimeout})
}

type teleportMethod struct {
	// proxy is the proxy's web address, host:port.
	proxy, teleportCluster, kubeCluster string
	insecure                            bool
	http                                *http.Client
}

func init() {
	extraSignInMethods[authTeleport] = func(user *kubeStoreUser, cluster *kubeStoreCluster) (signInMethod, error) {
		return newTeleportMethod(user, cluster)
	}
	previewSignIns[authTeleport] = true
}

func newTeleportMethod(user *kubeStoreUser, cluster *kubeStoreCluster) (*teleportMethod, error) {
	e := user.User.Exec
	if e == nil || execCommandName(e) != "tsh" {
		return nil, errors.New("not a Teleport kubeconfig")
	}

	proxy, err := teleportProxyAddr(flagValue(e.Args, "--proxy"))
	if err != nil {
		return nil, err
	}

	m := &teleportMethod{
		proxy:           proxy,
		teleportCluster: flagValue(e.Args, "--teleport-cluster"),
		kubeCluster:     flagValue(e.Args, "--kube-cluster"),
		insecure:        slices.Contains(e.Args, "--insecure"),
	}
	if m.kubeCluster == "" {
		return nil, errors.New("the tsh kubeconfig names no Kubernetes cluster (--kube-cluster)")
	}

	m.http = teleportHTTPClient(httpx.BaseTLS(teleportRoots(cluster), m.insecure))

	return m, nil
}

// teleportProxyAddr is tsh's --proxy as host:port: "host", "host:web", "host:web,ssh",
// with or without https://; the web port is 443 when unset.
func teleportProxyAddr(raw string) (string, error) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "https://")
	raw, _, _ = strings.Cut(raw, ",")
	raw = strings.TrimSuffix(raw, "/")

	if raw == "" || strings.ContainsAny(raw, "/@?#") {
		return "", errors.New("the tsh kubeconfig names no usable Teleport proxy (--proxy)")
	}

	if _, _, err := net.SplitHostPort(raw); err != nil {
		raw = net.JoinHostPort(strings.Trim(raw, "[]"), "443")
	}

	return raw, nil
}

// teleportRoots are the system's roots plus the kubeconfig's CA (a proxy with its own CA);
// nil, the system's alone, when the kubeconfig has none.
func teleportRoots(cluster *kubeStoreCluster) *x509.CertPool {
	if cluster == nil || cluster.Cluster.CertificateAuthorityData == "" {
		return nil
	}

	ca, err := base64.StdEncoding.DecodeString(cluster.Cluster.CertificateAuthorityData)
	if err != nil {
		return nil
	}

	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}

	pool.AppendCertsFromPEM(ca)

	return pool
}

func (m *teleportMethod) name() string { return authTeleport }

// describe is what KubeSignInInfo shows of the sign-in: where it goes, never what it holds.
func (m *teleportMethod) describe(state kubeAuthState) map[string]string {
	out := map[string]string{"teleportProxy": m.proxy, "kubeCluster": m.kubeCluster}
	if m.teleportCluster != "" {
		out["teleportCluster"] = m.teleportCluster
	}

	if c := state.secret(teleportConnector); c != "" {
		out[teleportConnector] = c
	}

	return out
}

// mint returns the stored certificate and key as one PEM bundle while the certificate has
// more than tokenRefreshMargin left. It is a client certificate, never a bearer token.
func (m *teleportMethod) mint(_ context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	certPEM, keyPEM := state.session(teleportSessionCert), state.session(teleportSessionKey)
	if certPEM == "" || keyPEM == "" {
		return "", time.Time{}, state, signInRequired(authTeleport, "")
	}

	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil || pair.Leaf == nil {
		return "", time.Time{}, state, signInRequired(authTeleport, "the stored Teleport certificate is not readable")
	}

	if time.Until(pair.Leaf.NotAfter) <= tokenRefreshMargin {
		return "", time.Time{}, state, signInRequired(authTeleport, reasonTeleportSession)
	}

	return certPEM + "\n" + keyPEM, pair.Leaf.NotAfter, state, nil
}

// teleportPingAnswer is the part of /webapi/ping the sign-in reads.
type teleportPingAnswer struct {
	Auth struct {
		Type   string         `json:"type"`
		OIDC   *teleportNamed `json:"oidc"`
		SAML   *teleportNamed `json:"saml"`
		GitHub *teleportNamed `json:"github"`
	} `json:"auth"`
	ServerVersion string `json:"server_version"`
}

type teleportNamed struct {
	Name string `json:"name"`
}

// connector is the SSO connector the ping names: its type (the console path) and name.
func (p teleportPingAnswer) connector() (kind, name string, err error) {
	byType := map[string]*teleportNamed{"oidc": p.Auth.OIDC, "saml": p.Auth.SAML, "github": p.Auth.GitHub}

	named, ok := byType[p.Auth.Type]
	if !ok {
		return "", "", errTeleportLocalOnly
	}

	if named == nil || named.Name == "" {
		return "", "", fmt.Errorf("the Teleport proxy names no %s connector", p.Auth.Type)
	}

	return p.Auth.Type, named.Name, nil
}

// useRSA reports a proxy older than the ECDSA keys tsh makes since v17.
func (p teleportPingAnswer) useRSA() bool {
	major, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(p.ServerVersion, "v"), ".", 2)[0])

	return err == nil && major < teleportRSABefore
}

func (m *teleportMethod) ping(ctx context.Context, connector string) (teleportPingAnswer, error) {
	path := "/webapi/ping"
	if connector != "" {
		path += "/" + url.PathEscape(connector)
	}

	var p teleportPingAnswer

	err := m.call(ctx, http.MethodGet, path, nil, &p)

	return p, err
}

// call sends body (JSON) to the proxy's web API and reads its JSON answer. A redirect is
// refused: the request never goes anywhere but the proxy.
func (m *teleportMethod) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader

	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, "https://"+m.proxy+path, reader)
	if err != nil {
		return fmt.Errorf("teleport proxy: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("teleport proxy not reachable: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return errors.New("the Teleport proxy answered with a redirect, which the sign-in does not follow")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the Teleport proxy refused the sign-in (HTTP %d): %s", resp.StatusCode, teleportErrorMessage(raw))
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("teleport proxy answer: %w", err)
	}

	return nil
}

// teleportErrorMessage is the message of a web API error ({"error":{"message":…}}, or
// {"message":…}), one line.
func teleportErrorMessage(raw []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}

	_ = json.Unmarshal(raw, &e)

	return oneLine(cmpOr(e.Error.Message, e.Message, "no reason given"))
}
