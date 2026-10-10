package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
)

const promMaxCandidates = 10

// promDiscovery lists the query APIs found in the cluster, the likeliest first.
type promDiscovery struct {
	Sources []promSource `json:"sources"`
}

// PromDiscover looks for Prometheus-compatible query APIs among the cluster's Services
// (Prometheus, Thanos Query, Mimir, VictoriaMetrics), to reach through the service proxy:
// {"sources":[{mode:"proxy",kind,namespace,service,port,pathPrefix}]}, the likeliest
// first. kubeServer is the API server address set for the cluster, "" for the kubeconfig's.
func PromDiscover(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoPromDiscovery, discoverProm)
}

type promService struct {
	Metadata struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Ports []struct {
			Name     string `json:"name"`
			Port     int    `json:"port"`
			Protocol string `json:"protocol"`
		} `json:"ports"`
	} `json:"spec"`
}

func discoverProm(ctx context.Context, k *kubeClient) (promDiscovery, error) {
	var list kubeList[promService]

	if err := getList(ctx, k, "/api/v1/services", &list); err != nil {
		return promDiscovery{}, err
	}

	return promDiscovery{Sources: promCandidates(list.Items)}, nil
}

type promCandidate struct {
	src   promSource
	score int
}

func promCandidates(services []promService) []promSource {
	return rankSources(services, promMatch)
}

// rankSources keeps the Services match recognises, the best score first.
func rankSources(services []promService, match func(promService) (promCandidate, bool)) []promSource {
	var found []promCandidate

	for _, s := range services {
		if c, ok := match(s); ok {
			found = append(found, c)
		}
	}

	slices.SortStableFunc(found, func(a, b promCandidate) int {
		return cmp.Or(b.score-a.score, cmp.Compare(a.src.Namespace, b.src.Namespace), cmp.Compare(a.src.Service, b.src.Service))
	})

	out := []promSource{}
	for _, c := range found[:min(len(found), promMaxCandidates)] {
		out = append(out, c.src)
	}

	return out
}

// promMatch recognises a query API Service by its name and labels, and picks its port.
func promMatch(s promService) (promCandidate, bool) {
	name := s.Metadata.Name
	app := s.Metadata.Labels["app.kubernetes.io/name"]
	component := s.Metadata.Labels["app.kubernetes.io/component"]
	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(name, w) })
	}

	src := promSource{Mode: promModeProxy, Namespace: s.Metadata.Namespace, Service: name}

	var (
		score int
		ports []int
		names []string
	)

	switch {
	case has("alertmanager", "operator", "exporter", "pushgateway", "adapter", "kube-state", "grafana", "-agent", "ruler", "ingester", "distributor", "compactor", "store-gateway",
		"thanos-discovery", "sidecar", "thanos-receive", "storegateway", "thanos-bucket", "thanos-rule", "thanos-compact",
		// Mimir parts without the query API (the gossip ring reaches every component).
		"query-scheduler", "gossip-ring", "memcached", "minio", "-cache"):
		return promCandidate{}, false
	case name == "prometheus-operated":
		src.Kind, score, ports, names = "prometheus", 100, []int{9090}, []string{"web"}
	case strings.Contains(name, "mimir") && has("nginx", "gateway"):
		src.Kind, src.PathPrefix, score, ports, names = "mimir", "/prometheus", 90, []int{80, 8080}, []string{"http-metrics", "http"}
	case strings.Contains(name, "mimir") && has("query-frontend"):
		src.Kind, src.PathPrefix, score, ports, names = "mimir", "/prometheus", 85, []int{8080}, []string{"http-metrics", "http"}
	case strings.Contains(name, "mimir") || app == "mimir":
		// Monolithic (-target=all) or the querier: the query API on the HTTP port too.
		src.Kind, src.PathPrefix, score, ports, names = "mimir", "/prometheus", 80, []int{8080, 80}, []string{"http-metrics", "http"}
	case has("thanos-query", "thanos-querier"):
		src.Kind, score, ports, names = "thanos", 80, []int{9090, 10902}, []string{"http"}
	case has("vmsingle", "victoria-metrics-single"):
		src.Kind, score, ports, names = "victoriametrics", 85, []int{8428}, []string{"http"}
	case has("vmselect", "victoria-metrics-cluster-vmselect"):
		src.Kind, src.PathPrefix, score, ports, names = "victoriametrics", "/select/0/prometheus", 80, []int{8481}, []string{"http"}
	case app == "prometheus" || strings.Contains(name, "prometheus") && component != "":
		src.Kind, score, ports, names = "prometheus", 70, []int{9090}, []string{"web", "http-web", "http"}
	case strings.Contains(name, "prometheus"):
		src.Kind, score, ports, names = "prometheus", 60, []int{9090}, []string{"web", "http-web", "http"}
	default:
		return promCandidate{}, false
	}

	for _, p := range s.Spec.Ports {
		if p.Protocol != "" && p.Protocol != "TCP" {
			continue
		}

		if slices.Contains(ports, p.Port) || slices.Contains(names, p.Name) {
			src.Port = p.Port

			return promCandidate{src: src, score: score}, true
		}
	}

	return promCandidate{}, false
}
