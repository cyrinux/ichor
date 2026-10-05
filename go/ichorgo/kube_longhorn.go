package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type longhornStatus struct {
	Version       string                 `json:"version"` // API version read (v1beta2)
	Error         string                 `json:"error"`
	Volumes       []longhornVolume       `json:"volumes"`
	Nodes         []longhornNode         `json:"nodes"`
	BackupTargets []longhornBackupTarget `json:"backupTargets"`
}

type longhornVolume struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	PVCNamespace    string   `json:"pvcNamespace"` // "" when no claim is bound
	PVCName         string   `json:"pvcName"`
	State           string   `json:"state"`      // creating|attached|detached|attaching|detaching|deleting
	Robustness      string   `json:"robustness"` // healthy|degraded|faulted|unknown
	Health          string   `json:"health"`     // critical|warning|ok|idle
	ReplicasDesired int      `json:"replicasDesired"`
	ReplicasHealthy int      `json:"replicasHealthy"`
	Rebuilding      int      `json:"rebuilding"`
	ReplicaNodes    []string `json:"replicaNodes"` // nodes holding a replica, failed ones too
	Node            string   `json:"node"`         // where it is attached
	Size            int64    `json:"size"`
	ActualSize      int64    `json:"actualSize"`
	LastBackupAt    int64    `json:"lastBackupAt"` // unix ms, 0 when never
	// Progress of what the engine is doing, 0-100, read when the flag beside it is set.
	RebuildProgress  int    `json:"rebuildProgress"` // slowest replica rebuild, with Rebuilding > 0
	BackingUp        bool   `json:"backingUp"`
	BackupProgress   int    `json:"backupProgress"`
	Restoring        bool   `json:"restoring"`
	RestoreProgress  int    `json:"restoreProgress"`
	ScheduleError    string `json:"scheduleError"`    // why a replica cannot be placed, "" when it can
	TooManySnapshots bool   `json:"tooManySnapshots"` // near Longhorn's snapshot limit
}

type longhornNode struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"` // Longhorn's own, where the node object lives
	Ready       bool   `json:"ready"`
	Schedulable bool   `json:"schedulable"`
	// What the user asked: new replicas on the node, its replicas moved away.
	AllowScheduling   bool           `json:"allowScheduling"`
	EvictionRequested bool           `json:"evictionRequested"`
	Replicas          int            `json:"replicas"` // replicas it holds, failed ones too
	Disks             []longhornDisk `json:"disks"`
}

type longhornDisk struct {
	Path        string `json:"path"`
	Schedulable bool   `json:"schedulable"`
	Available   int64  `json:"available"`
	Maximum     int64  `json:"maximum"`
	Scheduled   int64  `json:"scheduled"`
}

type longhornBackupTarget struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

type kubeCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// conditionStatus is the status ("True", "False") of a condition type, "" when absent.
func conditionStatus(conds []kubeCondition, typ string) string {
	for _, c := range conds {
		if c.Type == typ {
			return c.Status
		}
	}

	return ""
}

type lhVolumeObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		NumberOfReplicas int    `json:"numberOfReplicas"`
		Size             string `json:"size"` // bytes, as a string
	} `json:"spec"`
	Status struct {
		State            string          `json:"state"`
		Robustness       string          `json:"robustness"`
		CurrentNodeID    string          `json:"currentNodeID"`
		ActualSize       int64           `json:"actualSize"`
		LastBackupAt     string          `json:"lastBackupAt"`
		Conditions       []kubeCondition `json:"conditions"`
		KubernetesStatus struct {
			Namespace string `json:"namespace"`
			PVCName   string `json:"pvcName"`
		} `json:"kubernetesStatus"`
	} `json:"status"`
}

// lhReplicaTimes are where Longhorn records a replica's health: in spec up to 1.12 at least,
// possibly in status later. Both are read.
type lhReplicaTimes struct {
	HealthyAt string `json:"healthyAt"`
	FailedAt  string `json:"failedAt"`
}

type lhReplicaObject struct {
	Spec struct {
		lhReplicaTimes
		VolumeName string `json:"volumeName"`
		NodeID     string `json:"nodeID"`
	} `json:"spec"`
	Status struct {
		lhReplicaTimes
		CurrentState string `json:"currentState"`
	} `json:"status"`
}

func (r lhReplicaObject) times() lhReplicaTimes {
	t := r.Spec.lhReplicaTimes
	if t.HealthyAt == "" {
		t.HealthyAt = r.Status.HealthyAt
	}

	if t.FailedAt == "" {
		t.FailedAt = r.Status.FailedAt
	}

	return t
}

