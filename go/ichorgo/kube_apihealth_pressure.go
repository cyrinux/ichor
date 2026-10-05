package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"
)

// The API server metrics the report reads. Kubernetes renamed a few over releases; both
// names are kept where it did.
const (
	metricRequests        = "apiserver_request_total"
	metricTerminations    = "apiserver_request_terminations_total" // refused before a handler ran (APF 429s)
	metricRequestDuration = "apiserver_request_duration_seconds"
	metricInflight        = "apiserver_current_inflight_requests"
	metricDispatched      = "apiserver_flowcontrol_dispatched_requests_total"
	metricRejected        = "apiserver_flowcontrol_rejected_requests_total"
	metricInQueue         = "apiserver_flowcontrol_current_inqueue_requests"
	metricWait            = "apiserver_flowcontrol_request_wait_duration_seconds"
	metricExecSeats       = "apiserver_flowcontrol_current_executing_seats"
	metricLimitSeats      = "apiserver_flowcontrol_current_limit_seats"
	metricNominalSeats    = "apiserver_flowcontrol_nominal_limit_seats"
	metricConcurrency     = "apiserver_flowcontrol_request_concurrency_limit"
	metricLongRunning     = "apiserver_longrunning_requests"
	metricWatchEvents     = "apiserver_watch_events_total"
	metricResourceObjects = "apiserver_resource_objects" // 1.34+
	metricStorageObjects  = "apiserver_storage_objects"  // before
	metricEtcdDuration    = "etcd_request_duration_seconds"
)

var apiMetricNames = map[string]bool{
	metricProcessStart: true, metricRequests: true, metricTerminations: true, metricInflight: true,
	metricRequestDuration + "_sum": true, metricRequestDuration + "_count": true,
	metricDispatched: true, metricRejected: true, metricInQueue: true,
	metricWait + "_sum": true, metricWait + "_count": true,
	metricExecSeats: true, metricLimitSeats: true, metricNominalSeats: true, metricConcurrency: true,
	metricLongRunning: true, metricWatchEvents: true, metricResourceObjects: true, metricStorageObjects: true,
	metricEtcdDuration + "_sum": true, metricEtcdDuration + "_count": true,
}

// How many rows the busiest lists keep.
const (
	apiTopRequests = 15
	apiTopClients  = 15
	apiTopCounts   = 10
	apiTopQueued   = 20
)

// fillAPIPressure adds what the metrics window tells to r.
func fillAPIPressure(r apiHealthReport, w promWindow) apiHealthReport {
	r.UptimeSeconds = int64(w.uptime)
	r.WindowSeconds = w.seconds
	r.RequestRate = w.rate(metricRequests, nil)
	r.ErrorRate = w.rate(metricRequests, codeClass("5"))
	// A 429 from API Priority and Fairness or the inflight limit never reaches a handler, so
	// apiserver_request_total does not count it (its 429s are mostly evictions a
	// PodDisruptionBudget refused): terminations do.
	r.ThrottledRate = w.rate(metricTerminations, codeIs("429"))
	r.RejectedRate = w.rate(metricRejected, nil)
	r.WatchEventRate = w.rate(metricWatchEvents, nil)

	inflight := w.after.sumBy(metricInflight, nil, "request_kind")
	r.InflightRead = int(inflight["readOnly"])
	r.InflightMutate = int(inflight["mutating"])
	r.Queued = int(w.after.total(metricInQueue, nil))

	if etcd := w.meanMsBy(metricEtcdDuration, nil); len(etcd) > 0 {
		r.EtcdLatencyMs = etcd[""]
	}

	r.Clients = apiClients(w)
	r.Priorities = apiPriorities(w)
	r.Requests = apiRequests(w)
	r.WatchedKinds, r.Watches = apiWatches(w.after)
	r.Objects = apiObjects(w.after)

	return r
}

func codeIs(code string) func(map[string]string) bool {
	return func(l map[string]string) bool { return l["code"] == code }
}

func codeClass(prefix string) func(map[string]string) bool {
	return func(l map[string]string) bool { return strings.HasPrefix(l["code"], prefix) }
}

