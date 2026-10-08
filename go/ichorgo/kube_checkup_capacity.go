package ichorgo

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"
)

// The findings of the capacity section.
const (
	findNodeRequestsHigh = "nodeRequestsHigh" // Name: the node, Extra: cpu or memory, Value: percent requested
	findNodePodsFull     = "nodePodsFull"     // Count pods of Limit the kubelet takes
	// findNoRoomToDrain: the pods of the node (Name) would not fit on the others (Extra: cpu
	// or memory); Count is how many nodes are in that case.
	findNoRoomToDrain  = "noRoomToDrain"
	findQuotaNearLimit = "quotaNearLimit" // Name: the quota, Extra: the resource, Value: percent used
)

// The findings of the nodes section, each about the node in Name.
const (
	findNodeNotReady    = "nodeNotReady"    // Reason, Message: the Ready condition's
	findNodePressure    = "nodePressure"    // Extra: MemoryPressure, DiskPressure, PIDPressure or NetworkUnavailable
	findNodeCordoned    = "nodeCordoned"    // Since: when it was cordoned
	findNodeVersionSkew = "nodeVersionSkew" // Extra: its kubelet's version, Reason: the API server's
)

const (
	requestsWarnPercent = 90.0
	podsWarnPercent     = 95.0
	quotaWarnPercent    = 90.0
	// cordonedLong is when a cordoned node stops being maintenance in progress.
	cordonedLong = time.Hour
	// kubeletMaxSkew is how many minor versions a kubelet may be behind the API server.
	kubeletMaxSkew = 3

	taintUnschedulable = "node.kubernetes.io/unschedulable"
	roleLabelPrefix    = "node-role.kubernetes.io/"
)

// checkupNode is a node as Kubernetes sees it: what its pods request of what it offers,
// and what keeps pods away from it. CPU is in cores, memory in bytes.
type checkupNode struct {
	Name              string   `json:"name"`
	Roles             []string `json:"roles"`
	Ready             bool     `json:"ready"`
	Cordoned          bool     `json:"cordoned"`
	Kubelet           string   `json:"kubelet,omitempty"`
	Taints            []string `json:"taints"` // "key=value:Effect"
	Labels            []string `json:"labels"` // "key=value", sorted
	CPURequests       float64  `json:"cpuRequests"`
	CPUAllocatable    float64  `json:"cpuAllocatable"`
	CPUPercent        float64  `json:"cpuPercent"`
	MemoryRequests    float64  `json:"memoryRequests"`
	MemoryAllocatable float64  `json:"memoryAllocatable"`
	MemoryPercent     float64  `json:"memoryPercent"`
	Pods              int      `json:"pods"`
	PodCapacity       int      `json:"podCapacity"`
}

type quotaObject struct {
	Metadata checkMeta `json:"metadata"`
	Status   struct {
		Hard map[string]string `json:"hard"`
		Used map[string]string `json:"used"`
	} `json:"status"`
}

// checkupCapacity makes the capacity and nodes sections, and the node list both describe.
func checkupCapacity(ctx context.Context, k *kubeClient, in checkupInput, version string) (capacity, nodesSection checkupSection, nodes []checkupNode) {
	if in.nodesErr != nil {
		return newSection(checkCapacity, 0, nil, in.nodesErr), newSection(checkNodes, 0, nil, in.nodesErr), nil
	}

	quotas, quotaErr := listObjects[quotaObject](ctx, k, "/api/v1/resourcequotas")
	nodes = mapCheckupNodes(in.nodes, in.pods)

	findings := capacityFindings(nodes)
	findings = append(findings, quotaFindings(quotas)...)

	return newSection(checkCapacity, len(nodes)+len(quotas), findings, in.podsErr, quotaErr),
		newSection(checkNodes, len(nodes), nodeFindings(in.nodes, version, in.now)),
		nodes
}

func mapCheckupNodes(objs []kubeNodeObject, pods []checkPod) []checkupNode {
	index := map[string]int{}
	nodes := make([]checkupNode, 0, len(objs))

	for _, obj := range objs {
		n := checkupNode{
			Name: obj.Metadata.Name, Roles: obj.rawRoles(), Taints: obj.taintStrings(), Labels: []string{},
			Ready: obj.ready(), Cordoned: obj.Spec.Unschedulable,
			Kubelet:        obj.Status.NodeInfo.KubeletVersion,
			CPUAllocatable: parseQuantity(obj.Status.Allocatable["cpu"]), MemoryAllocatable: parseQuantity(obj.Status.Allocatable["memory"]),
			PodCapacity: int(parseQuantity(obj.Status.Allocatable["pods"])),
		}

		for key, value := range obj.Metadata.Labels {
			n.Labels = append(n.Labels, strings.TrimSuffix(key+"="+value, "="))
		}

		slices.Sort(n.Labels)

		index[n.Name] = len(nodes)
		nodes = append(nodes, n)
	}

	for _, p := range pods {
		i, ok := index[p.Node]
		if !ok || !p.running() {
			continue
		}

		nodes[i].Pods++
		nodes[i].CPURequests += p.CPU
		nodes[i].MemoryRequests += p.Memory
	}

	for i := range nodes {
		nodes[i].CPUPercent = percentOf(nodes[i].CPURequests, nodes[i].CPUAllocatable)
		nodes[i].MemoryPercent = percentOf(nodes[i].MemoryRequests, nodes[i].MemoryAllocatable)
	}

	return nodes
}

// schedulable tells whether ordinary pods may land on the node.
func (n checkupNode) schedulable() bool {
	return n.Ready && !n.Cordoned && !slices.ContainsFunc(n.Taints, func(t string) bool {
		return strings.HasSuffix(t, ":NoSchedule") || strings.HasSuffix(t, ":NoExecute")
	})
}

