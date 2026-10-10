package ichorgo

import (
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"google.golang.org/grpc"
)

func readNetToolFixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile("testdata/nettools/" + name)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func TestParseDig(t *testing.T) {
	ok := parseDig(readNetToolFixture(t, "dig_ok.txt"))
	if ok.Status != "NOERROR" || ok.Server != "10.96.0.10" || ok.QueryMs != 1 {
		t.Errorf("dig ok = %+v", ok)
	}

	if want := (dnsRecord{Name: "kubernetes.default.svc.cluster.local.", Type: "A", TTL: 30, Value: "10.96.0.1"}); len(ok.Records) != 1 || ok.Records[0] != want {
		t.Errorf("records = %+v", ok.Records)
	}

	nx := parseDig(readNetToolFixture(t, "dig_nxdomain.txt"))
	if nx.Status != "NXDOMAIN" || len(nx.Records) != 0 || nx.QueryMs != 3 {
		t.Errorf("dig nxdomain = %+v", nx)
	}

	multi := parseDig(readNetToolFixture(t, "dig_multi.txt"))
	values := []string{}

	for _, r := range multi.Records {
		values = append(values, r.Type+" "+r.Value)
	}

	want := []string{"MX 10 mail.example.test.", `TXT "v=spf1 -all"`, "SRV 0 5 443 www.example.test."}
	if !slices.Equal(values, want) || multi.Server != "203.0.113.53" {
		t.Errorf("dig multi = %v (%s)", values, multi.Server)
	}
}

func TestParsePing(t *testing.T) {
	cases := []struct {
		file string
		want pingResult
	}{
		{"ping_iputils.txt", pingResult{Sent: 3, Received: 2, LossPct: 33.3333, MinMs: 0.412, AvgMs: 0.501, MaxMs: 0.611}},
		{"ping_busybox_lost.txt", pingResult{Sent: 3, Received: 0, LossPct: 100}},
	}

	for _, c := range cases {
		got, ok := parsePing(readNetToolFixture(t, c.file))
		if !ok || got != c.want {
			t.Errorf("%s = %+v, %v", c.file, got, ok)
		}
	}

	if _, ok := parsePing("ping: bad address 'nowhere.invalid'"); ok {
		t.Error("an error parsed as a summary")
	}
}

func TestParseNc(t *testing.T) {
	open := parseNc("Connection to 10.0.0.2 6443 port [tcp/*] succeeded!\n", 0)
	if !open.Open || open.Message != "Connection to 10.0.0.2 6443 port [tcp/*] succeeded!" {
		t.Errorf("open = %+v", open)
	}

	closed := parseNc("nc: connect to 10.0.0.2 port 6444 (tcp) failed: Connection refused\n", 1)
	if closed.Open || !strings.Contains(closed.Message, "refused") {
		t.Errorf("closed = %+v", closed)
	}
}

func TestParseMtrAndTracepath(t *testing.T) {
	want := []traceHop{{1, "10.0.0.1", 0, 0.3}, {2, "???", 100, 0}, {3, "203.0.113.20", 10, 4.8}}
	if got := parseMtr(readNetToolFixture(t, "mtr.txt")); !slices.Equal(got, want) {
		t.Errorf("mtr = %+v", got)
	}

	wantPath := []traceHop{{1, "10.0.0.1", 0, 0.312}, {2, "???", 100, 0}, {3, "203.0.113.20", 0, 4.802}}
	if got := parseTracepath(readNetToolFixture(t, "tracepath.txt")); !slices.Equal(got, wantPath) {
		t.Errorf("tracepath = %+v", got)
	}

	// tracepath's output is no mtr report.
	if got := parseMtr(readNetToolFixture(t, "tracepath.txt")); len(got) != 0 {
		t.Errorf("mtr parsed tracepath: %+v", got)
	}
}

