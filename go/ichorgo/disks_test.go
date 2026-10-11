package ichorgo

import (
	"slices"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

func testDisk(name string, size uint64, transport string) *block.Disk {
	d := block.NewDisk(block.NamespaceName, name)
	d.TypedSpec().DevPath, d.TypedSpec().Size, d.TypedSpec().Transport = "/dev/"+name, size, transport

	return d
}

type partSpec struct {
	parent, label, fs string
	index             uint
	offset, size      uint64
}

func testPartition(p partSpec) *block.DiscoveredVolume {
	id := p.parent + string(rune('0'+p.index))
	dv := block.NewDiscoveredVolume(block.NamespaceName, id)
	spec := dv.TypedSpec()
	spec.Type, spec.DevPath, spec.Parent, spec.ParentDevPath = "partition", "/dev/"+id, p.parent, "/dev/"+p.parent
	spec.PartitionIndex, spec.PartitionLabel, spec.Name = p.index, p.label, p.fs
	spec.Offset, spec.Size = p.offset, p.size
	spec.UUID, spec.PartitionUUID = "uuid-"+id, "part-"+id

	return dv
}

func testVolume(id, location string, enc block.EncryptionProviderType) *block.VolumeStatus {
	v := block.NewVolumeStatus(block.NamespaceName, id)
	v.TypedSpec().Phase, v.TypedSpec().Type, v.TypedSpec().Location = block.VolumePhaseReady, block.VolumeTypePartition, location
	v.TypedSpec().Filesystem, v.TypedSpec().EncryptionProvider = block.FilesystemTypeXFS, enc

	return v
}

func testMount(volumeID, target string) *block.MountStatus {
	m := block.NewMountStatus(block.NamespaceName, volumeID+"-mount")
	m.TypedSpec().Spec.VolumeID, m.TypedSpec().Target = volumeID, target

	return m
}

const mib = 1 << 20

func TestBuildDisks(t *testing.T) {
	disks := []*block.Disk{
		testDisk("sdb", 100*mib, "sata"),
		testDisk("sda", 1000*mib, "virtio"),
		testDisk("loop0", 10*mib, ""),
	}
	discovered := []*block.DiscoveredVolume{
		// sda: EFI, META, an encrypted STATE (probed as luks), a 2 MiB gap, EPHEMERAL, a
		// foreign luks partition, a 4 MiB tail.
		testPartition(partSpec{parent: "sda", index: 4, label: "EPHEMERAL", fs: "xfs", offset: 200 * mib, size: 700 * mib}),
		testPartition(partSpec{parent: "sda", index: 1, label: "EFI", fs: "vfat", offset: mib, size: 100 * mib}),
		testPartition(partSpec{parent: "sda", index: 2, label: "META", fs: "talosmeta", offset: 101 * mib, size: mib}),
		testPartition(partSpec{parent: "sda", index: 3, label: "STATE", fs: "luks", offset: 102 * mib, size: 96 * mib}),
		testPartition(partSpec{parent: "sda", index: 5, fs: "luks", offset: 900 * mib, size: 96 * mib}),
		// The disk itself is a discovered volume too: not a partition.
		func() *block.DiscoveredVolume {
			dv := block.NewDiscoveredVolume(block.NamespaceName, "sda")
			dv.TypedSpec().Type = "disk"

			return dv
		}(),
	}
	volumes := []*block.VolumeStatus{
		testVolume("STATE", "/dev/sda3", block.EncryptionProviderLUKS2),
		testVolume("EPHEMERAL", "/dev/sda4", block.EncryptionProviderNone),
		testVolume("META", "", block.EncryptionProviderNone),
	}
	// META has no location: found by parent and index.
	volumes[2].TypedSpec().ParentLocation, volumes[2].TypedSpec().PartitionIndex = "/dev/sda", 2
	volumes[2].TypedSpec().Filesystem = block.FilesystemTypeNone

	got := buildDisks(disks, discovered, volumes, []*block.MountStatus{testMount("EPHEMERAL", "/var/lib"), testMount("EPHEMERAL", "/var")}, "sda")

	if !got.Supported || len(got.Disks) != 2 || got.Disks[0].Name != "sda" || !got.Disks[0].SystemDisk || got.Disks[1].Name != "sdb" || got.Disks[1].SystemDisk {
		t.Fatalf("disks = %+v", got.Disks)
	}

	sda := got.Disks[0]
	if sda.Transport != "virtio" || sda.Type != "ssd" || sda.Unallocated != 6*mib {
		t.Errorf("sda = %+v", sda)
	}

	type row struct {
		index                 uint
		label, volume, fs     string
		encryption, provider  string
		mountedOn, phase, dev string
	}

	var rows []row
	for _, p := range sda.Partitions {
		rows = append(rows, row{p.Index, p.Label, p.VolumeID, p.Filesystem, p.Encryption, p.EncryptionProvider, p.MountedOn, p.Phase, p.DevPath})
	}

	wantRows := []row{
		{1, "EFI", "", "vfat", "unknown", "", "", "", "/dev/sda1"},
		{2, "META", "META", "talosmeta", "disabled", "", "", "ready", "/dev/sda2"},
		{3, "STATE", "STATE", "xfs", "enabled", "luks2", "", "ready", "/dev/sda3"},
		{4, "EPHEMERAL", "EPHEMERAL", "xfs", "disabled", "", "/var", "ready", "/dev/sda4"},
		{5, "", "", "luks", "enabled", "", "", "", "/dev/sda5"},
	}
	if !slices.Equal(rows, wantRows) {
		t.Errorf("partitions =\n%+v\nwant\n%+v", rows, wantRows)
	}

	if p := sda.Partitions[0]; p.UUID != "uuid-sda1" || p.PartitionUUID != "part-sda1" || p.Offset != mib || p.Size != 100*mib {
		t.Errorf("EFI = %+v", p)
	}

	wantSegments := []diskSegment{
		{Kind: "partition", Index: 1, Label: "EFI", Offset: mib, Size: 100 * mib},
		{Kind: "partition", Index: 2, Label: "META", Offset: 101 * mib, Size: mib},
		{Kind: "partition", Index: 3, Label: "STATE", Offset: 102 * mib, Size: 96 * mib},
		{Kind: "free", Offset: 198 * mib, Size: 2 * mib},
		{Kind: "partition", Index: 4, Label: "EPHEMERAL", Offset: 200 * mib, Size: 700 * mib},
		{Kind: "partition", Index: 5, Offset: 900 * mib, Size: 96 * mib},
		{Kind: "free", Offset: 996 * mib, Size: 4 * mib},
	}
	if !slices.Equal(sda.Segments, wantSegments) {
		t.Errorf("segments =\n%+v\nwant\n%+v", sda.Segments, wantSegments)
	}

	// No partition: one free segment from the 1 MiB alignment to the end.
	sdb := got.Disks[1]
	if len(sdb.Partitions) != 0 || !slices.Equal(sdb.Segments, []diskSegment{{Kind: "free", Offset: mib, Size: 99 * mib}}) || sdb.Unallocated != 99*mib {
		t.Errorf("sdb = %+v", sdb)
	}
}

func TestDiskSegments(t *testing.T) {
	tests := []struct {
		name            string
		size            uint64
		parts           []diskPartition
		wantSegments    []diskSegment
		wantUnallocated uint64
	}{
		{
			name:  "small gaps counted, not drawn",
			size:  100 * mib,
			parts: []diskPartition{{Index: 1, Offset: mib, Size: 49 * mib}, {Index: 2, Offset: 50*mib + 4096, Size: 50*mib - 4096 - 512}},
			wantSegments: []diskSegment{
				{Kind: "partition", Index: 1, Offset: mib, Size: 49 * mib},
				{Kind: "partition", Index: 2, Offset: 50*mib + 4096, Size: 50*mib - 4096 - 512},
			},
			wantUnallocated: 4096 + 512,
		},
		{
			name:  "overlap and overflow clamped",
			size:  100 * mib,
			parts: []diskPartition{{Index: 1, Offset: mib, Size: 60 * mib}, {Index: 2, Offset: 50 * mib, Size: 80 * mib}},
			wantSegments: []diskSegment{
				{Kind: "partition", Index: 1, Offset: mib, Size: 60 * mib},
				{Kind: "partition", Index: 2, Offset: 61 * mib, Size: 39 * mib},
			},
		},
		{
			name:         "empty and out of range partitions dropped",
			size:         10 * mib,
			parts:        []diskPartition{{Index: 1, Offset: mib, Size: 0}, {Index: 2, Offset: 20 * mib, Size: mib}},
			wantSegments: []diskSegment{{Kind: "free", Offset: mib, Size: 9 * mib}},

			wantUnallocated: 9 * mib,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments, unallocated := diskSegments(tt.size, tt.parts)
			if !slices.Equal(segments, tt.wantSegments) || unallocated != tt.wantUnallocated {
				t.Errorf("got %+v, %d", segments, unallocated)
			}
		})
	}
}

