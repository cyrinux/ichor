package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// groupCeph is Rook's API group (cephclusters, cephblockpools, cephfilesystems, cephobjectstores).
const groupCeph = "ceph.rook.io"

type cephStatus struct {
	Version  string        `json:"version"`
	Error    string        `json:"error"`
	Clusters []cephCluster `json:"clusters"`
	Pools    []cephPool    `json:"pools"`
	OSDs     []cephOSD     `json:"osds"`
}

type cephCluster struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Phase is Rook's own word: Ready (Connected when external), Progressing, Failure...
	Phase      string      `json:"phase"`
	Message    string      `json:"message"`
	CephHealth string      `json:"cephHealth"` // HEALTH_OK|HEALTH_WARN|HEALTH_ERR, "" before Ceph reported
	Health     string      `json:"health"`     // critical|warning|ok
	Reasons    []string    `json:"reasons"`    // see cephReason*
	Checks     []cephCheck `json:"checks"`     // Ceph's health checks, errors first
	BytesTotal int64       `json:"bytesTotal"`
	BytesUsed  int64       `json:"bytesUsed"`
	OSDsUp     int         `json:"osdsUp"` // OSD pods ready
	OSDsTotal  int         `json:"osdsTotal"`
	MonsReady  int         `json:"monsReady"`
	MonsTotal  int         `json:"monsTotal"`
	// NotReadyNodes are the nodes of its OSD and mon pods that are not ready, for the likely cause.
	NotReadyNodes []string `json:"notReadyNodes"`
	Version       string   `json:"version"` // Ceph's, e.g. 19.2.3-0
	External      bool     `json:"external"`
}

type cephCheck struct {
	Name     string `json:"name"`     // MON_DOWN, OSD_NEARFULL...
	Severity string `json:"severity"` // HEALTH_WARN|HEALTH_ERR
	Message  string `json:"message"`
}

// cephPool is a block pool, a filesystem or an object store.
type cephPool struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // blockPool|filesystem|objectStore
	Phase     string `json:"phase"`
	Health    string `json:"health"` // critical on Failure, warning when not Ready
}

