package ichorgo

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// qosRank orders the QoS classes from the first evicted.
var qosRank = map[string]int{qosBestEffort: 0, qosBurstable: 1, qosGuaranteed: 2}

// capacityNodes adds to each node its pods' limits, its usage (metrics-server) and its free room.
func capacityNodes(nodes []checkupNode, pods []checkPod, top kubeTopNodes) []capacityNode {
	usage := map[string]kubeTopNode{}
	for _, n := range top.Nodes {
		usage[n.Name] = n
	}

	limits := map[string][2]float64{}

	for _, p := range pods {
		if p.Node == "" || !p.running() {
			continue
		}

		l := limits[p.Node]
		limits[p.Node] = [2]float64{l[0] + p.CPULimit, l[1] + p.MemoryLimit}
	}

	out := make([]capacityNode, 0, len(nodes))

	for _, n := range nodes {
		c := capacityNode{
			checkupNode: n,
			CPULimits:   limits[n.Name][0], MemoryLimits: limits[n.Name][1],
			CPUUsage: usage[n.Name].CPU, MemoryUsage: usage[n.Name].Memory,
			CPUFree:    max(0, n.CPUAllocatable-n.CPURequests),
			MemoryFree: max(0, n.MemoryAllocatable-n.MemoryRequests),
			PodsFree:   max(0, n.PodCapacity-n.Pods),
		}
		c.Overcommitted = n.MemoryAllocatable > 0 && c.MemoryLimits > n.MemoryAllocatable
		out = append(out, c)
	}

	return out
}

func capacityTotalsOf(nodes []capacityNode) capacityTotals {
	t := capacityTotals{Nodes: len(nodes)}

	for _, n := range nodes {
		t.CPUAllocatable += n.CPUAllocatable
		t.CPURequests += n.CPURequests
		t.CPULimits += n.CPULimits
		t.CPUUsage += n.CPUUsage
		t.MemoryAllocatable += n.MemoryAllocatable
		t.MemoryRequests += n.MemoryRequests
		t.MemoryLimits += n.MemoryLimits
		t.MemoryUsage += n.MemoryUsage
		t.Pods += n.Pods
		t.PodCapacity += n.PodCapacity

		if n.schedulable() {
			t.SchedulableNodes++
		}
	}

	return t
}

// workloadAcc is a workload being summed up from its pods.
type workloadAcc struct {
	capacityWorkload

	cpuUnbounded, memoryUnbounded, nearLimit bool
	labels                                   labelSet
}

// podWorkload is the controller a pod belongs to: a ReplicaSet's Deployment (through sets),
// the StatefulSet, DaemonSet or Job, else the pod itself.
func podWorkload(p checkPod, sets map[string]string) (kind, name string) {
	kind, name, ok := strings.Cut(p.Owner, "/")

	switch {
	case !ok || kind == "Node": // no owner, or a static pod mirrored by its kubelet
		return "Pod", p.Name
	case kind == "ReplicaSet":
		if owner, found := sets[p.Namespace+"/"+name]; found {
			kind, name, _ = strings.Cut(owner, "/")
		}
	}

	return kind, name
}

// controllerOwners maps each ReplicaSet ("ns/name") to its controller ("Deployment/web").
func controllerOwners(sets []capacityReplicaSet) map[string]string {
	out := map[string]string{}

	for _, s := range sets {
		for _, o := range s.Metadata.OwnerReferences {
			if o.Controller != nil && *o.Controller {
				out[s.Metadata.Namespace+"/"+s.Metadata.Name] = o.Kind + "/" + o.Name
			}
		}
	}

	return out
}

// capacityWorkloads groups the running pods by controller, what goes first first.
func capacityWorkloads(pods []checkPod, sets []capacityReplicaSet, pdbs []pdbObject, usage map[string]kubeTopPod, nodes []capacityNode) []capacityWorkload {
	owners := controllerOwners(sets)
	index := map[string]*workloadAcc{}

	var order []*workloadAcc

	for _, p := range pods {
		if !p.running() {
			continue
		}

		kind, name := podWorkload(p, owners)
		key := kind + "/" + p.Namespace + "/" + name

		w, ok := index[key]
		if !ok {
			w = &workloadAcc{capacityWorkload: capacityWorkload{
				Kind: kind, Namespace: p.Namespace, Name: name, PDBAllowed: -1,
				PriorityClassName: p.PriorityClassName, Priority: p.Priority, NoPriorityClass: p.PriorityClassName == "",
				QoS: p.QoS,
			}, labels: p.Labels}
			index[key] = w
			order = append(order, w)
		}

		addPod(w, p, usage[p.Namespace+"/"+p.Name])
	}

	out := make([]capacityWorkload, 0, len(order))

	for _, w := range order {
		finishWorkload(w, pdbs, nodes)
		out = append(out, w.capacityWorkload)
	}

	sortWorkloads(out)

	return out
}

