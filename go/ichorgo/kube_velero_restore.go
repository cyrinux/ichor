package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Restoring a Velero backup, the way `velero restore create --from-backup` does: a preview of
// what the backup holds and of the Restores of it already there, then a Restore the server's
// restore controller carries out (its phase and item progress are followed on the preview).

var (
	errVeleroRestorePhase      = &kubeAPIError{Code: 409, Reason: "FailedValidation", Message: "the backup cannot be used as a restore source: only a Completed or PartiallyFailed backup can"}
	errVeleroLocationDown      = &kubeAPIError{Code: 409, Reason: "LocationUnavailable", Message: "the backup's storage location is unavailable"}
	errVeleroRestoreInProgress = &kubeAPIError{Code: 409, Reason: "RestoreRunning", Message: "a restore of this backup is already running"}
)

// veleroRestoreSource is what a restore reads of its Backup.
type veleroRestoreSource struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		StorageLocation          string   `json:"storageLocation"`
		IncludedNamespaces       []string `json:"includedNamespaces"`
		ExcludedNamespaces       []string `json:"excludedNamespaces"`
		IncludedResources        []string `json:"includedResources"`
		IncludeClusterResources  *bool    `json:"includeClusterResources"`
		SnapshotVolumes          *bool    `json:"snapshotVolumes"`
		DefaultVolumesToFsBackup *bool    `json:"defaultVolumesToFsBackup"`
	} `json:"spec"`
	Status struct {
		Phase                    string `json:"phase"`
		CompletionTimestamp      string `json:"completionTimestamp"`
		Expiration               string `json:"expiration"`
		Errors                   int    `json:"errors"`
		Warnings                 int    `json:"warnings"`
		VolumeSnapshotsCompleted int    `json:"volumeSnapshotsCompleted"`
		Progress                 struct {
			TotalItems    int `json:"totalItems"`
			ItemsBackedUp int `json:"itemsBackedUp"`
		} `json:"progress"`
	} `json:"status"`
}

// veleroRestoreObject is a Restore as the preview reads it.
type veleroRestoreObject struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		BackupName             string            `json:"backupName"`
		IncludedNamespaces     []string          `json:"includedNamespaces"`
		NamespaceMapping       map[string]string `json:"namespaceMapping"`
		ExistingResourcePolicy string            `json:"existingResourcePolicy"`
	} `json:"spec"`
	Status struct {
		Phase               string   `json:"phase"`
		StartTimestamp      string   `json:"startTimestamp"`
		CompletionTimestamp string   `json:"completionTimestamp"`
		Errors              int      `json:"errors"`
		Warnings            int      `json:"warnings"`
		FailureReason       string   `json:"failureReason"`
		ValidationErrors    []string `json:"validationErrors"`
		Progress            struct {
			TotalItems    int `json:"totalItems"`
			ItemsRestored int `json:"itemsRestored"`
		} `json:"progress"`
	} `json:"status"`
}

// veleroRestoreDetail is one Restore of the backup, for the preview's list and for the
// progress the apps follow after launching one (the newest is theirs).
type veleroRestoreDetail struct {
	Name                   string            `json:"name"`
	Phase                  string            `json:"phase"`
	StartedAt              int64             `json:"startedAt"`
	CompletedAt            int64             `json:"completedAt"`
	Errors                 int               `json:"errors"`
	Warnings               int               `json:"warnings"`
	FailureReason          string            `json:"failureReason"`
	ValidationErrors       []string          `json:"validationErrors"`
	ItemsRestored          int               `json:"itemsRestored"`
	TotalItems             int               `json:"totalItems"`
	NamespaceMapping       map[string]string `json:"namespaceMapping"`
	IncludedNamespaces     []string          `json:"includedNamespaces"`
	ExistingResourcePolicy string            `json:"existingResourcePolicy"`
}

