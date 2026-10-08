package ichorgo

// Where a node came from, read off the labels the cloud autoscalers set: the pool that made
// it, its machine type, and whether it is spot capacity.

const (
	instanceTypeLabel       = "node.kubernetes.io/instance-type"
	legacyInstanceTypeLabel = "beta.kubernetes.io/instance-type"

	// Karpenter (open source and EKS Auto Mode): v1beta1+ names the NodePool, v1alpha5 the
	// Provisioner. capacity-type is "spot", "on-demand" or "reserved".
	karpenterPoolLabel        = "karpenter.sh/nodepool"
	karpenterProvisionerLabel = "karpenter.sh/provisioner-name"
	karpenterCapacityLabel    = "karpenter.sh/capacity-type"

	// EKS managed node groups: capacityType is "SPOT", "ON_DEMAND" or "CAPACITY_BLOCK".
	eksNodeGroupLabel = "eks.amazonaws.com/nodegroup"
	eksCapacityLabel  = "eks.amazonaws.com/capacityType"

	// GKE: a custom compute class is the NodePool equivalent (its nodes land in auto-provisioned
	// pools with generated names); the node pool otherwise. Spot (and the older preemptible)
	// VMs are flagged; the rest are on-demand.
	gkeComputeClassLabel = "cloud.google.com/compute-class"
	gkePoolLabel         = "cloud.google.com/gke-nodepool"
	gkeSpotLabel         = "cloud.google.com/gke-spot"
	gkePreemptibleLabel  = "cloud.google.com/gke-preemptible"

	// AKS: scalesetpriority is "spot" on spot pools, unset otherwise.
	aksPoolLabel     = "kubernetes.azure.com/agentpool"
	aksPriorityLabel = "kubernetes.azure.com/scalesetpriority"
)

const (
	capacitySpot     = "spot"
	capacityOnDemand = "on-demand"
	capacityReserved = "reserved"
)

// nodePool is the autoscaler pool a node belongs to and which autoscaler runs it. The
// declarative layer wins over the cloud's own pool: an EKS Auto Mode node carries its
// Karpenter NodePool label alongside EKS' own, a GKE compute-class node its generated pool.
func nodePool(labels map[string]string) (pool, kind string) {
	switch {
	case labels[karpenterPoolLabel] != "":
		return labels[karpenterPoolLabel], "karpenter"
	case labels[karpenterProvisionerLabel] != "":
		return labels[karpenterProvisionerLabel], "karpenter"
	case labels[eksNodeGroupLabel] != "":
		return labels[eksNodeGroupLabel], "eks"
	case labels[gkeComputeClassLabel] != "":
		return labels[gkeComputeClassLabel], "gke-class"
	case labels[gkePoolLabel] != "":
		return labels[gkePoolLabel], "gke"
	case labels[aksPoolLabel] != "":
		return labels[aksPoolLabel], "aks"
	}

	return "", ""
}

// nodeCapacity is "spot", "on-demand" or "reserved" when the labels say, "" when they do not
// (on-prem, or a cloud this does not know). The pool kind decides the GKE and AKS defaults:
// their nodes are on-demand unless flagged spot.
func nodeCapacity(labels map[string]string, poolKind string) string {
	switch labels[karpenterCapacityLabel] {
	case "spot":
		return capacitySpot
	case "on-demand":
		return capacityOnDemand
	case "reserved":
		return capacityReserved
	}

	switch labels[eksCapacityLabel] {
	case "SPOT":
		return capacitySpot
	case "ON_DEMAND":
		return capacityOnDemand
	case "CAPACITY_BLOCK":
		return capacityReserved
	}

	if labels[gkeSpotLabel] == "true" || labels[gkePreemptibleLabel] == "true" || labels[aksPriorityLabel] == "spot" {
		return capacitySpot
	}

	if poolKind == "gke" || poolKind == "gke-class" || poolKind == "aks" {
		return capacityOnDemand
	}

	return ""
}

// firstLabel is the first of keys that is set.
func firstLabel(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}

	return ""
}
