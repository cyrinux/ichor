package ichorgo

import (
	"bytes"
	"context"
	"crypto/tls"
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
	tls   *tls.Config // the kubeconfig's TLS settings, for connections outside http (exec)
	// namespace is the kubeconfig context's namespace, "" when it sets none.
	namespace string
}

// kubeListExpired starts the message of a 410 Gone: the continue token of a paged list
// expired (about 5 minutes) or the list changed too much. The apps recognise it by this
// prefix and load the list again from its first page, so it must not change.
const kubeListExpired = "Kubernetes API: list expired"

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
	case http.StatusGone:
		// The apps match this prefix to restart a paged list from its first page.
		return kubeListExpired + ": " + e.Message
	default:
		return fmt.Sprintf("Kubernetes API (%d %s): %s", e.Code, e.Reason, e.Message)
	}
}

// serviceProxyPath addresses a Service's HTTP port through the API server's proxy: service is
// "name" or "name:port" (already escaped), path starts with "/".
func serviceProxyPath(namespace, service, path string) string {
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/services/" + service + "/proxy" + path
}

// isKubeStatus tells the API server's own answer (a Status object) from the backend's: a
// Prometheus envelope has no kind. Only called on errors, whose bodies are small.
func isKubeStatus(body []byte) bool {
	var obj struct {
		Kind string `json:"kind"`
	}

	return json.Unmarshal(body, &obj) == nil && obj.Kind == "Status"
}

// kubeCode is the HTTP status of a kubeAPIError, 0 for any other error.
func kubeCode(err error) int {
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return 0
}

func isNotFound(err error) bool { return kubeCode(err) == http.StatusNotFound }

// ignoreNotFound drops a 404: the object is simply absent.
func ignoreNotFound(err error) error {
	if isNotFound(err) {
		return nil
	}

	return err
}

// openKubeClient builds a client for kubeconfig and picks the first API server address that
// answers: the kubeconfig's own server, then each Talos endpoint host on the same port. The
// server is often a VIP or an internal name the phone cannot reach while the Talos endpoints
// are; Talos puts every node address in the API server certificate, so TLS still verifies.
// A server the user set (see NormalizeKubeServer) is the only address tried.
func openKubeClient(ctx context.Context, kubeconfig string, talosEndpoints []string, server string) (*kubeClient, error) {
	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	candidates := []*url.URL{creds.server}

	switch {
	case server != "":
		if err := creds.applyKubeServer(server); err != nil {
			return nil, err
		}

		candidates = []*url.URL{creds.server}
	case !creds.tls.InsecureSkipVerify:
		// Without verification, another address could be anyone: only the configured one.
		candidates = kubeServerCandidates(creds.server, talosEndpoints)
	}

	// Probed in parallel (one probe timeout in all), the first candidate in order that
	// answers wins: the kubeconfig's own server is preferred when it works.
	clients := make([]*kubeClient, len(candidates))
	results := make([]chan error, len(candidates))

	for i, base := range candidates {
		clients[i], results[i] = newKubeClient(base, creds), make(chan error, 1)
		go func() { results[i] <- safeCall(func() error { return clients[i].probe(ctx) }) }()
	}

	// Once decided, the other probes finish in the background and their clients are closed.
	discardFrom := func(i int) {
		go func() {
			for j := i; j < len(results); j++ {
				<-results[j]
				clients[j].close()
			}
		}()
	}

	var failures []string

	for i, result := range results {
		err := <-result

		var apiErr *kubeAPIError

		switch {
		case err == nil:
			discardFrom(i + 1)

			return clients[i], nil
		case errors.As(err, &apiErr):
			// It answered: the address is right, the credentials are not.
			clients[i].close()
			discardFrom(i + 1)

			return nil, err
		default:
			clients[i].close()
			failures = append(failures, candidates[i].Host+": "+kubeTransportError(err))
		}
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

	return &kubeClient{base: base, http: &http.Client{
		Transport: transport,
		// The API server does not redirect; never send the credentials anywhere else.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, token: creds.token, tls: creds.tls.Clone(), namespace: creds.namespace}
}

// close drops the client's idle connections (its TLS sessions hold the client key).
func (k *kubeClient) close() { k.http.CloseIdleConnections() }

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

// endpoint is the URL of an API path below the client's base address.
func (k *kubeClient) endpoint(path string) (*url.URL, error) {
	// path is already escaped (url.PathEscape on names): keep it as the raw path.
	u := *k.base
	path, u.RawQuery, _ = strings.Cut(path, "?")
	u.RawPath = strings.TrimSuffix(k.base.EscapedPath(), "/") + path

	unescaped, err := url.PathUnescape(u.RawPath)
	if err != nil {
		return nil, fmt.Errorf("bad Kubernetes API path %q: %w", path, err)
	}

	u.Path = unescaped

	return &u, nil
}

// getRaw reads path whatever the HTTP status: a service proxy answers with the backend's
// own status and body, which the caller interprets. ctype is the answer's Content-Type.
// header is passed on to the backend (a Mimir tenant), nil for none.
func (k *kubeClient) getRaw(ctx context.Context, path string, header map[string]string) (status int, ctype string, body []byte, err error) {
	resp, data, err := k.send(ctx, http.MethodGet, path, "application/json", "", nil, header)
	if err != nil {
		return 0, "", nil, err
	}

	return resp.StatusCode, resp.Header.Get("Content-Type"), data, nil
}

func (k *kubeClient) post(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	return k.do(ctx, http.MethodPost, path, "application/json", data, out)
}

// getText reads a plain-text answer, such as a pod log.
func (k *kubeClient) getText(ctx context.Context, path string) (string, error) {
	resp, data, err := k.send(ctx, http.MethodGet, path, "text/plain, */*", "", nil, nil)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", kubeStatusError(resp.StatusCode, data)
	}

	return string(data), nil
}

func (k *kubeClient) do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	resp, data, err := k.send(ctx, method, path, "application/json", contentType, body, nil)
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

// send makes one request and reads its answer, capped at kubeMaxBody. header adds request
// headers, nil for none; it cannot replace the client's own.
func (k *kubeClient) send(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, []byte, error) {
	resp, err := k.request(ctx, method, path, accept, contentType, body, header)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, kubeMaxBody+1))
	if err != nil {
		return nil, nil, err
	}

	if len(data) > kubeMaxBody {
		return nil, nil, &kubeAPIError{Code: http.StatusRequestEntityTooLarge, Reason: "TooLarge", Message: "the answer is larger than the app reads (32 MiB)"}
	}

	return resp, data, nil
}

