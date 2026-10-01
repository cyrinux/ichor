package talosmobile

import (
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMapServicesSortedWithHealth(t *testing.T) {
	changed := time.Unix(1_700_000_000, 0)

	in := []*machineapi.ServiceInfo{
		{Id: "kubelet", State: "Running", Health: &machineapi.ServiceHealth{Healthy: true, LastChange: timestamppb.New(changed)}},
		{Id: "apid", State: "Running", Health: &machineapi.ServiceHealth{Unknown: true}},
		{Id: "etcd", State: "Preparing", Health: &machineapi.ServiceHealth{LastMessage: "waiting for peers"},
			Events: &machineapi.ServiceEvents{Events: []*machineapi.ServiceEvent{{Msg: "old"}, {Msg: "Starting etcd"}}}},
	}

	got := mapServices(in)

	want := []serviceInfo{
		{ID: "apid", State: "Running", Health: "unknown"},
		{ID: "etcd", State: "Preparing", Health: "unhealthy", Message: "waiting for peers", LastEvent: "Starting etcd"},
		{ID: "kubelet", State: "Running", Health: "healthy", LastChange: changed.Unix()},
	}

	if !equalJSON(t, got, want) {
		t.Fail()
	}
}

func TestMapServicesNilHealthIsUnknown(t *testing.T) {
	got := mapServices([]*machineapi.ServiceInfo{{Id: "x", State: "Finished"}})

	if len(got) != 1 || got[0].Health != "unknown" {
		t.Errorf("got %+v", got)
	}
}

func TestBuildNodeResources(t *testing.T) {
	r := buildNodeResources(
		&machineapi.Memory{Meminfo: &machineapi.MemInfo{Memtotal: 8 << 20, Memavailable: 2 << 20, Swaptotal: 0}},
		&machineapi.LoadAvg{Load1: 0.5, Load5: 1.25, Load15: 2},
		&machineapi.SystemStat{BootTime: 1_700_000_000},
		&machineapi.CPUsInfo{CpuInfo: []*machineapi.CPUInfo{{ModelName: "Cortex-A76"}, {ModelName: "Cortex-A76"}}},
		&machineapi.Mounts{Stats: []*machineapi.MountStat{
			{Filesystem: "/dev/sda6", MountedOn: "/var", Size: 100, Available: 40},
			{Filesystem: "tmpfs", MountedOn: "/run", Size: 0, Available: 0},
			{Filesystem: "none", MountedOn: "/etc", Size: 8, Available: 8},
			{Filesystem: "/dev/sda6", MountedOn: "/var/lib/containerd", Size: 100, Available: 40},
			{Filesystem: "/dev/longhorn/pvc-1", MountedOn: "/var/lib/kubelet/plugins/csi/globalmount", Size: 5, Available: 1},
			{Filesystem: "/dev/sda5", MountedOn: "/system/state", Size: 10, Available: 9},
		}},
	)

	want := nodeResources{
		MemTotal: 8 << 30, MemAvailable: 2 << 30,
		Load1: 0.5, Load5: 1.25, Load15: 2,
		BootTime: 1_700_000_000, CPUCount: 2, CPUModel: "Cortex-A76",
		Mounts: []mountUsage{
			{Filesystem: "/dev/sda5", MountedOn: "/system/state", Size: 10, Available: 9},
			{Filesystem: "/dev/sda6", MountedOn: "/var", Size: 100, Available: 40},
		},
	}

	if !equalJSON(t, r, want) {
		t.Fail()
	}
}

func TestBuildNodeResourcesToleratesMissingParts(t *testing.T) {
	r := buildNodeResources(nil, nil, nil, nil, nil)

	if r.CPUCount != 0 || r.Mounts == nil {
		t.Errorf("got %+v", r)
	}
}
