package ichorgo

import (
	"context"
	"slices"
	"strings"
)

// The kinds of log backend, in the metrics source shape (promSource.Kind).
const (
	logsKindLoki         = "loki"
	logsKindVictoriaLogs = "victorialogs"
)

// logsCandidate is one log backend found among the Services.
type logsCandidate struct {
	Kind   string     `json:"kind"`
	Source promSource `json:"source"`
}

type logsDiscovery struct {
	Candidates []logsCandidate `json:"candidates"`
}

// LogsDiscover looks for a log backend among the cluster's Services (Loki, VictoriaLogs), to
// reach through the service proxy: {"candidates":[{kind:"loki"|"victorialogs",source:{mode:
// "proxy",kind,namespace,service,port}}]}, the likeliest first, in the metrics source shape
// (see NormalizePromSource) LogsLabels and LogsQuery take. kubeServer is the API server
// address set for the cluster, "" for the kubeconfig's.
func LogsDiscover(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoLogsDiscovery, discoverLogs)
}

func discoverLogs(ctx context.Context, k *kubeClient) (logsDiscovery, error) {
	var list kubeList[promService]

	if err := getList(ctx, k, "/api/v1/services", &list); err != nil {
		return logsDiscovery{}, err
	}

	return logsDiscoveryOf(rankSources(list.Items, logsMatch)), nil
}

func logsDiscoveryOf(sources []promSource) logsDiscovery {
	out := logsDiscovery{Candidates: []logsCandidate{}}
	for _, src := range sources {
		out.Candidates = append(out.Candidates, logsCandidate{Kind: src.Kind, Source: src})
	}

	return out
}

// logsMatch recognises a log backend's query Service by its name and labels, and picks its
// HTTP port. In Loki's simple-scalable chart the gateway answers for the read path, so it
// ranks first, then the read path itself, then a single binary.
func logsMatch(s promService) (promCandidate, bool) {
	name := s.Metadata.Name
	app := s.Metadata.Labels["app.kubernetes.io/name"]
	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(name, w) })
	}

	src := promSource{Mode: promModeProxy, Namespace: s.Metadata.Namespace, Service: name}
	loki := has("loki") || app == "loki"

	var (
		score int
		ports []int
		names []string
	)

	switch {
	// Parts of the stack without the query API, and log shippers named after the backend.
	case has("operator", "exporter", "promtail", "canary", "memberlist", "headless", "-write", "-backend", "ingester", "distributor", "compactor", "index-gateway", "-cache", "ruler", "grafana", "agent", "alloy", "vector", "fluent", "vlinsert", "vlstorage"):
		return promCandidate{}, false
	case loki && has("gateway"):
		src.Kind, score = logsKindLoki, 100
		ports, names = []int{80, 8080, 3100}, []string{"http-metrics", "http"}
	case loki && has("-read"):
		src.Kind, score = logsKindLoki, 90
		ports, names = []int{3100}, []string{"http-metrics"}
	case loki && has("query-frontend"):
		src.Kind, score = logsKindLoki, 85
		ports, names = []int{3100}, []string{"http-metrics"}
	case loki:
		src.Kind, score = logsKindLoki, 70
		ports, names = []int{3100}, []string{"http-metrics"}
	case has("vlselect"):
		src.Kind, score = logsKindVictoriaLogs, 90
		ports, names = []int{9428, 9471}, []string{"http"}
	case has("victoria-logs", "vlogs", "victorialogs") || app == "victoria-logs-single":
		src.Kind, score = logsKindVictoriaLogs, 80
		ports, names = []int{9428}, []string{"http"}
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
