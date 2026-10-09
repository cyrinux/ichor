package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// `kubectl top`: CPU and memory in use, from metrics-server (metrics.k8s.io/v1beta1), next to
// what each node can give and what each pod asked for. Kubeconfig-only clusters (EKS, GKE,
// AKS) have no Talos stats nor Prometheus by default, but almost always run metrics-server.

const metricsGroup = "/apis/metrics.k8s.io/v1beta1"

// maxSelectorLength bounds a label selector from the app.
const maxSelectorLength = 1024

// kubeTopNodes is KubeTopNodes' answer. Available false: no metrics-server (not installed, or
// its aggregated API is down); Forbidden: the credentials may not read node metrics.
type kubeTopNodes struct {
	Available bool          `json:"available"`
	Forbidden bool          `json:"forbidden"`
	Nodes     []kubeTopNode `json:"nodes"`
}

// kubeTopNode is one node's usage: CPU in cores, memory in bytes, percent of allocatable.
type kubeTopNode struct {
	Name              string  `json:"name"`
	CPU               float64 `json:"cpu"`
	Memory            float64 `json:"memory"`
	CPUAllocatable    float64 `json:"cpuAllocatable"`
	MemoryAllocatable float64 `json:"memoryAllocatable"`
	CPUPercent        float64 `json:"cpuPercent"`
	MemoryPercent     float64 `json:"memoryPercent"`
}

// kubeTopPods is KubeTopPods' answer, Available and Forbidden as in kubeTopNodes.
// BoundsRead: the pods' requests and limits were read (one namespace or one pod); false for
// every namespace, where only the usage is (the bounds would mean every pod's full object).
type kubeTopPods struct {
	Available  bool         `json:"available"`
	Forbidden  bool         `json:"forbidden"`
	BoundsRead bool         `json:"boundsRead"`
	Pods       []kubeTopPod `json:"pods"`
}

// kubeTopPod is one pod's usage (summed over its containers) with its requests and limits.
// A limit is 0 when some container has none: the pod is then not bounded.
type kubeTopPod struct {
	Namespace     string  `json:"namespace"`
	Name          string  `json:"name"`
	Node          string  `json:"node"`
	CPU           float64 `json:"cpu"`
	Memory        float64 `json:"memory"`
	CPURequest    float64 `json:"cpuRequest"`
	CPULimit      float64 `json:"cpuLimit"`
	MemoryRequest float64 `json:"memoryRequest"`
	MemoryLimit   float64 `json:"memoryLimit"`
}

type metricsUsage struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type nodeMetrics struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Usage metricsUsage `json:"usage"`
}

type podMetrics struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Containers []struct {
		Usage metricsUsage `json:"usage"`
	} `json:"containers"`
}

// podResources is the part of a pod top needs: where it runs and what its containers asked.
type podResources struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Resources struct {
				Requests map[string]string `json:"requests"`
				Limits   map[string]string `json:"limits"`
			} `json:"resources"`
		} `json:"containers"`
	} `json:"spec"`
}

// KubeTopNodes is the CPU and memory each node uses, as a JSON kubeTopNodes.
func KubeTopNodes(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	return kubeReadJSON(target, demoTopNodesFor(target), readTopNodes)
}

// KubeTopPods is the CPU and memory the pods of namespace ("" for all) matching selector
// ("" for all, `app=web,tier!=db` form) use, as a JSON kubeTopPods. The requests and limits
// are joined for one namespace only (see kubeTopPods.BoundsRead).
func KubeTopPods(configYAML, contextName, kubeServer, namespace, selector string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))
	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	selector = strings.TrimSpace(selector)
	if !validSelector(selector) {
		return "", errors.New("the label selector must be key=value, key!=value or key pairs separated by commas")
	}

	return kubeReadJSON(target, demoTopPods(namespace), func(ctx context.Context, k *kubeClient) (kubeTopPods, error) {
		return readTopPods(ctx, k, namespace, selector)
	})
}

// validSelector accepts the equality form of a label selector: terms `key`, `!key`, `key=v`,
// `key==v` or `key!=v`, comma-separated. Set-based terms (`in`, `notin`) are not needed here.
func validSelector(s string) bool {
	if s == "" {
		return true
	}

	if len(s) > maxSelectorLength {
		return false
	}

	for _, term := range strings.Split(s, ",") {
		key, value, hasValue := strings.Cut(strings.TrimPrefix(term, "!"), "=")
		if hasValue {
			key = strings.TrimSuffix(key, "!")
			value = strings.TrimPrefix(value, "=")
		}

		if !selectorToken(key) || hasValue && value != "" && !selectorToken(value) {
			return false
		}
	}

	return true
}

