package talosmobile

import (
	"context"
	"sync"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

// A slow node must not hold the whole sample back past the app's next poll.
const clusterStatsNodeTimeout = 4 * time.Second

// clusterStats is one sample of every answering node's counters, for the live cluster summary.
type clusterStats struct {
	At    int64          `json:"at"` // unix milliseconds
	Nodes []nodeCounters `json:"nodes"`
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

	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterStats", configYAML, contextName, "")
	}

	return withSession(configYAML, contextName, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		nodes := targetNodes(s.context)
		samples := make([]*nodeCounters, len(nodes))

		var wg sync.WaitGroup

		for i, node := range nodes {
			wg.Go(func() { samples[i] = sampleCounters(ctx, s.client, node) })
		}

		wg.Wait()

		return toJSON(clusterStats{At: time.Now().UnixMilli(), Nodes: answered(samples)})
	})
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
