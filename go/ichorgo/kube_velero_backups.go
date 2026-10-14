package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// veleroBackupsMax is how many backups one list keeps, the newest.
const veleroBackupsMax = 50

// veleroBackupList is one schedule's backups (or the ones taken by hand when Schedule is ""),
// newest first. Error is the deletion requests being unreadable: the backups still show.
type veleroBackupList struct {
	Namespace string               `json:"namespace"`
	Schedule  string               `json:"schedule"`
	Backups   []veleroBackupDetail `json:"backups"`
	Error     string               `json:"error"`
}

type veleroBackupDetail struct {
	Name                     string   `json:"name"`
	Phase                    string   `json:"phase"`
	StartedAt                int64    `json:"startedAt"`
	CompletedAt              int64    `json:"completedAt"`
	ExpiresAt                int64    `json:"expiresAt"` // unix ms, 0 when it never expires
	Errors                   int      `json:"errors"`
	Warnings                 int      `json:"warnings"`
	FailureReason            string   `json:"failureReason"`
	ValidationErrors         []string `json:"validationErrors"`
	StorageLocation          string   `json:"storageLocation"`
	IncludedNamespaces       []string `json:"includedNamespaces"`
	IncludedResources        []string `json:"includedResources"`
	ItemsBackedUp            int      `json:"itemsBackedUp"`
	TotalItems               int      `json:"totalItems"`
	VolumeSnapshots          int      `json:"volumeSnapshots"` // completed
	VolumeSnapshotsAttempted int      `json:"volumeSnapshotsAttempted"`
	CSISnapshots             int      `json:"csiSnapshots"` // completed
	Running                  bool     `json:"running"`
	Finished                 bool     `json:"finished"`
	// Deleting: the backup is being deleted, or a deletion request for it is pending.
	Deleting bool `json:"deleting"`
	// DeleteErrors are the errors of the latest deletion request that failed.
	DeleteErrors []string `json:"deleteErrors"`
}

// veleroBackupFull is the Backup as the list reads it.
type veleroBackupFull struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		StorageLocation    string   `json:"storageLocation"`
		IncludedNamespaces []string `json:"includedNamespaces"`
		IncludedResources  []string `json:"includedResources"`
	} `json:"spec"`
	Status struct {
		Phase                       string   `json:"phase"`
		StartTimestamp              string   `json:"startTimestamp"`
		CompletionTimestamp         string   `json:"completionTimestamp"`
		Expiration                  string   `json:"expiration"`
		Errors                      int      `json:"errors"`
		Warnings                    int      `json:"warnings"`
		FailureReason               string   `json:"failureReason"`
		ValidationErrors            []string `json:"validationErrors"`
		VolumeSnapshotsAttempted    int      `json:"volumeSnapshotsAttempted"`
		VolumeSnapshotsCompleted    int      `json:"volumeSnapshotsCompleted"`
		CSIVolumeSnapshotsCompleted int      `json:"csiVolumeSnapshotsCompleted"`
		Progress                    struct {
			TotalItems    int `json:"totalItems"`
			ItemsBackedUp int `json:"itemsBackedUp"`
		} `json:"progress"`
	} `json:"status"`
}

// KubeVeleroBackups lists the backups of the Velero schedule namespace/schedule, or the ones
// taken by hand when schedule is "", newest first (at most 50), with the deletions pending or
// failed (os:admin): {"namespace","schedule","backups","error"} (see veleroBackupList).
// kubeServer: see KubePods.
func KubeVeleroBackups(configYAML, contextName, kubeServer, namespace, schedule string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, schedule = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(schedule))

	if err := validateKubeName("backups", namespace, cmp.Or(schedule, "adhoc")); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() veleroBackupList { return demoVeleroBackups(namespace, schedule, time.Now()) },
		func(ctx context.Context, k *kubeClient) (veleroBackupList, error) {
			return readVeleroBackups(ctx, k, namespace, schedule)
		})
}

func readVeleroBackups(ctx context.Context, k *kubeClient, namespace, schedule string) (veleroBackupList, error) {
	base := veleroNamespacePath(namespace)

	selector := "!" + veleroScheduleLabel // the backups taken by hand carry no schedule label
	if schedule != "" {
		selector = veleroScheduleLabel + "=" + veleroLabelValue(schedule)
	}

	var (
		backups                 kubeList[veleroBackupFull]
		requests                kubeList[veleroDeleteRequestObject]
		backupsErr, requestsErr error
		wg                      sync.WaitGroup
	)

	wg.Go(func() {
		backupsErr = getList(ctx, k, base+"backups?labelSelector="+url.QueryEscape(selector), &backups)
	})
	wg.Go(func() { requestsErr = getList(ctx, k, base+"deletebackuprequests", &requests) })
	wg.Wait()

	if backupsErr != nil {
		return veleroBackupList{}, backupsErr
	}

	return veleroBackupList{
		Namespace: namespace,
		Schedule:  schedule,
		Backups:   mapVeleroBackups(backups.Items, requests.Items),
		Error:     sectionError(requestsErr),
	}, nil
}