func readTopNodes(ctx context.Context, k *kubeClient) (kubeTopNodes, error) {
	var metrics kubeList[nodeMetrics]
	if err := getList(ctx, k, metricsGroup+"/nodes", &metrics); err != nil {
		return metricsUnavailable[kubeTopNodes](err, func(forbidden bool) kubeTopNodes {
			return kubeTopNodes{Forbidden: forbidden, Nodes: []kubeTopNode{}}
		})
	}

	var nodes kubeList[kubeNodeObject]
	if err := getList(ctx, k, "/api/v1/nodes", &nodes); err != nil {
		return kubeTopNodes{}, err
	}

	allocatable := map[string]kubeNodeObject{}
	for _, n := range nodes.Items {
		allocatable[n.Metadata.Name] = n
	}

	out := kubeTopNodes{Available: true, Nodes: []kubeTopNode{}}

	for _, m := range metrics.Items {
		node := allocatable[m.Metadata.Name]
		n := kubeTopNode{
			Name:              m.Metadata.Name,
			CPU:               parseQuantity(m.Usage.CPU),
			Memory:            parseQuantity(m.Usage.Memory),
			CPUAllocatable:    parseQuantity(node.Status.Allocatable["cpu"]),
			MemoryAllocatable: parseQuantity(node.Status.Allocatable["memory"]),
		}
		n.CPUPercent, n.MemoryPercent = percentOf(n.CPU, n.CPUAllocatable), percentOf(n.Memory, n.MemoryAllocatable)
		out.Nodes = append(out.Nodes, n)
	}

	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })

	return out, nil
}

func readTopPods(ctx context.Context, k *kubeClient, namespace, selector string) (kubeTopPods, error) {
	query := ""
	if selector != "" {
		query = "?labelSelector=" + url.QueryEscape(selector)
	}

	var metrics kubeList[podMetrics]
	if err := getList(ctx, k, scopedPath(metricsGroup, namespace, "pods")+query, &metrics); err != nil {
		return metricsUnavailable[kubeTopPods](err, func(forbidden bool) kubeTopPods {
			return kubeTopPods{Forbidden: forbidden, Pods: []kubeTopPod{}}
		})
	}

	// Every namespace: the usage only. The bounds come from the pods' full objects (Table rows
	// do not carry them), the whole cluster's on a phone, on every refresh.
	var pods kubeList[podResources]

	if namespace != "" {
		if err := getList(ctx, k, scopedPath("/api/v1", namespace, "pods")+query, &pods); err != nil {
			return kubeTopPods{}, err
		}
	}

	specs := map[string]podResources{}
	for _, p := range pods.Items {
		specs[p.Metadata.Namespace+"/"+p.Metadata.Name] = p
	}

	out := kubeTopPods{Available: true, BoundsRead: namespace != "", Pods: []kubeTopPod{}}

	for _, m := range metrics.Items {
		p := topPodOf(m, specs[m.Metadata.Namespace+"/"+m.Metadata.Name])
		out.Pods = append(out.Pods, p)
	}

	sort.Slice(out.Pods, func(i, j int) bool {
		a, b := out.Pods[i], out.Pods[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}

		return a.Name < b.Name
	})

	return out, nil
}

// KubeTopPod is the CPU and memory one pod uses, with its requests and limits, as a JSON
// kubeTopPods holding that pod (none when metrics-server has none for it yet): two reads, the
// pod's metrics and the pod, as `kubectl top pod NAME` does.
func KubeTopPod(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))

	if err := validateKubeName("pod", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoTopPod(namespace, name), func(ctx context.Context, k *kubeClient) (kubeTopPods, error) {
		return readTopPod(ctx, k, namespace, name)
	})
}

func readTopPod(ctx context.Context, k *kubeClient, namespace, name string) (kubeTopPods, error) {
	var m podMetrics
	if err := k.get(ctx, scopedPath(metricsGroup, namespace, "pods")+"/"+url.PathEscape(name), &m); err != nil {
		return metricsUnavailable[kubeTopPods](err, func(forbidden bool) kubeTopPods {
			return kubeTopPods{Forbidden: forbidden, Pods: []kubeTopPod{}}
		})
	}

	var spec podResources
	if err := k.get(ctx, podPath(namespace, name), &spec); err != nil {
		return kubeTopPods{}, err
	}

	return kubeTopPods{Available: true, BoundsRead: true, Pods: []kubeTopPod{topPodOf(m, spec)}}, nil
}

func topPodOf(m podMetrics, spec podResources) kubeTopPod {
	p := kubeTopPod{Namespace: m.Metadata.Namespace, Name: m.Metadata.Name, Node: spec.Spec.NodeName}

	for _, c := range m.Containers {
		p.CPU += parseQuantity(c.Usage.CPU)
		p.Memory += parseQuantity(c.Usage.Memory)
	}

	cpuBounded, memoryBounded := len(spec.Spec.Containers) > 0, len(spec.Spec.Containers) > 0

	for _, c := range spec.Spec.Containers {
		p.CPURequest += parseQuantity(c.Resources.Requests["cpu"])
		p.MemoryRequest += parseQuantity(c.Resources.Requests["memory"])
		cpuBounded = cpuBounded && c.Resources.Limits["cpu"] != ""
		memoryBounded = memoryBounded && c.Resources.Limits["memory"] != ""
		p.CPULimit += parseQuantity(c.Resources.Limits["cpu"])
		p.MemoryLimit += parseQuantity(c.Resources.Limits["memory"])
	}

	if !cpuBounded {
		p.CPULimit = 0
	}

	if !memoryBounded {
		p.MemoryLimit = 0
	}

	return p
}

// metricsUnavailable turns a metrics API refusal into an answer: not found (no
// metrics-server) or unavailable (its aggregated API is down) give an empty result, forbidden
// says so. Any other error stays an error.
func metricsUnavailable[T any](err error, empty func(forbidden bool) T) (T, error) {
	switch kubeCode(err) {
	case http.StatusNotFound, http.StatusServiceUnavailable:
		return empty(false), nil
	case http.StatusForbidden:
		return empty(true), nil
	default:
		var zero T

		return zero, err
	}
}
