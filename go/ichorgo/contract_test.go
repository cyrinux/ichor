package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The facade contract (CYR-440): golden JSON of the responses the apps read most, written from
// the demo builders. The Kotlin and Swift tests decode the same files and fail when Go writes a
// key their model does not read, so a renamed json tag cannot drift into a silent default.
// Rewrite them with `just contract-update` after changing one of these types.

var updateContract = flag.Bool("update-contract", false, "rewrite go/ichorgo/testdata/contract/*.json")

const contractDir = "testdata/contract"

// contractNow freezes the demo's clock, so a fixture is the same on every run.
var contractNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

var contractFixtures = []struct {
	name  string
	build func() any
}{
	{"cluster-overview", func() any { return clusterOverview{Context: "demo", Nodes: demoNodes()} }},
	{"kube-pods", func() any { return kubePodList{Pods: demoPods()} }},
	{"kube-workloads", func() any { return kubeWorkloadList{Workloads: demoKubeWorkloads()} }},
	{"kube-cronjobs", func() any { return kubeCronJobList{CronJobs: demoCronJobs(contractNow)} }},
	{"kube-nodes", func() any { return demoEKSNodes() }},
	{"argocd", func() any { return demoArgoCD(contractNow) }},
	{"flux", func() any { return demoFlux(contractNow) }},
	{"alerts", func() any { return groupAMAlerts(demoAMAlerts(contractNow)) }},
	{"checkup", func() any { return demoCheckup(contractNow, "en") }},
	{"inventory", func() any { return demoInventory(contractNow.UnixMilli()) }},
	{"kubespan", func() any { return demoKubeSpan() }},
	{"topology", func() any { return demoTopology() }},
	// Written out: the demo's since is its clock's, inside a string the fixture cannot freeze.
	{"kubespan-mesh", func() any {
		return kubespanMeshHealth{
			Context: "demo", Checked: true, Partial: false, Links: 3, Broken: 1,
			Issues: map[string]string{"node-a|node-b": "critical|neverHandshaked|0|node-a|node-b"},
		}
	}},
	{"cluster-updates", func() any {
		u := demoClusterUpdates(contractNow)
		sortUpdates(&u)

		return u
	}},
	{"update-proposal-plan", contractUpdatePlan},
	{"diag-entries", contractDiagEntries},
	{"oncall-incidents", func() any {
		alerts := demoAMAlerts(contractNow)
		inputs := make([]onCallAmAlert, 0, len(alerts))

		for _, a := range alerts {
			inputs = append(inputs, onCallAmAlert{Fingerprint: a.Fingerprint, Labels: a.Labels})
		}

		return matchOnCall(demoOnCall(contractNow), inputs)
	}},
	{"fleet-summary", func() any {
		now := contractNow.UnixMilli()

		return summarizeFleet(demoFleetEntries(now), now)
	}},
}

// contractDiagEntries is DiagEntries' shape, newest first: a failed call with its error, a core
// trace step and an app line without attributes.
func contractDiagEntries() any {
	at := contractNow.UnixMilli()

	return []diagEntry{
		{
			At: at, Level: "warn", Component: "api", Msg: "call failed",
			Attrs: map[string]any{"fn": "ClusterOverview"}, Err: "dial tcp <redacted>: connection refused",
		},
		{At: at, Level: "info", Component: "trace", Msg: "startup step", Attrs: map[string]any{"step": "config.parse", "ms": 12}},
		{At: at, Level: "info", Component: "ios", Msg: "startup app.init start"},
	}
}

// contractUpdatePlan is the demo's plan for podinfo, its exporter and ingress-nginx, its branch
// frozen.
func contractUpdatePlan() any {
	u := demoClusterUpdates(contractNow)

	var items []updateProposalItem

	for _, v := range []any{u.Images[0], u.Images[1], u.Charts[0]} {
		b, _ := json.Marshal(v) //nolint:errcheck

		var it updateProposalItem
		_ = json.Unmarshal(b, &it) //nolint:errcheck
		it.Version = ""
		items = append(items, it)
	}

	plan, err := planUpdateProposal(context.Background(), demoUpdateEditEnv(), demoUpdateProposalRepo(), items)
	if err != nil {
		panic(err)
	}

	plan.Branch = "ichor/update-3-dependencies-0001"

	return plan
}

// contractJSON is v as a fixture: indented, map keys sorted (encoding/json does), lists cut to
// their first three items so a file stays small, epoch instants frozen to contractNow.
func contractJSON(t *testing.T, v any) []byte {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatal(err)
	}

	out, err := json.MarshalIndent(contractTrim(tree), "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	return append(out, '\n')
}

func contractTrim(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, item := range v {
			v[k] = contractTrim(item)
		}

		return v
	case []any:
		if len(v) > 3 {
			v = v[:3]
		}

		for i, item := range v {
			v[i] = contractTrim(item)
		}

		return v
	case float64:
		// Instants (2020-2039, epoch s or ms) the demo takes from the clock at start: the frozen one.
		switch {
		case v >= 1.6e12 && v < 2.2e12:
			return float64(contractNow.UnixMilli())
		case v >= 1.6e9 && v < 2.2e9:
			return float64(contractNow.Unix())
		}

		return v
	default:
		return v
	}
}

func TestContractFixtures(t *testing.T) {
	seen := map[string]bool{}

	for _, f := range contractFixtures {
		t.Run(f.name, func(t *testing.T) {
			got := contractJSON(t, f.build())
			if again := contractJSON(t, f.build()); !bytes.Equal(got, again) {
				t.Fatalf("%s is not deterministic: freeze what changes between runs", f.name)
			}

			if len(got) > 64<<10 {
				t.Fatalf("%s is %d bytes, keep a fixture under 64 KiB", f.name, len(got))
			}

			path := filepath.Join(contractDir, f.name+".json")
			seen[f.name+".json"] = true

			if *updateContract {
				if err := os.MkdirAll(contractDir, 0o755); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}

				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v: run `just contract-update`", err)
			}

			if line, ok := firstDiff(want, got); !ok {
				t.Fatalf("%s changed at line %d: %s\nrun `just contract-update`, then fix the Kotlin and Swift models the app tests point at", path, line.n, line.text)
			}
		})
	}

	entries, err := os.ReadDir(contractDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		if !seen[e.Name()] && !*updateContract {
			t.Errorf("%s has no fixture builder: remove it or add one", filepath.Join(contractDir, e.Name()))
		}
	}
}

type diffLine struct {
	n    int
	text string
}

// firstDiff is the first line where got differs from want; ok when they are the same.
func firstDiff(want, got []byte) (diffLine, bool) {
	if bytes.Equal(want, got) {
		return diffLine{}, true
	}

	w, g := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	for i := range max(len(w), len(g)) {
		wl, gl := "", ""
		if i < len(w) {
			wl = w[i]
		}

		if i < len(g) {
			gl = g[i]
		}

		if wl != gl {
			return diffLine{i + 1, fmt.Sprintf("want %q, got %q", strings.TrimSpace(wl), strings.TrimSpace(gl))}, false
		}
	}

	return diffLine{}, true
}

func TestContractFixturesAreListedOnce(t *testing.T) {
	names := make([]string, 0, len(contractFixtures))
	for _, f := range contractFixtures {
		names = append(names, f.name)
	}

	slices.Sort(names)

	if len(slices.Compact(slices.Clone(names))) != len(names) {
		t.Fatalf("duplicate fixture names: %v", names)
	}
}
