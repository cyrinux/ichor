package ichorgo

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHealthFailurePrefersLastProgress(t *testing.T) {
	got := healthFailure("waiting for etcd to be healthy: 10.0.0.3: service is not healthy: etcd", context.DeadlineExceeded)

	if !strings.Contains(got, "etcd") || strings.Contains(got, "reachable") {
		t.Errorf("got %q", got)
	}
}

func TestHealthFailureWithoutProgress(t *testing.T) {
	if got := healthFailure("", context.DeadlineExceeded); !strings.Contains(got, "timed out") {
		t.Errorf("got %q", got)
	}
}

func TestHealthFailurePermissionDeniedExplainsRole(t *testing.T) {
	err := status.Error(codes.PermissionDenied, "not authorized")

	got := healthFailure("discovered nodes: [...]", err)
	if !strings.Contains(got, "os:admin") || strings.Contains(got, "discovered") {
		t.Errorf("got %q", got)
	}
}
