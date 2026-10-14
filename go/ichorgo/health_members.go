package ichorgo

import (
	"context"
	"net/netip"
	"time"

	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// memberProbeTimeout bounds one address probe: a firewalled address drops the dial instead of
// refusing it, and a member may list several such addresses.
const memberProbeTimeout = 3 * time.Second

// reachableClusterInfo is the cluster members for the health check, each at the first of its
// addresses the Talos API answers on. Left to itself, Talos checks a member at its first
// address, which can be an overlay one (a VPN interface such as NetBird's wt0) the Talos API
// is not reachable on, and then reports the cluster unhealthy. Empty (Talos discovers the
// members itself) when they cannot be listed.
//
// Probes go through the context's endpoint, which usually shares the runner's network: the
// runner is the node that dials the members during the check.
func reachableClusterInfo(ctx context.Context, c *client.Client, targets []string) *clusterapi.ClusterInfo {
	members, err := listMembers(ctx, c, targets)
	if err != nil {
		return &clusterapi.ClusterInfo{}
	}

	addresses := make([]string, len(members))

	forEachNode(members, func(i int, m clusterMember) {
		addresses[i] = reachableAddress(ctx, c, m.addresses)
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

// reachableAddress is the first address the Talos API answers on, else the first address (the
// one Talos would use, so the check still reports the member down), "" when there is none.
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
