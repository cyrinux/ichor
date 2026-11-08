package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The database plugins' Go readers (go/plugin/builtin/mariadb.yaml, percona-xtradb.yaml). A
// cluster's last successful and failed backup is the latest over several objects, which no
// expression can fold: native:mariadb-backups and native:percona-backups return one synthetic
// object (ichor.dev/v1alpha1, a group no cluster serves) per cluster the backups name, with that
// cluster's namespace and name, for its row to join.

// groupMariaDB is mariadb-operator's API group (mariadbs, backups, physicalbackups).
const groupMariaDB = "k8s.mariadb.com"

// groupPercona is the API group of the Percona Operator for MySQL based on XtraDB Cluster
// (perconaxtradbclusters, one per cluster, and perconaxtradbclusterbackups, one per run).
const groupPercona = "pxc.percona.com"

// dbBackupsAPIVersion is the synthetic backup summaries' API version.
const dbBackupsAPIVersion = "ichor.dev/v1alpha1"

// backupRef names the cluster a backup is of.
type backupRef struct{ namespace, name string }

// sortedBackupRefs are the clusters of m by namespace, then name.
func sortedBackupRefs[T any](m map[backupRef]T) []backupRef {
	return slices.SortedFunc(maps.Keys(m), func(a, b backupRef) int {
		return cmp.Or(strings.Compare(a.namespace, b.namespace), strings.Compare(a.name, b.name))
	})
}

// backupSummary is the synthetic object of one cluster's backups: spec as given, the latest
// successful and failed run (unix ms, left out when none) and how many backup objects count.
func backupSummary(kind string, ref backupRef, spec map[string]any, success, failure int64, count int) map[string]any {
	status := map[string]any{"backups": count}

	if success != 0 {
		status["lastSuccessTime"] = time.UnixMilli(success).UTC().Format(time.RFC3339)
	}

	if failure != 0 {
		status["lastFailureTime"] = time.UnixMilli(failure).UTC().Format(time.RFC3339)
	}

	return map[string]any{
		"apiVersion": dbBackupsAPIVersion, "kind": kind,
		"metadata": map[string]any{"namespace": ref.namespace, "name": ref.name},
		"spec":     spec, "status": status,
	}
}

// mariadbBackupObject is a Backup (logical) or a PhysicalBackup: both have the same reference,
// schedule and Complete condition.
type mariadbBackupObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		MariaDBRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"mariaDbRef"`
		Schedule *struct {
			Cron    string `json:"cron"`
			Suspend bool   `json:"suspend"`
		} `json:"schedule"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// mariadbCronJob is the CronJob the operator runs a scheduled logical Backup with.
