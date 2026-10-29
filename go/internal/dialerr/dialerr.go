// Package dialerr turns the message of a failed dial into a short reason a person can read
// on a phone: the raw transport message ("connection error: desc = \"transport: Error while
// dialing: dial tcp ...\"") is not. Shared by the Talos (gRPC) and Kubernetes (HTTP) sides.
package dialerr

import "strings"

// failures maps the cause of a failed dial to a short reason.
var failures = []struct{ needle, reason string }{
	{"i/o timeout", "no answer (timed out)"},
	{"connection refused", "connection refused"},
	{"no route to host", "no route to host"},
	{"network is unreachable", "network unreachable"},
	{"no such host", "unknown host name"},
	{"connection reset", "connection reset"},
}

// Reason returns a short reason when msg is a transport dial error, or "".
func Reason(msg string) string {
	if !strings.Contains(msg, "Error while dialing") && !strings.Contains(msg, "dial tcp") {
		return ""
	}

	for _, f := range failures {
		if strings.Contains(msg, f.needle) {
			return f.reason
		}
	}

	return "connection failed"
}
