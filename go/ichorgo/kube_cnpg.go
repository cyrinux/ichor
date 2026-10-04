package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

// barmanCloudPlugin is the CNPG-I plugin that backs clusters up to object storage.
const barmanCloudPlugin = "barman-cloud.cloudnative-pg.io"

type cnpgStatus struct {
	Version  string        `json:"version"`
	Error    string        `json:"error"`
	Clusters []cnpgCluster `json:"clusters"`
}

type cnpgCluster struct {
	Namespace      string    `json:"namespace"`
	Name           string    `json:"name"`
	Phase          string    `json:"phase"`
	PhaseReason    string    `json:"phaseReason"`
	Health         string    `json:"health"` // critical|warning|ok|idle (hibernated)
	Hibernated     bool      `json:"hibernated"`
	Reasons        []string  `json:"reasons"` // why it is not ok, see cnpgReason*
	Instances      int       `json:"instances"`
	ReadyInstances int       `json:"readyInstances"`
	CurrentPrimary string    `json:"currentPrimary"`
	TargetPrimary  string    `json:"targetPrimary"`
	InstancePods   []cnpgPod `json:"instancePods"`
	Archiving      string    `json:"archiving"`    // ok|failing|off|unknown
	LastBackup     string    `json:"lastBackup"`   // ok|failed|stale|none
	BackupMethod   string    `json:"backupMethod"` // plugin|in-tree|none
	ObjectStore    string    `json:"objectStore"`  // plugin ObjectStore name
	Scheduled      bool      `json:"scheduled"`    // a ScheduledBackup that is not suspended targets it
	LastSuccessAt  int64     `json:"lastSuccessfulBackupAt"`
	LastFailureAt  int64     `json:"lastFailedBackupAt"`
	RecoverableAt  int64     `json:"firstRecoverabilityAt"`
}

type cnpgPod struct {
	Name  string `json:"name"`
	Node  string `json:"node"`  // "" while Pending: not scheduled anywhere
	Phase string `json:"phase"` // Running, Pending...
	Role  string `json:"role"`  // primary|replica, "" when not running
	Ready bool   `json:"ready"`
}

// Reasons a cluster is not ok, for the app to word.
const (
	cnpgReasonNoInstance   = "noInstance"   // no instance ready: the database is down
	cnpgReasonFailover     = "failover"     // the operator is promoting a replica
	cnpgReasonInstances    = "instances"    // fewer ready instances than wanted
	cnpgReasonSwitchover   = "switchover"   // current and target primary differ
	cnpgReasonNotReady     = "notReady"     // the Ready condition is not True
	cnpgReasonArchiving    = "archiving"    // WAL archiving fails
	cnpgReasonBackupFailed = "backupFailed" // the last backup failed
	cnpgReasonBackupStale  = "backupStale"  // no successful backup for longer than its schedule
)

type cnpgClusterObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Instances int `json:"instances"`
		Plugins   []struct {
			Name          string            `json:"name"`
			Enabled       *bool             `json:"enabled"`
			IsWALArchiver *bool             `json:"isWALArchiver"`
			Parameters    map[string]string `json:"parameters"`
		} `json:"plugins"`
		Backup struct {
			BarmanObjectStore json.RawMessage `json:"barmanObjectStore"`
		} `json:"backup"`
	} `json:"spec"`
	Status struct {
		Instances                int             `json:"instances"`
		ReadyInstances           int             `json:"readyInstances"`
		Phase                    string          `json:"phase"`
		PhaseReason              string          `json:"phaseReason"`
		CurrentPrimary           string          `json:"currentPrimary"`
		TargetPrimary            string          `json:"targetPrimary"`
		LastSuccessfulBackup     string          `json:"lastSuccessfulBackup"`
		LastFailedBackup         string          `json:"lastFailedBackup"`
		FirstRecoverabilityPoint string          `json:"firstRecoverabilityPoint"`
		Conditions               []kubeCondition `json:"conditions"`
	} `json:"status"`
}

type cnpgObjectStoreObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		ServerRecoveryWindow map[string]cnpgRecoveryWindow `json:"serverRecoveryWindow"`
	} `json:"status"`
}

type cnpgRecoveryWindow struct {
	FirstRecoverabilityPoint string `json:"firstRecoverabilityPoint"`
	LastSuccessfulBackupTime string `json:"lastSuccessfulBackupTime"`
	LastFailedBackupTime     string `json:"lastFailedBackupTime"`
}

type cnpgScheduledBackupObject struct {
	Metadata struct {
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Schedule string `json:"schedule"`
		Suspend  *bool  `json:"suspend"`
		Cluster  struct {
			Name string `json:"name"`
		} `json:"cluster"`
	} `json:"spec"`
}

