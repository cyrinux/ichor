package talosmobile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var logNow = time.Date(2026, 10, 1, 17, 0, 0, 0, time.UTC)

func ms(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}

	return t.UnixMilli()
}

func kv(pairs ...string) []logField {
	fields := []logField{}
	for i := 0; i+1 < len(pairs); i += 2 {
		fields = append(fields, logField{K: pairs[i], V: pairs[i+1]})
	}

	return fields
}

func TestParseLogLine(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		want       logEntry
	}{
		{
			name: "kernel",
			line: "kern:    info: [2026-10-01T16:47:32.186463035Z]: eth0: link up",
			want: logEntry{TS: ms("2026-10-01T16:47:32.186Z"), Level: "info", Source: "kern", Msg: "eth0: link up", Fields: kv()},
		},
		{
			name: "kernel err",
			line: "kern:     err: [2026-10-01T16:47:32Z]: ata1: failed command",
			want: logEntry{TS: ms("2026-10-01T16:47:32Z"), Level: "error", Source: "kern", Msg: "ata1: failed command", Fields: kv()},
		},
		{
			name: "kernel talos with nested json",
			line: `user: warning: [2026-10-01T16:47:32.186Z]: [talos] DHCP request/renew failed {"component": "controller-runtime", "controller": "network.OperatorSpecController", "link": {"name": "eth0", "up": true}, "error": "no offer from 10.0.0.1", "retries": 3}`,
			want: logEntry{
				TS: ms("2026-10-01T16:47:32.186Z"), Level: "warn", Source: "talos", Msg: "DHCP request/renew failed",
				Fields: kv("component", "controller-runtime", "controller", "network.OperatorSpecController",
					"link.name", "eth0", "link.up", "true", "error", "no offer from 10.0.0.1", "retries", "3"),
			},
		},
		{
			name: "kernel talos without json",
			line: "daemon:  notice: [2026-10-01T16:47:32Z]: [talos] service[apid](Running): Health check successful",
			want: logEntry{TS: ms("2026-10-01T16:47:32Z"), Level: "info", Source: "talos", Msg: "service[apid](Running): Health check successful", Fields: kv()},
		},
		{
			name: "kernel talos broken json stays in msg",
			line: `user: warning: [2026-10-01T16:47:32Z]: [talos] odd {"a": }`,
			want: logEntry{TS: ms("2026-10-01T16:47:32Z"), Level: "info", Source: "talos", Msg: `odd {"a": }`, Fields: kv()},
		},
		{
			name: "kernel talos warning without error field is info",
			line: `user: warning: [2026-10-01T16:47:32Z]: [talos] rendered new static pod {"component": "controller-runtime", "id": "kube-apiserver"}`,
			want: logEntry{TS: ms("2026-10-01T16:47:32Z"), Level: "info", Source: "talos", Msg: "rendered new static pod", Fields: kv("component", "controller-runtime", "id", "kube-apiserver")},
		},
		{
			name: "klog json kubelet error",
			line: `{"ts":1790873866688.1968,"caller":"prober/prober.go:272","msg":"Unable to write all bytes","expectedBytes":15187,"pod":{"name":"dns-abc12","namespace":"infra"},"containerName":"dns","err":"short write"}`,
			want: logEntry{
				TS: 1790873866688, Level: "error", Source: "prober.go:272", Msg: "Unable to write all bytes",
				Fields: kv("expectedBytes", "15187", "pod.name", "dns-abc12", "pod.namespace", "infra", "containerName", "dns", "err", "short write"),
			},
		},
		{
			name: "klog json verbosity",
			line: `{"ts":1790873866688.5,"caller":"kubelet/kubelet.go:2400","msg":"SyncLoop","v":2,"pods":["a","b"]}`,
			want: logEntry{TS: 1790873866688, Level: "debug", Source: "kubelet.go:2400", Msg: "SyncLoop", Fields: kv("pods", `["a","b"]`)},
		},
		{
			name: "klog json v0",
			line: `{"ts":1790873866688,"caller":"x/y.go:1","msg":"hello","v":0}`,
			want: logEntry{TS: 1790873866688, Level: "info", Source: "y.go:1", Msg: "hello", Fields: kv()},
		},
		{
			name: "klog json Errorf has no v nor err",
			line: `{"ts":1790873866688,"caller":"projected/projected.go:301","msg":"Couldn't get configMap infra/kube-root-ca.crt"}`,
			want: logEntry{TS: 1790873866688, Level: "error", Source: "projected.go:301", Msg: "Couldn't get configMap infra/kube-root-ca.crt", Fields: kv()},
		},
		{
			name: "etcd zap",
			line: `{"level":"warn","ts":"2026-10-01T16:59:51.146778Z","caller":"rafthttp/snapshot_sender.go:82","msg":"slow \"send\"","remote-peer-id":"3fc6b33f78ad8aec","bytes":52073053,"ok":false,"x":null}`,
			want: logEntry{
				TS: ms("2026-10-01T16:59:51.146Z"), Level: "warn", Source: "snapshot_sender.go:82", Msg: `slow "send"`,
				Fields: kv("remote-peer-id", "3fc6b33f78ad8aec", "bytes", "52073053", "ok", "false", "x", "null"),
			},
		},
		{
			name: "zap float seconds and logger",
			line: `{"level":"ERROR","ts":1790873866.25,"logger":"raft","msg":"lost leader"}`,
			want: logEntry{TS: 1790873866250, Level: "error", Source: "raft", Msg: "lost leader", Fields: kv()},
		},
		{
			name: "containerd json logrus",
			line: `{"address":"unix:///run/containerd/s/5fa9","level":"info","msg":"connecting to shim trustd","namespace":"system","time":"2026-09-29T17:37:01.368701461Z","version":3}`,
			want: logEntry{
				TS: ms("2026-09-29T17:37:01.368Z"), Level: "info", Msg: "connecting to shim trustd",
				Fields: kv("address", "unix:///run/containerd/s/5fa9", "namespace", "system", "version", "3"),
			},
		},
		{
			name: "json without msg keeps line as msg",
			line: `{"a":1}`,
			want: logEntry{Msg: `{"a":1}`, Fields: kv("a", "1")},
		},
		{
			name: "json unicode escapes",
			line: `{"msg":"café \ttab","level":"info"}`,
			want: logEntry{Level: "info", Msg: "café \ttab", Fields: kv()},
		},
		{
			name: "malformed json",
			line: `{"level":"info","msg":"cut`,
			want: logEntry{Msg: `{"level":"info","msg":"cut`, Fields: kv()},
		},
		{
			name: "json then garbage",
			line: `{"level":"info"} trailing`,
			want: logEntry{Msg: `{"level":"info"} trailing`, Fields: kv()},
		},
		{
			name: "logfmt",
			line: `time="2026-10-01T16:00:00.5Z" level=warning msg="failed to \"pull\" image" image="registry.example.com/app:1" retry=2`,
			want: logEntry{
				TS: ms("2026-10-01T16:00:00.5Z"), Level: "warn", Msg: `failed to "pull" image`,
				Fields: kv("image", "registry.example.com/app:1", "retry", "2"),
			},
		},
		{
			name: "logfmt empty and spaced values",
			line: `level=debug msg="a  b" empty="" path=/var/lib`,
			want: logEntry{Level: "debug", Msg: "a  b", Fields: kv("empty", "", "path", "/var/lib")},
		},
		{
			name: "prose with equals is not logfmt",
			line: "retrying with timeout=5s now",
			want: logEntry{Msg: "retrying with timeout=5s now", Fields: kv()},
		},
		{
			name: "klog text",
			line: "W1001 16:00:00.123456    1234 reflector.go:561] watch of *v1.Pod ended",
			want: logEntry{TS: ms("2026-10-01T16:00:00.123Z"), Level: "warn", Source: "reflector.go:561", Msg: "watch of *v1.Pod ended", Fields: kv()},
		},
		{
			name: "klog text from last year",
			line: "E1231 23:59:59.000001 7 x.go:1] boom",
			want: logEntry{TS: ms("2025-12-31T23:59:59Z"), Level: "error", Source: "x.go:1", Msg: "boom", Fields: kv()},
		},
		{
			name: "machined grpc",
			line: "2026/10/01 17:00:19.523686 machined OK [/cosi.resource.State/Get] 154.451µs unary Success {authorized} (:authority=localhost)",
			want: logEntry{TS: ms("2026-10-01T17:00:19.523Z"), Level: "info", Source: "machined", Msg: "OK [/cosi.resource.State/Get] 154.451µs unary Success {authorized} (:authority=localhost)", Fields: kv()},
		},
		{
			name: "apid grpc error",
			line: "2026/10/01 17:00:20.546096 log.go:118: Unavailable [/machine.MachineService/Version] 5s unary connection refused (peer=10.0.0.10:43166)",
			want: logEntry{TS: ms("2026-10-01T17:00:20.546Z"), Level: "warn", Source: "log.go:118", Msg: "Unavailable [/machine.MachineService/Version] 5s unary connection refused (peer=10.0.0.10:43166)", Fields: kv()},
		},
		{
			name: "trustd plain",
			line: "2026/10/01 16:37:32.234076 reg.go:68: received CSR signing request from [fd00::b]:61302",
			want: logEntry{TS: ms("2026-10-01T16:37:32.234Z"), Source: "reg.go:68", Msg: "received CSR signing request from [fd00::b]:61302", Fields: kv()},
		},
		{
			name: "machined talos message",
			line: `2026/10/01 17:05:32.105870 [talos] DHCP request/renew failed {"component": "controller-runtime", "operator": "dhcp4"}`,
			want: logEntry{
				TS: ms("2026-10-01T17:05:32.105Z"), Source: "talos", Msg: "DHCP request/renew failed",
				Fields: kv("component", "controller-runtime", "operator", "dhcp4"),
			},
		},
		{
			name: "go log without fraction",
			line: "2026/10/01 16:37:32 error while starting: failed",
			want: logEntry{TS: ms("2026-10-01T16:37:32Z"), Msg: "error while starting: failed", Fields: kv()},
		},
		{
			name: "udevd",
			line: "/usr/lib/udev/rules.d/50-udev-default.rules:101 The line has no effect, ignoring.",
			want: logEntry{Msg: "/usr/lib/udev/rules.d/50-udev-default.rules:101 The line has no effect, ignoring.", Fields: kv()},
		},
		{name: "empty", line: "", want: logEntry{Fields: kv()}},
		{name: "lone brace", line: "{", want: logEntry{Msg: "{", Fields: kv()}},
		{name: "not a date", line: "2026 was a good year", want: logEntry{Msg: "2026 was a good year", Fields: kv()}},
		{name: "not klog", line: "Error: something", want: logEntry{Msg: "Error: something", Fields: kv()}},
		{name: "not kernel", line: "kern: bogus: [x]: y", want: logEntry{Msg: "kern: bogus: [x]: y", Fields: kv()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogLine(tc.line, logNow)

			want := tc.want
			want.Raw = tc.line

			if !equalJSON(t, got, want) {
				t.Errorf("parseLogLine(%q) mismatch", tc.line)
			}
		})
	}
}

