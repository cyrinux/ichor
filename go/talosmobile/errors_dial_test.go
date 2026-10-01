package talosmobile

import (
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFriendlyErrorDialFailures(t *testing.T) {
	cases := map[string]string{
		`connection error: desc = "transport: Error while dialing: dial tcp 192.0.2.7:50000: i/o timeout"`:                 "unreachable: no answer (timed out)",
		`connection error: desc = "transport: Error while dialing: dial tcp 192.0.2.7:50000: connect: no route to host"`:   "unreachable: no route to host",
		`connection error: desc = "transport: Error while dialing: dial tcp 192.0.2.7:50000: connect: connection refused"`: "unreachable: connection refused",
		`connection error: desc = "transport: Error while dialing: something new"`:                                         "unreachable: connection failed",
	}

	for msg, want := range cases {
		if got := friendlyError(status.Error(codes.Unavailable, msg)); got != want {
			t.Errorf("status %q: got %q, want %q", msg, got, want)
		}

		// Per-node event stream failures arrive as plain strings.
		if got := friendlyError(errors.New(msg)); got != want {
			t.Errorf("plain %q: got %q, want %q", msg, got, want)
		}
	}

	if got := friendlyError(status.Error(codes.Unavailable, "service restarting")); got != "unreachable: service restarting" {
		t.Errorf("non-dial Unavailable changed: %q", got)
	}
}
