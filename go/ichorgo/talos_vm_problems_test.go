package ichorgo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestVirtualMachineProblems(t *testing.T) {
	readyDisk := talosVMDisk{Name: "root", Phase: "ready"}
	brokenDisk := talosVMDisk{Name: "data", Phase: "notReady", Error: `volume pool "fast" is not ready`}

	cases := []struct {
		name   string
		vm     talosVM
		kind   string
		detail string
	}{
		{"running as wanted", talosVM{WantedPowerState: "running", PowerState: "running", Stage: "ready", Disks: []talosVMDisk{readyDisk}}, "", ""},
		{"stopped on purpose is not a problem", talosVM{WantedPowerState: "stopped", PowerState: "stopped", Stage: "ready"}, "", ""},
		{"stopped on purpose with an error is not a problem", talosVM{WantedPowerState: "stopped", PowerState: "stopped", Stage: "error", Error: "x"}, "", ""},
		{"stopped through the API (override, converged)", talosVM{WantedPowerState: "running", PowerState: "stopped", Stage: "ready", Disks: []talosVMDisk{brokenDisk}}, "", ""},
		{"no spec", talosVM{PowerState: "stopped", Stage: "unknown"}, "", ""},
		{"no status yet", talosVM{WantedPowerState: "running"}, "", ""},
		{"starting", talosVM{WantedPowerState: "running", PowerState: "stopped", Stage: "pending", Error: "domain definition has not caught up"}, "", ""},
		{"error", talosVM{WantedPowerState: "running", PowerState: "stopped", Stage: "error", Error: "domain name is occupied by another VM"}, vmProblemError, "domain name is occupied by another VM"},
		{"error outranks a failed disk", talosVM{WantedPowerState: "running", Stage: "error", Error: "boom", Disks: []talosVMDisk{brokenDisk}}, vmProblemError, "boom"},
		{"failed disk while pending", talosVM{WantedPowerState: "running", PowerState: "unknown", Stage: "pending", Disks: []talosVMDisk{readyDisk, brokenDisk}}, vmProblemDiskFailed, `data: volume pool "fast" is not ready`},
		{"disk observation lost is transient", talosVM{WantedPowerState: "running", PowerState: "running", Stage: "ready", Disks: []talosVMDisk{{Name: "root", Phase: "observationUnavailable", Error: "lost"}}}, "", ""},
		{"disk not ready without a reason", talosVM{WantedPowerState: "running", Stage: "pending", Disks: []talosVMDisk{{Name: "root", Phase: "notReady"}}}, "", ""},
		{"down: domain not observed", talosVM{WantedPowerState: "running", PowerState: "unknown", Stage: "unknown", Error: "domain has not been observed"}, vmProblemDown, "domain has not been observed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.vm.Name = "app"
			got := vmProblemsOf("10.5.0.2", []talosVM{tc.vm})

			if tc.kind == "" {
				if len(got) != 0 {
					t.Fatalf("problems = %+v, want none", got)
				}

				return
			}

			want := []vmProblem{{Node: "10.5.0.2", VM: "app", Kind: tc.kind, Detail: tc.detail}}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("problems = %+v, want %+v", got, want)
			}
		})
	}
}

// The node's text is untrusted: control characters go, whitespace collapses, long text is clipped.
func TestVirtualMachineProblemDetailClipped(t *testing.T) {
	vm := talosVM{Name: "app", WantedPowerState: "running", Stage: "error", Error: "line one\n\x1b[31mline\ttwo " + strings.Repeat("x", 400)}

	got := vmProblemsOf("n", []talosVM{vm})[0].Detail
	if strings.ContainsAny(got, "\n\t\x1b") || !strings.HasPrefix(got, "line one [31mline two x") {
		t.Errorf("detail not cleaned: %q", got)
	}

	if n := len([]rune(got)); n != vmProblemDetailMax || !strings.HasSuffix(got, "…") {
		t.Errorf("detail is %d runes: %q", n, got)
	}
}

// The demo cluster has one VM that should run and has an error: one problem, on its node.
func TestDemoVirtualMachineProblems(t *testing.T) {
	out, err := ClusterVirtualMachines(demoConfigForTest(t), "")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Nodes []struct {
			Node     string      `json:"node"`
			Hostname string      `json:"hostname"`
			Problems []vmProblem `json:"problems"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	var problems []vmProblem

	for _, n := range got.Nodes {
		if n.Problems == nil {
			t.Errorf("%s: problems is null", n.Hostname)
		}

		for _, p := range n.Problems {
			if p.Node != n.Node {
				t.Errorf("problem node %q on node %q", p.Node, n.Node)
			}
		}

		problems = append(problems, n.Problems...)
	}

	if len(problems) != 1 || problems[0].VM != "test-desktop" || problems[0].Kind != vmProblemError || problems[0].Detail == "" {
		t.Errorf("demo problems = %+v, want test-desktop's error only", problems)
	}
}
