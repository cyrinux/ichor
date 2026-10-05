package ichorgo

import (
	"cmp"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

// Kubernetes node labels a node's zone and region come from (the legacy ones last).
const (
	zoneLabel         = "topology.kubernetes.io/zone"
	regionLabel       = "topology.kubernetes.io/region"
	legacyZoneLabel   = "failure-domain.beta.kubernetes.io/zone"
	legacyRegionLabel = "failure-domain.beta.kubernetes.io/region"
)

type clusterTopology struct {
	Nodes []topologyNode `json:"nodes"`
	Links []topologyLink `json:"links"`
	Sites []topologySite `json:"sites"`
}

type topologyNode struct {
	ID        string   `json:"id"`             // hostname, the key links and sites refer to
	Node      string   `json:"node,omitempty"` // talosconfig target, "" when not targeted
	Hostname  string   `json:"hostname"`
	Role      string   `json:"role,omitempty"`
	Addresses []string `json:"addresses"`
	Zone      string   `json:"zone,omitempty"`
	Region    string   `json:"region,omitempty"`
	Country   string   `json:"country,omitempty"` // ISO 3166-1 alpha-2 guessed from zone/region
	Site      string   `json:"site"`
	KubeSpan  bool     `json:"kubespan"`
	Queried   bool     `json:"queried"`
	Error     string   `json:"error,omitempty"`
}

// topologySite groups nodes by zone label or, without one, by the LAN they share.
type topologySite struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"` // zone, shared private subnet, or "" (a lone node)
	Kind    string   `json:"kind"`  // zone | lan | node
	Country string   `json:"country,omitempty"`
	Nodes   []string `json:"nodes"`
}

// topologyLink is the KubeSpan tunnel between two nodes, as each end reports it.
type topologyLink struct {
	A     string             `json:"a"`
	B     string             `json:"b"`
	State string             `json:"state"` // up | down | degraded (one end down) | unknown
	Sides []topologyLinkSide `json:"sides"`
}

type topologyLinkSide struct {
	From          string `json:"from"`
	To            string `json:"to"`
	State         string `json:"state"`
	Endpoint      string `json:"endpoint,omitempty"`
	Private       bool   `json:"private"` // endpoint on a private network: both ends share a LAN
	Rx            int64  `json:"rx"`
	Tx            int64  `json:"tx"`
	LastHandshake int64  `json:"lastHandshake"`
}

// topologyObservation is what one targeted node reported.
type topologyObservation struct {
	node           string
	hostname       string
	err            error
	peers          []kubespanPeerInput
	labels         map[string]string
	platformZone   string
	platformRegion string
}

type topologyBuilder struct {
	nodes []*topologyNode
	byKey map[string]*topologyNode // hostname, short hostname and address -> node
	links map[[2]string]*topologyLink
}

// buildTopology joins the cluster members with what each targeted node reported into the
// nodes, KubeSpan links and sites of the cluster map.
func buildTopology(members []clusterMember, observations []topologyObservation) clusterTopology {
	b := &topologyBuilder{byKey: map[string]*topologyNode{}, links: map[[2]string]*topologyLink{}}

	for _, m := range members {
		n := b.node(m.hostname, m.addresses)
		n.Role = m.role
	}

	// A machine targeted twice (a VIP or hostname besides its IP) counts once.
	var observed []topologyObservation

	for _, o := range observations {
		if b.observe(o) {
			observed = append(observed, o)
		}
	}

	for _, o := range observed {
		from := b.find(o.hostname, o.node)
		for _, p := range o.peers {
			b.addSide(from, b.peerNode(p), p)
		}
	}

	slices.SortFunc(b.nodes, func(x, y *topologyNode) int {
		return cmp.Or(cmp.Compare(roleRank(x.Role), roleRank(y.Role)), cmp.Compare(x.ID, y.ID))
	})

	out := clusterTopology{Nodes: []topologyNode{}, Links: b.sortedLinks(), Sites: b.sites()}
	for _, n := range b.nodes {
		out.Nodes = append(out.Nodes, *n)
	}

	return out
}

