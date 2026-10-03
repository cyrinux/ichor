package talosmobile

import (
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
}

type longhornNode struct {
	Name        string         `json:"name"`
	Ready       bool           `json:"ready"`
	Schedulable bool           `json:"schedulable"`
	Disks       []longhornDisk `json:"disks"`
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
		State            string `json:"state"`
		Robustness       string `json:"robustness"`
		CurrentNodeID    string `json:"currentNodeID"`
		ActualSize       int64  `json:"actualSize"`
		LastBackupAt     string `json:"lastBackupAt"`
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
		Name string `json:"name"`
	} `json:"metadata"`
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

type lhList[T any] struct {
	Items []T `json:"items"`
}

// readLonghorn lists volumes, replicas, nodes and backup targets of every namespace.
func readLonghorn(ctx context.Context, k *kubeClient, version string) *longhornStatus {
	base := "/apis/" + groupLonghorn + "/" + version + "/"

	var (
		volumes  lhList[lhVolumeObject]
		replicas lhList[lhReplicaObject]
		nodes    lhList[lhNodeObject]
		targets  lhList[lhBackupTargetObject]
		errs     = make([]error, 3)
		wg       sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"volumes", &volumes) })
	wg.Go(func() { errs[1] = k.get(ctx, base+"replicas", &replicas) })
	wg.Go(func() { errs[2] = k.get(ctx, base+"nodes", &nodes) })
	// Older releases have no BackupTarget resource: its absence is not an error.
	wg.Go(func() { _ = k.get(ctx, base+"backuptargets", &targets) })
	wg.Wait()

	return mapLonghorn(version, volumes.Items, replicas.Items, nodes.Items, targets.Items, sectionError(errs...))
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

	for _, n := range nodes {
		out.Nodes = append(out.Nodes, mapLonghornNode(n))
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
		Ready:       conditionStatus(n.Status.Conditions, "Ready") == "True",
		Schedulable: conditionStatus(n.Status.Conditions, "Schedulable") == "True",
		Disks:       []longhornDisk{},
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
