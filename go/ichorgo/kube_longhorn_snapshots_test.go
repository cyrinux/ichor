package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// lhSnapshots and lhBackups are Longhorn v1beta2 lists: vol-a has a backed-up CSI snapshot,
// a system one and a deleted one waiting for the purge; vol-z is gone.
const (
	lhSnapshots = `{"items":[
 {"metadata":{"name":"snapshot-old","labels":{"longhornvolume":"vol-a"}},"spec":{"volume":"vol-a"},
  "status":{"creationTime":"2026-09-04T12:00:00Z","size":1024,"readyToUse":true,"userCreated":true}},
 {"metadata":{"name":"0b7e-system","labels":{"longhornvolume":"vol-a"}},"spec":{"volume":"vol-a"},
  "status":{"creationTime":"2026-10-14T18:00:00Z","size":2048,"readyToUse":true}},
 {"metadata":{"name":"snapshot-gone","labels":{"longhornvolume":"vol-a"}},"spec":{"volume":"vol-a"},
  "status":{"creationTime":"2026-10-15T19:06:14Z","markRemoved":true,"userCreated":true}},
 {"metadata":{"name":"base","labels":{"longhornvolume":"vol-b"}},"spec":{},"status":{"readyToUse":true,"size":4096}},
 {"metadata":{"name":"orphan"},"spec":{"volume":"vol-z"},"status":{"readyToUse":true}}]}`
	lhBackups = `{"items":[
 {"metadata":{"name":"backup-1","labels":{"backup-volume":"vol-a"}},"status":{"volumeName":"vol-a","snapshotName":"snapshot-old",
  "state":"Completed","progress":100,"backupCreatedAt":"2026-09-04T12:05:00Z","size":"1457520640"}},
 {"metadata":{"name":"backup-2","labels":{"backup-volume":"vol-a"}},"status":{"snapshotName":"0b7e-system",
  "state":"Error","progress":40,"backupCreatedAt":"2026-10-14T18:01:00Z","error":"target unreachable"}},
 {"metadata":{"name":"backup-3","labels":{"backup-volume":"vol-a"}},"status":{"volumeName":"vol-a","snapshotName":"x"}}]}`
)

func TestAddLonghornSnapshots(t *testing.T) {
	var snaps kubeList[lhSnapshotObject]
	var backups kubeList[lhBackupObject]

	if err := json.Unmarshal([]byte(lhSnapshots), &snaps); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(lhBackups), &backups); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 15, 20, 0, 0, 0, time.UTC)
	vols := []longhornVolume{
		{Name: "vol-a", LastBackupAt: unixMilli("2026-09-04T12:05:00Z")},
		{Name: "vol-b", LastBackupAt: unixMilli("2026-10-14T00:00:00Z")},
		{Name: "vol-c"},
	}
	addLonghornSnapshots(vols, snaps.Items, backups.Items, now)

	a, b, c := vols[0], vols[1], vols[2]

	if len(a.Snapshots) != 2 || a.Snapshots[0].Name != "0b7e-system" || a.Snapshots[1].Name != "snapshot-old" {
		t.Fatalf("vol-a snapshots, newest first, deleted one left out: %+v", a.Snapshots)
	}

	if old := a.Snapshots[1]; !old.BackedUp || !old.UserCreated || old.Size != 1024 || old.CreatedAt != unixMilli("2026-09-04T12:00:00Z") {
		t.Errorf("snapshot-old: %+v", old)
	}

	if a.Snapshots[0].BackedUp {
		t.Errorf("a failed backup does not cover its snapshot: %+v", a.Snapshots[0])
	}

	if a.SnapshotsRemoving != 1 || a.BackupCount != 3 || !a.BackupStale {
		t.Errorf("vol-a: removing %d, backups %d, stale %v", a.SnapshotsRemoving, a.BackupCount, a.BackupStale)
	}

	if got := a.Backups; got[0].Name != "backup-2" || got[0].State != "Error" || got[0].Error != "target unreachable" || got[1].Size != 1457520640 || got[2].State != "Pending" {
		t.Errorf("vol-a backups, newest first, the label for a missing volume name: %+v", got)
	}

	if len(b.Snapshots) != 1 || b.Snapshots[0].CreatedAt != 0 || b.BackupStale || b.Backups == nil {
		t.Errorf("vol-b: the label names the volume, a recent backup is not stale: %+v", b)
	}

	if c.Snapshots == nil || c.Backups == nil || c.BackupStale {
		t.Errorf("vol-c: empty lists, not null: %+v", c)
	}
}

func TestAddLonghornSnapshotsKeepsTheNewestBackups(t *testing.T) {
	var backups []lhBackupObject

	for i := range lhBackupsPerVolume + 5 {
		var b lhBackupObject
		b.Status.VolumeName = "vol-a"
		b.Status.State = "Completed"
		b.Status.BackupCreatedAt = time.Date(2026, 10, 1+i, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		backups = append(backups, b)
	}

	vols := []longhornVolume{{Name: "vol-a"}}
	addLonghornSnapshots(vols, nil, backups, time.Now())

	if len(vols[0].Backups) != lhBackupsPerVolume || vols[0].BackupCount != lhBackupsPerVolume+5 {
		t.Fatalf("kept %d of %d", len(vols[0].Backups), vols[0].BackupCount)
	}

	if vols[0].Backups[0].CreatedAt != unixMilli(backups[len(backups)-1].Status.BackupCreatedAt) {
		t.Fatalf("newest first: %+v", vols[0].Backups[0])
	}
}

func TestLonghornSnapshotActionsGoThroughTheManager(t *testing.T) {
	cases := map[string][]string{
		lhSnapshotTake:   {"action=snapshotCreate {}"},
		lhSnapshotBackup: {`action=snapshotBackup {"name":"snap-1"}`},
		lhSnapshotDelete: {`action=snapshotDelete {"name":"snap-1"}`, "action=snapshotPurge {}"},
	}

	for action, want := range cases {
		f, k := longhornActionAPI(t, "attached")

		if err := longhornSnapshotAction(context.Background(), k, "storage", "pvc-1", "snap-1", action); err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		var got []string

		for _, r := range f.recorded() {
			if r.method == "POST" && r.path == lhProxyPath {
				got = append(got, r.query+" "+r.body)
			}
		}

		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s: %q", action, got)
		}
	}
}

func TestLonghornSnapshotActionRefusesADetachedVolume(t *testing.T) {
	f, k := longhornActionAPI(t, "detached")

	if err := longhornSnapshotAction(context.Background(), k, "storage", "pvc-1", "", lhSnapshotTake); !errors.Is(err, errLonghornDetached) {
		t.Fatalf("got %v", err)
	}

	if last := lastRequest(f); last.method != "GET" {
		t.Fatalf("nothing may be sent: %+v", last)
	}
}

func TestKubeLonghornSnapshotActionValidates(t *testing.T) {
	cases := []struct{ volume, snapshot, action, want string }{
		{"pvc-1", "snap-1", "revert", "unsupported"},
		{"", "", lhSnapshotTake, "no Longhorn volume"},
		{"pvc-1", "", lhSnapshotBackup, "no Longhorn snapshot"},
		{"pvc-1", "../x", lhSnapshotDelete, "invalid Kubernetes name"},
		{"pvc-1", "volume-head", lhSnapshotDelete, "live data"},
	}

	for _, c := range cases {
		err := KubeLonghornSnapshotAction("", "", "", "storage", c.volume, c.snapshot, c.action)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v", c, err)
		}
	}
}
