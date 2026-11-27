package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestTalosContainerIssue(t *testing.T) {
	tests := []struct {
		name     string
		c        containerInfo
		severity string
	}{
		{"healthy", containerInfo{Status: "running", Health: "healthy"}, ""},
		{"healthy after many restarts", containerInfo{Status: "running", Health: "healthy", RestartCount: 40}, ""},
		{"restarting, below the threshold", containerInfo{Status: "backoff", Health: "degraded", RestartCount: talosContainerRestartsAlert - 1}, ""},
		{"restarting repeatedly", containerInfo{Status: "backoff", Health: "degraded", RestartCount: talosContainerRestartsAlert}, fleetWarning},
		{"failed (given up)", containerInfo{Status: "failed", Health: "degraded", ExitCode: 2}, fleetCritical},
		{"pending", containerInfo{Status: "pending", Health: "pending", RestartCount: 9}, ""},
		{"succeeded", containerInfo{Status: "succeeded", Health: "healthy"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := talosContainerIssue(tt.c); got != tt.severity {
				t.Errorf("severity = %q, want %q", got, tt.severity)
			}
		})
	}
}

func TestTalosContainersHealthOf(t *testing.T) {
	entries := []talosContainersEntry{
		{Node: "192.0.2.71", Hostname: "edge-1", Containers: []containerInfo{
			{Name: "agent", Status: "backoff", Health: "degraded", RestartCount: 7, ExitCode: 1, Error: "secret detail", LastLog: "workload output"},
			{Name: "edge-proxy", Status: "running", Health: "healthy"},
			{Name: "job|x", Status: "failed", Health: "degraded", ExitCode: 3},
		}},
		{Node: "192.0.2.72", Hostname: "edge-2"},
		{Node: "192.0.2.73", Hostname: "192.0.2.73", Error: "connection refused"},
	}

	got := talosContainersHealthOf("lab", entries)

	want := talosContainersHealth{
		Context: "lab", Checked: true, Unread: []string{"192.0.2.73"},
		Issues: map[string]string{
			"192.0.2.71|agent": "warning|backoff|7|1|edge-1|agent",
			"192.0.2.71|job|x": "critical|failed|0|3|edge-1|job/x",
		},
	}

	if !equalJSON(t, got, want) {
		t.Error("health differs")
	}

	// Never the error text nor the last log: they are workload output, not for a notification.
	out, err := toJSON(got)
	if err != nil || strings.Contains(out, "secret detail") || strings.Contains(out, "workload output") {
		t.Errorf("health JSON = %s %v", out, err)
	}

	if none := talosContainersHealthOf("lab", []talosContainersEntry{{Node: "a", Error: "down"}}); none.Checked || len(none.Issues) != 0 {
		t.Errorf("nothing read = %+v", none)
	}
}

func TestTalosContainersHealthFake(t *testing.T) {
	f, cfg := talosContainerCluster(t)

	read := func() talosContainersHealth {
		t.Helper()

		out, err := TalosContainersHealth(cfg, "fake")
		if err != nil {
			t.Fatal(err)
		}

		var got talosContainersHealth
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}

		return got
	}

	// The 1.14 node answers (it has no Talos containers); the 1.15 one has a crashing agent.
	got := read()
	if !got.Checked || len(got.Unread) != 0 || len(got.Issues) != 1 || got.Issues["192.0.2.71|agent"] != "warning|backoff|7|1|host-192-0-2-71|agent" {
		t.Errorf("health = %+v", got)
	}

	f.mu.Lock()
	delete(f.versions, "192.0.2.71") // down: its issues are the apps' to keep
	f.mu.Unlock()

	if got := read(); !got.Checked || !slices.Equal(got.Unread, []string{"192.0.2.71"}) || len(got.Issues) != 0 {
		t.Errorf("health with a node down = %+v", got)
	}
}

func TestDemoTalosContainersHealth(t *testing.T) {
	yaml := demoConfigForTest(t)

	out, err := TalosContainersHealth(yaml, "")
	if err != nil {
		t.Fatal(err)
	}

	var got talosContainersHealth
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if !got.Checked || len(got.Issues) != 1 || len(got.Unread) != 0 {
		t.Errorf("demo health = %s", out)
	}

	for key, value := range got.Issues {
		if !strings.HasSuffix(key, "|sensor-agent") || !strings.HasPrefix(value, "warning|backoff|12|1|"+demoTalosContainersHost+"|") {
			t.Errorf("demo issue %s = %s", key, value)
		}
	}
}