type mariadbCronJob struct {
	Metadata struct {
		Namespace       string `json:"namespace"`
		OwnerReferences []struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Name       string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Status struct {
		Active             []json.RawMessage `json:"active"`
		LastScheduleTime   string            `json:"lastScheduleTime"`
		LastSuccessfulTime string            `json:"lastSuccessfulTime"`
	} `json:"status"`
}

// mariadbBackups is what the backups of one cluster add up to.
type mariadbBackups struct {
	lastSuccess, lastFailure int64
	interval                 time.Duration
	schedule                 string
	count                    int
}

// readMariaDBBackups lists the Backups and PhysicalBackups in scope, plus the CronJobs of the
// scheduled logical ones (a Backup's condition only says how its first run went), and sums
// them up per MariaDB. A list that fails is the error; what the others gave still shows.
func readMariaDBBackups(ctx context.Context, k *kubeClient, p *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	version := p.groups[groupMariaDB]
	if version == "" {
		return nil, fmt.Errorf("%w %s", errPluginNotServed, groupMariaDB)
	}

	path := func(group, version, resource string) string {
		ref, _ := newResourceRef(group, version, resource)

		return ref.path(scope)
	}

	var (
		logical, physical kubeList[mariadbBackupObject]
		errs              = make([]error, 3)
		wg                sync.WaitGroup
	)

	wg.Go(func() { errs[0] = getList(ctx, k, path(groupMariaDB, version, "backups"), &logical) })
	// Physical backups came later: an operator without them answers 404.
	wg.Go(func() {
		errs[1] = ignoreNotFound(getList(ctx, k, path(groupMariaDB, version, "physicalbackups"), &physical))
	})
	wg.Wait()

	if errs[0] != nil && errs[1] != nil {
		return nil, errs[0]
	}

	var cronJobs kubeList[mariadbCronJob]
	if slices.ContainsFunc(logical.Items, func(b mariadbBackupObject) bool { return b.Spec.Schedule != nil }) {
		errs[2] = getList(ctx, k, path("batch", "v1", "cronjobs"), &cronJobs)
	}

	return mariadbBackupSummaries(logical.Items, physical.Items, cronJobs.Items), errors.Join(errs...)
}

// mariadbBackupSummaries is one MariaDBBackups object per MariaDB the backups name, sorted by
// namespace and name.
func mariadbBackupSummaries(logical, physical []mariadbBackupObject, cronJobs []mariadbCronJob) []map[string]any {
	runs := map[string]mariadbCronJob{} // namespace/Backup name
	for _, cj := range cronJobs {
		for _, o := range cj.Metadata.OwnerReferences {
			if o.Kind == "Backup" && strings.HasPrefix(o.APIVersion, groupMariaDB+"/") {
				runs[cj.Metadata.Namespace+"/"+o.Name] = cj
			}
		}
	}

	backups := map[backupRef]mariadbBackups{}
	add := func(b mariadbBackupObject, run mariadbCronJob) {
		key := backupRef{cmp.Or(b.Spec.MariaDBRef.Namespace, b.Metadata.Namespace), b.Spec.MariaDBRef.Name}
		if key.name != "" {
			backups[key] = addMariaDBBackup(backups[key], b, run)
		}
	}

	for _, b := range logical {
		add(b, runs[b.Metadata.Namespace+"/"+b.Metadata.Name])
	}

	for _, b := range physical {
		add(b, mariadbCronJob{})
	}

	out := make([]map[string]any, 0, len(backups))

	for _, key := range sortedBackupRefs(backups) {
		acc := backups[key]
		spec := map[string]any{
			"mariaDbRef": map[string]any{"namespace": key.namespace, "name": key.name},
			"schedule":   acc.schedule, "intervalSeconds": int64(acc.interval / time.Second),
		}
		out = append(out, backupSummary("MariaDBBackups", key, spec, acc.lastSuccess, acc.lastFailure, acc.count))
	}

	return out
}

// addMariaDBBackup adds one Backup or PhysicalBackup (and the CronJob of a scheduled logical one)
// to its cluster's totals.
func addMariaDBBackup(acc mariadbBackups, b mariadbBackupObject, run mariadbCronJob) mariadbBackups {
	acc.count++

	for _, c := range b.Status.Conditions {
		if c.Type != "Complete" {
			continue
		}

		// The reason first: a failed Job leaves Complete True with reason JobFailed.
		switch t := unixMilli(c.LastTransitionTime); {
		case strings.Contains(strings.ToLower(c.Reason), "fail"): // JobFailed, CronJobFailed
			acc.lastFailure = max(acc.lastFailure, t)
		case c.Status == "True":
			acc.lastSuccess = max(acc.lastSuccess, t)
		}
	}

	// A scheduled run's outcome: the condition's transition time stays put while runs succeed.
	success, scheduled := unixMilli(run.Status.LastSuccessfulTime), unixMilli(run.Status.LastScheduleTime)
	acc.lastSuccess = max(acc.lastSuccess, success)

	if len(run.Status.Active) == 0 && scheduled > success {
		acc.lastFailure = max(acc.lastFailure, scheduled)
	}

	// The most frequent active schedule: it says when a backup is late.
	if s := b.Spec.Schedule; s != nil && !s.Suspend && s.Cron != "" {
		if d := cronInterval(s.Cron); acc.interval == 0 || d < acc.interval {
			acc.interval, acc.schedule = d, s.Cron
		}
	}

	return acc
}

type perconaBackupObject struct {
	Metadata struct {
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		PXCCluster string `json:"pxcCluster"`
	} `json:"spec"`
	Status struct {
		State     string `json:"state"` // Starting, Running, Succeeded, Failed...
		Completed string `json:"completed"`
	} `json:"status"`
}

// readPerconaBackups lists the PerconaXtraDBClusterBackups in scope and sums them up per
// cluster.
func readPerconaBackups(ctx context.Context, k *kubeClient, p *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	version := p.groups[groupPercona]
	if version == "" {
		return nil, fmt.Errorf("%w %s", errPluginNotServed, groupPercona)
	}

	ref, err := newResourceRef(groupPercona, version, "perconaxtradbclusterbackups")
	if err != nil {
		return nil, err
	}

	var backups kubeList[perconaBackupObject]
	if err := getList(ctx, k, ref.path(scope), &backups); err != nil {
		return nil, err
	}

	return perconaBackupSummaries(backups.Items), nil
}

// perconaBackupSummaries is one PerconaXtraDBBackups object per cluster the backups name,
// sorted by namespace and name: its latest successful and failed run. A failed run may never
// complete: its creation dates it then.
func perconaBackupSummaries(backups []perconaBackupObject) []map[string]any {
	type totals struct {
		success, failure int64
		count            int
	}

	byCluster := map[backupRef]totals{}

	for _, b := range backups {
		key := backupRef{b.Metadata.Namespace, b.Spec.PXCCluster}
		if key.name == "" {
			continue
		}

		at := unixMilli(b.Status.Completed)
		if at == 0 {
			at = unixMilli(b.Metadata.CreationTimestamp)
		}

		t := byCluster[key]
		t.count++

		switch b.Status.State {
		case "Succeeded":
			t.success = max(t.success, at)
		case "Failed":
			t.failure = max(t.failure, at)
		}

		byCluster[key] = t
	}

	out := make([]map[string]any, 0, len(byCluster))

	for _, key := range sortedBackupRefs(byCluster) {
		t := byCluster[key]
		out = append(out, backupSummary("PerconaXtraDBBackups", key, map[string]any{"pxcCluster": key.name}, t.success, t.failure, t.count))
	}

	return out
}