func TestParseLogLineKeepsRawAndTrimsCR(t *testing.T) {
	got := parseLogLine("level=info msg=hi\r", logNow)
	if got.Raw != "level=info msg=hi\r" || got.Msg != "hi" || got.Level != "info" {
		t.Errorf("got %+v", got)
	}
}

func TestParseLogLineExportedJSON(t *testing.T) {
	SetPrivacyMask(false, "")

	var e map[string]any
	if err := json.Unmarshal([]byte(ParseLogLine("plain text")), &e); err != nil {
		t.Fatal(err)
	}

	for _, k := range []string{"ts", "level", "source", "msg", "fields", "raw"} {
		if _, ok := e[k]; !ok {
			t.Errorf("missing %q in %v", k, e)
		}
	}

	if fields, ok := e["fields"].([]any); !ok || len(fields) != 0 {
		t.Errorf("fields must be an empty array, got %v", e["fields"])
	}
}

// A followed line arrives masked; parsing it must keep the same fakes, not mint new ones.
func TestParseLogLineMaskIsStable(t *testing.T) {
	enableMask(t, "")

	privacy.learnHosts([]hostEntry{{address: "192.0.2.11", hostname: "talos-cp-a", role: "controlplane"}})

	masked := privacy.maskPlain(`{"level":"info","msg":"talos-cp-a at 192.0.2.11 sees 198.51.100.7","peer":"198.51.100.7"}`)
	if strings.Contains(masked, "192.0.2.") || strings.Contains(masked, "talos-cp-a") {
		t.Fatalf("not masked: %s", masked)
	}

	var e logEntry
	if err := json.Unmarshal([]byte(ParseLogLine(masked)), &e); err != nil {
		t.Fatal(err)
	}

	if e.Raw != masked {
		t.Errorf("raw changed:\n got  %s\n want %s", e.Raw, masked)
	}

	if e.Msg != "cp-1 at 10.0.0.1 sees 10.0.0.2" || !equalJSON(t, e.Fields, kv("peer", "10.0.0.2")) {
		t.Errorf("got %+v", e)
	}
}

