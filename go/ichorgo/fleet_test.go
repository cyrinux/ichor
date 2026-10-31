package ichorgo

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const fleetNow = int64(1_800_000_000_000)

func runFleet(t *testing.T, entries ...map[string]any) fleetSummary {
	t.Helper()

	in, err := json.Marshal(map[string]any{"clusters": entries})
	if err != nil {
		t.Fatal(err)
	}

	out, err := FleetSummary(string(in), fleetNow)
	if err != nil {
		t.Fatal(err)
	}

	var got fleetSummary
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return got
}

// fleetCluster is a fresh, healthy Talos cluster of one ready node, with extra fields set.
func fleetCluster(name string, extra map[string]any) map[string]any {
	e := map[string]any{
		"key": "k-" + name, "name": name, "context": "ctx-" + name, "takenAt": fleetNow - 60_000,
		"nodes":       map[string]any{"10.0.0.1": map[string]any{"hostname": name + "-1", "health": "READY"}},
		"etcdChecked": true, "etcdAlarms": []string{},
	}

	for k, v := range extra {
		e[k] = v
	}

	return e
}

func fleetNodes(healths ...string) map[string]any {
	out := map[string]any{}
	for i, h := range healths {
		out["10.0.0."+string(rune('1'+i))] = map[string]any{"hostname": "node-" + string(rune('a'+i)), "health": h}
	}

	return out
}

