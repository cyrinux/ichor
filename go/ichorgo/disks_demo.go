package ichorgo

// demoDisks is the demo node's system disk: the usual Talos layout (EFI, META, STATE,
// EPHEMERAL) with STATE encrypted and a little free space at the end, matching the
// NodeVolumes demo (/dev/nvme0n1p3 and p4).
func demoDisks() diskLayoutList {
	const (
		size     = 120 << 30
		efiStart = 1 << 20
		efiSize  = 1 << 30
		metaSize = 1 << 20
		stateEnd = efiStart + efiSize + metaSize + 100<<20
		freeTail = 2 << 20
	)

	partitions := []diskPartition{
		{
			Index: 1, DevPath: "/dev/nvme0n1p1", Label: "EFI", Offset: efiStart, Size: efiSize, Filesystem: "vfat",
			Encryption: encryptionUnknown, UUID: "1A2B-3C4D", PartitionUUID: "11111111-0000-4000-8000-000000000001",
			PartitionType: "c12a7328-f81f-11d2-ba4b-00a0c93ec93b",
		},
		{
			Index: 2, DevPath: "/dev/nvme0n1p2", Label: "META", VolumeID: "META", Offset: efiStart + efiSize, Size: metaSize,
			Filesystem: "talosmeta", Encryption: encryptionDisabled, PartitionUUID: "11111111-0000-4000-8000-000000000002",
			PartitionType: "0fc63daf-8483-4772-8e79-3d69d8477de4", Phase: "ready",
		},
		{
			Index: 3, DevPath: "/dev/nvme0n1p3", Label: "STATE", VolumeID: "STATE", Offset: efiStart + efiSize + metaSize, Size: 100 << 20,
			Filesystem: "xfs", Encryption: encryptionEnabled, EncryptionProvider: "luks2", UUID: "22222222-0000-4000-8000-000000000003",
			PartitionUUID: "11111111-0000-4000-8000-000000000003", PartitionType: "0fc63daf-8483-4772-8e79-3d69d8477de4",
			MountedOn: "/system/state", Phase: "ready",
		},
		{
			Index: 4, DevPath: "/dev/nvme0n1p4", Label: "EPHEMERAL", VolumeID: "EPHEMERAL", Offset: stateEnd, Size: size - stateEnd - freeTail,
			Filesystem: "xfs", Encryption: encryptionDisabled, UUID: "22222222-0000-4000-8000-000000000004",
			PartitionUUID: "11111111-0000-4000-8000-000000000004", PartitionType: "0fc63daf-8483-4772-8e79-3d69d8477de4",
			MountedOn: "/var", Phase: "ready",
		},
	}

	segments, unallocated := diskSegments(size, partitions)

	return diskLayoutList{Supported: true, Disks: []diskLayout{
		{
			Name: "nvme0n1", DevPath: "/dev/nvme0n1", Size: size, Type: "nvme", Transport: "nvme", Model: "Demo NVMe",
			SystemDisk: true, Unallocated: unallocated, Partitions: partitions, Segments: segments,
		},
		demoDataDisk(),
	}}
}

// demoDataDisk is a spare data disk with a partition left by an old storage system: no Talos
// volume and not mounted, so the demo offers "Wipe this disk…" on it.
func demoDataDisk() diskLayout {
	const size = 500 << 30

	partitions := []diskPartition{{
		Index: 1, DevPath: "/dev/sdb1", Label: "old-data", Offset: 1 << 20, Size: size - 2<<20, Filesystem: "ext4",
		Encryption: encryptionUnknown, UUID: "33333333-0000-4000-8000-000000000001",
		PartitionUUID: "44444444-0000-4000-8000-000000000001", PartitionType: "0fc63daf-8483-4772-8e79-3d69d8477de4",
	}}

	segments, unallocated := diskSegments(size, partitions)

	return diskLayout{
		Name: "sdb", DevPath: "/dev/sdb", Size: size, Type: "hdd", Transport: "sata", Model: "Demo HDD", Serial: "DEMO-HDD-0001",
		Unallocated: unallocated, Partitions: partitions, Segments: segments,
	}
}
