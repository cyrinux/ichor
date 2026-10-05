package ichorgo

// demoAPIHealth is a busy API server: a service account lists pods in a loop, so the
// service-accounts flow queues and its priority level is nearly full.
func demoAPIHealth() apiHealthReport {
	checks := func(names ...string) []apiCheck {
		out := make([]apiCheck, 0, len(names))
		for _, n := range names {
			out = append(out, apiCheck{Name: n, OK: true})
		}

		return out
	}

	report := apiHealthReport{
		Version: "v1.34.1",
		Ready: apiProbe{OK: true, Checks: checks("ping", "log", "etcd", "etcd-readiness", "informer-sync",
			"poststarthook/start-apiserver-admission-initializer", "poststarthook/apiservice-registration-controller", "shutdown")},
		Live:           apiProbe{OK: true, Checks: checks("ping", "log", "etcd", "poststarthook/start-apiserver-admission-initializer")},
		UptimeSeconds:  11 * 24 * 3600,
		WindowSeconds:  5,
		RequestRate:    142.6,
		ErrorRate:      0.2,
		InflightRead:   18,
		InflightMutate: 3,
		Queued:         4,
		Watches:        612,
		WatchEventRate: 88.4,
		EtcdLatencyMs:  6.8,
		Clients: []apiFlow{
			{Name: "service-accounts", Priority: "workload-low", Rate: 61.2, Queued: 4, WaitMs: 140},
			{Name: "system-nodes", Priority: "system", Rate: 32.4},
			{Name: "kube-controller-manager", Priority: "workload-high", Rate: 18.8},
			{Name: "kube-system-service-accounts", Priority: "workload-high", Rate: 12.6},
			{Name: "system-leader-election", Priority: "leader-election", Rate: 7.4},
			{Name: "probes", Priority: "exempt", Rate: 4.2},
			{Name: "kube-scheduler", Priority: "workload-high", Rate: 3.8},
			{Name: "global-default", Priority: "global-default", Rate: 2.2},
		},
		Priorities: []apiPriority{
			{Name: "workload-low", Executing: 21, Limit: 24, Queued: 4},
			{Name: "system", Executing: 6, Limit: 30},
			{Name: "workload-high", Executing: 5, Limit: 40},
			{Name: "global-default", Executing: 1, Limit: 20},
			{Name: "leader-election", Executing: 0, Limit: 10},
			{Name: "exempt", Executing: 2},
		},
		Requests: []apiRequestRow{
			{Verb: "LIST", Resource: "pods", Rate: 48.6, LatencyMs: 412},
			{Verb: "GET", Resource: "leases", Rate: 14.2, LatencyMs: 3.1},
			{Verb: "PUT", Resource: "leases", Rate: 13.8, LatencyMs: 7.4},
			{Verb: "GET", Resource: "nodes", Rate: 9.8, LatencyMs: 2.2},
			{Verb: "PATCH", Resource: "nodes/status", Rate: 6.4, LatencyMs: 11.6},
			{Verb: "LIST", Resource: "configmaps", Rate: 5.2, ErrorRate: 0.2, LatencyMs: 96},
			{Verb: "GET", Resource: "", Rate: 4.2, LatencyMs: 0.6},
			{Verb: "WATCH", Resource: "pods", Rate: 3.6},
			{Verb: "CREATE", Resource: "events", Rate: 2.8, LatencyMs: 9.2},
			{Verb: "LIST", Resource: "secrets", Rate: 1.6, LatencyMs: 38},
		},
		WatchedKinds: []apiCount{
			{Resource: "pods", Count: 148}, {Resource: "configmaps", Count: 96}, {Resource: "secrets", Count: 81},
			{Resource: "nodes", Count: 64}, {Resource: "endpointslices", Count: 52}, {Resource: "services", Count: 47},
		},
		Objects: []apiCount{
			{Resource: "events", Count: 4210}, {Resource: "replicasets.apps", Count: 1186}, {Resource: "secrets", Count: 642},
			{Resource: "configmaps", Count: 388}, {Resource: "pods", Count: 214}, {Resource: "leases.coordination.k8s.io", Count: 96},
		},
		QueuedRequests: []apiQueued{
			{User: "system:serviceaccount:monitoring:pod-exporter", FlowSchema: "service-accounts", Priority: "workload-low", Verb: "list", Path: "/api/v1/pods"},
			{User: "system:serviceaccount:monitoring:pod-exporter", FlowSchema: "service-accounts", Priority: "workload-low", Verb: "list", Path: "/api/v1/pods"},
			{User: "system:serviceaccount:media:jellyfin", FlowSchema: "service-accounts", Priority: "workload-low", Verb: "get", Path: "/api/v1/namespaces/media/configmaps/jellyfin"},
		},
	}
	report.Status = apiVerdict(report)

	return report
}
