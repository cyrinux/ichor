package ichorgo

import (
	"slices"
	"strings"
)

// The demo cluster losing a worker (see demoKubeCapacity): postgres preempts a pod of the
// class-less worker, which then stays Pending for memory; the batch-low cache (preemptionPolicy
// Never) stays Pending for the ephemeral storage its 1 GiB request asks. The suggestions start
// with the class-less importer and never touch a system class.
// Losing both workers leaves only the NoSchedule control planes: all eleven movable pods stay
// Pending, and as every scalable workload then has a Pending pod there is nothing to suggest.
// As with the real planner, every lost or preempted pod ends up placed or Pending exactly
// once, and the lost requests are the demo workloads' (see TestDemoKubeNodeLossAddsUp).

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
	const mi = 1 << 20

	plan := emptyNodeLossPlan(node)
	plan.Nodes = nodes
	// coredns, postgres, two hello-ichor, the cache and an importer: 1.45 cores, 1670 MiB.
	plan.CPULost, plan.MemoryLost, plan.PodsLost, plan.DaemonSetPods = 1.45, 1670*mi, 6, 1
	plan.Placed = []nodeLossPlaced{
		{Namespace: "kube-system", Name: "coredns-7c65d-x2k9p", Workload: "Deployment/coredns", Priority: 2000000000, To: other},
		{Namespace: "demo", Name: "postgres-0", Workload: "StatefulSet/postgres", Priority: 10000, To: other},
		{Namespace: "demo", Name: "hello-ichor-6d9f8-abcde", Workload: "Deployment/hello-ichor", Priority: 10000, To: other},
		{Namespace: "demo", Name: "hello-ichor-6d9f8-fghij", Workload: "Deployment/hello-ichor", Priority: 10000, To: other},
		{Namespace: "demo", Name: "batch-importer-5b7c9-k3m2q", Workload: "Deployment/batch-importer", To: other},
	}
	plan.Preempted = []nodeLossPreempted{
		{Namespace: "demo", Name: "worker-6f4b8-uvwxy", Workload: "Deployment/worker", Node: other, By: "demo/postgres-0"},
	}
	plan.Pending = []nodeLossPending{
		{Namespace: "demo", Name: "report-cache-8d5f6-q7r4t", Workload: "Deployment/report-cache", Priority: 100,
			PriorityClassName: "batch-low", QoS: qosBurstable, Reason: pendingEphemeral},
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
// planes remain, so nothing is placed and every movable pod stays Pending, in the scheduler's
// queue order. Each scalable workload then has a Pending pod, which the suggester never
// proposes, so there is nothing to suggest.
func demoNodeLossBothWorkers(nodes []string) nodeLossPlan {
	const mi = 1 << 20

	pending := func(namespace, name, workload string, priority int32, class, qos string) nodeLossPending {
		return nodeLossPending{Namespace: namespace, Name: name, Workload: workload, Priority: priority,
			PriorityClassName: class, QoS: qos, Reason: pendingPods}
	}

	plan := emptyNodeLossPlan(strings.Join(nodes, ", "))
	plan.Nodes = nodes
	// Every movable pod of demoKubeCapacity: 1.8 cores, 2028 MiB; the two kube-proxy pods stay.
	plan.CPULost, plan.MemoryLost, plan.PodsLost, plan.DaemonSetPods = 1.8, 2028*mi, 11, 2
	plan.Pending = []nodeLossPending{
		pending("kube-system", "coredns-7c65d-x2k9p", "Deployment/coredns", 2000000000, "system-cluster-critical", qosBurstable),
		pending("kube-system", "coredns-7c65d-r8w4n", "Deployment/coredns", 2000000000, "system-cluster-critical", qosBurstable),
		pending("demo", "postgres-0", "StatefulSet/postgres", 10000, "web-high", qosGuaranteed),
		pending("demo", "hello-ichor-6d9f8-abcde", "Deployment/hello-ichor", 10000, "web-high", qosBurstable),
		pending("demo", "hello-ichor-6d9f8-fghij", "Deployment/hello-ichor", 10000, "web-high", qosBurstable),
		pending("demo", "hello-ichor-6d9f8-klmno", "Deployment/hello-ichor", 10000, "web-high", qosBurstable),
		pending("demo", "report-cache-8d5f6-q7r4t", "Deployment/report-cache", 100, "batch-low", qosBurstable),
		pending("demo", "worker-6f4b8-uvwxy", "Deployment/worker", 0, "", qosBurstable),
		pending("demo", "worker-6f4b8-z1a2b", "Deployment/worker", 0, "", qosBurstable),
		pending("demo", "batch-importer-5b7c9-k3m2q", "Deployment/batch-importer", 0, "", qosBestEffort),
		pending("demo", "batch-importer-5b7c9-p4s6d", "Deployment/batch-importer", 0, "", qosBestEffort),
	}
	plan.Covered = false

	return plan
}
