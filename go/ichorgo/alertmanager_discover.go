package ichorgo

import (
	"context"
	"slices"
	"strings"
)

// AlertmanagerDiscover looks for Alertmanager among the cluster's Services (the Prometheus
// Operator's alertmanager-operated, the kube-prometheus-stack and alertmanager charts,
// VictoriaMetrics' vmalertmanager, Mimir's alertmanager), to reach through the service
// proxy: {"sources":[{mode:"proxy",kind,namespace,service,port,pathPrefix}]}, the likeliest
// first, in the metrics source shape (see NormalizePromSource) the Alertmanager calls take.
// kubeServer is the API server address set for the cluster, "" for the kubeconfig's.
func AlertmanagerDiscover(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoAMDiscovery, discoverAM)
}

func discoverAM(ctx context.Context, k *kubeClient) (promDiscovery, error) {
	var list kubeList[promService]

	if err := getList(ctx, k, "/api/v1/services", &list); err != nil {
		return promDiscovery{}, err
	}

	return promDiscovery{Sources: rankSources(list.Items, amMatch)}, nil
}

// amMatch recognises an Alertmanager Service by its name and labels, and picks its HTTP
// port (9093, not the 9094 cluster gossip).
func amMatch(s promService) (promCandidate, bool) {
	name := s.Metadata.Name
	app := s.Metadata.Labels["app.kubernetes.io/name"]
	if app == "" {
		app = s.Metadata.Labels["app"]
	}

	has := func(words ...string) bool {
		return slices.ContainsFunc(words, func(w string) bool { return strings.Contains(name, w) })
	}

	src := promSource{Mode: promModeProxy, Kind: "alertmanager", Namespace: s.Metadata.Namespace, Service: name}

	var (
		score int
		ports = []int{9093}
		names = []string{"web", "http-web", "http"}
	)

	switch {
	// Receivers and bridges named after Alertmanager, not Alertmanager itself.
	case has("operator", "exporter", "webhook", "bot", "bridge", "relay", "forwarder", "discord", "telegram", "slack", "teams", "ntfy", "gotify", "matrix", "signal", "sns", "karma", "alerta"):
		return promCandidate{}, false
	case name == "alertmanager-operated":
		score = 100
	case strings.Contains(name, "mimir") && has("alertmanager"):
		// Mimir serves the Alertmanager API below /alertmanager, per tenant.
		src.Kind, src.PathPrefix, score, ports, names = "mimir", "/alertmanager", 50, []int{8080, 80}, []string{"http-metrics", "http"}
	case has("alertmanager") && app == "alertmanager":
		score = 90
	case has("alertmanager"):
		score = 70
	case app == "alertmanager" || app == "vmalertmanager":
		score = 60
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
