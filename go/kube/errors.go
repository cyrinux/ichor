package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cyrinux/ichor/go/internal/dialerr"
	"github.com/cyrinux/ichor/go/internal/httpx"
)

// ListExpired starts the message of a 410 Gone: the continue token of a paged list expired
// (about 5 minutes) or the list changed too much. The apps recognise it by this prefix and
// load the list again from its first page, so it must not change.
const ListExpired = "Kubernetes API: list expired"

// APIError is a non-2xx answer of the API server, with its Status message.
type APIError struct {
	Code    int
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	switch e.Code {
	case http.StatusUnauthorized:
		return "Kubernetes API: authentication failed (" + e.Message + ")"
	case http.StatusForbidden:
		return "Kubernetes API: permission denied: " + e.Message
	case http.StatusNotFound:
		return "Kubernetes API: not found: " + e.Message
	case http.StatusGone:
		// The apps match this prefix to restart a paged list from its first page.
		return ListExpired + ": " + e.Message
	default:
		return fmt.Sprintf("Kubernetes API (%d %s): %s", e.Code, e.Reason, e.Message)
	}
}

// IsStatus tells the API server's own answer (a Status object) from the backend's: a
// Prometheus envelope has no kind. Only called on errors, whose bodies are small.
func IsStatus(body []byte) bool {
	var obj struct {
		Kind string `json:"kind"`
	}

	return json.Unmarshal(body, &obj) == nil && obj.Kind == "Status"
}

// Code is the HTTP status of an *APIError, 0 for any other error.
func Code(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return 0
}

// IsNotFound reports a 404.
func IsNotFound(err error) bool { return Code(err) == http.StatusNotFound }

// IsForbidden reports a 403.
func IsForbidden(err error) bool { return Code(err) == http.StatusForbidden }

// IgnoreNotFound drops a 404: the object is simply absent.
func IgnoreNotFound(err error) error {
	if IsNotFound(err) {
		return nil
	}

	return err
}

// StatusError is the *APIError of a non-2xx answer: the Status object's reason and message,
// or the first 200 bytes of a body that is not one.
func StatusError(code int, data []byte) error {
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

	return &APIError{Code: code, Reason: status.Reason, Message: status.Message}
}

// TransportError is a short reason for a request that got no HTTP answer.
func TransportError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}

	// Its own sentence: the host in it is the proxy's, not the API server's.
	var proxyErr *httpx.ProxyError
	if errors.As(err, &proxyErr) {
		return proxyErr.Error()
	}

	if reason := dialerr.Reason(err.Error()); reason != "" {
		return reason
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err.Error()
	}

	return err.Error()
}

// Error is the message for an error of a Kubernetes call: an API answer as it is (with what
// an operation wrapped around it), a transport failure as "Kubernetes API: <reason>", and
// anything else (an operation's own refusal) unchanged.
func Error(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		// What an operation wrapped around it ("worker-1 stays cordoned: …") is what the user
		// needs; errors.As still finds the API error through it.
		if err != error(apiErr) {
			return err
		}

		return apiErr
	}

	if IsTransportError(err) {
		return errors.New("Kubernetes API: " + TransportError(err))
	}

	// An operation's own refusal: not the API's doing.
	return err
}

// IsTransportError reports whether err comes from reaching the API server (dial, TLS,
// timeout, cancel) rather than from what an operation decided.
func IsTransportError(err error) bool {
	var urlErr *url.Error

	var netErr net.Error

	var proxyErr *httpx.ProxyError

	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		errors.As(err, &urlErr) || errors.As(err, &netErr) || errors.As(err, &proxyErr) || dialerr.Reason(err.Error()) != ""
}
