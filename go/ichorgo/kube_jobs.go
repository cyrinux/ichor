package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The Jobs screen: each Job with its state, how long it ran, its completions and the
// CronJob that started it, failures first. Deleting one goes through the object screen's
// generic delete.

// jobSuspended is a Job whose spec.suspend holds it: it runs no pod until resumed.
const jobSuspended = "suspended"

// kubeJobs is KubeJobs' answer.
type kubeJobs struct {
	Jobs []jobRow `json:"jobs"`
}

// jobRow is a Job as the screen shows it. State: running, succeeded, failed (the cronRun*
// states) or suspended. Started and Finished in unix ms (Finished 0 while running), Duration
// in ms up to the end, or up to the read while it runs. Completions: kubectl's "1/1".
type jobRow struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Owner       string `json:"owner,omitempty"` // the CronJob that created it
	Manual      bool   `json:"manual"`          // started by hand from its CronJob
	Started     int64  `json:"started"`
	Finished    int64  `json:"finished"`
	Duration    int64  `json:"duration"`
	Completions string `json:"completions"`
	// Reason is why a failed Job stopped (BackoffLimitExceeded, DeadlineExceeded...).
	Reason string `json:"reason,omitempty"`
	// Level is critical (failed), warning (suspended) or ok.
	Level string `json:"level"`
}

type jobListObject struct {
	jobObject

	Spec struct {
		Completions *int32 `json:"completions"`
		Suspend     *bool  `json:"suspend"`
	} `json:"spec"`
}

// KubeJobs lists the Jobs of namespace ("" for all), failures first, then running ones,
// newest first, as a JSON kubeJobs.
func KubeJobs(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoJobs(namespace, time.Now()), func(ctx context.Context, k *kubeClient) (kubeJobs, error) {
		return readJobs(ctx, k, namespace, time.Now())
	})
}

func readJobs(ctx context.Context, k *kubeClient, namespace string, now time.Time) (kubeJobs, error) {
	jobs, err := listObjects[jobListObject](ctx, k, scopedPath("/apis/batch/v1", namespace, "jobs"))
	if err != nil {
		return kubeJobs{}, err
	}

	privacy.learnNamespaces(namespacesOf(jobs, func(j jobListObject) string { return j.Metadata.Namespace }))

	rows := make([]jobRow, 0, len(jobs))
	for _, j := range jobs {
		rows = append(rows, mapJob(j, now))
	}

	sortJobs(rows)

	return kubeJobs{Jobs: rows}, nil
}

func mapJob(j jobListObject, now time.Time) jobRow {
	run := mapJobRun(j.jobObject)
	row := jobRow{
		Namespace: j.Metadata.Namespace, Name: j.Metadata.Name, State: run.State, Owner: j.cronOwner(),
		Manual: run.Manual, Started: run.Started, Finished: run.Finished,
	}

	if row.State == cronRunRunning && j.Spec.Suspend != nil && *j.Spec.Suspend {
		row.State = jobSuspended
	}

	wanted := int32(1)
	if j.Spec.Completions != nil {
		wanted = *j.Spec.Completions
	}

	row.Completions = strconv.Itoa(int(j.Status.Succeeded)) + "/" + strconv.Itoa(int(wanted))
	row.Duration = jobDuration(row, now)

	for _, c := range j.Status.Conditions {
		if c.Type == "Failed" && c.Status == "True" {
			row.Reason = c.Reason
		}
	}

	row.Level = jobLevel(row.State)

	return row
}

// jobDuration is how long the Job ran: to its end, or to now while it runs. 0 before it starts.
func jobDuration(row jobRow, now time.Time) int64 {
	switch {
	case row.Started == 0:
		return 0
	case row.Finished > 0:
		return max(row.Finished-row.Started, 0)
	case row.State == cronRunRunning:
		return max(now.UnixMilli()-row.Started, 0)
	default:
		return 0
	}
}

func jobLevel(state string) string {
	switch state {
	case cronRunFailed:
		return storageCritical
	case jobSuspended:
		return storageWarning
	default:
		return storageOK
	}
}

// sortJobs puts failures first, then suspended and running Jobs, each newest first.
func sortJobs(jobs []jobRow) {
	rank := func(j jobRow) int {
		if j.State == cronRunRunning {
			return 1 // between the suspended (warning) and the finished ones
		}

		return 2 * levelRank(j.Level)
	}

	slices.SortFunc(jobs, func(a, b jobRow) int {
		return cmp.Or(
			cmp.Compare(rank(b), rank(a)),
			cmp.Compare(b.Started, a.Started),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
