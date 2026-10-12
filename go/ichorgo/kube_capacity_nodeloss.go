package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
)

// Losing a node: where its pods would land on the other schedulable nodes, which lower
// priority pods they would preempt, which would stay Pending, and what to scale down (lowest
// priority first, never a system class, within PodDisruptionBudgets) so they all fit. An
// estimate built on the capacity read, by requests, with the scheduler's main rules.

// systemPriority is where the system classes start (system-cluster-critical is 2000000000,
// system-node-critical 2000001000): pods at or above it are never preempted nor suggested.
const systemPriority = 1_000_000_000

// Why a pod stays Pending: the first resource no remaining node has room for.
const (
	pendingPods   = "pods"
	pendingMemory = "memory"
	pendingCPU    = "cpu"
)

// nodeLossAssumptions say what the simulation leaves out, for the apps to word it as an estimate.
var nodeLossAssumptions = []string{
	"Placement counts requests against allocatable only: affinity, topology spread, volume node affinity, tolerations and extended resources are ignored.",
	"Preemption picks the node whose evicted pods have the lowest highest priority, then the fewest PodDisruptionBudget violations; the scheduler weighs more.",
	"DaemonSet and static pods stay with their node: they are neither moved, preempted nor suggested.",
	"Suggestions free requests anywhere in the cluster; where the Pending pods then land is not simulated again.",
}

// nodeLossPlan is KubeCapacityNodeLoss's answer. CPU is in cores, memory in bytes. Lost:
// the requests and count of the pods that must move. Covered: the suggestions free at least
// what the Pending pods ask (true when nothing is Pending).
type nodeLossPlan struct {
	Node          string               `json:"node"`
	WorstCase     bool                 `json:"worstCase"`
	CPULost       float64              `json:"cpuLost"`
	MemoryLost    float64              `json:"memoryLost"`
	PodsLost      int                  `json:"podsLost"`
	DaemonSetPods int                  `json:"daemonSetPods"`
	Placed        []nodeLossPlaced     `json:"placed"`
	Preempted     []nodeLossPreempted  `json:"preempted"`
	Pending       []nodeLossPending    `json:"pending"`
	Suggestions   []nodeLossSuggestion `json:"suggestions"`
	Covered       bool                 `json:"covered"`
	Candidates    []nodeLossCandidate  `json:"candidates"`
	Assumptions   []string             `json:"assumptions"`
}

type nodeLossPlaced struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Workload  string `json:"workload"` // "Deployment/web"
	Priority  int32  `json:"priority"`
	To        string `json:"to"`
}

type nodeLossPreempted struct {
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	Workload          string `json:"workload"`
	Priority          int32  `json:"priority"`
	PriorityClassName string `json:"priorityClassName"`
	Node              string `json:"node"`
	By                string `json:"by"` // "namespace/pod" of the preemptor
	PDBViolated       bool   `json:"pdbViolated"`
}

type nodeLossPending struct {
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	Workload          string `json:"workload"`
	Priority          int32  `json:"priority"`
	PriorityClassName string `json:"priorityClassName"`
	QoS               string `json:"qos"`
	Reason            string `json:"reason"` // cpu, memory or pods
}

// nodeLossSuggestion: scale Kind/Namespace/Name from Replicas to ScaleTo. PDBBlocks: its
// PodDisruptionBudget lets no replica go, so ScaleTo is Replicas and it frees nothing.
type nodeLossSuggestion struct {
	Kind              string        `json:"kind"`
	Namespace         string        `json:"namespace"`
	Name              string        `json:"name"`
	Replicas          int           `json:"replicas"`
	ScaleTo           int           `json:"scaleTo"`
	Priority          int32         `json:"priority"`
	PriorityClassName string        `json:"priorityClassName"`
	NoPriorityClass   bool          `json:"noPriorityClass"`
	PDB               string        `json:"pdb"`
	PDBMinAvailable   int           `json:"pdbMinAvailable"`
	PDBBlocks         bool          `json:"pdbBlocks"`
	Frees             nodeLossFrees `json:"frees"`
}

type nodeLossFrees struct {
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	Pods   int     `json:"pods"`
}

