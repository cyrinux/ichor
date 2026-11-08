package ichorgo

// demoGarageLayout is the demo cluster's layout: three storage nodes in three zones, one
// down, a fourth node connected and staged to join (garage namespace); the archive instance
// is a single node with nothing staged. Node IDs match the demo instances' nodes.
func demoGarageLayout(namespace string) garageLayoutReport {
	const gib = int64(1) << 30

	if namespace == "garage-archive" {
		return garageLayoutReport{
			Version: 1, PartitionSize: 8 * gib, ZoneRedundancy: "maximum",
			Nodes: []garageLayoutNode{
				{ID: "e41a7f0c93b2d865", Hostname: "garage-archive-0", KubeNode: "demo-worker-2", Up: true, Known: true, DataTotal: 2000 * gib,
					Role: &garageLayoutRole{Zone: "nas", Capacity: 1800 * gib, Tags: []string{"nas"}, StoredPartitions: 256, UsableCapacity: 1800 * gib}, Staged: garageStagedNone},
			},
			Versions: []garageLayoutVersion{{Version: 1, Status: garageLayoutCurrent, StorageNodes: 1}},
			MinAck:   1, Trackers: []garageLayoutTracker{}, Messages: []string{},
		}
	}

	role := func(zone, tag string) *garageLayoutRole {
		return &garageLayoutRole{Zone: zone, Capacity: 450 * gib, Tags: []string{tag}, StoredPartitions: 256, UsableCapacity: 448 * gib}
	}

	return garageLayoutReport{
		Version: 12, NextVersion: 13, PartitionSize: 1750 << 20, ZoneRedundancy: "maximum",
		Preview: &garageLayoutPreview{
			OK: true, MovedPartitions: 192, UsableCapacity: 1792 * gib, EffectiveCapacity: 597 * gib, TotalCapacity: 1800 * gib,
			Messages: []string{
				"Partitions are replicated 3 times on at least 3 distinct zones.",
				"Optimal partition size: 1.75 GB (1.75 GB in previous layout)",
				"Usable capacity: 1.79 TB / 1.80 TB (99.6%)",
				"zone-4: 192 partitions moved in, 0 moved out",
			},
		},
		Nodes: []garageLayoutNode{
			{ID: "9c2f61a0d4e8b7c3", Hostname: "garage-2xv9p", KubeNode: "demo-worker-3", Known: true, DataTotal: 500 * gib, Role: role("zone-3", "demo-worker-3"), Staged: garageStagedNone},
			{ID: "1b7e44c9a2f03d58", Hostname: "garage-4kq7z", KubeNode: "demo-worker-1", Up: true, Known: true, DataTotal: 500 * gib, Role: role("zone-1", "demo-worker-1"), Staged: garageStagedNone},
			{ID: "6d03b8f15e9a2c47", Hostname: "garage-q8m2d", KubeNode: "demo-worker-2", Up: true, Known: true, DataTotal: 500 * gib, Role: role("zone-2", "demo-worker-2"), Staged: garageStagedNone},
			{ID: "f7a2c4e81b9d3056", Hostname: "garage-n7t4w", KubeNode: "demo-worker-4", Up: true, Known: true, DataTotal: 500 * gib,
				Staged: garageStagedAdd, StagedRole: &garageLayoutRole{Zone: "zone-4", Capacity: 450 * gib, Tags: []string{"demo-worker-4"}}},
		},
		Versions: []garageLayoutVersion{
			{Version: 12, Status: garageLayoutCurrent, StorageNodes: 3},
			{Version: 11, Status: garageLayoutDraining, StorageNodes: 3},
			{Version: 10, Status: garageLayoutHistorical, StorageNodes: 2},
		},
		MinAck: 11,
		Trackers: []garageLayoutTracker{
			{ID: "9c2f61a0d4e8b7c3", Hostname: "garage-2xv9p", Ack: 11, Sync: 11, SyncAck: 11},
			{ID: "1b7e44c9a2f03d58", Hostname: "garage-4kq7z", Ack: 12, Sync: 12, SyncAck: 11},
			{ID: "6d03b8f15e9a2c47", Hostname: "garage-q8m2d", Ack: 12, Sync: 12, SyncAck: 11},
		},
		Stuck:    true,
		Messages: []string{},
	}
}