func TestFleetTone(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		tone  string
		top   *fleetFinding
	}{
		{"all ready, upper case", map[string]any{"nodes": fleetNodes("READY", "READY")}, fleetOK, nil},
		{"all ready, iOS spelling", map[string]any{"nodes": fleetNodes("ready", "ready")}, fleetOK, nil},
		{"not ready, Android", map[string]any{"nodes": fleetNodes("READY", "NOT_READY")}, fleetWarn,
			&fleetFinding{Track: "nodes", Severity: "warning", Subject: "node-b not ready"}},
		{"not ready, iOS", map[string]any{"nodes": fleetNodes("notReady", "ready")}, fleetWarn,
			&fleetFinding{Track: "nodes", Severity: "warning", Subject: "node-a not ready"}},
		{"two unreachable", map[string]any{"nodes": fleetNodes("UNREACHABLE", "unreachable", "READY")}, fleetBad,
			&fleetFinding{Track: "nodes", Severity: "critical", Subject: "2 of 3 nodes unreachable"}},
		{"unknown health is unreachable", map[string]any{"nodes": fleetNodes("READY", "gone")}, fleetBad,
			&fleetFinding{Track: "nodes", Severity: "critical", Subject: "node-b unreachable"}},
		{"none answered, missed checks", map[string]any{"nodes": fleetNodes("UNREACHABLE", "UNREACHABLE"), "failures": 3}, fleetBad,
			&fleetFinding{Track: "reach", Severity: "critical", Subject: "no node answered the last 3 checks"}},
		{"etcd alarm", map[string]any{"etcdAlarms": []string{"8e9e05c52164694d:NOSPACE"}}, fleetBad,
			&fleetFinding{Track: "etcd", Severity: "critical", Subject: "NOSPACE"}},
		{"etcd not checked", map[string]any{"etcdChecked": false, "etcdAlarms": []string{"NOSPACE"}}, fleetOK, nil},
		{"kube has no etcd", map[string]any{"kube": true, "etcdAlarms": []string{"NOSPACE"}}, fleetOK, nil},
		{"critical alert, pipe", map[string]any{"amChecked": true, "amIssues": map[string]string{"f1": "critical|DiskFull|ns/pod"}}, fleetBad,
			&fleetFinding{Track: "alertmanager", Severity: "critical", Subject: "DiskFull · ns/pod"}},
		{"warning alert", map[string]any{"amChecked": true, "amIssues": map[string]string{"f1": "warning|Slow|"}}, fleetWarn,
			&fleetFinding{Track: "alertmanager", Severity: "warning", Subject: "Slow"}},
		{"unchecked track ignored", map[string]any{"amChecked": false, "amIssues": map[string]string{"f1": "critical|DiskFull|x"}}, fleetOK, nil},
		{"gitops colon severity (iOS)", map[string]any{"gitopsChecked": true, "gitopsIssues": map[string]string{"argocd|apps/shop": "critical:degraded"}}, fleetBad,
			&fleetFinding{Track: "gitops", Severity: "critical", Subject: "apps/shop (degraded)", Detail: "argocd"}},
		{"gitops flux pipe", map[string]any{"gitopsChecked": true, "gitopsIssues": map[string]string{"flux|Kustomization flux-system/apps": "warning|notReady"}}, fleetWarn,
			&fleetFinding{Track: "gitops", Severity: "warning", Subject: "flux-system/apps (notReady)", Detail: "flux"}},
		{"unknown severity is a warning", map[string]any{"dataChecked": true, "dataIssues": map[string]string{"longhorn|pvc-1": "odd"}}, fleetWarn,
			&fleetFinding{Track: "data", Severity: "warning", Subject: "longhorn pvc-1"}},
		{"checkup", map[string]any{"checkupChecked": true, "checkupIssues": map[string]string{"security|Pod|kube-system/x": "critical"}}, fleetBad,
			&fleetFinding{Track: "checkup", Severity: "critical", Subject: "Pod kube-system/x"}},
		{"storage uses the hostname", map[string]any{"storageChecked": true, "storageIssues": map[string]string{"10.0.0.1|EPHEMERAL": "warning|fill|node-a|EPHEMERAL|91|1|10|85|"}}, fleetWarn,
			&fleetFinding{Track: "storage", Severity: "warning", Subject: "node-a EPHEMERAL"}},
		{"sensor uses the hostname", map[string]any{"sensorChecked": true, "sensorIssues": map[string]string{"10.0.0.1|hot": "critical|hot|node-a|cpu|95.0|90.0"}}, fleetBad,
			&fleetFinding{Track: "sensor", Severity: "critical", Subject: "node-a hot"}},
		{"kubespan names both ends", map[string]any{"kubespanChecked": true, "kubespanIssues": map[string]string{"node-a|node-b": "critical|neverHandshaked|0|node-a|node-b"}}, fleetBad,
			&fleetFinding{Track: "kubespan", Severity: "critical", Subject: "node-a ↔ node-b", Detail: "neverHandshaked"}},
		{"kubespan short value uses the key", map[string]any{"kubespanChecked": true, "kubespanIssues": map[string]string{"node-a|node-b": "critical"}}, fleetBad,
			&fleetFinding{Track: "kubespan", Severity: "critical", Subject: "node-a ↔ node-b"}},
		{"kubespan unchecked is ignored", map[string]any{"kubespanChecked": false, "kubespanIssues": map[string]string{"node-a|node-b": "critical|down|0|node-a|node-b"}}, fleetOK, nil},
		{"plugin JSON severity", map[string]any{"pluginChecked": true, "pluginIssues": map[string]string{"plugin:x:y:ns/a": `{"severity":"critical","plugin":"x"}`}}, fleetBad,
			&fleetFinding{Track: "plugin", Severity: "critical", Subject: "plugin:x:y:ns/a"}},
		{"stale keeps counts", map[string]any{"takenAt": fleetNow - 2*fleetStaleAfterMillis, "nodes": fleetNodes("UNREACHABLE", "READY")}, fleetUnknown,
			&fleetFinding{Track: "nodes", Severity: "critical", Subject: "node-a unreachable"}},
		{"never checked", map[string]any{"takenAt": 0, "nodes": map[string]any{}}, fleetUnknown, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runFleet(t, fleetCluster("c", tc.extra))
			row := got.Clusters[0]

			if row.Tone != tc.tone {
				t.Errorf("tone = %s, want %s", row.Tone, tc.tone)
			}

			if !reflect.DeepEqual(row.TopFinding, tc.top) {
				t.Errorf("top = %+v, want %+v", row.TopFinding, tc.top)
			}
		})
	}
}