type nodeLossCandidate struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Preempted int    `json:"preempted"`
}

// KubeCapacityNodeLoss simulates losing the Kubernetes node kubeNode, as JSON (see
// nodeLossPlan): where its pods land, what they preempt, what stays Pending and what to scale
// down. kubeNode "" simulates every schedulable node and answers the worst, with the others
// as candidates. It only reads, the same lists as KubeCapacity (same RBAC). kubeServer: see
// KubePods.
func KubeCapacityNodeLoss(configYAML, contextName, kubeServer, kubeNode string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(kubeNode))

	if kubeNode != "" && (!kubeNamePattern.MatchString(kubeNode) || strings.Contains(kubeNode, "..")) {
		return "", fmt.Errorf("invalid Kubernetes node name %q", kubeNode)
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() nodeLossPlan { return demoKubeNodeLoss(kubeNode) },
		func(ctx context.Context, k *kubeClient) (nodeLossPlan, error) {
			in, err := readCapacityInput(ctx, k)
			if err != nil {
				return nodeLossPlan{}, err
			}

			if kubeNode != "" && !slices.ContainsFunc(in.nodes, func(n checkNodeObject) bool { return n.Metadata.Name == kubeNode }) {
				return nodeLossPlan{}, fmt.Errorf("no Kubernetes node %q", kubeNode)
			}

			return simulateNodeLoss(in, kubeNode), nil
		})
}

// simulateNodeLoss is the plan for losing node, or the worst node when node is ""; pure.
func simulateNodeLoss(in capacityInput, node string) nodeLossPlan {
	s := newLossSim(in)

	if node != "" {
		plan := s.lose(node)
		plan.Candidates = []nodeLossCandidate{}

		return plan
	}

	var (
		worst      nodeLossPlan
		candidates = []nodeLossCandidate{}
	)

	for _, n := range s.nodes {
		if !n.schedulable() {
			continue
		}

		plan := s.lose(n.Name)
		candidates = append(candidates, nodeLossCandidate{Name: n.Name, Pending: len(plan.Pending), Preempted: len(plan.Preempted)})

		if len(candidates) == 1 || len(plan.Pending) > len(worst.Pending) ||
			len(plan.Pending) == len(worst.Pending) && len(plan.Preempted) > len(worst.Preempted) {
			worst = plan
		}
	}

	if len(candidates) == 0 {
		worst = emptyNodeLossPlan("")
	}

	worst.WorstCase, worst.Candidates = true, candidates

	return worst
}

func emptyNodeLossPlan(node string) nodeLossPlan {
	return nodeLossPlan{
		Node: node, Placed: []nodeLossPlaced{}, Preempted: []nodeLossPreempted{}, Pending: []nodeLossPending{},
		Suggestions: []nodeLossSuggestion{}, Covered: true, Candidates: []nodeLossCandidate{}, Assumptions: nodeLossAssumptions,
	}
}

// lossSim is the cluster as the simulation sees it, shared by every node it loses.
type lossSim struct {
	nodes     []capacityNode
	pods      []checkPod
	workloads []capacityWorkload
	owners    map[string]string    // ReplicaSet → controller, see controllerOwners
	never     map[string]bool      // classes with preemptionPolicy Never
	pdbs      map[string]pdbObject // "namespace/name"
	byKey     map[string]capacityWorkload
}

func newLossSim(in capacityInput) lossSim {
	pods := resolvePriorities(in.pods, in.classes)
	nodes := capacityNodes(mapCheckupNodes(in.nodes, pods), pods, kubeTopNodes{})
	s := lossSim{
		nodes: nodes, pods: pods, owners: controllerOwners(in.sets),
		workloads: capacityWorkloads(pods, in.sets, in.pdbs, map[string]kubeTopPod{}, nodes),
		never:     map[string]bool{}, pdbs: map[string]pdbObject{}, byKey: map[string]capacityWorkload{},
	}

	for _, c := range in.classes {
		s.never[c.Metadata.Name] = c.PreemptionPolicy == "Never"
		if c.GlobalDefault {
			s.never[""] = s.never[c.Metadata.Name]
		}
	}

	for _, b := range in.pdbs {
		s.pdbs[b.Metadata.Namespace+"/"+b.Metadata.Name] = b
	}

	for _, w := range s.workloads {
		s.byKey[w.Kind+"/"+w.Namespace+"/"+w.Name] = w
	}

	return s
}

