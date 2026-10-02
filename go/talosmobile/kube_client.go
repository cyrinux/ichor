package talosmobile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// kubeProbeTimeout bounds the reachability check of one API server address.
	kubeProbeTimeout = 5 * time.Second
	// kubeClientTTL is how long a fetched admin kubeconfig is reused. Talos signs a new
	// certificate on every Kubeconfig call, so the app does not ask for one per request.
	kubeClientTTL = 30 * time.Minute
	// kubeMaxBody caps a response read from the API server.
	kubeMaxBody = 32 << 20
)

// kubeClient is a minimal Kubernetes REST client: JSON over HTTPS with the credentials of
// a kubeconfig. It replaces client-go, which would add megabytes to the app for a few calls.
type kubeClient struct {
	base  *url.URL
	http  *http.Client
	token string
}

// kubeAPIError is a non-2xx answer of the API server, with its Status message.
type kubeAPIError struct {
	Code    int
	Reason  string
	Message string
}

func (e *kubeAPIError) Error() string {
	switch e.Code {
	case http.StatusUnauthorized:
		return "Kubernetes API: authentication failed (" + e.Message + ")"
	case http.StatusForbidden:
		return "Kubernetes API: permission denied: " + e.Message
	case http.StatusNotFound:
		return "Kubernetes API: not found: " + e.Message
	default:
		return fmt.Sprintf("Kubernetes API (%d %s): %s", e.Code, e.Reason, e.Message)
	}
}

// openKubeClient builds a client for kubeconfig and picks the first API server address that
// answers: the kubeconfig's own server, then each Talos endpoint host on the same port. The
// server is often a VIP or an internal name the phone cannot reach while the Talos endpoints
// are; Talos puts every node address in the API server certificate, so TLS still verifies.
func openKubeClient(ctx context.Context, kubeconfig string, talosEndpoints []string) (*kubeClient, error) {
	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	var failures []string

	for _, base := range kubeServerCandidates(creds.server, talosEndpoints) {
		k := newKubeClient(base, creds)

		err := k.probe(ctx)
		if err == nil {
			return k, nil
		}

		var apiErr *kubeAPIError
		if errors.As(err, &apiErr) {
			// It answered: the address is right, the credentials are not.
			return nil, err
		}

		failures = append(failures, base.Host+": "+kubeTransportError(err))
	}

	return nil, errors.New("Kubernetes API not reachable from this device (" + strings.Join(failures, "; ") + ")")
}

// kubeServerCandidates lists server, then each Talos endpoint host with server's port.
func kubeServerCandidates(server *url.URL, talosEndpoints []string) []*url.URL {
	port := server.Port()
	if port == "" {
		port = "443"
	}

	out := []*url.URL{server}
	seen := []string{server.Host}

	for _, endpoint := range talosEndpoints {
		host := endpoint
		if h, _, err := net.SplitHostPort(endpoint); err == nil {
			host = h
		}

		host = strings.Trim(host, "[]")
		if host == "" {
			continue
		}

		addr := net.JoinHostPort(host, port)
		if slices.Contains(seen, addr) {
			continue
		}

		seen = append(seen, addr)
		out = append(out, &url.URL{Scheme: "https", Host: addr, Path: server.Path})
	}

	return out
}

func newKubeClient(base *url.URL, creds *kubeCredentials) *kubeClient {
	transport := &http.Transport{
		TLSClientConfig:     creds.tls.Clone(),
		DialContext:         (&net.Dialer{Timeout: kubeProbeTimeout}).DialContext,
		TLSHandshakeTimeout: kubeProbeTimeout,
		ForceAttemptHTTP2:   true,
		IdleConnTimeout:     sessionIdle,
	}

	return &kubeClient{base: base, http: &http.Client{Transport: transport}, token: creds.token}
}

func (k *kubeClient) probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, kubeProbeTimeout)
	defer cancel()

	var version struct {
		GitVersion string `json:"gitVersion"`
	}

	return k.do(ctx, http.MethodGet, "/version", "", nil, &version)
}

func (k *kubeClient) get(ctx context.Context, path string, out any) error {
	return k.do(ctx, http.MethodGet, path, "", nil, out)
}