// cnpgSources is what readCNPG needs besides the client.
type cnpgSources struct {
	version       string // postgresql.cnpg.io
	pluginVersion string // barmancloud.cnpg.io, "" without the plugin
	pods          []dsPod
	podsErr       error
}

// readCNPG lists the clusters, their scheduled backups and (with the barman-cloud plugin)
// their object stores: three listings whatever the number of clusters.
func readCNPG(ctx context.Context, k *kubeClient, src cnpgSources, now time.Time) *cnpgStatus {
	base := "/apis/" + groupCNPG + "/" + src.version + "/"

	var (
		clusters  kubeList[cnpgClusterObject]
		scheduled kubeList[cnpgScheduledBackupObject]
		stores    kubeList[cnpgObjectStoreObject]
		errs      = make([]error, 3)
		wg        sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"clusters", &clusters) })
	wg.Go(func() { errs[1] = k.get(ctx, base+"scheduledbackups", &scheduled) })

	if src.pluginVersion != "" {
		wg.Go(func() { errs[2] = k.get(ctx, "/apis/"+groupBarmanPlug+"/"+src.pluginVersion+"/objectstores", &stores) })
	}

	wg.Wait()

	out := mapCNPG(clusters.Items, scheduled.Items, stores.Items, src.pods, now)
	out.Version = src.version
	out.Error = sectionError(append(errs, src.podsErr)...)

	return out
}

