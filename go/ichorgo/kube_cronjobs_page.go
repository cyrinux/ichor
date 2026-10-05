package ichorgo

import (
	"context"
	"time"
)

// kubeCronJobPage is one page of CronJobs with their recent runs, in the API server's order.
type kubeCronJobPage struct {
	CronJobs []kubeCronJob `json:"cronJobs"`
	pageCursor
}

// KubeCronJobsPage lists one page of the CronJobs of namespace ("" for every namespace)
// with their recent runs, for the apps to load a large cluster page by page (os:admin):
// {"cronJobs":[...as KubeCronJobs],"continue","remaining","complete"}. continueToken: the
// previous page's "continue", "" for the first page; limit 0 is 500. kubeServer: see KubePods.
//
// Full objects, never a Table: the time zone, the icon labels, the template's images and the
// last success are not among the Table's columns, nor are a Job's conditions. The Jobs of the
// same scope are read page by page for each page of CronJobs, keeping only the runs of that
// page: a scope rarely holds more than one page of CronJobs.
func KubeCronJobsPage(configYAML, contextName, kubeServer, namespace, continueToken string, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	args, err := newPageArgs(namespace, continueToken, limit)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeCronJobPage {
			rows := inNamespace(demoCronJobs(time.Now()), args.namespace, func(c kubeCronJob) string { return c.Namespace })

			return kubeCronJobPage{CronJobs: rows, pageCursor: completeCursor}
		},
		func(ctx context.Context, k *kubeClient) (kubeCronJobPage, error) {
			return listCronJobsPage(ctx, k, args.namespace, pageQuery{limit: args.limit, continueToken: args.continueToken}, time.Now())
		})
}

func listCronJobsPage(ctx context.Context, k *kubeClient, namespace string, q pageQuery, now time.Time) (kubeCronJobPage, error) {
	page, err := k.getPage(ctx, scopedPath("/apis/batch/v1", namespace, "cronjobs"), q)
	if err != nil {
		return kubeCronJobPage{}, err
	}

	crons, err := decodeItems[cronJobObject](page)
	if err != nil {
		return kubeCronJobPage{}, err
	}

	runs := map[string][]kubeJobRun{}

	if len(crons) > 0 {
		wanted := map[string]bool{}
		for _, c := range crons {
			wanted[c.Metadata.Namespace+"/"+c.Metadata.Name] = true
		}

		if runs, err = listCronRuns(ctx, k, namespace, func(key string) bool { return wanted[key] }); err != nil {
			return kubeCronJobPage{}, err
		}
	}

	out := make([]kubeCronJob, 0, len(crons))
	for _, c := range crons {
		out = append(out, mapCronJob(c, runs[c.Metadata.Namespace+"/"+c.Metadata.Name], now))
	}

	return kubeCronJobPage{CronJobs: out, pageCursor: cursorOf(page)}, nil
}

// listCronRuns reads the Jobs of namespace ("" for every namespace) page by page and keeps
// the runs of the CronJobs keep accepts, by "namespace/cronjob".
func listCronRuns(ctx context.Context, k *kubeClient, namespace string, keep func(string) bool) (map[string][]kubeJobRun, error) {
	runs := map[string][]kubeJobRun{}

	err := k.listAll(ctx, scopedPath("/apis/batch/v1", namespace, "jobs"), pageQuery{}, func() { clear(runs) }, func(page kubePage) error {
		jobs, err := decodeItems[jobObject](page)

		for _, j := range jobs {
			owner := j.cronOwner()
			if key := j.Metadata.Namespace + "/" + owner; owner != "" && keep(key) {
				runs[key] = append(runs[key], mapJobRun(j))
			}
		}

		return err
	})

	return runs, err
}