func TestFleetKubespanRanksAfterAlertmanagerBeforeCheckup(t *testing.T) {
	tunnel := map[string]string{"node-a|node-b": "critical|down|0|node-a|node-b"}

	got := runFleet(t, fleetCluster("c", map[string]any{
		"amChecked": true, "amIssues": map[string]string{"f1": "critical|DiskFull|x"},
		"kubespanChecked": true, "kubespanIssues": tunnel,
		"checkupChecked": true, "checkupIssues": map[string]string{"security|Pod|ns/x": "critical"},
	}))

	row := got.Clusters[0]
	if row.TopFinding == nil || row.TopFinding.Track != "alertmanager" {
		t.Errorf("top = %+v, want the Alertmanager critical", row.TopFinding)
	}

	if row.Critical != 3 {
		t.Errorf("critical = %d, want 3", row.Critical)
	}

	if want := []string{"etcd", "alertmanager", "kubespan", "checkup"}; !slices.Equal(row.TracksChecked, want) {
		t.Errorf("tracksChecked = %v, want %v", row.TracksChecked, want)
	}

	got = runFleet(t, fleetCluster("c", map[string]any{
		"kubespanChecked": true, "kubespanIssues": tunnel,
		"checkupChecked": true, "checkupIssues": map[string]string{"security|Pod|ns/x": "critical"},
	}))

	if top := got.Clusters[0].TopFinding; top == nil || top.Track != "kubespan" {
		t.Errorf("top = %+v, want the tunnel before the checkup", top)
	}
}

func TestFleetCriticalBeforeWarningAcrossTracks(t *testing.T) {
	got := runFleet(t, fleetCluster("c", map[string]any{
		"amChecked": true, "amIssues": map[string]string{"f1": "warning|Slow|x"},
		"pluginChecked": true, "pluginIssues": map[string]string{"plugin:b": `{"severity":"warning"}`, "plugin:a": `{"severity":"critical"}`},
	}))

	row := got.Clusters[0]
	if row.TopFinding == nil || row.TopFinding.Track != "plugin" || row.TopFinding.Subject != "plugin:a" {
		t.Errorf("top = %+v, want the plugin's critical first", row.TopFinding)
	}

	if row.Critical != 1 || row.Warnings != 2 {
		t.Errorf("critical/warnings = %d/%d, want 1/2", row.Critical, row.Warnings)
	}

	if want := []string{"etcd", "alertmanager", "plugin"}; !slices.Equal(row.TracksChecked, want) {
		t.Errorf("tracksChecked = %v, want %v", row.TracksChecked, want)
	}
}

func TestFleetOrder(t *testing.T) {
	warnAlert := map[string]any{"amChecked": true, "amIssues": map[string]string{"f1": "warning|A|", "f2": "warning|B|"}}

	got := runFleet(t,
		fleetCluster("ok-b", nil),
		fleetCluster("Ok-a", nil),
		fleetCluster("stale", map[string]any{"takenAt": fleetNow - fleetStaleAfterMillis - 1}),
		fleetCluster("warn-one", map[string]any{"nodes": fleetNodes("NOT_READY")}),
		fleetCluster("warn-two", warnAlert),
		fleetCluster("bad", map[string]any{"nodes": fleetNodes("UNREACHABLE", "READY")}),
	)

	var names []string
	for _, r := range got.Clusters {
		names = append(names, r.Name)
	}

	if want := []string{"bad", "warn-two", "warn-one", "stale", "Ok-a", "ok-b"}; !slices.Equal(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}

	if want := (fleetCounts{Bad: 1, Warn: 2, Unknown: 1, OK: 2}); got.Counts != want {
		t.Errorf("counts = %+v, want %+v", got.Counts, want)
	}

	if got.Worst != fleetBad {
		t.Errorf("worst = %s", got.Worst)
	}
}

func TestFleetStaleBoundary(t *testing.T) {
	got := runFleet(t, fleetCluster("edge", map[string]any{"takenAt": fleetNow - fleetStaleAfterMillis}))
	if got.Clusters[0].Stale || got.Clusters[0].Tone != fleetOK {
		t.Errorf("a check exactly one hour old is still fresh: %+v", got.Clusters[0])
	}
}

