package ichorgo

import (
	"errors"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

func TestBuildNodeOverviewHealthy(t *testing.T) {
	probe := nodeProbe{
		version: &machineapi.Version{
			Metadata: &common.Metadata{Hostname: "10.0.0.2"},
			Version:  &machineapi.VersionInfo{Tag: "v1.14.1", Arch: "arm64"},
			Platform: &machineapi.PlatformInfo{Name: "metal"},
		},
		status: &runtime.MachineStatusSpec{
			Stage:  runtime.MachineStageRunning,
			Status: runtime.MachineStatusStatus{Ready: true},
		},
		machineType: machine.TypeControlPlane,
		hostname:    "cp-1",
	}

	got := buildNodeOverview("10.0.0.2", probe)

	want := nodeOverview{
		Node: "10.0.0.2", Hostname: "cp-1", Reachable: true, Version: "v1.14.1",
		Arch: "arm64", Platform: "metal", Role: "controlplane", Stage: "running", Ready: true,
		UnmetConditions: []unmetCondition{},
	}

	if !equalJSON(t, got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestBuildNodeOverviewUnreachable(t *testing.T) {
	got := buildNodeOverview("10.0.0.9", nodeProbe{versionErr: errors.New("connection refused")})

	if got.Reachable || got.Ready {
		t.Errorf("unreachable node reported reachable/ready: %+v", got)
	}

	if got.Error == "" || got.Hostname != "10.0.0.9" || got.Role != "unknown" {
		t.Errorf("unexpected overview: %+v", got)
	}

	if got.ErrorKind != errorKindOther {
		t.Errorf("error kind: got %q, want %q", got.ErrorKind, errorKindOther)
	}
}

func TestBuildNodeOverviewUnreachableNetwork(t *testing.T) {
	dial := errors.New(`transport: Error while dialing: dial tcp 10.0.0.9:50000: connect: no route to host`)
	got := buildNodeOverview("10.0.0.9", nodeProbe{versionErr: dial})

	if got.ErrorKind != errorKindNetwork {
		t.Errorf("error kind: got %q, want %q", got.ErrorKind, errorKindNetwork)
	}
}

func TestBuildNodeOverviewNotReadyWithConditions(t *testing.T) {
	probe := nodeProbe{
		version: &machineapi.Version{Version: &machineapi.VersionInfo{Tag: "v1.14.1"}},
		status: &runtime.MachineStatusSpec{
			Stage: runtime.MachineStageBooting,
			Status: runtime.MachineStatusStatus{UnmetConditions: []runtime.UnmetCondition{
				{Name: "services", Reason: "service \"etcd\" not healthy"},
			}},
		},
		machineType: machine.TypeWorker,
		statusErr:   nil,
		hostnameErr: errors.New("not found"),
	}

	got := buildNodeOverview("10.0.0.3", probe)

	if !got.Reachable || got.Ready || got.Stage != "booting" || got.Role != "worker" {
		t.Errorf("unexpected overview: %+v", got)
	}

	if len(got.UnmetConditions) != 1 || got.UnmetConditions[0].Name != "services" {
		t.Errorf("conditions = %+v", got.UnmetConditions)
	}

	if got.Hostname != "10.0.0.3" {
		t.Errorf("hostname should fall back to node address, got %q", got.Hostname)
	}
}

func TestBuildNodeOverviewPartialFailureSurfacesError(t *testing.T) {
	probe := nodeProbe{
		version:   &machineapi.Version{Version: &machineapi.VersionInfo{Tag: "v1.14.1"}},
		statusErr: errors.New("permission denied"),
	}

	got := buildNodeOverview("10.0.0.4", probe)

	if !got.Reachable || got.Ready || got.Error == "" || got.Stage != "unknown" {
		t.Errorf("unexpected overview: %+v", got)
	}
}

func TestBuildNodeOverviewCapacity(t *testing.T) {
	probe := nodeProbe{
		version: &machineapi.Version{Version: &machineapi.VersionInfo{Tag: "v1.14.1"}},
		memory:  &machineapi.Memory{Meminfo: &machineapi.MemInfo{Memtotal: 8 * 1024 * 1024, Memavailable: 2 * 1024 * 1024}},
		cpu:     &machineapi.CPUsInfo{CpuInfo: []*machineapi.CPUInfo{{}, {}, {}, {}}},
	}

	got := buildNodeOverview("10.0.0.2", probe)

	if got.CPUCount != 4 || got.MemTotal != 8<<30 || got.MemAvailable != 2<<30 {
		t.Errorf("capacity = %d cpus, %d/%d bytes", got.CPUCount, got.MemAvailable, got.MemTotal)
	}
}

// Capacity is a best-effort extra: without it the node is still fine, just of unknown size.
func TestBuildNodeOverviewCapacityUnknown(t *testing.T) {
	got := buildNodeOverview("10.0.0.2", nodeProbe{version: &machineapi.Version{}})

	if got.CPUCount != 0 || got.MemTotal != 0 || got.MemAvailable != 0 || got.Error != "" {
		t.Errorf("missing capacity should stay zero without an error: %+v", got)
	}
}
