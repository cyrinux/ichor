package ichorgo

import "time"

func demoVolumeSnapshots(now time.Time) volumeSnapshots {
	r := emptyVolumeSnapshots()
	r.Installed = true
	r.Version = "v1"
	r.Classes = []snapshotClass{{Name: "csi-default", Driver: "csi.example.io", DeletionPolicy: "Delete", Default: true}}
	r.Snapshots = []volumeSnapshot{
		{Namespace: "shop", Name: "orders-nightly", PVC: "orders", Class: "csi-default", Content: "orders-content", Ready: true, CreatedAt: now.Add(-6 * time.Hour).UnixMilli(), AgeSeconds: 21600},
		{Namespace: "billing", Name: "ledger-nightly", PVC: "ledger", Class: "csi-default", Content: "ledger-content", Ready: true, CreatedAt: now.Add(-72 * time.Hour).UnixMilli(), AgeSeconds: 259200},
		{Namespace: "shop", Name: "cache-failed", PVC: "cache", Class: "csi-default", CreatedAt: now.Add(-time.Hour).UnixMilli(), AgeSeconds: 3600, Error: "snapshot storage is unavailable"},
	}
	r.Contents = []snapshotContent{{Name: "orders-content", Namespace: "shop", Snapshot: "orders-nightly", Class: "csi-default", Driver: "csi.example.io", Ready: true, CreatedAt: r.Snapshots[0].CreatedAt}}
	r.Claims = []snapshotClaim{{Namespace: "shop", Name: "orders", LastGoodAt: r.Snapshots[0].CreatedAt, Watched: true}, {Namespace: "billing", Name: "ledger", LastGoodAt: r.Snapshots[1].CreatedAt, Watched: true}, {Namespace: "shop", Name: "cache", Watched: true}}
	for i := range r.Claims {
		setSnapshotFreshness(&r.Claims[i], now)
	}
	return r
}
func demoBackupJobs(now time.Time) *backupJobStatus {
	return &backupJobStatus{Jobs: []backupCronJob{
		{Namespace: "billing", Name: "ledger-backup", Schedule: "0 2 * * *", LastSuccess: now.Add(-96 * time.Hour).UnixMilli(), AgeSeconds: 345600, FailureStreak: 3, Health: healthCritical},
		{Namespace: "shop", Name: "orders-backup", Schedule: "0 1 * * *", LastSuccess: now.Add(-12 * time.Hour).UnixMilli(), AgeSeconds: 43200, Health: healthOK},
	}}
}
