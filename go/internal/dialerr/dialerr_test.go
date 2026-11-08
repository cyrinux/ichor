package dialerr

import "testing"

func TestReason(t *testing.T) {
	tests := []struct{ msg, want string }{
		{"rpc error: code = Unavailable desc = connection error: desc = \"transport: Error while dialing: dial tcp 192.0.2.1:50000: i/o timeout\"", "no answer (timed out)"},
		{"dial tcp 192.0.2.1:6443: connect: connection refused", "connection refused"},
		{"dial tcp: lookup api.example.invalid: no such host", "unknown host name"},
		{"dial tcp 192.0.2.1:6443: something else", "connection failed"},
		{"Kubernetes API: permission denied", ""},
	}

	for _, tt := range tests {
		if got := Reason(tt.msg); got != tt.want {
			t.Errorf("Reason(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}
