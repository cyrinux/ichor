package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// groupVelero is Velero's API group. The reader asks v1 whatever /apis prefers: the group also
// serves v2alpha1 (DataUpload, DataDownload), which may become preferred but has no schedules,
// backups or storage locations.
const (
	groupVelero   = "velero.io"
	veleroVersion = "v1"
)

// veleroAdhocWindow is how far back failed backups taken by hand (no schedule) are shown.
const veleroAdhocWindow = 7 * 24 * time.Hour

type veleroStatus struct {
	Version   string           `json:"version"`
	Error     string           `json:"error"`
	Schedules []veleroSchedule `json:"schedules"`
	Adhoc     []veleroAdhoc    `json:"adhoc"`
	Locations []veleroLocation `json:"locations"`
}

type veleroSchedule struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"` // cron, as written
	Paused    bool   `json:"paused"`
	// Phase is the schedule's own: New, Enabled or FailedValidation.
	Phase              string   `json:"phase"`
	ValidationErrors   []string `json:"validationErrors"`
	Health             string   `json:"health"`  // critical|warning|ok|idle (paused)
	Reasons            []string `json:"reasons"` // see veleroReason*
	StorageLocation    string   `json:"storageLocation"`
	IncludedNamespaces []string `json:"includedNamespaces"` // empty or "*": every namespace
	// LastBackup is the latest finished backup (completed or not), nil when none is left.
	LastBackup    *veleroBackup `json:"lastBackup"`
	LastSuccessAt int64         `json:"lastSuccessAt"` // the latest Completed one, 0 when none
	InProgress    bool          `json:"inProgress"`
}

type veleroBackup struct {
	Name          string `json:"name"`
	Phase         string `json:"phase"` // Completed, PartiallyFailed, Failed, FailedValidation
	StartedAt     int64  `json:"startedAt"`
	CompletedAt   int64  `json:"completedAt"`
	Errors        int    `json:"errors"`
	Warnings      int    `json:"warnings"`
	FailureReason string `json:"failureReason"`
}

// veleroAdhoc is a backup taken without a schedule that failed in the last week.
type veleroAdhoc struct {
	Namespace string `json:"namespace"`
	veleroBackup
	StorageLocation string `json:"storageLocation"`
	Health          string `json:"health"` // warning
}

type veleroLocation struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Bucket          string `json:"bucket"`
	Default         bool   `json:"default"`
	Phase           string `json:"phase"` // Available|Unavailable, "" before the first check
	Message         string `json:"message"`
	LastValidatedAt int64  `json:"lastValidatedAt"`
	Health          string `json:"health"` // critical when Unavailable, else ok
}

// Reasons a schedule is not ok, for the app to word.
const (
	veleroReasonFailed   = "failed"          // the last backup failed (or failed validation)
	veleroReasonLocation = "location"        // its storage location is unavailable
	veleroReasonPartial  = "partiallyFailed" // the last backup completed with errors
	veleroReasonStale    = "stale"           // no completed backup for twice its interval
	veleroReasonInvalid  = "invalid"         // the schedule itself failed validation
)

type veleroMetadata struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Labels            map[string]string `json:"labels"`
}

