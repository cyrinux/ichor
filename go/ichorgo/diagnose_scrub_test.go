package ichorgo

import (
	"strings"
	"testing"
)

func TestScrubSecrets(t *testing.T) {
	hex64 := strings.Repeat("0a1b2c3d", 8)
	base64Run := strings.Repeat("aB3dE6gH9k", 5)
	jwt := "eyJhbGciOiJub25lIn0.eyJzdWIiOiJzYW1wbGUifQ.c2lnbmF0dXJl"

	for _, tc := range []struct{ name, in, want string }{
		{"bearer", "got Bearer abc123def456 back", "got Bearer [redacted] back"},
		{"bearer of letters only, long", "Bearer " + strings.Repeat("abcde", 5), "Bearer [redacted]"},
		{"authorization", "Authorization: Basic c2FtcGxl, next", "Authorization: [redacted], next"},
		{"authorization with bearer", "authorization: Bearer abc123", "authorization: [redacted]"},
		{"authorization in json", `{"Authorization":"Bearer abc123"}`, `{"Authorization":"[redacted]"}`},
		{"token", "url?token=abc123&page=2", "url?token=[redacted]&page=2"},
		{"prefixed token", "auth_token=abc123 ok", "auth_token=[redacted] ok"},
		{"password", `password="hunter-two" user=sample`, `password="[redacted]" user=sample`},
		{"password in json", `"password": "hunter-two"`, `"password": "[redacted]"`},
		{"apikey", "api_key=abc123 apikey=def456", "api_key=[redacted] apikey=[redacted]"},
		{"token holding a bearer", `token="Bearer abc123" next`, `token="[redacted]" next`},
		{"aws", "key AKIA" + strings.Repeat("Q7", 8) + " used", "key [redacted] used"},
		{"github", "ghp_" + strings.Repeat("aB3", 12) + " used", "[redacted] used"},
		{"github server", "ghs_" + strings.Repeat("aB3", 13), "[redacted]"},
		{"jwt", "cookie " + jwt + " sent", "cookie [redacted] sent"},
		{"pem", "a\n-----BEGIN RSA PRIVATE KEY-----\nMIIE\nabcd\n-----END RSA PRIVATE KEY-----\nb", "a\n[redacted]\nb"},
		{"pem without its end", "-----BEGIN PRIVATE KEY-----\n" + base64Run, "[redacted]\n[redacted]"},
		{"hex run", "value " + hex64 + " seen", "value [redacted] seen"},
		{"base64 run", "value " + base64Run + "== seen", "value [redacted] seen"},
		{"base64 run with a slash", "value " + base64Run[:24] + "/" + base64Run[:24] + " seen", "value [redacted] seen"},

		// Not secrets.
		{"uuid", "pod 1b4e28ba-2fa1-11d2-883f-0016d3cca427 gone", "pod 1b4e28ba-2fa1-11d2-883f-0016d3cca427 gone"},
		{"containerd id", `containerID="containerd://` + hex64 + `"`, `containerID="containerd://` + hex64 + `"`},
		{"cri-o id", "cri-o://" + hex64, "cri-o://" + hex64},
		{"docker id", "docker://" + hex64, "docker://" + hex64},
		{"digest", "ghcr.io/sample/app@sha256:" + hex64, "ghcr.io/sample/app@sha256:" + hex64},
		{"prose bearer", "the bearer token expired", "the bearer token expired"},
		{"prose token", "failed to get token: connection refused", "failed to get token: connection refused"},
		{"long path", "/var/lib/kubelet/pods/volumes/kubernetes/projected/serviceaccount", "/var/lib/kubelet/pods/volumes/kubernetes/projected/serviceaccount"},
		{"long pod name", "acme-billing-invoice-worker-processor-5d8f7c9b6d-x2x4z", "acme-billing-invoice-worker-processor-5d8f7c9b6d-x2x4z"},
		{"short hex", "member 8e9e05c52164694d", "member 8e9e05c52164694d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrubSecrets(tc.in); got != tc.want {
				t.Errorf("scrubSecrets(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReportCountsWhatWasScrubbed(t *testing.T) {
	data := sampleDiagnosis()

	if strings.Contains(renderDiagnosis(data), "secrets-like") {
		t.Error("a clean report has no counter")
	}

	data.Nodes[1].Logs = []serviceLogTail{{Service: "kubelet", Lines: []string{"sent Bearer abc123def456", "password=hunter-two"}}}
	data.Events = []nodeEvent{{Node: "192.0.2.20", Kind: "service", Subject: "kubelet", Message: "token=abc123", Severity: "error"}}

	report := renderDiagnosis(data)

	for _, leak := range []string{"abc123", "hunter-two"} {
		if strings.Contains(report, leak) {
			t.Errorf("report shows %q:\n%s", leak, report)
		}
	}

	if !strings.HasSuffix(report, "\n\n3 secrets-like strings redacted\n") {
		t.Errorf("report lacks the counter as its last line:\n%s", report)
	}

	health := renderHealthExplanation(nil, "", data)
	if strings.Contains(health, "token=abc123") || !strings.Contains(health, "1 secrets-like strings redacted") {
		t.Errorf("health report:\n%s", health)
	}
}
