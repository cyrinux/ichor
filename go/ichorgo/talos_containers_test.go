package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// ContainerStatus specs as a Talos 1.15 node writes them (invented containers).
const (
	ctrStatusRunningYAML = `state: running
health: healthy
image: registry.example.org/edge/proxy@sha256:0123
pid: 4242
restartCount: 0
generation: 2
`
	ctrStatusBackoffYAML = `state: backoff
health: degraded
image: registry.example.org/edge/agent:1.4
exitCode: 1
restartCount: 7
error: task exited with code 1
generation: 8
errorLastLog: |
  starting agent
  fatal: cannot open /var/lib/agent/state.db
`
	// Enums written as numbers, a gate still open, a waiting list with a non-string item.
	ctrStatusPendingNumericYAML = `state: 0
health: 0
image: registry.example.org/edge/cache:2
restartCount: 0
waitingFor:
  - image
  - 3
  - mount data
`
)

func ctrSpec(t *testing.T, id, yamlSpec string) map[string]any {
	t.Helper()

	spec, err := resourceSpecMap(untypedResource(t, containerStatusType, id, yamlSpec))
	if err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestBuildTalosContainers(t *testing.T) {
	statuses := map[string]map[string]any{
		"edge-proxy": ctrSpec(t, "edge-proxy", ctrStatusRunningYAML),
		"agent":      ctrSpec(t, "agent", ctrStatusBackoffYAML),
		"cache":      ctrSpec(t, "cache", ctrStatusPendingNumericYAML),
	}
	runtime := []*machineapi.ContainerInfo{
		{Id: "edge-proxy-2", PodId: "edge-proxy-2", Image: "registry.example.org/edge/proxy:3", Status: "RUNNING", Pid: 4242},
		// An older generation still being torn down: not the one the status names.
		{Id: "edge-proxy-1", PodId: "edge-proxy-1", Status: "STOPPED", Pid: 99},
		// A container with no status (being removed): not listed.
		{Id: "gone-4", PodId: "gone-4", Status: "STOPPED"},
	}
	stats := []*machineapi.Stat{{Id: "edge-proxy-2", MemoryUsage: 32 << 20, CpuUsage: 5e9}}

	got := buildTalosContainers(statuses, runtime, stats)

	want := []containerInfo{
		{
			ID: "agent", Namespace: containerNSTalos, Name: "agent", Image: "registry.example.org/edge/agent:1.4",
			Status: "backoff", Health: "degraded", RestartCount: 7, ExitCode: 1, Error: "task exited with code 1",
			WaitingFor: []string{}, LastLog: "starting agent\nfatal: cannot open /var/lib/agent/state.db",
		},
		{
			ID: "cache", Namespace: containerNSTalos, Name: "cache", Image: "registry.example.org/edge/cache:2",
			Status: "pending", Health: "pending", WaitingFor: []string{"image", "mount data"},
		},
		{
			ID: "edge-proxy", Namespace: containerNSTalos, Name: "edge-proxy", Image: "registry.example.org/edge/proxy@sha256:0123",
			Status: "running", Health: "healthy", Pid: 4242, Memory: 32 << 20, CPUNanos: 5e9, WaitingFor: []string{},
		},
	}

	if !equalJSON(t, got, want) {
		t.Error("Talos containers differ")
	}
}

func TestTalosContainerMissingFields(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		info *machineapi.ContainerInfo
		want containerInfo
	}{
		{
			// Wrong types and out-of-range enums never fail the mapping.
			name: "odd fields",
			spec: map[string]any{"state": 42, "health": "unheard-of", "restartCount": "many", "waitingFor": "image", "pid": -1},
			want: containerInfo{ID: "odd", Namespace: containerNSTalos, Name: "odd", Health: "unheard-of", WaitingFor: []string{}},
		},
		{
			// No status fields at all: the runtime fills what it knows.
			name: "runtime only",
			spec: map[string]any{},
			info: &machineapi.ContainerInfo{Id: "odd-0", Image: "registry.example.org/odd:1", Status: "RUNNING", Pid: 7},
			want: containerInfo{
				ID: "odd", Namespace: containerNSTalos, Name: "odd", Image: "registry.example.org/odd:1", Pid: 7, WaitingFor: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapTalosContainer("odd", tt.spec, tt.info, nil); !equalJSON(t, got, tt.want) {
				t.Error("container differs")
			}
		})
	}
}

