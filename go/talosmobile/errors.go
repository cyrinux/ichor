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

	if isUnavailableAPI(err) {
		return notAvailable
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

// Error kinds let the apps tell "the phone can't reach the cluster" (VPN off, wrong LAN)
// from a cluster that answers but refuses us, without matching the English messages.
const (
	errorKindNetwork = "network"
	errorKindTLS     = "tls"
	errorKindAuth    = "auth"
	errorKindOther   = "other"
)

// errorKind classifies err along the same lines as friendlyError; "" for nil.
func errorKind(err error) string {
	if err == nil {
		return ""
	}

	if errors.Is(err, context.DeadlineExceeded) || dialFailure(err.Error()) != "" {
		return errorKindNetwork
	}

	st, ok := status.FromError(err)
	if !ok {
		return errorKindOther
	}

	switch st.Code() { //nolint:exhaustive
	case codes.Unavailable:
		if msg := st.Message(); strings.Contains(msg, "certificate") || strings.Contains(msg, "tls") {
			return errorKindTLS
		}

		return errorKindNetwork
	case codes.DeadlineExceeded:
		return errorKindNetwork
	case codes.PermissionDenied, codes.Unauthenticated:
		return errorKindAuth
	default:
		return errorKindOther
	}
}

// notAvailable is the message for an API or resource type the node's Talos version lacks.
const notAvailable = "not available on this node's Talos version"

// notAvailableOn is notAvailable naming the node's version when it is known.
func notAvailableOn(version string) string {
	if version == "" {
		return notAvailable
	}

	return notAvailable + " (" + version + ")"
}

// isUnavailableAPI tells whether err means the server does not know the gRPC service or
// method (older or newer Talos than this client), or the COSI resource type.
func isUnavailableAPI(err error) bool {
	if err == nil {
		return false
	}

	if status.Code(err) == codes.Unimplemented {
		return true
	}

	msg := err.Error()

	for _, needle := range []string{
		"unknown service", "unknown method", // grpc-go's Unimplemented text, kept when the code is lost in wrapping
		"is not registered",          // client.ResolveResourceKind / COSI: unknown resource type
		"unknown resource type",      //
		"resource type is not known", //
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}

	return false
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
