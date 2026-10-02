package talosmobile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func demoConfigForTest(t *testing.T) string {
	t.Helper()
	yaml, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	return yaml
}

func TestDemoImportMergeSwitchAndRemove(t *testing.T) {
	yaml := demoConfigForTest(t)
	again := demoConfigForTest(t)
	if yaml != again {
		t.Fatal("demo identity must be stable across imports")
	}
	if _, err := ParseConfig(yaml); err != nil {
		t.Fatalf("demo must pass normal import validation: %v", err)
	}
	real := testConfig(t, time.Now().Add(time.Hour))
	merged, err := MergeConfig(real, yaml, "")
	if err != nil {
		t.Fatal(err)
	}
	merged, err = MergeConfig(merged, again, `[{"index":0,"replace":true}]`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(merged)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contexts) != 3 {
		t.Fatalf("duplicate import lost real contexts or duplicated demo: %d contexts", len(cfg.Contexts))
	}
	if !isDemoContext(merged, "") || !isDemoContext(merged, "Demo cluster") || isDemoContext(merged, "lab") || isDemoContext(merged, "missing") {
		t.Fatal("mock selection must follow the active endpoint, not the whole YAML")
	}
	if _, err := openSession(merged, "Demo cluster"); !errors.Is(err, demoUnavailable) {
		t.Fatalf("demo must never open a network session: %v", err)
	}
	removed, err := RemoveContext(merged, "Demo cluster")
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := loadConfig(removed)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Contexts) != 2 || remaining.Contexts["lab"].CA != cfg.Contexts["lab"].CA {
		t.Fatal("removing demo changed real cluster credentials")
	}
}

func TestDemoPublicReadsAndActions(t *testing.T) {
	yaml := demoConfigForTest(t)
	out, err := ClusterOverview(yaml, "")
	if err != nil {
		t.Fatal(err)
	}
	var overview clusterOverview
	if err := json.Unmarshal([]byte(out), &overview); err != nil {
		t.Fatal(err)
	}
	if len(overview.Nodes) != 5 || overview.Context != "Demo cluster" {
		t.Fatalf("bad overview: %+v", overview)
	}
	for _, n := range overview.Nodes {
		if !n.Ready || !n.Reachable || n.CPUCount == 0 || n.MemTotal == 0 {
			t.Fatalf("incomplete sample node: %+v", n)
		}
		reads := []func() (string, error){
			func() (string, error) { return NodeServices(yaml, "", n.Node) },
			func() (string, error) { return NodeResources(yaml, "", n.Node) },
			func() (string, error) { return NodeStats(yaml, "", n.Node) },
			func() (string, error) { return NodeFeatures(yaml, "", n.Node) },
			func() (string, error) { return NodeTime(yaml, "", n.Node) },
			func() (string, error) { return NodeNetwork(yaml, "", n.Node) },
			func() (string, error) { return NodeConnections(yaml, "", n.Node) },
			func() (string, error) { return NodeProcesses(yaml, "", n.Node) },
			func() (string, error) { return NodeContainers(yaml, "", n.Node) },
			func() (string, error) { return NodeImages(yaml, "", n.Node) },
			func() (string, error) { return NodeHardware(yaml, "", n.Node) },
			func() (string, error) { return NodeMounts(yaml, "", n.Node) },
			func() (string, error) { return NodeVolumes(yaml, "", n.Node) },
			func() (string, error) { return NodeDiskUsage(yaml, "", n.Node, "/var/log", 1) },
			func() (string, error) { return NodeDiskHealth(yaml, "", n.Node) },
			func() (string, error) { return ResourceTypes(yaml, "", n.Node) },
			func() (string, error) { return ResourceList(yaml, "", n.Node, "network", "hostname") },
			func() (string, error) { return ResourceGet(yaml, "", n.Node, "network", "hostname", "hostname") },
			func() (string, error) { return KernelLogs(yaml, "", n.Node, 10) },
			func() (string, error) { return ServiceLogs(yaml, "", n.Node, "kubelet", 10) },
			func() (string, error) { return ContainerLogs(yaml, "", n.Node, "demo-web", 10) },
		}
		for i, read := range reads {
			out, err := read()
			if err != nil || !json.Valid([]byte(out)) {
				t.Fatalf("node %s read %d: %q %v", n.Node, i, out, err)
			}
		}
		mc, err := NodeMachineConfig(yaml, "", n.Node, false)
		if err != nil || !strings.Contains(mc, n.Hostname) {
			t.Fatalf("machine config: %s %v", mc, err)
		}
		if err := Reboot(yaml, "", n.Node, "default"); !errors.Is(err, demoUnavailable) {
			t.Fatalf("reboot must be blocked locally: %v", err)
		}
		if err := ServiceAction(yaml, "", n.Node, "kubelet", "restart"); !errors.Is(err, demoUnavailable) {
			t.Fatalf("service control must be blocked locally: %v", err)
		}
	}
	for _, read := range []func(string, string) (string, error){ClusterStats, ClusterTime, EtcdStatus, KubeSpanStatus} {
		out, err := read(yaml, "")
		if err != nil || !json.Valid([]byte(out)) {
			t.Fatalf("cluster read: %s %v", out, err)
		}
	}
	logs, err := ServiceLogs(yaml, "", overview.Nodes[0].Node, "kubelet", 3)
	if err != nil {
		t.Fatal(err)
	}
	var tail logTail
	if err := json.Unmarshal([]byte(logs), &tail); err != nil {
		t.Fatal(err)
	}
	if len(tail.Lines) != 3 || len(tail.Entries) != 3 || !tail.Truncated || !strings.Contains(tail.Lines[0], "kubelet") {
		t.Fatalf("tail/source not respected: %+v", tail)
	}
	if _, err := NodeStats(yaml, "", "203.0.113.1"); err == nil {
		t.Fatal("unknown demo node must be rejected")
	}
}