func TestTalosContainerLastLog(t *testing.T) {
	long := strings.Repeat("x", talosLastLogMax+100)

	tests := []struct{ in, want string }{
		{"", ""},
		{"  one line \n", "one line"},
		{"a\r\nb\n\n", "a\nb"},
		{long, strings.Repeat("x", talosLastLogMax-len("…")) + "…"},
	}

	for _, tt := range tests {
		if got := cleanLastLog(tt.in); got != tt.want {
			t.Errorf("cleanLastLog(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTalosInstanceID(t *testing.T) {
	tests := []struct {
		name   string
		spec   map[string]any
		infos  []*machineapi.ContainerInfo
		wantID string
	}{
		{"from the generation", map[string]any{"generation": 3}, nil, "web-3"},
		{"no generation, the newest runtime one", map[string]any{}, []*machineapi.ContainerInfo{{Id: "web-2"}, {Id: "web-10"}, {Id: "web-x"}}, "web-10"},
		{"no generation, no runtime", map[string]any{}, nil, ""},
		{"another container's instance is not taken", map[string]any{}, []*machineapi.ContainerInfo{{Id: "web-cache-1"}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := talosInstanceID("web", tt.spec, tt.infos); got != tt.wantID {
				t.Errorf("instance = %q, want %q", got, tt.wantID)
			}
		})
	}
}

// talosContainerCluster is a Talos 1.15 node running two Talos containers next to a 1.14 one.
func talosContainerCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.71", "v1.15.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.72", "v1.14.1", machine.TypeWorker)
	f.put("192.0.2.71",
		resourceDefinition(t, meta.ResourceDefinitionSpec{Type: containerStatusType, DefaultNamespace: talosContainersNamespace}),
		untypedResource(t, containerStatusType, "edge-proxy", ctrStatusRunningYAML),
		untypedResource(t, containerStatusType, "agent", ctrStatusBackoffYAML),
	)
	f.talosContainers = []*machineapi.ContainerInfo{
		{Id: "edge-proxy-2", PodId: "edge-proxy-2", Image: "registry.example.org/edge/proxy:3", Status: "RUNNING", Pid: 4242},
	}

	return f, f.start(t, "192.0.2.71", "192.0.2.72")
}

func TestNodeContainersListsTalosContainers(t *testing.T) {
	f, cfg := talosContainerCluster(t)

	tests := []struct {
		node string
		want []string
	}{
		{"192.0.2.71", []string{
			"system apid -",
			"system trustd -",
			"taloscontainers agent degraded",
			"taloscontainers edge-proxy healthy",
			"k8s.io web -",
			"k8s.io kube-apiserver -",
		}},
		// A 1.14 node knows no ContainerStatus: the two usual groups only.
		{"192.0.2.72", []string{"system apid -", "system trustd -", "k8s.io web -", "k8s.io kube-apiserver -"}},
	}

	for _, tt := range tests {
		t.Run(tt.node, func(t *testing.T) {
			out, err := NodeContainers(cfg, "fake", tt.node)
			if err != nil {
				t.Fatal(err)
			}

			var list containerList
			if err := json.Unmarshal([]byte(out), &list); err != nil {
				t.Fatal(err)
			}

			var got []string

			for _, c := range list.Containers {
				health := c.Health
				if health == "" {
					health = "-"
				}

				got = append(got, c.Namespace+" "+c.Name+" "+health)

				if c.WaitingFor == nil {
					t.Errorf("%s: waitingFor is null", c.Name)
				}
			}

			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("containers =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}

	// The 1.14 node is never asked for the taloscontainers namespace.
	for _, ns := range f.containerNamespaces("192.0.2.72") {
		if ns == containerNSTalos {
			t.Error("a 1.14 node was asked for Talos containers")
		}
	}
}

func TestContainerRestartTalosContainer(t *testing.T) {
	tests := []struct {
		id, wantID string
	}{
		// The ContainerConfig name: restarted as its current instance.
		{"agent", "agent-8"},
		// An id without a status is sent as given (an instance id from the probe).
		{"agent-7", "agent-7"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			withDataDir(t)

			f, cfg := talosContainerCluster(t)

			if err := ContainerRestart(cfg, "fake", "192.0.2.71", containerNSTalos, tt.id); err != nil {
				t.Fatal(err)
			}

			if len(f.restarts) != 1 {
				t.Fatalf("restarts = %v", f.restarts)
			}

			r := f.restarts[0]
			if r.GetNamespace() != containerNSTalos || r.GetId() != tt.wantID || r.GetDriver() != common.ContainerDriver_CONTAINERD {
				t.Errorf("request = %v", r)
			}

			entries := readAudit(t, "fake", "container-restart")
			if len(entries) != 1 || entries[0].Object != containerNSTalos+"/"+tt.id || entries[0].Outcome != auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestContainerLogsNamespaces(t *testing.T) {
	const node = "192.0.2.62"

	f := newFakeTalos()
	f.addNode(t, node, "v1.15.0", machine.TypeWorker)

	cfg := f.start(t, node)

	tests := []struct {
		namespace, id string
		wantNS        string
		wantDriver    common.ContainerDriver
	}{
		{"", "kube-system/web:app:1", "k8s.io", common.ContainerDriver_CRI},
		{"k8s.io", "kube-system/web:app:1", "k8s.io", common.ContainerDriver_CRI},
		{" taloscontainers ", "edge-proxy", containerNSTalos, common.ContainerDriver_CONTAINERD},
	}

	for _, tt := range tests {
		f.mu.Lock()
		f.logRequests = nil
		f.mu.Unlock()

		out, err := ContainerLogs(cfg, "fake", node, tt.namespace, tt.id, 50)
		if tail := decodeJSON[logTail](t, out, err); len(tail.Lines) != 1 || tail.Lines[0] != "log line of "+tt.id {
			t.Errorf("%q logs = %s", tt.namespace, out)
		}

		f.mu.Lock()
		reqs := f.logRequests
		f.mu.Unlock()

		if len(reqs) != 1 || reqs[0].GetNamespace() != tt.wantNS || reqs[0].GetDriver() != tt.wantDriver || reqs[0].GetId() != tt.id {
			t.Errorf("%q requests = %v", tt.namespace, reqs)
		}
	}

	if _, err := ContainerLogs(cfg, "fake", node, "default", "web", 50); err == nil || !strings.Contains(err.Error(), "unknown container namespace") {
		t.Errorf("an unknown namespace must be refused: %v", err)
	}
}

func TestDemoTalosContainers(t *testing.T) {
	yaml := demoConfigForTest(t)

	for _, n := range demoNodes() {
		out, err := NodeContainers(yaml, "", n.Node)
		if err != nil {
			t.Fatal(err)
		}

		var list containerList
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			t.Fatal(err)
		}

		var talos []containerInfo

		for _, c := range list.Containers {
			if c.Namespace == containerNSTalos {
				talos = append(talos, c)
			}
		}

		if n.Hostname != demoTalosContainersHost {
			if len(talos) != 0 {
				t.Errorf("%s lists Talos containers: %s", n.Hostname, out)
			}

			continue
		}

		if len(talos) != 2 || talos[0].Health != "healthy" || talos[1].Health != "degraded" ||
			talos[1].RestartCount < talosContainerRestartsAlert || talos[1].LastLog == "" {
			t.Errorf("demo Talos containers: %s", out)
		}

		// System first, then the Talos containers, then Kubernetes.
		if list.Containers[0].Namespace != containerNSSystem || list.Containers[len(list.Containers)-1].Namespace != containerNSK8s {
			t.Errorf("demo order: %s", out)
		}

		if err := ContainerRestart(yaml, "", n.Node, containerNSTalos, talos[0].ID); err == nil {
			t.Error("a demo restart must be refused")
		}

		logs, err := ContainerLogs(yaml, "", n.Node, containerNSTalos, talos[0].ID, 10)
		if err != nil || !strings.Contains(logs, "lines") {
			t.Errorf("demo logs: %s %v", logs, err)
		}
	}

	features, err := NodeFeatures(yaml, "", demoNodes()[0].Node)
	if err != nil || !strings.Contains(features, `"talosContainers":{"supported":true,"minVersion":"v1.15"`) {
		t.Errorf("demo features: %s %v", features, err)
	}
}

// Screenshot mode shows Talos container names like hostnames (ctr-N), and they map back.
func TestTalosContainersMasked(t *testing.T) {
	yaml := demoConfigForTest(t)
	enableMask(t, "")

	var host string

	for _, n := range demoNodes() {
		if n.Hostname == demoTalosContainersHost {
			host = n.Node
		}
	}

	out, err := NodeContainers(yaml, "", host)
	if err != nil {
		t.Fatal(err)
	}

	for _, real := range []string{`"edge-proxy"`, `"sensor-agent"`} {
		if strings.Contains(out, real) {
			t.Errorf("%s shows in screenshot mode: %s", real, out)
		}
	}

	if !strings.Contains(out, `"name":"ctr-1"`) {
		t.Errorf("no masked container name: %s", out)
	}

	if got := privacy.unmaskText("ctr-1"); got != "edge-proxy" && got != "sensor-agent" {
		t.Errorf("ctr-1 maps back to %q", got)
	}
}
