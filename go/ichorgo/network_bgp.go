package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.yaml.in/yaml/v4"
)

// bgpPeerStatusType is the BGPPeerStatus resource of Talos's native BGP (pkg/machinery/resources/
// network/bgp_peer_status.go): one per peer of each BGPInstanceConfig, in the network namespace,
// first in v1.14. Its spec at v1.14.7 is {peer, localASN, peerASN, state, routerID, since,
// received, advertised, accepted, bfdState, instance}; v1.15.0-beta.1 adds gracefulRestart and
// peerRestarting. The client's machinery (1.14) registers the 1.14 Go type, which would decode
// the protobuf spec and drop the 1.15 fields: the list is read raw and the YAML spec the node
// sends along is decoded by key instead, so both versions read the same way.
const bgpPeerStatusType = "BGPPeerStatuses.net.talos.dev"

// bgpStateEstablished is nethelpers.BGPSessionState's text for an up session (the RFC 4271 FSM:
// UNKNOWN, IDLE, CONNECT, ACTIVE, OPEN_SENT, OPEN_CONFIRM, ESTABLISHED).
const bgpStateEstablished = "ESTABLISHED"

// bgpPeersMax caps the peers of one node.
const bgpPeersMax = 64

// bgpDocASNBase is the first of the 16 ASNs reserved for documentation (RFC 5398): what an ASN
// becomes in screenshot mode, an ASN being the number that names a network.
const bgpDocASNBase = 64496

type bgpPeer struct {
	Instance        string `json:"instance"`
	Peer            string `json:"peer"` // the neighbour's address, or link/<name> for an unnumbered peer
	PeerASN         int64  `json:"peerAsn"`
	LocalASN        int64  `json:"localAsn"`
	State           string `json:"state"`
	Established     bool   `json:"established"`
	Since           string `json:"since"` // RFC 3339, when the session came up; empty when it is not up
	Received        int64  `json:"received"`
	Advertised      int64  `json:"advertised"`
	GracefulRestart bool   `json:"gracefulRestart"` // Talos 1.15+
	PeerRestarting  bool   `json:"peerRestarting"`  // Talos 1.15+: its routes are kept while it restarts
}

// down is a session that should carry routes and does not. A peer restarting under graceful
// restart keeps its routes: not down.
func (p bgpPeer) down() bool {
	return !p.Established && !p.PeerRestarting
}

// readBGPPeers fills out's BGP section, like `talosctl get bgppeerstatus`: only when the node
// knows the resource type (Talos 1.14+); a failure goes to errors["bgp"].
func readBGPPeers(ctx context.Context, s *session, node string, out *nodeNetwork) {
	known, err := s.hasResourceType(ctx, node, bgpPeerStatusType)
	if err != nil {
		out.Errors["bgp"] = s.friendly(node, err)

		return
	}

	if !known {
		return
	}

	out.BGPSupported = true

	specs, err := listRawSpecs(ctx, s, network.NamespaceName, bgpPeerStatusType)
	if err != nil {
		out.Errors["bgp"] = s.friendly(node, err)

		return
	}

	out.BGPPeers = mapBGPPeers(specs)
}

// listRawSpecs lists a type's resources with the raw COSI API and decodes the YAML spec each one
// carries, whatever this client's machinery knows of the type; ctx targets the node.
func listRawSpecs(ctx context.Context, s *session, namespace, resourceType string) ([]map[string]any, error) {
	stream, err := v1alpha1.NewStateClient(s.client.Conn()).List(ctx, &v1alpha1.ListRequest{
		Namespace: namespace,
		Type:      resourceType,
		Options:   &v1alpha1.ListOptions{},
	})
	if err != nil {
		return nil, err
	}

	out := []map[string]any{}

	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return out, nil
		}

		if err != nil {
			return nil, err
		}

		spec := map[string]any{}
		if err := yaml.Unmarshal([]byte(resp.GetResource().GetSpec().GetYamlSpec()), &spec); err != nil {
			return nil, fmt.Errorf("%s %s: decode spec: %w", resourceType, resp.GetResource().GetMetadata().GetId(), err)
		}

		out = append(out, spec)
	}
}

// mapBGPPeers maps the BGPPeerStatus specs, by instance then peer.
func mapBGPPeers(specs []map[string]any) []bgpPeer {
	out := make([]bgpPeer, 0, len(specs))

	for _, spec := range specs {
		if len(out) == bgpPeersMax {
			break
		}

		out = append(out, mapBGPPeer(spec))
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Instance != out[j].Instance {
			return out[i].Instance < out[j].Instance
		}

		return out[i].Peer < out[j].Peer
	})

	return out
}

func mapBGPPeer(spec map[string]any) bgpPeer {
	state := specString(spec, "state")
	if state == "" {
		state = "UNKNOWN"
	}

	p := bgpPeer{
		Instance:        cleanLLDPText(specString(spec, "instance")),
		Peer:            cleanLLDPText(specString(spec, "peer")),
		PeerASN:         maskASN(specIntOr0(spec, "peerASN")),
		LocalASN:        maskASN(specIntOr0(spec, "localASN")),
		State:           cleanLLDPText(state),
		Established:     state == bgpStateEstablished,
		Received:        specIntOr0(spec, "received"),
		Advertised:      specIntOr0(spec, "advertised"),
		GracefulRestart: specBool(spec, "gracefulRestart"),
		PeerRestarting:  specBool(spec, "peerRestarting"),
	}

	if p.Established {
		p.Since = specTime(spec, "since")
	}

	return p
}

// maskASN swaps an ASN for a documentation one in screenshot mode (the same ASN, the same fake).
func maskASN(asn int64) int64 {
	if asn == 0 || !privacy.isEnabled() {
		return asn
	}

	return bgpDocASNBase + asn%16
}

func specBool(spec map[string]any, key string) bool {
	b, _ := spec[key].(bool) //nolint:errcheck

	return b
}

// specTime reads a YAML timestamp as RFC 3339, empty when absent or zero.
func specTime(spec map[string]any, key string) string {
	var t time.Time

	switch v := spec[key].(type) {
	case time.Time:
		t = v
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return ""
		}

		t = parsed
	default:
		return ""
	}

	if t.IsZero() {
		return ""
	}

	return t.UTC().Format(time.RFC3339)
}

// demoBGPPeers are a demo node's two sessions with its top-of-rack routers, both up
// (documentation addresses and private ASNs).
func demoBGPPeers() []bgpPeer {
	since := time.Now().Add(-26 * time.Hour).UTC().Truncate(time.Minute).Format(time.RFC3339)

	return []bgpPeer{
		{
			Instance: "fabric", Peer: "192.0.2.252", PeerASN: 64512, LocalASN: 64601, State: bgpStateEstablished,
			Established: true, Since: since, Received: 12, Advertised: 3, GracefulRestart: true,
		},
		{
			Instance: "fabric", Peer: "192.0.2.253", PeerASN: 64512, LocalASN: 64601, State: bgpStateEstablished,
			Established: true, Since: since, Received: 12, Advertised: 3, GracefulRestart: true,
		},
	}
}

// demoNodeBGPPeers is the BGP section of a demo node's network: only demoBGPHost peers.
func demoNodeBGPPeers(hostname string) []bgpPeer {
	if hostname != demoBGPHost {
		return []bgpPeer{}
	}

	return demoBGPPeers()
}

// demoBGPHost is the demo node that peers with the fabric.
const demoBGPHost = "demo-worker-1"
