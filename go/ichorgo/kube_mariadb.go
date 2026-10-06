package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

// groupMariaDB is mariadb-operator's API group (mariadbs, backups, physicalbackups).
const groupMariaDB = "k8s.mariadb.com"

type mariadbStatus struct {
	Version  string           `json:"version"`
	Error    string           `json:"error"`
	Clusters []mariadbCluster `json:"clusters"`
}

type mariadbCluster struct {
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	Topology  string   `json:"topology"` // standalone|replication|galera
	Health    string   `json:"health"`   // critical|warning|ok|idle (suspended)
	Reasons   []string `json:"reasons"`  // see mariadbReason*
	Suspended bool     `json:"suspended"`
	// Message is the operator's Ready condition message when it is not True.
	Message   string       `json:"message"`
	Replicas  int          `json:"replicas"`
	ReadyPods int          `json:"readyPods"`
	Primary   string       `json:"primary"` // status.currentPrimary, "" when none
	Pods      []mariadbPod `json:"pods"`
	// Last successful and failed backup (logical or physical), unix ms, 0 when none.
	LastBackupAt       int64  `json:"lastBackupAt"`
	LastBackupFailedAt int64  `json:"lastBackupFailedAt"`
	BackupSchedule     string `json:"backupSchedule"` // the most frequent active cron, "" when none
}

type mariadbPod struct {
	Name  string `json:"name"`
	Node  string `json:"node"`
	Phase string `json:"phase"`
	Role  string `json:"role"` // primary, replica or member (Galera), "" when unknown
	Ready bool   `json:"ready"`
}

// Reasons a MariaDB cluster is not ok, for the app to word.
const (
	mariadbReasonNoReady        = "noReady"        // no pod ready: the database is down
	mariadbReasonNoPrimary      = "noPrimary"      // replication without a ready primary
	mariadbReasonPods           = "pods"           // fewer ready pods than replicas
	mariadbReasonGaleraRecovery = "galeraRecovery" // the operator is recovering the Galera cluster
	mariadbReasonBackupFailed   = "backupFailed"   // the latest backup attempt failed
	mariadbReasonBackupStale    = "backupStale"    // no successful backup for twice its schedule
	mariadbReasonNotReady       = "notReady"       // the Ready condition is False, see Message
)

type mariadbObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
		Suspend  bool `json:"suspend"`
		Galera   *struct {
			Enabled bool `json:"enabled"`
		} `json:"galera"`
		Replication *struct {
			Enabled bool `json:"enabled"`
		} `json:"replication"`
	} `json:"spec"`
	Status struct {
		Conditions     []kubeCondition `json:"conditions"`
		CurrentPrimary string          `json:"currentPrimary"`
		GaleraRecovery json.RawMessage `json:"galeraRecovery"`
	} `json:"status"`
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

// mariadbSources is what mapMariaDB reads.
type mariadbSources struct {
	mariadbs []mariadbObject
	logical  []mariadbBackupObject
	physical []mariadbBackupObject
	cronJobs []mariadbCronJob // of the scheduled logical backups
	pods     []dsPod
}

// readMariaDB lists the MariaDB clusters, their backups and their pods (labelled by the operator
// with app.kubernetes.io/name=mariadb and app.kubernetes.io/instance=<cluster>), plus the
// CronJobs of scheduled logical backups: a Backup's condition only says how its last run went.
func readMariaDB(ctx context.Context, k *kubeClient, version string, now time.Time) *mariadbStatus {
	base := "/apis/" + groupMariaDB + "/" + version + "/"

	var (
		mariadbs kubeList[mariadbObject]
		logical  kubeList[mariadbBackupObject]
		physical kubeList[mariadbBackupObject]
		pods     []dsPod
		errs     = make([]error, 5)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = getList(ctx, k, base+"mariadbs", &mariadbs) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"backups", &logical) })
	// Physical backups came later: an operator without them answers 404.
	wg.Go(func() { errs[2] = ignoreNotFound(getList(ctx, k, base+"physicalbackups", &physical)) })
	wg.Go(func() { pods, errs[3] = listDSPods(ctx, k, "app.kubernetes.io/name=mariadb") })
	wg.Wait()

	var cronJobs kubeList[mariadbCronJob]
	if slices.ContainsFunc(logical.Items, func(b mariadbBackupObject) bool { return b.Spec.Schedule != nil }) {
		errs[4] = getList(ctx, k, "/apis/batch/v1/cronjobs", &cronJobs)
	}

	out := mapMariaDB(mariadbSources{
		mariadbs: mariadbs.Items, logical: logical.Items, physical: physical.Items, cronJobs: cronJobs.Items, pods: pods,
	}, now)
	out.Version = version
	out.Error = sectionError(errs...)

	return out
}

// mariadbBackups is what the backups of one cluster add up to.
type mariadbBackups struct {
	lastSuccess, lastFailure int64
	interval                 time.Duration
	schedule                 string
}

