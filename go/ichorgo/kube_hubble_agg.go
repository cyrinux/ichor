package ichorgo

import (
	"cmp"
	"slices"
	"strconv"
	"sync"
)

const (
	// hubbleMaxFlows is how many recent flows a snapshot carries: it is sent every second.
	hubbleMaxFlows = 100
	// hubbleMaxGroups caps the drop groups kept, and sent; the oldest go first.
	hubbleMaxGroups = 100
)

// Node states of a live flow view.
const (
	hubbleNodeConnecting = "connecting"
	hubbleNodeLive       = "live"
	hubbleNodeError      = "error"
)

// hubbleSnapshot is what a live flow view shows, sent to the app as JSON.
type hubbleSnapshot struct {
	Namespace     string            `json:"namespace"` // Cilium's
	Version       string            `json:"version"`
	Buffer        int               `json:"buffer"`
	Nodes         []hubbleNodeState `json:"nodes"`
	Flows         []hubbleFlow      `json:"flows"` // newest first
	Drops         []dropGroup       `json:"drops"` // last seen first
	Seen          int64             `json:"seen"`
	Dropped       int64             `json:"dropped"`
	Lost          uint64            `json:"lost"` // events Hubble lost (ring buffer overrun)
	PoliciesError string            `json:"policiesError,omitempty"`
}

