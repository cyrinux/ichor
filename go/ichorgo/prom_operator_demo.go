package ichorgo

import (
	"encoding/json"
	"fmt"
	"time"
)

// The demo cluster runs a kube-prometheus-stack: a healthy Prometheus and Alertmanager, the
// crash-looping worker's alert firing, a recording rule of the demo app failing to
// evaluate, and two of its pods not answering their scrape.

const demoPromRuleDir = "/etc/prometheus/rules/prometheus-kube-prometheus-stack-prometheus-rulefiles-0/"

// demoPromRules is the demo Prometheus' rules, as GET /api/v1/rules would list them.
func demoPromRules(now time.Time) promRulesResult {
	evaluated := now.Add(-12 * time.Second).UTC().Format(time.RFC3339)

	rule := func(typ, name, state, health, lastError, query, severity string, alerts int) map[string]any {
		r := map[string]any{"type": typ, "name": name, "query": query, "health": health, "lastError": lastError, "lastEvaluation": evaluated,
			"labels": map[string]string{"severity": severity}}
		if typ == promRuleAlerting {
			r["state"], r["duration"], r["alerts"] = state, 900, make([]map[string]string, alerts)
		}

		return r
	}

	group := func(file, name string, rules ...map[string]any) map[string]any {
		return map[string]any{"name": name, "file": demoPromRuleDir + file, "interval": 30, "evaluationTime": 0.0042, "lastEvaluation": evaluated, "rules": rules}
	}

	raw := map[string]any{"groups": []map[string]any{
		group("monitoring-kube-prometheus-stack-kubernetes-apps-0d6f3b5e-1c2a-4e8b-9f70-3a4b5c6d7e8f.yaml", "kubernetes-apps",
			rule(promRuleAlerting, "KubePodCrashLooping", promStateFiring, promHealthOK, "", `max_over_time(kube_pod_container_status_waiting_reason{job="kube-state-metrics",reason="CrashLoopBackOff"}[5m]) >= 1`, "warning", 1),
			rule(promRuleAlerting, "KubeDeploymentReplicasMismatch", promStateInactive, promHealthOK, "", `kube_deployment_spec_replicas{job="kube-state-metrics"} > kube_deployment_status_replicas_available{job="kube-state-metrics"}`, "warning", 0),
			rule(promRuleAlerting, "KubeJobFailed", promStateInactive, promHealthOK, "", `kube_job_failed{job="kube-state-metrics"} > 0`, "warning", 0)),
		group("monitoring-kube-prometheus-stack-node-exporter-4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8.yaml", "node-exporter",
			rule(promRuleAlerting, "NodeFilesystemSpaceFillingUp", promStateInactive, promHealthOK, "", `predict_linear(node_filesystem_avail_bytes{job="node-exporter",fstype!=""}[6h], 24*60*60) < 0`, "warning", 0),
			rule(promRuleAlerting, "NodeHighNumberConntrackEntriesUsed", promStateInactive, promHealthOK, "", `(node_nf_conntrack_entries / node_nf_conntrack_entries_limit) > 0.75`, "warning", 0)),
		group("monitoring-kube-prometheus-stack-k8s.rules.container-cpu-9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d.yaml", "k8s.rules.container_cpu_usage_seconds_total",
			rule(promRuleRecording, "node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate", "", promHealthOK, "", `sum by (namespace, pod, container) (irate(container_cpu_usage_seconds_total{job="kubelet"}[5m]))`, "", 0)),
		group("demo-hello-ichor-7b6a5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d.yaml", "hello-ichor.rules",
			rule(promRuleRecording, "hello_ichor:http_requests:rate5m", "", promHealthErr,
				`found duplicate series for the match group {pod="hello-ichor-7d9c5-abcde"} on the right hand-side of the operation: [{__name__="kube_pod_info", pod="hello-ichor-7d9c5-abcde"}, {__name__="kube_pod_info", pod="hello-ichor-7d9c5-abcde"}];many-to-many matching not allowed: matching labels must be unique on one side`,
				`sum by (pod) (rate(http_requests_total{namespace="demo"}[5m]) * on (pod) group_left (node) kube_pod_info)`, "", 0),
			rule(promRuleAlerting, "HelloIchorHighErrorRate", promStateInactive, promHealthOK, "", `sum(rate(http_requests_total{namespace="demo",code=~"5.."}[5m])) / sum(rate(http_requests_total{namespace="demo"}[5m])) > 0.05`, "critical", 0)),
	}}

	data, _ := json.Marshal(raw) //nolint:errcheck // a literal

	res, _ := parsePromRules(data) //nolint:errcheck // a literal
	res.setOwners(demoPromRuleOwners())

	return res
}