func roleRank(role string) int {
	switch role {
	case "controlplane":
		return 0
	case "worker":
		return 1
	default:
		return 2
	}
}

// find returns the node known by hostname or address, nil when there is none.
func (b *topologyBuilder) find(hostname, address string) *topologyNode {
	for _, key := range []string{hostKey(hostname), shortHost(hostname), address} {
		if n := b.byKey[key]; key != "" && n != nil {
			return n
		}
	}

	return nil
}

// node returns the node known by hostname, adding it when it is new.
func (b *topologyBuilder) node(hostname string, addresses []netip.Addr) *topologyNode {
	var addr string
	if len(addresses) > 0 {
		addr = addresses[0].String()
	}

	n := b.find(hostname, addr)
	if n == nil {
		n = &topologyNode{ID: cmp.Or(hostKey(hostname), addr), Hostname: hostname, Addresses: []string{}}
		b.nodes = append(b.nodes, n)
	}

	for _, a := range addresses {
		if s := a.String(); !slices.Contains(n.Addresses, s) {
			n.Addresses = append(n.Addresses, s)
			if b.byKey[s] == nil {
				b.byKey[s] = n
			}
		}
	}

	if n.Hostname == "" {
		n.Hostname = hostname
	}

	b.index(n)

	return n
}

func (b *topologyBuilder) index(n *topologyNode) {
	for _, key := range []string{hostKey(n.Hostname), shortHost(n.Hostname), n.ID} {
		if key != "" && b.byKey[key] == nil {
			b.byKey[key] = n
		}
	}
}

// peerNode is the node a KubeSpan peer is: by its label (the peer's node name), else by its
// endpoint address (a label that differs from the discovered hostname), else a new node.
func (b *topologyBuilder) peerNode(p kubespanPeerInput) *topologyNode {
	if n := b.find(p.spec.Label, ""); n != nil {
		return n
	}

	if p.spec.Endpoint.IsValid() {
		if n := b.byKey[p.spec.Endpoint.Addr().Unmap().String()]; n != nil {
			return n
		}
	}

	return b.node(cmp.Or(p.spec.Label, p.id), nil)
}

// observe records what a target reported, false when its machine was already observed.
func (b *topologyBuilder) observe(o topologyObservation) bool {
	n := b.find(o.hostname, o.node)
	if n != nil && n.Queried && n.Error == "" {
		return false
	}

	if n == nil {
		var addrs []netip.Addr
		if a, err := netip.ParseAddr(o.node); err == nil {
			addrs = []netip.Addr{a}
		}

		n = b.node(cmp.Or(o.hostname, o.node), addrs)
	}

	n.Node, n.Queried = o.node, true
	if o.err != nil {
		n.Error = friendlyError(o.err)
	}

	n.KubeSpan = len(o.peers) > 0
	n.Zone = cmp.Or(o.labels[zoneLabel], o.labels[legacyZoneLabel], o.platformZone)
	n.Region = cmp.Or(o.labels[regionLabel], o.labels[legacyRegionLabel], o.platformRegion)
	n.Country = zoneCountry(n.Zone, n.Region)

	return true
}

func (b *topologyBuilder) addSide(from, to *topologyNode, p kubespanPeerInput) {
	if from == nil || from == to {
		return
	}

	side := topologyLinkSide{From: from.ID, To: to.ID, State: p.spec.State.String(), Rx: p.spec.ReceiveBytes, Tx: p.spec.TransmitBytes}
	if p.spec.Endpoint.IsValid() {
		side.Endpoint = p.spec.Endpoint.String()
		side.Private = lanAddress(p.spec.Endpoint.Addr())
	}

	if !p.spec.LastHandshakeTime.IsZero() {
		side.LastHandshake = p.spec.LastHandshakeTime.Unix()
	}

	key := pairKey(from.ID, to.ID)

	l := b.links[key]
	if l == nil {
		l = &topologyLink{A: key[0], B: key[1]}
		b.links[key] = l
	}

	l.Sides = append(l.Sides, side)
	l.State = linkState(l.Sides)
}

