package talosmobile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// enableMask turns the mask on with a fresh mapping and off again after the test.
func enableMask(t *testing.T, words string) {
	t.Helper()

	SetPrivacyMask(false, "")
	SetPrivacyMask(true, words)
	t.Cleanup(func() { SetPrivacyMask(false, "") })
}

func TestMaskIPv4(t *testing.T) {
	enableMask(t, "")

	for _, tc := range []struct{ in, want string }{
		{"node 192.168.1.10 up", "node 10.0.0.1 up"},
		{"192.168.1.10/24", "10.0.0.1/24"},
		{"192.168.1.10:50000", "10.0.0.1:50000"},
		{"https://192.168.1.11:6443/version", "https://10.0.0.2:6443/version"},
		{"via 192.168.1.11, 192.168.1.10.", "via 10.0.0.2, 10.0.0.1."},
		{"::ffff:192.168.1.10", "::ffff:10.0.0.1"},
		{`dial tcp 8.8.8.8:53: i/o timeout`, `dial tcp 10.0.0.3:53: i/o timeout`},
		// Not addresses, or not worth hiding.
		{"talos v1.14.1", "talos v1.14.1"},
		{"etcd 3.7.0", "etcd 3.7.0"},
		{"fw v1.2.3.4", "fw v1.2.3.4"},
		{"1.2.3.4.5", "1.2.3.4.5"},
		{"2026.10.01.12", "2026.10.01.12"},
		{"256.1.1.1", "256.1.1.1"},
		{"01.2.3.4", "01.2.3.4"},
		{"a1.2.3.4", "a1.2.3.4"},
		{"2026-10-01T12:34:56.789Z", "2026-10-01T12:34:56.789Z"},
		{"52:54:00:12:34:56", "52:54:00:12:34:56"},
		{"0.0.0.0/0", "0.0.0.0/0"},
		{"127.0.0.1:50000", "127.0.0.1:50000"},
		{"169.254.116.108", "169.254.116.108"},
		{"255.255.255.0", "255.255.255.0"},
		{"255.255.255.255", "255.255.255.255"},
		{"224.0.0.251", "224.0.0.251"},
		{"6.12.40-talos", "6.12.40-talos"},
	} {
		if got := privacy.maskPlain(tc.in); got != tc.want {
			t.Errorf("mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskIPv4Rollover(t *testing.T) {
	enableMask(t, "")

	privacy.nextV4 = 253

	if got := privacy.maskPlain("192.168.1.1 192.168.1.2 192.168.1.3"); got != "10.0.0.254 10.0.1.1 10.0.1.2" {
		t.Fatalf("got %q", got)
	}
}

func TestMaskIPv6(t *testing.T) {
	enableMask(t, "")

	for _, tc := range []struct{ in, want string }{
		{"2001:db8::1", "fd00::1"},
		{"[2001:DB8::1]:50000", "[fd00::1]:50000"},
		{"2001:db8::/32", "fd00::2/32"},
		{"inet6:2001:db8::5 up", "inet6:fd00::3 up"},
		{"addr 2001:db8::1.", "addr fd00::1."},
		{"fe80::1%eth0", "fe80::1%eth0"},
		{"::1", "::1"},
		{"::/0", "::/0"},
		{"ff02::1", "ff02::1"},
		{"std::string", "std::string"},
		{"12:34:56", "12:34:56"},
		{"52:54:00:ab:cd:ef", "52:54:00:ab:cd:ef"},
		{"sha256:3f4e5d6c7b8a", "sha256:3f4e5d6c7b8a"},
	} {
		if got := privacy.maskPlain(tc.in); got != tc.want {
			t.Errorf("mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskHostnames(t *testing.T) {
	enableMask(t, "")

	privacy.learnDomains("example.com", "lan")
	privacy.learnHosts([]hostEntry{
		{address: "192.168.1.30", hostname: "talos-w1", role: "worker"},
		{address: "192.168.1.12", hostname: "talos-cp2", role: "controlplane"},
		{address: "192.168.1.11", hostname: "Talos-CP1.example.com", role: "controlplane"},
		{address: "192.168.1.40", hostname: "odd", role: "unknown"},
	})
	privacy.learnHost("talos-cp1", "worker") // already known: keeps cp-1

	for _, tc := range []struct{ in, want string }{
		{"talos-cp1", "cp-1"},
		{"TALOS-CP2", "cp-2"},
		{"talos-w1", "worker-1"},
		{"odd", "node-1"},
		{"talos-cp1.example.com", "cp-1.homelab.lan"},
		{"talos-cp1.lan", "cp-1.homelab.lan"},
		{"kube-apiserver-talos-cp1", "kube-apiserver-cp-1"},
		{"talos-cp10", "talos-cp10"},
		{"lan party", "lan party"},
		{"lan", "homelab.lan"},
		{"search example.com", "search homelab.lan"},
		{"svc.cluster.local", "svc.cluster.local"},
		{"talos-cp2 at 192.168.1.12", "cp-2 at 10.0.0.2"},
	} {
		if got := privacy.maskPlain(tc.in); got != tc.want {
			t.Errorf("mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskContexts(t *testing.T) {
	enableMask(t, "")

	privacy.learnConfig(`context: cyril@acme
contexts:
  cyril@acme:
    endpoints: [192.168.1.10]
  admin@lab2:
    endpoints: ["https://192.168.2.10:50000"]
  default:
    endpoints: [talos.example.org]
  homelab-3:
    endpoints: [192.168.3.10]
`)

	// Sorted: admin@lab2, cyril@acme, default, homelab-3. "homelab-3" is a real name, so no
	// fake may reuse it; "default" is generic and stays.
	for real, want := range map[string]string{
		"admin@lab2": "admin@homelab",
		"cyril@acme": "user@homelab-2",
		"default":    "default",
		"homelab-3":  "homelab-4",
	} {
		if got := privacy.maskPlain(real); got != want {
			t.Errorf("mask(%q) = %q, want %q", real, got, want)
		}

		if got := privacy.unmaskContext(want); got != real {
			t.Errorf("unmaskContext(%q) = %q, want %q", want, got, real)
		}
	}

	// Addresses are numbered in config order; context parts are hidden in text too.
	for _, tc := range []struct{ in, want string }{
		{"192.168.2.10 192.168.1.10 192.168.3.10", "10.0.0.1 10.0.0.2 10.0.0.3"},
		{"namespace cyril-apps on acme", "namespace user-apps on homelab-2"},
		{"prod", "prod"},
		{"default via 10.0.0.1", "default via 10.0.0.4"},
		{"api.example.org", "api.homelab.lan"},
		{"talos.example.org", "talos.homelab.lan"},
	} {
		if got := privacy.maskPlain(tc.in); got != tc.want {
			t.Errorf("mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMaskExtraWords(t *testing.T) {
	enableMask(t, " Cyril , tracearr,,")

	for _, tc := range []struct{ in, want string }{
		{"cyril-tracearr", "redacted-redacted"},
		{"ns CYRIL/app", "ns redacted/app"},
		{"cyrillic", "cyrillic"},
		{"my_cyril.svc", "my_redacted.svc"},
	} {
		if got := privacy.maskPlain(tc.in); got != tc.want {
			t.Errorf("mask(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUnmaskNodes(t *testing.T) {
	enableMask(t, "")

	privacy.learnHosts([]hostEntry{{address: "192.168.1.11", hostname: "talos-cp1", role: "controlplane"}})
	privacy.maskPlain("192.168.1.12 2001:db8::7")

	for _, tc := range []struct{ in, want string }{
		{"10.0.0.1", "192.168.1.11"},
		{"10.0.0.2:50000", "192.168.1.12:50000"},
		{"fd00::1", "2001:db8::7"},
		{"CP-1", "talos-cp1"},
		{"10.9.9.9", "10.9.9.9"},
		{"", ""},
	} {
		if got := privacy.unmaskNode(tc.in); got != tc.want {
			t.Errorf("unmaskNode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	if got := privacy.unmaskNodes("10.0.0.1, 10.0.0.2"); got != "192.168.1.11,192.168.1.12" {
		t.Errorf("unmaskNodes = %q", got)
	}
}

func TestMaskJSONKeepsStructure(t *testing.T) {
	enableMask(t, "")
	privacy.learnHost("node1x", "worker")

	in := map[string]any{
		"node1x":  "node1x",
		"num":     192.168,
		"big":     uint64(1) << 60,
		"ip":      "192.168.1.10",
		"quoted":  `say "node1x" <b>`,
		"list":    []string{"10.1.2.3/24", "v1.14.1"},
		"escaped": "line\nnode1x\t192.168.1.10",
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	out := privacy.mask(string(raw))
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid JSON: %s", out)
	}

	var got map[string]any

	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()

	if err := dec.Decode(&got); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"node1x":  "worker-1",
		"num":     json.Number("192.168"),
		"big":     json.Number("1152921504606846976"),
		"ip":      "10.0.0.1",
		"quoted":  `say "worker-1" <b>`,
		"list":    []any{"10.0.0.2/24", "v1.14.1"},
		"escaped": "line\nworker-1\t10.0.0.1",
	}

	if !equalJSON(t, got, want) {
		t.Fail()
	}
}

func TestMaskDisabledIsIdentity(t *testing.T) {
	SetPrivacyMask(false, "")

	if PrivacyMaskEnabled() {
		t.Fatal("mask should be off")
	}

	privacy.learnHost("talos-cp1", "controlplane")
	privacy.learnConfig("context: a@b\ncontexts:\n  a@b:\n    endpoints: [192.168.1.1]\n")

	s := `{"node":"192.168.1.1","host":"talos-cp1","ctx":"a@b"}`
	if privacy.mask(s) != s || privacy.maskPlain(s) != s {
		t.Error("disabled mask changed the output")
	}

	if privacy.unmaskNode("10.0.0.1") != "10.0.0.1" || privacy.unmaskContext("homelab") != "homelab" {
		t.Error("disabled mask changed an argument")
	}

	err := errors.New("192.168.1.1 down")
	if maskErr(&err); err.Error() != "192.168.1.1 down" {
		t.Error("disabled mask changed an error")
	}
}

func TestSetPrivacyMaskResets(t *testing.T) {
	enableMask(t, "a")

	privacy.maskPlain("192.168.1.50")
	SetPrivacyMask(true, "A, ") // same words: mapping kept

	if got := privacy.maskPlain("192.168.1.60"); got != "10.0.0.2" {
		t.Errorf("same words should keep the mapping, got %q", got)
	}

	SetPrivacyMask(true, "b") // new words: fresh mapping

	if got := privacy.maskPlain("192.168.1.60"); got != "10.0.0.1" {
		t.Errorf("new words should reset the mapping, got %q", got)
	}

	SetPrivacyMask(false, "")
	SetPrivacyMask(true, "b")

	if got := privacy.unmaskNode("10.0.0.1"); got != "10.0.0.1" {
		t.Errorf("turning off should forget the mapping, got %q", got)
	}
}

func TestMaskErrAndListeners(t *testing.T) {
	enableMask(t, "")

	err := errors.New(`node "192.168.1.10" is not part of this context`)
	maskErr(&err)

	if err.Error() != `node "10.0.0.1" is not part of this context` {
		t.Errorf("maskErr = %q", err)
	}

	rec := &recordingEvents{}
	l := maskedEventListener{rec}
	l.OnEvent(`{"node":"192.168.1.10","message":"192.168.1.11"}`)
	l.OnDone("unreachable: 192.168.1.10")

	if rec.events[0] != `{"node":"10.0.0.1","message":"10.0.0.2"}` || rec.done != "unreachable: 10.0.0.1" {
		t.Errorf("listener got %q / %q", rec.events, rec.done)
	}
}

type recordingEvents struct {
	events []string
	done   string
}

func (r *recordingEvents) OnEvent(json string)      { r.events = append(r.events, json) }
func (r *recordingEvents) OnDone(errMessage string) { r.done = errMessage }

// End to end on a function that needs no cluster: masked summary out, masked name back in.
func TestParseConfigMaskedRoundTrip(t *testing.T) {
	enableMask(t, "")

	cfg := testConfig(t, time.Now().Add(time.Hour))

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var summary configSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	// "lab" is a generic name; "other" is not. The config's real 10.0.0.x addresses collide
	// with the fake range: they still map one to one.
	if summary.Current != "lab" || summary.Contexts[1].Name != "homelab" {
		t.Errorf("names: %+v", summary)
	}

	if got := strings.Join(summary.Contexts[0].Endpoints, ","); got != "10.0.0.1,talos.homelab.lan" {
		t.Errorf("endpoints: %s", got)
	}

	if got := strings.Join(summary.Contexts[1].Endpoints, ","); got != "10.0.0.4" {
		t.Errorf("endpoints: %s", got)
	}

	ctx, node := unmaskTarget(cfg, "homelab", "10.0.0.4")
	if ctx != "other" || node != "10.1.0.1" {
		t.Errorf("unmaskTarget = %q, %q", ctx, node)
	}

	if _, err := NodeServices(cfg, "homelab", "10.0.0.2"); err == nil || strings.Contains(err.Error(), "10.1.0.1") {
		t.Errorf("unexpected error: %v", err)
	}
}
