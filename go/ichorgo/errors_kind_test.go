package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestErrorKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"deadline", fmt.Errorf("version: %w", context.DeadlineExceeded), errorKindNetwork},
		{"grpc deadline", status.Error(codes.DeadlineExceeded, "slow"), errorKindNetwork},
		{"dial", errors.New(`transport: Error while dialing: dial tcp 192.0.2.7:50000: i/o timeout`), errorKindNetwork},
		{"unavailable", status.Error(codes.Unavailable, "service restarting"), errorKindNetwork},
		{"tls", status.Error(codes.Unavailable, "x509: certificate signed by unknown authority"), errorKindTLS},
		{"unauthenticated", status.Error(codes.Unauthenticated, "bad cert"), errorKindAuth},
		{"permission", status.Error(codes.PermissionDenied, "role"), errorKindAuth},
		{"other", status.Error(codes.Internal, "boom"), errorKindOther},
	}

	for _, c := range cases {
		if got := errorKind(c.err); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
