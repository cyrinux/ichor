package ichorgo

// promPreset is a ready-made panel the user can add and edit. Unit tells the app how to
// format values: percent, bytes, cores, persec, count or "" (plain). Legend is a template
// of label names in {{ }}, "" for the series name.
type promPreset struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Query  string `json:"query"`
	Unit   string `json:"unit"`
	Legend string `json:"legend"`
}

// The metrics of node-exporter, cAdvisor (kubelet), kube-state-metrics and the API server,
// as kube-prometheus-stack and most setups scrape them.
var promPresets = []promPreset{
	{ID: "cpu", Title: "Cluster CPU usage", Unit: "percent",
		Query: `100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])))`},
	{ID: "memory", Title: "Cluster memory usage", Unit: "percent",
		Query: `100 * (1 - sum(node_memory_MemAvailable_bytes) / sum(node_memory_MemTotal_bytes))`},
	{ID: "cpu-namespace", Title: "CPU by namespace", Unit: "cores", Legend: "{{namespace}}",
		Query: `topk(10, sum by (namespace) (rate(container_cpu_usage_seconds_total{container!=""}[5m])))`},
	{ID: "memory-namespace", Title: "Memory by namespace", Unit: "bytes", Legend: "{{namespace}}",
		Query: `topk(10, sum by (namespace) (container_memory_working_set_bytes{container!=""}))`},
	{ID: "restarts", Title: "Container restarts (1 h)", Unit: "count", Legend: "{{namespace}}/{{pod}}",
		Query: `topk(10, sum by (namespace, pod) (increase(kube_pod_container_status_restarts_total[1h])) > 0)`},
	{ID: "apiserver-errors", Title: "API server 5xx", Unit: "persec", Legend: "{{verb}}",
		Query: `sum by (verb) (rate(apiserver_request_total{code=~"5.."}[5m]))`},
	{ID: "filesystem", Title: "Node disk free", Unit: "percent", Legend: "{{instance}} {{mountpoint}}",
		Query: `100 * node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|squashfs|ramfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|squashfs|ramfs"}`},
}

// PromPresets returns the built-in panels: [{id,title,query,unit,legend}].
func PromPresets() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(promPresets)
}
