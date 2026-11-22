package ichorgo

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	wipeNode = "192.0.2.95"
	wipeOld  = "192.0.2.96"
)

// wipeCluster is a 1.11 node with a system disk (sda), a data disk with a stray partition
// (sdb), a disk holding a user volume (sdc) and a read-only disk (sdd); and a 1.8 node with
// the same sdb.
func wipeCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeTalos()
	f.addNode(t, wipeNode, "v1.11.0", machine.TypeWorker)
	f.addNode(t, wipeOld, "v1.8.3", machine.TypeWorker)

	sd := block.NewSystemDisk(block.NamespaceName, block.SystemDiskID)
	sd.TypedSpec().DiskID = "sda"

	readonly := testDisk("sdd", 10*mib, "usb")
	readonly.TypedSpec().Readonly = true

	disks := func() []resource.Resource {
		return []resource.Resource{
			resourceDefinition(t, block.DiscoveredVolumeExtension{}.ResourceDefinition()),
			testDisk("sda", 1000*mib, "virtio"), sd,
			testPartition(partSpec{parent: "sda", index: 1, label: "EPHEMERAL", fs: "xfs", offset: mib, size: 999 * mib}),
			testVolume("EPHEMERAL", "/dev/sda1", block.EncryptionProviderNone),
			testMount("EPHEMERAL", "/var"),
			testDisk("sdb", 500*mib, "sata"),
			testPartition(partSpec{parent: "sdb", index: 1, label: "old-data", fs: "ext4", offset: mib, size: 400 * mib}),
		}
	}

	f.put(wipeNode, append(disks(),
		testDisk("sdc", 500*mib, "sata"),
		testPartition(partSpec{parent: "sdc", index: 1, label: "u-data", fs: "xfs", offset: mib, size: 400 * mib}),
		testVolume("u-data", "/dev/sdc1", block.EncryptionProviderNone),
		readonly,
	)...)
	f.put(wipeOld, disks()...)

	return f, f.start(t, wipeNode, wipeOld)
}

func TestNodeDiskWipe(t *testing.T) {
	f, cfg := wipeCluster(t)

	if err := NodeDiskWipe(cfg, "fake", wipeNode, "/dev/sdb", ""); err != nil {
		t.Fatal(err)
	}

	if len(f.wipes) != 1 || len(f.wipes[0].Devices) != 1 {
		t.Fatalf("wipes %+v", f.wipes)
	}

	if d := f.wipes[0].Devices[0]; d.Device != "sdb" || d.Method != storageapi.BlockDeviceWipeDescriptor_FAST ||
		d.SkipVolumeCheck || d.SkipSecondaryCheck || d.DropPartition {
		t.Fatalf("descriptor %+v", d)
	}

	entries := readAudit(t, "", "disk-wipe")
	if len(entries) != 1 || entries[0].Node != wipeNode || entries[0].Params != "device=sdb,method=fast" || entries[0].Outcome != auditOK {
		t.Fatalf("audit %+v", entries)
	}

	if err := NodeDiskWipe(cfg, "fake", wipeNode, "sdb", "zeroes"); err != nil {
		t.Fatal(err)
	}

	if f.wipes[1].Devices[0].Method != storageapi.BlockDeviceWipeDescriptor_ZEROES {
		t.Fatalf("zeroes %+v", f.wipes[1].Devices[0])
	}
}

func TestNodeDiskWipeRefusals(t *testing.T) {
	f, cfg := wipeCluster(t)

	for _, c := range []struct{ node, device, method, want string }{
		{wipeNode, "sda", "", "sda is the system disk: only a node reset wipes it"},
		{wipeNode, "sdc", "", "this disk holds the volume u-data: remove the volume first"},
		{wipeNode, "sdd", "", "sdd is read-only"},
		{wipeNode, "sdz", "", "sdz is not one of this node's disks"},
		{wipeNode, "sdb1", "", "sdb1 is not one of this node's disks"},
		{wipeNode, " ", "", "no disk named"},
		{wipeNode, "sdb", "secure", `unknown wipe method "secure"`},
		{"192.0.2.250", "sdb", "", "not"},
	} {
		err := NodeDiskWipe(cfg, "fake", c.node, c.device, c.method)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}

	if len(f.wipes) != 0 {
		t.Fatalf("%d wipes sent for refused disks", len(f.wipes))
	}

	if n := len(readAudit(t, "", "disk-wipe")); n != 8 {
		t.Fatalf("%d audit entries, one per attempt", n)
	}
}

// A disk a volume uses still answers Talos' own refusal when the disk list missed it.
func TestNodeDiskWipeTalosRefusal(t *testing.T) {
	f, cfg := wipeCluster(t)
	f.wipeErr = status.Error(codes.FailedPrecondition, `blockdevice "sdb" is in use by volume "u-x"`)

	err := NodeDiskWipe(cfg, "fake", wipeNode, "sdb", "")
	if err == nil || !strings.Contains(err.Error(), "in use by volume") {
		t.Fatalf("got %v", err)
	}
}

func TestNodeDiskWipeOldTalos(t *testing.T) {
	_, cfg := wipeCluster(t)

	err := NodeDiskWipe(cfg, "fake", wipeOld, "sdb", "")
	if err == nil || err.Error() != "this node's Talos cannot wipe one disk (needs v1.9)" {
		t.Fatalf("got %v", err)
	}
}

func TestNodeDiskWipeDemo(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo := mustDemoConfig(t)

	err := NodeDiskWipe(demo, "", "10.5.0.2", "sdb", "")
	if err == nil || (!errors.Is(err, errDemoUnavailable) && err.Error() != errDemoUnavailable.Error()) {
		t.Fatalf("demo: %v", err)
	}
}

func TestWipeableDemoDisk(t *testing.T) {
	disks := demoDisks().Disks
	if len(disks) != 2 {
		t.Fatalf("%d demo disks", len(disks))
	}

	if err := wipeableDisk(disks, disks[1].Name); err != nil {
		t.Fatalf("the demo's data disk: %v", err)
	}

	if err := wipeableDisk(disks, disks[0].Name); err == nil {
		t.Fatal("the demo's system disk is wipeable")
	}
}
