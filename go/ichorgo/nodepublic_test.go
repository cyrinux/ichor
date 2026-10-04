package ichorgo

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

func TestPublicAddresses(t *testing.T) {
	in := []netip.Addr{
		netip.MustParseAddr("2001:db8::10"),       // global IPv6: public
		netip.MustParseAddr("10.0.0.2"),           // RFC 1918
		netip.MustParseAddr("192.168.1.4"),        // RFC 1918
		netip.MustParseAddr("100.72.1.9"),         // CGNAT / Tailscale
		netip.MustParseAddr("fd00::1"),            // ULA
		netip.MustParseAddr("fe80::1"),            // link-local
		netip.MustParseAddr("169.254.1.1"),        // link-local
		netip.MustParseAddr("127.0.0.1"),          // loopback
		netip.MustParseAddr("203.0.113.7"),        // public
		netip.MustParseAddr("::ffff:203.0.113.7"), // same address, mapped: deduplicated
		{}, // zero value: skipped
	}

	got := publicAddresses(in)
	want := []string{"203.0.113.7", "2001:db8::10"} // IPv4 first

	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPublicAddressesNone(t *testing.T) {
	if got := publicAddresses([]netip.Addr{netip.MustParseAddr("10.1.2.3")}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestWithPeerSeenPublicIPs(t *testing.T) {
	nodes := []nodeOverview{
		{Node: "10.0.0.2", Hostname: "cp-1", PublicIPs: []string{"2001:db8::2"}},
		{Node: "10.0.0.3", Hostname: "edge-1.example.org"}, // labelled by its short name
		{Node: "10.0.0.4", Hostname: "lan-1"},              // only seen on the LAN
	}
	seen := []map[string][]netip.Addr{
		{"edge-1": {netip.MustParseAddr("198.51.100.9")}, "lan-1": {netip.MustParseAddr("10.0.0.4")}},
		{"cp-1": {netip.MustParseAddr("203.0.113.2")}, "edge-1": {netip.MustParseAddr("198.51.100.9")}},
		nil, // a node without KubeSpan
	}

	got := withPeerSeenPublicIPs(nodes, seen)

	want := [][]string{{"203.0.113.2", "2001:db8::2"}, {"198.51.100.9"}, nil}
	for i := range want {
		if !slices.Equal(got[i].PublicIPs, want[i]) {
			t.Errorf("%s: got %v, want %v", got[i].Hostname, got[i].PublicIPs, want[i])
		}
	}

	if !slices.Equal(nodes[0].PublicIPs, []string{"2001:db8::2"}) {
		t.Errorf("input mutated: %v", nodes[0].PublicIPs)
	}
}

func TestWithPeerSeenPublicIPsAmbiguousShortName(t *testing.T) {
	nodes := []nodeOverview{{Node: "10.0.0.5", Hostname: "edge-1"}, {Node: "10.0.0.6", Hostname: "edge-1.b.org"}}
	seen := []map[string][]netip.Addr{{
		"edge-1.a.org": {netip.MustParseAddr("198.51.100.1")},
		"edge-1.b.org": {netip.MustParseAddr("198.51.100.2")},
	}}

	got := withPeerSeenPublicIPs(nodes, seen)

	if got[0].PublicIPs != nil || !slices.Equal(got[1].PublicIPs, []string{"198.51.100.2"}) {
		t.Errorf("got %v and %v", got[0].PublicIPs, got[1].PublicIPs)
	}
}

func TestBuildNodeOverviewPublicIPs(t *testing.T) {
	probe := nodeProbe{
		status:    &runtime.MachineStatusSpec{Stage: runtime.MachineStageRunning},
		publicIPs: []string{"203.0.113.7"},
	}

	if got := buildNodeOverview("10.0.0.2", probe).PublicIPs; !slices.Equal(got, []string{"203.0.113.7"}) {
		t.Errorf("got %v", got)
	}
}