// apiClients ranks the flow schemas by request rate; those queued or rejected are kept
// even when idle otherwise.
func apiClients(w promWindow) []apiFlow {
	keys := []string{"flow_schema", "priority_level"}
	dispatched := w.rateBy(metricDispatched, nil, keys...)
	rejected := w.rateBy(metricRejected, nil, keys...)
	queued := w.after.sumBy(metricInQueue, nil, keys...)
	waits := w.meanMsBy(metricWait, nil, keys...)

	flows := []apiFlow{}

	for _, key := range unionKeys(dispatched, rejected, queued) {
		f := apiFlow{Rate: dispatched[key], RejectedRate: rejected[key], Queued: int(queued[key]), WaitMs: waits[key]}
		if f.Rate == 0 && f.RejectedRate == 0 && f.Queued == 0 {
			continue
		}

		parts := promSplit(key)
		f.Name, f.Priority = parts[0], parts[1]
		flows = append(flows, f)
	}

	slices.SortFunc(flows, func(a, b apiFlow) int {
		return cmp.Or(cmp.Compare(b.RejectedRate, a.RejectedRate), cmp.Compare(b.Rate, a.Rate), cmp.Compare(a.Name, b.Name))
	})

	return flows[:min(len(flows), apiTopClients)]
}

// apiPriorities lists the priority levels with their seats in use and their limit.
func apiPriorities(w promWindow) []apiPriority {
	executing := w.after.sumBy(metricExecSeats, nil, "priority_level")
	limits := firstMetric(w.after, "priority_level", metricLimitSeats, metricNominalSeats, metricConcurrency)
	queued := w.after.sumBy(metricInQueue, nil, "priority_level")
	rejected := w.rateBy(metricRejected, nil, "priority_level")

	levels := []apiPriority{}

	for _, name := range unionKeys(executing, limits, queued, rejected) {
		levels = append(levels, apiPriority{
			Name: name, Executing: executing[name], Limit: limits[name],
			Queued: int(queued[name]), RejectedRate: rejected[name],
		})
	}

	slices.SortFunc(levels, func(a, b apiPriority) int {
		return cmp.Or(cmp.Compare(seatShare(b), seatShare(a)), cmp.Compare(a.Name, b.Name))
	})

	return levels
}

func seatShare(p apiPriority) float64 {
	if p.Limit <= 0 {
		return -1 // exempt: never full, listed last
	}

	return p.Executing / p.Limit
}

// firstMetric is the first of names the exposition has, summed by key.
func firstMetric(s promSamples, key string, names ...string) map[string]float64 {
	for _, name := range names {
		if s.has(name) {
			return s.sumBy(name, nil, key)
		}
	}

	return map[string]float64{}
}

// longRunning is a verb whose requests stay open: they have no meaningful duration.
func longRunning(l map[string]string) bool { return l["verb"] == "WATCH" || l["verb"] == "CONNECT" }

// apiRequests ranks verb and resource pairs by request rate. Non-resource paths (/livez,
// /apis...) come with an empty resource and the path as subresource: they are summed into
// one "" row per verb, their latency weighted by rate.
func apiRequests(w promWindow) []apiRequestRow {
	keys := []string{"verb", "resource", "subresource"}
	rates := w.rateBy(metricRequests, nil, keys...)
	errors := w.rateBy(metricRequests, codeClass("5"), keys...)
	latency := w.meanMsBy(metricRequestDuration, func(l map[string]string) bool { return !longRunning(l) }, keys...)

	byRow := map[[2]string]apiRequestRow{}

	for key, rate := range rates {
		if rate == 0 {
			continue
		}

		parts := promSplit(key)
		id := [2]string{parts[0], requestResource(parts[1], parts[2])}
		row := byRow[id]
		total := row.Rate + rate
		row.LatencyMs = (row.LatencyMs*row.Rate + latency[key]*rate) / total
		row.Verb, row.Resource, row.Rate, row.ErrorRate = id[0], id[1], total, row.ErrorRate+errors[key]
		byRow[id] = row
	}

	rows := make([]apiRequestRow, 0, len(byRow))
	for _, row := range byRow {
		rows = append(rows, row)
	}

	slices.SortFunc(rows, func(a, b apiRequestRow) int {
		return cmp.Or(cmp.Compare(b.Rate, a.Rate), cmp.Compare(a.Resource, b.Resource), cmp.Compare(a.Verb, b.Verb))
	})

	return rows[:min(len(rows), apiTopRequests)]
}