func TestFleetGroups(t *testing.T) {
	got := runFleet(t,
		fleetCluster("a", map[string]any{"tags": []string{"prod", "eu"}}),
		fleetCluster("b", map[string]any{"tags": []string{" prod ", "prod", ""}, "nodes": fleetNodes("UNREACHABLE")}),
		fleetCluster("c", map[string]any{"tags": []string{}}),
		fleetCluster("d", nil),
	)

	want := []fleetGroup{
		{Tag: "prod", Counts: fleetCounts{Bad: 1, OK: 1}, Keys: []string{"k-b", "k-a"}},
		{Tag: "eu", Counts: fleetCounts{OK: 1}, Keys: []string{"k-a"}},
		{Tag: "", Counts: fleetCounts{OK: 2}, Keys: []string{"k-c", "k-d"}},
	}

	if !reflect.DeepEqual(got.Groups, want) {
		t.Errorf("groups = %+v, want %+v", got.Groups, want)
	}

	if !slices.Equal(got.Clusters[0].Tags, []string{"prod"}) {
		t.Errorf("tags = %v, want trimmed and deduplicated", got.Clusters[0].Tags)
	}
}

func TestFleetEmptyAndBadJSON(t *testing.T) {
	got := runFleet(t)
	if got.Worst != fleetUnknown || len(got.Clusters) != 0 || got.Groups == nil {
		t.Errorf("empty fleet = %+v", got)
	}

	if _, err := FleetSummary("{not json", fleetNow); err == nil {
		t.Error("bad JSON accepted")
	}

	if _, err := FleetSummary(`{"clusters":[{"takenAt":"yesterday"}]}`, fleetNow); err == nil {
		t.Error("a takenAt that is not an integer was accepted")
	}
}

func TestFleetDeterministic(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"clusters": demoFleetEntries(fleetNow)})
	if err != nil {
		t.Fatal(err)
	}

	first, err := FleetSummary(string(raw), fleetNow)
	if err != nil {
		t.Fatal(err)
	}

	for range 20 {
		again, err := FleetSummary(string(raw), fleetNow)
		if err != nil || again != first {
			t.Fatalf("not deterministic:\n%s\n%s", first, again)
		}
	}
}

func TestDemoFleetSummary(t *testing.T) {
	var got fleetSummary
	if err := json.Unmarshal([]byte(DemoFleetSummary(fleetNow)), &got); err != nil {
		t.Fatal(err)
	}

	if got.Worst != fleetBad || len(got.Clusters) != 5 {
		t.Fatalf("demo = %+v", got)
	}

	if want := (fleetCounts{Bad: 2, Warn: 1, Unknown: 1, OK: 1}); got.Counts != want {
		t.Errorf("counts = %+v, want %+v", got.Counts, want)
	}

	for _, r := range got.Clusters {
		if (r.Tone == fleetBad || r.Tone == fleetWarn) && r.TopFinding == nil {
			t.Errorf("%s: %s row without a top finding", r.Name, r.Tone)
		}

		if strings.Contains(r.TopFinding.subjectOrEmpty(), "10.0.") {
			t.Errorf("%s: an address in the finding %q", r.Name, r.TopFinding.Subject)
		}
	}
}

// TestFleetSampleIsTheDemoInput keeps the probe's sample (go/cmd/probe/testdata/fleet.json) the
// demo fleet's input, as of its newest check.
func TestFleetSampleIsTheDemoInput(t *testing.T) {
	raw, err := os.ReadFile("../cmd/probe/testdata/fleet.json")
	if err != nil {
		t.Fatal(err)
	}

	var sample struct {
		Clusters []fleetEntry `json:"clusters"`
	}
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}

	newest := int64(0)
	for _, e := range sample.Clusters {
		newest = max(newest, e.TakenAt)
	}

	// The demo's newest check is three minutes old.
	if want := demoFleetEntries(newest + 3*fleetMinute); !reflect.DeepEqual(sample.Clusters, want) {
		t.Errorf("testdata/fleet.json is not the demo input; regenerate it from demoFleetEntries")
	}
}

func (f *fleetFinding) subjectOrEmpty() string {
	if f == nil {
		return ""
	}

	return f.Subject
}
