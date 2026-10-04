package ichorgo

import (
	"context"
	"net/netip"
	"slices"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// cgnat is the shared address space (RFC 6598) carriers and Tailscale hand out: not public.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// probePublicIPs returns the node's internet-facing addresses as it knows them, best effort
// (nil when none). A cloud node has them on its links or as platform external IPs, both in
// NodeAddress "current". A node behind NAT knows the address the discovery service saw it
// come from, an AddressStatus in the cluster namespace (ID "service"), KubeSpan or not.
// With KubeSpan, its own affiliate also announces endpoints, extraAnnouncedEndpoints
// included (a port forwarded on the router).
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

	return publicAddresses(append(addrs, announcedEndpoints(ctx, c)...))
}

// announcedEndpoints are the KubeSpan endpoint addresses the node announces to its peers
// (nil without KubeSpan): its own affiliate, found through its cluster identity.
func announcedEndpoints(ctx context.Context, c *client.Client) []netip.Addr {
	id, err := safe.StateGetByID[*cluster.Identity](ctx, c.COSI, cluster.LocalIdentity)
	if err != nil {
		return nil
	}

	self, err := safe.StateGetByID[*cluster.Affiliate](ctx, c.COSI, id.TypedSpec().NodeID)
	if err != nil {
		return nil
	}

	var out []netip.Addr
	for _, e := range self.TypedSpec().KubeSpan.Endpoints {
		out = append(out, e.Addr())
	}

	return out
}

// probePeerEndpoints maps each KubeSpan peer that is up (by its label, the peer's node name)
// to the address this node reaches it at: for a peer behind NAT on another site, its public
// IP as WireGuard sees it, even with the discovery service off. Nil without KubeSpan.
func probePeerEndpoints(ctx context.Context, c *client.Client) map[string][]netip.Addr {
	peers, err := safe.StateListAll[*kubespan.PeerStatus](ctx, c.COSI)
	if err != nil {
		return nil
	}

	var out map[string][]netip.Addr

	for p := range peers.All() {
		spec := p.TypedSpec()
		if spec.State != kubespan.PeerStateUp || !spec.Endpoint.IsValid() || spec.Label == "" {
			continue
		}

		if out == nil {
			out = map[string][]netip.Addr{}
		}

		out[spec.Label] = append(out[spec.Label], spec.Endpoint.Addr())
	}

	return out
}

// withPeerSeenPublicIPs adds to each node the public addresses its KubeSpan peers reach it
// at (seen holds one probePeerEndpoints result per node), after the ones it reported itself.
func withPeerSeenPublicIPs(nodes []nodeOverview, seen []map[string][]netip.Addr) []nodeOverview {
	byName := map[string][]netip.Addr{}
	// A short name two peers share (edge-1.a.org, edge-1.b.org) names neither.
	shortOf := map[string]string{} // short name -> its full label, "" when ambiguous

	for _, peers := range seen {
		for label, addrs := range peers {
			if key := hostKey(label); key != "" {
				byName[key] = append(byName[key], addrs...)
			}

			if short := shortHost(label); short != "" {
				if full, ok := shortOf[short]; ok && full != hostKey(label) {
					shortOf[short] = ""
				} else if !ok {
					shortOf[short] = hostKey(label)
				}
			}
		}
	}

	for short, full := range shortOf {
		if full != "" && short != full {
			byName[short] = append(byName[short], byName[full]...)
		}
	}

	out := make([]nodeOverview, len(nodes))

	for i, n := range nodes {
		out[i] = n

		extra := byName[hostKey(n.Hostname)]
		if len(extra) == 0 {
			extra = byName[shortHost(n.Hostname)]
		}

		if len(extra) > 0 {
			out[i].PublicIPs = publicAddresses(append(parseAddrs(n.PublicIPs), extra...))
		}
	}

	return out
}

func parseAddrs(in []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(in))

	for _, s := range in {
		if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, a)
		}
	}

	return out
}

// publicAddresses keeps the routable addresses (no private, CGNAT, ULA, link-local or
// loopback ones), deduplicated, IPv4 first.
func publicAddresses(in []netip.Addr) []string {
	var keep []netip.Addr

	for _, a := range in {
		a = a.Unmap()
		if !isPublicAddr(a) || slices.Contains(keep, a) {
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

func isPublicAddr(a netip.Addr) bool {
	a = a.Unmap()

	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
}