// simPod is a pod being moved or a pod a preemption may evict.
type simPod struct {
	checkPod

	workload string // "Kind/name"
	key      string // "Kind/namespace/name", see lossSim.byKey
}

// simNode is a remaining node: its free room and the pods a preemption may evict from it.
type simNode struct {
	name        string
	cpu, memory float64
	slots       int
	pods        []simPod
}

func (n simNode) fits(p simPod) bool {
	return n.slots > 0 && n.cpu >= p.CPU && n.memory >= p.Memory
}

func (s lossSim) simPod(p checkPod) simPod {
	kind, name := podWorkload(p, s.owners)

	return simPod{checkPod: p, workload: kind + "/" + name, key: kind + "/" + p.Namespace + "/" + name}
}

// stays tells a pod that never leaves its node: a DaemonSet's or a static one.
func stays(p checkPod) bool {
	return p.Static || strings.HasPrefix(p.Owner, "DaemonSet/") || strings.HasPrefix(p.Owner, "Node/")
}

// lose simulates losing node: queue its pods, place, preempt, then suggest.
func (s lossSim) lose(node string) nodeLossPlan {
	plan := emptyNodeLossPlan(node)
	room := s.remaining(node)

	var queue []simPod

	for _, p := range s.pods {
		if p.Node != node || !p.running() {
			continue
		}

		if stays(p) {
			plan.DaemonSetPods++

			continue
		}

		queue = append(queue, s.simPod(p))
		plan.CPULost += p.CPU
		plan.MemoryLost += p.Memory
		plan.PodsLost++
	}

	// Each preemption evicts strictly lower priority pods, so the queue drains; the cap is a
	// guard against a bug, not a rule.
	for steps := 0; len(queue) > 0 && steps < 10*(len(s.pods)+1); steps++ {
		sortQueue(queue)
		p := queue[0]
		queue = queue[1:]

		if to := placeOn(room, p); to != "" {
			plan.Placed = append(plan.Placed, nodeLossPlaced{Namespace: p.Namespace, Name: p.Name, Workload: p.workload, Priority: p.Priority, To: to})

			continue
		}

		if evicted, on := s.preempt(room, p); on != "" {
			for _, e := range evicted {
				plan.Preempted = append(plan.Preempted, nodeLossPreempted{
					Namespace: e.Namespace, Name: e.Name, Workload: e.workload, Priority: e.Priority, PriorityClassName: e.PriorityClassName,
					Node: on, By: p.Namespace + "/" + p.Name, PDBViolated: s.byKey[e.key].PDBAllowed == 0,
				})
			}

			plan.Placed = append(plan.Placed, nodeLossPlaced{Namespace: p.Namespace, Name: p.Name, Workload: p.workload, Priority: p.Priority, To: on})
			queue = append(queue, evicted...)

			continue
		}

		plan.Pending = append(plan.Pending, nodeLossPending{
			Namespace: p.Namespace, Name: p.Name, Workload: p.workload, Priority: p.Priority,
			PriorityClassName: p.PriorityClassName, QoS: p.QoS, Reason: pendingReason(room, p),
		})
	}

	plan.Suggestions, plan.Covered = s.suggest(plan.Pending)

	return plan
}

// sortQueue is the scheduler's queue order (highest priority first), then the largest
// memory request first so the big pods find room before the small ones fill it.
func sortQueue(queue []simPod) {
	slices.SortStableFunc(queue, func(a, b simPod) int {
		return cmp.Or(cmp.Compare(b.Priority, a.Priority), cmp.Compare(b.Memory, a.Memory))
	})
}

