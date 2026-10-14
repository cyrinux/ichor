package ichorgo

import (
	"slices"
	"strings"
)

// The demo cluster losing a worker (see demoKubeCapacity): postgres preempts a pod of the
// class-less worker, which then stays Pending with the batch-low cache (preemptionPolicy
// Never); the suggestions start with the class-less importer and never touch a system class.
// Losing both workers leaves only the NoSchedule control planes: every movable pod stays
// Pending and the suggestions are the whole answer.

func demoKubeNodeLoss(nodes []string) nodeLossPlan {
	known := func(name string) bool { return slices.Contains(nodes, name) }

	switch {
	case len(nodes) == 1 && known("demo-worker-2"):
		return demoNodeLossOne("demo-worker-2", "demo-worker-1", nodes)
	case len(nodes) == 1 && known("demo-worker-1"):
		return demoNodeLossOne("demo-worker-1", "demo-worker-2", nodes)
	case len(nodes) == 2 && known("demo-worker-1") && known("demo-worker-2"):
		return demoNodeLossBothWorkers(nodes)
	default:
		worst := demoNodeLossOne("demo-worker-1", "demo-worker-2", []string{"demo-worker-1"})
		worst.WorstCase = true
		worst.Nodes = []string{}
		worst.Candidates = []nodeLossCandidate{{Name: "demo-worker-1", Pending: 2, Preempted: 1}, {Name: "demo-worker-2", Pending: 1}}

		return worst
	}
}

// demoNodeLossOne is the plan for losing one worker, whose pods land on the other.
func demoNodeLossOne(node, other string, nodes []string) nodeLossPlan {
	const (
		gi = 1 << 30
		mi = 1 << 20
	)

	plan := emptyNodeLossPlan(node)
	plan.Nodes = nodes
	plan.CPULost, plan.MemoryLost, plan.PodsLost, plan.DaemonSetPods = 1.55, 1.83*gi, 6, 1
	plan.Placed = []nodeLossPlaced{
		{Namespace: "kube-system", Name: "coredns-7c65d-x2k9p", Workload: "Deployment/coredns", Priority: 2000000000, To: other},
		{Namespace: "demo", Name: "postgres-0", Workload: "StatefulSet/postgres", Priority: 10000, To: other},
		{Namespace: "demo", Name: "hello-ichor-6d9f8-abcde", Workload: "Deployment/hello-ichor", Priority: 10000, To: other},
		{Namespace: "demo", Name: "batch-importer-5b7c9-k3m2q", Workload: "Deployment/batch-importer", To: other},
	}
	plan.Preempted = []nodeLossPreempted{
		{Namespace: "demo", Name: "worker-6f4b8-uvwxy", Workload: "Deployment/worker", Node: other, By: "demo/postgres-0"},
	}
	plan.Pending = []nodeLossPending{
		{Namespace: "demo", Name: "report-cache-8d5f6-q7r4t", Workload: "Deployment/report-cache", Priority: 100,
			PriorityClassName: "batch-low", QoS: qosBurstable, Reason: pendingMemory},
		{Namespace: "demo", Name: "worker-6f4b8-uvwxy", Workload: "Deployment/worker", QoS: qosBurstable, Reason: pendingMemory},
	}
	plan.Suggestions = []nodeLossSuggestion{
		{Kind: "Deployment", Namespace: "demo", Name: "batch-importer", Replicas: 2, ScaleTo: 0, NoPriorityClass: true,
			Frees: nodeLossFrees{Pods: 2}},
		{Kind: "Deployment", Namespace: "demo", Name: "hello-ichor", Replicas: 3, ScaleTo: 2, Priority: 10000,
			PriorityClassName: "web-high", PDB: "hello-ichor", PDBMinAvailable: 2, Frees: nodeLossFrees{CPU: 0.05, Memory: 32 * mi, Pods: 1}},
		{Kind: "StatefulSet", Namespace: "demo", Name: "postgres", Replicas: 1, ScaleTo: 1, Priority: 10000,
			PriorityClassName: "web-high", PDB: "postgres", PDBMinAvailable: 1, PDBBlocks: true},
	}
	plan.Covered = false

	return plan
}

// demoNodeLossBothWorkers is the plan for losing both workers: only the NoSchedule control
// planes remain, so nothing is placed and every movable pod stays Pending.
func demoNodeLossBothWorkers(nodes []string) nodeLossPlan {
	const gi = 1 << 30

	plan := emptyNodeLossPlan(strings.Join(nodes, ", "))
	plan.Nodes = nodes
	plan.CPULost, plan.MemoryLost, plan.PodsLost, plan.DaemonSetPods = 2.9, 3.66*gi, 11, 2
	plan.Pending = []nodeLossPending{
		{Namespace: "kube-system", Name: "coredns-7c65d-x2k9p", Workload: "Deployment/coredns", Priority: 2000000000,
			PriorityClassName: "system-cluster-critical", QoS: qosBurstable, Reason: pendingPods},
		{Namespace: "demo", Name: "postgres-0", Workload: "StatefulSet/postgres", Priority: 10000,
			PriorityClassName: "web-high", QoS: qosGuaranteed, Reason: pendingPods},
		{Namespace: "demo", Name: "hello-ichor-6d9f8-abcde", Workload: "Deployment/hello-ichor", Priority: 10000,
			PriorityClassName: "web-high", QoS: qosBurstable, Reason: pendingPods},
		{Namespace: "demo", Name: "batch-importer-5b7c9-k3m2q", Workload: "Deployment/batch-importer", QoS: qosBestEffort, Reason: pendingPods},
		{Namespace: "demo", Name: "worker-6f4b8-uvwxy", Workload: "Deployment/worker", QoS: qosBurstable, Reason: pendingPods},
		{Namespace: "demo", Name: "report-cache-8d5f6-q7r4t", Workload: "Deployment/report-cache", Priority: 100,
			PriorityClassName: "batch-low", QoS: qosBurstable, Reason: pendingPods},
	}
	plan.Suggestions = []nodeLossSuggestion{
		{Kind: "Deployment", Namespace: "demo", Name: "batch-importer", Replicas: 2, ScaleTo: 0, NoPriorityClass: true,
			Frees: nodeLossFrees{Pods: 2}},
		{Kind: "Deployment", Namespace: "demo", Name: "hello-ichor", Replicas: 3, ScaleTo: 2, Priority: 10000,
			PriorityClassName: "web-high", PDB: "hello-ichor", PDBMinAvailable: 2, Frees: nodeLossFrees{CPU: 0.05, Memory: 32 * (1 << 20), Pods: 1}},
		{Kind: "StatefulSet", Namespace: "demo", Name: "postgres", Replicas: 1, ScaleTo: 1, Priority: 10000,
			PriorityClassName: "web-high", PDB: "postgres", PDBMinAvailable: 1, PDBBlocks: true},
	}
	plan.Covered = false

	return plan
}
