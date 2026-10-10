package ichorgo

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
)

// Why a KubeSpan peer is down: the peer specs (the endpoints Talos tries), the peer statuses
// (handshakes, endpoint changes), the node's KubeSpan config and, for a node Omni manages,
// its SideroLink status. Like KubeSpanStatus, the node identity (it holds the WireGuard
// private key) is never read, and the config's shared secret is dropped.

const (
	// kubespanStaleHandshake: WireGuard handshakes every two minutes on a live tunnel.
	kubespanStaleHandshake = 3 * time.Minute
	// kubespanRecentChange: a down peer whose endpoint changed this recently is being retried.
	kubespanRecentChange = 5 * time.Minute

	verdictNoEndpoint     = "noEndpoint"
	verdictStaleHandshake = "staleHandshake"
	verdictFlapping       = "endpointFlapping"
	verdictMTUMismatch    = "mtuMismatch"
	verdictFiltered       = "filtered"
	verdictNoStatus       = "noStatus"

	// kubespanLink is the WireGuard link KubeSpan creates on a node.
	kubespanLink = "kubespan"
)

type kubespanDiag struct {
	Node       string              `json:"node"`
	Hostname   string              `json:"hostname"`
	Config     *kubespanDiagConfig `json:"config"`
	LinkMTU    uint32              `json:"linkMtu"` // 0 when the kubespan link is not there
	Peers      []kubespanDiagPeer  `json:"peers"`
	Siderolink *siderolinkDiag     `json:"siderolink"`
	Errors     map[string]string   `json:"errors"`
}

// kubespanDiagConfig is kubespan.ConfigSpec without its shared secret.
type kubespanDiagConfig struct {
	Enabled                     bool     `json:"enabled"`
	MTU                         uint32   `json:"mtu"`
	ForceRouting                bool     `json:"forceRouting"`
	AdvertiseKubernetesNetworks bool     `json:"advertiseKubernetesNetworks"`
	EndpointFilters             []string `json:"endpointFilters"`
	HarvestExtraEndpoints       bool     `json:"harvestExtraEndpoints"`
}

type kubespanDiagPeer struct {
	PublicKey          string            `json:"publicKey"`
	Label              string            `json:"label"`
	State              string            `json:"state"` // up | down | unknown
	Address            string            `json:"address"`
	AllowedIPs         []string          `json:"allowedIPs"`
	EndpointsTried     []string          `json:"endpointsTried"`
	Endpoint           string            `json:"endpoint"`
	LastUsedEndpoint   string            `json:"lastUsedEndpoint"`
	LastHandshake      int64             `json:"lastHandshake"`      // unix seconds, 0 = never
	LastEndpointChange int64             `json:"lastEndpointChange"` // unix seconds, 0 = never
	Rx                 int64             `json:"rx"`
	Tx                 int64             `json:"tx"`
	Verdicts           []kubespanVerdict `json:"verdicts"`
}

type kubespanVerdict struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type siderolinkDiag struct {
	Host        string `json:"host"`
	Connected   bool   `json:"connected"`
	LinkName    string `json:"linkName"`
	GRPCTunnel  bool   `json:"grpcTunnel"`
	NodeAddress string `json:"nodeAddress"`
	MTU         int    `json:"mtu"`
}

type kubespanDiagAll struct {
	Nodes []kubespanDiag `json:"nodes"`
}

// kubespanSpecInput is one peer spec as read.
type kubespanSpecInput struct {
	id   string
	spec kubespan.PeerSpecSpec
}

// kubespanDiagInput is what was read from one node.
type kubespanDiagInput struct {
	node, hostname string
	config         *kubespan.ConfigSpec
	specs          []kubespanSpecInput
	statuses       []kubespanPeerInput
	linkMTU        uint32
	sidero         *siderolink.StatusSpec
	tunnel         *siderolink.TunnelSpec
	errors         map[string]string
	now            time.Time
}

// KubeSpanDiagnostics tells why node's KubeSpan peers are up or down (os:reader): see
// kubespanDiag for the JSON. Each peer carries verdicts: noEndpoint (no endpoint to try),
// staleHandshake (down, no handshake for over 3 min), endpointFlapping (down, its endpoint
// changed in the last 5 min), noStatus (a peer Talos knows of has no status yet). A section
// that cannot be read is reported in "errors" (a node without COSI, for one).
func KubeSpanDiagnostics(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("KubeSpanDiagnostics", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		diag := buildKubeSpanDiag(readKubeSpanDiag(ctx, s, node))
		learnKubeSpanHosts([]kubespanDiag{diag})

		return toJSON(diag)
	})
}