// veleroRestorePreview is what restoring one backup offers: what the backup holds, whether it
// can be restored, and the Restores of it already there (newest first). The same read is what
// the apps poll after launching a restore.
type veleroRestorePreview struct {
	Namespace   string `json:"namespace"`
	Backup      string `json:"backup"`
	Phase       string `json:"phase"`
	CompletedAt int64  `json:"completedAt"`
	ExpiresAt   int64  `json:"expiresAt"` // unix ms, 0 when it never expires

	StorageLocation   string `json:"storageLocation"`
	LocationAvailable bool   `json:"locationAvailable"`

	IncludedNamespaces []string `json:"includedNamespaces"`
	ExcludedNamespaces []string `json:"excludedNamespaces"`
	IncludedResources  []string `json:"includedResources"`
	// IncludeClusterResources is the Backup's own: "true", "false" or "unset" (null).
	IncludeClusterResources  string `json:"includeClusterResources"`
	SnapshotVolumes          *bool  `json:"snapshotVolumes"`
	DefaultVolumesToFsBackup *bool  `json:"defaultVolumesToFsBackup"`

	ItemsBackedUp   int `json:"itemsBackedUp"`
	TotalItems      int `json:"totalItems"`
	VolumeSnapshots int `json:"volumeSnapshots"` // completed
	Errors          int `json:"errors"`
	Warnings        int `json:"warnings"`

	CanRestore bool `json:"canRestore"`
	// Reasons it cannot (or had better not) be restored: partiallyFailed, locationUnavailable,
	// expired, restoreRunning, allNamespaces. The spec's "warnings" name is taken by Velero's
	// warning count, so the list travels as "reasons".
	Reasons  []string              `json:"reasons"`
	Restores []veleroRestoreDetail `json:"restores"`
	Error    string                `json:"error"`
}

// KubeVeleroRestorePreview reads what restoring the Velero backup namespace/backup holds
// (os:admin): its namespaces, items and volumes, whether it can be restored, and the Restores
// of it already there, newest first (at most 20). kubeServer: see KubePods.
func KubeVeleroRestorePreview(configYAML, contextName, kubeServer, namespace, backup string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, backup = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(backup))

	if err := validateKubeName("backup", namespace, backup); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() veleroRestorePreview { return demoVeleroRestorePreview(namespace, backup, time.Now()) },
		func(ctx context.Context, k *kubeClient) (veleroRestorePreview, error) {
			return readVeleroRestorePreview(ctx, k, namespace, backup, time.Now())
		})
}

// readVeleroRestorePreview reads the Backup, its storage location and the Restores of it. The
// Backup is the read that must work; the other two only shape the answer (their failure is the
// preview's error, and an unreadable location counts as unavailable).
func readVeleroRestorePreview(ctx context.Context, k *kubeClient, namespace, backup string, now time.Time) (veleroRestorePreview, error) {
	base := veleroNamespacePath(namespace)

	var b veleroRestoreSource
	if err := k.get(ctx, base+"backups/"+url.PathEscape(backup), &b); err != nil {
		return veleroRestorePreview{}, err
	}

	var (
		restores             kubeList[veleroRestoreObject]
		locations            kubeList[veleroLocationObject]
		restoresErr, locsErr error
		wg                   sync.WaitGroup
	)

	wg.Go(func() { restoresErr = getList(ctx, k, base+"restores", &restores) })
	wg.Go(func() { locsErr = getList(ctx, k, base+"backupstoragelocations", &locations) })
	wg.Wait()

	p := veleroRestorePreviewOf(b, namespace, backup, now)
	p.LocationAvailable = veleroLocationPhase(locations.Items, b.Spec.StorageLocation) == "Available"
	p.Restores = mapVeleroRestores(restores.Items, backup)
	p.Reasons = veleroRestoreReasons(p, restores.Items, backup, now)
	p.CanRestore = veleroRestorable(p.Phase) && p.LocationAvailable && !slices.Contains(p.Reasons, "expired") && !veleroRestoreRunning(restores.Items, backup)
	p.Error = sectionError(restoresErr, locsErr)

	return p, nil
}

