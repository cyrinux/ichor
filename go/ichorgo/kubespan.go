package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

// Talos reports "unknown" for more than a peer it just added: a tunnel that never handshakes
// cycles down, new endpoint, unknown, and a peer without a usable endpoint is unknown for good.
// So "broken" reads the handshake and the status's age too. This is the one place it is decided.
const (
	// kubespanJoinGrace is how long a peer that never handshaked counts as still joining.
	kubespanJoinGrace = 5 * time.Minute
	// kubespanPeerDown is Talos's PeerDownInterval: a live tunnel handshakes well within it.
	kubespanPeerDown = 275 * time.Second

	// Why a tunnel is broken, worst first (see brokenRank). kubespanReasonOneSide is a link's only.
	kubespanReasonNoEndpoint = "noEndpoint"
	kubespanReasonNever      = "neverHandshaked"
	kubespanReasonStale      = "staleHandshake"
	kubespanReasonDown       = "down"
	kubespanReasonOneSide    = "oneSide"
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
	Unknown int            `json:"unknown"`
	Broken  int            `json:"broken"` // down, and unknown ones that are in fact dead
	Peers   []kubespanPeer `json:"peers"`
}

type kubespanPeer struct {
	PublicKey     string `json:"publicKey"`
	Label         string `json:"label"`
	State         string `json:"state"` // up | down | unknown
	Broken        bool   `json:"broken"`
	Reason        string `json:"reason,omitempty"` // why it is broken, see kubespanBrokenReason
	Endpoint      string `json:"endpoint"`
	Rx            int64  `json:"rx"`
	Tx            int64  `json:"tx"`
	LastHandshake int64  `json:"lastHandshake"` // unix seconds, 0 = never
}

type kubespanPeerInput struct {
	id      string
	created time.Time // when Talos first listed the peer, zero when unknown
	spec    kubespan.PeerStatusSpec
}

// kubespanBrokenReason says why a peer's tunnel is effectively down, "" when it is not: Talos
// says down, or says unknown with no handshake past the grace or a stale one. An unknown peer
// of unknown age is given the benefit of the doubt, so the banner does not cry wolf.
func kubespanBrokenReason(p kubespanPeerInput, now time.Time) string {
	never := p.spec.LastHandshakeTime.IsZero()
	stale := !never && now.Sub(p.spec.LastHandshakeTime) > kubespanPeerDown

	switch p.spec.State {
	case kubespan.PeerStateDown:
	case kubespan.PeerStateUnknown:
		joining := p.created.IsZero() || now.Sub(p.created) <= kubespanJoinGrace
		if !stale && (!never || joining) {
			return ""
		}
	default:
		return ""
	}

	switch {
	case never && !p.spec.Endpoint.IsValid():
		return kubespanReasonNoEndpoint
	case never:
		return kubespanReasonNever
	case stale:
		return kubespanReasonStale
	default:
		return kubespanReasonDown
	}
}

// KubeSpanStatus lists every node's KubeSpan peers (like `talosctl get kubespanpeerstatuses`).
// Only peer statuses are read: the node identity resource is sensitive (it holds the
// WireGuard private key), so it is neither needed nor touched.
func KubeSpanStatus(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("KubeSpanStatus", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		nodes := targetNodes(s.context)
		out := kubespanOverview{Nodes: make([]kubespanNode, len(nodes))}

		forEachNode(nodes, func(i int, node string) {
			nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
			defer cancel()

			list, err := safe.StateListAll[*kubespan.PeerStatus](nodeCtx, s.client.COSI)

			var peers []kubespanPeerInput

			if err == nil {
				for res := range list.All() {
					peers = append(peers, kubespanPeerInput{id: res.Metadata().ID(), created: res.Metadata().Created(), spec: *res.TypedSpec()})
				}
			}

			out.Nodes[i] = buildKubeSpanNode(node, peers, err)
		})

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

	now := time.Now()

	for _, p := range peers {
		reason := kubespanBrokenReason(p, now)
		peer := kubespanPeer{
			PublicKey: p.id,
			Label:     p.spec.Label,
			State:     p.spec.State.String(),
			Broken:    reason != "",
			Reason:    reason,
			Rx:        p.spec.ReceiveBytes,
			Tx:        p.spec.TransmitBytes,
		}

		if peer.Broken {
			out.Broken++
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
		case kubespan.PeerStateUnknown:
			out.Unknown++
		}

		out.Peers = append(out.Peers, peer)
	}

	out.Enabled = len(out.Peers) > 0
	// The peers with a problem first, the rest by name.
	slices.SortFunc(out.Peers, func(x, y kubespanPeer) int {
		return cmp.Or(brokenFirst(x.Broken, y.Broken), cmp.Compare(x.Label, y.Label))
	})

	return out
}

func brokenFirst(x, y bool) int {
	switch {
	case x == y:
		return 0
	case x:
		return -1
	default:
		return 1
	}
}
