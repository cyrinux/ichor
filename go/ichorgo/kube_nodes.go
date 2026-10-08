package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The home of a cluster added from a kubeconfig: its nodes as Kubernetes sees them, since
// the Talos overview cannot be read.

type kubeNodesOverview struct {
	// ServerVersion is the API server's gitVersion, "" when /version is not readable.
	ServerVersion string         `json:"serverVersion"`
	Nodes         []kubeNodeInfo `json:"nodes"`
	// Forbidden: the credentials may not list nodes (a namespaced ServiceAccount).
	Forbidden bool `json:"forbidden,omitempty"`
}

// kubeNodeInfo is a node for the home screen. CPU in cores, memory in bytes.
type kubeNodeInfo struct {
	Name       string   `json:"name"`
	Roles      []string `json:"roles"`
	Ready      bool     `json:"ready"`
	Cordoned   bool     `json:"cordoned"`
	InternalIP string   `json:"internalIP,omitempty"`
	ExternalIP string   `json:"externalIP,omitempty"`
	Kubelet    string   `json:"kubelet,omitempty"`
	OSImage    string   `json:"osImage,omitempty"`
	Kernel     string   `json:"kernel,omitempty"`
	Runtime    string   `json:"runtime,omitempty"`
	Arch       string   `json:"arch,omitempty"`
	// Pool is the autoscaler pool the node came from, PoolKind which autoscaler names it:
	// "karpenter" (a NodePool, EKS Auto Mode included), "eks" (a managed node group),
	// "gke-class" (a custom compute class), "gke" (a node pool) or "aks" (an agent pool).
	// Both "" when no such label is set.
	Pool     string `json:"pool,omitempty"`
	PoolKind string `json:"poolKind,omitempty"`
	// InstanceType is the cloud machine type (node.kubernetes.io/instance-type).
	InstanceType string `json:"instanceType,omitempty"`
	// Capacity is "spot", "on-demand" or "reserved" when the cloud labels say which.
	Capacity string  `json:"capacity,omitempty"`
	CPU      float64 `json:"cpu"`
	Memory   float64 `json:"memory"`
	PodLimit int     `json:"podLimit"`
	// Pressure lists the problem conditions that are on (MemoryPressure, DiskPressure,
	// PIDPressure, NetworkUnavailable).
	Pressure []string `json:"pressure"`
	// Created is the node's age, Unix seconds.
	Created int64 `json:"created"`
}

// kubeNodeObject is a Node as the API server serves it, with every field a reader of nodes
// needs (the Kubernetes home, netperf, the checkup, the Argo CD network, drains, the wait
// after a reboot): one decode shape, read with listKubeNodeObjects or readKubeNodeObject and
// asked through the accessors below.
type kubeNodeObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool        `json:"unschedulable"`
		Taints        []nodeTaint `json:"taints"`
	} `json:"spec"`
	Status struct {
		Allocatable map[string]string `json:"allocatable"`
		Conditions  []nodeCondition   `json:"conditions"`
		Addresses   []struct {
			Type    string `json:"type"`
			Address string `json:"address"`
		} `json:"addresses"`
		NodeInfo struct {
			KubeletVersion          string `json:"kubeletVersion"`
			OSImage                 string `json:"osImage"`
			KernelVersion           string `json:"kernelVersion"`
			ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
			Architecture            string `json:"architecture"`
		} `json:"nodeInfo"`
	} `json:"status"`
}

type nodeTaint struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	Effect    string     `json:"effect"`
	TimeAdded *time.Time `json:"timeAdded"`
}

// nodeCondition is a kubeCondition with the kubelet heartbeat only Node conditions carry.
type nodeCondition struct {
	kubeCondition
	LastHeartbeatTime time.Time `json:"lastHeartbeatTime"`
}

var nodePressureConditions = []string{"MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable"}

