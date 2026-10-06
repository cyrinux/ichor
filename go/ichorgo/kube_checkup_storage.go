package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The findings of the storage section, about a PersistentVolumeClaim unless said.
const (
	findVolumeFull   = "volumeFull"   // Value: percent used, Limit: its size in bytes, Extra: the pod
	findVolumeInodes = "volumeInodes" // Value: percent of inodes used
	findPVCPending   = "pvcPending"   // not bound (Extra: its storage class)
	findPVCLost      = "pvcLost"      // its volume is gone
	findPVFailed     = "pvFailed"     // a PersistentVolume in Failed (Message)
	findPVReleased   = "pvReleased"   // a PersistentVolume kept after its claim (Extra: the claim)
)

const (
	volumeWarnPercent     = 85.0
	volumeCriticalPercent = 95.0
	inodesWarnPercent     = 90.0
	inodesCriticalPercent = 98.0
	// kubeletStatsParallel bounds the kubelets asked at once, kubeletStatsTimeout each one.
	kubeletStatsParallel = 8
	kubeletStatsTimeout  = 10 * time.Second
	checkupMaxVolumes    = 300
)

// checkMeta is the metadata the checkup reads of an object.
type checkMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp time.Time         `json:"creationTimestamp"`
	DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
	Finalizers        []string          `json:"finalizers"`
}

// deleted is when the object was asked to go, zero when it was not.
func (m checkMeta) deleted() time.Time {
	if m.DeletionTimestamp == nil {
		return time.Time{}
	}

	return *m.DeletionTimestamp
}

type pvcObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		StorageClassName string `json:"storageClassName"`
		VolumeName       string `json:"volumeName"`
		Resources        struct {
			Requests map[string]string `json:"requests"`
		} `json:"resources"`
	} `json:"spec"`
	Status struct {
		Phase    string            `json:"phase"`
		Capacity map[string]string `json:"capacity"`
	} `json:"status"`
}

type pvObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		ClaimRef *struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"claimRef"`
		ReclaimPolicy string `json:"persistentVolumeReclaimPolicy"`
	} `json:"spec"`
	Status struct {
		Phase   string `json:"phase"`
		Message string `json:"message"`
	} `json:"status"`
}

type storageClassObject struct {
	Metadata          checkMeta `json:"metadata"`
	VolumeBindingMode string    `json:"volumeBindingMode"`
}

// kubeletSummary is the part of a kubelet's stats/summary about volumes.
type kubeletSummary struct {
	Pods []struct {
		PodRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"podRef"`
		Volumes []struct {
			PVCRef *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"pvcRef"`
			UsedBytes     float64 `json:"usedBytes"`
			CapacityBytes float64 `json:"capacityBytes"`
			Inodes        float64 `json:"inodes"`
			InodesUsed    float64 `json:"inodesUsed"`
		} `json:"volume"`
	} `json:"pods"`
}

// volumeUse is how full a claim's volume is, as the kubelet of the node mounting it says.
type volumeUse struct {
	used, capacity, inodes, inodesUsed float64
	pod, node                          string
}

// checkupVolume is a PersistentVolumeClaim with its fill level. Measured is false when no
// kubelet reported it: not mounted, a block volume, or a node that did not answer.
type checkupVolume struct {
	Namespace     string  `json:"namespace"`
	Name          string  `json:"name"`
	Phase         string  `json:"phase"`
	StorageClass  string  `json:"storageClass,omitempty"`
	Capacity      float64 `json:"capacity"` // bytes
	Used          float64 `json:"used"`     // bytes
	UsedPercent   float64 `json:"usedPercent"`
	InodesPercent float64 `json:"inodesPercent"`
	Measured      bool    `json:"measured"`
	Pod           string  `json:"pod,omitempty"`
	Node          string  `json:"node,omitempty"`
}

func checkupStorage(ctx context.Context, k *kubeClient, in checkupInput) (checkupSection, []checkupVolume) {
	if in.pvcsErr != nil {
		return newSection(checkStorage, 0, nil, in.pvcsErr), nil
	}

	pvs, pvsErr := listObjects[pvObject](ctx, k, "/api/v1/persistentvolumes")
	classes, _ := listObjects[storageClassObject](ctx, k, "/apis/storage.k8s.io/v1/storageclasses") //nolint:errcheck // only refines pvcPending

	var use map[string]volumeUse
	if len(in.pvcs) > 0 {
		use = readVolumeUse(ctx, k, in.nodes)
	}

	volumes, findings := volumeFindings(in.pvcs, use, classes, in.pods, in.now)
	findings = append(findings, pvFindings(pvs)...)

	return newSection(checkStorage, len(in.pvcs)+len(pvs), findings, pvsErr), volumes
}

// readVolumeUse asks every ready node's kubelet for its volumes' fill level, keyed
// "namespace/claim". A kubelet that does not answer leaves its volumes unmeasured.
func readVolumeUse(ctx context.Context, k *kubeClient, nodes []checkNodeObject) map[string]volumeUse {
	ready := slices.DeleteFunc(slices.Clone(nodes), func(n checkNodeObject) bool { return !kubeConditions(n.Status.Conditions).is("Ready") })
	summaries := make([]kubeletSummary, len(ready))

	forEachLimit(ready, kubeletStatsParallel, func(i int, n checkNodeObject) {
		nodeCtx, cancel := context.WithTimeout(ctx, kubeletStatsTimeout)
		defer cancel()

		_ = k.get(nodeCtx, "/api/v1/nodes/"+url.PathEscape(n.Metadata.Name)+"/proxy/stats/summary", &summaries[i]) //nolint:errcheck // unmeasured
	})

	use := map[string]volumeUse{}

	for i, s := range summaries {
		for _, p := range s.Pods {
			for _, v := range p.Volumes {
				if v.PVCRef == nil || v.CapacityBytes <= 0 {
					continue
				}

				// A claim several pods mount reports the same numbers from each.
				use[v.PVCRef.Namespace+"/"+v.PVCRef.Name] = volumeUse{
					used: v.UsedBytes, capacity: v.CapacityBytes, inodes: v.Inodes, inodesUsed: v.InodesUsed,
					pod: p.PodRef.Name, node: ready[i].Metadata.Name,
				}
			}
		}
	}

	return use
}

// volumeFindings maps the claims to volumes, fullest first, and finds those in trouble. A
// claim that waits for its first pod (WaitForFirstConsumer, no pod mounting it) is not
// pending: it is unused.
func volumeFindings(pvcs []pvcObject, use map[string]volumeUse, classes []storageClassObject, pods []checkPod, now time.Time) ([]checkupVolume, []checkupFinding) {
	immediate := map[string]bool{}
	for _, c := range classes {
		immediate[c.Metadata.Name] = c.VolumeBindingMode != "WaitForFirstConsumer"
	}

	mounted := map[string]bool{}

	for _, p := range pods {
		for _, claim := range p.Claims {
			mounted[p.Namespace+"/"+claim] = true
		}
	}

	volumes := make([]checkupVolume, 0, len(pvcs))
	findings := []checkupFinding{}

	for _, pvc := range pvcs {
		key := pvc.Metadata.Namespace + "/" + pvc.Metadata.Name
		v := checkupVolume{
			Namespace: pvc.Metadata.Namespace, Name: pvc.Metadata.Name, Phase: pvc.Status.Phase, StorageClass: pvc.Spec.StorageClassName,
			Capacity: parseQuantity(cmp.Or(pvc.Status.Capacity["storage"], pvc.Spec.Resources.Requests["storage"])),
		}
		f := checkupFinding{Namespace: v.Namespace, Name: v.Name, Extra: v.StorageClass, Since: milli(pvc.Metadata.CreationTimestamp)}

		if u, ok := use[key]; ok {
			v.Measured, v.Used, v.Capacity, v.Pod, v.Node = true, u.used, u.capacity, u.pod, u.node
			v.UsedPercent, v.InodesPercent = percentOf(u.used, u.capacity), percentOf(u.inodesUsed, u.inodes)
		}

		volumes = append(volumes, v)

		switch {
		case pvc.Metadata.DeletionTimestamp != nil:
			// The terminating section's.
		case pvc.Status.Phase == "Lost":
			f.Kind, f.Severity = findPVCLost, sevCritical
			findings = append(findings, f)
		case pvc.Status.Phase == "Pending":
			if !olderThan(pvc.Metadata.CreationTimestamp, now, pendingGrace) || (!mounted[key] && !immediate[v.StorageClass]) {
				continue
			}

			f.Kind, f.Severity = findPVCPending, sevWarning
			if mounted[key] {
				f.Severity = sevCritical
			}

			findings = append(findings, f)
		}

		findings = append(findings, fillFindings(v)...)
	}

	slices.SortStableFunc(volumes, func(a, b checkupVolume) int {
		return cmp.Or(cmp.Compare(b.UsedPercent, a.UsedPercent), strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	if len(volumes) > checkupMaxVolumes {
		volumes = volumes[:checkupMaxVolumes]
	}

	return volumes, findings
}

// fillFindings are a measured volume's levels past the thresholds.
func fillFindings(v checkupVolume) []checkupFinding {
	var out []checkupFinding

	level := func(kind string, percent, warn, critical float64) {
		if percent < warn {
			return
		}

		f := checkupFinding{Kind: kind, Severity: sevWarning, Namespace: v.Namespace, Name: v.Name, Node: v.Node, Extra: v.Pod, Value: percent, Limit: v.Capacity}
		if percent >= critical {
			f.Severity = sevCritical
		}

		out = append(out, f)
	}

	level(findVolumeFull, v.UsedPercent, volumeWarnPercent, volumeCriticalPercent)
	level(findVolumeInodes, v.InodesPercent, inodesWarnPercent, inodesCriticalPercent)

	return out
}

func pvFindings(pvs []pvObject) []checkupFinding {
	findings := []checkupFinding{}

	for _, pv := range pvs {
		f := checkupFinding{Name: pv.Metadata.Name, Reason: pv.Spec.ReclaimPolicy, Message: pv.Status.Message}
		if pv.Spec.ClaimRef != nil {
			f.Extra = pv.Spec.ClaimRef.Namespace + "/" + pv.Spec.ClaimRef.Name
		}

		switch pv.Status.Phase {
		case "Failed":
			f.Kind, f.Severity = findPVFailed, sevWarning
		case "Released":
			f.Kind, f.Severity = findPVReleased, sevInfo
		default:
			continue
		}

		findings = append(findings, f)
	}

	return findings
}