// KubeSpanDiagnosticsAll is KubeSpanDiagnostics for every node of the context, in one call:
// {"nodes":[…]}. Comparing the nodes adds two verdicts: mtuMismatch (the peer's KubeSpan MTU
// is not this node's) and filtered (the peer advertised no endpoint while its endpoint
// filters are set: they may exclude all its addresses).
func KubeSpanDiagnosticsAll(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("KubeSpanDiagnosticsAll", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		nodes := targetNodes(s.context)
		inputs := make([]kubespanDiagInput, len(nodes))

		forEachNode(nodes, func(i int, node string) {
			inputs[i] = readKubeSpanDiag(client.WithNode(ctx, node), s, node)
		})

		all := buildKubeSpanDiagAll(inputs)
		learnKubeSpanHosts(all.Nodes)

		return toJSON(all)
	})
}

func learnKubeSpanHosts(diags []kubespanDiag) {
	for _, d := range diags {
		for _, p := range d.Peers {
			privacy.learnHost(p.Label, "node")
		}
	}
}

// readKubeSpanDiag reads node's KubeSpan resources; nodeCtx targets node.
func readKubeSpanDiag(nodeCtx context.Context, s *session, node string) kubespanDiagInput {
	nodeCtx, cancel := context.WithTimeout(client.WithNode(nodeCtx, node), nodeTimeout)
	defer cancel()

	in := kubespanDiagInput{node: node, errors: map[string]string{}, now: time.Now()}
	st := s.client.COSI

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, st, network.HostnameID); err == nil {
		in.hostname = hs.TypedSpec().Hostname
	}

	if c, err := safe.StateGetByID[*kubespan.Config](nodeCtx, st, kubespan.ConfigID); err == nil {
		spec := *c.TypedSpec()
		spec.SharedSecret = "" // never kept, never returned
		in.config = &spec
	} else if !state.IsNotFoundError(err) {
		in.errors["config"] = s.friendly(node, err)
	}

	if specs, err := safe.StateListAll[*kubespan.PeerSpec](nodeCtx, st); err == nil {
		for res := range specs.All() {
			in.specs = append(in.specs, kubespanSpecInput{id: res.Metadata().ID(), spec: *res.TypedSpec()})
		}
	} else {
		in.errors["peerSpecs"] = s.friendly(node, err)
	}

	if statuses, err := safe.StateListAll[*kubespan.PeerStatus](nodeCtx, st); err == nil {
		for res := range statuses.All() {
			in.statuses = append(in.statuses, kubespanPeerInput{id: res.Metadata().ID(), spec: *res.TypedSpec()})
		}
	} else {
		in.errors["peerStatuses"] = s.friendly(node, err)
	}

	if link, err := safe.StateGetByID[*network.LinkStatus](nodeCtx, st, kubespanLink); err == nil {
		in.linkMTU = link.TypedSpec().MTU
	}

	if status, err := safe.StateGetByID[*siderolink.Status](nodeCtx, st, siderolink.StatusID); err == nil {
		in.sidero = status.TypedSpec()
	}

	if tunnel, err := safe.StateGetByID[*siderolink.Tunnel](nodeCtx, st, siderolink.TunnelID); err == nil {
		in.tunnel = tunnel.TypedSpec()
	}

	return in
}

func buildKubeSpanDiag(in kubespanDiagInput) kubespanDiag {
	out := kubespanDiag{Node: in.node, Hostname: in.hostname, LinkMTU: in.linkMTU, Peers: []kubespanDiagPeer{}, Errors: in.errors}
	if out.Errors == nil {
		out.Errors = map[string]string{}
	}

	if c := in.config; c != nil {
		out.Config = &kubespanDiagConfig{
			Enabled: c.Enabled, MTU: c.MTU, ForceRouting: c.ForceRouting, AdvertiseKubernetesNetworks: c.AdvertiseKubernetesNetworks,
			EndpointFilters: orEmpty(c.EndpointFilters), HarvestExtraEndpoints: c.HarvestExtraEndpoints,
		}
	}

	if in.sidero != nil {
		out.Siderolink = &siderolinkDiag{Host: in.sidero.Host, Connected: in.sidero.Connected, LinkName: in.sidero.LinkName, GRPCTunnel: in.sidero.GRPCTunnel}

		if t := in.tunnel; t != nil {
			out.Siderolink.MTU = t.MTU
			if t.NodeAddress.IsValid() {
				out.Siderolink.NodeAddress = t.NodeAddress.String()
			}
		}
	}

	statuses := map[string]kubespan.PeerStatusSpec{}
	for _, s := range in.statuses {
		statuses[s.id] = s.spec
	}

	seen := map[string]bool{}

	for _, sp := range in.specs {
		seen[sp.id] = true
		status, ok := statuses[sp.id]
		out.Peers = append(out.Peers, diagPeer(sp.id, &sp.spec, status, ok, in.now))
	}

	for _, st := range in.statuses {
		if !seen[st.id] {
			out.Peers = append(out.Peers, diagPeer(st.id, nil, st.spec, true, in.now))
		}
	}

	sort.Slice(out.Peers, func(i, j int) bool { return out.Peers[i].Label < out.Peers[j].Label })

	return out
}