func pairKey(a, b string) [2]string {
	if a > b {
		return [2]string{b, a}
	}

	return [2]string{a, b}
}

// linkState: up when every end that reports it says up, degraded when only some do.
func linkState(sides []topologyLinkSide) string {
	var up, down int

	for _, s := range sides {
		switch s.State {
		case kubespan.PeerStateUp.String():
			up++
		case kubespan.PeerStateDown.String():
			down++
		}
	}

	switch {
	case up > 0 && down > 0:
		return "degraded"
	case down > 0:
		return "down"
	case up > 0:
		return "up"
	default:
		return "unknown"
	}
}

func (b *topologyBuilder) sortedLinks() []topologyLink {
	out := make([]topologyLink, 0, len(b.links))
	for _, l := range b.links {
		slices.SortFunc(l.Sides, func(x, y topologyLinkSide) int { return cmp.Compare(x.From, y.From) })
		out = append(out, *l)
	}

	slices.SortFunc(out, func(x, y topologyLink) int { return cmp.Or(cmp.Compare(x.A, y.A), cmp.Compare(x.B, y.B)) })

	return out
}

// sites groups the nodes: by zone label first, then nodes without one join the nodes they
// reach over a private address or share a private or public /24 with. Two zones never merge.
func (b *topologyBuilder) sites() []topologySite {
	index := make(map[string]int, len(b.nodes))
	for i, n := range b.nodes {
		index[n.ID] = i
	}

	g := newSiteGroups(b.nodes)

	for i, n := range b.nodes {
		for j := range i {
			if n.Zone != "" && n.Zone == b.nodes[j].Zone {
				g.union(i, j)
			}
		}
	}

	// A link reached over public endpoints says its ends are apart, even when they reuse
	// the same private subnet (two homes on 192.168.1.0/24, default cloud VPCs).
	public := map[[2]string]bool{}

	for _, l := range b.sortedLinks() {
		switch {
		case slices.ContainsFunc(l.Sides, func(s topologyLinkSide) bool { return s.Private }):
			g.union(index[l.A], index[l.B])
		case slices.ContainsFunc(l.Sides, func(s topologyLinkSide) bool { return s.Endpoint != "" }):
			public[pairKey(l.A, l.B)] = true
		}
	}

	// Public addresses in one /24 are one datacenter: unlabelled servers with public IPs only
	// reach each other over public endpoints, so this is the one clue they are together.
	for i, n := range b.nodes {
		for j := range i {
			m := b.nodes[j]
			if sharedSubnet(n.Addresses, m.Addresses, lanSubnet) != "" && !public[pairKey(n.ID, m.ID)] ||
				sharedSubnet(n.Addresses, m.Addresses, publicSubnet) != "" {
				g.union(i, j)
			}
		}
	}

	return b.collectSites(g)
}

func (b *topologyBuilder) collectSites(g *siteGroups) []topologySite {
	var out []topologySite

	byRoot := map[int]int{}

	for i, n := range b.nodes {
		root := g.find(i)

		k, ok := byRoot[root]
		if !ok {
			k = len(out)
			byRoot[root] = k
			out = append(out, topologySite{ID: fmt.Sprintf("site-%d", k+1), Nodes: []string{}})
		}

		out[k].Nodes = append(out[k].Nodes, n.ID)
		n.Site = out[k].ID
	}

	for k := range out {
		out[k] = b.describeSite(out[k])
	}

	return out
}