// stream GETs path and hands a successful answer's body to read as it arrives, uncapped:
// for an answer read line by line and mostly skipped (the API server's /metrics).
func (k *kubeClient) stream(ctx context.Context, path, accept string, read func(io.Reader) error) error {
	resp, err := k.request(ctx, http.MethodGet, path, accept, "", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) //nolint:errcheck

		return kubeStatusError(resp.StatusCode, data)
	}

	return read(resp.Body)
}

// request makes one request and returns the answer with its body unread.
func (k *kubeClient) request(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, error) {
	u, err := k.endpoint(path)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for name, value := range header {
		req.Header.Set(name, value)
	}

	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "ichor")

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	return k.http.Do(req)
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

// kubeTarget is what a Kubernetes client is opened for: a talosconfig context and the API
// server address the user set for it, if any.
type kubeTarget struct {
	config, context, server string
}

func (t kubeTarget) key() string { return cacheKey(t.config, t.context+"\x00"+t.server) }

// kubeClients caches one client per kubeTarget for kubeClientTTL.
var kubeClients = newKubeClientCache(openKubeClientForContext)

type kubeClientEntry struct {
	client  *kubeClient
	expires time.Time
}

// kubeOpening is an open in flight: concurrent callers wait for it instead of each asking
// Talos for a kubeconfig (every Kubeconfig call signs a new certificate).
type kubeOpening struct {
	done   chan struct{}
	client *kubeClient
	err    error
}

type kubeClientCache struct {
	mu      sync.Mutex
	entries map[string]kubeClientEntry
	opening map[string]*kubeOpening
	open    func(kubeTarget) (*kubeClient, error)
}

func newKubeClientCache(open func(kubeTarget) (*kubeClient, error)) *kubeClientCache {
	return &kubeClientCache{entries: map[string]kubeClientEntry{}, opening: map[string]*kubeOpening{}, open: open}
}

// get returns the cached client, or opens one; fresh tells the client was opened for this call.
func (c *kubeClientCache) get(target kubeTarget) (k *kubeClient, fresh bool, err error) {
	key := target.key()

	c.mu.Lock()
	c.sweepLocked()

	if entry, ok := c.entries[key]; ok {
		c.mu.Unlock()

		return entry.client, false, nil
	}

	if op, ok := c.opening[key]; ok {
		c.mu.Unlock()
		<-op.done

		return op.client, false, op.err
	}

	op := &kubeOpening{done: make(chan struct{})}
	c.opening[key] = op
	c.mu.Unlock()

	// Deferred so that a panic in open still releases the callers waiting on op.done.
	defer func() {
		if op.client == nil && op.err == nil {
			op.err = errors.New("kubernetes client not opened")
			err = op.err
		}

		c.mu.Lock()
		delete(c.opening, key)

		if op.err == nil {
			c.entries[key] = kubeClientEntry{client: op.client, expires: time.Now().Add(kubeClientTTL)}
		}
		c.mu.Unlock()
		close(op.done)
	}()

	op.client, op.err = c.open(target)

	return op.client, true, op.err
}

