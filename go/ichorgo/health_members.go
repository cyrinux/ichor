package ichorgo

import (
	"context"
	"net/netip"
	"strings"
	"time"

	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// memberProbeTimeout bounds one address probe: a firewalled address drops the dial instead of
// refusing it, and a member may list several such addresses.
const memberProbeTimeout = 3 * time.Second

// reachableClusterInfo is the cluster members for the health check, each at the first of its
// addresses the Talos API answers on, trying first those the talosconfig lists (known: its
// nodes and endpoints). Left to itself, Talos checks a member at its first address, the
// lowest routed one, which can be an overlay one (a VPN interface such as NetBird's wt0 in
// 100.64.0.0/10) the Talos API is not reachable on, and then reports the cluster unhealthy.
// Empty (Talos discovers the members itself) when they cannot be listed.
//
// Probes go through the context's endpoint, not the runner (the node that dials the members
// during the check): an address the talosconfig lists is the one known to work, so it goes
// first even when another one answers the endpoint.
func reachableClusterInfo(ctx context.Context, c *client.Client, targets, known []string) *clusterapi.ClusterInfo {
	members, err := listMembers(ctx, c, targets)
	if err != nil {
		return &clusterapi.ClusterInfo{}
	}

	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[strings.ToLower(endpointHost(k))] = true
	}

	addresses := make([]string, len(members))

	forEachNode(members, func(i int, m clusterMember) {
		addresses[i] = reachableAddress(ctx, c, knownFirst(m.addresses, knownSet))
	})

	info := &clusterapi.ClusterInfo{}

	for i, m := range members {
		switch {
		case addresses[i] == "":
			// No address: Talos would refuse the whole check over it.
		case m.role == "controlplane":
			info.ControlPlaneNodes = append(info.ControlPlaneNodes, addresses[i])
		default:
			info.WorkerNodes = append(info.WorkerNodes, addresses[i])
		}
	}

	return info
}

// knownFirst is addresses in probe order, each group in its order: those in known, then the
// others, then overlay VPN ones (a worker the talosconfig does not list still goes LAN first).
func knownFirst(addresses []netip.Addr, known map[string]bool) []netip.Addr {
	tier := func(a netip.Addr) int {
		switch {
		case known[a.String()]:
			return 0
		case cgnatPrefix.Contains(a):
			return 2
		default:
			return 1
		}
	}

	out := make([]netip.Addr, 0, len(addresses))

	for t := range 3 {
		for _, a := range addresses {
			if tier(a) == t {
				out = append(out, a)
			}
		}
	}

	return out
}

// reachableAddress is the first address the Talos API answers on, else the first address (so
// the check still reports the member down), "" when there is none.
func reachableAddress(ctx context.Context, c *client.Client, addresses []netip.Addr) string {
	if len(addresses) == 0 {
		return ""
	}

	for _, a := range addresses {
		// A link-local address needs a zone, which cannot be passed on.
		if a.IsLinkLocalUnicast() || ctx.Err() != nil {
			continue
		}

		if probeAddress(ctx, c, a.String()) {
			return a.String()
		}
	}

	return addresses[0].String()
}

func probeAddress(ctx context.Context, c *client.Client, address string) bool {
	ctx, cancel := context.WithTimeout(ctx, memberProbeTimeout)
	defer cancel()

	_, err := c.Version(client.WithNode(ctx, address))

	return err == nil
}
