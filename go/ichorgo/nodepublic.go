package ichorgo

import (
	"context"
	"net/netip"
	"slices"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// cgnat is the shared address space (RFC 6598) carriers and Tailscale hand out: not public.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// probePublicIPs returns the node's internet-facing addresses, best effort (nil when none).
// A cloud node has them on its links or as platform external IPs, both in NodeAddress
// "current". A node behind NAT only knows the address the discovery service saw it come
// from, an AddressStatus in the cluster namespace (ID "service"), KubeSpan or not.
func probePublicIPs(ctx context.Context, c *client.Client) []string {
	var addrs []netip.Addr

	if na, err := safe.StateGetByID[*network.NodeAddress](ctx, c.COSI, network.NodeAddressCurrentID); err == nil {
		for _, p := range na.TypedSpec().Addresses {
			addrs = append(addrs, p.Addr())
		}
	}

	discovered, err := safe.StateList[*network.AddressStatus](ctx, c.COSI,
		resource.NewMetadata(cluster.NamespaceName, network.AddressStatusType, "", resource.VersionUndefined))
	if err == nil {
		for a := range discovered.All() {
			addrs = append(addrs, a.TypedSpec().Address.Addr())
		}
	}

	return publicAddresses(addrs)
}

// publicAddresses keeps the routable addresses (no private, CGNAT, ULA, link-local or
// loopback ones), deduplicated, IPv4 first.
func publicAddresses(in []netip.Addr) []string {
	var keep []netip.Addr

	for _, a := range in {
		a = a.Unmap()
		if !a.IsGlobalUnicast() || a.IsPrivate() || cgnat.Contains(a) || slices.Contains(keep, a) {
			continue
		}

		keep = append(keep, a)
	}

	slices.SortStableFunc(keep, func(x, y netip.Addr) int {
		switch {
		case x.Is4() == y.Is4():
			return 0
		case x.Is4():
			return -1
		default:
			return 1
		}
	})

	var out []string
	for _, a := range keep {
		out = append(out, a.String())
	}

	return out
}
