package ichorgo

import (
	"slices"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

func TestVolumeFillLevel(t *testing.T) {
	for _, tc := range []struct {
		used, warn, crit float64
		want             string
	}{
		{50, 0, 0, storageOK},
		{84.9, 0, 0, storageOK},
		{85, 0, 0, storageWarning},
		{94.9, 0, 0, storageWarning},
		{95, 0, 0, storageCritical},
		{100, 0, 0, storageCritical},
		{70, 70, 90, storageWarning},
		{90, 70, 90, storageCritical},
		{80, -1, 90, storageOK},
		// warn above crit is capped at crit.
		{92, 97, 90, storageCritical},
	} {
		if got := volumeFillLevel(tc.used, tc.warn, tc.crit); got != tc.want {
			t.Errorf("volumeFillLevel(%v, %v, %v) = %s, want %s", tc.used, tc.warn, tc.crit, got, tc.want)
		}
	}
}

func TestStorageKeys(t *testing.T) {
	if k := storageVolumeKey("192.0.2.20", "EPHEMERAL"); k != "192.0.2.20|EPHEMERAL" {
		t.Errorf("volume key = %s", k)
	}

	if k := storageDiskKey("192.0.2.21", "sda"); k != "192.0.2.21|smart|sda" {
		t.Errorf("disk key = %s", k)
	}
}

func TestBuildVolumeFill(t *testing.T) {
	mounts := []mountUsage{
		{Filesystem: "/dev/loop0", MountedOn: "/", Size: 80 << 20, Available: 0},
		{Filesystem: "/dev/sda5", MountedOn: "/system/state", Size: 100 << 20, Available: 90 << 20},
		{Filesystem: "/dev/sda6", MountedOn: "/var", Size: 100 << 30, Available: 9 << 30},
		{Filesystem: "/dev/sdb1", MountedOn: "/var/mnt/data", Size: 10 << 30, Available: 5 << 30},
		{Filesystem: "/dev/sdc1", MountedOn: "/mnt/other", Size: 10 << 30, Available: 10 << 30},
		{Filesystem: "/dev/sdd1", MountedOn: "/mnt/odd", Size: 1, Available: 2},
	}

	got := buildVolumeFill("192.0.2.20", mounts, map[string]string{"/var/mnt/data": "u-data"})

	want := []volumeFill{
		{Key: "192.0.2.20|STATE", Name: "STATE", Mount: "/system/state", UsedPercent: 10, FreeBytes: 90 << 20, SizeBytes: 100 << 20, Level: storageOK},
		{Key: "192.0.2.20|EPHEMERAL", Name: "EPHEMERAL", Mount: "/var", UsedPercent: 91, FreeBytes: 9 << 30, SizeBytes: 100 << 30, Level: storageWarning},
		{Key: "192.0.2.20|data", Name: "data", Mount: "/var/mnt/data", UsedPercent: 50, FreeBytes: 5 << 30, SizeBytes: 10 << 30, Level: storageOK},
		{Key: "192.0.2.20|/mnt/other", Name: "/mnt/other", Mount: "/mnt/other", UsedPercent: 0, FreeBytes: 10 << 30, SizeBytes: 10 << 30, Level: storageOK},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestBuildDiskConditions(t *testing.T) {
	disks := []diskSMART{
		{Device: "nvme0n1", Model: "M1", Healthy: new(true)},
		{Device: "nvme1n1", Healthy: new(true), CriticalWarnings: []string{"2 media errors"}},
		{Device: "sda", Healthy: new(false), Message: "attribute 5 failing"},
		{Device: "sdb", Healthy: new(false)},
		{Device: "vda"},
	}

	got := buildDiskConditions("192.0.2.21", disks)

	want := []diskCondition{
		{Key: "192.0.2.21|smart|nvme0n1", Device: "nvme0n1", Model: "M1", Health: diskHealthOK},
		{Key: "192.0.2.21|smart|nvme1n1", Device: "nvme1n1", Health: diskHealthFailing, Reason: "2 media errors"},
		{Key: "192.0.2.21|smart|sda", Device: "sda", Health: diskHealthFailing, Reason: "attribute 5 failing"},
		{Key: "192.0.2.21|smart|sdb", Device: "sdb", Health: diskHealthFailing, Reason: "SMART reports the disk unhealthy"},
		{Key: "192.0.2.21|smart|vda", Device: "vda", Health: diskHealthUnknown},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// storageNode is a Talos 1.15 node: volume and SMART resources, one failing disk.
func storageNode(t *testing.T, f *fakeTalos, node string) {
	t.Helper()

	f.addNode(t, node, "v1.15.0", machine.TypeWorker)

	f.mu.Lock()
	if f.mounts == nil {
		f.mounts = map[string][]*machineapi.MountStat{}
	}
	f.mounts[node] = []*machineapi.MountStat{
		{Filesystem: "overlay", MountedOn: "/etc/cni", Size: 1 << 30, Available: 0},
		{Filesystem: "/dev/loop0", MountedOn: "/", Size: 80 << 20, Available: 0},
		{Filesystem: "/dev/sda5", MountedOn: "/system/state", Size: 100 << 20, Available: 95 << 20},
		{Filesystem: "/dev/sda6", MountedOn: "/var", Size: 100 << 30, Available: 4 << 30},
		{Filesystem: "/dev/sda6", MountedOn: "/var/lib", Size: 100 << 30, Available: 4 << 30},
		{Filesystem: "/dev/sdb1", MountedOn: "/var/lib/kubelet/pods/x/volumes/y", Size: 1 << 30, Available: 0},
	}
	f.mu.Unlock()

	ephemeral := block.NewVolumeStatus(block.NamespaceName, "EPHEMERAL")
	ephemeral.TypedSpec().Phase = block.VolumePhaseReady
	ephemeralMount := block.NewMountStatus(block.NamespaceName, "EPHEMERAL-mount")
	ephemeralMount.TypedSpec().Target = "/var"
	ephemeralMount.TypedSpec().Spec.VolumeID = "EPHEMERAL"

	disk := block.NewDisk(block.NamespaceName, "sda")
	disk.TypedSpec().Size, disk.TypedSpec().Model = 120<<30, "Fake SSD"

	f.put(node,
		resourceDefinition(t, block.VolumeStatusExtension{}.ResourceDefinition()),
		resourceDefinition(t, meta.ResourceDefinitionSpec{Type: smartStatusType, DefaultNamespace: block.NamespaceName}),
		ephemeral, ephemeralMount, disk,
		untypedResource(t, smartStatusType, "sda", "dev_type: sata\nhealthy: false\nmessage: reallocated sectors\n"),
	)
}

func TestClusterStorageHealthFake(t *testing.T) {
	const full, old, down = "192.0.2.71", "192.0.2.72", "192.0.2.73"

	f := newFakeTalos()
	storageNode(t, f, full)
	// Talos 1.7: no volume or SMART resources, the default mount (/dev/sda6 on /var, 40 %).
	f.addNode(t, old, "v1.7.0", machine.TypeWorker)

	cfg := f.start(t, full, old, down)

	out, err := ClusterStorageHealth(cfg, "fake")
	got := decodeJSON[clusterStorage](t, out, err)

	if got.Context != "fake" || len(got.Nodes) != 3 {
		t.Fatalf("storage = %s", out)
	}

	n := got.Nodes[0]
	wantVolumes := []volumeFill{
		{Key: full + "|STATE", Name: "STATE", Mount: "/system/state", UsedPercent: 5, FreeBytes: 95 << 20, SizeBytes: 100 << 20, Level: storageOK},
		{Key: full + "|EPHEMERAL", Name: "EPHEMERAL", Mount: "/var", UsedPercent: 96, FreeBytes: 4 << 30, SizeBytes: 100 << 30, Level: storageCritical},
	}
	wantDisks := []diskCondition{{Key: full + "|smart|sda", Device: "sda", Model: "Fake SSD", Health: diskHealthFailing, Reason: "reallocated sectors"}}

	if n.Node != full || n.Hostname != "host-192-0-2-71" || n.Error != "" || !slices.Equal(n.Volumes, wantVolumes) || !slices.Equal(n.Disks, wantDisks) {
		t.Errorf("full node = %+v", n)
	}

	// Older Talos: volumes named from their well-known mount point, no disks, no error.
	n = got.Nodes[1]
	if n.Error != "" || len(n.Disks) != 0 || len(n.Volumes) != 1 || n.Volumes[0].Name != "EPHEMERAL" || n.Volumes[0].UsedPercent != 40 || n.Volumes[0].Level != storageOK {
		t.Errorf("old node = %+v", n)
	}

	// A node that does not answer is reported, not fatal.
	n = got.Nodes[2]
	if n.Node != down || n.Error == "" || n.Hostname != down || len(n.Volumes) != 0 || len(n.Disks) != 0 {
		t.Errorf("down node = %+v", n)
	}

	if !strings.Contains(out, `"volumes":[]`) || !strings.Contains(out, `"disks":[]`) {
		t.Errorf("empty lists must encode as []: %s", out)
	}
}

func TestClusterStorageHealthDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	out, err := ClusterStorageHealth(yaml, "")
	got := decodeJSON[clusterStorage](t, out, err)

	if len(got.Nodes) != 5 {
		t.Fatalf("demo = %s", out)
	}

	var warnings, failing []string

	for _, n := range got.Nodes {
		if n.Error != "" || len(n.Volumes) != 2 || len(n.Disks) != 1 {
			t.Errorf("demo node = %+v", n)
		}

		for _, v := range n.Volumes {
			if v.Level != storageOK {
				warnings = append(warnings, n.Hostname+" "+v.Name+" "+v.Level)
			}
		}

		for _, d := range n.Disks {
			if d.Health != diskHealthOK {
				failing = append(failing, n.Hostname+" "+d.Device+" "+d.Health)
			}
		}
	}

	if !slices.Equal(warnings, []string{"demo-worker-1 EPHEMERAL warning"}) || !slices.Equal(failing, []string{"demo-worker-2 nvme0n1 failing"}) {
		t.Errorf("warnings = %v, failing = %v", warnings, failing)
	}
}
