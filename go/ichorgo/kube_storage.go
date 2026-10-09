package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
)

// The Storage screen: each PersistentVolumeClaim with its volume, StorageClass, the pods
// mounting it and how full it is (kubelet stats/summary, as the checkup reads it), problems
// first. Only the claims are needed: a namespace-scoped account that may not read volumes,
// classes or nodes still gets its claims, with what it may read.

// Claim levels, for the apps' colours: the checkup's thresholds.
const (
	storageOK       = "ok"
	storageWarning  = "warning"
	storageCritical = "critical"
)

// Data services a volume can belong to, by their catalog id (the apps' DataServiceKind).
const (
	managedLonghorn = "longhorn"
	managedRook     = "rook"
	managedCNPG     = "cloudnative-pg"
)

// kubeStorage is KubeStorage's answer. PartialAccess: volumes, classes or nodes could not
// be read, so some columns (volume, provisioner, fill) may be empty.
type kubeStorage struct {
	Claims        []storageClaim `json:"claims"`
	PartialAccess bool           `json:"partialAccess"`
}

// storageClaim is a claim as the Storage screen shows it. Capacity and Used in bytes;
// Measured: a kubelet reported the fill (a mounted filesystem volume on a node that answered).
type storageClaim struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Phase         string   `json:"phase"`
	StorageClass  string   `json:"storageClass,omitempty"`
	Provisioner   string   `json:"provisioner,omitempty"`
	Volume        string   `json:"volume,omitempty"`
	ReclaimPolicy string   `json:"reclaimPolicy,omitempty"`
	AccessModes   []string `json:"accessModes"`
	Capacity      float64  `json:"capacity"`
	Used          float64  `json:"used"`
	UsedPercent   float64  `json:"usedPercent"`
	InodesPercent float64  `json:"inodesPercent"`
	Measured      bool     `json:"measured"`
	Pods          []string `json:"pods"`
	Terminating   bool     `json:"terminating"`
	// ManagedBy is the data service the volume belongs to (its catalog id), "" for none.
	ManagedBy string `json:"managedBy,omitempty"`
	// Level is ok, warning (pending, terminating, filling) or critical (lost, nearly full).
	Level string `json:"level"`
}

type storageClaimObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		StorageClassName string   `json:"storageClassName"`
		VolumeName       string   `json:"volumeName"`
		AccessModes      []string `json:"accessModes"`
		Resources        struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
	Status struct {
		Phase    string            `json:"phase"`
		Capacity map[string]string `json:"capacity"`
	} `json:"status"`
}

type storageVolumeObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		StorageClassName string `json:"storageClassName"`
		CSI              *struct {
			Driver string `json:"driver"`
		} `json:"csi"`
		ReclaimPolicy string `json:"persistentVolumeReclaimPolicy"`
	} `json:"spec"`
}

type storageClassInfo struct {
	Metadata    checkMeta `json:"metadata"`
	Provisioner string    `json:"provisioner"`
}

type storagePodObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Volumes []struct {
			PersistentVolumeClaim *struct {
				ClaimName string `json:"claimName"`
			} `json:"persistentVolumeClaim"`
		} `json:"volumes"`
	} `json:"spec"`
}

// KubeStorage lists the PersistentVolumeClaims of namespace ("" for all) with their volume,
// class, pods and fill level, problems first, as a JSON kubeStorage.
func KubeStorage(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoStorage(namespace), func(ctx context.Context, k *kubeClient) (kubeStorage, error) {
		return readStorage(ctx, k, namespace)
	})
}

func readStorage(ctx context.Context, k *kubeClient, namespace string) (kubeStorage, error) {
	claims, err := listObjects[storageClaimObject](ctx, k, scopedPath("/api/v1", namespace, "persistentvolumeclaims"))
	if err != nil {
		return kubeStorage{}, err
	}

	privacy.learnNamespaces(namespacesOf(claims, func(c storageClaimObject) string { return c.Metadata.Namespace }))

	// The rest only completes the rows: read together, each allowed to fail.
	var (
		wg                              sync.WaitGroup
		pods                            []storagePodObject
		volumes                         []storageVolumeObject
		classes                         []storageClassInfo
		nodes                           []checkNodeObject
		podsErr, volErr, classErr, nErr error
	)

	wg.Add(4)

	go func() {
		defer wg.Done()

		pods, podsErr = listObjects[storagePodObject](ctx, k, scopedPath("/api/v1", namespace, "pods"))
	}()
	go func() {
		defer wg.Done()

		volumes, volErr = listObjects[storageVolumeObject](ctx, k, "/api/v1/persistentvolumes")
	}()
	go func() {
		defer wg.Done()

		classes, classErr = listObjects[storageClassInfo](ctx, k, "/apis/storage.k8s.io/v1/storageclasses")
	}()
	go func() {
		defer wg.Done()

		nodes, nErr = listObjects[checkNodeObject](ctx, k, "/api/v1/nodes")
	}()

	wg.Wait()

	var use map[string]volumeUse
	if nErr == nil && len(claims) > 0 {
		use = readVolumeUse(ctx, k, nodes)
	}

	out := kubeStorage{
		Claims:        joinStorage(claims, pods, volumes, classes, use),
		PartialAccess: podsErr != nil || volErr != nil || classErr != nil || nErr != nil,
	}

	return out, nil
}

