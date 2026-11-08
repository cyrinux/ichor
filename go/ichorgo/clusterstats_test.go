package ichorgo

import (
	"context"
	"slices"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestCountersFrom(t *testing.T) {
	sys := &machineapi.SystemStat{
		CpuTotal: &machineapi.CPUStat{User: 30, System: 10, Idle: 50, Iowait: 10},
		Cpu:      []*machineapi.CPUStat{{}, {}, {}, {}},
	}
	mem := &machineapi.Memory{Meminfo: &machineapi.MemInfo{Memtotal: 8, Memavailable: 2}}

	got := countersFrom("10.0.0.2", sys, mem)
	want := &nodeCounters{Node: "10.0.0.2", CPUBusy: 40, CPUTotal: 100, CPUCount: 4, MemTotal: 8 * 1024, MemAvailable: 2 * 1024}

	if !equalJSON(t, got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestCountersFromWithoutMemoryKeepsCPU(t *testing.T) {
	sys := &machineapi.SystemStat{CpuTotal: &machineapi.CPUStat{User: 1, Idle: 3}}

	got := countersFrom("10.0.0.2", sys, nil)
	if got == nil || got.CPUTotal != 4 || got.MemTotal != 0 {
		t.Errorf("unexpected counters: %+v", got)
	}
}

func TestCountersFromWithoutCPUTimesIsNil(t *testing.T) {
	if got := countersFrom("10.0.0.2", &machineapi.SystemStat{}, nil); got != nil {
		t.Errorf("want nil, got %+v", got)
	}

	if got := countersFrom("10.0.0.2", nil, nil); got != nil {
		t.Errorf("want nil for a missing message, got %+v", got)
	}
}

func TestAnsweredDropsSilentNodes(t *testing.T) {
	got := answered([]*nodeCounters{{Node: "a"}, nil, {Node: "c"}})
	if len(got) != 2 || got[0].Node != "a" || got[1].Node != "c" {
		t.Errorf("unexpected nodes: %+v", got)
	}

	if got := answered(nil); got == nil {
		t.Error("an empty sample must encode as [], not null")
	}
}

// SystemStat answers fixed CPU times (60 busy of 100) on every node that is up.
func (m fakeTalosMachine) SystemStat(ctx context.Context, _ *emptypb.Empty) (*machineapi.SystemStatResponse, error) {
	if _, err := m.f.enter(ctx, "SystemStat"); err != nil {
		return nil, err
	}

	total := &machineapi.CPUStat{User: 50, System: 10, Idle: 40}

	return &machineapi.SystemStatResponse{Messages: []*machineapi.SystemStat{{CpuTotal: total, Cpu: []*machineapi.CPUStat{total}}}}, nil
}

// Memory answers 4 KiB total, 1 KiB available on every node that is up.
func (m fakeTalosMachine) Memory(ctx context.Context, _ *emptypb.Empty) (*machineapi.MemoryResponse, error) {
	if _, err := m.f.enter(ctx, "Memory"); err != nil {
		return nil, err
	}

	return &machineapi.MemoryResponse{Messages: []*machineapi.Memory{{Meminfo: &machineapi.MemInfo{Memtotal: 4, Memavailable: 1}}}}, nil
}

func TestClusterStatsNodes(t *testing.T) {
	nodes := []string{"192.0.2.61", "192.0.2.62", "192.0.2.63", "192.0.2.64"}

	f := newFakeTalos()
	for _, n := range nodes[:3] { // the fourth node is down
		f.addNode(t, n, "v1.11.0", machine.TypeWorker)
	}

	cfg := f.start(t, nodes...)

	tests := []struct {
		name    string
		nodes   string
		want    []string
		sampled int
	}{
		{name: "empty samples every node", nodes: "", want: nodes[:3], sampled: 4},
		{name: "subset samples only those nodes", nodes: "192.0.2.63\n192.0.2.61", want: []string{"192.0.2.61", "192.0.2.63"}, sampled: 2},
		{name: "unknown nodes are left out", nodes: "192.0.2.62\nnode-gone.example.test\n", want: []string{"192.0.2.62"}, sampled: 1},
		{name: "a down node counts as sampled", nodes: "192.0.2.64\n192.0.2.61", want: []string{"192.0.2.61"}, sampled: 2},
		{name: "only unknown nodes samples none", nodes: "node-gone.example.test", want: []string{}, sampled: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(f.called("SystemStat"))

			out, err := ClusterStatsNodes(cfg, "", tt.nodes)
			got := decodeJSON[clusterStats](t, out, err)

			names := []string{}
			for _, n := range got.Nodes {
				names = append(names, n.Node)
			}

			if !slices.Equal(names, tt.want) || got.Sampled != tt.sampled || got.Total != len(nodes) {
				t.Errorf("nodes %v sampled %d total %d, want %v %d %d", names, got.Sampled, got.Total, tt.want, tt.sampled, len(nodes))
			}

			if asked := len(f.called("SystemStat")) - before; asked != tt.sampled {
				t.Errorf("asked %d nodes, want %d", asked, tt.sampled)
			}
		})
	}
}

func TestClusterStatsSamplesEveryNode(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.71", "v1.11.0", machine.TypeControlPlane)
	cfg := f.start(t, "192.0.2.71")

	out, err := ClusterStats(cfg, "")
	got := decodeJSON[clusterStats](t, out, err)

	if len(got.Nodes) != 1 || got.Nodes[0].CPUBusy != 60 || got.Nodes[0].MemTotal != 4*1024 || got.Sampled != 1 || got.Total != 1 {
		t.Errorf("unexpected sample: %+v", got)
	}
}

func TestClusterStatsNodesDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	out, err := ClusterStats(yaml, "")
	all := decodeJSON[clusterStats](t, out, err)

	if len(all.Nodes) < 2 || all.Sampled != len(all.Nodes) || all.Total != len(all.Nodes) {
		t.Fatalf("full demo sample: %+v", all)
	}

	first := all.Nodes[0].Node

	out, err = ClusterStatsNodes(yaml, "", first+"\nnode-gone.example.test")
	one := decodeJSON[clusterStats](t, out, err)

	if len(one.Nodes) != 1 || one.Nodes[0].Node != first || one.Sampled != 1 || one.Total != all.Total {
		t.Errorf("demo subset: %+v", one)
	}
}