func (k *kubeClient) patch(ctx context.Context, path, contentType string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode patch: %w", err)
	}

	return k.do(ctx, http.MethodPatch, path, contentType, data, out)
}

func (k *kubeClient) do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	// path is already escaped (url.PathEscape on names): keep it as the raw path.
	u := *k.base
	path, u.RawQuery, _ = strings.Cut(path, "?")
	u.RawPath = strings.TrimSuffix(k.base.EscapedPath(), "/") + path

	unescaped, err := url.PathUnescape(u.RawPath)
	if err != nil {
		return fmt.Errorf("bad Kubernetes API path %q: %w", path, err)
	}

	u.Path = unescaped

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ichor")

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	resp, err := k.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, kubeMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return kubeStatusError(resp.StatusCode, data)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode Kubernetes API answer: %w", err)
	}

	return nil
}

func kubeStatusError(code int, data []byte) error {
	var status struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
	}

	if json.Unmarshal(data, &status) != nil || status.Message == "" {
		status.Message = strings.TrimSpace(string(data))
		if len(status.Message) > 200 {
			status.Message = status.Message[:200]
		}
	}

	if status.Reason == "" {
		status.Reason = http.StatusText(code)
	}

	return &kubeAPIError{Code: code, Reason: status.Reason, Message: status.Message}
}

// kubeTransportError is a short reason for a request that got no HTTP answer.
func kubeTransportError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}

	if reason := dialFailure(err.Error()); reason != "" {
		return reason
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}

	return err.Error()
}

// kubeError is the message for any error of a Kubernetes call.
func kubeError(err error) error {
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) {
		return apiErr
	}

	return errors.New("Kubernetes API: " + kubeTransportError(err))
}

// kubeClients caches one client per (talosconfig, context) for kubeClientTTL.
var kubeClients = &kubeClientCache{entries: map[string]kubeClientEntry{}, open: openKubeClientForContext}

type kubeClientEntry struct {
	client  *kubeClient
	expires time.Time
}

type kubeClientCache struct {
	mu      sync.Mutex
	entries map[string]kubeClientEntry
	open    func(ctx context.Context, configYAML, contextName string) (*kubeClient, error)
}

func (c *kubeClientCache) get(ctx context.Context, configYAML, contextName string) (*kubeClient, error) {
	key := cacheKey(configYAML, contextName)

	c.mu.Lock()
	entry, ok := c.entries[key]
	c.mu.Unlock()

	if ok && time.Now().Before(entry.expires) {
		return entry.client, nil
	}

	k, err := c.open(ctx, configYAML, contextName)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[key] = kubeClientEntry{client: k, expires: time.Now().Add(kubeClientTTL)}
	c.mu.Unlock()

	return k, nil
}

// forget drops the cached client, so the next call fetches a new kubeconfig and probes again.
func (c *kubeClientCache) forget(configYAML, contextName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, cacheKey(configYAML, contextName))
}

func openKubeClientForContext(ctx context.Context, configYAML, contextName string) (*kubeClient, error) {
	return withSession(configYAML, contextName, callTimeout, func(sctx context.Context, s *session) (*kubeClient, error) {
		kubeconfig, err := fetchKubeconfig(sctx, s)
		if err != nil {
			return nil, err
		}

		return openKubeClient(ctx, kubeconfig, s.context.Endpoints)
	})
}

// withKube runs fn with the context's Kubernetes client. A call that gets no answer or is
// refused for its credentials drops the client: the next one fetches a fresh kubeconfig
// and looks for a reachable address again (the phone may have changed networks).
func withKube[T any](configYAML, contextName string, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	var zero T

	k, err := kubeClients.get(ctx, configYAML, contextName)
	if err != nil {
		return zero, err
	}

	out, err := fn(ctx, k)
	if err != nil {
		var apiErr *kubeAPIError
		if !errors.As(err, &apiErr) || apiErr.Code == http.StatusUnauthorized {
			kubeClients.forget(configYAML, contextName)
		}

		return zero, kubeError(err)
	}

	return out, nil
}
