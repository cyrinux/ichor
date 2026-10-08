package ichorgo

import (
	"hash/fnv"
	"math"
	"slices"
	"strings"
)

func demoPromDiscovery() promDiscovery {
	return promDiscovery{Sources: []promSource{
		{Mode: promModeProxy, Kind: "prometheus", Namespace: "monitoring", Service: "prometheus-operated", Port: 9090},
	}}
}

// demoPromResult makes up gently moving series for any query: one per namespace or node
// when it groups by one, else a single series, scaled from what the query reads.
func demoPromResult(query string, grid promGrid) promResult {
	h := fnv.New32a()
	h.Write([]byte(query)) //nolint:errcheck
	seed := float64(h.Sum32()%1000) / 1000

	var labels []map[string]string

	switch {
	case strings.Contains(query, "namespace"):
		for _, ns := range []string{"kube-system", "monitoring", "argocd", "default"} {
			labels = append(labels, map[string]string{"namespace": ns})
		}
	case strings.Contains(query, "instance"):
		for _, n := range demoNodes() {
			labels = append(labels, map[string]string{"instance": n.Node + ":9100"})
		}
	default:
		labels = []map[string]string{{}}
	}

	base := 1.5
	switch {
	case strings.HasPrefix(query, "100"):
		base = 40
	case strings.Contains(query, "bytes"):
		base = 2 << 30
	}

	res := promResult{ResultType: "matrix", Times: grid.times(), Series: []promSeries{}, Warnings: []string{}, Total: len(labels)}
	if grid.n == 1 {
		res.ResultType = "vector"
	}

	for i, l := range labels {
		scale := base * (1 - 0.15*float64(i)) * (0.8 + 0.4*seed)
		values := make([]*float64, grid.n)

		for j, t := range res.Times {
			v := scale * (1 + 0.15*math.Sin(float64(t)/1000/900+seed*6+float64(i)))
			values[j] = &v
		}

		res.Series = append(res.Series, promSeries{Name: promSeriesName(l), Labels: l, Values: values})
	}

	return res
}

// demoPromMetricNames is what the demo cluster's Prometheus would list: enough for every
// preset and the usual questions (node-exporter, cAdvisor, kube-state-metrics, the API
// server, etcd, CoreDNS).
func demoPromMetricNames() []string {
	names := []string{
		"apiserver_request_duration_seconds_bucket", "apiserver_request_total", "apiserver_current_inflight_requests",
		"container_cpu_usage_seconds_total", "container_memory_working_set_bytes", "container_memory_rss",
		"container_network_receive_bytes_total", "container_network_transmit_bytes_total",
		"container_fs_reads_bytes_total", "container_fs_writes_bytes_total", "container_oom_events_total",
		"coredns_dns_requests_total", "coredns_dns_responses_total", "coredns_dns_request_duration_seconds_bucket",
		"etcd_disk_backend_commit_duration_seconds_bucket", "etcd_disk_wal_fsync_duration_seconds_bucket",
		"etcd_server_leader_changes_seen_total", "etcd_server_has_leader", "etcd_mvcc_db_total_size_in_bytes",
		"etcd_network_peer_round_trip_time_seconds_bucket", "etcd_server_proposals_failed_total",
		"kube_deployment_status_replicas_available", "kube_deployment_spec_replicas", "kube_daemonset_status_number_unavailable",
		"kube_node_status_condition", "kube_node_status_allocatable", "kube_node_status_capacity",
		"kube_pod_container_resource_requests", "kube_pod_container_resource_limits",
		"kube_pod_container_status_restarts_total", "kube_pod_container_status_waiting_reason",
		"kube_pod_status_phase", "kube_pod_info", "kube_persistentvolumeclaim_status_phase",
		"kubelet_volume_stats_used_bytes", "kubelet_volume_stats_capacity_bytes", "kubelet_running_pods",
		"kubelet_pleg_relist_duration_seconds_bucket",
		"node_boot_time_seconds", "node_cpu_seconds_total", "node_disk_io_time_seconds_total",
		"node_disk_read_bytes_total", "node_disk_written_bytes_total",
		"node_filesystem_avail_bytes", "node_filesystem_size_bytes", "node_filesystem_free_bytes",
		"node_load1", "node_load5", "node_load15",
		"node_memory_MemAvailable_bytes", "node_memory_MemTotal_bytes", "node_memory_MemFree_bytes",
		"node_memory_Cached_bytes", "node_memory_Buffers_bytes", "node_memory_SwapFree_bytes",
		"node_network_receive_bytes_total", "node_network_transmit_bytes_total",
		"node_network_receive_errs_total", "node_network_transmit_errs_total",
		"node_nf_conntrack_entries", "node_nf_conntrack_entries_limit", "node_timex_offset_seconds",
		"node_vmstat_pgmajfault", "up",
	}

	slices.Sort(names)

	return names
}
