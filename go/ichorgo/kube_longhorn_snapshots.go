package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Longhorn's own snapshots and backups, per volume. They are Longhorn objects
// (snapshots.longhorn.io, backups.longhorn.io), not CSI VolumeSnapshots: a CSI snapshot of a
// Longhorn volume shows here too, as the Longhorn snapshot behind it.

const (
	lhBackupsPerVolume = 20                 // newest kept per volume; BackupCount has them all
	lhBackupStaleAfter = 7 * 24 * time.Hour // older last backup: flagged
)

type longhornSnapshot struct {
	Name        string `json:"name"`
	CreatedAt   int64  `json:"createdAt"` // unix ms, 0 when Longhorn has none (a restored volume's base)
	Size        int64  `json:"size"`      // data it holds on top of its parent
	Ready       bool   `json:"ready"`
	UserCreated bool   `json:"userCreated"` // asked for (by a user, a job, a CSI snapshot), not taken by Longhorn itself
	BackedUp    bool   `json:"backedUp"`    // a completed backup was taken from it
	Error       string `json:"error"`
}

type longhornBackup struct {
	Name      string `json:"name"`
	Snapshot  string `json:"snapshot"`
	State     string `json:"state"` // Completed|InProgress|Pending|Error|Unknown
	Progress  int    `json:"progress"`
	CreatedAt int64  `json:"createdAt"` // unix ms
	Size      int64  `json:"size"`
	Error     string `json:"error"`
}

type lhSnapshotObject struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Volume string `json:"volume"`
	} `json:"spec"`
	Status struct {
		CreationTime string `json:"creationTime"`
		Size         int64  `json:"size"`
		ReadyToUse   bool   `json:"readyToUse"`
		MarkRemoved  bool   `json:"markRemoved"`
		UserCreated  bool   `json:"userCreated"`
		Error        string `json:"error"`
	} `json:"status"`
}

type lhBackupObject struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		VolumeName      string `json:"volumeName"`
		SnapshotName    string `json:"snapshotName"`
		State           string `json:"state"`
		Progress        int    `json:"progress"`
		BackupCreatedAt string `json:"backupCreatedAt"`
		Size            string `json:"size"` // bytes, as a string
		Error           string `json:"error"`
	} `json:"status"`
}

func (s lhSnapshotObject) volume() string {
	return cmp.Or(s.Spec.Volume, s.Metadata.Labels["longhornvolume"])
}

func (b lhBackupObject) volume() string {
	return cmp.Or(b.Status.VolumeName, b.Metadata.Labels["backup-volume"])
}

// addLonghornSnapshots gives each volume its live snapshots and its backups, newest first.
// Snapshots marked removed wait for Longhorn to purge them: counted, not listed.
func addLonghornSnapshots(volumes []longhornVolume, snapshots []lhSnapshotObject, backups []lhBackupObject, now time.Time) {
	backed := map[string]bool{} // volume/snapshot with a completed backup
	byVolume := map[string][]longhornBackup{}

	for _, b := range backups {
		vol := b.volume()
		if b.Status.State == "Completed" {
			backed[vol+"/"+b.Status.SnapshotName] = true
		}

		size, _ := strconv.ParseInt(b.Status.Size, 10, 64)
		byVolume[vol] = append(byVolume[vol], longhornBackup{
			Name: b.Metadata.Name, Snapshot: b.Status.SnapshotName, State: cmp.Or(b.Status.State, "Pending"),
			Progress: b.Status.Progress, CreatedAt: unixMilli(b.Status.BackupCreatedAt), Size: size, Error: b.Status.Error,
		})
	}

	snaps := map[string][]longhornSnapshot{}
	removing := map[string]int{}

	for _, s := range snapshots {
		vol := s.volume()
		if s.Status.MarkRemoved {
			removing[vol]++
			continue
		}

		snaps[vol] = append(snaps[vol], longhornSnapshot{
			Name: s.Metadata.Name, CreatedAt: unixMilli(s.Status.CreationTime), Size: s.Status.Size,
			Ready: s.Status.ReadyToUse, UserCreated: s.Status.UserCreated, Error: s.Status.Error,
			BackedUp: backed[vol+"/"+s.Metadata.Name],
		})
	}

	for i := range volumes {
		v := &volumes[i]

		v.Snapshots = append([]longhornSnapshot{}, snaps[v.Name]...)
		slices.SortFunc(v.Snapshots, func(a, b longhornSnapshot) int {
			return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.Name, b.Name))
		})
		v.SnapshotsRemoving = removing[v.Name]

		list := byVolume[v.Name]
		slices.SortFunc(list, func(a, b longhornBackup) int {
			return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.Name, b.Name))
		})
		v.BackupCount = len(list)
		v.Backups = append([]longhornBackup{}, list[:min(len(list), lhBackupsPerVolume)]...)

		v.BackupStale = v.LastBackupAt > 0 && now.Sub(time.UnixMilli(v.LastBackupAt)) > lhBackupStaleAfter
	}
}

