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
// the requests and count of the pods that must move. A preempted pod that finds room
// elsewhere is in Placed too. Covered: the suggestions free at least what the Pending pods
// ask beyond the room left (true when nothing is Pending).
type nodeLossPlan struct {
	Node          string               `json:"node"`  // the lost node, or the joined names when several are lost
	Nodes         []string             `json:"nodes"` // the explicit selection; empty for the worst case
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

// KubeCapacityNodeLoss simulates losing the Kubernetes nodes of the comma-separated
// kubeNode list, as JSON (see nodeLossPlan): where their pods land, what they preempt, what
// stays Pending and what to scale down. One name behaves like a single lost node; ""
// simulates every schedulable node and answers the worst, with the others as candidates. It
// only reads, the same lists as KubeCapacity (same RBAC). kubeServer: see KubePods.
func KubeCapacityNodeLoss(configYAML, contextName, kubeServer, kubeNode string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, kubeNode = unmaskTargets(configYAML, contextName, kubeNode)

	nodes, err := parseKubeNodeList(kubeNode)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() nodeLossPlan { return demoKubeNodeLoss(nodes) },
		func(ctx context.Context, k *kubeClient) (nodeLossPlan, error) {
			in, err := readCapacityInput(ctx, k)
			if err != nil {
				return nodeLossPlan{}, err
			}

			for _, node := range nodes {
				if !slices.ContainsFunc(in.nodes, func(n checkNodeObject) bool { return n.Metadata.Name == node }) {
					return nodeLossPlan{}, fmt.Errorf("no Kubernetes node %q", node)
				}
			}

			return simulateNodeLoss(in, nodes), nil
		})
}

// parseKubeNodeList reads a comma-separated node list: "" is no node (the worst case), one
// name a single lost node. Empty elements and duplicates are refused.
func parseKubeNodeList(kubeNode string) ([]string, error) {
	if strings.TrimSpace(kubeNode) == "" {
		return nil, nil
	}

	var nodes []string

	for _, part := range strings.Split(kubeNode, ",") {
		name := strings.TrimSpace(part)

		switch {
		case name == "":
			return nil, fmt.Errorf("invalid Kubernetes node list %q: empty node", kubeNode)
		case slices.Contains(nodes, name):
			return nil, fmt.Errorf("duplicate Kubernetes node %q", name)
		case !kubeNamePattern.MatchString(name) || strings.Contains(name, ".."):
			return nil, fmt.Errorf("invalid Kubernetes node name %q", name)
		}

		nodes = append(nodes, name)
	}

	return nodes, nil
}

