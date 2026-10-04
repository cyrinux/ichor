package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestParsePublicIP(t *testing.T) {
	cases := []struct {
		log, want, err string
	}{
		{"203.0.113.9\n", "203.0.113.9", ""},
		{"2001:db8::7", "2001:db8::7", ""},
		{"curl: (28) Operation timed out\ncurl: (22) 429\n203.0.113.9\n", "203.0.113.9", ""}, // a fallback answered
		{"10.0.0.5", "", "not a public address"},
		{"<html>rate limited</html>", "", "unexpected answer"},
		{"", "", "unexpected answer"},
	}

	for _, c := range cases {
		got, err := parsePublicIP(c.log)
		if got != c.want || (c.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%q: got %q, %v", c.log, got, err)
		}
	}
}

func TestPublicIPCommand(t *testing.T) {
	cmd := publicIPCommand()
	if cmd[0] != "sh" || cmd[1] != "-c" || !slices.Equal(cmd[4:], publicIPServices) {
		t.Fatalf("command %q", cmd)
	}
}

func TestDetectPublicIPs(t *testing.T) {
	f := newFakeNetPerfAPI(t)

	report, err := detectPublicIPs(context.Background(), f.client(t))
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]publicIPNode{}
	for _, n := range report.Nodes {
		got[n.Name] = n
	}

	if got["node-a"].PublicIP != "203.0.113.9" || got["node-b"].PublicIP != "203.0.113.9" {
		t.Fatalf("nodes %+v", report.Nodes)
	}

	if down := got["node-down"]; down.PublicIP != "" || down.Error != "node not ready" {
		t.Fatalf("node-down %+v", down)
	}

	var ns string

	for name, body := range f.posted {
		switch {
		case strings.HasPrefix(name, publicIPName+"-"):
			ns = name
			if strings.Contains(body, "privileged") {
				t.Errorf("namespace should stay restricted: %s", body)
			}
		case strings.HasPrefix(name, "probe-"):
			var pod struct {
				Spec struct {
					HostNetwork bool `json:"hostNetwork"`
					Containers  []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			}
			if err := json.Unmarshal([]byte(body), &pod); err != nil || pod.Spec.HostNetwork || pod.Spec.Containers[0].Image != publicIPImage {
				t.Errorf("probe pod %s: %s", name, body)
			}
		}
	}

	if ns == "" || !slices.Contains(f.deleted, ns) {
		t.Fatalf("namespace %q not deleted (%v)", ns, f.deleted)
	}

	if _, ok := f.posted["probe-2"]; ok {
		t.Error("a pod was started on the node that is not ready")
	}
}

func TestDemoPublicIPs(t *testing.T) {
	r := demoPublicIPs()
	if len(r.Nodes) != 5 || r.Nodes[3].PublicIP != demoNodes()[3].PublicIPs[0] {
		t.Fatalf("demo %+v", r.Nodes)
	}
}
