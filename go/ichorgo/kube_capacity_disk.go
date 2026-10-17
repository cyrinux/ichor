package ichorgo

import "math"

// The disk side of capacity: each node's ephemeral-storage allocatable and DiskPressure (from
// the node object), and its filesystems and its pods' ephemeral use (from the kubelet's
// stats/summary, through the API server's nodes/proxy).

// nodeDisk is what the kubelets said, by node and by pod.
type nodeDisk struct {
	nodes        map[string]kubeletSummary // the nodes that answered with a nodefs
	ephemeralUse map[string]float64        // "namespace/pod" → bytes used
	measured     bool                      // at least one node's disk is known
	forbidden    bool                      // a kubelet read was refused (403 on nodes/proxy)
}

// measureNodeDisk sorts the kubelets' answers: one that failed leaves its node unmeasured.
func measureNodeDisk(reads []kubeletRead) nodeDisk {
	d := nodeDisk{nodes: map[string]kubeletSummary{}, ephemeralUse: map[string]float64{}}

	for _, r := range reads {
		if r.err != nil {
			d.forbidden = d.forbidden || isForbidden(r.err)

			continue
		}

		if r.summary.Node.Fs.CapacityBytes > 0 {
			d.nodes[r.node] = r.summary
			d.measured = true
		}

		for _, p := range r.summary.Pods {
			d.ephemeralUse[p.PodRef.Namespace+"/"+p.PodRef.Name] += p.EphemeralStorage.UsedBytes
		}
	}

	return d
}

// withNodeDisk sets each measured node's filesystems.
func withNodeDisk(nodes []capacityNode, d nodeDisk) []capacityNode {
	out := make([]capacityNode, len(nodes))

	for i, n := range nodes {
		if s, ok := d.nodes[n.Name]; ok {
			n.DiskMeasured = true
			n.DiskCapacity, n.DiskAvailable = s.Node.Fs.CapacityBytes, s.Node.Fs.AvailableBytes
			n.ImageFsCapacity, n.ImageFsAvailable = s.Node.Runtime.ImageFs.CapacityBytes, s.Node.Runtime.ImageFs.AvailableBytes
		}

		out[i] = n
	}

	return out
}

// nodeEphemeral is a node's ephemeral-storage allocatable (0 when it does not say) and
// whether its DiskPressure condition is on, by node name.
type nodeEphemeral struct {
	allocatable float64
	pressure    bool
}

func nodeEphemerals(objs []checkNodeObject) map[string]nodeEphemeral {
	out := make(map[string]nodeEphemeral, len(objs))

	for _, o := range objs {
		out[o.Metadata.Name] = nodeEphemeral{
			allocatable: parseQuantity(o.Status.Allocatable[ephemeralStorage]),
			pressure:    kubeConditions(o.Status.Conditions).is("DiskPressure"),
		}
	}

	return out
}

// ephemeralFits is how many pods asking request fit in free of allocatable: unbounded
// (MaxInt) when the pod asks none or the node's allocatable is unknown (0).
func ephemeralFits(request, free, allocatable float64) int {
	if request <= 0 || allocatable <= 0 {
		return math.MaxInt
	}

	return int(math.Floor(free / request))
}
