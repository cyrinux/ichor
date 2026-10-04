package ichorgo

import "time"

// demoCronJobs are the CronJobs of the built-in demo cluster, with recent runs around now.
func demoCronJobs(now time.Time) []kubeCronJob {
	run := func(name, state string, manual bool, ago, took time.Duration) kubeJobRun {
		r := kubeJobRun{Name: name, State: state, Manual: manual, Started: now.Add(-ago).UnixMilli()}
		if state != cronRunRunning {
			r.Finished = now.Add(-ago + took).UnixMilli()
		}

		return r
	}

	return []kubeCronJob{
		demoCronJob(now, kubeCronJob{
			Namespace: "demo", Name: "db-backup", Title: "Database backup", Description: "Dumps postgres to object storage",
			Icon: "postgresql", Schedule: "0 3 * * *", TimeZone: "UTC", Triggerable: true, Images: []string{"postgres:17"},
			Runs: []kubeJobRun{
				run("db-backup-manual-x7k2p", cronRunSucceeded, true, 2*time.Hour, 94*time.Second),
				run("db-backup-29331540", cronRunSucceeded, false, 9*time.Hour, 87*time.Second),
				run("db-backup-29330100", cronRunSucceeded, false, 33*time.Hour, 91*time.Second),
			},
		}),
		demoCronJob(now, kubeCronJob{
			Namespace: "demo", Name: "report-mailer", Title: "Weekly report", Schedule: "0 8 * * MON",
			Triggerable: true, Images: []string{"busybox:1.37"},
			Runs: []kubeJobRun{run("report-mailer-29328960", cronRunFailed, false, 5*24*time.Hour, 12*time.Second)},
		}),
		demoCronJob(now, kubeCronJob{
			Namespace: "demo", Name: "wipe-staging", Description: "Runs on schedule only: manual runs are disabled",
			Schedule: "0 0 1 * *", Images: []string{"busybox:1.37"},
		}),
		demoCronJob(now, kubeCronJob{
			Namespace: "longhorn-system", Name: "snapshot-cleanup", Icon: "longhorn", Schedule: "*/30 * * * *",
			Triggerable: true, Images: []string{"longhornio/longhorn-manager:v1.9.0"},
			Runs: []kubeJobRun{run("snapshot-cleanup-29331600", cronRunRunning, false, 40*time.Second, 0)},
		}),
		demoCronJob(now, kubeCronJob{
			Namespace: "velero", Name: "nightly-backup", Icon: "velero", Schedule: "30 1 * * *", Suspended: true,
			Triggerable: true, Images: []string{"velero/velero:v1.16.0"},
		}),
	}
}

// demoCronJob fills in what the API server would report from cj's runs.
func demoCronJob(now time.Time, cj kubeCronJob) kubeCronJob {
	cj.Created = demoBoot.UnixMilli()
	if !cj.Suspended {
		cj.NextRun = cronNextRun(cj.Schedule, cj.TimeZone, now)
	}

	for _, r := range cj.Runs {
		if r.State == cronRunRunning {
			cj.Active++
		}

		if !r.Manual && cj.LastSchedule == 0 {
			cj.LastSchedule = r.Started
		}

		if r.State == cronRunSucceeded && cj.LastSuccess == 0 {
			cj.LastSuccess = r.Finished
		}
	}

	if cj.Runs == nil {
		cj.Runs = []kubeJobRun{}
	}

	cj.State = cronJobState(cj)

	return cj
}
