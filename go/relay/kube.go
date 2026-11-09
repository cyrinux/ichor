package relay

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// In-cluster ServiceAccount files.
const (
	saDir       = "/var/run/secrets/kubernetes.io/serviceaccount"
	saTokenFile = saDir + "/token"
	saCAFile    = saDir + "/ca.crt"
	// SANamespaceFile holds the namespace the relay runs in.
	SANamespaceFile = saDir + "/namespace"

	kubeTimeout = 15 * time.Second
	// kubeMaxBody bounds an API server answer (a Secret is at most 1 MiB).
	kubeMaxBody = 2 << 20
)

// errNotFound and errConflict are the API server's 404 and 409.
var (
	errNotFound = errors.New("not found")
	errConflict = errors.New("conflict")
)

// Kube talks to the API server with plain REST, the way the apps do (no client-go).
type Kube struct {
	// Base is the API server URL, without a trailing slash.
	Base string
	// TokenFile is re-read on every call (projected tokens rotate); empty sends no token.
	TokenFile string
	Client    *http.Client
}

// InClusterKube is the API server as the relay's ServiceAccount sees it.
func InClusterKube() (*Kube, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, errors.New("not running in a cluster: KUBERNETES_SERVICE_HOST is unset (set RELAY_KUBE_API for a local run)")
	}

	ca, err := os.ReadFile(saCAFile)
	if err != nil {
		return nil, fmt.Errorf("read the ServiceAccount CA: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("the ServiceAccount CA holds no certificate")
	}

	return &Kube{
		Base:      "https://" + net.JoinHostPort(host, port),
		TokenFile: saTokenFile,
		Client:    newHTTPClient(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, nil, kubeTimeout),
	}, nil
}

// LocalKube is an API server reached without credentials, such as `kubectl proxy`.
func LocalKube(base string) *Kube {
	return &Kube{Base: strings.TrimRight(base, "/"), Client: newHTTPClient(nil, nil, kubeTimeout)}
}

// do sends a JSON request and decodes a 2xx answer into out (when not nil).
func (k *Kube) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, k.Base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if k.TokenFile != "" {
		token, err := os.ReadFile(k.TokenFile)
		if err != nil {
			return fmt.Errorf("read the ServiceAccount token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}

	resp, err := k.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, kubeMaxBody))
	if err != nil {
		return err
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotFound
	case resp.StatusCode == http.StatusConflict:
		return errConflict
	case resp.StatusCode/100 != 2:
		return fmt.Errorf("%s %s: %s", method, path, kubeStatusMessage(resp.StatusCode, data))
	case out == nil:
		return nil
	}

	return json.Unmarshal(data, out)
}

// kubeStatusMessage is the message of a Status answer, or the HTTP status.
func kubeStatusMessage(code int, data []byte) string {
	var status struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &status) == nil && status.Message != "" {
		return fmt.Sprintf("%d %s", code, status.Message)
	}

	return fmt.Sprintf("%d %s", code, http.StatusText(code))
}

// newHTTPClient builds a client with TLS 1.2 at least, bounded dials and no redirects, the
// settings the apps' client uses. proxy is nil for a direct connection.
func newHTTPClient(tlsConfig *tls.Config, proxy func(*http.Request) (*url.URL, error), timeout time.Duration) *http.Client {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		Proxy:               proxy,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}

	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