// requestResource is "pods", "nodes/status", or "" for a non-resource path.
func requestResource(resource, subresource string) string {
	if resource == "" || subresource == "" {
		return resource
	}

	return resource + "/" + subresource
}

// apiWatches counts the open watches by resource, and in all.
func apiWatches(s promSamples) ([]apiCount, int) {
	watches := s.sumBy(metricLongRunning, func(l map[string]string) bool { return l["verb"] == "WATCH" }, "resource")
	total := 0

	for _, n := range watches {
		total += int(n)
	}

	return topCounts(watches), total
}

// apiObjects is the number of stored objects by resource ("deployments.apps").
func apiObjects(s promSamples) []apiCount {
	if !s.has(metricResourceObjects) {
		return topCounts(s.sumBy(metricStorageObjects, nil, "resource"))
	}

	counts := map[string]float64{}

	for key, n := range s.sumBy(metricResourceObjects, nil, "resource", "group") {
		parts := promSplit(key)
		name := parts[0]

		if parts[1] != "" {
			name += "." + parts[1]
		}

		counts[name] += n
	}

	return topCounts(counts)
}

// topCounts keeps the largest counts; negative ones mean unknown.
func topCounts(counts map[string]float64) []apiCount {
	rows := []apiCount{}

	for name, n := range counts {
		if n > 0 {
			rows = append(rows, apiCount{Resource: name, Count: int(n)})
		}
	}

	slices.SortFunc(rows, func(a, b apiCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Resource, b.Resource))
	})

	return rows[:min(len(rows), apiTopCounts)]
}

// unionKeys is the sorted keys of every map.
func unionKeys(maps ...map[string]float64) []string {
	seen := map[string]bool{}

	for _, m := range maps {
		for k := range m {
			seen[k] = true
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}

// apiQueuedTimeout bounds the queue dump: it only adds detail, the report must not wait.
const apiQueuedTimeout = 4 * time.Second

// readAPIQueued lists the requests waiting in API Priority and Fairness queues, with the
// user that sent each, from the server's debug dump. Best effort: nil when unavailable.
func readAPIQueued(ctx context.Context, k *kubeClient) []apiQueued {
	ctx, cancel := context.WithTimeout(ctx, apiQueuedTimeout)
	defer cancel()

	text, err := k.getText(ctx, "/debug/api_priority_and_fairness/dump_requests?includeRequestDetails=1")
	if err != nil {
		return nil
	}

	return parseAPIQueued(text)
}

// parseAPIQueued reads the dump's comma-separated table, finding columns by their header
// so a release adding one does not shift the others.
func parseAPIQueued(text string) []apiQueued {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 2 {
		return nil
	}

	column := map[string]int{}
	for i, name := range strings.Split(lines[0], ",") {
		column[strings.TrimSpace(name)] = i
	}

	var out []apiQueued

	for _, line := range lines[1:] {
		cells := strings.Split(line, ",")
		cell := func(name string) string {
			if i, ok := column[name]; ok && i < len(cells) {
				return strings.TrimSpace(cells[i])
			}

			return ""
		}

		// Since 1.28 the dump also lists the requests running: index -1 in (or outside) a
		// queue. Before, a level without queues printed "<none>" rows.
		if cell("PriorityLevelName") == "" || cell("FlowSchemaName") == "" || cell("FlowSchemaName") == "<none>" ||
			cell("QueueIndex") == "-1" || cell("RequestIndexInQueue") == "-1" {
			continue
		}

		out = append(out, apiQueued{
			User: cell("UserName"), FlowSchema: cell("FlowSchemaName"), Priority: cell("PriorityLevelName"),
			Verb: cell("Verb"), Path: cell("APIPath"),
		})
		if len(out) == apiTopQueued {
			break
		}
	}

	return out
}