// Snapshot actions, through Longhorn's API like a volume backup: the engine of an attached
// volume runs them.
const (
	lhSnapshotTake   = "take"   // a new snapshot (no name: Longhorn picks one)
	lhSnapshotBackup = "backup" // back the snapshot up to the volume's target
	lhSnapshotDelete = "delete" // delete it, then purge so its space comes back
)

var lhSnapshotActions = []string{lhSnapshotTake, lhSnapshotBackup, lhSnapshotDelete}

// KubeLonghornSnapshotAction runs a snapshot action on the Longhorn volume namespace/volume
// (os:admin): take (snapshot ""), backup or delete the snapshot named snapshot. The volume
// must be attached. kubeServer: see KubePods.
func KubeLonghornSnapshotAction(configYAML, contextName, kubeServer, namespace, volume, snapshot, action string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, volume = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(volume))
	snapshot = strings.TrimSpace(snapshot)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "longhorn-snapshot-" + action, Namespace: namespace, Object: volume, Params: snapshot})

	if !slices.Contains(lhSnapshotActions, action) {
		return fmt.Errorf("unsupported Longhorn snapshot action %q", action)
	}

	if err := validateKubeName("Longhorn volume", namespace, volume); err != nil {
		return err
	}

	if action != lhSnapshotTake {
		if err := validateKubeName("Longhorn snapshot", namespace, snapshot); err != nil {
			return err
		}

		if snapshot == "volume-head" {
			return errors.New("volume-head is the live data, not a snapshot")
		}
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return longhornSnapshotAction(ctx, k, namespace, volume, snapshot, action)
	})
}

func longhornSnapshotAction(ctx context.Context, k *kubeClient, namespace, volume, snapshot, action string) error {
	if err := longhornRequireAttached(ctx, k, namespace, volume); err != nil {
		return err
	}

	switch action {
	case lhSnapshotTake:
		return longhornManagerAction(ctx, k, namespace, volume, "snapshotCreate", map[string]any{})
	case lhSnapshotBackup:
		return longhornManagerAction(ctx, k, namespace, volume, "snapshotBackup", map[string]any{"name": snapshot})
	default:
		if err := longhornManagerAction(ctx, k, namespace, volume, "snapshotDelete", map[string]any{"name": snapshot}); err != nil {
			return err
		}
		// What Longhorn's UI does too: a deleted snapshot keeps its space until purged.
		return longhornManagerAction(ctx, k, namespace, volume, "snapshotPurge", map[string]any{})
	}
}

// longhornRequireAttached fails unless Longhorn is installed and the volume attached.
func longhornRequireAttached(ctx context.Context, k *kubeClient, namespace, volume string) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[groupLonghorn]
	if !ok {
		return errLonghornMissing
	}

	var vol lhVolumeRef
	if err := k.get(ctx, "/apis/"+groupLonghorn+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/volumes/"+url.PathEscape(volume), &vol); err != nil {
		return err
	}

	if vol.Status.State != "attached" {
		return errLonghornDetached
	}

	return nil
}
