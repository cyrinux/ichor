package ichorgo

import (
	"context"
	"slices"
	"strings"
	"time"
)

type backupJobStatus struct {
	Error     string          `json:"error"`
	JobsError string          `json:"jobsError"`
	Jobs      []backupCronJob `json:"jobs"`
}
type backupCronJob struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	Schedule      string `json:"schedule"`
	Suspended     bool   `json:"suspended"`
	LastSuccess   int64  `json:"lastSuccess"`
	AgeSeconds    int64  `json:"ageSeconds"`
	FailureStreak int    `json:"failureStreak"`
	Health        string `json:"health"`
}

func isBackupCronJob(c cronJobObject) bool {
	switch strings.ToLower(c.Metadata.meta(backupMatchKey)) {
	case "true":
		return true
	case "false":
		return false
	}
	return strings.Contains(strings.ToLower(c.Metadata.Name), "backup")
}
func backupFailureStreak(runs []kubeJobRun, lastSuccess int64) int {
	runs = slices.Clone(runs)
	slices.SortFunc(runs, func(a, b kubeJobRun) int {
		if max(a.Finished, a.Started) > max(b.Finished, b.Started) {
			return -1
		}
		if max(a.Finished, a.Started) < max(b.Finished, b.Started) {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	failures := 0
	for _, r := range runs {
		if r.State == cronRunRunning {
			continue
		}
		if r.State == cronRunSucceeded || max(r.Finished, r.Started) <= lastSuccess {
			break
		}
		if r.State == cronRunFailed {
			failures++
		}
	}
	return failures
}
func readBackupJobs(ctx context.Context, k *kubeClient, now time.Time) *backupJobStatus {
	r := &backupJobStatus{Jobs: []backupCronJob{}}
	crons, err := pagedSnapshotObjects[cronJobObject](ctx, k, "/apis/batch/v1/cronjobs")
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		r.Error = err.Error()
		return r
	}
	wanted := map[string]bool{}
	for _, c := range crons {
		if isBackupCronJob(c) {
			wanted[c.Metadata.Namespace+"/"+c.Metadata.Name] = true
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	runs, err := listCronRuns(ctx, k, "", func(key string) bool { return wanted[key] })
	if err != nil {
		r.JobsError = err.Error()
	}
	for _, c := range crons {
		key := c.Metadata.Namespace + "/" + c.Metadata.Name
		if !wanted[key] {
			continue
		}
		mapped := mapCronJob(c, nil, now)
		row := backupCronJob{Namespace: mapped.Namespace, Name: mapped.Name, Schedule: mapped.Schedule, Suspended: mapped.Suspended, LastSuccess: mapped.LastSuccess, Health: healthOK}
		for _, run := range runs[key] {
			if run.State == cronRunSucceeded {
				row.LastSuccess = max(row.LastSuccess, run.Finished)
			}
		}
		if err == nil {
			row.FailureStreak = backupFailureStreak(runs[key], row.LastSuccess)
		}
		row.AgeSeconds = backupAge(row.LastSuccess, now)
		if row.FailureStreak > 0 {
			row.Health = healthWarning
		}
		if row.FailureStreak >= 3 {
			row.Health = healthCritical
		}
		if row.Suspended {
			row.Health = healthIdle
		}
		r.Jobs = append(r.Jobs, row)
	}
	slices.SortFunc(r.Jobs, byHealthThenKey(func(c backupCronJob) (string, string) { return c.Health, c.Namespace + "/" + c.Name }))
	return r
}