// diagPeer is one peer from its spec (nil when only the status exists) and status.
func diagPeer(id string, spec *kubespan.PeerSpecSpec, status kubespan.PeerStatusSpec, hasStatus bool, now time.Time) kubespanDiagPeer {
	p := kubespanDiagPeer{
		PublicKey: id, Label: status.Label, State: "unknown",
		AllowedIPs: []string{}, EndpointsTried: []string{}, Verdicts: []kubespanVerdict{},
	}

	if spec != nil {
		if p.Label == "" {
			p.Label = spec.Label
		}

		if spec.Address.IsValid() {
			p.Address = spec.Address.String()
		}

		for _, a := range spec.AllowedIPs {
			p.AllowedIPs = append(p.AllowedIPs, a.String())
		}

		for _, e := range spec.Endpoints {
			p.EndpointsTried = append(p.EndpointsTried, e.String())
		}
	}

	if !hasStatus {
		p.Verdicts = append(p.Verdicts, kubespanVerdict{verdictNoStatus, "Talos knows this peer but reports no state for it yet"})

		return p
	}

	p.State = status.State.String()
	p.Endpoint = addrPortText(status.Endpoint)
	p.LastUsedEndpoint = addrPortText(status.LastUsedEndpoint)
	p.Rx, p.Tx = status.ReceiveBytes, status.TransmitBytes

	if !status.LastHandshakeTime.IsZero() {
		p.LastHandshake = status.LastHandshakeTime.Unix()
	}

	if !status.LastEndpointChange.IsZero() {
		p.LastEndpointChange = status.LastEndpointChange.Unix()
	}

	if status.State != kubespan.PeerStateDown {
		return p
	}

	if spec != nil && len(spec.Endpoints) == 0 {
		p.Verdicts = append(p.Verdicts, kubespanVerdict{verdictNoEndpoint, "no endpoint to try: the peer advertised none"})
	}

	switch {
	case status.LastHandshakeTime.IsZero():
		p.Verdicts = append(p.Verdicts, kubespanVerdict{verdictStaleHandshake, "no handshake ever completed" + triedText(p.EndpointsTried)})
	case now.Sub(status.LastHandshakeTime) > kubespanStaleHandshake:
		p.Verdicts = append(p.Verdicts, kubespanVerdict{
			verdictStaleHandshake,
			"handshake stale for " + roundAgo(now.Sub(status.LastHandshakeTime)) + triedText(p.EndpointsTried),
		})
	}

	if !status.LastEndpointChange.IsZero() && now.Sub(status.LastEndpointChange) < kubespanRecentChange {
		p.Verdicts = append(p.Verdicts, kubespanVerdict{
			verdictFlapping,
			"endpoint changed " + roundAgo(now.Sub(status.LastEndpointChange)) + " ago: Talos is still trying the peer's endpoints",
		})
	}

	return p
}

func addrPortText(a netip.AddrPort) string {
	if !a.IsValid() {
		return ""
	}

	return a.String()
}

func triedText(endpoints []string) string {
	if len(endpoints) == 0 {
		return ""
	}

	return " (tried " + strings.Join(endpoints, ", ") + ")"
}

// roundAgo is a duration as "45 s", "12 min" or "3 h".
func roundAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h", int(d.Hours()))
	}
}

// buildKubeSpanDiagAll builds every node's diagnosis, then compares the nodes: a peer whose
// node has another KubeSpan MTU, or that advertised no endpoint while its filters are set.
func buildKubeSpanDiagAll(inputs []kubespanDiagInput) kubespanDiagAll {
	all := kubespanDiagAll{Nodes: make([]kubespanDiag, len(inputs))}
	byHost := map[string]*kubespanDiagConfig{}

	for i, in := range inputs {
		all.Nodes[i] = buildKubeSpanDiag(in)
		if all.Nodes[i].Hostname != "" && all.Nodes[i].Config != nil {
			byHost[all.Nodes[i].Hostname] = all.Nodes[i].Config
		}
	}

	for i := range all.Nodes {
		own := all.Nodes[i].Config

		for j := range all.Nodes[i].Peers {
			p := &all.Nodes[i].Peers[j]

			peer, ok := byHost[p.Label]
			if !ok {
				continue
			}

			if own != nil && peer.MTU != own.MTU {
				p.Verdicts = append(p.Verdicts, kubespanVerdict{
					verdictMTUMismatch, fmt.Sprintf("KubeSpan MTU %s here, %s on %s", mtuText(own.MTU), mtuText(peer.MTU), p.Label),
				})
			}

			if p.State == "down" && len(p.EndpointsTried) == 0 && len(peer.EndpointFilters) > 0 {
				p.Verdicts = append(p.Verdicts, kubespanVerdict{
					verdictFiltered, fmt.Sprintf("%s filters the endpoints it advertises (%s): they may exclude all its addresses", p.Label, strings.Join(peer.EndpointFilters, ", ")),
				})
			}
		}
	}

	return all
}

func mtuText(mtu uint32) string {
	if mtu == 0 {
		return "default"
	}

	return fmt.Sprint(mtu)
}
