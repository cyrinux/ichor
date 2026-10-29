package ichorgo

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
	"strings"
	"time"
)

// The Kubernetes transport: requests to the API server, their answers and errors. Nothing
// here knows a talosconfig context, the demo inventory, the privacy mask or the audit log;
// those live in the facade (kube_facade.go).

const (
	// kubeProbeTimeout bounds the reachability check of one API server address.
	kubeProbeTimeout = 5 * time.Second
	// kubeMaxBody caps a response read from the API server.
	kubeMaxBody = 32 << 20
)

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

func isForbidden(err error) bool { return kubeCode(err) == http.StatusForbidden }

// ignoreNotFound drops a 404: the object is simply absent.
func ignoreNotFound(err error) error {
	if isNotFound(err) {
		return nil
	}

	return err
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

	token, err := k.bearer(ctx)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
		// What an operation wrapped around it ("worker-1 stays cordoned: …") is what the user
		// needs; errors.As still finds the API error through it.
		if err != error(apiErr) {
			return err
		}

		return apiErr
	}

	var signIn *errSignInRequired
	if errors.As(err, &signIn) {
		return signIn
	}

	if isKubeTransportError(err) {
		return errors.New("Kubernetes API: " + kubeTransportError(err))
	}

	// An operation's own refusal: not the API's doing.
	return err
}

// isKubeTransportError reports whether err comes from reaching the API server (dial, TLS,
// timeout, cancel) rather than from what an operation decided.
func isKubeTransportError(err error) bool {
	var urlErr *url.Error

	var netErr net.Error

	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.As(err, &urlErr) || errors.As(err, &netErr) || dialFailure(err.Error()) != ""
}

// kubeList is the shape of a Kubernetes list response.
type kubeList[T any] struct {
	Items []T `json:"items"`
}
