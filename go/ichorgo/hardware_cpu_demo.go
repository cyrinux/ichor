package ichorgo

import "fmt"

// demoNUMA splits a demo node's CPUs and memory over two NUMA nodes.
func demoNUMA(n nodeOverview) []numaNode {
	half := n.CPUCount / 2
	memory := int64(n.MemTotal / 2) //nolint:gosec

	return []numaNode{
		{ID: 0, CPUs: demoCPURange(0, half), CPUCount: half, MemoryBytes: memory},
		{ID: 1, CPUs: demoCPURange(half, n.CPUCount), CPUCount: n.CPUCount - half, MemoryBytes: memory},
	}
}

// demoCPUScaling is one cpufreq policy per NUMA node on the performance governor, set by a
// CPUScalingConfig document.
func demoCPUScaling(n nodeOverview) []cpuScalingPolicy {
	half := n.CPUCount / 2
	policy := func(id string, from, to int) cpuScalingPolicy {
		return cpuScalingPolicy{
			Policy: id, CPUs: demoCPURange(from, to), Driver: "intel_pstate", Governor: "performance",
			EnergyPreference: "performance", MinKhz: 800000, MaxKhz: 3400000, ConfiguredBy: "all-cores",
		}
	}

	return []cpuScalingPolicy{policy("policy0", 0, half), policy("policy1", half, n.CPUCount)}
}

// demoCPURange is CPUs [from, to) as a cpulist.
func demoCPURange(from, to int) string {
	if to-from == 1 {
		return fmt.Sprint(from)
	}

	return fmt.Sprintf("%d-%d", from, to-1)
}
