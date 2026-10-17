package ichorgo

import (
	"context"
	"slices"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

// A slow node must not hold the whole sample back past the app's next poll.
const clusterStatsNodeTimeout = 4 * time.Second

// clusterStats is one sample of the answering nodes' counters, for the live cluster summary.
//
// Sampled is how many nodes were asked (the chosen set, unknown nodes left out) and Total how
// many nodes the context has, so the apps say "live: N of M nodes" when Sampled < Total. A node
// asked that does not answer still counts in Sampled and is only missing from Nodes, as in a
// full sample: a down node does not make a full sample read as a partial one.
type clusterStats struct {
	At      int64          `json:"at"` // unix milliseconds
	Nodes   []nodeCounters `json:"nodes"`
	Sampled int            `json:"sampled"`
	Total   int            `json:"total"`
}

// nodeCounters are cumulative CPU times and current memory; the app derives usage from two samples.
type nodeCounters struct {
	Node         string  `json:"node"`
	CPUBusy      float64 `json:"cpuBusy"`
	CPUTotal     float64 `json:"cpuTotal"`
	CPUCount     int     `json:"cpuCount"`
	MemTotal     uint64  `json:"memTotal"`     // bytes
	MemAvailable uint64  `json:"memAvailable"` // bytes
}

// ClusterStats samples CPU and memory counters of every node of the context in parallel.
// Nodes that do not answer in time are left out rather than failing the sample.
func ClusterStats(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return clusterStatsOf(configYAML, contextName, "")
}

// ClusterStatsNodes samples only the given nodes (newline-separated, as the context names
// them; gomobile passes strings only). Empty samples every node, like ClusterStats. Nodes the
// context does not have are left out, never an error.
func ClusterStatsNodes(configYAML, contextName, nodes string) (out string, err error) {
	defer maskResult(&out, &err)

	// Screenshot mode hands back masked names: unmask them like any node list (comma-separated).
	contextName, nodes = unmaskTargets(configYAML, contextName, strings.ReplaceAll(nodes, "\n", ","))

	return clusterStatsOf(configYAML, contextName, nodes)
}

// clusterStatsOf samples the comma-separated nodes ("": every node); names are already unmasked.
func clusterStatsOf(configYAML, contextName, nodes string) (string, error) {
	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterStats", configYAML, contextName, "", nodes)
	}

	return withSession(configYAML, contextName, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		all := targetNodes(s.context)
		chosen := chooseNodes(all, nodes)
		samples := make([]*nodeCounters, len(chosen))

		forEachNode(chosen, func(i int, node string) { samples[i] = sampleCounters(ctx, s.client, node) })

		return toJSON(clusterStats{
			At: time.Now().UnixMilli(), Nodes: answered(samples), Sampled: len(chosen), Total: len(all),
		})
	})
}

// chooseNodes keeps the nodes of all that the comma-separated list names, in all's order;
// an empty list keeps every node.
func chooseNodes(all []string, list string) []string {
	if strings.Trim(list, ", \t") == "" {
		return all
	}

	wanted := map[string]bool{}

	for _, n := range strings.Split(list, ",") {
		if n = strings.TrimSpace(n); n != "" {
			wanted[n] = true
		}
	}

	return slices.DeleteFunc(slices.Clone(all), func(n string) bool { return !wanted[n] })
}

// sampleCounters returns nil when the node did not report its CPU times.
func sampleCounters(ctx context.Context, c *client.Client, node string) *nodeCounters {
	ctx, cancel := context.WithTimeout(ctx, clusterStatsNodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)

	stat, err := c.MachineClient.SystemStat(nodeCtx, &emptypb.Empty{})
	if err != nil {
		return nil
	}

	// Best effort: without memory the CPU sample is still worth having.
	mem, _ := c.Memory(nodeCtx) //nolint:errcheck

	return countersFrom(node, first(stat.GetMessages()), first(mem.GetMessages()))
}

func countersFrom(node string, sys *machineapi.SystemStat, mem *machineapi.Memory) *nodeCounters {
	if sys.GetCpuTotal() == nil {
		return nil
	}

	const kib = 1024

	busy, total := cpuTimes(sys.GetCpuTotal())

	return &nodeCounters{
		Node:         node,
		CPUBusy:      busy,
		CPUTotal:     total,
		CPUCount:     len(sys.GetCpu()),
		MemTotal:     mem.GetMeminfo().GetMemtotal() * kib,
		MemAvailable: mem.GetMeminfo().GetMemavailable() * kib,
	}
}

func answered(samples []*nodeCounters) []nodeCounters {
	out := make([]nodeCounters, 0, len(samples))

	for _, s := range samples {
		if s != nil {
			out = append(out, *s)
		}
	}

	return out
}