func TestParseCurlTLS(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

	https, ok := parseCurlTLS(readNetToolFixture(t, "curl_https.txt"), true, now)
	if !ok || https.Status != 200 || !https.TLSOk || https.TotalMs != 84.2 {
		t.Errorf("https = %+v", https)
	}

	if https.Subject != "CN = www.example.test" || https.NotAfter != "2026-11-30T23:59:59Z" || https.NotBefore != "2026-09-01T00:00:00Z" || https.DaysLeft != 51 {
		t.Errorf("certificate = %+v", https)
	}

	refused, ok := parseCurlTLS(readNetToolFixture(t, "curl_refused.txt"), false, now)
	if !ok || refused.Status != 0 || refused.TLSOk || !strings.Contains(refused.Error, "Could not connect") {
		t.Errorf("refused = %+v", refused)
	}

	if _, ok := parseCurlTLS("sh: curl: not found", false, now); ok {
		t.Error("no curl line parsed")
	}
}

func TestNetToolCommandValidation(t *testing.T) {
	refused := []struct{ tool, target, options string }{
		{"dns", "", ""},
		{"dns", "example.test; rm -rf /", ""},
		{"dns", "example test", ""},
		{"dns", "$(reboot)", ""},
		{"dns", "-x", ""},
		{"dns", "example.test", `{"record":"ANY"}`},
		{"dns", "example.test", `{"server":"dns.example.test"}`},
		{"ping", "a..b", ""},
		{"ping", "203.0.113.5|true", ""},
		{"port", "203.0.113.5", ""},
		{"port", "203.0.113.5:99999", ""},
		{"trace", "`id`", ""},
		{"http", "ftp://example.test", ""},
		{"http", "https://user:pw@example.test", ""},
		{"http", "https://example.test/a&b", ""},
		{"nmap", "example.test", ""},
		{"dns", "example.test", "{"},
	}

	for _, c := range refused {
		if _, err := netToolCommandFor(c.tool, c.target, c.options); err == nil {
			t.Errorf("%s %q %s: accepted", c.tool, c.target, c.options)
		}
	}

	cases := []struct {
		tool, target, options string
		tail                  []string
	}{
		{"dns", "kubernetes.default.svc.cluster.local", `{"server":"10.96.0.10"}`, []string{"@10.96.0.10", "kubernetes.default.svc.cluster.local", "A"}},
		{"dns", "_https._tcp.example.test", `{"record":"srv"}`, []string{"+stats", "_https._tcp.example.test", "SRV"}},
		{"dns", "203.0.113.5", "", []string{"-x", "203.0.113.5"}},
		{"ping", "2001:db8::1", `{"count":50}`, []string{"ping", "-c", "10", "-W", "2", "2001:db8::1"}},
		{"port", "[2001:db8::1]:6443", "", []string{"nc", "-zvw2", "2001:db8::1", "6443"}},
		{"trace", "203.0.113.5", "", []string{"sh", "203.0.113.5"}},
		{"http", "https://[2001:db8::1]:8443/healthz?verbose", "", []string{"https://[2001:db8::1]:8443/healthz?verbose", "10", "[2001:db8::1]:8443", "2001:db8::1"}},
		{"http", "http://example.test", `{"timeoutSec":20}`, []string{"http://example.test", "20", "", ""}},
	}

	for _, c := range cases {
		cmd, err := netToolCommandFor(c.tool, c.target, c.options)
		if err != nil {
			t.Errorf("%s %q: %v", c.tool, c.target, err)

			continue
		}

		if cmd.argv[0] != "/bin/sh" || cmd.argv[1] != "-c" || !slices.Equal(cmd.argv[len(cmd.argv)-len(c.tail):], c.tail) {
			t.Errorf("%s %q argv = %q", c.tool, c.target, cmd.argv)
		}
	}

	if cmd, _ := netToolCommandFor("ping", "203.0.113.5", `{"timeoutSec":900}`); cmd.timeout != netToolMaxTimeout {
		t.Errorf("timeout = %s", cmd.timeout)
	}
}

