package ichorgo

import "time"

// demoJobs are the demo CronJobs' runs as Jobs, with a one-off migration Job and a suspended
// import, so the Jobs screen matches the CronJobs screen.
func demoJobs(namespace string, now time.Time) func() kubeJobs {
	return func() kubeJobs {
		var jobs []jobRow

		for _, cj := range demoCronJobs(now) {
			for _, r := range cj.Runs {
				jobs = append(jobs, jobRow{
					Namespace: cj.Namespace, Name: r.Name, State: r.State, Owner: cj.Name, Manual: r.Manual,
					Started: r.Started, Finished: r.Finished,
				})
			}
		}

		jobs = append(jobs,
			jobRow{Namespace: "demo", Name: "db-migrate-v42", State: cronRunSucceeded,
				Started: now.Add(-26 * time.Hour).UnixMilli(), Finished: now.Add(-26*time.Hour + 41*time.Second).UnixMilli()},
			jobRow{Namespace: "demo", Name: "catalog-import", State: jobSuspended, Completions: "0/1"},
		)

		for i, j := range jobs {
			if j.Completions == "" {
				jobs[i].Completions = "0/1"
				if j.State == cronRunSucceeded {
					jobs[i].Completions = "1/1"
				}
			}

			if j.State == cronRunFailed {
				jobs[i].Reason = "BackoffLimitExceeded"
			}

			jobs[i].Duration = jobDuration(jobs[i], now)
			jobs[i].Level = jobLevel(j.State)
		}

		jobs = inNamespace(jobs, namespace, func(j jobRow) string { return j.Namespace })
		sortJobs(jobs)

		return kubeJobs{Jobs: append([]jobRow{}, jobs...)}
	}
}
