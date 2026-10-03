package talosmobile

import (
	"net/netip"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

// demoSite places each demo node (by index into demoNodes) in a zone with the public address
// its site is reached at: three control planes and a worker at home in Paris, one control
// plane in Amsterdam, a worker at Hetzner Falkenstein.
var demoSites = []struct{ zone, public string }{
	{"fr-par-1", "198.51.100.10"},
	{"fr-par-1", "198.51.100.10"},
	{"nl-ams-1", "198.51.100.20"},
	{"fr-par-1", "198.51.100.10"},
	{"fsn1-dc14", "203.0.113.30"},
}

// demoPeers is what demo node i reports about its KubeSpan peers: the node's own address
// within its site, the site's public address across sites. The Falkenstein worker cannot
// reach Amsterdam (its tunnel there is down) while Amsterdam still reports it up.
func demoPeers(i int) []kubespanPeerInput {
	nodes := demoNodes()
	handshake := time.Now().Add(-10 * time.Second)

	var peers []kubespanPeerInput

	for j, peer := range nodes {
		if j == i {
			continue
		}

		endpoint := peer.Node
		if demoSites[i].zone != demoSites[j].zone {
			endpoint = demoSites[j].public
		}

		spec := kubespan.PeerStatusSpec{
			Label: peer.Hostname, State: kubespan.PeerStateUp, Endpoint: netip.AddrPortFrom(netip.MustParseAddr(endpoint), 51820),
			ReceiveBytes: 1 << 20, TransmitBytes: 512 << 10, LastHandshakeTime: handshake,
		}

		if i == 4 && j == 2 {
			spec.State, spec.ReceiveBytes, spec.LastHandshakeTime = kubespan.PeerStateDown, 0, time.Now().Add(-9*time.Minute)
		}

		peers = append(peers, kubespanPeerInput{id: "demo-" + peer.Hostname, spec: spec})
	}

	return peers
}

func demoKubeSpan() kubespanOverview {
	out := kubespanOverview{Nodes: []kubespanNode{}}
	for i, n := range demoNodes() {
		out.Nodes = append(out.Nodes, buildKubeSpanNode(n.Node, demoPeers(i), nil))
	}

	return out
}

func demoTopology() clusterTopology {
	nodes := demoNodes()
	members := make([]clusterMember, len(nodes))
	observations := make([]topologyObservation, len(nodes))

	for i, n := range nodes {
		members[i] = clusterMember{hostname: n.Hostname, role: n.Role, addresses: []netip.Addr{netip.MustParseAddr(n.Node)}}
		observations[i] = topologyObservation{
			node: n.Node, hostname: n.Hostname, peers: demoPeers(i),
			labels: map[string]string{zoneLabel: demoSites[i].zone},
		}
	}

	return buildTopology(members, observations)
}
