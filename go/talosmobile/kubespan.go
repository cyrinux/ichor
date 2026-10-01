package talosmobile

import (
	"context"
	"sort"
	"sync"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

type kubespanOverview struct {
	Nodes []kubespanNode `json:"nodes"`
}

type kubespanNode struct {
	Node    string         `json:"node"`
	Error   string         `json:"error,omitempty"`
	Enabled bool           `json:"enabled"`
	Up      int            `json:"up"`
	Down    int            `json:"down"`
	Peers   []kubespanPeer `json:"peers"`
}

type kubespanPeer struct {
	PublicKey     string `json:"publicKey"`
	Label         string `json:"label"`
	State         string `json:"state"` // up | down | unknown
	Endpoint      string `json:"endpoint"`
	Rx            int64  `json:"rx"`
	Tx            int64  `json:"tx"`
	LastHandshake int64  `json:"lastHandshake"` // unix seconds, 0 = never
}

type kubespanPeerInput struct {
	id   string
	spec kubespan.PeerStatusSpec
}

// KubeSpanStatus lists every node's KubeSpan peers (like `talosctl get kubespanpeerstatuses`).
// Only peer statuses are read: the node identity resource is sensitive (it holds the
// WireGuard private key), so it is neither needed nor touched.
func KubeSpanStatus(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		nodes := targetNodes(s.context)
		out := kubespanOverview{Nodes: make([]kubespanNode, len(nodes))}

		var wg sync.WaitGroup

		for i, node := range nodes {
			wg.Go(func() {
				nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
				defer cancel()

				list, err := safe.StateListAll[*kubespan.PeerStatus](nodeCtx, s.client.COSI)

				var peers []kubespanPeerInput

				if err == nil {
					for res := range list.All() {
						peers = append(peers, kubespanPeerInput{id: res.Metadata().ID(), spec: *res.TypedSpec()})
					}
				}

				out.Nodes[i] = buildKubeSpanNode(node, peers, err)
			})
		}

		wg.Wait()

		for _, n := range out.Nodes {
			for _, p := range n.Peers {
				privacy.learnHost(p.Label, "node")
			}
		}

		return toJSON(out)
	})
}

func buildKubeSpanNode(node string, peers []kubespanPeerInput, err error) kubespanNode {
	out := kubespanNode{Node: node, Peers: []kubespanPeer{}}
	if err != nil {
		out.Error = friendlyError(err)

		return out
	}

	for _, p := range peers {
		peer := kubespanPeer{
			PublicKey: p.id,
			Label:     p.spec.Label,
			State:     p.spec.State.String(),
			Rx:        p.spec.ReceiveBytes,
			Tx:        p.spec.TransmitBytes,
		}

		if p.spec.Endpoint.IsValid() {
			peer.Endpoint = p.spec.Endpoint.String()
		}

		if !p.spec.LastHandshakeTime.IsZero() {
			peer.LastHandshake = p.spec.LastHandshakeTime.Unix()
		}

		switch p.spec.State {
		case kubespan.PeerStateUp:
			out.Up++
		case kubespan.PeerStateDown:
			out.Down++
		}

		out.Peers = append(out.Peers, peer)
	}

	out.Enabled = len(out.Peers) > 0
	sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].Label < out.Peers[j].Label })

	return out
}