// sortWorkloads puts what goes first first: lowest priority, no class before a class of the
// same value, then the most memory requested.
func sortWorkloads(workloads []capacityWorkload) {
	slices.SortStableFunc(workloads, func(a, b capacityWorkload) int {
		return cmp.Or(
			cmp.Compare(a.Priority, b.Priority),
			-cmpBool(a.NoPriorityClass, b.NoPriorityClass),
			cmp.Compare(b.MemoryRequest, a.MemoryRequest),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}

// cmpBool orders false before true.
func cmpBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

func addPod(w *workloadAcc, p checkPod, use kubeTopPod) {
	w.Replicas++
	w.CPURequest = max(w.CPURequest, p.CPU)
	w.MemoryRequest = max(w.MemoryRequest, p.Memory)
	w.CPULimit = max(w.CPULimit, p.CPULimit)
	w.MemoryLimit = max(w.MemoryLimit, p.MemoryLimit)
	w.cpuUnbounded = w.cpuUnbounded || p.CPULimit == 0
	w.memoryUnbounded = w.memoryUnbounded || p.MemoryLimit == 0
	w.CPUUsage += use.CPU
	w.MemoryUsage += use.Memory

	if qosRank[p.QoS] < qosRank[w.QoS] {
		w.QoS = p.QoS
	}

	if strings.HasPrefix(p.LastTermination, "OOMKilled") {
		w.OOMKills++
	}

	if p.MemoryLimit > 0 && percentOf(use.Memory, p.MemoryLimit) >= oomNearPercent {
		w.nearLimit = true
	}
}

func finishWorkload(w *workloadAcc, pdbs []pdbObject, nodes []capacityNode) {
	if w.cpuUnbounded {
		w.CPULimit = 0
	}

	if w.memoryUnbounded {
		w.MemoryLimit = 0
	}

	for _, b := range pdbs {
		if b.Metadata.Namespace == w.Namespace && b.Spec.Selector != nil && b.Spec.Selector.matches(w.labels) {
			w.PDB, w.PDBAllowed = b.Metadata.Name, b.Status.DisruptionsAllowed

			break
		}
	}

	switch {
	case w.OOMKills > 0 || w.nearLimit:
		w.Risk = riskOOM
	case w.QoS == qosBestEffort:
		w.Risk = riskEvictFirst
	case w.QoS == qosBurstable && w.MemoryLimit == 0:
		w.Risk = riskUnbounded
	default:
		w.Risk = riskOK
	}

	// A DaemonSet runs once per node, a bare pod is not scaled: "more" means nothing for them.
	if w.Kind == "DaemonSet" || w.Kind == "Pod" {
		w.MoreFitNA = true

		return
	}

	w.MoreFit = moreFit(w.CPURequest, w.MemoryRequest, nodes)
}

// moreFit is how many more pods asking cpu and memory fit on the schedulable nodes' free room.
func moreFit(cpu, memory float64, nodes []capacityNode) int {
	total := 0

	for _, n := range nodes {
		if !n.schedulable() {
			continue
		}

		fit := n.PodsFree
		if cpu > 0 {
			fit = min(fit, int(math.Floor(n.CPUFree/cpu)))
		}

		if memory > 0 {
			fit = min(fit, int(math.Floor(n.MemoryFree/memory)))
		}

		total += max(0, fit)
	}

	return total
}

// nPlusOne places the pods of each schedulable node in turn on the other schedulable nodes'
// free room (largest memory request first, first node that fits) and reports the first node
// whose pods would not all find a place. DaemonSet and static pods stay with their node.
func nPlusOne(nodes []capacityNode, pods []checkPod) capacityNPlusOne {
	var schedulable []capacityNode

	for _, n := range nodes {
		if n.schedulable() {
			schedulable = append(schedulable, n)
		}
	}

	for i, lost := range schedulable {
		var moving []checkPod

		for _, p := range pods {
			if p.Node == lost.Name && p.running() && !p.Static && !strings.HasPrefix(p.Owner, "DaemonSet/") {
				moving = append(moving, p)
			}
		}

		slices.SortStableFunc(moving, func(a, b checkPod) int { return cmp.Compare(b.Memory, a.Memory) })

		room := slices.Delete(slices.Clone(schedulable), i, i+1)
		verdict := capacityNPlusOne{Node: lost.Name}

		for _, p := range moving {
			if !place(room, p) {
				verdict.PendingPods++
				verdict.ShortCPU += p.CPU
				verdict.ShortMemory += p.Memory
			}
		}

		if verdict.PendingPods > 0 {
			return verdict
		}
	}

	return capacityNPlusOne{OK: true}
}

// place puts p on the first node of room with enough free CPU, memory and pod slots.
func place(room []capacityNode, p checkPod) bool {
	for j := range room {
		n := &room[j]
		if n.PodsFree > 0 && n.CPUFree >= p.CPU && n.MemoryFree >= p.Memory {
			n.PodsFree--
			n.CPUFree -= p.CPU
			n.MemoryFree -= p.Memory

			return true
		}
	}

	return false
}
