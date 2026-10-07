package ichorgo

import (
	"context"
	"fmt"
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
	CPU        float64  `json:"cpu"`
	Memory     float64  `json:"memory"`
	PodLimit   int      `json:"podLimit"`
	// Pressure lists the problem conditions that are on (MemoryPressure, DiskPressure,
	// PIDPressure, NetworkUnavailable).
	Pressure []string `json:"pressure"`
	// Created is the node's age, Unix seconds.
	Created int64 `json:"created"`
}

type kubeNodeObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Allocatable map[string]string `json:"allocatable"`
		Conditions  kubeConditions    `json:"conditions"`
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

var nodePressureConditions = []string{"MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable"}

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

	objs, err := listObjects[kubeNodeObject](ctx, k, "/api/v1/nodes")
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
		Name:     obj.Metadata.Name,
		Roles:    nodeRoleNames(obj.Metadata.Labels),
		Ready:    obj.Status.Conditions.is("Ready"),
		Cordoned: obj.Spec.Unschedulable,
		Kubelet:  obj.Status.NodeInfo.KubeletVersion,
		OSImage:  obj.Status.NodeInfo.OSImage,
		Kernel:   obj.Status.NodeInfo.KernelVersion,
		Runtime:  obj.Status.NodeInfo.ContainerRuntimeVersion,
		Arch:     obj.Status.NodeInfo.Architecture,
		CPU:      parseQuantity(obj.Status.Allocatable["cpu"]),
		Memory:   parseQuantity(obj.Status.Allocatable["memory"]),
		PodLimit: int(parseQuantity(obj.Status.Allocatable["pods"])),
		Pressure: []string{},
		Created:  obj.Metadata.CreationTimestamp.Unix(),
	}

	for _, a := range obj.Status.Addresses {
		switch {
		case a.Type == "InternalIP" && n.InternalIP == "":
			n.InternalIP = a.Address
		case a.Type == "ExternalIP" && n.ExternalIP == "":
			n.ExternalIP = a.Address
		}
	}

	for _, c := range nodePressureConditions {
		if obj.Status.Conditions.is(c) {
			n.Pressure = append(n.Pressure, c)
		}
	}

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