func capacityFindings(nodes []checkupNode) []checkupFinding {
	findings := []checkupFinding{}

	for _, n := range nodes {
		if n.CPUPercent >= requestsWarnPercent {
			findings = append(findings, checkupFinding{Kind: findNodeRequestsHigh, Severity: sevWarning, Name: n.Name, Extra: "cpu", Value: n.CPUPercent})
		}

		if n.MemoryPercent >= requestsWarnPercent {
			findings = append(findings, checkupFinding{Kind: findNodeRequestsHigh, Severity: sevWarning, Name: n.Name, Extra: "memory", Value: n.MemoryPercent})
		}

		if n.PodCapacity > 0 && percentOf(float64(n.Pods), float64(n.PodCapacity)) >= podsWarnPercent {
			findings = append(findings, checkupFinding{Kind: findNodePodsFull, Severity: sevWarning, Name: n.Name, Count: n.Pods, Limit: float64(n.PodCapacity)})
		}
	}

	if f, ok := drainFinding(nodes); ok {
		findings = append(findings, f)
	}

	return findings
}

// drainFinding names the node whose pods would least fit on the other schedulable nodes,
// by their requests: draining it (an upgrade, a failure) would leave pods Pending. A sum
// over the nodes, so a rough answer: it knows nothing of affinities nor of pod sizes.
func drainFinding(nodes []checkupNode) (checkupFinding, bool) {
	workers := slices.DeleteFunc(slices.Clone(nodes), func(n checkupNode) bool { return !n.schedulable() })
	if len(workers) < 2 {
		return checkupFinding{}, false
	}

	var freeCPU, freeMemory float64

	for _, n := range workers {
		freeCPU += max(0, n.CPUAllocatable-n.CPURequests)
		freeMemory += max(0, n.MemoryAllocatable-n.MemoryRequests)
	}

	worst := checkupFinding{Kind: findNoRoomToDrain, Severity: sevInfo}
	worstShort := 0.0

	for _, n := range workers {
		short, resource := 0.0, ""

		// What the others have free, against what this node's pods ask for.
		if others := freeCPU - max(0, n.CPUAllocatable-n.CPURequests); n.CPURequests > others && n.CPURequests > 0 {
			short, resource = (n.CPURequests-others)/n.CPURequests, "cpu"
		}

		if others := freeMemory - max(0, n.MemoryAllocatable-n.MemoryRequests); n.MemoryRequests > others && n.MemoryRequests > 0 {
			if s := (n.MemoryRequests - others) / n.MemoryRequests; s > short {
				short, resource = s, "memory"
			}
		}

		if resource == "" {
			continue
		}

		worst.Count++

		if short > worstShort {
			worstShort, worst.Name, worst.Extra = short, n.Name, resource
		}
	}

	return worst, worst.Count > 0
}

func quotaFindings(quotas []quotaObject) []checkupFinding {
	findings := []checkupFinding{}

	for _, q := range quotas {
		for _, resource := range slices.Sorted(maps.Keys(q.Status.Hard)) {
			hard, used := parseQuantity(q.Status.Hard[resource]), parseQuantity(q.Status.Used[resource])
			if percent := percentOf(used, hard); percent >= quotaWarnPercent {
				findings = append(findings, checkupFinding{
					Kind: findQuotaNearLimit, Severity: sevWarning, Namespace: q.Metadata.Namespace, Name: q.Metadata.Name,
					Extra: resource, Value: percent,
				})
			}
		}
	}

	return findings
}

// nodeFindings is what Kubernetes itself says of its nodes: not ready, under pressure,
// left cordoned, or with a kubelet too far from the API server's version.
func nodeFindings(nodes []kubeNodeObject, version string, now time.Time) []checkupFinding {
	findings := []checkupFinding{}
	major, minor, known := kubeMinor(version)

	for _, n := range nodes {
		conds := n.conditions()

		if ready := conds.get("Ready"); ready.Status != "True" {
			findings = append(findings, checkupFinding{Kind: findNodeNotReady, Severity: sevCritical, Name: n.Metadata.Name, Reason: ready.Reason, Message: ready.Message})
		}

		for _, typ := range []string{"MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable"} {
			if c := conds.get(typ); c.Status == "True" {
				findings = append(findings, checkupFinding{Kind: findNodePressure, Severity: sevWarning, Name: n.Metadata.Name, Extra: typ, Reason: c.Reason, Message: c.Message})
			}
		}

		if n.Spec.Unschedulable {
			f := checkupFinding{Kind: findNodeCordoned, Severity: sevInfo, Name: n.Metadata.Name}

			if since := n.cordonedSince(); since != nil {
				f.Since = milli(*since)

				if olderThan(*since, now, cordonedLong) {
					f.Severity = sevWarning
				}
			}

			findings = append(findings, f)
		}

		kMajor, kMinor, ok := kubeMinor(n.Status.NodeInfo.KubeletVersion)
		if !known || !ok || (kMajor == major && kMinor == minor) {
			continue
		}

		f := checkupFinding{Kind: findNodeVersionSkew, Severity: sevInfo, Name: n.Metadata.Name, Extra: n.Status.NodeInfo.KubeletVersion, Reason: version}
		// A kubelet must not be newer than the API server, nor more than three minors older.
		if kMajor != major || kMinor > minor || minor-kMinor > kubeletMaxSkew {
			f.Severity = sevWarning
		}

		findings = append(findings, f)
	}

	slices.SortStableFunc(findings, func(a, b checkupFinding) int { return cmp.Compare(a.Name, b.Name) })

	return findings
}
