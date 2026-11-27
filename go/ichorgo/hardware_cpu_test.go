package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

// The fixtures follow the yaml tags of NUMATopologySpec, CPUScalingStatusSpec and
// CPUScalingSpecSpec (pkg/machinery/resources/hardware at v1.15.0-beta.1).
const (
	numaTwoNodesYAML = `nodes:
    - cpus: [8, 9, 10, 11, 12, 13, 14, 15]
      memoryTotalBytes: 34359738368
      id: 1
    - cpus: [0, 1, 2, 3, 4, 5, 6, 7]
      memoryTotalBytes: 34359738368
      id: 0
presentCPUs: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15]
onlineCPUs: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15]
`
	cpuPolicyPerformanceYAML = `driver: intel_pstate
affectedCPUs: [0, 1, 2, 3]
relatedCPUs: [0, 1, 2, 3]
availableGovernors: [performance, powersave]
availableEPPs: [default, performance, balance_performance, power]
governor: performance
energyPerformancePreference: performance
cpuinfoMinFrequencyKhz: 800000
cpuinfoMaxFrequencyKhz: 5100000
cpuCapacity: 1024
coreType: performance
scalingMinFrequencyKhz: 800000
scalingMaxFrequencyKhz: 5100000
`
	cpuPolicyPowersaveYAML = `driver: intel_pstate
relatedCPUs: [4, 5]
governor: powersave
energyPerformancePreference: power
scalingMinFrequencyKhz: 800000
scalingMaxFrequencyKhz: 3000000
coreType: efficiency
`
	cpuScalingSpecYAML = `governor: performance
configName: performance-cores
`
)

