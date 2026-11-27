package ichorgo

import (
	"context"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// bgpNodePeers is one node of ClusterBGPPeers.
type bgpNodePeers struct {
	Node      string    `json:"node"`
	Hostname  string    `json:"hostname"`
	Supported bool      `json:"supported"` // the node knows BGPPeerStatus (Talos 1.14+)
	Peers     []bgpPeer `json:"peers"`
	Error     string    `json:"error,omitempty"` // the node, or its peers, could not be read
}

// bgpDown is a session down, for the background check: one per peer.
type bgpDown struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	Instance string `json:"instance"`
	Peer     string `json:"peer"`
	State    string `json:"state"`
}

type bgpCluster struct {
	Context string         `json:"context"`
	Nodes   []bgpNodePeers `json:"nodes"`
	Down    []bgpDown      `json:"down"`
}

// ClusterBGPPeers reads every node's BGP sessions in parallel, for the background check:
// {"context","nodes":[{"node","hostname","supported","peers","error"}],"down":[{"node","hostname",
// "instance","peer","state"}]}. A session is down when it is not established and its peer is
// not restarting under graceful restart. A node that does not answer carries an error and adds
// nothing to down (unreadable, not resolved: the apps keep what they knew).
func ClusterBGPPeers(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demoClusterBGPPeers(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		readings := make([]bgpNodePeers, len(nodes))

		forEachNode(nodes, func(i int, node string) {
			readings[i] = readNodeBGPPeers(ctx, s, node)
		})

		return toJSON(bgpClusterOf(name, readings))
	})
}

func readNodeBGPPeers(ctx context.Context, s *session, node string) bgpNodePeers {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	out := bgpNodePeers{Node: node, Hostname: node, Peers: []bgpPeer{}}

	section := nodeNetwork{Errors: map[string]string{}}
	readBGPPeers(nodeCtx, s, node, &section)

	if e := section.Errors["bgp"]; e != "" {
		out.Error = e

		return out
	}

	out.Supported = section.BGPSupported
	if section.BGPPeers != nil {
		out.Peers = section.BGPPeers
	}

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		out.Hostname = hs.TypedSpec().Hostname
	}

	return out
}

// bgpClusterOf gathers the sessions down of readings (a node with an error adds none).
func bgpClusterOf(contextName string, readings []bgpNodePeers) bgpCluster {
	out := bgpCluster{Context: contextName, Nodes: readings, Down: []bgpDown{}}

	for _, n := range readings {
		if n.Error != "" {
			continue
		}

		for _, p := range n.Peers {
			if p.down() {
				out.Down = append(out.Down, bgpDown{Node: n.Node, Hostname: n.Hostname, Instance: p.Instance, Peer: p.Peer, State: p.State})
			}
		}
	}

	return out
}

// demoClusterBGPPeers is ClusterBGPPeers for the demo cluster: one node peers, both sessions up.
func demoClusterBGPPeers(configYAML, contextName string) (string, error) {
	name, _, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	nodes := demoNodes()
	readings := make([]bgpNodePeers, 0, len(nodes))

	for _, n := range nodes {
		entry := bgpNodePeers{Node: n.Node, Hostname: n.Hostname, Supported: true, Peers: []bgpPeer{}}
		if n.Hostname == demoBGPHost {
			entry.Peers = demoBGPPeers()
		}

		readings = append(readings, entry)
	}

	return toJSON(bgpClusterOf(name, readings))
}
