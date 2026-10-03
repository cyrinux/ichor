package talosmobile

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Outputs of netperf 2.7 from the netPerfImage, for the commands netPerfCommand builds.
const (
	netPerfStreamOutput = `MIGRATED TCP STREAM TEST from 0.0.0.0 (0.0.0.0) port 0 AF_INET to 10.244.1.7 () port 12867 AF_INET
Minimum Latency Microseconds,Mean Latency Microseconds,Maximum Latency Microseconds,50th Percentile Latency Microseconds,90th Percentile Latency Microseconds,99th Percentile Latency Microseconds,Transaction Rate Tran/s,Throughput,Throughput Units
0,1.44,819,1,3,4,1.000,88361.62,10^6bits/s
`
	netPerfRROutput = `MIGRATED TCP REQUEST/RESPONSE TEST from 0.0.0.0 (0.0.0.0) port 0 AF_INET to 10.244.1.7 () port 12867 AF_INET : first burst 0
Minimum Latency Microseconds,Mean Latency Microseconds,Maximum Latency Microseconds,50th Percentile Latency Microseconds,90th Percentile Latency Microseconds,99th Percentile Latency Microseconds,Transaction Rate Tran/s,Throughput,Throughput Units
4,14.49,6655,13,17,33,68492.367,68492.37,Trans/s
`
	netPerfNoServerOutput = `establish control: are you sure there is a netserver listening on 192.0.2.10 at port 12866?
establish_control could not establish the control connection from 0.0.0.0 port 0 address family AF_UNSPEC to 192.0.2.10 port 12866 address family AF_INET
`
)

func TestParseNetPerf(t *testing.T) {
	r, err := parseNetPerf(netPerfThroughput, netPerfStreamOutput)
	if err != nil || r.ThroughputMbps != 88361.62 || r.Latency != nil {
		t.Fatalf("stream: %+v %v", r, err)
	}

	r, err = parseNetPerf(netPerfLatencyTest, netPerfRROutput)
	if err != nil || r.TransactionRate != 68492.367 || r.Latency == nil || *r.Latency != (netPerfLatency{4, 14.49, 6655, 13, 17, 33}) {
		t.Fatalf("rr: %+v %v", r, err)
	}

	if _, err := parseNetPerf(netPerfThroughput, strings.Replace(netPerfStreamOutput, "10^6bits/s", "10^3Bytes/s", 1)); err == nil {
		t.Fatal("other throughput unit accepted")
	}

	if _, err := parseNetPerf(netPerfLatencyTest, netPerfNoServerOutput); err == nil || !strings.Contains(err.Error(), "establish_control") {
		t.Fatalf("got %v", err)
	}
}

func TestNetPerfCommand(t *testing.T) {
	got := strings.Join(netPerfCommand("10.244.1.7", netPerfLatencyTest, 10), " ")
	want := "netperf -H 10.244.1.7 -p 12866 -l 10 -t TCP_RR -- -R 1 -P ,12867 -o " + netPerfFields

	if got != want {
		t.Fatalf("got %s", got)
	}

	if got := netPerfCommand("fd00::7", netPerfThroughput, 5); got[1] != "-6" || !slices.Contains(got, "TCP_STREAM") {
		t.Fatalf("got %v", got)
	}
}

func TestNetPerfFailure(t *testing.T) {
	var pod netPerfPod
	if msg := netPerfFailure(netPerfNoServerOutput, pod, "192.0.2.10"); !strings.Contains(msg, "firewall may block TCP ports 12866-12867") {
		t.Fatalf("got %s", msg)
	}

	pod.Status.Reason = "DeadlineExceeded"
	if msg := netPerfFailure("", pod, "192.0.2.10"); !strings.HasPrefix(msg, "timed out waiting for 192.0.2.10") {
		t.Fatalf("got %s", msg)
	}
}

