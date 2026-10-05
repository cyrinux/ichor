package ichorgo

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// groupPercona is the API group of the Percona Operator for MySQL based on XtraDB Cluster
// (perconaxtradbclusters, one per cluster, and perconaxtradbclusterbackups).
const groupPercona = "pxc.percona.com"

type perconaStatus struct {
	Version  string           `json:"version"`
	Error    string           `json:"error"`
	Clusters []perconaCluster `json:"clusters"`
}

type perconaCluster struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// State is the operator's own word: ready, initializing, paused, stopping, error, unknown.
	State     string   `json:"state"`
	Message   string   `json:"message"` // the operator's messages, "; "-joined
	CRVersion string   `json:"crVersion"`
	Paused    bool     `json:"paused"`
	Health    string   `json:"health"`  // critical|warning|ok|idle (paused)
	Reasons   []string `json:"reasons"` // see perconaReason*
	PXCSize   int      `json:"pxcSize"`
	PXCReady  int      `json:"pxcReady"`
	// Proxy is the enabled proxy in front of the members: haproxy, proxysql or "" for none.
	Proxy              string            `json:"proxy"`
	ProxySize          int               `json:"proxySize"`
	ProxyReady         int               `json:"proxyReady"`
	Pods               []perconaPod      `json:"pods"` // the PXC members
	LastBackupAt       int64             `json:"lastBackupAt"`
	LastBackupFailedAt int64             `json:"lastBackupFailedAt"`
	BackupSchedules    []perconaSchedule `json:"backupSchedules"`
}

type perconaPod struct {
	Name  string `json:"name"`
	Node  string `json:"node"` // "" while Pending: not scheduled anywhere
	Phase string `json:"phase"`
	Ready bool   `json:"ready"`
}

type perconaSchedule struct {
	Name        string `json:"name"`
	Schedule    string `json:"schedule"` // cron, 5 fields
	Keep        int    `json:"keep"`
	StorageName string `json:"storageName"`
}

// Reasons a cluster is not ok, for the app to word.
const (
	perconaReasonError        = "error"        // the operator's state is error
	perconaReasonNoMember     = "noMember"     // no PXC member ready: the database is down
	perconaReasonMembers      = "members"      // fewer ready members than its size
	perconaReasonProxy        = "proxy"        // fewer ready HAProxy/ProxySQL pods than wanted
	perconaReasonInitializing = "initializing" // the operator is still bringing it up
	perconaReasonBackupFailed = "backupFailed" // the last backup failed
	perconaReasonBackupStale  = "backupStale"  // no successful backup for twice its schedule
)

