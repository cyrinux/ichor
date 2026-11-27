package ichorgo

import (
	"slices"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
)

const metricsV3 = "[metrics]\n  address = \"0.0.0.0:11234\"\n"

// TestCRIDocsFromConfig reads both sources of CRI fragments from a machine config: the
// CRICustomizationConfig documents and the legacy machine file.
func TestCRIDocsFromConfig(t *testing.T) {
	raw := `version: v1alpha1
machine:
  type: worker
  token: demo-token
  files:
    - path: /etc/cri/conf.d/20-customization.part
      op: create
      content: |
        [grpc]
          tcp_address = "0.0.0.0:2000"
cluster:
  controlPlane:
    endpoint: https://cp.example.invalid:6443
---
apiVersion: v1alpha1
kind: CRICustomizationConfig
name: enable-metrics
content: |
  [metrics]
    address = "0.0.0.0:11234"
`

	cfg, err := configloader.NewFromBytes([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}

	docs := criDocs(cfg)
	if len(docs) != 2 || docs[0].name != "customization" || docs[1].name != "enable-metrics" || !strings.Contains(docs[1].content, "[metrics]") {
		t.Fatalf("docs = %+v", docs)
	}

	if got := versionUpgradeNotes("v1.14.7", "v1.15.0", docs, "").warnings; len(got) != 2 {
		t.Fatalf("warnings = %q", got)
	}

	if criDocs(nil) != nil {
		t.Fatal("nil config has fragments")
	}
}

func TestUpgradeNotesContainerdV4(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
		docs     []criDoc
		readErr  string
		want     []string // substrings, one per warning, in order
		notes    bool     // the SMART note is expected
		unchecks bool
	}{
		{
			name: "metrics address in a document", from: "v1.14.7", to: "v1.15.0",
			docs:  []criDoc{{name: "enable-metrics", content: metricsV3}},
			want:  []string{`CRICustomizationConfig "enable-metrics": Talos 1.15 ignores [metrics] address; move it to [plugins."io.containerd.server.v1.metrics"]`},
			notes: true,
		},
		{
			name: "legacy machine file, renamed grpc keys", from: "v1.14.2", to: "v1.15.1",
			docs: []criDoc{{name: "customization", content: "# tune\n[grpc]\n  tcp_address = \"0.0.0.0:2000\" # tcp\n  max_recv_message_size = 1\n[debug]\n  level = \"debug\"\n"}},
			want: []string{
				`the machine file /etc/cri/conf.d/20-customization.part: Talos 1.15 ignores [grpc] tcp_address (now address); move it to [plugins."io.containerd.server.v1.grpc-tcp"]`,
				`[grpc] max_recv_message_size; move it to [plugins."io.containerd.server.v1.grpc"]`,
			},
			notes: true,
		},
		{
			name: "dotted keys before any table", from: "v1.14.0", to: "v1.15.0",
			docs:  []criDoc{{name: "m", content: "metrics.grpc_histogram = true\nttrpc.address = \"/run/t.sock\"\n"}},
			want:  []string{`[metrics] grpc_histogram; move it to [plugins."io.containerd.metrics.v1.grpc-prometheus"]`, `[ttrpc] address`},
			notes: true,
		},
		{
			name: "settings already in the version 4 place", from: "v1.14.7", to: "v1.15.0",
			docs:  []criDoc{{name: "enable-metrics", content: "[plugins.\"io.containerd.server.v1.metrics\"]\n  address = \"0.0.0.0:11234\"\n[plugins.\"io.containerd.cri.v1.runtime\"]\n  enable_cdi = true\n"}},
			notes: true,
		},
		{name: "no customization", from: "v1.14.7", to: "v1.15.0", notes: true},
		{
			name: "config unreadable (os:reader)", from: "v1.14.7", to: "v1.15.0",
			docs: []criDoc{{name: "enable-metrics", content: metricsV3}}, readErr: "permission denied",
			notes: true, unchecks: true,
		},
		{name: "patch within 1.15", from: "v1.15.0", to: "v1.15.1", docs: []criDoc{{name: "enable-metrics", content: metricsV3}}},
		{name: "patch within 1.14", from: "v1.14.6", to: "v1.14.7", docs: []criDoc{{name: "enable-metrics", content: metricsV3}}},
		{name: "unknown version", from: "", to: "v1.15.0", docs: []criDoc{{name: "enable-metrics", content: metricsV3}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := versionUpgradeNotes(tt.from, tt.to, tt.docs, tt.readErr)

			if len(got.warnings) != len(tt.want) {
				t.Fatalf("warnings = %q, want %d", got.warnings, len(tt.want))
			}

			for i, w := range tt.want {
				if !strings.Contains(got.warnings[i], w) {
					t.Errorf("warning %d = %q, want %q", i, got.warnings[i], w)
				}
			}

			if has := slices.Contains(got.notes, smartNote); has != tt.notes {
				t.Errorf("notes = %q, SMART note expected: %v", got.notes, tt.notes)
			}

			if (got.unchecked != "") != tt.unchecks || (tt.unchecks && !strings.Contains(got.unchecked, "os:admin")) {
				t.Errorf("unchecked = %q", got.unchecked)
			}
		})
	}
}

