package ichorgo

import (
	"net/url"
	"strings"
	"time"
)

// The Kubernetes demo: a cluster added from a kubeconfig, without Talos, that looks like an
// EKS cluster (managed node group and Karpenter pools, cloud labels). It is the Talos demo's
// inventory seen through the Kubernetes API only: every Kubernetes read answers from the
// same demo data (kubeReadJSON and the other isDemoContext gates), mutations are refused with
// errDemoUnavailable, and the Talos reads fail with errTalosUnavailable as for any kubeconfig
// cluster. Its API server is the reserved demo endpoint, never dialled.

// demoKubeContext is the kube demo's context name: an EKS cluster ARN (AWS' documentation
// account), so the apps show it with its region and account like a real EKS context.
const demoKubeContext = "arn:aws:eks:eu-west-1:123456789012:cluster/ichor-demo"

// demoKubeToken is the demo's static token: no secret, it is never sent anywhere.
const demoKubeToken = "ichor-demo-token"

// DemoKubeconfig creates an ordinary, mergeable kubeconfig for the built-in Kubernetes demo.
// It is deterministic, so adding it again replaces the same cluster (same server).
func DemoKubeconfig() (out string, err error) {
	defer maskErr(&err)

	doc := emptyKubeconfigDoc()
	doc.CurrentContext = demoKubeContext

	var cluster kubeStoreCluster

	cluster.Name = demoKubeContext
	cluster.Cluster.Server = "https://" + demoEndpoint

	var user kubeStoreUser

	user.Name = demoKubeContext
	user.User.Token = demoKubeToken

	var ctx kubeStoreContext

	ctx.Name = demoKubeContext
	ctx.Context.Cluster, ctx.Context.User = demoKubeContext, demoKubeContext

	doc.Clusters = []kubeStoreCluster{cluster}
	doc.Users = []kubeStoreUser{user}
	doc.Contexts = []kubeStoreContext{ctx}

	return encodeKubeconfigDoc(doc)
}

// isDemoServer reports whether a kubeconfig cluster's server is the reserved demo endpoint.
func isDemoServer(cluster *kubeStoreCluster) bool {
	if cluster == nil {
		return false
	}

	u, err := url.Parse(cluster.Cluster.Server)

	return err == nil && strings.EqualFold(u.Hostname(), demoEndpoint)
}

// isKubeDemoContext reports whether the named context ("" for the current one) of the
// kubeconfig yaml is the Kubernetes demo.
func isKubeDemoContext(yaml, name string) bool {
	if !strings.Contains(yaml, demoEndpoint) || !isKubeconfig(yaml) {
		return false
	}

	doc, err := loadKubeconfigDoc(yaml)
	if err != nil {
		return false
	}

	if name == "" {
		name = doc.currentName()
	}

	ctx, ok := doc.context(name)
	if !ok {
		return false
	}

	cluster, _ := doc.cluster(ctx.Context.Cluster)

	return isDemoServer(cluster)
}

// demoEKSPools places the demo nodes (by index into demoNodes()) the way an EKS cluster
// would: the first three in a managed node group running the system add-ons, the others
// started by Karpenter, one of them on spot capacity.
var demoEKSPools = []map[string]string{
	{eksNodeGroupLabel: "system", eksCapacityLabel: "ON_DEMAND", instanceTypeLabel: "m7i.xlarge"},
	{eksNodeGroupLabel: "system", eksCapacityLabel: "ON_DEMAND", instanceTypeLabel: "m7i.xlarge"},
	{eksNodeGroupLabel: "system", eksCapacityLabel: "ON_DEMAND", instanceTypeLabel: "m7i.xlarge"},
	{karpenterPoolLabel: "general", karpenterCapacityLabel: "on-demand", instanceTypeLabel: "m7i.2xlarge"},
	{karpenterPoolLabel: "general", karpenterCapacityLabel: "spot", instanceTypeLabel: "c7i.2xlarge"},
}

// demoEKSNodes is the demo's nodes as an EKS API server lists them: no control plane (AWS
// runs it), Amazon Linux, pools and machine types from the cloud labels. The node names stay
// those of the demo inventory, so pods, metrics and events still land on them.
func demoEKSNodes() kubeNodesOverview {
	created := time.Now().Add(-21 * 24 * time.Hour).Unix()
	nodes := []kubeNodeInfo{}

	for i, d := range demoNodes() {
		labels := demoEKSPools[i%len(demoEKSPools)]
		n := kubeNodeInfo{
			Name: d.Hostname, Roles: []string{}, Ready: d.Ready, InternalIP: d.Node,
			Kubelet: "v1.34.1-eks-ae0d8e0", OSImage: "Amazon Linux 2023.9.20251006", Kernel: "6.12.46-66.121.amzn2023.x86_64",
			Runtime: "containerd://2.1.4", Arch: d.Arch, CPU: float64(d.CPUCount), Memory: float64(d.MemTotal),
			PodLimit: 58, Pressure: []string{}, Created: created + int64(i)*3600,
			InstanceType: labels[instanceTypeLabel],
		}
		n.Pool, n.PoolKind = nodePool(labels)
		n.Capacity = nodeCapacity(labels, n.PoolKind)
		nodes = append(nodes, n)
	}

	sortKubeNodes(nodes)

	return kubeNodesOverview{ServerVersion: "v1.34.1-eks-ae0d8e0", Nodes: nodes}
}

// demoKubeNodesFor is the demo node list of target: EKS-flavoured for the Kubernetes demo,
// the Talos demo's nodes otherwise.
func demoKubeNodesFor(target kubeTarget) func() kubeNodesOverview {
	return func() kubeNodesOverview {
		if isKubeconfig(target.config) {
			return demoEKSNodes()
		}

		return demoKubeNodes()
	}
}

// demoKubeWhoAmI is who the demo credentials are: an IAM role mapped to cluster admins on
// the Kubernetes demo, the Talos demo's admin otherwise.
func demoKubeWhoAmI(target kubeTarget) func() kubeWhoAmI {
	return func() kubeWhoAmI {
		if isKubeconfig(target.config) {
			return kubeWhoAmI{User: "arn:aws:sts::123456789012:assumed-role/IchorDemoAdmin/demo", Groups: []string{"system:authenticated", "eks:cluster-admins"}}
		}

		return kubeWhoAmI{User: "demo", Groups: []string{"system:masters"}}
	}
}
