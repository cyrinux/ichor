package ichorgo

import (
	"slices"
	"strings"
	"time"
)

// demoDataServices are the storage and database operators of the built-in demo cluster, in
// every state the app shows: a degraded and a faulted volume, a Garage cluster with a node
// down next to a healthy single-node one, Postgres clusters with failed and stale backups,
// a MariaDB cluster whose last backup failed next to a healthy Galera one and a suspended one,
// an expired certificate and one failing to renew,
// Velero schedules with a failed and a partially failed backup and a storage location down, a
// nearly full Ceph cluster with an OSD down.
func demoDataServices(now time.Time) dataServices {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

	const gib = int64(1) << 30

	longhorn := &longhornStatus{
		Version: "v1beta2",
		Volumes: []longhornVolume{
			{Name: "pvc-5d1e", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "search-data", State: "detached", Robustness: "faulted", Health: healthCritical,
				ReplicasDesired: 1, ReplicaNodes: []string{"demo-worker-3"}, Size: 30 * gib, ActualSize: 12 * gib, LastBackupAt: ms(9 * 24 * time.Hour),
				ScheduleError: "replica scheduling failed: no node with enough free space", TooManySnapshots: true},
			{Name: "pvc-8a42", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "uploads", State: "attached", Robustness: "degraded", Health: healthWarning,
				ReplicasDesired: 3, ReplicasHealthy: 2, Rebuilding: 1, RebuildProgress: 42, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2", "demo-worker-3"}, Node: "demo-worker-1",
				Size: 20 * gib, ActualSize: 7 * gib, LastBackupAt: ms(20 * time.Hour)},
			{Name: "pvc-1c07", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "postgres-data", State: "attached", Robustness: "healthy", Health: healthOK,
				ReplicasDesired: 3, ReplicasHealthy: 3, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2", "demo-worker-3"}, Node: "demo-worker-2",
				Size: 10 * gib, ActualSize: 3 * gib, LastBackupAt: ms(20 * time.Hour), BackingUp: true, BackupProgress: 63},
			{Name: "pvc-9f33", Namespace: "longhorn-system", PVCNamespace: "demo", PVCName: "scratch", State: "detached", Robustness: "unknown", Health: healthIdle,
				ReplicasDesired: 2, ReplicaNodes: []string{"demo-worker-1", "demo-worker-2"}, Size: 5 * gib},
		},
		Nodes: []longhornNode{
			{Name: "demo-worker-1", Namespace: "longhorn-system", Ready: true, Schedulable: true, AllowScheduling: true, Replicas: 3, Disks: []longhornDisk{{Path: "/var/lib/longhorn", Schedulable: true, Available: 180 * gib, Maximum: 250 * gib, Scheduled: 60 * gib}}},
			{Name: "demo-worker-2", Namespace: "longhorn-system", Ready: true, Schedulable: true, AllowScheduling: true, Replicas: 3, Disks: []longhornDisk{{Path: "/var/lib/longhorn", Schedulable: true, Available: 120 * gib, Maximum: 250 * gib, Scheduled: 110 * gib}}},
			{Name: "demo-worker-3", Namespace: "longhorn-system", EvictionRequested: true, Replicas: 3, Disks: []longhornDisk{{Path: "/var/lib/longhorn", Maximum: 250 * gib}}},
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
					DataTotal: 500 * gib, ResyncQueue: -1, ResyncErrors: -1, TableSyncQueue: -1, Tranquility: -1, StatsError: "Network error: Not connected: 9c2f61a0d4e8b7c3"},
				{ID: "1b7e44c9a2f03d58", Hostname: "garage-4kq7z", Storage: true, Zone: "zone-1", Tags: []string{"demo-worker-1"}, KubeNode: "demo-worker-1", Up: true, LastSeenSecs: -1,
					DataAvail: 310 * gib, DataTotal: 500 * gib, ResyncQueue: 655, ResyncErrors: 648, TableSyncQueue: 2100, Tranquility: 0},
				{ID: "6d03b8f15e9a2c47", Hostname: "garage-q8m2d", Storage: true, Zone: "zone-2", Tags: []string{"demo-worker-2"}, KubeNode: "demo-worker-2", Up: true, LastSeenSecs: -1,
					DataAvail: 295 * gib, DataTotal: 500 * gib, ResyncQueue: 655, ResyncErrors: 646, TableSyncQueue: 2100, Tranquility: 2},
			},
		},
		{
			Namespace: "garage-archive", Name: "garage-archive", Pod: "garage-archive-0", Pods: 1, PodsReady: 1, Version: "v2.3.0", Status: garageHealthy,
			ConnectedNodes: 1, KnownNodes: 1, StorageNodes: 1, StorageNodesUp: 1, Partitions: 256, PartitionsQuorum: 256, PartitionsAllOk: 256,
			TableSyncQueue: 12, LayoutVersion: 1, Source: garageSourceCLI,
			Nodes: []garageNode{{ID: "e41a7f0c93b2d865", Hostname: "garage-archive-0", Storage: true, Zone: "nas", Tags: []string{"nas"}, KubeNode: "demo-worker-2", Up: true, LastSeenSecs: -1,
				DataAvail: 1400 * gib, DataTotal: 2000 * gib, TableSyncQueue: 12, Tranquility: 2}},
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

	mariadb := &mariadbStatus{Version: "v1alpha1", Clusters: []mariadbCluster{
		{Namespace: "demo", Name: "shop-db", Topology: "replication", Health: healthWarning, Reasons: []string{mariadbReasonBackupFailed},
			Replicas: 2, ReadyPods: 2, Primary: "shop-db-0", Pods: []mariadbPod{
				{Name: "shop-db-0", Node: "demo-worker-2", Phase: "Running", Role: "primary", Ready: true},
				{Name: "shop-db-1", Node: "demo-worker-3", Phase: "Running", Role: "replica", Ready: true},
			}, LastBackupAt: ms(3 * 24 * time.Hour), LastBackupFailedAt: ms(20 * time.Hour), BackupSchedule: "0 3 * * *"},
		{Namespace: "demo", Name: "forum-db", Topology: "galera", Health: healthOK, Reasons: []string{},
			Replicas: 3, ReadyPods: 3, Primary: "forum-db-1", Pods: []mariadbPod{
				{Name: "forum-db-1", Node: "demo-worker-2", Phase: "Running", Role: "primary", Ready: true},
				{Name: "forum-db-0", Node: "demo-worker-1", Phase: "Running", Role: "member", Ready: true},
				{Name: "forum-db-2", Node: "demo-worker-3", Phase: "Running", Role: "member", Ready: true},
			}, LastBackupAt: ms(9 * time.Hour), BackupSchedule: "0 */12 * * *"},
		{Namespace: "demo", Name: "legacy-db", Topology: "standalone", Health: healthIdle, Reasons: []string{}, Suspended: true,
			Replicas: 1, ReadyPods: 1, Primary: "legacy-db-0", Pods: []mariadbPod{
				{Name: "legacy-db-0", Node: "demo-worker-1", Phase: "Running", Role: "primary", Ready: true},
			}},
	}}

	pxcPods := func(name string, ready ...bool) []perconaPod {
		out := []perconaPod{}
		for i, r := range ready {
			pod := perconaPod{Name: name + "-pxc-" + string(rune('0'+i)), Phase: "Pending"}
			if r {
				pod.Node, pod.Phase, pod.Ready = "demo-worker-"+string(rune('1'+i)), "Running", true
			}

			out = append(out, pod)
		}

		return out
	}

	daily := []perconaSchedule{{Name: "daily-backup", Schedule: "0 2 * * *", Keep: 7, StorageName: "s3-garage"}}

	percona := &perconaStatus{Version: "v1", Clusters: []perconaCluster{
		{Namespace: "demo", Name: "shop-mysql", State: "initializing", CRVersion: "1.18.0", Health: healthWarning,
			Reasons: []string{perconaReasonMembers, perconaReasonBackupStale}, PXCSize: 3, PXCReady: 2, Pods: pxcPods("shop-mysql", true, true, false),
			Proxy: "haproxy", ProxySize: 2, ProxyReady: 2, LastBackupAt: ms(4 * 24 * time.Hour), BackupSchedules: daily},
		{Namespace: "demo", Name: "crm-mysql", State: "ready", CRVersion: "1.18.0", Health: healthOK, Reasons: []string{},
			PXCSize: 3, PXCReady: 3, Pods: pxcPods("crm-mysql", true, true, true),
			Proxy: "proxysql", ProxySize: 2, ProxyReady: 2, LastBackupAt: ms(10 * time.Hour), BackupSchedules: daily},
		{Namespace: "demo", Name: "legacy-mysql", State: "paused", CRVersion: "1.17.0", Paused: true, Health: healthIdle, Reasons: []string{},
			PXCSize: 1, Pods: []perconaPod{}, LastBackupAt: ms(30 * 24 * time.Hour), BackupSchedules: []perconaSchedule{}},
	}}

	in := func(d time.Duration) int64 { return now.Add(d).UnixMilli() }

	const day = 24 * time.Hour

	certManager := &certManagerStatus{Version: "v1",
		Certificates: []certManagerCert{
			{Namespace: "demo", Name: "legacy-tls", SecretName: "legacy-tls", DNSNames: []string{"legacy.example.com"}, DNSNameCount: 1,
				Issuer: "Issuer/internal-ca", Health: healthCritical, Reasons: []string{certReasonExpired, certReasonIssuer},
				Message:  "Certificate expired; issuing a new one is waiting for its issuer",
				NotAfter: ms(2 * day), RenewalTime: ms(32 * day), FailedAttempts: 6},
			{Namespace: "demo", Name: "shop-tls", SecretName: "shop-tls", DNSNames: []string{"shop.example.com", "www.shop.example.com"}, DNSNameCount: 2,
				Issuer: "ClusterIssuer/letsencrypt", Health: healthWarning, Reasons: []string{certReasonNotReady, certReasonExpiring, certReasonRenewalOverdue},
				Message:  "The certificate request has failed to complete and will be retried: Failed to wait for order resource \"shop-tls-1-2468\" to become ready",
				NotAfter: in(11 * day), RenewalTime: ms(19 * day), FailedAttempts: 3},
			{Namespace: "demo", Name: "wildcard-tls", SecretName: "wildcard-tls", DNSNames: []string{"*.example.com", "example.com"}, DNSNameCount: 2,
				Issuer: "ClusterIssuer/letsencrypt", Health: healthOK, Reasons: []string{}, Ready: true,
				NotAfter: in(63 * day), RenewalTime: in(33 * day)},
			{Namespace: "monitoring", Name: "grafana-tls", SecretName: "grafana-tls", DNSNames: []string{"grafana.example.com"}, DNSNameCount: 1,
				Issuer: "ClusterIssuer/letsencrypt", Health: healthOK, Reasons: []string{}, Ready: true,
				NotAfter: in(81 * day), RenewalTime: in(51 * day)},
		},
		Issuers: []certIssuer{
			{Kind: "Issuer", Namespace: "demo", Name: "internal-ca", Type: "ca", Health: healthWarning,
				Message: "Error getting keypair for CA issuer: secrets \"internal-ca\" not found"},
			{Kind: "ClusterIssuer", Name: "letsencrypt", Type: "acme", Server: "acme-v02.api.letsencrypt.org", Ready: true, Health: healthOK,
				Message: "The ACME account was registered with the ACME server"},
		},
	}

	backup := func(name, phase string, ago time.Duration, errs, warns int) *veleroBackup {
		return &veleroBackup{Name: name, Phase: phase, StartedAt: ms(ago + 10*time.Minute), CompletedAt: ms(ago), Errors: errs, Warnings: warns}
	}

	velero := &veleroStatus{Version: "v1",
		Schedules: []veleroSchedule{
			{Namespace: "velero", Name: "offsite-weekly", Schedule: "0 3 * * 0", Phase: "Enabled", ValidationErrors: []string{}, Health: healthCritical,
				Reasons: []string{veleroReasonFailed, veleroReasonLocation}, StorageLocation: "offsite", IncludedNamespaces: []string{"*"},
				LastBackup:    &veleroBackup{Name: "offsite-weekly-20261004030000", Phase: "Failed", StartedAt: ms(9 * time.Hour), CompletedAt: ms(9 * time.Hour), FailureReason: "backup storage location offsite is unavailable"},
				LastSuccessAt: ms(8 * 24 * time.Hour)},
			{Namespace: "velero", Name: "apps-daily", Schedule: "0 2 * * *", Phase: "Enabled", ValidationErrors: []string{}, Health: healthWarning,
				Reasons: []string{veleroReasonPartial}, StorageLocation: "default", IncludedNamespaces: []string{"demo", "monitoring"},
				LastBackup: backup("apps-daily-20261004020000", "PartiallyFailed", 10*time.Hour, 2, 5), LastSuccessAt: ms(34 * time.Hour)},
			{Namespace: "velero", Name: "cluster-hourly", Schedule: "@every 1h", Phase: "Enabled", ValidationErrors: []string{}, Health: healthOK,
				Reasons: []string{}, StorageLocation: "default", IncludedNamespaces: []string{},
				LastBackup: backup("cluster-hourly-20261004110000", "Completed", 40*time.Minute, 0, 0), LastSuccessAt: ms(40 * time.Minute), InProgress: true},
			{Namespace: "velero", Name: "media-monthly", Schedule: "0 4 1 * *", Paused: true, Phase: "Enabled", ValidationErrors: []string{}, Health: healthIdle,
				Reasons: []string{}, StorageLocation: "default", IncludedNamespaces: []string{"media"},
				LastBackup: backup("media-monthly-20260901040000", "Completed", 33*24*time.Hour, 0, 0), LastSuccessAt: ms(33 * 24 * time.Hour)},
		},
		Adhoc: []veleroAdhoc{
			{Namespace: "velero", veleroBackup: *backup("before-upgrade", "PartiallyFailed", 2*24*time.Hour, 1, 0), StorageLocation: "default", Health: healthWarning},
		},
		Locations: []veleroLocation{
			{Namespace: "velero", Name: "offsite", Provider: "aws", Bucket: "offsite-backups", Phase: "Unavailable", Health: healthCritical,
				Message: "rpc error: code = Unknown desc = operation error S3: ListObjectsV2, https response error StatusCode: 403", LastValidatedAt: ms(2 * time.Minute)},
			{Namespace: "velero", Name: "default", Provider: "aws", Bucket: "velero", Default: true, Phase: "Available", Health: healthOK, LastValidatedAt: ms(time.Minute)},
		},
	}

	tib := 1024 * gib
	ceph := &cephStatus{Version: "v1",
		Clusters: []cephCluster{
			{Namespace: "rook-ceph", Name: "rook-ceph", Phase: "Ready", Message: "Cluster created successfully", CephHealth: "HEALTH_WARN",
				Health: healthWarning, Reasons: []string{cephReasonHealthWarn, cephReasonNearFull, cephReasonOSDs},
				Checks: []cephCheck{
					{Name: "OSD_DOWN", Severity: "HEALTH_WARN", Message: "1 osds down"},
					{Name: "OSD_NEARFULL", Severity: "HEALTH_WARN", Message: "1 nearfull osd(s)"},
					{Name: "PG_DEGRADED", Severity: "HEALTH_WARN", Message: "Degraded data redundancy: 2114/6342 objects degraded (33.333%), 97 pgs degraded"},
				},
				BytesTotal: 3 * tib, BytesUsed: 2662 * gib, OSDsUp: 2, OSDsTotal: 3, MonsReady: 3, MonsTotal: 3,
				NotReadyNodes: []string{"demo-worker-3"}, Version: "19.2.3-0"},
		},
		Pools: []cephPool{
			{Namespace: "rook-ceph", Name: "ceph-objectstore", Kind: "objectStore", Phase: "Progressing", Health: healthWarning},
			{Namespace: "rook-ceph", Name: "ceph-blockpool", Kind: "blockPool", Phase: "Ready", Health: healthOK},
			{Namespace: "rook-ceph", Name: "ceph-filesystem", Kind: "filesystem", Phase: "Ready", Health: healthOK},
		},
		OSDs: []cephOSD{
			{Namespace: "rook-ceph", ID: "0", Pod: "rook-ceph-osd-0-6d8f9c7b5-x2k4p", Node: "demo-worker-1", Phase: "Running", Ready: true},
			{Namespace: "rook-ceph", ID: "1", Pod: "rook-ceph-osd-1-7c9b8d6f4-q8m2d", Node: "demo-worker-2", Phase: "Running", Ready: true},
			{Namespace: "rook-ceph", ID: "2", Pod: "rook-ceph-osd-2-5f7d6c8b9-r4t7n", Node: "demo-worker-3", Phase: "Running"},
		},
	}

	return dataServices{Longhorn: longhorn, Garage: garage, CNPG: cnpg, Dragonfly: dragonfly, MariaDB: mariadb, Percona: percona, CertManager: certManager, Velero: velero, Ceph: ceph, CastAI: demoCastAI()}
}

