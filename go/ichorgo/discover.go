package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/cluster"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// errNoMembers: the nodes answered but know no member, so cluster discovery is off.
var errNoMembers = errors.New("no cluster members found: is cluster discovery enabled in the machine config?")

type nodeDiscovery struct {
	Context string           `json:"context"`
	Nodes   []discoveredNode `json:"nodes"`
}

// discoveredNode is a cluster member; Known when the context already targets it.
type discoveredNode struct {
	Address   string   `json:"address"`
	Addresses []string `json:"addresses"`
	Hostname  string   `json:"hostname"`
	Role      string   `json:"role"`
	Known     bool     `json:"known"`
}

// clusterMember is the part of a cluster.Member discovery needs.
type clusterMember struct {
	hostname  string
	role      string
	addresses []netip.Addr
}

// DiscoverNodes lists the cluster's members (Talos cluster discovery) as seen by the first
// context node that answers, and flags those the context already targets. Lets a
// talosconfig listing only an endpoint or a few nodes learn the rest of the cluster.
func DiscoverNodes(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoDiscovery(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		targets := targetNodes(s.context)

		members, err := listMembers(ctx, s.client, targets)
		if err != nil {
			return "", err
		}

		nodes := classifyMembers(targets, targetHostnames(ctx, s.client, targets), members)
		learnDiscoveredHosts(nodes)

		return toJSON(nodeDiscovery{Context: name, Nodes: nodes})
	})
}

// listMembers asks the targets in turn, the first one that answers wins.
func listMembers(ctx context.Context, c *client.Client, targets []string) ([]clusterMember, error) {
	var errs []error

	for _, node := range targets {
		members, err := nodeMembers(ctx, c, node)
		if err == nil {
			if len(members) == 0 {
				return nil, errNoMembers
			}

			return members, nil
		}

		errs = append(errs, fmt.Errorf("%s: %s", node, friendlyError(err)))
	}

	if len(errs) == 0 {
		return nil, errors.New("the context has no endpoint or node")
	}

	return nil, errors.Join(errs...)
}

func nodeMembers(ctx context.Context, c *client.Client, node string) ([]clusterMember, error) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	list, err := safe.StateListAll[*cluster.Member](client.WithNode(ctx, node), c.COSI)
	if err != nil {
		return nil, err
	}

	var out []clusterMember

	for m := range list.All() {
		spec := m.TypedSpec()
		out = append(out, clusterMember{hostname: spec.Hostname, role: roleName(spec.MachineType, nil), addresses: spec.Addresses})
	}

	return out, nil
}

// targetHostnames is each target's hostname, "" when it does not answer: an endpoint that
// is a VIP or a DNS name still matches the member behind it.
func targetHostnames(ctx context.Context, c *client.Client, targets []string) []string {
	out := make([]string, len(targets))

	forEachNode(targets, func(i int, node string) {
		nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
		defer cancel()

		if hs, err := safe.StateGetByID[*network.HostnameStatus](client.WithNode(nodeCtx, node), c.COSI, network.HostnameID); err == nil {
			out[i] = hs.TypedSpec().Hostname
		}
	})

	return out
}

// classifyMembers turns members into discovered nodes, sorted control planes first then by
// hostname. A member is known when one of its addresses or its hostname is a target.
func classifyMembers(targets, hostnames []string, members []clusterMember) []discoveredNode {
	known := make(map[string]bool, len(targets)+len(hostnames))

	for _, t := range append(slices.Clone(targets), hostnames...) {
		if t != "" {
			known[strings.ToLower(t)] = true
		}
	}

	out := make([]discoveredNode, 0, len(members))

	for _, m := range members {
		addresses := make([]string, len(m.addresses))
		for i, a := range m.addresses {
			addresses[i] = a.String()
		}

		isKnown := known[strings.ToLower(m.hostname)]
		for _, a := range addresses {
			isKnown = isKnown || known[a]
		}

		out = append(out, discoveredNode{
			Address:   preferredAddress(m.addresses),
			Addresses: addresses,
			Hostname:  m.hostname,
			Role:      m.role,
			Known:     isKnown,
		})
	}

	slices.SortFunc(out, func(a, b discoveredNode) int {
		if (a.Role == "controlplane") != (b.Role == "controlplane") {
			if a.Role == "controlplane" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Hostname, b.Hostname)
	})

	return out
}

// preferredAddress is the address to reach a clusterMember at: a global IPv4 first (the one
// talosconfigs usually list), then a global IPv6, then whatever there is.
func preferredAddress(addresses []netip.Addr) string {
	rank := func(a netip.Addr) int {
		switch {
		case a.Is4() && !a.IsLinkLocalUnicast() && !a.IsLoopback():
			return 0
		case a.IsGlobalUnicast() && !a.IsLinkLocalUnicast():
			return 1
		default:
			return 2
		}
	}

	best := ""
	bestRank := 3

	for _, a := range addresses {
		if r := rank(a); r < bestRank {
			best, bestRank = a.String(), r
		}
	}

	return best
}

// AddContextNodes returns storedYAML with nodes (comma-separated) added to contextName's
// nodes. A context without nodes targets its endpoints: they become its first nodes, so
// adding some does not drop them from the overview.
func AddContextNodes(storedYAML, contextName, nodes string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName, nodes = unmaskTargets(storedYAML, contextName, nodes)

	stored, err := parseTalosconfig(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	target, ok := stored.Contexts[contextName]
	if !ok || target == nil {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	updated := *target
	updated.Nodes = targetNodes(target)

	for _, n := range strings.Split(nodes, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}

		if _, err := netip.ParseAddr(n); err != nil && !validHostname(n) {
			return "", fmt.Errorf("invalid node address %q", n)
		}

		if !slices.Contains(updated.Nodes, n) {
			updated.Nodes = append(updated.Nodes, n)
		}
	}

	contexts := make(map[string]*clientconfig.Context, len(stored.Contexts))
	for name, c := range stored.Contexts {
		contexts[name] = c
	}

	contexts[contextName] = &updated

	result := *stored
	result.Contexts = contexts

	return encodeConfig(&result)
}

// validHostname: letters, digits, dots and dashes, as a DNS name.
func validHostname(s string) bool {
	if len(s) > 253 {
		return false
	}

	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}

	return s != ""
}

// demoDiscovery: the demo context's nodes, all known.
func demoDiscovery(yaml, name string) (string, error) {
	contextName, _, err := resolveContext(yaml, name)
	if err != nil {
		return "", err
	}

	nodes := []discoveredNode{}
	for _, n := range demoNodes() {
		nodes = append(nodes, discoveredNode{Address: n.Node, Addresses: []string{n.Node}, Hostname: n.Hostname, Role: n.Role, Known: true})
	}

	return toJSON(nodeDiscovery{Context: contextName, Nodes: nodes})
}