func TestUpgradePlanVersionNotes(t *testing.T) {
	docs := []criDoc{{name: "enable-metrics", content: metricsV3}}

	plan := computePlan(planInput{target: running("a", "v1.14.7", false), cri: docs})
	if !containsText(plan.Warnings, "Talos 1.15 ignores [metrics] address") || !slices.Contains(plan.Notes, smartNote) {
		t.Fatalf("1.14 node: warnings %q notes %q", plan.Warnings, plan.Notes)
	}

	if got := rebootWarnings(plan); containsText(got, "Talos 1.15") {
		t.Errorf("a reboot keeps the 1.15 warning: %q", got)
	}

	if p := computePlan(planInput{target: running("a", "v1.15.0", false), cri: docs}); containsText(p.Warnings, "1.15") || len(p.Notes) != 0 {
		t.Fatalf("1.15 node: warnings %q notes %q", p.Warnings, p.Notes)
	}

	unreadable := computePlan(planInput{target: running("a", "v1.14.7", false), imageErr: "permission denied"})
	if !containsText(unreadable.Notes, "could not check the containerd settings") {
		t.Fatalf("unreadable config: notes %q", unreadable.Notes)
	}

	// The cluster plan recomputes them for its version: none within 1.14, once at 1.15.
	within := clusterNodeOf("a", plan, "v1.14.8", false)
	if containsText(within.Warnings, "Talos 1.15") || len(within.notes) != 0 {
		t.Fatalf("cluster plan to 1.14.8: warnings %q notes %q", within.Warnings, within.notes)
	}

	across := clusterNodeOf("a", plan, "v1.15.0", false)
	if n := countMatches(across.Warnings, "Talos 1.15 ignores"); n != 1 || !slices.Contains(across.notes, smartNote) {
		t.Fatalf("cluster plan to 1.15.0: warnings %q notes %q", across.Warnings, across.notes)
	}

	cluster := clusterUpgradePlan{Version: "v1.15.0", Nodes: []clusterPlanNode{across, clusterNodeOf("b", unreadable, "v1.15.0", false)}, Notes: []string{}}
	summarizeClusterPlan(&cluster)

	if len(cluster.Notes) != 2 || cluster.Notes[0] != smartNote || !strings.HasPrefix(cluster.Notes[1], "host-a: could not check") {
		t.Fatalf("cluster notes %q", cluster.Notes)
	}
}

func TestDemoClusterPlanContainerdWarning(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := ClusterUpgradePlan(demo, "", "", "v1.15.0")
	plan := decodeJSON[clusterUpgradePlan](t, out, err)

	if !slices.Contains(plan.Notes, smartNote) {
		t.Errorf("notes = %q", plan.Notes)
	}

	for _, n := range plan.Nodes {
		if got := containsText(n.Warnings, `CRICustomizationConfig "enable-metrics"`); got != (n.Hostname == "demo-worker-1") {
			t.Errorf("%s warnings = %q", n.Hostname, n.Warnings)
		}
	}
}

func countMatches(list []string, text string) int {
	n := 0

	for _, s := range list {
		if strings.Contains(s, text) {
			n++
		}
	}

	return n
}
