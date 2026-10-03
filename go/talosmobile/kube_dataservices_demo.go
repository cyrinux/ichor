package talosmobile

import "time"

// demoDataServices are the storage and database operators of the built-in demo cluster, in
// every state the app shows: a degraded and a faulted volume, a Garage cluster with a node
// down next to a healthy single-node one, Postgres clusters with failed and stale backups.
func demoDataServices(now time.Time) dataServices {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

	const gib = int64(1) << 30

	longhorn := &longhornStatus{
		Version: "v1beta2",
		Volumes: []longhornVolume{
			{Name: "pvc-5d1e", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "search-data", State: "detached", Robustness: "faulted", Health: healthCritical,
				ReplicasDesired: 1, ReplicaNodes: []string{"demo-worker-3"}, Size: 30 * gib, ActualSize: 12 * gib, LastBackupAt: ms(9 * 24 * time.Hour)},
			{Name: "pvc-8a42", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "uploads", State: "attached", Robustness: "degraded", Health: healthWarning,
				ReplicasDesired: 3, ReplicasHealthy: 2, Rebuilding: 1, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2", "demo-worker-3"}, Node: "demo-worker-1",
				Size: 20 * gib, ActualSize: 7 * gib, LastBackupAt: ms(20 * time.Hour)},
			{Name: "pvc-1c07", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "postgres-data", State: "attached", Robustness: "healthy", Health: healthOK,
				ReplicasDesired: 3, ReplicasHealthy: 3, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2", "demo-worker-3"}, Node: "demo-worker-2",
				Size: 10 * gib, ActualSize: 3 * gib, LastBackupAt: ms(20 * time.Hour)},
			{Name: "pvc-9f33", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "scratch", State: "detached", Robustness: "unknown", Health: healthIdle,
				ReplicasDesired: 2, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2"}, Size: 5 * gib},
		},
		Nodes: []longhornNode{
			{Name: "demo-worker-1", Ready: true, Schedulable: true, Disks: []longhornDisk{{Path: "/var/lib/longhorn", Schedulable: true, Available: 180 * gib, Maximum: 250 * gib, Scheduled: 60 * gib}}},
			{Name: "demo-worker-2", Ready: true, Schedulable: true, Disks: []longhornDisk{{Path: "/var/lib/longhorn", Schedulable: true, Available: 120 * gib, Maximum: 250 * gib, Scheduled: 110 * gib}}},
			{Name: "demo-worker-3", Disks: []longhornDisk{{Path: "/var/lib/longhorn", Maximum: 250 * gib}}},
		},
		BackupTargets: []longhornBackupTarget{{Name: "default", URL: "s3://longhorn-backups@garage/", Available: true}},
	}

	garage := &garageStatus{Instances: []garageInstance{
		{
			Namespace: "garage", Name: "garage", Pod: "garage-4kq7z", Pods: 3, PodsReady: 2, Version: "v2.3.0",
			Status: garageDegraded, Message: "1 node down (zone zone-3, on demo-worker-3, last seen 2d ago); 85/256 partitions not fully replicated; 1294 blocks failing to resync",
			ConnectedNodes: 2, KnownNodes: 3, StorageNodes: 3, StorageNodesUp: 2, Partitions: 256, PartitionsQuorum: 256, PartitionsAllOk: 171,
			ResyncQueue: 1310, ResyncErrors: 1294, TableSyncQueue: 4200, LayoutVersion: 12, Source: garageSourceCLI,
			Nodes: []garageNode{
				{ID: "9c2f61a0d4e8b7c3", Hostname: "garage-2xv9p", Storage: true, Zone: "zone-3", Tags: []string{"demo-worker-3"}, KubeNode: "demo-worker-3", LastSeenSecs: 172800,
					DataTotal: 500 * gib, ResyncQueue: -1, ResyncErrors: -1, TableSyncQueue: -1, StatsError: "Network error: Not connected: 9c2f61a0d4e8b7c3"},
				{ID: "1b7e44c9a2f03d58", Hostname: "garage-4kq7z", Storage: true, Zone: "zone-1", Tags: []string{"demo-worker-1"}, KubeNode: "demo-worker-1", Up: true, LastSeenSecs: -1,
					DataAvail: 310 * gib, DataTotal: 500 * gib, ResyncQueue: 655, ResyncErrors: 648, TableSyncQueue: 2100},
				{ID: "6d03b8f15e9a2c47", Hostname: "garage-q8m2d", Storage: true, Zone: "zone-2", Tags: []string{"demo-worker-2"}, KubeNode: "demo-worker-2", Up: true, LastSeenSecs: -1,
					DataAvail: 295 * gib, DataTotal: 500 * gib, ResyncQueue: 655, ResyncErrors: 646, TableSyncQueue: 2100},
			},
		},
		{
			Namespace: "garage-archive", Name: "garage-archive", Pod: "garage-archive-0", Pods: 1, PodsReady: 1, Version: "v2.3.0", Status: garageHealthy,
			ConnectedNodes: 1, KnownNodes: 1, StorageNodes: 1, StorageNodesUp: 1, Partitions: 256, PartitionsQuorum: 256, PartitionsAllOk: 256,
			TableSyncQueue: 12, LayoutVersion: 1, Source: garageSourceCLI,
			Nodes: []garageNode{{ID: "e41a7f0c93b2d865", Hostname: "garage-archive-0", Storage: true, Zone: "nas", Tags: []string{"nas"}, KubeNode: "demo-worker-2", Up: true, LastSeenSecs: -1,
				DataAvail: 1400 * gib, DataTotal: 2000 * gib, TableSyncQueue: 12}},
		},
	}}

	pods := func(name string, ready ...bool) []cnpgPod {
		out := []cnpgPod{}
		for i, r := range ready {
			// An instance that is not ready waits unscheduled, as when its node is gone.
			pod := cnpgPod{Name: name + "-" + string(rune('1'+i)), Phase: "Pending"}
			if r {
				pod.Node, pod.Phase, pod.Ready = "demo-worker-"+string(rune('1'+i)), "Running", true
			}

			out = append(out, pod)
		}

		return out
	}

	cnpg := &cnpgStatus{Version: "v1", Clusters: []cnpgCluster{
		{Namespace: "demo", Name: "orders-db", Phase: "Waiting for the instances to become active", Health: healthCritical, Reasons: []string{cnpgReasonNoInstance},
			Instances: 2, CurrentPrimary: "orders-db-1", TargetPrimary: "orders-db-1", InstancePods: pods("orders-db", false, false),
			Archiving: "ok", LastBackup: "ok", BackupMethod: "plugin", ObjectStore: "garage-store", Scheduled: true,
			LastSuccessAt: ms(2 * 24 * time.Hour), RecoverableAt: ms(14 * 24 * time.Hour)},
		{Namespace: "demo", Name: "app-db", Phase: "Switchover in progress", Health: healthWarning, Reasons: []string{cnpgReasonInstances, cnpgReasonSwitchover},
			Instances: 3, ReadyInstances: 2, CurrentPrimary: "app-db-3", TargetPrimary: "app-db-1", InstancePods: pods("app-db", true, true, false),
			Archiving: "ok", LastBackup: "ok", BackupMethod: "plugin", ObjectStore: "garage-store", Scheduled: true,
			LastSuccessAt: ms(26 * time.Hour), RecoverableAt: ms(13 * 24 * time.Hour)},
		{Namespace: "demo", Name: "wiki-db", Phase: "Cluster in healthy state", Health: healthWarning, Reasons: []string{cnpgReasonBackupFailed},
			Instances: 2, ReadyInstances: 2, CurrentPrimary: "wiki-db-1", TargetPrimary: "wiki-db-1", InstancePods: pods("wiki-db", true, true),
			Archiving: "ok", LastBackup: "failed", BackupMethod: "plugin", ObjectStore: "garage-store", Scheduled: true,
			LastSuccessAt: ms(9 * 24 * time.Hour), LastFailureAt: ms(2 * 24 * time.Hour), RecoverableAt: ms(23 * 24 * time.Hour)},
		{Namespace: "demo", Name: "dns-db", Phase: "Cluster in healthy state", Health: healthWarning, Reasons: []string{cnpgReasonBackupStale},
			Instances: 3, ReadyInstances: 3, CurrentPrimary: "dns-db-2", TargetPrimary: "dns-db-2", InstancePods: pods("dns-db", true, true, true),
			Archiving: "ok", LastBackup: "stale", BackupMethod: "plugin", ObjectStore: "garage-store", Scheduled: true,
			LastSuccessAt: ms(45 * 24 * time.Hour), RecoverableAt: ms(59 * 24 * time.Hour)},
		{Namespace: "demo", Name: "auth-db", Phase: "Cluster in healthy state", Health: healthOK, Reasons: []string{},
			Instances: 3, ReadyInstances: 3, CurrentPrimary: "auth-db-1", TargetPrimary: "auth-db-1", InstancePods: pods("auth-db", true, true, true),
			Archiving: "off", LastBackup: "ok", BackupMethod: "plugin", ObjectStore: "garage-store", Scheduled: true,
			LastSuccessAt: ms(3 * 24 * time.Hour), RecoverableAt: ms(17 * 24 * time.Hour)},
	}}

	dragonfly := &dragonflyStatus{Version: "v1alpha1", Instances: []dragonflyInstance{
		{Namespace: "demo", Name: "session-cache", Phase: "Ready", Health: healthWarning, Reasons: []string{dragonflyReasonPods},
			Replicas: 2, ReadyPods: 1, Master: "session-cache-0", Pods: []dragonflyPod{
				{Name: "session-cache-0", Node: "demo-worker-1", Phase: "Running", Role: "master", Ready: true},
				{Name: "session-cache-1", Phase: "Pending", Role: "replica"},
			}},
		{Namespace: "demo", Name: "queue-cache", Phase: "Ready", Health: healthOK, Reasons: []string{},
			Replicas: 2, ReadyPods: 2, Master: "queue-cache-1", Pods: []dragonflyPod{
				{Name: "queue-cache-1", Node: "demo-worker-2", Phase: "Running", Role: "master", Ready: true},
				{Name: "queue-cache-0", Node: "demo-worker-1", Phase: "Running", Role: "replica", Ready: true},
			}},
	}}

	return dataServices{Longhorn: longhorn, Garage: garage, CNPG: cnpg, Dragonfly: dragonfly}
}
