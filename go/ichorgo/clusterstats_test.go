package ichorgo

import (
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
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