func yamlSpec(t *testing.T, doc string) map[string]any {
	t.Helper()

	spec := map[string]any{}
	if err := yaml.Unmarshal([]byte(doc), &spec); err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestCompactCPUs(t *testing.T) {
	tests := []struct {
		in   []int64
		want string
	}{
		{nil, ""},
		{[]int64{3}, "3"},
		{[]int64{0, 1, 2, 3}, "0-3"},
		{[]int64{0, 1, 2, 3, 8, 10, 11}, "0-3,8,10-11"},
		{[]int64{11, 10, 0, 2, 1}, "0-2,10-11"}, // unsorted
		{[]int64{0, 0, 1, 1}, "0-1"},            // duplicates
		{[]int64{0, 2, 4}, "0,2,4"},
	}

	for _, tt := range tests {
		if got := compactCPUs(tt.in); got != tt.want {
			t.Errorf("compactCPUs(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMapNUMATopology(t *testing.T) {
	got := mapNUMATopology(yamlSpec(t, numaTwoNodesYAML))
	want := []numaNode{
		{ID: 0, CPUs: "0-7", CPUCount: 8, MemoryBytes: 32 << 30},
		{ID: 1, CPUs: "8-15", CPUCount: 8, MemoryBytes: 32 << 30},
	}

	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("numa = %+v, want %+v", got, want)
	}

	if got := mapNUMATopology(map[string]any{}); got == nil || len(got) != 0 {
		t.Errorf("empty spec = %#v", got)
	}
}

func TestMapCPUScaling(t *testing.T) {
	statuses := map[string]map[string]any{
		"policy10": yamlSpec(t, cpuPolicyPowersaveYAML),
		"policy2":  yamlSpec(t, cpuPolicyPerformanceYAML),
		"other":    {},
	}
	specs := map[string]map[string]any{"policy2": yamlSpec(t, cpuScalingSpecYAML)}

	got := mapCPUScaling(statuses, specs)
	if len(got) != 3 || got[0].Policy != "policy2" || got[1].Policy != "policy10" || got[2].Policy != "other" {
		t.Fatalf("order = %+v", got)
	}

	want := cpuScalingPolicy{
		Policy: "policy2", CPUs: "0-3", Driver: "intel_pstate", Governor: "performance", EnergyPreference: "performance",
		MinKhz: 800000, MaxKhz: 5100000, CoreType: "performance", ConfiguredBy: "performance-cores",
	}
	if got[0] != want {
		t.Errorf("policy2 = %+v, want %+v", got[0], want)
	}

	// No affected (online) CPUs: the related ones; no CPUScalingSpec: configured by nobody.
	if p := got[1]; p.CPUs != "4-5" || p.Governor != "powersave" || p.CoreType != "efficiency" || p.ConfiguredBy != "" {
		t.Errorf("policy10 = %+v", p)
	}
}

func TestCPUTopologyJSONShape(t *testing.T) {
	out, err := json.Marshal(nodeHardware{
		NUMA:       []numaNode{{ID: 0, CPUs: "0-3", CPUCount: 4, MemoryBytes: 1}},
		CPUScaling: []cpuScalingPolicy{{Policy: "policy0"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		`"numa":[{"id":0,"cpus":"0-3","cpuCount":4,"memoryBytes":1}]`,
		`"cpuScaling":[{"policy":"policy0","cpus":"","driver":"","governor":"","energyPreference":"","minKhz":0,"maxKhz":0,"coreType":"","configuredBy":""}]`,
	} {
		if !strings.Contains(string(out), key) {
			t.Errorf("JSON %s lacks %s", out, key)
		}
	}
}

func TestDemoCPUTopology(t *testing.T) {
	n := nodeOverview{CPUCount: 8, MemTotal: 16 << 30}

	numa := demoNUMA(n)
	if len(numa) != 2 || numa[0].CPUs != "0-3" || numa[1].CPUs != "4-7" || numa[1].MemoryBytes != 8<<30 {
		t.Errorf("demo numa = %+v", numa)
	}

	policies := demoCPUScaling(n)
	if len(policies) != 2 || policies[0].Governor != "performance" || policies[1].CPUs != "4-7" {
		t.Errorf("demo policies = %+v", policies)
	}
}

// rawHardwareResource is a Talos 1.15 hardware resource as the node sends it, kept raw.
func rawHardwareResource(t *testing.T, typ, id, specYAML string) *protobuf.Resource {
	t.Helper()

	r, err := protobuf.Unmarshal(&v1alpha1.Resource{
		Metadata: &v1alpha1.Metadata{Namespace: hardwareNamespace, Type: typ, Id: id, Version: "1", Phase: "running"},
		Spec:     &v1alpha1.Spec{YamlSpec: specYAML},
	})
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// TestNodeHardwareCPUTopologyFake reads both sections through the raw COSI list on a 1.15 node;
// a 1.14 node, which does not know the types, has both empty and no error.
func TestNodeHardwareCPUTopologyFake(t *testing.T) {
	const newer, older = "192.0.2.81", "192.0.2.82"

	f := newFakeTalos()
	f.addNode(t, newer, "v1.15.0", machine.TypeWorker)
	f.addNode(t, older, "v1.14.7", machine.TypeWorker)

	for _, typ := range []string{numaTopologyType, cpuScalingStatusType, cpuScalingSpecType} {
		f.put(newer, resourceDefinition(t, meta.ResourceDefinitionSpec{Type: typ, DefaultNamespace: hardwareNamespace}))
	}

	f.put(newer,
		rawHardwareResource(t, numaTopologyType, "numa", numaTwoNodesYAML),
		rawHardwareResource(t, cpuScalingStatusType, "policy0", cpuPolicyPerformanceYAML),
		rawHardwareResource(t, cpuScalingStatusType, "policy4", cpuPolicyPowersaveYAML),
		rawHardwareResource(t, cpuScalingSpecType, "policy0", cpuScalingSpecYAML),
	)

	cfg := f.start(t, newer, older)

	out, err := NodeHardware(cfg, "fake", newer)
	got := decodeJSON[nodeHardware](t, out, err)

	if got.Errors["numa"] != "" || got.Errors["cpuScaling"] != "" {
		t.Errorf("errors = %v", got.Errors)
	}

	if len(got.NUMA) != 2 || got.NUMA[1].CPUs != "8-15" {
		t.Errorf("numa = %+v", got.NUMA)
	}

	if len(got.CPUScaling) != 2 || got.CPUScaling[0].ConfiguredBy != "performance-cores" || got.CPUScaling[1].Governor != "powersave" {
		t.Errorf("cpuScaling = %+v", got.CPUScaling)
	}

	out, err = NodeHardware(cfg, "fake", older)
	old := decodeJSON[nodeHardware](t, out, err)

	if old.NUMA == nil || len(old.NUMA) != 0 || old.CPUScaling == nil || len(old.CPUScaling) != 0 {
		t.Errorf("1.14 node: numa %v, cpuScaling %v (want empty lists)", old.NUMA, old.CPUScaling)
	}

	if old.Errors["numa"] != "" || old.Errors["cpuScaling"] != "" {
		t.Errorf("1.14 node errors = %v", old.Errors)
	}
}
