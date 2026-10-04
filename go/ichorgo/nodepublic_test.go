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

func TestBuildNodeOverviewPublicIPs(t *testing.T) {
	probe := nodeProbe{
		status:    &runtime.MachineStatusSpec{Stage: runtime.MachineStageRunning},
		publicIPs: []string{"203.0.113.7"},
	}

	if got := buildNodeOverview("10.0.0.2", probe).PublicIPs; !slices.Equal(got, []string{"203.0.113.7"}) {
		t.Errorf("got %v", got)
	}
}
