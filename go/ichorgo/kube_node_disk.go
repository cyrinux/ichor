package ichorgo

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"
)

// The background monitor's view of a Kubernetes cluster's node disks: each ready node's
// nodefs and imagefs from its kubelet's stats/summary, and its DiskPressure condition, so a
// node filling up alerts before the kubelet evicts pods (nodefs.available < 10 %).

const (
	// nodeDiskParallel bounds the kubelets asked at once on a monitor run, nodeDiskTimeout
	// each one, nodeDiskTotalTimeout all of them: the monitor runs often and must stay cheap,
	// and the read must leave the caller's time to its other reads.
	nodeDiskParallel     = 4
	nodeDiskTimeout      = 5 * time.Second
	nodeDiskTotalTimeout = 10 * time.Second
	// nodeDiskMaxNodes: above it the monitor does not read the kubelets at all (Skipped).
	nodeDiskMaxNodes = 200
)

// kubeNodeDisk is KubeNodeDisk's answer. Nodes holds the nodes whose kubelet answered with a
// nodefs: one that did not is absent, never full, unless it reports DiskPressure, then it is
// listed with a zero size (CapacityBytes 0, not measured). Forbidden: a kubelet read (or the
// node list) was refused, a missing reading rather than a problem. Skipped: the cluster has
// more than nodeDiskMaxNodes nodes and no kubelet was asked. Measured counts the nodes with a size.
type kubeNodeDisk struct {
	Nodes     []kubeNodeDiskUse `json:"nodes"`
	Forbidden bool              `json:"forbidden"`
	Skipped   bool              `json:"skipped"`
	Measured  int               `json:"measured"`
}

// kubeNodeDiskUse is one node's disk. UsedPercent is the nodefs's, 0..100;
// ImageFsUsedPercent the image filesystem's (0 when the kubelet does not report one). Bytes
// are whole numbers.
type kubeNodeDiskUse struct {
	Name               string `json:"name"`
	UsedPercent        int    `json:"usedPercent"`
	AvailableBytes     int64  `json:"availableBytes"`
	CapacityBytes      int64  `json:"capacityBytes"`
	ImageFsUsedPercent int    `json:"imageFsUsedPercent"`
	DiskPressure       bool   `json:"diskPressure"`
}

// KubeNodeDisk is each ready node's disk fill and DiskPressure as JSON (see kubeNodeDisk),
// for the background monitor. It only reads. RBAC: get nodes, get nodes/proxy.
// kubeServer: see KubePods.
func KubeNodeDisk(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, demoKubeNodeDiskFor(target), readNodeDisk)
}

func readNodeDisk(ctx context.Context, k *kubeClient) (kubeNodeDisk, error) {
	nodes, err := listObjects[checkNodeObject](ctx, k, "/api/v1/nodes")
	if isForbidden(err) {
		return kubeNodeDisk{Nodes: []kubeNodeDiskUse{}, Forbidden: true}, nil
	}

	if err != nil {
		return kubeNodeDisk{}, err
	}

	if len(nodes) > nodeDiskMaxNodes {
		return kubeNodeDisk{Nodes: []kubeNodeDiskUse{}, Skipped: true}, nil
	}

	readCtx, cancel := context.WithTimeout(ctx, nodeDiskTotalTimeout)
	defer cancel()

	reads := readKubeletSummariesWithin(readCtx, k, nodes, nil, nodeDiskParallel, nodeDiskTimeout)

	return nodeDiskOf(nodes, reads), nil
}

// nodeDiskOf joins the kubelets' answers with the nodes' DiskPressure, by node name. A node
// reporting DiskPressure whose kubelet gave no size (refused, failed, timed out, not ready) is
// listed with a zero size, so its condition still reaches the monitor.
func nodeDiskOf(nodes []checkNodeObject, reads []kubeletRead) kubeNodeDisk {
	pressure := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		pressure[n.Metadata.Name] = kubeConditions(n.Status.Conditions).is("DiskPressure")
	}

	out := kubeNodeDisk{Nodes: []kubeNodeDiskUse{}}
	sized := make(map[string]bool, len(reads))

	for _, r := range reads {
		if r.err != nil {
			out.Forbidden = out.Forbidden || isForbidden(r.err)

			continue
		}

		fs := r.summary.Node.Fs
		if fs.CapacityBytes <= 0 {
			continue
		}

		out.Nodes = append(out.Nodes, kubeNodeDiskUse{
			Name:               r.node,
			UsedPercent:        fsUsedPercent(fs),
			AvailableBytes:     int64(max(fs.AvailableBytes, 0)),
			CapacityBytes:      int64(fs.CapacityBytes),
			ImageFsUsedPercent: fsUsedPercent(r.summary.Node.Runtime.ImageFs),
			DiskPressure:       pressure[r.node],
		})
		sized[r.node] = true
	}

	out.Measured = len(out.Nodes)

	for _, n := range nodes {
		if name := n.Metadata.Name; pressure[name] && !sized[name] {
			out.Nodes = append(out.Nodes, kubeNodeDiskUse{Name: name, DiskPressure: true})
		}
	}

	slices.SortFunc(out.Nodes, func(a, b kubeNodeDiskUse) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// fsUsedPercent is how full fs is, a whole percent in 0..100, rounded down so a node never
// reads above a threshold it is under. The used side is the larger of
// usedBytes and capacity minus available: the kubelet evicts on available, which also counts
// the blocks reserved for root.
func fsUsedPercent(fs kubeletFsStats) int {
	if fs.CapacityBytes <= 0 {
		return 0
	}

	used := max(fs.UsedBytes, fs.CapacityBytes-fs.AvailableBytes)
	percent := math.Floor(used / fs.CapacityBytes * 100)

	return int(min(max(percent, 0), 100))
}

// demoNodeDiskUsed is the nodefs fill of each demo node but the last, by position; the last
// one is at demoNodeDiskFull, past the default warning threshold.
var demoNodeDiskUsed = []int{42, 57, 36, 63}

const (
	demoNodeDiskFull     = 91
	demoNodeDiskCapacity = int64(100 << 30)
)

// demoKubeNodeDiskFor is the demo cluster's node disks: every node measured, one at 91 %.
func demoKubeNodeDiskFor(target kubeTarget) func() kubeNodeDisk {
	return func() kubeNodeDisk {
		nodes := demoKubeNodesFor(target)().Nodes
		out := kubeNodeDisk{Nodes: make([]kubeNodeDiskUse, 0, len(nodes))}

		for i, n := range nodes {
			used := demoNodeDiskUsed[i%len(demoNodeDiskUsed)]
			if i == len(nodes)-1 {
				used = demoNodeDiskFull
			}

			out.Nodes = append(out.Nodes, kubeNodeDiskUse{
				Name: n.Name, UsedPercent: used, CapacityBytes: demoNodeDiskCapacity,
				AvailableBytes:     demoNodeDiskCapacity / 100 * int64(100-used),
				ImageFsUsedPercent: used / 2,
			})
		}

		slices.SortFunc(out.Nodes, func(a, b kubeNodeDiskUse) int { return strings.Compare(a.Name, b.Name) })
		out.Measured = len(out.Nodes)

		return out
	}
}
