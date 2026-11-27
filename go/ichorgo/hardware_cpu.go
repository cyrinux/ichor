package ichorgo

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// Talos 1.15's CPU topology resources, in the hardware namespace (pkg/machinery/resources/
// hardware at v1.15.0-beta.1). This client's machinery (1.14) has no Go types for them: they are
// read raw by type string and their YAML spec decoded by key.
//
//   - NUMATopology (numa_topology.go), one resource with id "numa":
//     nodes[{id, cpus[], memoryTotalBytes}], presentCPUs[], onlineCPUs[].
//   - CPUScalingStatus (cpu_scaling_status.go), one per cpufreq policy ("policy0"): driver,
//     affectedCPUs[], relatedCPUs[], governor, energyPerformancePreference,
//     scalingMinFrequencyKhz, scalingMaxFrequencyKhz, coreType (hybrid CPUs only), and more.
//   - CPUScalingSpec (cpu_scaling_spec.go), same ids: what a CPUScalingConfig document asks of
//     the policy; configName names the document that claimed it.
//
// Host inventory is not exposed when Talos runs in a container, and a VM often has no cpufreq
// driver: the lists are then empty and the apps show no card.
const (
	numaTopologyType     = "NUMATopologies.hardware.talos.dev"
	cpuScalingStatusType = "CPUScalingStatuses.hardware.talos.dev"
	cpuScalingSpecType   = "CPUScalingSpecs.hardware.talos.dev"
	// hardwareNamespace is hardware.NamespaceName.
	hardwareNamespace = "hardware"
)

// numaNode is one NUMA node of the host: its CPUs as compact ranges ("0-15,32-47") and memory.
type numaNode struct {
	ID          int64  `json:"id"`
	CPUs        string `json:"cpus"`
	CPUCount    int    `json:"cpuCount"`
	MemoryBytes int64  `json:"memoryBytes"`
}

// cpuScalingPolicy is one cpufreq policy: the CPUs it drives, its governor and frequency window.
type cpuScalingPolicy struct {
	Policy           string `json:"policy"` // policy0...
	CPUs             string `json:"cpus"`   // compact ranges of the online CPUs it drives
	Driver           string `json:"driver"` // intel_pstate, acpi-cpufreq, amd-pstate...
	Governor         string `json:"governor"`
	EnergyPreference string `json:"energyPreference"` // "" unless the driver has EPP
	MinKhz           int64  `json:"minKhz"`           // the window the governor may pick from
	MaxKhz           int64  `json:"maxKhz"`
	CoreType         string `json:"coreType"`     // performance | efficiency on hybrid CPUs, else ""
	ConfiguredBy     string `json:"configuredBy"` // the CPUScalingConfig document, "" when none
}

// readCPUTopology fills out's NUMA and CPU scaling sections when the node knows the types
// (Talos 1.15+); a failure goes to errors["numa"] or errors["cpuScaling"].
func readCPUTopology(ctx context.Context, s *session, node string, out *nodeHardware) {
	if specs, err := readKnownRaw(ctx, s, node, numaTopologyType); err != nil {
		out.Errors["numa"] = s.friendly(node, err)
	} else if len(specs) > 0 {
		out.NUMA = mapNUMATopology(specs[0])
	}

	statuses, err := readKnownRawByID(ctx, s, node, cpuScalingStatusType)
	if err != nil {
		out.Errors["cpuScaling"] = s.friendly(node, err)

		return
	}

	if len(statuses) == 0 {
		return
	}

	// The spec only adds who configured the policy: without it the status still shows.
	specs, _ := readKnownRawByID(ctx, s, node, cpuScalingSpecType) //nolint:errcheck
	out.CPUScaling = mapCPUScaling(statuses, specs)
}

// readKnownRaw lists a type's specs, none when the node does not know the type.
func readKnownRaw(ctx context.Context, s *session, node, resourceType string) ([]map[string]any, error) {
	known, err := s.hasResourceType(ctx, node, resourceType)
	if err != nil || !known {
		return nil, err
	}

	return listRawSpecs(ctx, s, hardwareNamespace, resourceType)
}

