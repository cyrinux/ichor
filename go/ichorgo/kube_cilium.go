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

// ciliumStatus is whether Cilium runs, where, and whether Hubble records flows.
type ciliumStatus struct {
	Installed bool          `json:"installed"`
	Namespace string        `json:"namespace,omitempty"`
	Version   string        `json:"version,omitempty"` // the agents' image tag
	Hubble    bool          `json:"hubble"`
	Buffer    int           `json:"buffer,omitempty"` // flows each agent keeps
	Agents    []ciliumAgent `json:"agents"`
}

type ciliumAgent struct {
	Node  string `json:"node"`
	Pod   string `json:"pod"`
	Ready bool   `json:"ready"`
}

// KubeCilium tells whether Cilium runs in the cluster, through the Kubernetes API (os:admin):
// {"installed","namespace","version","hubble","buffer","agents":[{node,pod,ready}]}. Its agents
// are found by label in every namespace (Helm installs Cilium anywhere). kubeServer: see KubePods.
func KubeCilium(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoCiliumStatus, readCiliumStatus)
}

func readCiliumStatus(ctx context.Context, k *kubeClient) (ciliumStatus, error) {
	pods, err := listDSPods(ctx, k, ciliumAgentSelector)
	if err != nil {
		return ciliumStatus{}, err
	}

	out := ciliumStatus{Agents: []ciliumAgent{}}

	for _, p := range pods {
		image := ""
		for _, c := range p.Spec.Containers {
			if c.Name == ciliumAgentContainer {
				image = c.Image
			}
		}

		if image == "" || p.Spec.NodeName == "" {
			continue
		}

		out.Installed, out.Namespace = true, p.Metadata.Namespace
		if out.Version == "" {
			out.Version = parseImageRef(image).Tag
		}

		out.Agents = append(out.Agents, ciliumAgent{Node: p.Spec.NodeName, Pod: p.Metadata.Name, Ready: p.containerReady(ciliumAgentContainer)})
	}

	slices.SortFunc(out.Agents, func(a, b ciliumAgent) int { return cmp.Compare(a.Node, b.Node) })

	if !out.Installed {
		return out, nil
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
