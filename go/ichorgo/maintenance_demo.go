package ichorgo

// demoMaintenancePlan shows a typical plan: pods to evict (one behind a budget that allows
// no disruption, one with emptyDir data), DaemonSet pods left alone, and, for a control
// plane, its static pods and the etcd acknowledgment.
func demoMaintenancePlan(node string) maintenancePlan {
	plan := maintenancePlan{Node: node, Hostname: node, KubeNode: node, Blockers: []string{}, Warnings: []string{}, Acknowledge: []string{}}

	for _, n := range demoNodes() {
		if n.Node == node {
			plan.Hostname, plan.KubeNode, plan.ControlPlane = n.Hostname, n.Hostname, n.Role == "controlplane"
		}
	}

	plan.Pods = []drainPod{
		{Namespace: "db", Name: "postgres-1", Owner: "Cluster/postgres", Kind: drainEvict, PDB: "postgres-primary", PDBAllowed: 0},
		{Namespace: "web", Name: "frontend-7d9c6-x2kq", Owner: "ReplicaSet/frontend-7d9c6", Kind: drainEvict, PDB: "frontend", PDBAllowed: 1},
		{Namespace: "web", Name: "cache-0", Owner: "StatefulSet/cache", Kind: drainEvict, EmptyDir: true, PDBAllowed: -1},
		{Namespace: "kube-system", Name: "cilium-8xk2p", Owner: "DaemonSet/cilium", Kind: drainDaemonSet, PDBAllowed: -1},
		{Namespace: "longhorn-system", Name: "longhorn-manager-p4z9d", Owner: "DaemonSet/longhorn-manager", Kind: drainDaemonSet, PDBAllowed: -1},
	}
	plan.Warnings = append(plan.Warnings, "PodDisruptionBudget db/postgres-primary allows no disruption now: the drain waits for it")

	if plan.ControlPlane {
		plan.Pods = append(plan.Pods,
			drainPod{Namespace: "kube-system", Name: "kube-apiserver-" + plan.KubeNode, Owner: "Node/" + plan.KubeNode, Kind: drainStatic, PDBAllowed: -1})
		plan.Acknowledge = append(plan.Acknowledge, "etcd keeps quorum with 2 of 3 members while this control plane reboots")
	}

	return plan
}