// perconaAppStatus is the operator's status of one component (pxc, haproxy, proxysql).
type perconaAppStatus struct {
	Size    int    `json:"size"`
	Ready   int    `json:"ready"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// perconaPodSpec holds the fields of a component's spec the reader uses.
type perconaPodSpec struct {
	Enabled bool `json:"enabled"`
	Size    int  `json:"size"`
}

type perconaClusterObject struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		CRVersion string          `json:"crVersion"`
		Pause     bool            `json:"pause"`
		PXC       *perconaPodSpec `json:"pxc"`
		HAProxy   *perconaPodSpec `json:"haproxy"`
		ProxySQL  *perconaPodSpec `json:"proxysql"`
		Backup    *struct {
			Schedule []perconaSchedule `json:"schedule"`
		} `json:"backup"`
	} `json:"spec"`
	Status struct {
		State    string           `json:"state"`
		Messages []string         `json:"message"`
		PXC      perconaAppStatus `json:"pxc"`
		HAProxy  perconaAppStatus `json:"haproxy"`
		ProxySQL perconaAppStatus `json:"proxysql"`
	} `json:"status"`
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

// readPercona lists the clusters, their backups and their pods (labelled by the operator with
// app.kubernetes.io/name=percona-xtradb-cluster, app.kubernetes.io/instance=<cluster> and
// app.kubernetes.io/component=pxc|haproxy|proxysql): three listings whatever the number of clusters.
func readPercona(ctx context.Context, k *kubeClient, version string, now time.Time) *perconaStatus {
	base := "/apis/" + groupPercona + "/" + version + "/"

	var (
		clusters kubeList[perconaClusterObject]
		backups  kubeList[perconaBackupObject]
		pods     []dsPod
		errs     = make([]error, 3)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = getList(ctx, k, base+"perconaxtradbclusters", &clusters) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"perconaxtradbclusterbackups", &backups) })
	wg.Go(func() { pods, errs[2] = listDSPods(ctx, k, "app.kubernetes.io/name=percona-xtradb-cluster") })
	wg.Wait()

	out := mapPercona(clusters.Items, backups.Items, pods, now)
	out.Version = version
	out.Error = sectionError(errs...)

	return out
}

// perconaBackupTimes is the latest successful and failed backup of one cluster, unix ms.
type perconaBackupTimes struct{ success, failure int64 }

func mapPercona(clusters []perconaClusterObject, backups []perconaBackupObject, pods []dsPod, now time.Time) *perconaStatus {
	out := &perconaStatus{Clusters: []perconaCluster{}}

	times := map[string]perconaBackupTimes{}

	for _, b := range backups {
		key := b.Metadata.Namespace + "/" + b.Spec.PXCCluster
		t := times[key]

		// A failed backup may never complete: its creation dates it then.
		at := unixMilli(b.Status.Completed)
		if at == 0 {
			at = unixMilli(b.Metadata.CreationTimestamp)
		}

		switch b.Status.State {
		case "Succeeded":
			t.success = max(t.success, at)
		case "Failed":
			t.failure = max(t.failure, at)
		}

		times[key] = t
	}

	byCluster := map[string][]perconaPod{}

	for _, p := range pods {
		labels := p.Metadata.Labels

		// Proxies and backup jobs carry the instance label too: keep the members.
		name := labels["app.kubernetes.io/instance"]
		if name == "" || labels["app.kubernetes.io/component"] != "pxc" {
			continue
		}

		key := p.Metadata.Namespace + "/" + name
		byCluster[key] = append(byCluster[key], perconaPod{
			Name: p.Metadata.Name, Node: p.Spec.NodeName, Phase: p.Status.Phase, Ready: p.containerReady(""),
		})
	}

	for _, c := range clusters {
		key := c.Metadata.Namespace + "/" + c.Metadata.Name
		out.Clusters = append(out.Clusters, mapPerconaCluster(c, times[key], byCluster[key], now))
	}

	slices.SortFunc(out.Clusters, func(a, b perconaCluster) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	return out
}

func mapPerconaCluster(c perconaClusterObject, backups perconaBackupTimes, pods []perconaPod, now time.Time) perconaCluster {
	st := c.Status
	out := perconaCluster{
		Namespace: c.Metadata.Namespace, Name: c.Metadata.Name, State: st.State,
		CRVersion: c.Spec.CRVersion, Paused: c.Spec.Pause || st.State == "paused",
		PXCSize: st.PXC.Size, PXCReady: st.PXC.Ready, Pods: pods, Reasons: []string{},
		LastBackupAt: backups.success, LastBackupFailedAt: backups.failure, BackupSchedules: []perconaSchedule{},
	}

	// The status omits a size of 0 and is empty before the operator's first pass.
	if out.PXCSize == 0 && c.Spec.PXC != nil {
		out.PXCSize = c.Spec.PXC.Size
	}

	out.Proxy, out.ProxySize, out.ProxyReady = perconaProxy(c)

	if out.Pods == nil {
		out.Pods = []perconaPod{}
	}

	slices.SortFunc(out.Pods, func(a, b perconaPod) int { return strings.Compare(a.Name, b.Name) })

	if c.Spec.Backup != nil && c.Spec.Backup.Schedule != nil {
		out.BackupSchedules = c.Spec.Backup.Schedule
	}

	var msgs []string

	for _, m := range append(slices.Clone(st.Messages), st.PXC.Message) {
		if m = strings.TrimSpace(m); m != "" {
			msgs = append(msgs, m)
		}
	}

	out.Message = strings.Join(slices.Compact(msgs), "; ")

	// A paused cluster has no member on purpose: idle, not down.
	if out.Paused {
		out.Health = healthIdle

		return out
	}

	out.Health, out.Reasons = perconaHealth(out, perconaBackupStale(c, out, now))

	return out
}

// perconaProxy is the enabled proxy with its wanted and ready pods (the operator refuses
// HAProxy and ProxySQL both enabled).
func perconaProxy(c perconaClusterObject) (string, int, int) {
	pick := func(name string, spec *perconaPodSpec, st perconaAppStatus) (string, int, int) {
		size := st.Size
		if size == 0 {
			size = spec.Size
		}

		return name, size, st.Ready
	}

	switch {
	case c.Spec.HAProxy != nil && c.Spec.HAProxy.Enabled:
		return pick("haproxy", c.Spec.HAProxy, c.Status.HAProxy)
	case c.Spec.ProxySQL != nil && c.Spec.ProxySQL.Enabled:
		return pick("proxysql", c.Spec.ProxySQL, c.Status.ProxySQL)
	default:
		return "", 0, 0
	}
}

// perconaBackupStale reports whether the cluster has scheduled backups but no successful one
// for twice the most frequent schedule's interval. A cluster younger than that is not late yet.
func perconaBackupStale(c perconaClusterObject, out perconaCluster, now time.Time) bool {
	var interval time.Duration

	for _, s := range out.BackupSchedules {
		if d := cronInterval(s.Schedule); interval == 0 || d < interval {
			interval = d
		}
	}

	if interval == 0 {
		return false
	}

	since := out.LastBackupAt
	if since == 0 {
		since = unixMilli(c.Metadata.CreationTimestamp)
	}

	return since != 0 && now.Sub(time.UnixMilli(since)) > 2*interval
}

func perconaHealth(c perconaCluster, stale bool) (string, []string) {
	reasons := []string{}
	critical := false

	if strings.EqualFold(c.State, "error") {
		reasons, critical = append(reasons, perconaReasonError), true
	}

	switch {
	case c.PXCSize > 0 && c.PXCReady == 0:
		reasons, critical = append(reasons, perconaReasonNoMember), true
	case c.PXCReady < c.PXCSize:
		reasons = append(reasons, perconaReasonMembers)
	}

	if c.ProxyReady < c.ProxySize {
		reasons = append(reasons, perconaReasonProxy)
	}

	// The operator says initializing whenever a pod is not ready: only shown when nothing
	// more precise explains it.
	if strings.EqualFold(c.State, "initializing") && len(reasons) == 0 {
		reasons = append(reasons, perconaReasonInitializing)
	}

	switch {
	case c.LastBackupFailedAt > c.LastBackupAt:
		reasons = append(reasons, perconaReasonBackupFailed)
	case stale:
		reasons = append(reasons, perconaReasonBackupStale)
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
