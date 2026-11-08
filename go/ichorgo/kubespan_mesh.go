package ichorgo

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// kubespanMeshHealth is the background monitor's view of the KubeSpan mesh. Issues are keyed
// "<idA>|<idB>" (both node ids, sorted, so a tunnel seen from either side is one key) and valued
// "critical|<reason>|<since>|<host-a>|<host-b>", the hosts in the key's order.
type kubespanMeshHealth struct {
	Context string `json:"context"`
	Checked bool   `json:"checked"`
	// Partial: a target could not be read, so a tunnel missing from Issues may only be out of
	// sight. The apps keep the issues they knew instead of saying "back".
	Partial bool              `json:"partial"`
	Links   int               `json:"links"`
	Broken  int               `json:"broken"`
	Issues  map[string]string `json:"issues"`
}

// KubeSpanMeshHealth reads, for every node of the context in parallel, its hostname and KubeSpan
// peers, and returns the tunnels Go judges broken (see kubespanBrokenReason): what the KubeSpan
// alert of the background monitor needs (os:reader). It skips the discovery members and the
// location reads of ClusterTopology. A node that does not answer never fails the call.
func KubeSpanMeshHealth(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("KubeSpanMeshHealth", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		targets := targetNodes(s.context)
		observations := make([]topologyObservation, len(targets))

		forEachNode(targets, func(i int, node string) { observations[i] = observeMesh(ctx, s.client, node) })

		topology := buildTopology(nil, observations)
		for _, n := range topology.Nodes {
			privacy.learnHost(n.Hostname, n.Role)
		}

		partial := slices.ContainsFunc(observations, func(o topologyObservation) bool { return o.err != nil })

		return toJSON(meshHealthOf(name, topology, partial))
	})
}

// observeMesh reads what the mesh needs of one node. A node whose peers cannot be read counts as
// unreachable: its links are not judged, and the read is partial.
func observeMesh(ctx context.Context, c *client.Client, node string) topologyObservation {
	nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
	defer cancel()

	o := topologyObservation{node: node}

	hostname, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, c.COSI, network.HostnameID)
	if err != nil {
		o.err = err

		return o
	}

	o.hostname = hostname.TypedSpec().Hostname

	peers, err := safe.StateListAll[*kubespan.PeerStatus](nodeCtx, c.COSI)
	if err != nil {
		o.err = err

		return o
	}

	for res := range peers.All() {
		o.peers = append(o.peers, kubespanPeerInput{id: res.Metadata().ID(), created: res.Metadata().Created(), spec: *res.TypedSpec()})
	}

	return o
}

// meshHealthOf keeps the broken links of topology. Without the member list, a target that did not
// answer is known by its address while its peers name it by hostname: on a partial read, a link
// to a node that was not read may be that target, so it is not reported.
func meshHealthOf(contextName string, topology clusterTopology, partial bool) kubespanMeshHealth {
	nodes := make(map[string]topologyNode, len(topology.Nodes))
	for _, n := range topology.Nodes {
		nodes[n.ID] = n
	}

	out := kubespanMeshHealth{
		Context: contextName, Checked: topology.Mesh.Checked, Partial: partial,
		Links: topology.Mesh.Links, Issues: map[string]string{},
	}

	for _, l := range topology.Links {
		if !l.Broken || partial && (!nodes[l.A].Queried || !nodes[l.B].Queried) {
			continue
		}

		out.Issues[l.A+"|"+l.B] = strings.Join([]string{
			fleetCritical, l.Reason, strconv.FormatInt(l.Since, 10), meshHost(nodes[l.A], l.A), meshHost(nodes[l.B], l.B),
		}, "|")
	}

	out.Broken = len(out.Issues)

	return out
}

// meshHost is a node's name for an issue value, without the separator.
func meshHost(n topologyNode, id string) string {
	host := n.Hostname
	if host == "" {
		host = id
	}

	return strings.ReplaceAll(host, "|", "/")
}

// demoMeshHealth is the demo cluster's: the two broken tunnels of its map.
func demoMeshHealth(contextName string) kubespanMeshHealth {
	return meshHealthOf(contextName, demoTopology(), false)
}
