package ichorgo

// The demo cluster's capacity: the checkup's demo nodes, a few workloads covering each risk
// and priority case, and a worker whose pods would not fit elsewhere.

func demoKubeCapacity() kubeCapacity {
	const (
		gi = 1 << 30
		mi = 1 << 20
	)

	// Requests as in the checkup's demo; limits, usage and the pods' room on top.
	node := func(n checkupNode, cpuLimits, memoryLimits, cpuUsage, memoryUsage float64) capacityNode {
		c := capacityNode{
			checkupNode: n, CPULimits: cpuLimits, MemoryLimits: memoryLimits, CPUUsage: cpuUsage, MemoryUsage: memoryUsage,
			CPUFree: max(0, n.CPUAllocatable-n.CPURequests), MemoryFree: max(0, n.MemoryAllocatable-n.MemoryRequests),
			PodsFree: max(0, n.PodCapacity-n.Pods),
		}
		c.Overcommitted = c.MemoryLimits > n.MemoryAllocatable

		return c
	}

	nodes := []capacityNode{
		node(demoCheckupNode("demo-cp-1", "control-plane", 4, 8*gi, 1.2, 3.1*gi, 14), 2.4, 4.2*gi, 0.9, 3.6*gi),
		node(demoCheckupNode("demo-cp-2", "control-plane", 4, 8*gi, 1.1, 2.9*gi, 13), 2.2, 4.0*gi, 0.8, 3.3*gi),
		node(demoCheckupNode("demo-cp-3", "control-plane", 4, 8*gi, 1.3, 3.4*gi, 15), 2.6, 4.6*gi, 1.0, 3.9*gi),
		node(demoCheckupNode("demo-worker-1", "", 8, 16*gi, 7.4, 11.8*gi, 61), 12.5, 17.5*gi, 5.1, 12.9*gi),
		node(demoCheckupNode("demo-worker-2", "", 8, 16*gi, 5.9, 14.6*gi, 48), 9.0, 15.2*gi, 3.8, 13.4*gi),
	}

	workloads := []capacityWorkload{
		{
			Kind: "Deployment", Namespace: "demo", Name: "batch-importer", Replicas: 2, NoPriorityClass: true,
			QoS: qosBestEffort, PDBAllowed: -1, Risk: riskEvictFirst, MoreFit: 98,
			CPUUsage: 0.6, MemoryUsage: 900 * mi,
		},
		{
			Kind: "Deployment", Namespace: "demo", Name: "worker", Replicas: 2, NoPriorityClass: true,
			CPURequest: 0.1, MemoryRequest: 128 * mi, QoS: qosBurstable, PDBAllowed: -1, Risk: riskUnbounded,
			CPUUsage: 0.31, MemoryUsage: 99 * mi, MoreFit: 10,
		},
		{
			Kind: "Deployment", Namespace: "demo", Name: "report-cache", Replicas: 1, PriorityClassName: "batch-low", Priority: 100,
			CPURequest: 0.25, CPULimit: 0.5, MemoryRequest: 512 * mi, MemoryLimit: 768 * mi, QoS: qosBurstable,
			PDBAllowed: -1, OOMKills: 3, Risk: riskOOM, CPUUsage: 0.2, MemoryUsage: 731 * mi, MoreFit: 2,
		},
		{
			Kind: "Deployment", Namespace: "demo", Name: "hello-ichor", Replicas: 3, PriorityClassName: "web-high", Priority: 10000,
			CPURequest: 0.05, CPULimit: 0.2, MemoryRequest: 32 * mi, MemoryLimit: 64 * mi, QoS: qosBurstable,
			PDB: "hello-ichor", PDBAllowed: 1, Risk: riskOK, CPUUsage: 0.092, MemoryUsage: 104 * mi, MoreFit: 41,
		},
		{
			Kind: "StatefulSet", Namespace: "demo", Name: "postgres", Replicas: 1, PriorityClassName: "web-high", Priority: 10000,
			CPURequest: 1, CPULimit: 1, MemoryRequest: 1 * gi, MemoryLimit: 1 * gi, QoS: qosGuaranteed,
			PDB: "postgres", PDBAllowed: 0, Risk: riskOK, CPUUsage: 0.18, MemoryUsage: 410 * mi, MoreFit: 1,
		},
		{
			Kind: "DaemonSet", Namespace: "kube-system", Name: "kube-proxy", Replicas: 5, PriorityClassName: "system-node-critical",
			Priority: 2000001000, CPURequest: 0.1, MemoryRequest: 64 * mi, QoS: qosBurstable, PDBAllowed: -1, Risk: riskUnbounded,
			CPUUsage: 0.05, MemoryUsage: 120 * mi, MoreFitNA: true,
		},
		{
			Kind: "Deployment", Namespace: "kube-system", Name: "coredns", Replicas: 2, PriorityClassName: "system-cluster-critical",
			Priority: 2000000000, CPURequest: 0.1, MemoryRequest: 70 * mi, MemoryLimit: 170 * mi, QoS: qosBurstable,
			PDB: "coredns", PDBAllowed: 1, Risk: riskOK, CPUUsage: 0.011, MemoryUsage: 35 * mi, MoreFit: 10,
		},
	}

	sortWorkloads(workloads)

	return kubeCapacity{
		MetricsAvailable: true, PriorityClassesRead: true, PDBsRead: true,
		PriorityClasses: []capacityPriorityClass{
			{Name: "batch-low", Value: 100, PreemptionPolicy: "Never"},
			{Name: "web-high", Value: 10000, PreemptionPolicy: "PreemptLowerPriority"},
			{Name: "system-cluster-critical", Value: 2000000000, PreemptionPolicy: "PreemptLowerPriority"},
			{Name: "system-node-critical", Value: 2000001000, PreemptionPolicy: "PreemptLowerPriority"},
		},
		Totals:      capacityTotalsOf(nodes),
		Nodes:       nodes,
		Workloads:   workloads,
		NPlusOne:    capacityNPlusOne{Node: "demo-worker-1", PendingPods: 9, ShortCPU: 1.9, ShortMemory: 3.3 * gi},
		Assumptions: capacityAssumptions,
	}
}