// veleroRestorePreviewOf maps the Backup: what it holds and when it ends.
func veleroRestorePreviewOf(b veleroRestoreSource, namespace, backup string, now time.Time) veleroRestorePreview {
	orEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}

		return s
	}

	return veleroRestorePreview{
		Namespace: namespace, Backup: backup,
		Phase:                    b.Status.Phase,
		CompletedAt:              unixMilli(b.Status.CompletionTimestamp),
		ExpiresAt:                unixMilli(b.Status.Expiration),
		StorageLocation:          b.Spec.StorageLocation,
		IncludedNamespaces:       orEmpty(b.Spec.IncludedNamespaces),
		ExcludedNamespaces:       orEmpty(b.Spec.ExcludedNamespaces),
		IncludedResources:        orEmpty(b.Spec.IncludedResources),
		IncludeClusterResources:  includeClusterResources(b.Spec.IncludeClusterResources),
		SnapshotVolumes:          b.Spec.SnapshotVolumes,
		DefaultVolumesToFsBackup: b.Spec.DefaultVolumesToFsBackup,
		ItemsBackedUp:            b.Status.Progress.ItemsBackedUp,
		TotalItems:               b.Status.Progress.TotalItems,
		VolumeSnapshots:          b.Status.VolumeSnapshotsCompleted,
		Errors:                   b.Status.Errors,
		Warnings:                 b.Status.Warnings,
		Reasons:                  []string{},
		Restores:                 []veleroRestoreDetail{},
	}
}

// veleroRestoreReasons is why the backup cannot (or had better not) be restored.
func veleroRestoreReasons(p veleroRestorePreview, restores []veleroRestoreObject, backup string, now time.Time) []string {
	reasons := []string{}

	if p.Phase == "PartiallyFailed" {
		reasons = append(reasons, "partiallyFailed")
	}

	if !p.LocationAvailable {
		reasons = append(reasons, "locationUnavailable")
	}

	if p.ExpiresAt > 0 && time.UnixMilli(p.ExpiresAt).Before(now) {
		reasons = append(reasons, "expired")
	}

	if veleroRestoreRunning(restores, backup) {
		reasons = append(reasons, "restoreRunning")
	}

	// A backup of every namespace restores every namespace: worth saying before it is asked for.
	if len(p.IncludedNamespaces) == 0 || slices.Contains(p.IncludedNamespaces, "*") {
		reasons = append(reasons, "allNamespaces")
	}

	return reasons
}

// veleroRestorable reports whether a backup phase may be a restore source: the restore
// controller takes Completed and PartiallyFailed only.
func veleroRestorable(phase string) bool {
	return phase == "Completed" || phase == "PartiallyFailed"
}

// includeClusterResources words the Backup's tri-state: "true", "false" or "unset".
func includeClusterResources(v *bool) string {
	switch {
	case v == nil:
		return "unset"
	case *v:
		return "true"
	default:
		return "false"
	}
}

// veleroLocationPhase is the phase of the named storage location, or of the namespace's
// default one when no location is named; "" when neither is there.
func veleroLocationPhase(locations []veleroLocationObject, location string) string {
	for _, l := range locations {
		if l.Metadata.Name == location || (location == "" && l.Spec.Default) {
			return l.Status.Phase
		}
	}

	return ""
}