// demoCastAI is the demo cluster's CAST AI recommendations: the Workload Autoscaler right-sizing
// the demo workloads, one it cannot apply and one it is told not to.
func demoCastAI() *castAIStatus {
	container := func(name, origCPU, origMem, cpu, mem, cpuLimit, memLimit string) castAIContainer {
		return castAIContainer{Name: name, OriginalCPU: origCPU, OriginalMemory: origMem, CPU: cpu, Memory: mem, CPULimit: cpuLimit, MemoryLimit: memLimit}
	}

	recs := []castAIRecommendation{
		{Namespace: "demo", Name: "hello-ichor-deployment", Kind: "Deployment", Workload: "hello-ichor", Mode: "deferred", Health: healthOK, Reasons: []string{},
			Containers:    []castAIContainer{container("hello", "500m", "512Mi", "60m", "180Mi", "1", "512Mi")},
			CPUDeltaMilli: -440, MemoryDeltaBytes: -332 * 1024 * 1024},
		{Namespace: "demo", Name: "worker-deployment", Kind: "Deployment", Workload: "worker", Mode: "immediate", Health: healthOK, Reasons: []string{},
			Containers:    []castAIContainer{container("worker", "250m", "256Mi", "410m", "640Mi", "", "")},
			CPUDeltaMilli: 160, MemoryDeltaBytes: 384 * 1024 * 1024},
		{Namespace: "demo", Name: "postgres-statefulset", Kind: "StatefulSet", Workload: "postgres", Mode: "deferred", Health: healthWarning, Reasons: []string{castAIReasonReadOnly}, ReadOnly: true,
			Message:       "Workload is managed by another autoscaler.",
			Containers:    []castAIContainer{container("postgres", "1", "2Gi", "350m", "1536Mi", "2", "2Gi")},
			CPUDeltaMilli: -650, MemoryDeltaBytes: -512 * 1024 * 1024},
		{Namespace: "kube-system", Name: "coredns-deployment", Kind: "Deployment", Workload: "coredns", Mode: "immediate", Health: healthCritical, Reasons: []string{castAIReasonVPA},
			Message:       "VPA recommendation could not be applied: the mutating webhook is unreachable.",
			Containers:    []castAIContainer{container("coredns", "100m", "70Mi", "30m", "90Mi", "", "170Mi")},
			CPUDeltaMilli: -70, MemoryDeltaBytes: 20 * 1024 * 1024},
	}

	out := &castAIStatus{Version: castAIVersion, Recommendations: recs}
	for _, r := range recs {
		out.Compared++
		out.CPUDeltaMilli += r.CPUDeltaMilli
		out.MemoryDeltaBytes += r.MemoryDeltaBytes
	}

	slices.SortFunc(out.Recommendations, byHealthThenKey(func(r castAIRecommendation) (string, string) { return r.Health, r.Namespace + "/" + r.Name }))

	return out
}