// listKubeNodeObjects reads every node of the cluster.
func listKubeNodeObjects(ctx context.Context, k *kubeClient) ([]kubeNodeObject, error) {
	return listObjects[kubeNodeObject](ctx, k, "/api/v1/nodes")
}

// readKubeNodeObject reads the node called name.
func readKubeNodeObject(ctx context.Context, k *kubeClient, name string) (kubeNodeObject, error) {
	var obj kubeNodeObject

	err := k.get(ctx, "/api/v1/nodes/"+url.PathEscape(name), &obj)

	return obj, err
}

// conditions are the node's status conditions, as kubeConditions reads them.
func (n kubeNodeObject) conditions() kubeConditions {
	out := make(kubeConditions, len(n.Status.Conditions))
	for i, c := range n.Status.Conditions {
		out[i] = c.kubeCondition
	}

	return out
}

// ready tells whether the Ready condition is True.
func (n kubeNodeObject) ready() bool { return n.conditions().is("Ready") }

// readySince tells whether the node is Ready with a kubelet heartbeat after since: right
// after a fast reboot the Node object still says Ready from before it (Kubernetes marks a
// node NotReady only after a grace period), while the restarted kubelet posts its status,
// with a fresh heartbeat, as soon as it registers.
func (n kubeNodeObject) readySince(since time.Time) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True" && c.LastHeartbeatTime.After(since)
		}
	}

	return false
}

// roles are the node-role.kubernetes.io/<role> labels, sorted, "control-plane" for the legacy
// "master" too (nodeRoleNames).
func (n kubeNodeObject) roles() []string { return nodeRoleNames(n.Metadata.Labels) }

// rawRoles are the role labels as set, sorted: "master" stays "master".
func (n kubeNodeObject) rawRoles() []string {
	roles := []string{}

	for key := range n.Metadata.Labels {
		if role, ok := strings.CutPrefix(key, roleLabelPrefix); ok && role != "" {
			roles = append(roles, role)
		}
	}

	slices.Sort(roles)

	return roles
}

// controlPlane tells whether the node is labelled as a control plane (or legacy master).
func (n kubeNodeObject) controlPlane() bool { return slices.Contains(n.roles(), "control-plane") }

// address is the node's first address of type typ ("InternalIP", "ExternalIP", "Hostname"), "" for none.
func (n kubeNodeObject) address(typ string) string {
	for _, a := range n.Status.Addresses {
		if a.Type == typ {
			return a.Address
		}
	}

	return ""
}

func (n kubeNodeObject) internalIP() string { return n.address("InternalIP") }

// pressure lists the problem conditions that are on (nodePressureConditions); never nil.
func (n kubeNodeObject) pressure() []string {
	out := []string{}
	conds := n.conditions()

	for _, c := range nodePressureConditions {
		if conds.is(c) {
			out = append(out, c)
		}
	}

	return out
}

// taintStrings writes the taints as "key=value:Effect" ("key:Effect" without a value); never nil.
func (n kubeNodeObject) taintStrings() []string {
	out := []string{}

	for _, t := range n.Spec.Taints {
		out = append(out, strings.TrimSuffix(t.Key+"="+t.Value, "=")+":"+t.Effect)
	}

	return out
}

// cordonedSince is when the unschedulable taint was added, nil when unknown or not cordoned.
func (n kubeNodeObject) cordonedSince() *time.Time {
	for _, t := range n.Spec.Taints {
		if t.Key == taintUnschedulable && t.TimeAdded != nil {
			return t.TimeAdded
		}
	}

	return nil
}

// KubeNodes lists the cluster's nodes from the Kubernetes API, as a JSON kubeNodesOverview:
// control planes first, then by name.
func KubeNodes(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoKubeNodes, listKubeNodes)
}