// demoPromRuleOwners are the PrometheusRules the demo's rule files come from.
func demoPromRuleOwners() []kubeRowMeta {
	var owners []kubeRowMeta

	for _, g := range []struct{ ns, name, uid string }{
		{"monitoring", "kube-prometheus-stack-kubernetes-apps", "0d6f3b5e-1c2a-4e8b-9f70-3a4b5c6d7e8f"},
		{"monitoring", "kube-prometheus-stack-node-exporter", "4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8"},
		{"monitoring", "kube-prometheus-stack-k8s.rules.container-cpu", "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"},
		{"demo", "hello-ichor", "7b6a5c4d-3e2f-4a1b-9c8d-7e6f5a4b3c2d"},
	} {
		owners = append(owners, kubeRowMeta{Namespace: g.ns, Name: g.name, UID: g.uid})
	}

	return owners
}

// demoPromTargets is the demo Prometheus' active targets: the nodes, the API server,
// Prometheus itself, and the demo app, two of whose three pods refuse the scrape.
func demoPromTargets(now time.Time) promTargetsResult {
	scraped := now.Add(-9 * time.Second).UTC().Format(time.RFC3339)

	var targets []promRawTarget

	add := func(pool, health, url, lastError string, labels map[string]string) {
		targets = append(targets, promRawTarget{ScrapePool: pool, Health: health, ScrapeURL: url, LastError: lastError, LastScrape: scraped, LastScrapeDuration: 0.012, Labels: labels})
	}

	for i, node := range []string{"demo-cp-1", "demo-cp-2", "demo-cp-3", "demo-worker-1", "demo-worker-2"} {
		ip := fmt.Sprintf("10.0.0.%d", 1+i)
		add("serviceMonitor/monitoring/kube-prometheus-stack-kubelet/0", promHealthUp, "https://"+ip+":10250/metrics", "",
			map[string]string{"job": "kubelet", "namespace": "kube-system", "service": "kube-prometheus-stack-kubelet", "node": node, "instance": ip + ":10250"})
		add("serviceMonitor/monitoring/kube-prometheus-stack-prometheus-node-exporter/0", promHealthUp, "http://"+ip+":9100/metrics", "",
			map[string]string{"job": "node-exporter", "namespace": "monitoring", "service": "kube-prometheus-stack-prometheus-node-exporter", "instance": ip + ":9100"})
	}

	add("serviceMonitor/monitoring/kube-prometheus-stack-apiserver/0", promHealthUp, "https://10.0.0.1:6443/metrics", "",
		map[string]string{"job": "apiserver", "namespace": "default", "service": "kubernetes", "instance": "10.0.0.1:6443"})
	add("serviceMonitor/monitoring/kube-prometheus-stack-prometheus/0", promHealthUp, "http://10.244.1.20:9090/metrics", "",
		map[string]string{"job": "kube-prometheus-stack-prometheus", "namespace": "monitoring", "service": "kube-prometheus-stack-prometheus", "pod": "prometheus-kube-prometheus-stack-prometheus-0", "instance": "10.244.1.20:9090"})

	for i, pod := range []string{"hello-ichor-7d9c5-abcde", "hello-ichor-7d9c5-fghij", "hello-ichor-7d9c5-klmno"} {
		instance := fmt.Sprintf("10.244.%d.%d:8080", 1+i%2, 31+i)
		url := "http://" + instance + "/metrics"
		health, lastError := promHealthDown, `Get "`+url+`": dial tcp `+instance+`: connect: connection refused`

		if i == 0 {
			health, lastError = promHealthUp, ""
		}

		add("serviceMonitor/demo/hello-ichor/0", health, url, lastError,
			map[string]string{"job": "hello-ichor", "namespace": "demo", "service": "hello-ichor", "pod": pod, "endpoint": "metrics", "instance": instance})
	}

	return groupPromTargets(targets)
}

// demoCheckupMonitoring is the checkup's monitoring section on the demo's Prometheus.
func demoCheckupMonitoring(now time.Time) checkupSection {
	targets, rules := demoPromTargets(now), demoPromRules(now)

	return newSection(checkMonitoring, targets.Total+rules.Counts.Rules, monitoringFindings(demoPromDiscovery().Sources[0], &targets, &rules))
}

// demoPromOperator is the demo's Prometheus Operator: one Prometheus and one Alertmanager,
// both up.
func demoPromOperator() promOperatorStatus {
	ready := []kubeCondition{{Type: "Available", Status: "True"}, {Type: "Reconciled", Status: "True"}}

	server := func(name, version string) promOperatorObject {
		var o promOperatorObject
		o.Metadata.Namespace, o.Metadata.Name, o.Spec.Version = "monitoring", name, version
		o.Status.AvailableReplicas, o.Status.Conditions = 1, ready

		return o
	}

	return promOperatorStatus{
		Installed:       true,
		Prometheuses:    []promOperatorServer{promServerOf(server("kube-prometheus-stack-prometheus", "v3.5.0"), true)},
		Alertmanagers:   []promOperatorServer{promServerOf(server("kube-prometheus-stack-alertmanager", "v0.28.1"), false)},
		ServiceMonitors: 14, PodMonitors: 2, PrometheusRules: 35,
	}
}