// demoGarageBlockReport is the demo cluster's blocks failing to resync: one a live object
// references, one kept by a stale reference, one deleted data awaiting cleanup.
func demoGarageBlockReport() garageBlockReport {
	hash := func(c byte) string { return strings.Repeat(string(c), 64) }

	return garageBlockReport{
		Errored: 1294, Detailed: 3, Live: 1, CleanupOnly: 2, StaleRefs: 1, RefcountMismatches: 1, Retryable: 1290,
		Nodes: []garageBlockNode{
			{ID: "1b7e44c9a2f03d58", Hostname: "garage-4kq7z", Errored: 648, Blocks: []garageBlock{
				{Hash: hash('a'), Refcount: 1, Errors: 41, LastTrySecs: 300, NextTrySecs: 3300, Impact: garageImpactLive, Refs: []garageBlockRef{
					{Kind: "object", Bucket: "4f1d0c2e9b7a6583", Key: "photos/2024/beach.jpg", Live: true},
				}},
				{Hash: hash('b'), Refcount: 1, Errors: 38, LastTrySecs: 600, NextTrySecs: 3000, Impact: garageImpactStale, StaleRef: true, RefcountMismatch: true, Refs: []garageBlockRef{
					{Kind: "upload", Bucket: "4f1d0c2e9b7a6583", Key: "backups/db.tar", UploadID: "c3a9e1f07b2d4e68"},
				}},
			}},
			{ID: "6d03b8f15e9a2c47", Hostname: "garage-q8m2d", Errored: 646, Blocks: []garageBlock{
				{Hash: hash('c'), Errors: 12, LastTrySecs: 120, NextTrySecs: 1800, Impact: garageImpactCleanup, Refs: []garageBlockRef{}},
			}},
			{ID: "9c2f61a0d4e8b7c3", Hostname: "garage-2xv9p", Error: "Network error: Not connected: 9c2f61a0d4e8b7c3", Blocks: []garageBlock{}},
		},
	}
}