func mapMariaDB(src mariadbSources, now time.Time) *mariadbStatus {
	out := &mariadbStatus{Clusters: []mariadbCluster{}}

	runs := map[string]mariadbCronJob{} // namespace/Backup name
	for _, cj := range src.cronJobs {
		for _, o := range cj.Metadata.OwnerReferences {
			if o.Kind == "Backup" && strings.HasPrefix(o.APIVersion, groupMariaDB+"/") {
				runs[cj.Metadata.Namespace+"/"+o.Name] = cj
			}
		}
	}

	backups := map[string]mariadbBackups{}
	add := func(b mariadbBackupObject, run mariadbCronJob) {
		ns := b.Spec.MariaDBRef.Namespace
		if ns == "" {
			ns = b.Metadata.Namespace
		}

		key := ns + "/" + b.Spec.MariaDBRef.Name
		backups[key] = addMariaDBBackup(backups[key], b, run)
	}

	for _, b := range src.logical {
		add(b, runs[b.Metadata.Namespace+"/"+b.Metadata.Name])
	}

	for _, b := range src.physical {
		add(b, mariadbCronJob{})
	}

	byCluster := map[string][]mariadbPod{}

	for _, p := range src.pods {
		name := p.Metadata.Labels["app.kubernetes.io/instance"]
		// The StatefulSet's pods only (<cluster>-<ordinal>), not the operator's Jobs.
		if ordinal, ok := strings.CutPrefix(p.Metadata.Name, name+"-"); name == "" || !ok || !isDigits(ordinal) {
			continue
		}

		key := p.Metadata.Namespace + "/" + name
		byCluster[key] = append(byCluster[key], mariadbPod{
			Name: p.Metadata.Name, Node: p.Spec.NodeName, Phase: p.Status.Phase, Ready: p.containerReady(""),
		})
	}

	for _, obj := range src.mariadbs {
		key := obj.Metadata.Namespace + "/" + obj.Metadata.Name
		out.Clusters = append(out.Clusters, mapMariaDBCluster(obj, byCluster[key], backups[key], now))
	}

	slices.SortFunc(out.Clusters, byHealthThenKey(func(x mariadbCluster) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))

	return out
}

// addMariaDBBackup adds one Backup or PhysicalBackup (and the CronJob of a scheduled logical one)
// to its cluster's totals.
func addMariaDBBackup(acc mariadbBackups, b mariadbBackupObject, run mariadbCronJob) mariadbBackups {
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

	if s := b.Spec.Schedule; s != nil && !s.Suspend && s.Cron != "" {
		if d := cronInterval(s.Cron); acc.interval == 0 || d < acc.interval {
			acc.interval, acc.schedule = d, s.Cron
		}
	}

	return acc
}

func mapMariaDBCluster(obj mariadbObject, pods []mariadbPod, backups mariadbBackups, now time.Time) mariadbCluster {
	st := obj.Status
	c := mariadbCluster{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Topology: "standalone",
		Suspended: obj.Spec.Suspend, Replicas: 1, Primary: st.CurrentPrimary, Pods: pods, Reasons: []string{},
		LastBackupAt: backups.lastSuccess, LastBackupFailedAt: backups.lastFailure, BackupSchedule: backups.schedule,
	}

	switch {
	case obj.Spec.Galera != nil && obj.Spec.Galera.Enabled:
		c.Topology = "galera"
	case obj.Spec.Replication != nil && obj.Spec.Replication.Enabled:
		c.Topology = "replication"
	}

	if obj.Spec.Replicas != nil {
		c.Replicas = *obj.Spec.Replicas
	}

	if c.Pods == nil {
		c.Pods = []mariadbPod{}
	}

	primaryReady := false

	for i, p := range c.Pods {
		switch {
		case p.Name == st.CurrentPrimary:
			c.Pods[i].Role, primaryReady = "primary", p.Ready
		case c.Topology == "galera":
			c.Pods[i].Role = "member"
		case c.Topology == "replication":
			c.Pods[i].Role = "replica"
		}

		if p.Ready {
			c.ReadyPods++
		}
	}

	// The primary first, then by name.
	slices.SortFunc(c.Pods, func(a, b mariadbPod) int {
		if (a.Role == "primary") != (b.Role == "primary") {
			if a.Role == "primary" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})

	ready := readyCondition(st.Conditions)

	if ready.Status != "" && ready.Status != "True" {
		c.Message = ready.Message
	}

	// A suspended cluster is left alone by the operator on purpose: idle, not down.
	if c.Suspended {
		c.Health = healthIdle

		return c
	}

	recovering := len(st.GaleraRecovery) > 0 && string(st.GaleraRecovery) != "null"
	c.Health, c.Reasons = mariadbHealth(c, mariadbSignals{
		primaryReady: primaryReady, recovering: recovering, notReady: ready.Status == "False",
		backupFailed: backups.lastFailure > backups.lastSuccess,
		backupStale:  backups.interval > 0 && (backups.lastSuccess == 0 || now.Sub(time.UnixMilli(backups.lastSuccess)) > 2*backups.interval),
	})

	return c
}

// mariadbSignals are the facts mariadbHealth weighs besides the cluster's counts.
type mariadbSignals struct {
	primaryReady, recovering, notReady, backupFailed, backupStale bool
}

func mariadbHealth(c mariadbCluster, s mariadbSignals) (string, []string) {
	reasons := []string{}
	critical := false

	switch {
	case c.Replicas > 0 && c.ReadyPods == 0:
		reasons, critical = append(reasons, mariadbReasonNoReady), true
	case c.Topology == "replication" && c.Replicas > 0 && !s.primaryReady:
		reasons, critical = append(reasons, mariadbReasonNoPrimary), true
	}

	if c.ReadyPods > 0 && c.ReadyPods < c.Replicas {
		reasons = append(reasons, mariadbReasonPods)
	}

	if s.recovering {
		reasons = append(reasons, mariadbReasonGaleraRecovery)
	}

	if s.notReady && len(reasons) == 0 {
		reasons = append(reasons, mariadbReasonNotReady)
	}

	switch {
	case s.backupFailed:
		reasons = append(reasons, mariadbReasonBackupFailed)
	case s.backupStale:
		reasons = append(reasons, mariadbReasonBackupStale)
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
