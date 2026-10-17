package ichorgo

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestBottleneckRatesAndCounterReset(t *testing.T) {
	a := nodeStats{At: 1000, BootTime: 20, CPUTotal: 100, CPUWait: 5, CPUSteal: 2,
		NetworkDevices: []networkCounters{{Name: "eth0", Rx: 100, Tx: 100, Errors: 2, Drops: 3}},
		DiskDevices:    []diskCounters{{Name: "sda", Read: 100, Write: 100, Operations: 10, TimeMs: 20, BusyMs: 100}}}
	b := nodeStats{At: 3000, BootTime: 20, CPUTotal: 200, CPUWait: 15, CPUSteal: 7,
		NetworkDevices: []networkCounters{{Name: "eth0", Rx: 2100, Tx: 10, Errors: 6, Drops: 7}, {Name: "eth1", Rx: 9999}},
		DiskDevices:    []diskCounters{{Name: "sda", Read: 1100, Write: 100, Operations: 20, TimeMs: 60, BusyMs: 1100}}}
	r := calculateBottlenecks(a, b)
	if r.Wait != 10 || r.Steal != 5 || len(r.Network) != 1 || r.Network[0].Read != 1000 || r.Network[0].Write != 0 || r.Network[0].Errors != 2 || r.Disks[0].Busy != 50 || r.Disks[0].Latency != 4 {
		t.Fatalf("unexpected rates: %+v", r)
	}
	b.BootTime = 30
	r = calculateBottlenecks(a, b)
	if len(r.Disks) != 0 || len(r.Network) != 0 || r.Wait != 0 {
		t.Fatalf("reboot must invalidate all rates: %+v", r)
	}
	b.BootTime = 20
	b.Errors = map[string]string{"network": "unavailable"}
	if r = calculateBottlenecks(a, b); len(r.Network) != 0 || r.Errors["network"] == "" {
		t.Fatal("failed section appeared healthy")
	}
	a.Errors = map[string]string{"disk": "unavailable"}
	if len(calculateBottlenecks(a, b).Disks) != 0 {
		t.Fatal("recovery must wait for a second valid sample")
	}
}
func TestDriftUsesRolesAndSkipsUnknownFields(t *testing.T) {
	current := driftSnapshot{Scope: "cluster", Nodes: []driftNode{
		{Node: "cp-a", Role: "controlplane", Values: map[string]string{"dns": "a", "ntp": "time"}},
		{Node: "cp-b", Role: "controlplane", Values: map[string]string{"dns": "b"}},
		{Node: "worker", Role: "worker", Values: map[string]string{"dns": "c"}},
		{Node: "down", Role: "controlplane", Values: map[string]string{}, Errors: map[string]string{"node": "unreachable"}},
	}}
	changes := compareDrift(driftSnapshot{}, current, false)
	if len(changes) != 1 || changes[0].Key != "dns" || changes[0].Reference != "cp-a" {
		t.Fatalf("role/unknown comparison: %+v", changes)
	}
	baseline := current
	current.Nodes = append([]driftNode{}, current.Nodes...)
	current.Nodes[1].Values = map[string]string{"dns": "b", "ntp": "newly available"}
	if len(compareDrift(baseline, current, true)) != 0 {
		t.Fatal("unknown baseline is not drift")
	}
	raw, _ := toJSON(current)
	baseline.Scope = "other"
	old, _ := toJSON(baseline)
	if _, err := CompareDrift(old, raw); err == nil {
		t.Fatal("cross-cluster baseline accepted")
	}
}
func TestDiffSettingItemisesListValues(t *testing.T) {
	cases := []struct {
		name, before, after string
		added, removed      []string
		changed             []driftPair
	}{
		{name: "extensions",
			before:  "iscsi-tools=v0.2.0, qemu-guest-agent=10.0.2, schematic=0123abcd, util-linux-tools=2.41.1",
			after:   "btrfs=v1.14.1, iscsi-tools=v0.2.0, schematic=4567cdef, util-linux-tools=2.41.2",
			added:   []string{"btrfs=v1.14.1"},
			removed: []string{"qemu-guest-agent=10.0.2"},
			changed: []driftPair{{Name: "schematic", Before: "0123abcd", After: "4567cdef"}, {Name: "util-linux-tools", Before: "2.41.1", After: "2.41.2"}}},
		{name: "mtu renamed interface", before: "ens3=1500, eth1=9000", after: "enp1s0=1500, eth1=9000",
			added: []string{"enp1s0=1500"}, removed: []string{"ens3=1500"}, changed: []driftPair{}},
		{name: "servers", before: "192.0.2.1, 192.0.2.2", after: "192.0.2.2, 192.0.2.3",
			added: []string{"192.0.2.3"}, removed: []string{"192.0.2.1"}, changed: []driftPair{}},
		{name: "scalar", before: "true", after: "false",
			added: []string{"false"}, removed: []string{"true"}, changed: []driftPair{}},
		{name: "from empty", before: "", after: "a=1",
			added: []string{"a=1"}, removed: []string{}, changed: []driftPair{}},
	}
	for _, c := range cases {
		added, removed, changed := diffSetting(c.before, c.after)
		if !reflect.DeepEqual(added, c.added) || !reflect.DeepEqual(removed, c.removed) || !reflect.DeepEqual(changed, c.changed) {
			t.Errorf("%s: got +%v -%v ~%v", c.name, added, removed, changed)
		}
	}
}
func TestCompareDriftItemisesListKeysOnly(t *testing.T) {
	current := driftSnapshot{Scope: "cluster", Nodes: []driftNode{
		{Node: "192.0.2.20", Hostname: "worker-a", Role: "worker", Values: map[string]string{"mtu": "ens3=1500", "secureBoot": "true"}},
		{Node: "192.0.2.21", Hostname: "worker-b", Role: "worker", Values: map[string]string{"mtu": "enp1s0=1500", "secureBoot": "false"}},
	}}
	raw, _ := toJSON(current)
	out, err := CompareDrift("", raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"node":"192.0.2.21","reference":"192.0.2.20","nodeName":"worker-b","referenceName":"worker-a","key":"mtu","before":"ens3=1500","after":"enp1s0=1500","added":["enp1s0=1500"],"removed":["ens3=1500"],"changed":[]},` +
		`{"node":"192.0.2.21","reference":"192.0.2.20","nodeName":"worker-b","referenceName":"worker-a","key":"secureBoot","before":"true","after":"false","added":[],"removed":[],"changed":[]}]`
	if strings.TrimSpace(out) != want {
		t.Fatalf("unexpected drift JSON:\n%s", out)
	}
}
func TestDemoDriftShowsSampleDifferences(t *testing.T) {
	config, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ClusterDriftSnapshot(config, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := CompareDrift("", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var changes []driftChange
	if err = json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatal(err)
	}
	keys := map[string]driftChange{}
	for _, c := range changes {
		if c.NodeName != "demo-worker-2" || c.ReferenceName != "demo-worker-1" {
			t.Fatalf("unexpected demo drift on %s: %+v", c.NodeName, c)
		}
		keys[c.Key] = c
	}
	if ext := keys["extensions"]; len(ext.Added) != 1 || len(ext.Removed) != 1 || len(ext.Changed) != 2 {
		t.Fatalf("extensions: %+v", ext)
	}
	if mtu := keys["mtu"]; len(mtu.Added) != 1 || len(mtu.Removed) != 1 || len(mtu.Changed) != 0 {
		t.Fatalf("mtu: %+v", mtu)
	}
	if sb := keys["secureBoot"]; sb.Before != "true" || sb.After != "false" || len(keys) != 3 {
		t.Fatalf("secureBoot or extra keys: %+v", keys)
	}
}
func TestIncidentBoundsDeduplicationAndIsolation(t *testing.T) {
	observation := clusterObservation{Scope: "cluster", At: 1000, Nodes: []observedNode{{Status: nodeOverview{Node: "node", Ready: true, Reachable: true}, Services: []serviceInfo{}, Links: map[string]string{}, Errors: map[string]string{}}}}
	raw, _ := toJSON(observation)
	events := []nodeEvent{{Node: "node", ID: "same", At: 1001, Message: "first"}, {Node: "node", ID: "same", At: 1001, Message: "duplicate"}}
	batch, _ := toJSON(events)
	recording, err := UpdateIncident("", raw, batch)
	if err != nil {
		t.Fatal(err)
	}
	var doc incidentDocument
	if err = json.Unmarshal([]byte(recording), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != 2 {
		t.Fatalf("duplicates retained: %d", len(doc.Entries))
	}
	updated, err := UpdateIncident(recording, raw, batch)
	if err != nil {
		t.Fatal(err)
	}
	if updated != recording {
		t.Fatal("replaying the same sample should be idempotent")
	}
	observation.Scope = "another"
	other, _ := toJSON(observation)
	if _, err = UpdateIncident(recording, other, "[]"); err == nil {
		t.Fatal("cross-cluster recording accepted")
	}
	events = []nodeEvent{}
	for i := 0; i < 700; i++ {
		events = append(events, nodeEvent{Node: "node", ID: strings.Repeat("x", i+1), At: int64(i + 1002), Message: strings.Repeat("z", 3000)})
	}
	batch, _ = toJSON(events)
	recording, err = UpdateIncident(recording, raw, batch)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(recording), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Entries) != incidentLimit || doc.Dropped != 102 || len(doc.Entries[0].Detail) > incidentDetailLimit+3 {
		t.Fatalf("limits not enforced: %d entries, %d dropped", len(doc.Entries), doc.Dropped)
	}
}
func TestObservationDemoContainsOnlyAllowedSettings(t *testing.T) {
	config, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := ClusterDriftSnapshot(config, "")
	if err != nil {
		t.Fatal(err)
	}
	var drift driftSnapshot
	if err = json.Unmarshal([]byte(snapshot), &drift); err != nil {
		t.Fatal(err)
	}
	if len(drift.Nodes) != 5 || drift.Nodes[0].Values["dns"] == "" {
		t.Fatal("missing demo settings")
	}
	for _, forbidden := range []string{"PRIVATE KEY", "serial", "hardwareAddr", "machineconfig"} {
		if strings.Contains(snapshot, forbidden) {
			t.Fatalf("unexpected sensitive field %s", forbidden)
		}
	}
	raw, err := ClusterObservation(config, "")
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := UpdateIncident("", raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	var doc incidentDocument
	if err = json.Unmarshal([]byte(recorded), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Scope != drift.Scope || len(doc.Entries) == 0 {
		t.Fatal("demo recording failed")
	}
}

func TestStatsResponseErrorsAreNotZeroSamples(t *testing.T) {
	if statsResponseError(0, "", nil) == nil {
		t.Fatal("missing response accepted")
	}
	if statsResponseError(1, "proxy failure", nil) == nil {
		t.Fatal("proxy metadata failure accepted")
	}
	if statsResponseError(1, "", nil) != nil {
		t.Fatal("valid response rejected")
	}
}
func TestIncidentRecordsRemovalsButNotFailedReads(t *testing.T) {
	sample := clusterObservation{Scope: "cluster", At: 1000, Nodes: []observedNode{{Status: nodeOverview{Node: "node", Reachable: true, Ready: true}, Services: []serviceInfo{{ID: "kubelet", State: "Running", Health: "healthy"}}, Links: map[string]string{"eth0": "up"}, Errors: map[string]string{}}}}
	raw, _ := toJSON(sample)
	doc, err := UpdateIncident("", raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	sample.At = 2000
	sample.Nodes[0].Links = map[string]string{}
	sample.Nodes[0].Services = []serviceInfo{}
	sample.Nodes[0].Errors = map[string]string{"links": "unavailable", "services": "unavailable"}
	raw, _ = toJSON(sample)
	failed, err := UpdateIncident(doc, raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	var value incidentDocument
	if err = json.Unmarshal([]byte(failed), &value); err != nil {
		t.Fatal(err)
	}
	for _, e := range value.Entries {
		if e.At == 2000 && (e.Kind == "link" || e.Kind == "service") {
			t.Fatal("failed reads were treated as removal")
		}
	}
	sample.Nodes[0].Errors = map[string]string{}
	raw, _ = toJSON(sample)
	removed, err := UpdateIncident(doc, raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(removed), &value); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, e := range value.Entries {
		if e.At == 2000 && (e.Kind == "link" || e.Kind == "service") {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected link and service removals, got %d", found)
	}
}
func TestObservabilityScreenshotModeMasksNames(t *testing.T) {
	SetPrivacyMask(false, "")
	config, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}
	original, err := ClusterDriftSnapshot(config, "")
	if err != nil {
		t.Fatal(err)
	}
	var real driftSnapshot
	if err = json.Unmarshal([]byte(original), &real); err != nil {
		t.Fatal(err)
	}
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })
	masked, err := ClusterDriftSnapshot(config, "")
	if err != nil {
		t.Fatal(err)
	}
	var hidden driftSnapshot
	if err = json.Unmarshal([]byte(masked), &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Scope != real.Scope || strings.Contains(masked, "192.0.2.") || strings.Contains(masked, "demo-cp-") {
		t.Fatal("masking leaked names or changed cluster scope")
	}
}

func TestIncidentMetricSummarySurvivesDetailTruncation(t *testing.T) {
	stats := nodeStats{At: 1000, BootTime: 10, CPUTotal: 100, CPUWait: 1, Errors: map[string]string{}}
	for i := 0; i < 30; i++ {
		stats.NetworkDevices = append(stats.NetworkDevices, networkCounters{Name: strings.Repeat("n", 100) + strings.Repeat("x", i), Rx: 100})
	}
	observation := clusterObservation{Scope: "cluster", At: 1000, Nodes: []observedNode{{Status: nodeOverview{Node: "node", Reachable: true}, Stats: &stats, Services: []serviceInfo{}, Links: map[string]string{}, Errors: map[string]string{}}}}
	raw, _ := toJSON(observation)
	previous, err := UpdateIncident("", raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	stats.At = 2000
	stats.CPUTotal = 200
	stats.CPUWait = 3
	observation.At = 2000
	raw, _ = toJSON(observation)
	updated, err := UpdateIncident(previous, raw, "[]")
	if err != nil {
		t.Fatal(err)
	}
	var doc incidentDocument
	if err := json.Unmarshal([]byte(updated), &doc); err != nil {
		t.Fatal(err)
	}
	for _, entry := range doc.Entries {
		if entry.Kind != "metrics" {
			continue
		}
		if entry.Metrics == nil || len(entry.Metrics.Network) != 8 || entry.MetricsOmitted != 22 || entry.Metrics.Wait != 2 {
			t.Fatalf("lost bounded summary: %+v", entry)
		}
		if json.Valid([]byte(entry.Detail)) {
			t.Fatal("fixture must exercise truncated raw details")
		}
		if len(entry.Detail) > incidentDetailLimit+3 {
			t.Fatal("raw detail exceeded limit")
		}
		return
	}
	t.Fatal("missing metric entry")
}