type veleroScheduleObject struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		Schedule string `json:"schedule"`
		Paused   bool   `json:"paused"`
		Template struct {
			StorageLocation    string   `json:"storageLocation"`
			IncludedNamespaces []string `json:"includedNamespaces"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Phase            string   `json:"phase"`
		ValidationErrors []string `json:"validationErrors"`
		LastBackup       string   `json:"lastBackup"` // when the schedule last started one
	} `json:"status"`
}

type veleroBackupObject struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		StorageLocation string `json:"storageLocation"`
	} `json:"spec"`
	Status struct {
		Phase               string `json:"phase"`
		StartTimestamp      string `json:"startTimestamp"`
		CompletionTimestamp string `json:"completionTimestamp"`
		Errors              int    `json:"errors"`
		Warnings            int    `json:"warnings"`
		FailureReason       string `json:"failureReason"`
	} `json:"status"`
}

type veleroLocationObject struct {
	Metadata veleroMetadata `json:"metadata"`
	Spec     struct {
		Provider      string `json:"provider"`
		Default       bool   `json:"default"`
		ObjectStorage struct {
			Bucket string `json:"bucket"`
		} `json:"objectStorage"`
	} `json:"spec"`
	Status struct {
		Phase              string `json:"phase"`
		LastValidationTime string `json:"lastValidationTime"`
		Message            string `json:"message"`
	} `json:"status"`
}

// readVelero lists the schedules, the backups and the storage locations: three listings
// whatever their number. Only the latest backups are kept (see mapVelero).
func readVelero(ctx context.Context, k *kubeClient, now time.Time) *veleroStatus {
	base := "/apis/" + groupVelero + "/" + veleroVersion + "/"

	var (
		schedules kubeList[veleroScheduleObject]
		backups   kubeList[veleroBackupObject]
		locations kubeList[veleroLocationObject]
		errs      = make([]error, 3)
		wg        sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"schedules", &schedules) })
	wg.Go(func() { errs[1] = k.get(ctx, base+"backups", &backups) })
	wg.Go(func() { errs[2] = k.get(ctx, base+"backupstoragelocations", &locations) })
	wg.Wait()

	out := mapVelero(schedules.Items, backups.Items, locations.Items, now)
	out.Version = veleroVersion
	out.Error = sectionError(errs...)

	return out
}

// veleroFinished reports whether a backup phase is final; Deleting is neither final nor running.
func veleroFinished(phase string) bool {
	return slices.Contains([]string{"Completed", "PartiallyFailed", "Failed", "FailedValidation"}, phase)
}

// veleroRunning reports a backup on its way: New, InProgress, WaitingForPluginOperations(PartiallyFailed),
// Finalizing(PartiallyFailed).
func veleroRunning(phase string) bool {
	return phase == "New" || phase == "InProgress" || strings.HasPrefix(phase, "WaitingForPluginOperations") || strings.HasPrefix(phase, "Finalizing")
}

// veleroBackupAt is when a backup ended, else started, else was created: what orders them.
func veleroBackupAt(b veleroBackupObject) int64 {
	return cmp.Or(unixMilli(b.Status.CompletionTimestamp), unixMilli(b.Status.StartTimestamp), unixMilli(b.Metadata.CreationTimestamp))
}

func newVeleroBackup(b veleroBackupObject) *veleroBackup {
	return &veleroBackup{
		Name: b.Metadata.Name, Phase: b.Status.Phase,
		StartedAt: unixMilli(b.Status.StartTimestamp), CompletedAt: unixMilli(b.Status.CompletionTimestamp),
		Errors: b.Status.Errors, Warnings: b.Status.Warnings, FailureReason: b.Status.FailureReason,
	}
}

// veleroScheduleBackups is what is kept of one schedule's backups.
type veleroScheduleBackups struct {
	last          veleroBackupObject
	lastAt        int64
	lastSuccessAt int64
	inProgress    bool
}

func mapVelero(schedules []veleroScheduleObject, backups []veleroBackupObject, locations []veleroLocationObject, now time.Time) *veleroStatus {
	out := &veleroStatus{Schedules: []veleroSchedule{}, Adhoc: []veleroAdhoc{}, Locations: []veleroLocation{}}

	// Storage locations by namespace/name, and the default one of each namespace.
	locByKey := map[string]veleroLocation{}
	defaults := map[string]string{}

	for _, l := range locations {
		loc := veleroLocation{
			Namespace: l.Metadata.Namespace, Name: l.Metadata.Name, Provider: l.Spec.Provider,
			Bucket: l.Spec.ObjectStorage.Bucket, Default: l.Spec.Default, Phase: l.Status.Phase,
			Message: l.Status.Message, LastValidatedAt: unixMilli(l.Status.LastValidationTime), Health: healthOK,
		}

		if loc.Phase == "Unavailable" {
			loc.Health = healthCritical
		}

		locByKey[loc.Namespace+"/"+loc.Name] = loc
		if loc.Default {
			defaults[loc.Namespace] = loc.Name
		}

		out.Locations = append(out.Locations, loc)
	}

	// The backup list grows with every schedule run: keep the latest per schedule, and the
	// failed ones taken by hand in the last week.
	bySchedule := map[string]*veleroScheduleBackups{}
	adhocAt := map[string]int64{}

	for _, b := range backups {
		schedule := b.Metadata.Labels["velero.io/schedule-name"]
		at := veleroBackupAt(b)

		if schedule == "" {
			failed := b.Status.Phase == "Failed" || b.Status.Phase == "PartiallyFailed" || b.Status.Phase == "FailedValidation"
			if failed && now.Sub(time.UnixMilli(at)) <= veleroAdhocWindow {
				adhocAt[b.Metadata.Namespace+"/"+b.Metadata.Name] = at
				out.Adhoc = append(out.Adhoc, veleroAdhoc{
					Namespace: b.Metadata.Namespace, veleroBackup: *newVeleroBackup(b),
					StorageLocation: b.Spec.StorageLocation, Health: healthWarning,
				})
			}

			continue
		}

		key := b.Metadata.Namespace + "/" + schedule

		s := bySchedule[key]
		if s == nil {
			s = &veleroScheduleBackups{}
			bySchedule[key] = s
		}

		switch {
		case veleroRunning(b.Status.Phase):
			s.inProgress = true
		case veleroFinished(b.Status.Phase):
			if at > s.lastAt {
				s.last, s.lastAt = b, at
			}

			if b.Status.Phase == "Completed" {
				s.lastSuccessAt = max(s.lastSuccessAt, at)
			}
		}
	}

	for _, obj := range schedules {
		out.Schedules = append(out.Schedules, mapVeleroSchedule(obj, bySchedule[obj.Metadata.Namespace+"/"+obj.Metadata.Name], locByKey, defaults, now))
	}

	slices.SortFunc(out.Schedules, func(a, b veleroSchedule) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	// Newest first.
	slices.SortFunc(out.Adhoc, func(a, b veleroAdhoc) int {
		return cmp.Compare(adhocAt[b.Namespace+"/"+b.Name], adhocAt[a.Namespace+"/"+a.Name])
	})

	slices.SortFunc(out.Locations, func(a, b veleroLocation) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	return out
}

func mapVeleroSchedule(obj veleroScheduleObject, backups *veleroScheduleBackups, locations map[string]veleroLocation, defaults map[string]string, now time.Time) veleroSchedule {
	s := veleroSchedule{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Schedule: obj.Spec.Schedule, Paused: obj.Spec.Paused,
		Phase: obj.Status.Phase, ValidationErrors: obj.Status.ValidationErrors, Reasons: []string{},
		StorageLocation: obj.Spec.Template.StorageLocation, IncludedNamespaces: obj.Spec.Template.IncludedNamespaces,
	}

	if s.ValidationErrors == nil {
		s.ValidationErrors = []string{}
	}

	if s.IncludedNamespaces == nil {
		s.IncludedNamespaces = []string{}
	}

	// No location named: Velero uses the namespace's default one.
	if s.StorageLocation == "" {
		s.StorageLocation = defaults[s.Namespace]
	}

	if backups != nil {
		s.LastSuccessAt, s.InProgress = backups.lastSuccessAt, backups.inProgress
		if backups.lastAt > 0 {
			s.LastBackup = newVeleroBackup(backups.last)
		}
	}

	if s.Paused {
		s.Health = healthIdle

		return s
	}

	loc, hasLoc := locations[s.Namespace+"/"+s.StorageLocation]
	// Stale is counted from the schedule's creation, or from its last run when every backup
	// has expired since (a TTL shorter than the interval): not a missed backup.
	from := unixMilli(obj.Metadata.CreationTimestamp)
	if s.LastBackup == nil {
		from = max(from, unixMilli(obj.Status.LastBackup))
	}

	s.Health, s.Reasons = veleroScheduleHealth(s, hasLoc && loc.Health == healthCritical, from, now)

	return s
}

func veleroScheduleHealth(s veleroSchedule, locationDown bool, from int64, now time.Time) (string, []string) {
	reasons := []string{}
	critical := false

	if s.LastBackup != nil {
		switch s.LastBackup.Phase {
		case "Failed", "FailedValidation":
			reasons, critical = append(reasons, veleroReasonFailed), true
		case "PartiallyFailed":
			reasons = append(reasons, veleroReasonPartial)
		}
	}

	if locationDown {
		reasons, critical = append(reasons, veleroReasonLocation), true
	}

	if s.Phase == "FailedValidation" {
		reasons = append(reasons, veleroReasonInvalid)
	}

	// Stale: no completed backup within twice the schedule's interval. A schedule younger
	// than that has had no chance yet.
	since := s.LastSuccessAt
	if since == 0 {
		since = from
	}

	if late := 2 * cronInterval(s.Schedule); now.Sub(time.UnixMilli(since)) > late {
		reasons = append(reasons, veleroReasonStale)
	}

	switch {
	case critical:
		return healthCritical, reasons
	case len(reasons) > 0:
		return healthWarning, reasons
	default:
		return healthOK, reasons
	}
}
