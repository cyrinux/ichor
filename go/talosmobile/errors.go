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
