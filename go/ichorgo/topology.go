package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// ClusterTopology maps the cluster for the app's graph (os:reader): the members (cluster
// discovery), each targeted node's KubeSpan peers, and its zone and region from the
// topology.kubernetes.io labels or, on a cloud, the platform metadata. Nodes are grouped
// into sites by zone, else by the LAN they share; the zone's country is guessed for a flag.
// Every part is best-effort: without discovery or KubeSpan the map just has less to show.
func ClusterTopology(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterTopology", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		targets := targetNodes(s.context)
		observations := make([]topologyObservation, len(targets))

		var wg sync.WaitGroup

		for i, node := range targets {
			wg.Go(func() { observations[i] = observeTopology(ctx, s.client, node) })
		}

		wg.Wait()

		members, _ := listMembers(ctx, s.client, targets) // no discovery: the targets and their peers

		topology := buildTopology(members, observations)
		for _, n := range topology.Nodes {
			privacy.learnHost(n.Hostname, n.Role)
		}

		if privacy.isEnabled() {
			topology = hideLocations(topology)
		}

		return toJSON(topology)
	})
}

// hideLocations is the map for screenshot mode: zones say where the servers are, so each
// becomes a neutral "zone-N" (the same for the same zone, sites still group), and the region
// and guessed country (the flag) are dropped. The privacy mask only knows addresses and names.
func hideLocations(t clusterTopology) clusterTopology {
	fakes := map[string]string{}
	fake := func(zone string) string {
		if zone == "" {
			return ""
		}

		if _, ok := fakes[zone]; !ok {
			fakes[zone] = fmt.Sprintf("zone-%d", len(fakes)+1)
		}

		return fakes[zone]
	}

	out := clusterTopology{Nodes: slices.Clone(t.Nodes), Links: t.Links, Sites: slices.Clone(t.Sites)}

	for i, n := range out.Nodes {
		n.Zone, n.Region, n.Country = fake(n.Zone), "", ""
		out.Nodes[i] = n
	}

	for i, s := range out.Sites {
		if s.Kind == "zone" {
			s.Label = fake(s.Label)
		}

		s.Country = ""
		out.Sites[i] = s
	}

	return out
}

// observeTopology reads what one node knows: its hostname, KubeSpan peers, node labels and
// platform metadata. Only an unreachable node (no hostname) is reported as an error.
func observeTopology(ctx context.Context, c *client.Client, node string) topologyObservation {
	nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
	defer cancel()

	o := topologyObservation{node: node}

	hostname, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, c.COSI, network.HostnameID)
	if err != nil {
		o.err = err

		return o
	}

	o.hostname = hostname.TypedSpec().Hostname

	if peers, err := safe.StateListAll[*kubespan.PeerStatus](nodeCtx, c.COSI); err == nil {
		for res := range peers.All() {
			o.peers = append(o.peers, kubespanPeerInput{id: res.Metadata().ID(), spec: *res.TypedSpec()})
		}
	}

	// Talos keeps the status of its own Kubernetes node only, labels included.
	if statuses, err := safe.StateListAll[*k8s.NodeStatus](nodeCtx, c.COSI); err == nil {
		for res := range statuses.All() {
			o.labels = res.TypedSpec().Labels
		}
	}

	if md, err := safe.StateGetByID[*runtime.PlatformMetadata](nodeCtx, c.COSI, runtime.PlatformMetadataID); err == nil {
		o.platformZone, o.platformRegion = md.TypedSpec().Zone, md.TypedSpec().Region
	}

	return o
}