// readKnownRawByID is readKnownRaw keyed by resource id.
func readKnownRawByID(ctx context.Context, s *session, node, resourceType string) (map[string]map[string]any, error) {
	known, err := s.hasResourceType(ctx, node, resourceType)
	if err != nil || !known {
		return nil, err
	}

	return listRawSpecsByID(ctx, s, hardwareNamespace, resourceType)
}

// mapNUMATopology is the NUMA nodes by id; a single-node machine is reported too (the apps hide it).
func mapNUMATopology(spec map[string]any) []numaNode {
	nodes := specList(spec, "nodes")
	out := make([]numaNode, 0, len(nodes))

	for _, n := range nodes {
		cpus := specInts(n, "cpus")
		out = append(out, numaNode{
			ID:          specIntOr0(n, "id"),
			CPUs:        compactCPUs(cpus),
			CPUCount:    len(cpus),
			MemoryBytes: specIntOr0(n, "memoryTotalBytes"),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

// mapCPUScaling is the policies in their kernel order (policy2 before policy10), each with the
// config document its CPUScalingSpec names.
func mapCPUScaling(statuses, specs map[string]map[string]any) []cpuScalingPolicy {
	out := make([]cpuScalingPolicy, 0, len(statuses))

	for id, st := range statuses {
		cpus := specInts(st, "affectedCPUs")
		if len(cpus) == 0 {
			cpus = specInts(st, "relatedCPUs")
		}

		out = append(out, cpuScalingPolicy{
			Policy:           id,
			CPUs:             compactCPUs(cpus),
			Driver:           specString(st, "driver"),
			Governor:         specString(st, "governor"),
			EnergyPreference: specString(st, "energyPerformancePreference"),
			MinKhz:           specIntOr0(st, "scalingMinFrequencyKhz"),
			MaxKhz:           specIntOr0(st, "scalingMaxFrequencyKhz"),
			CoreType:         specString(st, "coreType"),
			ConfiguredBy:     specString(specs[id], "configName"),
		})
	}

	sort.Slice(out, func(i, j int) bool { return policyLess(out[i].Policy, out[j].Policy) })

	return out
}

// policyLess orders "policyN" ids by N, other ids after them by name.
func policyLess(a, b string) bool {
	na, errA := strconv.Atoi(strings.TrimPrefix(a, "policy"))
	nb, errB := strconv.Atoi(strings.TrimPrefix(b, "policy"))

	switch {
	case errA == nil && errB == nil:
		return na < nb
	case errA == nil || errB == nil:
		return errA == nil
	default:
		return a < b
	}
}

// specInts is the integers of a YAML list; never nil.
func specInts(spec map[string]any, key string) []int64 {
	raw, _ := spec[key].([]any) //nolint:errcheck
	out := make([]int64, 0, len(raw))

	for _, item := range raw {
		if n := yamlInt(item); n != nil {
			out = append(out, *n)
		}
	}

	return out
}

// compactCPUs writes CPU numbers as ranges, like the kernel's cpulist: [0 1 2 3 8 10 11] is
// "0-3,8,10-11". The input need not be sorted; duplicates are dropped.
func compactCPUs(cpus []int64) string {
	sorted := append([]int64(nil), cpus...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	parts := []string{}

	for i := 0; i < len(sorted); {
		j := i
		for j+1 < len(sorted) && sorted[j+1] <= sorted[j]+1 {
			j++
		}

		if sorted[i] == sorted[j] {
			parts = append(parts, strconv.FormatInt(sorted[i], 10))
		} else {
			parts = append(parts, strconv.FormatInt(sorted[i], 10)+"-"+strconv.FormatInt(sorted[j], 10))
		}

		i = j + 1
	}

	return strings.Join(parts, ",")
}