// sweepLocked drops expired clients: their keys should not stay in memory.
func (c *kubeClientCache) sweepLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			entry.client.close()
		}
	}
}

// forget drops k if it is still the cached client, so the next call fetches a new
// kubeconfig and probes again; a client another call already replaced it with stays.
func (c *kubeClientCache) forget(target kubeTarget, k *kubeClient) {
	key := target.key()

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok && entry.client == k {
		delete(c.entries, key)
		k.close()
	}
}

// openKubeClientForContext fetches an admin kubeconfig from Talos (bounded by callTimeout)
// and probes the API server addresses (bounded by kubeProbeTimeout).
func openKubeClientForContext(target kubeTarget) (*kubeClient, error) {
	var endpoints []string

	kubeconfig, err := withSession(target.config, target.context, callTimeout, func(ctx context.Context, s *session) (string, error) {
		endpoints = s.context.Endpoints

		return fetchKubeconfig(ctx, s)
	})
	if err != nil {
		return nil, err
	}

	return openKubeClient(context.Background(), kubeconfig, endpoints, target.server)
}

// kubeList is the shape of a Kubernetes list response.
type kubeList[T any] struct {
	Items []T `json:"items"`
}

// withKube runs fn with the context's Kubernetes client, bounded by callTimeout once the
// client is there. A call that gets no answer or is refused for its credentials drops the
// client: the next one fetches a fresh kubeconfig and looks for a reachable address again
// (the phone may have changed networks). A client just opened and probed is kept on a
// timeout: the address answered a moment ago, the call itself was slow.
func withKube[T any](target kubeTarget, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	k, fresh, err := kubeClients.get(target)
	if err != nil {
		var zero T

		return zero, err
	}

	// Started after opening: a cold open (kubeconfig fetch and probe) must not eat the budget.
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	return callKube(ctx, target, k, fresh, fn)
}

// withKubeContext is withKube bounded by ctx instead of callTimeout, for the calls that run
// longer (a network test). Cancelling ctx keeps the client: the address did not fail.
func withKubeContext[T any](ctx context.Context, target kubeTarget, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	var zero T

	k, fresh, err := kubeClients.get(target)
	if err != nil {
		return zero, err
	}

	return callKube(ctx, target, k, fresh, fn)
}

// callKube runs fn with k, dropping the client when the call shows it no longer works.
func callKube[T any](ctx context.Context, target kubeTarget, k *kubeClient, fresh bool, fn func(context.Context, *kubeClient) (T, error)) (T, error) {
	var zero T

	out, err := fn(ctx, k)
	if err != nil {
		var apiErr *kubeAPIError

		switch {
		case errors.As(err, &apiErr):
			if apiErr.Code == http.StatusUnauthorized {
				kubeClients.forget(target, k)
			}
		case errors.Is(err, context.Canceled):
		case fresh && errors.Is(err, context.DeadlineExceeded):
		default:
			kubeClients.forget(target, k)
		}

		return zero, kubeError(err)
	}

	return out, nil
}

// kubeReadJSON reads with fn (demo() in the demo inventory) and returns the result as JSON.
// target.context is the unmasked context name.
func kubeReadJSON[T any](target kubeTarget, demo func() T, fn func(context.Context, *kubeClient) (T, error)) (string, error) {
	if isDemoContext(target.config, target.context) {
		return toJSON(demo())
	}

	res, err := withKube(target, fn)
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// kubeMutate runs the action fn, refused in the demo inventory. target.context is the
// unmasked context name.
func kubeMutate(target kubeTarget, fn func(context.Context, *kubeClient) error) error {
	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	_, err := withKube(target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, fn(ctx, k)
	})

	return kubeMutationError(err)
}

// kubeMutationError explains an action that got no answer: the API server may have
// applied it anyway, so a blind retry could do it twice.
func kubeMutationError(err error) error {
	var apiErr *kubeAPIError
	if err == nil || errors.As(err, &apiErr) {
		return err
	}

	return fmt.Errorf("%w (it may have been applied: refresh before trying again)", err)
}