func listKubeNodes(ctx context.Context, k *kubeClient) (kubeNodesOverview, error) {
	out := kubeNodesOverview{Nodes: []kubeNodeInfo{}}

	var version struct {
		GitVersion string `json:"gitVersion"`
	}

	if k.get(ctx, "/version", &version) == nil {
		out.ServerVersion = version.GitVersion
	}

	objs, err := listKubeNodeObjects(ctx, k)
	if isForbidden(err) {
		out.Forbidden = true

		return out, nil
	}

	if err != nil {
		return kubeNodesOverview{}, err
	}

	for _, obj := range objs {
		out.Nodes = append(out.Nodes, mapKubeNode(obj))
	}

	sortKubeNodes(out.Nodes)

	return out, nil
}

func mapKubeNode(obj kubeNodeObject) kubeNodeInfo {
	n := kubeNodeInfo{
		Name:       obj.Metadata.Name,
		Roles:      obj.roles(),
		Ready:      obj.ready(),
		Cordoned:   obj.Spec.Unschedulable,
		InternalIP: obj.internalIP(),
		ExternalIP: obj.address("ExternalIP"),
		Kubelet:    obj.Status.NodeInfo.KubeletVersion,
		OSImage:    obj.Status.NodeInfo.OSImage,
		Kernel:     obj.Status.NodeInfo.KernelVersion,
		Runtime:    obj.Status.NodeInfo.ContainerRuntimeVersion,
		Arch:       obj.Status.NodeInfo.Architecture,
		CPU:        parseQuantity(obj.Status.Allocatable["cpu"]),
		Memory:     parseQuantity(obj.Status.Allocatable["memory"]),
		PodLimit:   int(parseQuantity(obj.Status.Allocatable["pods"])),
		Pressure:   obj.pressure(),
		Created:    obj.Metadata.CreationTimestamp.Unix(),
	}

	n.Pool, n.PoolKind = nodePool(obj.Metadata.Labels)
	n.InstanceType = firstLabel(obj.Metadata.Labels, instanceTypeLabel, legacyInstanceTypeLabel)
	n.Capacity = nodeCapacity(obj.Metadata.Labels, n.PoolKind)

	return n
}

// nodeRoleNames are the node-role.kubernetes.io/<role> labels, sorted; "control-plane" for
// the legacy "master" too.
func nodeRoleNames(labels map[string]string) []string {
	roles := []string{}

	for key := range labels {
		role, ok := strings.CutPrefix(key, roleLabelPrefix)
		if !ok || role == "" {
			continue
		}

		if role == "master" {
			role = "control-plane"
		}

		if !slices.Contains(roles, role) {
			roles = append(roles, role)
		}
	}

	slices.Sort(roles)

	return roles
}

func sortKubeNodes(nodes []kubeNodeInfo) {
	slices.SortStableFunc(nodes, func(a, b kubeNodeInfo) int {
		aCP, bCP := slices.Contains(a.Roles, "control-plane"), slices.Contains(b.Roles, "control-plane")
		if aCP != bCP {
			if aCP {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})
}

func demoKubeNodes() kubeNodesOverview {
	created := time.Now().Add(-90 * 24 * time.Hour).Unix()
	nodes := []kubeNodeInfo{}

	for _, d := range demoNodes() {
		roles := []string{}
		if d.Role == "controlplane" {
			roles = []string{"control-plane"}
		}

		nodes = append(nodes, kubeNodeInfo{
			Name: d.Hostname, Roles: roles, Ready: d.Ready, InternalIP: d.Node,
			Kubelet: "v1.34.1", OSImage: fmt.Sprintf("Talos (%s)", demoTalosVersion), Kernel: "6.12.40-talos",
			Runtime: "containerd://2.1.4", Arch: d.Arch, CPU: float64(d.CPUCount), Memory: float64(d.MemTotal),
			PodLimit: 110, Pressure: []string{}, Created: created,
		})
	}

	sortKubeNodes(nodes)

	return kubeNodesOverview{ServerVersion: "v1.34.1", Nodes: nodes}
}
