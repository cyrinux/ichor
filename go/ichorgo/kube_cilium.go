package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strconv"
)

const (
	ciliumAgentSelector  = "k8s-app=cilium"
	ciliumAgentContainer = "cilium-agent"
	ciliumConfigMap      = "cilium-config"
	// hubbleDefaultBuffer is Cilium's default hubble-event-buffer-capacity.
	hubbleDefaultBuffer = 4095
)

// ciliumStatus is whether a CNI the app follows flows from runs (Cilium, else Calico), where,
// and whether it records them: Hubble in the Cilium agents, Whisker with Calico (see
// kube_calico.go). Its name predates Calico; the apps bind it as is.
type ciliumStatus struct {
	Installed bool          `json:"installed"`
	CNI       string        `json:"cni,omitempty"`       // flowCNICilium or flowCNICalico
	Namespace string        `json:"namespace,omitempty"` // where the flow source runs
	Version   string        `json:"version,omitempty"`   // the agents' (cilium-agent or calico-node) image tag
	Hubble    bool          `json:"hubble"`              // flows are recorded: Hubble on, or Whisker present
	Buffer    int           `json:"buffer,omitempty"`    // flows each Cilium agent keeps; 0 with Calico
	Agents    []ciliumAgent `json:"agents"`
	Whisker   *whiskerInfo  `json:"whisker,omitempty"` // Calico only: where the flows are served
}

type ciliumAgent struct {
	Node  string `json:"node"`
	Pod   string `json:"pod"`
	Ready bool   `json:"ready"`
}

// KubeCilium tells which CNI the app can follow flows from, through the Kubernetes API
// (os:admin): Cilium, else Calico (3.30+ with Whisker). {"installed","cni","namespace",
// "version","hubble","buffer","agents":[{node,pod,ready}],"whisker":{namespace,pod,port,tls}}:
// hubble is whether flows are recorded (Hubble on, or Whisker found); whisker is set with
// Calico. The agents are found by label in every namespace. kubeServer: see KubePods.
func KubeCilium(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoCiliumStatus, readFlowSource)
}

func readCiliumStatus(ctx context.Context, k *kubeClient) (ciliumStatus, error) {
	out, err := readAgentPods(ctx, k, ciliumAgentSelector, ciliumAgentContainer, flowCNICilium)
	if err != nil || !out.Installed {
		return out, err
	}

	var cm struct {
		Data map[string]string `json:"data"`
	}

	if err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(out.Namespace)+"/configmaps/"+ciliumConfigMap, &cm); err != nil && !isNotFound(err) {
		return out, err
	}

	out.Hubble = cm.Data["enable-hubble"] == "true"
	out.Buffer = hubbleDefaultBuffer

	if n, err := strconv.Atoi(cm.Data["hubble-event-buffer-capacity"]); err == nil && n > 0 {
		out.Buffer = n
	}

	return out, nil
}

// readAgentPods finds a CNI's per-node agents by label (Helm installs them anywhere): the
// pods with container on a node, their image tag as the version, sorted by node.
func readAgentPods(ctx context.Context, k *kubeClient, selector, container, cni string) (ciliumStatus, error) {
	pods, err := listDSPods(ctx, k, selector)
	if err != nil {
		return ciliumStatus{}, err
	}

	out := ciliumStatus{Agents: []ciliumAgent{}}

	for _, p := range pods {
		image := ""
		for _, c := range p.Spec.Containers {
			if c.Name == container {
				image = c.Image
			}
		}

		if image == "" || p.Spec.NodeName == "" {
			continue
		}

		out.Installed, out.CNI, out.Namespace = true, cni, p.Metadata.Namespace
		if out.Version == "" {
			out.Version = parseImageRef(image).Tag
		}

		out.Agents = append(out.Agents, ciliumAgent{Node: p.Spec.NodeName, Pod: p.Metadata.Name, Ready: p.containerReady(container)})
	}

	slices.SortFunc(out.Agents, func(a, b ciliumAgent) int { return cmp.Compare(a.Node, b.Node) })

	return out, nil
}