// describeSite names a site after its zone, else the private subnet its nodes share.
func (b *topologyBuilder) describeSite(s topologySite) topologySite {
	var members []*topologyNode

	for _, n := range b.nodes {
		if slices.Contains(s.Nodes, n.ID) {
			members = append(members, n)
		}
	}

	for _, n := range members {
		if n.Zone != "" {
			s.Kind, s.Label = "zone", n.Zone
		}

		if n.Country != "" && s.Country == "" {
			s.Country = n.Country
		}
	}

	if s.Kind == "zone" {
		return s
	}

	if len(members) == 1 {
		s.Kind = "node"

		return s
	}

	s.Kind, s.Label = "lan", cmp.Or(commonSubnet(members, lanSubnet), commonSubnet(members, publicSubnet))

	return s
}

// subnetOf maps an address to the /24 it groups by, false when it does not group.
type subnetOf func(string) (netip.Prefix, bool)

// commonSubnet is the IPv4 /24 every node has an address in ("" for none).
func commonSubnet(nodes []*topologyNode, subnet subnetOf) string {
	for _, a := range nodes[0].Addresses {
		p, ok := subnet(a)
		if !ok {
			continue
		}

		if !slices.ContainsFunc(nodes[1:], func(n *topologyNode) bool {
			return !slices.ContainsFunc(n.Addresses, func(x string) bool { q, ok := subnet(x); return ok && q == p })
		}) {
			return p.String()
		}
	}

	return ""
}

// lanAddress: the address only makes sense inside a site (RFC 1918, ULA, link-local).
func lanAddress(a netip.Addr) bool {
	a = a.Unmap()

	return a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLoopback()
}

// sharedSubnet is the IPv4 /24 two address lists have in common ("" for none).
func sharedSubnet(a, b []string, subnet subnetOf) string {
	for _, x := range a {
		px, ok := subnet(x)
		if !ok {
			continue
		}

		for _, y := range b {
			if py, ok := subnet(y); ok && px == py {
				return px.String()
			}
		}
	}

	return ""
}

func lanSubnet(s string) (netip.Prefix, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Unmap().Is4() || !a.IsPrivate() {
		return netip.Prefix{}, false
	}

	p, _ := a.Unmap().Prefix(24)

	return p, true
}

// carrierNAT is the shared address space (RFC 6598): CGNAT and Tailscale hand it out
// across unrelated places, so a shared /24 there says nothing about where nodes are.
var carrierNAT = netip.MustParsePrefix("100.64.0.0/10")

// publicSubnet is the /24 of a global unicast IPv4 address outside private and shared space.
func publicSubnet(s string) (netip.Prefix, bool) {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, false
	}

	a = a.Unmap()
	if !a.Is4() || !a.IsGlobalUnicast() || a.IsPrivate() || carrierNAT.Contains(a) {
		return netip.Prefix{}, false
	}

	p, _ := a.Prefix(24)

	return p, true
}

func hostKey(h string) string { return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")) }

func shortHost(h string) string {
	h = hostKey(h)
	if _, err := netip.ParseAddr(h); err == nil {
		return ""
	}

	short, _, _ := strings.Cut(h, ".")

	return short
}

// siteGroups is a union-find over node indexes that keeps nodes of different zones apart.
type siteGroups struct {
	parent []int
	zone   []string
}

func newSiteGroups(nodes []*topologyNode) *siteGroups {
	g := &siteGroups{parent: make([]int, len(nodes)), zone: make([]string, len(nodes))}
	for i, n := range nodes {
		g.parent[i], g.zone[i] = i, n.Zone
	}

	return g
}

func (g *siteGroups) find(i int) int {
	for g.parent[i] != i {
		g.parent[i] = g.parent[g.parent[i]]
		i = g.parent[i]
	}

	return i
}

func (g *siteGroups) union(i, j int) {
	ri, rj := g.find(i), g.find(j)
	if ri == rj || (g.zone[ri] != "" && g.zone[rj] != "" && g.zone[ri] != g.zone[rj]) {
		return
	}

	g.parent[rj] = ri
	g.zone[ri] = cmp.Or(g.zone[ri], g.zone[rj])
}
