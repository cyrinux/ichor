package ichorgo

import "slices"

// The demo cluster losing a worker (see demoKubeCapacity): postgres preempts a pod of the
// class-less worker, which then stays Pending with the batch-low cache (preemptionPolicy
// Never); the suggestions start with the class-less importer and never touch a system class.

func demoKubeNodeLoss(kubeNode string) nodeLossPlan {
	const (
		gi = 1 << 30
		mi = 1 << 20
	)

	workers := []string{"demo-worker-1", "demo-worker-2"}

	node, other, worst := kubeNode, "demo-worker-2", false
	if !slices.Contains(workers, node) {
		node, worst = workers[0], true
	}

	if node == "demo-worker-2" {
		other = "demo-worker-1"
	}

	plan := emptyNodeLossPlan(node)
	plan.WorstCase = worst
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

	if worst {
		plan.Candidates = []nodeLossCandidate{{Name: "demo-worker-1", Pending: 2, Preempted: 1}, {Name: "demo-worker-2", Pending: 1}}
	}

	return plan
}