// mapVeleroRestores keeps the Restores of one backup, newest first, at most 20.
func mapVeleroRestores(restores []veleroRestoreObject, backup string) []veleroRestoreDetail {
	orEmpty := func(s []string) []string {
		if s == nil {
			return []string{}
		}

		return s
	}

	mine := slices.DeleteFunc(slices.Clone(restores), func(r veleroRestoreObject) bool {
		return r.Spec.BackupName != backup
	})

	slices.SortFunc(mine, func(a, b veleroRestoreObject) int {
		return cmp.Or(
			cmp.Compare(unixMilli(b.Status.StartTimestamp), unixMilli(a.Status.StartTimestamp)),
			strings.Compare(b.Metadata.Name, a.Metadata.Name),
		)
	})

	out := make([]veleroRestoreDetail, 0, min(len(mine), veleroBackupsMax))
	for _, r := range mine[:min(len(mine), veleroBackupsMax)] {
		out = append(out, veleroRestoreDetail{
			Name: r.Metadata.Name, Phase: r.Status.Phase,
			StartedAt: unixMilli(r.Status.StartTimestamp), CompletedAt: unixMilli(r.Status.CompletionTimestamp),
			Errors: r.Status.Errors, Warnings: r.Status.Warnings, FailureReason: r.Status.FailureReason,
			ValidationErrors:       orEmpty(r.Status.ValidationErrors),
			ItemsRestored:          r.Status.Progress.ItemsRestored,
			TotalItems:             r.Status.Progress.TotalItems,
			NamespaceMapping:       r.Spec.NamespaceMapping,
			IncludedNamespaces:     orEmpty(r.Spec.IncludedNamespaces),
			ExistingResourcePolicy: r.Spec.ExistingResourcePolicy,
		})
	}

	return out
}

// veleroRestoreRunning reports whether a Restore of the backup is on its way.
func veleroRestoreRunning(restores []veleroRestoreObject, backup string) bool {
	return slices.ContainsFunc(restores, func(r veleroRestoreObject) bool {
		return r.Spec.BackupName == backup && veleroRunning(r.Status.Phase)
	})
}

// veleroRestoreOptions is what KubeVeleroRestore takes as its optionsJSON (gomobile passes
// strings only): which namespaces, where each goes, how existing objects are treated, and
// whether volumes are restored.
type veleroRestoreOptions struct {
	IncludedNamespaces      []string          `json:"includedNamespaces"`
	NamespaceMapping        map[string]string `json:"namespaceMapping"`
	ExistingResourcePolicy  string            `json:"existingResourcePolicy"`
	RestorePVs              *bool             `json:"restorePVs"`
	IncludeClusterResources *bool             `json:"includeClusterResources"`
}

// validate checks the options before they reach the API: every namespace name (the ones
// restored and the mapping's two sides) and the existing-resource policy.
func (o veleroRestoreOptions) validate() error {
	names := slices.Clone(o.IncludedNamespaces)
	for src, dst := range o.NamespaceMapping {
		names = append(names, src, dst)
	}

	for _, ns := range names {
		if ns == "" || !kubeNamePattern.MatchString(ns) || strings.Contains(ns, "..") {
			return fmt.Errorf("invalid Kubernetes namespace %q", ns)
		}
	}

	switch o.ExistingResourcePolicy {
	case "", "none", "update":
	default:
		return fmt.Errorf("existingResourcePolicy must be none or update, not %q", o.ExistingResourcePolicy)
	}

	return nil
}

// KubeVeleroRestore restores the Velero backup namespace/backup (os:admin), like
// `velero restore create --from-backup`: a Restore named <backup>-<UTC time> with optionsJSON
// (see veleroRestoreOptions). Refused while the backup cannot be a source (not finished, its
// storage location down) or a restore of it is running. Returns the Restore's name.
// kubeServer: see KubePods.
func KubeVeleroRestore(configYAML, contextName, kubeServer, namespace, backup, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, backup = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(backup))

	var options veleroRestoreOptions
	if raw := strings.TrimSpace(optionsJSON); raw != "" {
		if err := json.Unmarshal([]byte(raw), &options); err != nil {
			return "", fmt.Errorf("cannot read the restore options: %w", err)
		}
	}

	if err := validateKubeName("backup", namespace, backup); err != nil {
		return "", err
	}

	if err := options.validate(); err != nil {
		return "", err
	}

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "velero-restore", Namespace: namespace, Object: "Backup/" + backup, Params: compactOptions(options)})

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		name, err := restoreVeleroBackup(ctx, k, namespace, backup, options, time.Now())
		out = name

		return err
	})

	return out, err
}

// compactOptions is the options as the audit log keeps them: one line, no spaces.
func compactOptions(options veleroRestoreOptions) string {
	raw, err := json.Marshal(options)
	if err != nil {
		return ""
	}

	return string(raw)
}

