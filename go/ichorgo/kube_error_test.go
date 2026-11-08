package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// kubeError keeps what an operation wrapped around a Kubernetes API error, shortens a bare API
// or transport error, and leaves an operation's own refusal as it is.
func TestKubeError(t *testing.T) {
	api := &kubeAPIError{Code: 500, Reason: "InternalError", Message: "InternalError"}
	wrapped := fmt.Errorf("worker-1 stays cordoned: uncordon it from its menu when ready: %w",
		fmt.Errorf("listing worker-1's pods: %w", api))
	signIn := &errSignInRequired{method: "oidc", reason: "token expired"}
	transport := &url.Error{Op: "Get", URL: "https://192.0.2.10:6443/api", Err: errors.New("connection refused")}
	refusal := errors.New("the cluster upgrade lock expired or was taken by another run")

	tests := []struct {
		name    string
		err     error
		want    string // the whole message, or its start when prefix is set
		prefix  bool
		wantAPI bool // errors.As still finds the API error
	}{
		{name: "bare API error", err: api, want: api.Error(), wantAPI: true},
		{name: "wrapped API error", err: wrapped, want: wrapped.Error(), wantAPI: true},
		{name: "sign-in required", err: signIn, want: signIn.Error()},
		{name: "wrapped sign-in required", err: fmt.Errorf("reading pods: %w", signIn), want: signIn.Error()},
		{name: "transport error", err: transport, want: "Kubernetes API: ", prefix: true},
		{name: "deadline", err: fmt.Errorf("listing pods: %w", context.DeadlineExceeded), want: "Kubernetes API: timed out"},
		{name: "canceled", err: context.Canceled, want: "Kubernetes API: context canceled"},
		{name: "operation refusal", err: refusal, want: refusal.Error()},
		{name: "wrapped refusal", err: fmt.Errorf("worker-1 stays cordoned: %w", refusal), want: "worker-1 stays cordoned: " + refusal.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kubeError(tt.err)
			if tt.prefix && !strings.HasPrefix(got.Error(), tt.want) || !tt.prefix && got.Error() != tt.want {
				t.Errorf("kubeError() = %q, want %q", got.Error(), tt.want)
			}

			var apiErr *kubeAPIError
			if errors.As(got, &apiErr) != tt.wantAPI {
				t.Errorf("errors.As(kubeAPIError) = %v, want %v", !tt.wantAPI, tt.wantAPI)
			}

			if tt.wantAPI && apiErr.Code != 500 {
				t.Errorf("API error code = %d", apiErr.Code)
			}
		})
	}
}