type hubbleNodeState struct {
	Node  string `json:"node"`
	Pod   string `json:"pod"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	Flows int64  `json:"flows"`
	last  int64  // time of the newest flow the node sent, unix ms
}

// dropGroup is the same denied traffic seen again and again: one client retrying.
type dropGroup struct {
	Source      hubblePeer  `json:"source"`
	Destination hubblePeer  `json:"destination"`
	Protocol    string      `json:"protocol,omitempty"`
	Port        uint32      `json:"port,omitempty"`
	Direction   string      `json:"direction,omitempty"`
	Verdict     string      `json:"verdict"` // DROPPED, or AUDIT: it would have been
	Reason      string      `json:"reason,omitempty"`
	Count       int64       `json:"count"`
	FirstSeen   int64       `json:"firstSeen"`
	LastSeen    int64       `json:"lastSeen"`
	Nodes       []string    `json:"nodes"`
	DeniedBy    []policyRef `json:"deniedBy"`  // what the flow names: explicit deny rules
	Isolating   []policyRef `json:"isolating"` // what put the endpoint in default-deny
	Sample      hubbleFlow  `json:"sample"`
	sourceLabel labelSet
	destLabel   labelSet
}

// hubbleAgg gathers the flows of every agent. Safe for concurrent use.
type hubbleAgg struct {
	mu       sync.Mutex
	status   ciliumStatus
	nodes    map[string]*hubbleNodeState
	flows    []hubbleFlow // ring, next at head
	head     int
	groups   map[string]*dropGroup
	seen     int64
	dropped  int64
	lost     uint64
	policies []netPolicy
	polErr   string
	dirty    bool
}

func newHubbleAgg(status ciliumStatus) *hubbleAgg {
	a := &hubbleAgg{status: status, nodes: map[string]*hubbleNodeState{}, groups: map[string]*dropGroup{}, dirty: true}

	for _, agent := range status.Agents {
		a.nodes[agent.Node] = &hubbleNodeState{Node: agent.Node, Pod: agent.Pod, State: hubbleNodeConnecting}
	}

	return a
}

func (a *hubbleAgg) setNode(node, state, errMessage string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if n, ok := a.nodes[node]; ok && (n.State != state || n.Error != errMessage) {
		n.State, n.Error, a.dirty = state, errMessage, true
	}
}

// syncAgents follows the agents listed again: new nodes start connecting, a replaced pod is
// renamed, nodes gone are dropped.
func (a *hubbleAgg) syncAgents(agents []ciliumAgent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	current := map[string]bool{}

	for _, agent := range agents {
		current[agent.Node] = true

		switch n, ok := a.nodes[agent.Node]; {
		case !ok:
			a.nodes[agent.Node] = &hubbleNodeState{Node: agent.Node, Pod: agent.Pod, State: hubbleNodeConnecting}
			a.dirty = true
		case n.Pod != agent.Pod:
			n.Pod, a.dirty = agent.Pod, true
		}
	}

	for node := range a.nodes {
		if !current[node] {
			delete(a.nodes, node)
			a.dirty = true
		}
	}
}

// lastFlow is the time of the newest flow node sent, 0 when none yet.
func (a *hubbleAgg) lastFlow(node string) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	if n, ok := a.nodes[node]; ok {
		return n.last
	}

	return 0
}

func (a *hubbleAgg) setPolicies(policies []netPolicy, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if policies != nil {
		a.policies = policies
	}

	a.polErr = ""
	if err != nil {
		a.polErr = sectionError(err)
	}

	a.dirty = true
}

// add takes one parsed line of node's agent.
func (a *hubbleAgg) add(node string, line hubbleLine) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.dirty = true
	a.lost += line.lost

	if line.flow == nil {
		return
	}

	f := *line.flow
	if f.Node == "" {
		f.Node = node
	}

	a.seen++
	if n, ok := a.nodes[node]; ok {
		n.Flows++
		n.State, n.Error = hubbleNodeLive, ""
		n.last = max(n.last, f.Time)
	}

	if len(a.flows) < hubbleMaxFlows {
		a.flows = append(a.flows, f)
	} else {
		a.flows[a.head] = f
	}

	a.head = (a.head + 1) % hubbleMaxFlows

	if f.Verdict == "DROPPED" || f.Verdict == "AUDIT" {
		a.dropped++
		a.addDrop(f)
	}
}

func (a *hubbleAgg) addDrop(f hubbleFlow) {
	key := dropKey(f)

	g, ok := a.groups[key]
	if !ok {
		if len(a.groups) >= hubbleMaxGroups {
			a.evictOldestGroup()
		}

		g = &dropGroup{
			Source: f.Source, Destination: f.Destination, Protocol: f.Protocol, Port: f.Port,
			Direction: f.Direction, Verdict: f.Verdict, Reason: f.Reason, FirstSeen: f.Time,
			sourceLabel: peerLabels(f.Source), destLabel: peerLabels(f.Destination),
		}
		a.groups[key] = g
	}

	g.Count++
	g.LastSeen = max(g.LastSeen, f.Time)
	g.FirstSeen = min(g.FirstSeen, f.Time)
	g.Sample = f

	if f.Node != "" && !slices.Contains(g.Nodes, f.Node) {
		g.Nodes = append(g.Nodes, f.Node)
	}

	for _, ref := range f.DeniedBy {
		if !slices.Contains(g.DeniedBy, ref) {
			g.DeniedBy = append(g.DeniedBy, ref)
		}
	}
}

func (a *hubbleAgg) evictOldestGroup() {
	oldestKey, oldest := "", int64(0)

	for k, g := range a.groups {
		if oldestKey == "" || g.LastSeen < oldest {
			oldestKey, oldest = k, g.LastSeen
		}
	}

	delete(a.groups, oldestKey)
}

// peerLabels are p's labels for policy matching, with its namespace even when the flow
// left that label out.
func peerLabels(p hubblePeer) labelSet {
	labels := flowLabels(p.Labels)
	if _, ok := labels[ciliumNamespaceLabel]; !ok && p.Namespace != "" {
		labels[ciliumNamespaceLabel] = p.Namespace
	}

	return labels
}

// dropKey groups drops by who, to whom, how, and why: the source port changes on every retry.
func dropKey(f hubbleFlow) string {
	return peerKey(f.Source) + "|" + peerKey(f.Destination) + "|" + f.Protocol + "/" + strconv.Itoa(int(f.Port)) +
		"|" + f.Direction + "|" + f.Verdict + "|" + f.Reason
}

func peerKey(p hubblePeer) string {
	switch {
	case p.Workload != "":
		return p.Namespace + "/" + p.Workload
	case p.Pod != "":
		return p.Namespace + "/" + p.Pod
	case p.Reserved != "" && p.Reserved != "world":
		return "reserved:" + p.Reserved
	default:
		return p.IP
	}
}

// snapshot returns the current view, and whether anything changed since the last one.
func (a *hubbleAgg) snapshot(force bool) (hubbleSnapshot, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.dirty && !force {
		return hubbleSnapshot{}, false
	}

	a.dirty = false

	s := hubbleSnapshot{
		Namespace: a.status.Namespace, Version: a.status.Version, Buffer: a.status.Buffer,
		Nodes: make([]hubbleNodeState, 0, len(a.nodes)), Flows: make([]hubbleFlow, 0, len(a.flows)),
		Drops: make([]dropGroup, 0, len(a.groups)), Seen: a.seen, Dropped: a.dropped, Lost: a.lost, PoliciesError: a.polErr,
	}

	for _, n := range a.nodes {
		s.Nodes = append(s.Nodes, *n)
	}

	slices.SortFunc(s.Nodes, func(x, y hubbleNodeState) int { return cmp.Compare(x.Node, y.Node) })

	// Newest first: walk the ring backwards from the last written slot.
	for i := range a.flows {
		s.Flows = append(s.Flows, a.flows[(a.head-1-i+2*len(a.flows))%len(a.flows)])
	}

	for _, g := range a.groups {
		out := *g
		out.Nodes = slices.Clone(g.Nodes)
		out.DeniedBy = append([]policyRef{}, g.DeniedBy...)
		out.Isolating = a.isolatingLocked(g)
		s.Drops = append(s.Drops, out)
	}

	slices.SortFunc(s.Drops, func(x, y dropGroup) int {
		return cmp.Or(cmp.Compare(y.LastSeen, x.LastSeen), cmp.Compare(y.Count, x.Count))
	})

	return s, true
}

// isolatingLocked names the policies that put the endpoint enforcing g in default-deny:
// the destination for an ingress drop, the source for an egress one.
func (a *hubbleAgg) isolatingLocked(g *dropGroup) []policyRef {
	out := []policyRef{}
	if g.Reason != "POLICY_DENIED" && g.Verdict != "AUDIT" {
		return out
	}

	peer, labels := g.Destination, g.destLabel
	if g.Direction == "EGRESS" {
		peer, labels = g.Source, g.sourceLabel
	}

	if peer.Pod == "" {
		return out
	}

	return append(out, isolatingPolicies(a.policies, peer.Namespace, labels, g.Direction)...)
}