func mapCNPG(clusters []cnpgClusterObject, scheduled []cnpgScheduledBackupObject, stores []cnpgObjectStoreObject, pods []dsPod, now time.Time) *cnpgStatus {
	out := &cnpgStatus{Clusters: []cnpgCluster{}}

	// Backup interval expected per cluster (namespace/name), from its active schedules.
	intervals := map[string]time.Duration{}

	for _, s := range scheduled {
		if s.Spec.Suspend != nil && *s.Spec.Suspend {
			continue
		}

		key := s.Metadata.Namespace + "/" + s.Spec.Cluster.Name
		if d := cronInterval(s.Spec.Schedule); intervals[key] == 0 || d < intervals[key] {
			intervals[key] = d
		}
	}

	storeByKey := map[string]cnpgObjectStoreObject{}
	for _, s := range stores {
		storeByKey[s.Metadata.Namespace+"/"+s.Metadata.Name] = s
	}

	podsByCluster := map[string][]cnpgPod{}

	for _, p := range pods {
		labels := p.Metadata.Labels

		// Job pods (initdb, join) carry the cluster label too: keep the instances.
		name := labels["cnpg.io/cluster"]
		if role := labels["cnpg.io/podRole"]; name == "" || (role != "" && role != "instance") {
			continue
		}

		instanceRole := labels["cnpg.io/instanceRole"]
		if instanceRole == "" {
			instanceRole = labels["role"] // before CNPG 1.23
		}

		key := p.Metadata.Namespace + "/" + name
		podsByCluster[key] = append(podsByCluster[key], cnpgPod{
			Name: p.Metadata.Name, Node: p.Spec.NodeName, Phase: p.Status.Phase, Role: instanceRole, Ready: p.containerReady(""),
		})
	}

	for _, c := range clusters {
		key := c.Metadata.Namespace + "/" + c.Metadata.Name
		out.Clusters = append(out.Clusters, mapCNPGCluster(c, intervals[key], storeByKey, podsByCluster[key], now))
	}

	slices.SortFunc(out.Clusters, func(a, b cnpgCluster) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	return out
}

func mapCNPGCluster(c cnpgClusterObject, interval time.Duration, stores map[string]cnpgObjectStoreObject, pods []cnpgPod, now time.Time) cnpgCluster {
	st := c.Status
	out := cnpgCluster{
		Namespace: c.Metadata.Namespace, Name: c.Metadata.Name,
		Phase: st.Phase, PhaseReason: st.PhaseReason,
		Instances: c.Spec.Instances, ReadyInstances: st.ReadyInstances,
		CurrentPrimary: st.CurrentPrimary, TargetPrimary: st.TargetPrimary,
		InstancePods: pods, Scheduled: interval > 0, Reasons: []string{},
		LastSuccessAt: unixMilli(st.LastSuccessfulBackup), LastFailureAt: unixMilli(st.LastFailedBackup),
		RecoverableAt: unixMilli(st.FirstRecoverabilityPoint),
	}

	if out.InstancePods == nil {
		out.InstancePods = []cnpgPod{}
	}

	// An in-tree barmanObjectStore both archives WAL and takes backups.
	archiver := len(c.Spec.Backup.BarmanObjectStore) > 0 && string(c.Spec.Backup.BarmanObjectStore) != "null"

	out.BackupMethod = "none"
	if archiver {
		out.BackupMethod = "in-tree"
	}

	for _, p := range c.Spec.Plugins {
		if p.Name != barmanCloudPlugin || (p.Enabled != nil && !*p.Enabled) {
			continue
		}

		out.BackupMethod, out.ObjectStore = "plugin", p.Parameters["barmanObjectName"]
		archiver = archiver || (p.IsWALArchiver != nil && *p.IsWALArchiver)

		server := p.Parameters["serverName"]
		if server == "" {
			server = c.Metadata.Name
		}

		// The plugin records backup times per server in its ObjectStore, not in the Cluster.
		if w, ok := stores[c.Metadata.Namespace+"/"+out.ObjectStore].Status.ServerRecoveryWindow[server]; ok {
			out.LastSuccessAt = max(out.LastSuccessAt, unixMilli(w.LastSuccessfulBackupTime))
			out.LastFailureAt = max(out.LastFailureAt, unixMilli(w.LastFailedBackupTime))

			if t := unixMilli(w.FirstRecoverabilityPoint); t != 0 {
				out.RecoverableAt = t
			}
		}
	}

	out.Archiving = cnpgArchiving(archiver, conditionStatus(st.Conditions, "ContinuousArchiving"))
	out.LastBackup = cnpgLastBackup(out, conditionStatus(st.Conditions, "LastBackupSucceeded"), interval, now)

	// A hibernated cluster has no instance on purpose: idle, not down.
	if c.Metadata.Annotations["cnpg.io/hibernation"] == "on" || conditionStatus(st.Conditions, "cnpg.io/hibernation") == "True" {
		out.Hibernated, out.Health = true, healthIdle

		return out
	}

	out.Health, out.Reasons = cnpgHealth(out, conditionStatus(st.Conditions, "Ready"))

	return out
}

// cnpgArchiving: no archiver configured is a choice, not a fault.
func cnpgArchiving(configured bool, condition string) string {
	switch {
	case !configured:
		return "off"
	case condition == "True":
		return "ok"
	case condition == "False":
		return "failing"
	default:
		return "unknown"
	}
}

func cnpgLastBackup(c cnpgCluster, condition string, interval time.Duration, now time.Time) string {
	switch {
	case condition == "False", c.LastFailureAt > c.LastSuccessAt:
		return "failed"
	case interval > 0 && (c.LastSuccessAt == 0 || now.Sub(time.UnixMilli(c.LastSuccessAt)) > interval*3/2):
		return "stale"
	case c.LastSuccessAt == 0:
		return "none"
	default:
		return "ok"
	}
}

func cnpgHealth(c cnpgCluster, ready string) (string, []string) {
	reasons := []string{}
	critical := false

	phase := strings.ToLower(c.Phase)

	switch {
	case c.Instances > 0 && c.ReadyInstances == 0:
		reasons, critical = append(reasons, cnpgReasonNoInstance), true
	case strings.Contains(phase, "failing over") || strings.Contains(phase, "failover"):
		reasons, critical = append(reasons, cnpgReasonFailover), true
	case c.ReadyInstances < c.Instances:
		reasons = append(reasons, cnpgReasonInstances)
	}

	if c.CurrentPrimary != c.TargetPrimary && c.TargetPrimary != "" {
		reasons = append(reasons, cnpgReasonSwitchover)
	}

	if ready != "" && ready != "True" && len(reasons) == 0 {
		reasons = append(reasons, cnpgReasonNotReady)
	}

	if c.Archiving == "failing" {
		reasons = append(reasons, cnpgReasonArchiving)
	}

	switch c.LastBackup {
	case "failed":
		reasons = append(reasons, cnpgReasonBackupFailed)
	case "stale":
		reasons = append(reasons, cnpgReasonBackupStale)
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

// cronInterval is roughly how often a CNPG (6 fields with seconds) or Velero (5) schedule fires:
// weekly when it names weekdays, monthly when it names days of the month, else daily.
// Good enough to tell a backup that is late; an unreadable schedule counts as weekly + 1 day.
func cronInterval(schedule string) time.Duration {
	const day = 24 * time.Hour

	fields := strings.Fields(schedule)

	switch {
	case len(fields) == 1 && slices.Contains([]string{"@daily", "@midnight", "@hourly"}, fields[0]),
		len(fields) == 2 && fields[0] == "@every": // @every 1h: lateness counted in days anyway
		return day
	case len(fields) == 1 && fields[0] == "@weekly":
		return 7 * day
	case len(fields) == 1 && fields[0] == "@monthly":
		return 31 * day
	case len(fields) == 1 && (fields[0] == "@yearly" || fields[0] == "@annually"):
		return 366 * day
	case len(fields) == 5:
		fields = append([]string{"0"}, fields...)
	case len(fields) != 6:
		return 8 * day
	}

	dom, dow := fields[3], fields[5]

	switch {
	case dow != "*" && dow != "?":
		return 7 * day
	case dom != "*" && dom != "?":
		return 31 * day
	default:
		return day
	}
}