type cephOSD struct {
	Namespace string `json:"namespace"` // the cluster's
	ID        string `json:"id"`        // the ceph-osd-id label
	Pod       string `json:"pod"`
	Node      string `json:"node"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
}

// Reasons a Ceph cluster is not ok, for the app to word.
const (
	cephReasonHealthErr  = "healthErr"  // Ceph reports HEALTH_ERR
	cephReasonFailure    = "failure"    // Rook's phase is Failure
	cephReasonFull       = "full"       // more than 95% used
	cephReasonNoOSD      = "noOSD"      // no OSD pod ready
	cephReasonNoQuorum   = "noQuorum"   // half the mons or more not ready
	cephReasonHealthWarn = "healthWarn" // Ceph reports HEALTH_WARN
	cephReasonNearFull   = "nearFull"   // more than 85% used
	cephReasonOSDs       = "osds"       // an OSD pod not ready
	cephReasonMons       = "mons"       // a mon pod not ready
	cephReasonNotReady   = "notReady"   // Rook's phase is not Ready, without anything else wrong
)

// Ceph's own default ratios: nearfull at 85%, full (writes blocked) at 95%.
const (
	cephNearFullRatio = 0.85
	cephFullRatio     = 0.95
)

type cephClusterObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		External struct {
			Enable bool `json:"enable"`
		} `json:"external"`
	} `json:"spec"`
	Status struct {
		Phase   string `json:"phase"`
		Message string `json:"message"`
		Ceph    *struct {
			Health  string `json:"health"`
			Details map[string]struct {
				Severity string `json:"severity"`
				Message  string `json:"message"`
			} `json:"details"`
			Capacity struct {
				BytesTotal int64 `json:"bytesTotal"`
				BytesUsed  int64 `json:"bytesUsed"`
			} `json:"capacity"`
		} `json:"ceph"`
		Version *struct {
			Version string `json:"version"`
		} `json:"version"`
	} `json:"status"`
}

// cephPoolObject is the shape cephblockpools, cephfilesystems and cephobjectstores share.
type cephPoolObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// readCeph lists the Ceph clusters, pools, filesystems and object stores Rook manages, and
// the OSD and mon pods (labelled app=rook-ceph-osd with ceph-osd-id, app=rook-ceph-mon) of
// each cluster's namespace.
func readCeph(ctx context.Context, k *kubeClient, version string) *cephStatus {
	base := "/apis/" + groupCeph + "/" + version + "/"

	var (
		clusters    kubeList[cephClusterObject]
		blockPools  kubeList[cephPoolObject]
		filesystems kubeList[cephPoolObject]
		objStores   kubeList[cephPoolObject]
		pods        []dsPod
		errs        = make([]error, 5)
		wg          sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, base+"cephclusters", &clusters) })
	wg.Go(func() { errs[1] = k.get(ctx, base+"cephblockpools", &blockPools) })
	wg.Go(func() { errs[2] = k.get(ctx, base+"cephfilesystems", &filesystems) })
	wg.Go(func() { errs[3] = k.get(ctx, base+"cephobjectstores", &objStores) })
	wg.Go(func() { pods, errs[4] = listDSPods(ctx, k, "app in (rook-ceph-osd,rook-ceph-mon)") })
	wg.Wait()

	out := mapCeph(clusters.Items, map[string][]cephPoolObject{
		"blockPool": blockPools.Items, "filesystem": filesystems.Items, "objectStore": objStores.Items,
	}, pods)
	out.Version = version
	out.Error = sectionError(errs...)

	return out
}

func mapCeph(clusters []cephClusterObject, pools map[string][]cephPoolObject, pods []dsPod) *cephStatus {
	out := &cephStatus{Clusters: []cephCluster{}, Pools: []cephPool{}, OSDs: []cephOSD{}}

	// Rook runs a cluster's daemons in its namespace.
	mons := map[string][]dsPod{}

	for _, p := range pods {
		switch p.Metadata.Labels["app"] {
		case "rook-ceph-osd":
			out.OSDs = append(out.OSDs, cephOSD{
				Namespace: p.Metadata.Namespace, ID: p.Metadata.Labels["ceph-osd-id"], Pod: p.Metadata.Name,
				Node: p.Spec.NodeName, Phase: p.Status.Phase, Ready: p.containerReady(""),
			})
		case "rook-ceph-mon":
			// A canary only tries a mon's placement: it is not a mon of the quorum.
			if p.Metadata.Labels["mon_canary"] != "true" {
				mons[p.Metadata.Namespace] = append(mons[p.Metadata.Namespace], p)
			}
		}
	}

	// By namespace, then OSD number.
	slices.SortFunc(out.OSDs, func(a, b cephOSD) int {
		if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}

		ai, _ := strconv.Atoi(a.ID)
		bi, _ := strconv.Atoi(b.ID)

		return cmp.Or(ai-bi, strings.Compare(a.Pod, b.Pod))
	})

	for _, obj := range clusters {
		out.Clusters = append(out.Clusters, mapCephCluster(obj, out.OSDs, mons[obj.Metadata.Namespace]))
	}

	slices.SortFunc(out.Clusters, func(a, b cephCluster) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	for kind, objs := range pools {
		for _, obj := range objs {
			out.Pools = append(out.Pools, cephPool{
				Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Kind: kind,
				Phase: obj.Status.Phase, Health: cephPoolHealth(obj.Status.Phase),
			})
		}
	}

	slices.SortFunc(out.Pools, func(a, b cephPool) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name+"/"+a.Kind, b.Namespace+"/"+b.Name+"/"+b.Kind)
	})

	return out
}

// cephPoolHealth: Failure is critical, anything but Ready (Connected for an external one) a warning.
func cephPoolHealth(phase string) string {
	switch phase {
	case "Ready", "Connected":
		return healthOK
	case "Failure":
		return healthCritical
	default:
		return healthWarning
	}
}

func mapCephCluster(obj cephClusterObject, osds []cephOSD, mons []dsPod) cephCluster {
	st := obj.Status
	c := cephCluster{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Phase: st.Phase, Message: st.Message,
		External: obj.Spec.External.Enable, Checks: []cephCheck{}, NotReadyNodes: []string{},
	}

	if st.Version != nil {
		c.Version = st.Version.Version
	}

	if st.Ceph != nil {
		c.CephHealth = st.Ceph.Health
		c.BytesTotal, c.BytesUsed = st.Ceph.Capacity.BytesTotal, st.Ceph.Capacity.BytesUsed

		for name, d := range st.Ceph.Details {
			c.Checks = append(c.Checks, cephCheck{Name: name, Severity: d.Severity, Message: d.Message})
		}

		// HEALTH_ERR before HEALTH_WARN, then by name.
		slices.SortFunc(c.Checks, func(a, b cephCheck) int {
			errA, errB := a.Severity == "HEALTH_ERR", b.Severity == "HEALTH_ERR"
			if errA != errB {
				if errA {
					return -1
				}

				return 1
			}

			return strings.Compare(a.Name, b.Name)
		})
	}

	// An external cluster's daemons run elsewhere: no pod of its own to count.
	if !c.External {
		notReady := func(node string) {
			if node != "" && !slices.Contains(c.NotReadyNodes, node) {
				c.NotReadyNodes = append(c.NotReadyNodes, node)
			}
		}

		for _, o := range osds {
			if o.Namespace != c.Namespace {
				continue
			}

			c.OSDsTotal++
			if o.Ready {
				c.OSDsUp++
			} else {
				notReady(o.Node)
			}
		}

		for _, m := range mons {
			c.MonsTotal++
			if m.containerReady("") {
				c.MonsReady++
			} else {
				notReady(m.Spec.NodeName)
			}
		}

		slices.Sort(c.NotReadyNodes)
	}

	c.Health, c.Reasons = cephClusterHealth(c)

	return c
}

func cephClusterHealth(c cephCluster) (string, []string) {
	reasons := []string{}
	critical := false

	crit := func(reason string) { reasons, critical = append(reasons, reason), true }

	used := 0.0
	if c.BytesTotal > 0 {
		used = float64(c.BytesUsed) / float64(c.BytesTotal)
	}

	if c.CephHealth == "HEALTH_ERR" {
		crit(cephReasonHealthErr)
	}

	if c.Phase == "Failure" {
		crit(cephReasonFailure)
	}

	if used > cephFullRatio {
		crit(cephReasonFull)
	}

	if c.OSDsTotal > 0 && c.OSDsUp == 0 {
		crit(cephReasonNoOSD)
	}

	// Without a majority of mons Ceph stops answering, and its last reported health goes stale.
	if c.MonsTotal > 0 && c.MonsReady*2 <= c.MonsTotal {
		crit(cephReasonNoQuorum)
	}

	if c.CephHealth == "HEALTH_WARN" {
		reasons = append(reasons, cephReasonHealthWarn)
	}

	if used > cephNearFullRatio && used <= cephFullRatio {
		reasons = append(reasons, cephReasonNearFull)
	}

	if c.OSDsUp > 0 && c.OSDsUp < c.OSDsTotal {
		reasons = append(reasons, cephReasonOSDs)
	}

	if c.MonsReady*2 > c.MonsTotal && c.MonsReady < c.MonsTotal {
		reasons = append(reasons, cephReasonMons)
	}

	if c.Phase != "" && c.Phase != "Ready" && c.Phase != "Connected" && len(reasons) == 0 {
		reasons = append(reasons, cephReasonNotReady)
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