func TestKernelLogsTailHasEntries(t *testing.T) {
	tl := newTailLines(10)
	tl.write([]byte("kern: info: [2026-10-01T16:00:00Z]: a\nnot parsed\n"))

	got := tl.result()
	if len(got.Entries) != len(got.Lines) || got.Entries[0].Level != "info" || got.Entries[1].Msg != "not parsed" {
		t.Errorf("got %+v", got)
	}
}

func TestParseLogLineNeverPanicsOnPrefixes(t *testing.T) {
	lines := []string{
		`user: warning: [2026-10-01T16:47:32.186Z]: [talos] x {"a": {"b": [1, {"c": "]"}]}, "d": "e"}`,
		"I1001 16:00:00.123456    1234 file.go:12] message",
		"2026/10/01 17:00:19.523686 machined OK [/cosi.resource.State/Get] 1µs",
		`time="2026-10-01T16:00:00Z" level=info msg="x \\ y" k=v`,
	}

	for _, l := range lines {
		for i := range len(l) + 1 {
			e := parseLogLine(l[:i], logNow)
			if e.Raw != l[:i] || e.Fields == nil {
				t.Fatalf("bad entry for %q: %+v", l[:i], e)
			}
		}
	}
}

func BenchmarkParseLogLine(b *testing.B) {
	lines := []string{
		`user: warning: [2026-10-01T16:47:32.186463035Z]: [talos] DHCP request/renew failed {"component": "controller-runtime", "controller": "network.OperatorSpecController", "operator": "dhcp4", "error": "unable to receive an offer"}`,
		`{"ts":1790873866688.1968,"caller":"prober/prober.go:272","msg":"Unable to write all bytes from execInContainer","expectedBytes":15187,"actualBytes":10240,"pod":{"name":"dns-abc12","namespace":"infra"},"containerName":"dns","err":"short write"}`,
		`{"level":"info","ts":"2026-10-01T16:59:51.146778Z","caller":"rafthttp/snapshot_sender.go:82","msg":"sending database snapshot","snapshot-index":359215635,"remote-peer-id":"3fc6b33f78ad8aec","bytes":52073053,"size":"52 MB"}`,
		"2026/10/01 17:00:19.523686 machined OK [/cosi.resource.State/Get] 154.451µs unary Success {authorized based on PID (366) match with service \"apid\"} (:authority=localhost;content-type=application/grpc)",
		`time="2026-10-01T16:00:00.5Z" level=warning msg="failed to pull image" image="registry.example.com/app:1" retry=2`,
		"I1001 16:00:00.123456    1234 reflector.go:561] watch of *v1.Pod ended",
		"/usr/lib/udev/rules.d/50-udev-default.rules:101 The line has no effect, ignoring.",
	}

	b.ReportAllocs()

	for i := 0; b.Loop(); i++ {
		parseLogLine(lines[i%len(lines)], logNow)
	}
}
