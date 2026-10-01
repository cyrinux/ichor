package talosmobile

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// friendlyError turns gRPC/transport errors into a short message for the UI.
func friendlyError(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out (is the endpoint reachable from this network?)"
	}

	if reason := dialFailure(err.Error()); reason != "" {
		return "unreachable: " + reason
	}

	st, ok := status.FromError(err)
	if !ok {
		return err.Error()
	}

	msg := st.Message()

	switch st.Code() { //nolint:exhaustive
	case codes.Unavailable:
		if strings.Contains(msg, "certificate") || strings.Contains(msg, "tls") {
			return "TLS error: " + msg
		}

		return "unreachable: " + msg
	case codes.DeadlineExceeded:
		return "timed out (is the endpoint reachable from this network?)"
	case codes.PermissionDenied:
		return "permission denied (talosconfig role too limited): " + msg
	case codes.Unauthenticated:
		return "authentication failed (certificate expired or wrong CA?): " + msg
	case codes.Unknown:
		return msg
	default:
		return st.Code().String() + ": " + msg
	}
}

// dialFailures maps the cause of a failed gRPC dial to a short reason; the raw transport
// message ("connection error: desc = \"transport: Error while dialing: dial tcp ...\"") is
// unreadable on a phone.
var dialFailures = []struct{ needle, reason string }{
	{"i/o timeout", "no answer (timed out)"},
	{"connection refused", "connection refused"},
	{"no route to host", "no route to host"},
	{"network is unreachable", "network unreachable"},
	{"no such host", "unknown host name"},
	{"connection reset", "connection reset"},
}

// dialFailure returns a short reason when msg is a transport dial error, or "".
func dialFailure(msg string) string {
	if !strings.Contains(msg, "Error while dialing") && !strings.Contains(msg, "dial tcp") {
		return ""
	}

	for _, f := range dialFailures {
		if strings.Contains(msg, f.needle) {
			return f.reason
		}
	}

	return "connection failed"
}