type lhNodeObject struct {
	Metadata struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		AllowScheduling   bool `json:"allowScheduling"`
		EvictionRequested bool `json:"evictionRequested"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
		DiskStatus map[string]struct {
			DiskPath         string          `json:"diskPath"`
			Conditions       []kubeCondition `json:"conditions"`
			StorageAvailable int64           `json:"storageAvailable"`
			StorageMaximum   int64           `json:"storageMaximum"`
			StorageScheduled int64           `json:"storageScheduled"`
		} `json:"diskStatus"`
	} `json:"status"`
}

// lhEngineObject is a volume's engine: what it is rebuilding, backing up or restoring.
// Its maps are keyed by replica address (rebuild, restore) or backup name.
type lhEngineObject struct {
	Spec struct {
		VolumeName string `json:"volumeName"`
	} `json:"spec"`
	Status struct {
		RebuildStatus map[string]*struct {
			IsRebuilding bool   `json:"isRebuilding"`
			Progress     int    `json:"progress"`
			State        string `json:"state"`
		} `json:"rebuildStatus"`
		BackupStatus map[string]*struct {
			Progress int    `json:"progress"`
			State    string `json:"state"` // in_progress|complete|error
		} `json:"backupStatus"`
		RestoreStatus map[string]*struct {
			IsRestoring bool `json:"isRestoring"`
			Progress    int  `json:"progress"`
		} `json:"restoreStatus"`
	} `json:"status"`
}

type lhBackupTargetObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		BackupTargetURL string `json:"backupTargetURL"`
	} `json:"spec"`
	Status struct {
		Available  bool            `json:"available"`
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// readLonghorn lists volumes, replicas, nodes and backup targets of every namespace.
func readLonghorn(ctx context.Context, k *kubeClient, version string) *longhornStatus {
	base := "/apis/" + groupLonghorn + "/" + version + "/"

	var (
		volumes  kubeList[lhVolumeObject]
		replicas kubeList[lhReplicaObject]
		nodes    kubeList[lhNodeObject]
		targets  kubeList[lhBackupTargetObject]
		engines  kubeList[lhEngineObject]
		errs     = make([]error, 3)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = getList(ctx, k, base+"volumes", &volumes) })
	wg.Go(func() { errs[1] = getList(ctx, k, base+"replicas", &replicas) })
	wg.Go(func() { errs[2] = getList(ctx, k, base+"nodes", &nodes) })
	// Older releases have no BackupTarget resource: its absence is not an error.
	wg.Go(func() { _ = getList(ctx, k, base+"backuptargets", &targets) })
	// Engines only add progress: without them the volumes still read right.
	wg.Go(func() { _ = getList(ctx, k, base+"engines", &engines) })
	wg.Wait()

	out := mapLonghorn(version, volumes.Items, replicas.Items, nodes.Items, targets.Items, sectionError(errs...))
	addLonghornProgress(out.Volumes, engines.Items)

	return out
}

func mapLonghorn(version string, volumes []lhVolumeObject, replicas []lhReplicaObject, nodes []lhNodeObject, targets []lhBackupTargetObject, errMsg string) *longhornStatus {
	out := &longhornStatus{Version: version, Error: errMsg, Volumes: []longhornVolume{}, Nodes: []longhornNode{}, BackupTargets: []longhornBackupTarget{}}

	byVolume := map[string][]lhReplicaObject{}
	for _, r := range replicas {
		byVolume[r.Spec.VolumeName] = append(byVolume[r.Spec.VolumeName], r)
	}

	for _, v := range volumes {
		out.Volumes = append(out.Volumes, mapLonghornVolume(v, byVolume[v.Metadata.Name]))
	}

	slices.SortFunc(out.Volumes, func(a, b longhornVolume) int {
		if c := healthRank(a.Health) - healthRank(b.Health); c != 0 {
			return c
		}

		return strings.Compare(a.PVCNamespace+"/"+a.PVCName+"/"+a.Name, b.PVCNamespace+"/"+b.PVCName+"/"+b.Name)
	})

	perNode := map[string]int{}
	for _, r := range replicas {
		perNode[r.Spec.NodeID]++
	}

	for _, n := range nodes {
		node := mapLonghornNode(n)
		node.Replicas = perNode[node.Name]
		out.Nodes = append(out.Nodes, node)
	}

	slices.SortFunc(out.Nodes, func(a, b longhornNode) int { return strings.Compare(a.Name, b.Name) })

	for _, t := range targets {
		bt := longhornBackupTarget{Name: t.Metadata.Name, URL: t.Spec.BackupTargetURL, Available: t.Status.Available}
		for _, c := range t.Status.Conditions {
			if c.Type == "Unavailable" && c.Status == "True" {
				bt.Message = c.Message
			}
		}

		out.BackupTargets = append(out.BackupTargets, bt)
	}

	return out
}

func mapLonghornVolume(v lhVolumeObject, replicas []lhReplicaObject) longhornVolume {
	st := v.Status
	out := longhornVolume{
		Name: v.Metadata.Name, Namespace: v.Metadata.Namespace,
		PVCNamespace: st.KubernetesStatus.Namespace, PVCName: st.KubernetesStatus.PVCName,
		State: st.State, Robustness: st.Robustness, Node: st.CurrentNodeID,
		ReplicasDesired: v.Spec.NumberOfReplicas, ActualSize: st.ActualSize,
		LastBackupAt: unixMilli(st.LastBackupAt), ReplicaNodes: []string{},
	}

	out.Size, _ = strconv.ParseInt(v.Spec.Size, 10, 64)

	for _, r := range replicas {
		t := r.times()
		running := r.Status.CurrentState == "running"

		switch {
		case running && t.HealthyAt != "" && t.FailedAt == "":
			out.ReplicasHealthy++
		case running && t.HealthyAt == "" && t.FailedAt == "":
			out.Rebuilding++
		}

		if r.Spec.NodeID != "" && !slices.Contains(out.ReplicaNodes, r.Spec.NodeID) {
			out.ReplicaNodes = append(out.ReplicaNodes, r.Spec.NodeID)
		}
	}

	slices.Sort(out.ReplicaNodes)

	for _, c := range st.Conditions {
		switch {
		case c.Type == "Scheduled" && c.Status == "False":
			out.ScheduleError = cmp.Or(c.Message, c.Reason, "replica scheduling failed")
		case c.Type == "TooManySnapshots" && c.Status == "True":
			out.TooManySnapshots = true
		}
	}

	out.Health = longhornVolumeHealth(st.State, st.Robustness)

	return out
}

// longhornVolumeHealth: a faulted volume lost its data path; degraded runs short of
// replicas; a detached volume is simply not in use.
func longhornVolumeHealth(state, robustness string) string {
	switch {
	case robustness == "faulted":
		return healthCritical
	case robustness == "degraded", robustness == "unknown" && state == "attached":
		return healthWarning
	case state == "detached":
		return healthIdle
	default:
		return healthOK
	}
}

func mapLonghornNode(n lhNodeObject) longhornNode {
	out := longhornNode{
		Name:        n.Metadata.Name,
		Namespace:   n.Metadata.Namespace,
		Ready:       conditionStatus(n.Status.Conditions, "Ready") == "True",
		Schedulable: conditionStatus(n.Status.Conditions, "Schedulable") == "True",
		Disks:       []longhornDisk{},

		AllowScheduling: n.Spec.AllowScheduling, EvictionRequested: n.Spec.EvictionRequested,
	}

	for _, d := range n.Status.DiskStatus {
		out.Disks = append(out.Disks, longhornDisk{
			Path: d.DiskPath, Schedulable: conditionStatus(d.Conditions, "Schedulable") == "True",
			Available: d.StorageAvailable, Maximum: d.StorageMaximum, Scheduled: d.StorageScheduled,
		})
	}

	slices.SortFunc(out.Disks, func(a, b longhornDisk) int { return strings.Compare(a.Path, b.Path) })

	return out
}

// addLonghornProgress fills in the rebuilds, backups and restores the volumes' engines run.
// Each reports its least advanced replica or backup.
func addLonghornProgress(volumes []longhornVolume, engines []lhEngineObject) {
	byVolume := map[string]lhEngineObject{}
	for _, e := range engines {
		byVolume[e.Spec.VolumeName] = e
	}

	for i := range volumes {
		e, ok := byVolume[volumes[i].Name]
		if !ok {
			continue
		}

		v := &volumes[i]
		rebuilding := false

		for _, r := range e.Status.RebuildStatus {
			if r != nil && (r.IsRebuilding || r.State == "in_progress") {
				v.RebuildProgress = lowestProgress(rebuilding, v.RebuildProgress, r.Progress)
				rebuilding = true
			}
		}

		if rebuilding && v.Rebuilding == 0 {
			v.Rebuilding = 1
		}

		for _, b := range e.Status.BackupStatus {
			if b != nil && b.State == "in_progress" {
				v.BackupProgress = lowestProgress(v.BackingUp, v.BackupProgress, b.Progress)
				v.BackingUp = true
			}
		}

		for _, r := range e.Status.RestoreStatus {
			if r != nil && r.IsRestoring {
				v.RestoreProgress = lowestProgress(v.Restoring, v.RestoreProgress, r.Progress)
				v.Restoring = true
			}
		}
	}
}

// lowestProgress is p clamped to 0-100, or the lower of it and current once one is known.
func lowestProgress(known bool, current, p int) int {
	p = min(max(p, 0), 100)
	if known {
		return min(current, p)
	}

	return p
}