func joinStorage(claims []storageClaimObject, pods []storagePodObject, volumes []storageVolumeObject, classes []storageClassInfo, use map[string]volumeUse) []storageClaim {
	mounted := map[string][]string{}

	for _, p := range pods {
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				key := p.Metadata.Namespace + "/" + v.PersistentVolumeClaim.ClaimName
				mounted[key] = append(mounted[key], p.Metadata.Name)
			}
		}
	}

	byVolume := map[string]storageVolumeObject{}
	for _, v := range volumes {
		byVolume[v.Metadata.Name] = v
	}

	provisioners := map[string]string{}
	for _, c := range classes {
		provisioners[c.Metadata.Name] = c.Provisioner
	}

	out := []storageClaim{}

	for _, c := range claims {
		key := c.Metadata.Namespace + "/" + c.Metadata.Name
		pv := byVolume[c.Spec.VolumeName]

		s := storageClaim{
			Namespace: c.Metadata.Namespace, Name: c.Metadata.Name, Phase: c.Status.Phase,
			StorageClass: c.Spec.StorageClassName, Volume: c.Spec.VolumeName, ReclaimPolicy: pv.Spec.ReclaimPolicy,
			AccessModes: cmpOrSlice(c.Spec.AccessModes), Pods: sortedNames(mounted[key]),
			Terminating: !c.Metadata.deleted().IsZero(),
			Capacity:    parseQuantity(cmpOr(c.Status.Capacity["storage"], c.Spec.Resources.Requests["storage"])),
			Provisioner: provisioners[c.Spec.StorageClassName],
		}

		if s.Provisioner == "" && pv.Spec.CSI != nil {
			s.Provisioner = pv.Spec.CSI.Driver
		}

		if u, ok := use[key]; ok {
			s.Measured, s.Used, s.Capacity = true, u.used, u.capacity
			s.UsedPercent, s.InodesPercent = percentOf(u.used, u.capacity), percentOf(u.inodesUsed, u.inodes)
		}

		s.ManagedBy = managedByOf(c.Metadata.Labels, s.Provisioner)
		s.Level = storageLevel(s)
		out = append(out, s)
	}

	sortClaims(out)

	return out
}

// sortClaims puts problems first, the worst first, then the fullest.
func sortClaims(claims []storageClaim) {
	slices.SortFunc(claims, func(a, b storageClaim) int {
		return cmp.Or(
			cmp.Compare(levelRank(b.Level), levelRank(a.Level)),
			cmp.Compare(b.UsedPercent, a.UsedPercent),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}

// managedByOf is the data service a claim belongs to: CloudNativePG by its label (the
// database matters more than the volume under it), else Longhorn or Rook-Ceph by provisioner.
func managedByOf(labels map[string]string, provisioner string) string {
	switch {
	case labels["cnpg.io/cluster"] != "":
		return managedCNPG
	case provisioner == "driver.longhorn.io":
		return managedLonghorn
	case strings.HasSuffix(provisioner, ".rbd.csi.ceph.com"), strings.HasSuffix(provisioner, ".cephfs.csi.ceph.com"):
		return managedRook
	default:
		return ""
	}
}

func storageLevel(s storageClaim) string {
	switch {
	case s.Phase == "Lost", s.UsedPercent >= volumeCriticalPercent, s.InodesPercent >= inodesCriticalPercent:
		return storageCritical
	case s.Phase == "Pending", s.Terminating, s.UsedPercent >= volumeWarnPercent, s.InodesPercent >= inodesWarnPercent:
		return storageWarning
	default:
		return storageOK
	}
}

func levelRank(level string) int {
	switch level {
	case storageCritical:
		return 2
	case storageWarning:
		return 1
	default:
		return 0
	}
}

func cmpOrSlice(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

func sortedNames(names []string) []string {
	out := slices.Clone(cmpOrSlice(names))
	slices.Sort(out)

	return out
}