// mapVeleroBackups maps the backups, newest first, and marks the ones a deletion request
// names: pending (deleting), or processed with errors (it failed and stayed).
func mapVeleroBackups(backups []veleroBackupFull, requests []veleroDeleteRequestObject) []veleroBackupDetail {
	pending := map[string]bool{}
	failed := map[string]veleroDeleteRequestObject{}

	for _, r := range requests {
		name := r.Spec.BackupName
		switch {
		case r.pending():
			pending[name] = true
		case len(r.Status.Errors) > 0:
			if prev, ok := failed[name]; !ok || unixMilli(r.Metadata.CreationTimestamp) > unixMilli(prev.Metadata.CreationTimestamp) {
				failed[name] = r
			}
		}
	}

	sorted := slices.Clone(backups)
	slices.SortFunc(sorted, func(a, b veleroBackupFull) int {
		return cmp.Or(cmp.Compare(veleroBackupFullAt(b), veleroBackupFullAt(a)), strings.Compare(a.Metadata.Name, b.Metadata.Name))
	})

	out := make([]veleroBackupDetail, 0, min(len(sorted), veleroBackupsMax))
	for _, b := range sorted[:min(len(sorted), veleroBackupsMax)] {
		d := veleroBackupDetailOf(b)
		d.Deleting = d.Deleting || pending[d.Name]
		if r, ok := failed[d.Name]; ok && !d.Deleting {
			d.DeleteErrors = r.Status.Errors
		}

		out = append(out, d)
	}

	return out
}

// veleroBackupFullAt is when a backup ended, else started, else was created.
func veleroBackupFullAt(b veleroBackupFull) int64 {
	return cmp.Or(unixMilli(b.Status.CompletionTimestamp), unixMilli(b.Status.StartTimestamp), unixMilli(b.Metadata.CreationTimestamp))
}

func veleroBackupDetailOf(b veleroBackupFull) veleroBackupDetail {
	orEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}

		return s
	}

	return veleroBackupDetail{
		Name: b.Metadata.Name, Phase: b.Status.Phase,
		StartedAt: unixMilli(b.Status.StartTimestamp), CompletedAt: unixMilli(b.Status.CompletionTimestamp),
		ExpiresAt: unixMilli(b.Status.Expiration),
		Errors:    b.Status.Errors, Warnings: b.Status.Warnings, FailureReason: b.Status.FailureReason,
		ValidationErrors:         orEmpty(b.Status.ValidationErrors),
		StorageLocation:          b.Spec.StorageLocation,
		IncludedNamespaces:       orEmpty(b.Spec.IncludedNamespaces),
		IncludedResources:        orEmpty(b.Spec.IncludedResources),
		ItemsBackedUp:            b.Status.Progress.ItemsBackedUp,
		TotalItems:               b.Status.Progress.TotalItems,
		VolumeSnapshots:          b.Status.VolumeSnapshotsCompleted,
		VolumeSnapshotsAttempted: b.Status.VolumeSnapshotsAttempted,
		CSISnapshots:             b.Status.CSIVolumeSnapshotsCompleted,
		Running:                  veleroRunning(b.Status.Phase),
		Finished:                 veleroFinished(b.Status.Phase),
		Deleting:                 b.Status.Phase == "Deleting",
		DeleteErrors:             []string{},
	}
}

// demoVeleroBackups is the demo cluster's list: a few nightly backups of apps-daily (one being
// deleted, one partially failed), a run of any other schedule, and the ad-hoc one.
func demoVeleroBackups(namespace, schedule string, now time.Time) veleroBackupList {
	at := func(ago time.Duration) int64 { return now.Add(-ago).UnixMilli() }
	backup := func(name, phase string, ago time.Duration, errs, warns int) veleroBackupDetail {
		return veleroBackupDetail{
			Name: name, Phase: phase, StartedAt: at(ago + 10*time.Minute), CompletedAt: at(ago), ExpiresAt: at(ago - 30*24*time.Hour),
			Errors: errs, Warnings: warns, ValidationErrors: []string{}, StorageLocation: "default",
			IncludedNamespaces: []string{"demo", "monitoring"}, IncludedResources: []string{},
			ItemsBackedUp: 412, TotalItems: 412, CSISnapshots: 3, Finished: veleroFinished(phase), Deleting: phase == "Deleting", DeleteErrors: []string{},
		}
	}

	out := veleroBackupList{Namespace: namespace, Schedule: schedule, Backups: []veleroBackupDetail{}}

	switch schedule {
	case "apps-daily":
		out.Backups = append(out.Backups,
			backup("apps-daily-20261004020000", "PartiallyFailed", 10*time.Hour, 2, 5),
			backup("apps-daily-20261003020000", "Completed", 34*time.Hour, 0, 0),
			backup("apps-daily-20261002020000", "Completed", 58*time.Hour, 0, 1),
			backup("apps-daily-20261001020000", "Deleting", 82*time.Hour, 0, 0),
		)
	case "":
		out.Backups = append(out.Backups, backup("before-upgrade", "PartiallyFailed", 2*24*time.Hour, 1, 0))
	default:
		out.Backups = append(out.Backups, backup(schedule+"-20261004000000", "Completed", 6*time.Hour, 0, 0))
	}

	return out
}
