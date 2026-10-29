package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAPIErrorMessages(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{http.StatusUnauthorized, "Kubernetes API: authentication failed (msg)"},
		{http.StatusForbidden, "Kubernetes API: permission denied: msg"},
		{http.StatusNotFound, "Kubernetes API: not found: msg"},
		{http.StatusGone, ListExpired + ": msg"},
		{http.StatusConflict, "Kubernetes API (409 Conflict): msg"},
	}

	for _, tt := range tests {
		err := &APIError{Code: tt.code, Reason: http.StatusText(tt.code), Message: "msg"}
		if err.Error() != tt.want {
			t.Errorf("%d: %q, want %q", tt.code, err.Error(), tt.want)
		}
	}
}

func TestStatusError(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		body       string
		wantReason string
		wantMsg    string
	}{
		{"status object", 409, `{"kind":"Status","reason":"AlreadyExists","message":"exists"}`, "AlreadyExists", "exists"},
		{"no reason", 500, `{"message":"boom"}`, "Internal Server Error", "boom"},
		{"plain text", 502, "  bad gateway \n", "Bad Gateway", "bad gateway"},
		{"long text is cut", 503, strings.Repeat("x", 300), "Service Unavailable", strings.Repeat("x", 200)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var apiErr *APIError
			if !errors.As(StatusError(tt.code, []byte(tt.body)), &apiErr) {
				t.Fatal("not an APIError")
			}

			if apiErr.Code != tt.code || apiErr.Reason != tt.wantReason || apiErr.Message != tt.wantMsg {
				t.Fatalf("got %+v", apiErr)
			}
		})
	}
}

func TestCodesAndIgnoreNotFound(t *testing.T) {
	notFound := fmt.Errorf("read: %w", &APIError{Code: http.StatusNotFound})
	forbidden := &APIError{Code: http.StatusForbidden}
	other := errors.New("other")

	if Code(notFound) != 404 || Code(other) != 0 || !IsNotFound(notFound) || IsNotFound(forbidden) || !IsForbidden(forbidden) {
		t.Fatal("codes are wrong")
	}

	if IgnoreNotFound(notFound) != nil || IgnoreNotFound(other) != other {
		t.Fatal("IgnoreNotFound is wrong")
	}
}

func TestIsStatus(t *testing.T) {
	if !IsStatus([]byte(`{"kind":"Status"}`)) || IsStatus([]byte(`{"status":"error"}`)) || IsStatus([]byte("x")) {
		t.Fatal("IsStatus is wrong")
	}
}

func TestErrorAndTransportError(t *testing.T) {
	apiErr := &APIError{Code: http.StatusConflict, Reason: "Conflict", Message: "busy"}
	wrapped := fmt.Errorf("worker-1 stays cordoned: %w", apiErr)
	dial := &url.Error{Op: "Get", URL: "https://api.example.invalid", Err: errors.New("dial tcp 192.0.2.1:6443: connect: connection refused")}
	refusal := errors.New("the node is the last control plane")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"api error", apiErr, apiErr.Error()},
		{"wrapped api error keeps its wrapper", wrapped, wrapped.Error()},
		{"deadline", fmt.Errorf("get: %w", context.DeadlineExceeded), "Kubernetes API: timed out"},
		{"dial", dial, "Kubernetes API: connection refused"},
		{"url error", &url.Error{Op: "Get", URL: "u", Err: errors.New("tls: bad certificate")}, "Kubernetes API: tls: bad certificate"},
		{"an operation's own refusal", refusal, refusal.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Error(tt.err).Error(); got != tt.want {
				t.Fatalf("Error = %q, want %q", got, tt.want)
			}
		})
	}

	if IsTransportError(refusal) || !IsTransportError(context.Canceled) {
		t.Fatal("IsTransportError is wrong")
	}
}