type netToolRecorder struct {
	mu     sync.Mutex
	lines  []string
	result string
	err    string
	done   chan struct{}
}

func newNetToolRecorder() *netToolRecorder { return &netToolRecorder{done: make(chan struct{})} }

func (r *netToolRecorder) OnOutput(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines = append(r.lines, line)
}

func (r *netToolRecorder) OnDone(result, errMessage string) {
	r.mu.Lock()
	r.result, r.err = result, errMessage
	r.mu.Unlock()
	close(r.done)
}

func (r *netToolRecorder) wait(t *testing.T) {
	t.Helper()

	select {
	case <-r.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the run never finished")
	}
}

func TestStartNodeNetToolFake(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.91", "v1.11.0", machine.TypeWorker)
	f.pull = func(stream grpc.ServerStreamingServer[machineapi.ImageServicePullResponse]) error {
		return stream.Send(&machineapi.ImageServicePullResponse{Response: &machineapi.ImageServicePullResponse_Name{Name: "docker.io/nicolaka/netshoot:latest"}})
	}

	var spec *machineapi.DebugContainerRunRequestSpec

	f.debugRun = func(_ string, s *machineapi.DebugContainerRunRequestSpec) (string, int32) {
		spec = s

		return readNetToolFixture(t, "ping_iputils.txt"), 0
	}

	cfg := f.start(t, "192.0.2.91")

	rec := newNetToolRecorder()
	StartNodeNetTool(cfg, "fake", "192.0.2.91", "ping", "203.0.113.5", "", rec)
	rec.wait(t)

	if rec.err != "" {
		t.Fatalf("error: %s", rec.err)
	}

	result := decodeJSON[netToolResult](t, rec.result, nil)
	if !result.OK || result.Ping == nil || result.Ping.Received != 2 || !strings.Contains(result.Raw, "ping statistics") {
		t.Errorf("result = %+v", result)
	}

	if spec.GetTty() || spec.GetProfile() != machineapi.DebugContainerRunRequestSpec_PROFILE_PRIVILEGED || spec.GetImageName() != "docker.io/nicolaka/netshoot:latest" {
		t.Errorf("spec = %v", spec)
	}

	// Each complete line went out as it came, after the pull's notice.
	if len(rec.lines) < 3 || !strings.HasPrefix(rec.lines[0], "pulling image") || !slices.Contains(rec.lines, "--- 203.0.113.5 ping statistics ---") {
		t.Errorf("lines = %q", rec.lines)
	}
}

func TestStartNodeNetToolRefusesBeforeRunning(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.92", "v1.11.0", machine.TypeWorker)
	cfg := f.start(t, "192.0.2.92")

	rec := newNetToolRecorder()
	StartNodeNetTool(cfg, "fake", "192.0.2.92", "dns", "example.test;reboot", "", rec)
	rec.wait(t)

	if rec.err == "" || rec.result != "" {
		t.Errorf("result = %q, error = %q", rec.result, rec.err)
	}

	if calls := append(f.called("Pull"), f.called("ContainerRun")...); len(calls) != 0 {
		t.Errorf("ran anyway: %v", calls)
	}
}

func TestStartNodeNetToolDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range []string{netToolDNS, netToolPing, netToolPort, netToolTrace, netToolHTTP} {
		target := map[string]string{netToolPort: "10.5.0.2:6443", netToolHTTP: "https://demo.invalid"}[tool]
		if target == "" {
			target = "kubernetes.default.svc.cluster.local"
		}

		rec := newNetToolRecorder()
		StartNodeNetTool(cfg, "", "", tool, target, "", rec)
		rec.wait(t)

		result := decodeJSON[netToolResult](t, rec.result, nil)
		if rec.err != "" || !result.OK || result.Tool != tool {
			t.Errorf("%s: %+v (%s)", tool, result, rec.err)
		}
	}
}