func TestDemoCountersAdvance(t *testing.T) {
	n := demoNodes()[0]
	a := demoStats(n)
	time.Sleep(10 * time.Millisecond)
	b := demoStats(n)
	if b.At <= a.At || b.CPUBusy <= a.CPUBusy || b.CPUTotal <= a.CPUTotal || b.NetRx <= a.NetRx {
		t.Fatal("live charts require advancing timestamps and cumulative counters")
	}
	usage := (b.CPUBusy - a.CPUBusy) / (b.CPUTotal - a.CPUTotal)
	if usage < 0.17 || usage > 0.31 {
		t.Fatalf("unrealistic demo CPU usage: %f", usage)
	}
}

func TestDemoScreenshotMode(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })
	yaml := demoConfigForTest(t)
	out, err := ParseConfig(yaml)
	if err != nil {
		t.Fatal(err)
	}
	var summary configSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if !summary.Contexts[0].Demo {
		t.Fatal("demo label must survive endpoint masking")
	}
	out, err = ClusterOverview(yaml, summary.Current)
	if err != nil {
		t.Fatal(err)
	}
	var overview clusterOverview
	if err := json.Unmarshal([]byte(out), &overview); err != nil {
		t.Fatal(err)
	}
	if _, err := NodeServices(yaml, summary.Current, overview.Nodes[0].Node); err != nil {
		t.Fatalf("masked node must still resolve into the demo: %v", err)
	}
}

type demoLogListener struct {
	lines chan string
	done  chan string
}

func (l demoLogListener) OnLine(line string) { l.lines <- line }
func (l demoLogListener) OnDone(err string)  { l.done <- err }

type demoEventListener struct {
	events chan string
	done   chan string
}

func (l demoEventListener) OnEvent(event string) { l.events <- event }
func (l demoEventListener) OnDone(err string)    { l.done <- err }

func TestDemoStreamsCancel(t *testing.T) {
	yaml := demoConfigForTest(t)
	logs := demoLogListener{make(chan string, 10), make(chan string, 2)}
	run := StartLogFollow(yaml, "", "192.0.2.10", "kubelet", 10, logs)
	select {
	case <-logs.lines:
	case <-time.After(time.Second):
		t.Fatal("demo log stream did not start")
	}
	run.Cancel()
	select {
	case err := <-logs.done:
		if err != "" {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("demo log stream did not cancel")
	}
	events := demoEventListener{make(chan string, 10), make(chan string, 2)}
	eventRun := StartEvents(yaml, "", "192.0.2.20", 5, events)
	select {
	case out := <-events.events:
		var event nodeEvent
		if err := json.Unmarshal([]byte(out), &event); err != nil {
			t.Fatal(err)
		}
		if event.Node != "192.0.2.20" {
			t.Fatal("node filter not respected")
		}
	case <-time.After(time.Second):
		t.Fatal("demo event stream did not start")
	}
	eventRun.Cancel()
	select {
	case err := <-events.done:
		if err != "" {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("demo event stream did not cancel")
	}
}