func TestNodeDisksFake(t *testing.T) {
	const node, old = "192.0.2.91", "192.0.2.92"

	f := newFakeTalos()
	f.addNode(t, node, "v1.11.0", machine.TypeWorker)
	f.addNode(t, old, "v1.7.0", machine.TypeWorker)

	sd := block.NewSystemDisk(block.NamespaceName, block.SystemDiskID)
	sd.TypedSpec().DiskID = "sda"

	f.put(node,
		resourceDefinition(t, block.DiscoveredVolumeExtension{}.ResourceDefinition()),
		testDisk("sda", 1000*mib, "virtio"), sd,
		testPartition(partSpec{parent: "sda", index: 1, label: "EFI", fs: "vfat", offset: mib, size: 100 * mib}),
		testPartition(partSpec{parent: "sda", index: 2, label: "EPHEMERAL", fs: "xfs", offset: 101 * mib, size: 899 * mib}),
		testVolume("EPHEMERAL", "/dev/sda2", block.EncryptionProviderNone),
		testMount("EPHEMERAL", "/var"),
	)

	cfg := f.start(t, node, old)

	out, err := NodeDisks(cfg, "fake", node)
	got := decodeJSON[diskLayoutList](t, out, err)

	if !got.Supported || len(got.Disks) != 1 || !got.Disks[0].SystemDisk || len(got.Disks[0].Partitions) != 2 {
		t.Fatalf("disks = %s", out)
	}

	if p := got.Disks[0].Partitions[1]; p.VolumeID != "EPHEMERAL" || p.MountedOn != "/var" || p.Encryption != encryptionDisabled {
		t.Errorf("EPHEMERAL = %+v", p)
	}

	if got.Disks[0].Unallocated != 0 || len(got.Disks[0].Segments) != 2 {
		t.Errorf("segments = %+v", got.Disks[0].Segments)
	}

	// Talos 1.7: no discovered volumes, a reason and no error.
	out, err = NodeDisks(cfg, "fake", old)
	got = decodeJSON[diskLayoutList](t, out, err)

	if got.Supported || !strings.Contains(got.Reason, "v1.8") || !strings.Contains(out, `"disks":[]`) {
		t.Errorf("old node = %s", out)
	}
}

func TestMarkSystemDisk(t *testing.T) {
	in := []diskInfo{{Name: "sda"}, {Name: "sdb"}}
	got := markSystemDisk(in, "sdb")

	if got[0].SystemDisk || !got[1].SystemDisk || in[1].SystemDisk {
		t.Errorf("got %+v, in %+v", got, in)
	}

	if got := markSystemDisk(in, ""); got[0].SystemDisk || got[1].SystemDisk {
		t.Errorf("empty id = %+v", got)
	}
}

func TestDemoDisksConsistent(t *testing.T) {
	d := demoDisks().Disks[0]

	var total uint64
	for _, s := range d.Segments {
		total += s.Size
	}

	if first := d.Segments[0].Offset; total+first != d.Size || d.Unallocated != 2*mib || len(d.Partitions) != 4 {
		t.Errorf("demo disk = %+v", d)
	}
}