// remaining is the schedulable nodes but node, with their free room and evictable pods.
func (s lossSim) remaining(node string) []simNode {
	var room []simNode

	for _, n := range s.nodes {
		if n.Name == node || !n.schedulable() {
			continue
		}

		r := simNode{name: n.Name, cpu: n.CPUFree, memory: n.MemoryFree, slots: n.PodsFree}

		for _, p := range s.pods {
			if p.Node == n.Name && p.running() && !stays(p) {
				r.pods = append(r.pods, s.simPod(p))
			}
		}

		room = append(room, r)
	}

	return room
}

// placeOn puts p on the node with the most memory left after it (LeastAllocated) and names
// it, "" when none fits.
func placeOn(room []simNode, p simPod) string {
	best := -1

	for i, n := range room {
		if n.fits(p) && (best < 0 || n.memory > room[best].memory) {
			best = i
		}
	}

	if best < 0 {
		return ""
	}

	room[best].add(p)

	return room[best].name
}

func (n *simNode) add(p simPod) {
	n.cpu -= p.CPU
	n.memory -= p.Memory
	n.slots--
	n.pods = append(n.pods, p)
}

func (n *simNode) remove(p simPod) {
	n.cpu += p.CPU
	n.memory += p.Memory
	n.slots++
	n.pods = slices.DeleteFunc(n.pods, func(q simPod) bool { return q.Namespace == p.Namespace && q.Name == p.Name })
}

// preemptable tells whether a pod of priority may be evicted to make room for one of victim:
// strictly lower, and never a system class.
func preemptable(p simPod, victim int32) bool {
	return p.Priority < victim && p.Priority < systemPriority && !strings.HasPrefix(p.PriorityClassName, "system-")
}

// preempt finds the node where evicting lower priority pods (lowest first) makes room for p,
// evicts them and places p. The node whose evicted set has the lowest highest priority wins,
// then the fewest PodDisruptionBudget violations, then the fewest pods. on is "" when p may
// not preempt (policy Never) or no node works.
func (s lossSim) preempt(room []simNode, p simPod) (evicted []simPod, on string) {
	if s.never[p.PriorityClassName] {
		return nil, ""
	}

	best, bestSet := -1, []simPod(nil)

	for i, n := range room {
		set, ok := evictionSet(n, p)
		if ok && (best < 0 || s.betterEviction(set, bestSet)) {
			best, bestSet = i, set
		}
	}

	if best < 0 {
		return nil, ""
	}

	for _, e := range bestSet {
		room[best].remove(e)
	}

	room[best].add(p)

	return bestSet, room[best].name
}

// evictionSet is the lowest priority pods of n to evict, in order, until p fits.
func evictionSet(n simNode, p simPod) ([]simPod, bool) {
	var lower []simPod

	for _, q := range n.pods {
		if preemptable(q, p.Priority) {
			lower = append(lower, q)
		}
	}

	slices.SortStableFunc(lower, func(a, b simPod) int {
		return cmp.Or(cmp.Compare(a.Priority, b.Priority), cmp.Compare(b.Memory, a.Memory))
	})

	for i, q := range lower {
		n.cpu, n.memory, n.slots = n.cpu+q.CPU, n.memory+q.Memory, n.slots+1
		if n.fits(p) {
			return lower[:i+1], true
		}
	}

	return nil, false
}

func (s lossSim) betterEviction(a, b []simPod) bool {
	top := func(set []simPod) int32 {
		return slices.MaxFunc(set, func(x, y simPod) int { return cmp.Compare(x.Priority, y.Priority) }).Priority
	}
	violations := func(set []simPod) int {
		n := 0

		for _, q := range set {
			if s.byKey[q.key].PDBAllowed == 0 {
				n++
			}
		}

		return n
	}

	return cmp.Or(cmp.Compare(top(a), top(b)), cmp.Compare(violations(a), violations(b)), cmp.Compare(len(a), len(b))) < 0
}

// pendingReason is the first resource no remaining node has: a pod slot, then memory, then CPU.
func pendingReason(room []simNode, p simPod) string {
	slot, memory := false, false

	for _, n := range room {
		if n.slots > 0 {
			slot = true
			memory = memory || n.memory >= p.Memory
		}
	}

	switch {
	case !slot:
		return pendingPods
	case !memory:
		return pendingMemory
	default:
		return pendingCPU
	}
}
