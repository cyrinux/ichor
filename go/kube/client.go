package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// ProbeTimeout bounds the reachability check of one API server address.
	ProbeTimeout = 5 * time.Second
	// MaxBody caps a response read from the API server.
	MaxBody = 32 << 20
)

// Client is a minimal Kubernetes REST client: JSON over HTTPS to the API server at Base,
// with HTTP's TLS settings (the kubeconfig's) and, when Token is set, a bearer token.
type Client struct {
	Base *url.URL
	HTTP *http.Client
	// Token returns the bearer token to send, "" for none (a client certificate). Nil sends none.
	Token func(context.Context) (string, error)
}

// ServiceProxyPath addresses a Service's HTTP port through the API server's proxy: service is
// "name" or "name:port" (already escaped), path starts with "/".
func ServiceProxyPath(namespace, service, path string) string {
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/services/" + service + "/proxy" + path
}

// List is the shape of a Kubernetes list response.
type List[T any] struct {
	Items []T `json:"items"`
}

// Probe checks that the API server answers, within ProbeTimeout.
func (c Client) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	var version struct {
		GitVersion string `json:"gitVersion"`
	}

	return c.Do(ctx, http.MethodGet, "/version", "", nil, &version)
}

// Get reads path and decodes its JSON answer into out (nil to drop it).
func (c Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, "", nil, out)
}

// Patch sends body as a patch of contentType (merge, strategic, JSON patch, apply).
func (c Client) Patch(ctx context.Context, path, contentType string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode patch: %w", err)
	}

	return c.Do(ctx, http.MethodPatch, path, contentType, data, out)
}

// Endpoint is the URL of an API path below the client's base address.
func (c Client) Endpoint(path string) (*url.URL, error) {
	// path is already escaped (url.PathEscape on names): keep it as the raw path.
	u := *c.Base
	path, u.RawQuery, _ = strings.Cut(path, "?")
	u.RawPath = strings.TrimSuffix(c.Base.EscapedPath(), "/") + path

	unescaped, err := url.PathUnescape(u.RawPath)
	if err != nil {
		return nil, fmt.Errorf("bad Kubernetes API path %q: %w", path, err)
	}

	u.Path = unescaped

	return &u, nil
}

// GetRaw reads path whatever the HTTP status: a service proxy answers with the backend's
// own status and body, which the caller interprets. ctype is the answer's Content-Type.
// header is passed on to the backend (a Mimir tenant), nil for none.
func (c Client) GetRaw(ctx context.Context, path string, header map[string]string) (status int, ctype string, body []byte, err error) {
	resp, data, err := c.Send(ctx, http.MethodGet, path, "application/json", "", nil, header)
	if err != nil {
		return 0, "", nil, err
	}

	return resp.StatusCode, resp.Header.Get("Content-Type"), data, nil
}

// Post sends body as JSON and decodes the JSON answer into out (nil to drop it).
func (c Client) Post(ctx context.Context, path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	return c.Do(ctx, http.MethodPost, path, "application/json", data, out)
}

// GetText reads a plain-text answer, such as a pod log.
func (c Client) GetText(ctx context.Context, path string) (string, error) {
	resp, data, err := c.Send(ctx, http.MethodGet, path, "text/plain, */*", "", nil, nil)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", StatusError(resp.StatusCode, data)
	}

	return string(data), nil
}

// Do makes one JSON request: a non-2xx answer is an *APIError, a 2xx one is decoded into
// out (nil to drop it).
func (c Client) Do(ctx context.Context, method, path, contentType string, body []byte, out any) error {
	resp, data, err := c.Send(ctx, method, path, "application/json", contentType, body, nil)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return StatusError(resp.StatusCode, data)
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode Kubernetes API answer: %w", err)
	}

	return nil
}

// Send makes one request and reads its answer, capped at MaxBody. header adds request
// headers, nil for none; it cannot replace the client's own.
func (c Client) Send(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, []byte, error) {
	resp, err := c.Request(ctx, method, path, accept, contentType, body, header)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, nil, err
	}

	if len(data) > MaxBody {
		return nil, nil, &APIError{Code: http.StatusRequestEntityTooLarge, Reason: "TooLarge", Message: "the answer is larger than the app reads (32 MiB)"}
	}

	return resp, data, nil
}

// Stream GETs path and hands a successful answer's body to read as it arrives, uncapped:
// for an answer read line by line and mostly skipped (the API server's /metrics).
func (c Client) Stream(ctx context.Context, path, accept string, read func(io.Reader) error) error {
	resp, err := c.Request(ctx, http.MethodGet, path, accept, "", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) //nolint:errcheck

		return StatusError(resp.StatusCode, data)
	}

	return read(resp.Body)
}

// Request makes one request and returns the answer with its body unread.
func (c Client) Request(ctx context.Context, method, path, accept, contentType string, body []byte, header map[string]string) (*http.Response, error) {
	u, err := c.Endpoint(path)
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

	if c.Token != nil {
		token, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	return c.HTTP.Do(req)
}