func restoreVeleroBackup(ctx context.Context, k *kubeClient, namespace, backup string, options veleroRestoreOptions, now time.Time) (string, error) {
	base := veleroNamespacePath(namespace)

	var b veleroRestoreSource
	if err := k.get(ctx, base+"backups/"+url.PathEscape(backup), &b); err != nil {
		return "", err
	}

	if !veleroRestorable(b.Status.Phase) {
		return "", errVeleroRestorePhase
	}

	var locations kubeList[veleroLocationObject]
	if err := getList(ctx, k, base+"backupstoragelocations", &locations); err != nil {
		return "", err
	}

	if veleroLocationPhase(locations.Items, b.Spec.StorageLocation) != "Available" {
		return "", errVeleroLocationDown
	}

	var restores kubeList[veleroRestoreObject]
	if err := getList(ctx, k, base+"restores", &restores); err != nil {
		return "", err
	}

	if veleroRestoreRunning(restores.Items, backup) {
		return "", errVeleroRestoreInProgress
	}

	restore := map[string]any{
		"apiVersion": groupVelero + "/" + veleroVersion,
		"kind":       "Restore",
		"metadata": map[string]any{
			"name":      backup + "-" + now.UTC().Format(veleroBackupNameTime),
			"namespace": namespace,
		},
		"spec": veleroRestoreSpec(backup, options),
	}

	var created struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := k.post(ctx, base+"restores", restore, &created); err != nil {
		return "", err
	}

	return created.Metadata.Name, nil
}

// veleroRestoreSpec is the Restore's spec: the backup and the options, empty ones left out.
func veleroRestoreSpec(backup string, options veleroRestoreOptions) map[string]any {
	spec := map[string]any{"backupName": backup}

	if len(options.IncludedNamespaces) > 0 {
		spec["includedNamespaces"] = options.IncludedNamespaces
	}

	if len(options.NamespaceMapping) > 0 {
		spec["namespaceMapping"] = options.NamespaceMapping
	}

	if options.ExistingResourcePolicy != "" {
		spec["existingResourcePolicy"] = options.ExistingResourcePolicy
	}

	if options.RestorePVs != nil {
		spec["restorePVs"] = *options.RestorePVs
	}

	if options.IncludeClusterResources != nil {
		spec["includeClusterResources"] = *options.IncludeClusterResources
	}

	return spec
}

// demoVeleroRestorePreview is the demo cluster's preview: the nightly backup restores fine,
// the failed manual one does not.
func demoVeleroRestorePreview(namespace, backup string, now time.Time) veleroRestorePreview {
	at := func(ago time.Duration) int64 { return now.Add(-ago).UnixMilli() }

	p := veleroRestorePreview{
		Namespace: namespace, Backup: backup, Phase: "Completed",
		CompletedAt: at(6 * time.Hour), ExpiresAt: at(-30 * 24 * time.Hour),
		StorageLocation: "default", LocationAvailable: true,
		IncludedNamespaces:      []string{"demo", "monitoring"},
		IncludeClusterResources: "unset",
		ItemsBackedUp:           412, TotalItems: 412, VolumeSnapshots: 3,
		Reasons: []string{}, Restores: []veleroRestoreDetail{},
	}

	if backup == "before-upgrade" {
		p.Phase = "PartiallyFailed"
		p.CompletedAt = at(2 * 24 * time.Hour)
		p.Errors = 1
	}

	p.Reasons = veleroRestoreReasons(p, nil, backup, now)
	p.CanRestore = veleroRestorable(p.Phase) && p.LocationAvailable && !slices.Contains(p.Reasons, "expired")

	if p.CanRestore {
		p.Restores = append(p.Restores, veleroRestoreDetail{
			Name: backup + "-20261005090000", Phase: "Completed",
			StartedAt: at(25 * time.Hour), CompletedAt: at(25*time.Hour - 4*time.Minute),
			ValidationErrors: []string{}, NamespaceMapping: map[string]string{},
			IncludedNamespaces: []string{"demo"},
		})
	}

	return p
}
