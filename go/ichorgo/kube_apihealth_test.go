package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// apiMetricsAt is an API server exposition whose counters are scaled by step, so two
// steps a window apart give known rates.
func apiMetricsAt(step float64, started float64) string {
	return fmt.Sprintf(`# HELP apiserver_request_total Counter of apiserver requests.
# TYPE apiserver_request_total counter
apiserver_request_total{code="200",component="apiserver",dry_run="",group="",resource="pods",scope="cluster",subresource="",verb="LIST",version="v1"} %[1]g
apiserver_request_total{code="200",component="apiserver",dry_run="",group="coordination.k8s.io",resource="leases",scope="namespace",subresource="",verb="PUT",version="v1"} %[2]g
apiserver_request_total{code="500",component="apiserver",dry_run="",group="",resource="configmaps",scope="namespace",subresource="",verb="LIST",version="v1"} %[3]g
apiserver_request_total{code="429",component="apiserver",dry_run="",group="",resource="pods",scope="namespace",subresource="eviction",verb="CREATE",version="v1"} 7
apiserver_request_total{code="200",component="",dry_run="",group="",resource="",scope="",subresource="/livez",verb="GET",version=""} %[2]g
apiserver_request_total{code="200",component="",dry_run="",group="",resource="",scope="",subresource="/readyz",verb="GET",version=""} %[2]g
apiserver_request_terminations_total{code="429",component="apiserver",group="",resource="pods",scope="cluster",subresource="",verb="LIST",version="v1"} %[3]g
apiserver_request_total{code="201",component="apiserver",dry_run="",group="",resource="nodes",scope="cluster",subresource="status",verb="PATCH",version="v1"} %[3]g
apiserver_request_duration_seconds_sum{component="apiserver",dry_run="",group="",resource="pods",scope="cluster",subresource="",verb="LIST",version="v1"} %[4]g
apiserver_request_duration_seconds_count{component="apiserver",dry_run="",group="",resource="pods",scope="cluster",subresource="",verb="LIST",version="v1"} %[1]g
apiserver_request_duration_seconds_bucket{component="apiserver",dry_run="",group="",resource="pods",scope="cluster",subresource="",verb="LIST",version="v1",le="+Inf"} %[1]g
apiserver_current_inflight_requests{request_kind="mutating"} 2
apiserver_current_inflight_requests{request_kind="readOnly"} 9
apiserver_flowcontrol_dispatched_requests_total{flow_schema="service-accounts",priority_level="workload-low"} %[1]g
apiserver_flowcontrol_dispatched_requests_total{flow_schema="system-nodes",priority_level="system"} %[2]g
apiserver_flowcontrol_dispatched_requests_total{flow_schema="probes",priority_level="exempt"} 40
apiserver_flowcontrol_rejected_requests_total{flow_schema="service-accounts",priority_level="workload-low",reason="queue-full"} %[3]g
apiserver_flowcontrol_current_inqueue_requests{flow_schema="service-accounts",priority_level="workload-low"} 3
apiserver_flowcontrol_request_wait_duration_seconds_sum{execute="true",flow_schema="service-accounts",priority_level="workload-low"} %[4]g
apiserver_flowcontrol_request_wait_duration_seconds_count{execute="true",flow_schema="service-accounts",priority_level="workload-low"} %[1]g
apiserver_flowcontrol_current_executing_seats{flow_schema="service-accounts",priority_level="workload-low"} 20
apiserver_flowcontrol_current_executing_seats{flow_schema="system-nodes",priority_level="system"} 1
apiserver_flowcontrol_nominal_limit_seats{priority_level="workload-low"} 24
apiserver_flowcontrol_nominal_limit_seats{priority_level="system"} 30
apiserver_flowcontrol_nominal_limit_seats{priority_level="exempt"} 0
apiserver_longrunning_requests{component="apiserver",group="",resource="pods",scope="cluster",subresource="",verb="WATCH",version="v1"} 12
apiserver_longrunning_requests{component="apiserver",group="",resource="pods",scope="namespace",subresource="",verb="WATCH",version="v1"} 3
apiserver_longrunning_requests{component="apiserver",group="",resource="pods",scope="namespace",subresource="exec",verb="CONNECT",version="v1"} 1
apiserver_longrunning_requests{component="apiserver",group="",resource="secrets",scope="cluster",subresource="",verb="WATCH",version="v1"} 4
apiserver_watch_events_total{group="",kind="Pod",version="v1"} %[2]g
apiserver_resource_objects{group="",resource="pods"} 214
apiserver_resource_objects{group="apps",resource="replicasets"} 1186
apiserver_resource_objects{group="",resource="unknownkind"} -1
etcd_request_duration_seconds_sum{operation="get",type="/registry/pods"} %[5]g
etcd_request_duration_seconds_count{operation="get",type="/registry/pods"} %[1]g
process_start_time_seconds %[6]g
some_label_escapes{path="a\"b,c\\d\n",x="}"} 1
`, 100*step, 10*step, step, 20*step, 0.5*step, started)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParsePromText(t *testing.T) {
	s, err := parsePromText(strings.NewReader(apiMetricsAt(1, 1000)), map[string]bool{"some_label_escapes": true, metricProcessStart: true})
	if err != nil {
		t.Fatal(err)
	}

	esc := s["some_label_escapes"]
	if len(esc) != 1 || esc[0].labels["path"] != "a\"b,c\\d\n" || esc[0].labels["x"] != "}" {
		t.Fatalf("escapes: %+v", esc)
	}

	if s.total(metricProcessStart, nil) != 1000 || s.has(metricRequests) {
		t.Fatalf("filter: %+v", s)
	}
}

func apiTestWindow(t *testing.T, sameServer bool) promWindow {
	t.Helper()

	before, err := parsePromText(strings.NewReader(apiMetricsAt(1, 1000)), apiMetricNames)
	if err != nil {
		t.Fatal(err)
	}

	afterStart := 1000.0
	if !sameServer {
		afterStart = 2000
	}

	after, err := parsePromText(strings.NewReader(apiMetricsAt(3, afterStart)), apiMetricNames)
	if err != nil {
		t.Fatal(err)
	}

	return newPromWindow(before, after, 2*time.Second, time.Unix(3000, 0))
}

func TestAPIPressureRates(t *testing.T) {
	r := fillAPIPressure(apiHealthReport{}, apiTestWindow(t, true))

	// Over 2 s the counters grow by 2 steps: LIST pods +200, PUT leases +20, /livez and
	// /readyz +20 each, 500 +2, PATCH +2; the evictions refused (429) do not move. The APF
	// 429s are terminations: +2.
	if r.WindowSeconds != 2 || r.UptimeSeconds != 2000 {
		t.Fatalf("window %v uptime %v", r.WindowSeconds, r.UptimeSeconds)
	}

	if !near(r.RequestRate, 132) || !near(r.ErrorRate, 1) || !near(r.ThrottledRate, 1) || !near(r.RejectedRate, 1) {
		t.Fatalf("rates %v %v %v %v", r.RequestRate, r.ErrorRate, r.ThrottledRate, r.RejectedRate)
	}

	if r.InflightRead != 9 || r.InflightMutate != 2 || r.Queued != 3 || r.Watches != 19 || !near(r.WatchEventRate, 10) {
		t.Fatalf("gauges %+v", r)
	}

	// etcd: +1 s over +200 calls; LIST pods latency: +40 s over +200.
	if !near(r.EtcdLatencyMs, 5) {
		t.Fatalf("etcd %v", r.EtcdLatencyMs)
	}

	top := r.Requests[0]
	if top.Verb != "LIST" || top.Resource != "pods" || !near(top.Rate, 100) || !near(top.LatencyMs, 200) {
		t.Fatalf("top request %+v", top)
	}

	// The non-resource paths are one row; ties go by resource.
	nonResource := r.Requests[1]
	if len(r.Requests) != 5 || nonResource.Verb != "GET" || nonResource.Resource != "" || !near(nonResource.Rate, 20) ||
		r.Requests[3].Resource != "configmaps" || r.Requests[4].Resource != "nodes/status" {
		t.Fatalf("requests %+v", r.Requests)
	}

	// The exempt probes did not grow: left out. The rejected flow comes first.
	if len(r.Clients) != 2 || r.Clients[0].Name != "service-accounts" || r.Clients[0].Queued != 3 ||
		!near(r.Clients[0].WaitMs, 200) || !near(r.Clients[1].Rate, 10) {
		t.Fatalf("clients %+v", r.Clients)
	}

	if r.Priorities[0].Name != "workload-low" || r.Priorities[0].Executing != 20 || r.Priorities[0].Limit != 24 ||
		r.Priorities[len(r.Priorities)-1].Name != "exempt" {
		t.Fatalf("priorities %+v", r.Priorities)
	}

	if r.WatchedKinds[0] != (apiCount{"pods", 15}) || len(r.Objects) != 2 || r.Objects[0] != (apiCount{"replicasets.apps", 1186}) {
		t.Fatalf("watches %+v objects %+v", r.WatchedKinds, r.Objects)
	}
}

func TestAPIPressureAcrossServersFallsBackToUptime(t *testing.T) {
	r := fillAPIPressure(apiHealthReport{}, apiTestWindow(t, false))

	// Another server answered the second scrape: totals over its 1000 s of uptime.
	if r.WindowSeconds != 0 || r.UptimeSeconds != 1000 || !near(r.RequestRate, (300+30+30+30+3+3+7)/1000.0) {
		t.Fatalf("window %v uptime %v rate %v", r.WindowSeconds, r.UptimeSeconds, r.RequestRate)
	}
}

func TestParseAPIChecks(t *testing.T) {
	checks := parseAPIChecks("[+]ping ok\n[+]log ok\n[-]etcd failed: reason withheld\n[+]poststarthook/x ok\nreadyz check failed\n")

	if len(checks) != 4 || !checks[0].OK || checks[2] != (apiCheck{Name: "etcd", Reason: "reason withheld"}) || checks[3].Name != "poststarthook/x" {
		t.Fatalf("%+v", checks)
	}
}

func TestParseAPIQueued(t *testing.T) {
	dump := `PriorityLevelName, FlowSchemaName,   QueueIndex, RequestIndexInQueue, FlowDistingsher,                ArriveTime,  InitialSeats, FinalSeats, AdditionalLatency, StartTime, UserName,                                   Verb, APIPath,      Namespace, Name, APIVersion, Resource, SubResource
exempt,            <none>,           <none>,     <none>,              <none>,                         <none>,      <none>,       <none>,     <none>,            <none>
system,            system-nodes,     -1,         -1,                  system:node:n1,                 2026-10-05,  1,            1,          0s,                2026-10-05, system:node:n1,                            get,  /api/v1/nodes/n1, ,     n1,   v1,         nodes,
workload-low,      service-accounts, 3,          -1,                  system:serviceaccount:a:c,      2026-10-05,  1,            1,          0s,                2026-10-05, system:serviceaccount:a:c,                 list, /api/v1/secrets, ,      ,     v1,         secrets,
workload-low,      service-accounts, 3,          0,                   system:serviceaccount:a:b,      2026-10-05,  1,            1,          0s,                2026-10-05, system:serviceaccount:a:b,                 list, /api/v1/pods, ,          ,     v1,         pods,
`
	queued := parseAPIQueued(dump)

	want := apiQueued{User: "system:serviceaccount:a:b", FlowSchema: "service-accounts", Priority: "workload-low", Verb: "list", Path: "/api/v1/pods"}
	if len(queued) != 1 || queued[0] != want {
		t.Fatalf("%+v", queued)
	}
}

func TestAPIVerdict(t *testing.T) {
	healthy := apiHealthReport{Ready: apiProbe{OK: true}, Live: apiProbe{OK: true}}

	cases := map[string]apiHealthReport{
		apiStatusOK:         healthy,
		apiStatusUnhealthy:  {Live: apiProbe{OK: true}, WindowSeconds: 5, RejectedRate: 1},
		apiStatusThrottling: {Ready: healthy.Ready, Live: healthy.Live, WindowSeconds: 5, ThrottledRate: 0.1, Queued: 2},
		apiStatusBusy:       {Ready: healthy.Ready, Live: healthy.Live, WindowSeconds: 5, RequestRate: 50, ErrorRate: 1},
	}

	for want, r := range cases {
		if got := apiVerdict(r); got != want {
			t.Errorf("%s: got %s for %+v", want, got, r)
		}
	}

	calm := map[string]apiHealthReport{
		// A broken aggregated API answering 503 now and then is no load.
		"rare 5xx": {Ready: healthy.Ready, Live: healthy.Live, WindowSeconds: 5, RequestRate: 200, ErrorRate: 0.2},
		// Averaged since the server started, an old burst of rejections must not weigh forever.
		"since start": {Ready: healthy.Ready, Live: healthy.Live, RejectedRate: 0.01, ThrottledRate: 0.01, RequestRate: 1, ErrorRate: 1},
		// A level running wide requests near its limit, nothing waiting.
		"full, no queue": {Ready: healthy.Ready, Live: healthy.Live, WindowSeconds: 5, Priorities: []apiPriority{{Name: "x", Executing: 9, Limit: 10}}},
	}

	for name, r := range calm {
		if got := apiVerdict(r); got != apiStatusOK {
			t.Errorf("%s: got %s", name, got)
		}
	}
}

// TestReadAPIHealth runs the whole read against an API server whose readyz fails and whose
// counters move between the two scrapes.
func TestReadAPIHealth(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	var scrapes atomic.Int32

	started := float64(time.Now().Unix() - 60)

	f.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.1"}`)
		case "/readyz":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "[+]ping ok\n[-]etcd failed: reason withheld\nreadyz check failed\n")
		case "/livez":
			_, _ = io.WriteString(w, "[+]ping ok\nlivez check passed\n")
		case "/metrics":
			_, _ = io.WriteString(w, apiMetricsAt(float64(1+2*scrapes.Add(1)), started))
		default:
			http.NotFound(w, r)
		}
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readAPIHealthOver(context.Background(), k, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if r.Status != apiStatusUnhealthy || r.Ready.OK || !r.Live.OK || len(r.Ready.Checks) != 2 || r.Version != "v1.34.1" {
		t.Fatalf("probes %+v", r)
	}

	if r.MetricsError != "" || r.WindowSeconds <= 0 || r.RequestRate <= 0 || r.QueuedRequests == nil {
		t.Fatalf("metrics %+v", r)
	}
}

func TestKubeAPIHealthDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeAPIHealth(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var r apiHealthReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	if r.Status != apiStatusBusy || len(r.Clients) == 0 || len(r.QueuedRequests) == 0 {
		t.Fatalf("demo %s", out)
	}
}