// TestNetPerfPodSpecIsRestricted keeps the pods within the "restricted" pod security level.
func TestNetPerfPodSpecIsRestricted(t *testing.T) {
	js, err := json.Marshal(netPerfPodSpec("p", "node-1", false, time.Minute, "netperf", "-V"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`"runAsNonRoot":true`, `"allowPrivilegeEscalation":false`, `"drop":["ALL"]`, `"type":"RuntimeDefault"`,
		`"hostNetwork":false`, `"nodeName":"node-1"`, `"restartPolicy":"Never"`, `"activeDeadlineSeconds":60`,
	} {
		if !strings.Contains(string(js), want) {
			t.Errorf("missing %s in %s", want, js)
		}
	}
}

func TestMapNetPerfNode(t *testing.T) {
	var obj nodeObject
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"cp-1","labels":{"node-role.kubernetes.io/control-plane":""}},
		"status":{"addresses":[{"type":"Hostname","address":"cp-1"},{"type":"InternalIP","address":"192.0.2.10"}],
		"conditions":[{"type":"MemoryPressure","status":"False"},{"type":"Ready","status":"True"}]}}`), &obj); err != nil {
		t.Fatal(err)
	}

	if n := mapNetPerfNode(obj); n != (netPerfNode{Name: "cp-1", Address: "192.0.2.10", ControlPlane: true, Ready: true}) {
		t.Fatalf("got %+v", n)
	}
}

// fakeNetPerfAPI plays the API server of a two-node cluster: servers start, the pull pod
// and the pod network clients succeed, host network clients find no netserver.
type fakeNetPerfAPI struct {
	*fakeKubeAPI

	mu         sync.Mutex
	posted     map[string]string // pod or namespace name -> body
	deleted    []string
	refuseHost bool   // the host network server is refused (pod security)
	waitReason string // server containers stay waiting with this reason
}

func newFakeNetPerfAPI(t *testing.T) *fakeNetPerfAPI {
	t.Helper()

	f := &fakeNetPerfAPI{fakeKubeAPI: &fakeKubeAPI{}, posted: map[string]string{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	old := netPerfPoll
	netPerfPoll = time.Millisecond

	t.Cleanup(func() { netPerfPoll = old })

	return f
}

func (f *fakeNetPerfAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/namespaces/"), "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	write := func(code int, s string) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, s)
	}

	switch {
	case path == "/version":
		write(200, `{"gitVersion":"v1.34.0"}`)
	case path == "/api/v1/nodes":
		write(200, `{"items":[{"metadata":{"name":"node-a"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
			{"metadata":{"name":"node-b"},"status":{"conditions":[{"type":"Ready","status":"True"}]}},
			{"metadata":{"name":"node-down"},"status":{"conditions":[{"type":"Ready","status":"False"}]}}]}`)
	case path == "/api/v1/namespaces" && r.Method == http.MethodGet:
		write(200, `{"items":[{"metadata":{"name":"ichor-netperf-old00","creationTimestamp":"2020-01-01T00:00:00Z"}},
			{"metadata":{"name":"ichor-netperf-new00","creationTimestamp":"`+time.Now().UTC().Format(time.RFC3339)+`"}}]}`)
	case path == "/api/v1/namespaces" && r.Method == http.MethodPost:
		var ns struct {
			Metadata struct{ Name string } `json:"metadata"`
		}

		_ = json.Unmarshal(body, &ns)
		f.posted[ns.Metadata.Name] = string(body)
		write(201, string(body))
	case r.Method == http.MethodDelete && len(parts) == 1:
		f.deleted = append(f.deleted, parts[0])
		write(200, `{}`)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "pods":
		var pod struct {
			Metadata struct{ Name string } `json:"metadata"`
		}

		_ = json.Unmarshal(body, &pod)
		if f.refuseHost && pod.Metadata.Name == "server-host" {
			write(403, `{"kind":"Status","reason":"Forbidden","message":"violates PodSecurity \"baseline:latest\": host namespaces (hostNetwork=true)"}`)

			return
		}

		f.posted[pod.Metadata.Name] = string(body)
		write(201, string(body))
	case len(parts) == 4 && parts[3] == "log":
		switch name := parts[2]; {
		case strings.Contains(name, "-host-"):
			write(200, netPerfNoServerOutput)
		case strings.HasSuffix(name, netPerfThroughput):
			write(200, netPerfStreamOutput)
		default:
			write(200, netPerfRROutput)
		}
	case len(parts) == 3 && parts[1] == "pods":
		write(200, f.podStatus(parts[2]))
	default:
		write(404, `{"kind":"Status","reason":"NotFound","message":"`+path+` not found"}`)
	}
}

func (f *fakeNetPerfAPI) podStatus(name string) string {
	switch {
	case strings.HasPrefix(name, "server-") && f.waitReason != "":
		return `{"status":{"phase":"Pending","containerStatuses":[{"state":{"waiting":{"reason":"` + f.waitReason + `","message":"pull access denied"}}}]}}`
	case name == "server-pod":
		return `{"status":{"phase":"Running","podIP":"10.244.1.7"}}`
	case name == "server-host":
		return `{"status":{"phase":"Running","podIP":"192.0.2.10"}}`
	case strings.Contains(name, "-host-"):
		return `{"status":{"phase":"Failed","containerStatuses":[{"state":{"terminated":{"exitCode":255,"reason":"Error"}}}]}}`
	default:
		return `{"status":{"phase":"Succeeded"}}`
	}
}

func (f *fakeNetPerfAPI) client(t *testing.T) *kubeClient {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestRunNetPerf(t *testing.T) {
	f := newFakeNetPerfAPI(t)

	var phases []string

	report, err := runNetPerf(context.Background(), f.client(t), netPerfOptions{server: "node-a", client: "node-b", hostNetwork: true, seconds: 5},
		func(p netPerfProgress) {
			if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
				phases = append(phases, p.Phase)
			}
		})
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"preparing", "starting", "testing", "cleaning"}; !slices.Equal(phases, want) {
		t.Fatalf("phases %v", phases)
	}

	r := report.Results
	if len(r) != 4 || r[0].ThroughputMbps != 88361.62 || r[1].Latency == nil || r[1].Latency.P50 != 13 || r[0].Path != "pod" {
		t.Fatalf("pod results %+v", r)
	}

	if r[2].Path != "host" || !strings.Contains(r[2].Error, "no answer from netserver at 192.0.2.10") || r[3].Error == "" {
		t.Fatalf("host results %+v", r[2:])
	}

	var ns string

	for name, body := range f.posted {
		if strings.HasPrefix(name, "ichor-netperf-") {
			ns = name
			if !strings.Contains(body, `"pod-security.kubernetes.io/enforce":"privileged"`) {
				t.Errorf("host network namespace not privileged: %s", body)
			}
		}
	}

	if !slices.Contains(f.deleted, ns) || !slices.Contains(f.deleted, "ichor-netperf-old00") || slices.Contains(f.deleted, "ichor-netperf-new00") {
		t.Fatalf("deleted %v (test namespace %s)", f.deleted, ns)
	}

	if !strings.Contains(f.posted["pull"], `"nodeName":"node-b"`) || !strings.Contains(f.posted["server-host"], `"hostNetwork":true`) {
		t.Fatalf("pods %v", f.posted)
	}
}

func TestRunNetPerfPodNetworkOnly(t *testing.T) {
	f := newFakeNetPerfAPI(t)

	report, err := runNetPerf(context.Background(), f.client(t), netPerfOptions{server: "node-a", client: "node-a", seconds: 5}, func(netPerfProgress) {})
	if err != nil || len(report.Results) != 2 || report.Results[0].Error != "" {
		t.Fatalf("%+v %v", report, err)
	}

	for name, body := range f.posted {
		if strings.Contains(body, "privileged") || name == "pull" || name == "server-host" {
			t.Errorf("unexpected %s: %s", name, body)
		}
	}
}

func TestRunNetPerfHostServerRefused(t *testing.T) {
	f := newFakeNetPerfAPI(t)
	f.refuseHost = true

	report, err := runNetPerf(context.Background(), f.client(t), netPerfOptions{server: "node-a", client: "node-b", hostNetwork: true, seconds: 5}, func(netPerfProgress) {})
	if err != nil {
		t.Fatal(err)
	}

	if r := report.Results; r[0].Error != "" || !strings.Contains(r[2].Error, "PodSecurity") {
		t.Fatalf("%+v", r)
	}
}

func TestRunNetPerfImagePullFails(t *testing.T) {
	f := newFakeNetPerfAPI(t)
	f.waitReason = "ImagePullBackOff"

	report, err := runNetPerf(context.Background(), f.client(t), netPerfOptions{server: "node-a", client: "node-a", seconds: 5}, func(netPerfProgress) {})
	if err != nil {
		t.Fatal(err)
	}

	if r := report.Results; len(r) != 2 || !strings.Contains(r[0].Error, "cannot start on node-a: ImagePullBackOff pull access denied") {
		t.Fatalf("%+v", r)
	}

	if len(f.deleted) == 0 {
		t.Fatal("namespace not deleted")
	}
}

func TestRunNetPerfRefusesNodes(t *testing.T) {
	f := newFakeNetPerfAPI(t)

	for _, opts := range []netPerfOptions{
		{server: "node-a", client: "node-x", seconds: 5},
		{server: "node-down", client: "node-a", seconds: 5},
		{server: "", client: "node-a", seconds: 5},
		{server: "../etc", client: "node-a", seconds: 5},
	} {
		if _, err := runNetPerf(context.Background(), f.client(t), opts, func(netPerfProgress) {}); err == nil {
			t.Errorf("%+v accepted", opts)
		}
	}

	if len(f.posted) != 0 {
		t.Fatalf("created %v", f.posted)
	}
}

type netPerfRecorder struct {
	mu       sync.Mutex
	progress []string
	done     chan [2]string
}

func (r *netPerfRecorder) OnProgress(js string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.progress = append(r.progress, js)
}

func (r *netPerfRecorder) OnDone(report, errMessage string) { r.done <- [2]string{report, errMessage} }

func TestStartNetPerfDemo(t *testing.T) {
	old := netPerfDemoStep
	netPerfDemoStep = time.Millisecond

	t.Cleanup(func() { netPerfDemoStep = old })

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	nodes, err := NetPerfNodes(cfg, "", "")
	if err != nil || !strings.Contains(nodes, `"name":"demo-cp-1"`) {
		t.Fatalf("nodes %v %s", err, nodes)
	}

	rec := &netPerfRecorder{done: make(chan [2]string, 1)}
	StartNetPerf(cfg, "", "", "demo-cp-1", "demo-worker-1", true, 99, rec)

	done := <-rec.done

	var report netPerfReport
	if err := json.Unmarshal([]byte(done[0]), &report); err != nil || done[1] != "" {
		t.Fatalf("%v %q %s", err, done[1], done[0])
	}

	if report.Seconds != netPerfMaxSeconds || len(report.Results) != 4 || report.Results[3].Latency == nil || report.Finished == 0 {
		t.Fatalf("report %+v", report)
	}

	if len(rec.progress) != 7 {
		t.Fatalf("%d progress events", len(rec.progress))
	}
}

func TestStartNetPerfCancel(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := &netPerfRecorder{done: make(chan [2]string, 1)}
	StartNetPerf(cfg, "", "", "demo-cp-1", "demo-worker-1", false, 5, rec).Cancel()

	if done := <-rec.done; done[1] != errNetPerfStopped.Error() {
		t.Fatalf("got %q", done[1])
	}
}