// simulateNodeLoss is the plan for losing nodes, or the worst node when the list is empty;
// pure.
func simulateNodeLoss(in capacityInput, nodes []string) nodeLossPlan {
	s := newLossSim(in)

	if len(nodes) > 0 {
		plan := s.lose(nodes...)
		plan.Candidates = []nodeLossCandidate{}
		plan.Nodes = nodes

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

	worst.WorstCase, worst.Candidates, worst.Nodes = true, candidates, []string{}

	return worst
}

func emptyNodeLossPlan(node string) nodeLossPlan {
	return nodeLossPlan{
		Node: node, Nodes: []string{}, Placed: []nodeLossPlaced{}, Preempted: []nodeLossPreempted{}, Pending: []nodeLossPending{},
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
	// unscheduled counts each workload's pods that have no node yet: scaling down removes
	// them first, and they free nothing.
	unscheduled map[string]int
}

func newLossSim(in capacityInput) lossSim {
	pods := resolvePriorities(in.pods, in.classes)
	nodes := capacityNodes(mapCheckupNodes(in.nodes, pods), pods, kubeTopNodes{})
	s := lossSim{
		nodes: nodes, pods: pods, owners: controllerOwners(in.sets),
		workloads: capacityWorkloads(pods, in.sets, in.pdbs, map[string]kubeTopPod{}, nodes),
		never:     map[string]bool{}, pdbs: map[string]pdbObject{}, byKey: map[string]capacityWorkload{},
		unscheduled: map[string]int{},
	}

	for _, p := range pods {
		if p.Node == "" && p.running() {
			s.unscheduled[s.simPod(p).key]++
		}
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

// lose simulates losing nodes: queue their pods, place, preempt, then suggest.
func (s lossSim) lose(nodes ...string) nodeLossPlan {
	plan := emptyNodeLossPlan(strings.Join(nodes, ", "))
	room := s.remaining(nodes...)
	budget := s.disruptionBudget()

	var queue []simPod

	for _, p := range s.pods {
		if !slices.Contains(nodes, p.Node) || !p.running() {
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

		if evicted, on := s.preempt(room, p, budget); on != "" {
			for _, e := range evicted {
				plan.Preempted = append(plan.Preempted, nodeLossPreempted{
					Namespace: e.Namespace, Name: e.Name, Workload: e.workload, Priority: e.Priority, PriorityClassName: e.PriorityClassName,
					Node: on, By: p.Namespace + "/" + p.Name, PDBViolated: budget.take(e),
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

	plan.Suggestions, plan.Covered = s.suggest(plan.Pending, room)

	return plan
}

// sortQueue is the scheduler's queue order (highest priority first), then the largest
// memory request first so the big pods find room before the small ones fill it.
func sortQueue(queue []simPod) {
	slices.SortStableFunc(queue, func(a, b simPod) int {
		return cmp.Or(cmp.Compare(b.Priority, a.Priority), cmp.Compare(b.Memory, a.Memory))
	})
}

// remaining is the schedulable nodes but the lost ones, with their free room and evictable
// pods.
func (s lossSim) remaining(nodes ...string) []simNode {
	var room []simNode

	for _, n := range s.nodes {
		if slices.Contains(nodes, n.Name) || !n.schedulable() {
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

// preempt finds the node where evicting lower priority pods makes room for p, evicts them and
// places p. The node whose evicted set has the lowest highest priority wins, then the fewest
// PodDisruptionBudget violations, then the fewest pods. on is "" when p may not preempt
// (policy Never) or no node works.
func (s lossSim) preempt(room []simNode, p simPod, budget disruptionBudget) (evicted []simPod, on string) {
	if s.never[p.PriorityClassName] {
		return nil, ""
	}

	best, bestSet := -1, []simPod(nil)

	for i, n := range room {
		set, ok := evictionSet(n, p)
		if ok && (best < 0 || betterEviction(set, bestSet, budget)) {
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

// evictionSet is the pods of n to evict so that p fits: the lower priority ones, lowest
// first, until it does; then, as the scheduler reprieves, the highest of them are kept again
// while p still fits without them.
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
			return reprieve(n, p, slices.Clone(lower[:i+1])), true
		}
	}

	return nil, false
}

// reprieve keeps back, highest priority first, the pods of set p still fits without; n is
// the node with all of set evicted.
func reprieve(n simNode, p simPod, set []simPod) []simPod {
	for j := len(set) - 1; j >= 0; j-- {
		q := set[j]

		kept := n
		kept.cpu, kept.memory, kept.slots = n.cpu-q.CPU, n.memory-q.Memory, n.slots-1

		if kept.fits(p) {
			n = kept
			set = slices.Delete(set, j, j+1)
		}
	}

	return set
}

func betterEviction(a, b []simPod, budget disruptionBudget) bool {
	top := func(set []simPod) int32 {
		return slices.MaxFunc(set, func(x, y simPod) int { return cmp.Compare(x.Priority, y.Priority) }).Priority
	}

	return cmp.Or(
		cmp.Compare(top(a), top(b)),
		cmp.Compare(budget.violations(a), budget.violations(b)),
		cmp.Compare(len(a), len(b)),
	) < 0
}

// disruptionBudget is what each covered workload's PodDisruptionBudget still allows
// ("Kind/namespace/name" → disruptions), spent as the simulation preempts.
type disruptionBudget map[string]int

func (s lossSim) disruptionBudget() disruptionBudget {
	b := disruptionBudget{}

	for key, w := range s.byKey {
		if w.PDB != "" {
			b[key] = max(0, w.PDBAllowed)
		}
	}

	return b
}

// violations is how many pods of set their budgets would not allow.
func (b disruptionBudget) violations(set []simPod) int {
	used, n := map[string]int{}, 0

	for _, q := range set {
		allowed, covered := b[q.key]
		if !covered {
			continue
		}

		used[q.key]++
		if used[q.key] > allowed {
			n++
		}
	}

	return n
}

// take spends e's disruption and tells whether its budget had none left (a violation).
func (b disruptionBudget) take(e simPod) bool {
	allowed, covered := b[e.key]
	if !covered {
		return false
	}

	b[e.key] = allowed - 1

	return allowed <= 0
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
