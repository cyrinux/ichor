package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
)

const nodeD = "dddddddddddddddd4444444444444444dddddddddddddddd4444444444444444"

// fakeGarageLayoutAPI answers the cluster-wide `garage json-api ENDPOINT [body]` calls the
// layout code makes, keyed by endpoint, and records each call with its body.
type fakeGarageLayoutAPI struct {
	t       *testing.T
	answers map[string]string
	fail    map[string]string // endpoint: the CLI's "Error: ..." line
	mu      sync.Mutex
	calls   []string
}

func (f *fakeGarageLayoutAPI) run(_ context.Context, namespace, pod, container string, argv []string) ([]byte, []byte, error) {
	if namespace != "storage" || pod != "garage-0" || container != "garage" {
		f.t.Errorf("exec target %s/%s/%s", namespace, pod, container)
	}

	if len(argv) < 3 || len(argv) > 4 || argv[0] != "/garage" || argv[1] != "json-api" {
		f.t.Errorf("unexpected argv %v", argv)

		return nil, nil, errors.New("not allowed")
	}

	body := ""
	if len(argv) == 4 {
		body = argv[3]
	}

	f.mu.Lock()
	f.calls = append(f.calls, strings.TrimSpace(argv[2]+" "+body))
	answer, ok := f.answers[argv[2]]
	msg, failing := f.fail[argv[2]]
	f.mu.Unlock()

	if failing {
		return nil, []byte("\x1b[31mError:\x1b[0m " + msg + "\n"), &kubeExecError{ExitCode: 1, Message: "exit"}
	}

	if !ok {
		f.t.Errorf("no answer for %s", argv[2])

		return nil, nil, errors.New("no answer")
	}

	return []byte(answer), nil, nil
}

func (f *fakeGarageLayoutAPI) called(endpoint string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, c := range f.calls {
		if strings.HasPrefix(c, endpoint) {
			out = append(out, c)
		}
	}

	return out
}

// garageLayoutAnswers: A and B hold storage roles (A is down), C is a gateway, D is known but
// has no role. Staged: D joins as storage, C leaves. Version 11 still drains, waiting on A.
func garageLayoutAnswers() map[string]string {
	return map[string]string{
		"GetClusterLayout": `{"version":12,"partitionSize":1835008000,
			"roles":[
				{"id":"` + nodeA + `","zone":"dc1","tags":["a"],"capacity":1000000000000,"storedPartitions":256,"usableCapacity":939524096000},
				{"id":"` + nodeB + `","zone":"dc2","tags":[],"capacity":1000000000000,"storedPartitions":256,"usableCapacity":939524096000},
				{"id":"` + nodeC + `","zone":"dc2","tags":["gw"],"capacity":null,"storedPartitions":null,"usableCapacity":null}],
			"parameters":{"zoneRedundancy":"maximum"},
			"stagedParameters":{"zoneRedundancy":{"atLeast":2}},
			"stagedRoleChanges":[
				{"id":"` + nodeD + `","zone":"dc3","tags":["d"],"capacity":2000000000000},
				{"id":"` + nodeC + `","remove":true}]}`,
		"GetClusterLayoutHistory": `{"currentVersion":12,"minAck":11,
			"versions":[{"version":10,"status":"Historical","storageNodes":1,"gatewayNodes":0},{"version":11,"status":"Draining","storageNodes":2,"gatewayNodes":1},{"version":12,"status":"Current","storageNodes":2,"gatewayNodes":1}],
			"updateTrackers":{"` + nodeA + `":{"ack":11,"sync":11,"syncAck":11},"` + nodeB + `":{"ack":12,"sync":12,"syncAck":11}}}`,
		"GetClusterStatus": `{"layoutVersion":12,"nodes":[
			{"id":"` + nodeA + `","hostname":"garage-a","isUp":false,"lastSeenSecsAgo":600,"draining":false,"role":{"zone":"dc1","tags":["a"],"capacity":1000000000000},"dataPartition":{"available":1,"total":1100000000000}},
			{"id":"` + nodeB + `","hostname":"garage-b","isUp":true,"draining":false,"role":{"zone":"dc2","tags":[],"capacity":1000000000000}},
			{"id":"` + nodeC + `","hostname":"garage-c","isUp":true,"draining":false,"role":{"zone":"dc2","tags":["gw"],"capacity":null}},
			{"id":"` + nodeD + `","hostname":"garage-d","isUp":true,"draining":false,"role":null,"dataPartition":{"available":2,"total":2200000000000}}]}`,
		"PreviewClusterLayoutChanges": `{"message":["Partitions are replicated 3 times on at least 2 distinct zones.","dc3: 256 partitions moved in"],
			"statistics":{"replicationFactor":3,"effectiveZoneRedundancy":2,"partitionSize":1835008000,"lowPartitionSize":false,"usableCapacity":3000000000000,"totalCapacity":4000000000000,"effectiveCapacity":1000000000000,"lowUsableCapacity":true,"totalMovedPartitions":256,"zones":[]},
			"newLayout":{"version":13}}`,
		"UpdateClusterLayout": `{"version":12,"roles":[],"stagedRoleChanges":[]}`,
		"RevertClusterLayout": `{"version":12,"roles":[],"stagedRoleChanges":[]}`,
		"ApplyClusterLayout":  `{"message":["New layout applied."],"layout":{"version":13}}`,
	}
}

func TestReadGarageLayout(t *testing.T) {
	f := &fakeGarageLayoutAPI{t: t, answers: garageLayoutAnswers()}

	r, err := readGarageLayout(context.Background(), f.run, testGarageTarget)
	if err != nil {
		t.Fatal(err)
	}

	if r.Version != 12 || r.NextVersion != 13 || r.PartitionSize != 1835008000 || r.ZoneRedundancy != "maximum" || r.StagedZoneRedundancy != "2" || r.MinAck != 11 {
		t.Fatalf("header: %+v", r)
	}

	if r.Preview == nil || !r.Preview.OK || r.Preview.MovedPartitions != 256 || !r.Preview.LowUsableCapacity || len(r.Preview.Messages) != 2 || r.Preview.UsableCapacity != 3000000000000 {
		t.Fatalf("preview: %+v", r.Preview)
	}

	if len(r.Nodes) != 4 {
		t.Fatalf("nodes: %+v", r.Nodes)
	}

	// Down member first, then the members by zone, then the staged newcomer.
	a, b, c, d := r.Nodes[0], r.Nodes[1], r.Nodes[2], r.Nodes[3]
	if a.ID != nodeA || a.Up || !a.Known || a.Role == nil || a.Role.Capacity != 1000000000000 || a.Role.UsableCapacity != 939524096000 || a.DataTotal != 1100000000000 || a.Staged != garageStagedNone {
		t.Errorf("node A: %+v", a)
	}

	if b.ID != nodeB || b.Hostname != "garage-b" || b.Role == nil || b.Role.Zone != "dc2" || len(b.Role.Tags) != 0 {
		t.Errorf("node B: %+v", b)
	}

	if c.ID != nodeC || c.Role == nil || c.Role.Capacity != garageGatewayCapacity || c.Staged != garageStagedRemove || c.StagedRole != nil {
		t.Errorf("node C (gateway leaving): %+v", c)
	}

	if d.ID != nodeD || d.Role != nil || d.Staged != garageStagedAdd || d.StagedRole == nil || d.StagedRole.Zone != "dc3" || d.StagedRole.Capacity != 2000000000000 || d.DataTotal != 2200000000000 {
		t.Errorf("node D (joining): %+v", d)
	}

	if len(r.Versions) != 3 || r.Versions[0].Version != 12 || r.Versions[0].Status != garageLayoutCurrent || r.Versions[1].Status != garageLayoutDraining || r.Versions[2].Status != garageLayoutHistorical {
		t.Errorf("versions, newest first: %+v", r.Versions)
	}

	if len(r.Trackers) != 2 || r.Trackers[0].Hostname != "garage-a" || r.Trackers[0].Ack != 11 || !r.Stuck {
		t.Errorf("trackers (A down and behind: stuck): %+v stuck=%v", r.Trackers, r.Stuck)
	}

	if got := f.called("PreviewClusterLayoutChanges"); len(got) != 1 {
		t.Errorf("preview asked once: %v", f.calls)
	}
}

func TestReadGarageLayoutWithoutStaging(t *testing.T) {
	answers := garageLayoutAnswers()
	answers["GetClusterLayout"] = `{"version":12,"partitionSize":1,"roles":[{"id":"` + nodeA + `","zone":"dc1","tags":[],"capacity":1024}],"parameters":{"zoneRedundancy":{"atLeast":1}},"stagedParameters":{"zoneRedundancy":{"atLeast":1}},"stagedRoleChanges":[]}`
	answers["GetClusterLayoutHistory"] = `{"currentVersion":12,"minAck":12,"versions":[{"version":12,"status":"Current","storageNodes":1,"gatewayNodes":0}],"updateTrackers":null}`
	delete(answers, "PreviewClusterLayoutChanges")

	f := &fakeGarageLayoutAPI{t: t, answers: answers}

	r, err := readGarageLayout(context.Background(), f.run, testGarageTarget)
	if err != nil {
		t.Fatal(err)
	}

	if r.NextVersion != 0 || r.Preview != nil || r.ZoneRedundancy != "1" || r.StagedZoneRedundancy != "" || r.Stuck || len(r.Trackers) != 0 {
		t.Fatalf("nothing staged: %+v", r)
	}

	// Known nodes without a role are listed after the members: candidates to join.
	if len(r.Nodes) != 4 || r.Nodes[0].ID != nodeA || r.Nodes[3].Role != nil || r.Nodes[3].Staged != garageStagedNone {
		t.Fatalf("nodes: %+v", r.Nodes)
	}

	if got := f.called("PreviewClusterLayoutChanges"); len(got) != 0 {
		t.Errorf("preview asked with nothing staged: %v", got)
	}
}

func TestReadGarageLayoutSurvivesHistoryAndStatusErrors(t *testing.T) {
	f := &fakeGarageLayoutAPI{t: t, answers: garageLayoutAnswers(), fail: map[string]string{"GetClusterLayoutHistory": "no", "GetClusterStatus": "no"}}

	r, err := readGarageLayout(context.Background(), f.run, testGarageTarget)
	if err != nil {
		t.Fatal(err)
	}

	// Without the status, a role's node has no hostname: its tags name it.
	if len(r.Nodes) != 4 || r.Nodes[0].Hostname != "a" || r.Nodes[0].Known || r.Nodes[0].Up || len(r.Versions) != 0 {
		t.Fatalf("layout alone: %+v", r.Nodes)
	}

	f = &fakeGarageLayoutAPI{t: t, answers: garageLayoutAnswers(), fail: map[string]string{"GetClusterLayout": "Network error: Not connected"}}
	if _, err := readGarageLayout(context.Background(), f.run, testGarageTarget); err == nil || !strings.Contains(err.Error(), "Not connected") {
		t.Fatalf("layout error lost: %v", err)
	}
}

func TestGarageRoleChange(t *testing.T) {
	role, err := garageRoleChange(nodeD, " dc3 ", 2000000000000, false, `["d"," ","host-4"]`)
	if err != nil {
		t.Fatal(err)
	}

	// The zone arrives trimmed by the entry point; here it is kept as given.
	if role["id"] != nodeD || role["zone"] != " dc3 " || role["capacity"] != int64(2000000000000) || !slices.Equal(role["tags"].([]string), []string{"d", "host-4"}) {
		t.Fatalf("role: %+v", role)
	}

	gw, err := garageRoleChange(nodeD, "dc3", 0, true, "")
	if err != nil || gw["capacity"] != nil || len(gw["tags"].([]string)) != 0 {
		t.Fatalf("gateway: %+v %v", gw, err)
	}

	for name, bad := range map[string]func() error{
		"short id":       func() error { _, err := garageRoleChange(nodeD[:16], "dc3", 2048, false, ""); return err },
		"no zone":        func() error { _, err := garageRoleChange(nodeD, "", 2048, false, ""); return err },
		"tiny capacity":  func() error { _, err := garageRoleChange(nodeD, "dc3", 1023, false, ""); return err },
		"control in tag": func() error { _, err := garageRoleChange(nodeD, "dc3", 2048, false, `["a\nb"]`); return err },
		"tags not JSON":  func() error { _, err := garageRoleChange(nodeD, "dc3", 2048, false, `a,b`); return err },
	} {
		if bad() == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestGarageZoneRedundancy(t *testing.T) {
	for in, want := range map[string]string{"maximum": `"maximum"`, "max": `"maximum"`, "none": `"maximum"`, "3": `{"atLeast":3}`} {
		body, err := garageZoneRedundancyBody(in)
		if err != nil {
			t.Fatal(err)
		}

		if got, _ := json.Marshal(body); string(got) != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}

	for _, bad := range []string{"", "0", "-1", "two"} {
		if _, err := garageZoneRedundancyBody(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	if got := garageZoneRedundancy(&garageAPIParameters{ZoneRedundancy: json.RawMessage(`{"atLeast":2}`)}); got != "2" {
		t.Errorf("atLeast: %q", got)
	}

	if got := garageZoneRedundancy(nil); got != "" {
		t.Errorf("nil: %q", got)
	}
}

func TestUpdateGarageLayoutStagesThenReads(t *testing.T) {
	f := &fakeGarageLayoutAPI{t: t, answers: garageLayoutAnswers()}

	role, _ := garageRoleChange(nodeD, "dc3", 2000000000000, false, `["d"]`)

	r, err := updateGarageLayout(context.Background(), f.run, testGarageTarget, map[string]any{"roles": []any{role}})
	if err != nil {
		t.Fatal(err)
	}

	want := `UpdateClusterLayout {"roles":[{"capacity":2000000000000,"id":"` + nodeD + `","tags":["d"],"zone":"dc3"}]}`
	if got := f.called("UpdateClusterLayout"); !slices.Equal(got, []string{want}) {
		t.Errorf("staged: %v", got)
	}

	if r.NextVersion != 13 || r.Preview == nil {
		t.Errorf("report after staging: %+v", r)
	}

	// Garage's refusal (a bad request) reaches the caller as its own message.
	f = &fakeGarageLayoutAPI{t: t, answers: garageLayoutAnswers(), fail: map[string]string{"UpdateClusterLayout": "BadRequest (400): Capacity should be at least 1K (1024)"}}
	if _, err := updateGarageLayout(context.Background(), f.run, testGarageTarget, map[string]any{"roles": []any{role}}); err == nil || !strings.Contains(err.Error(), "at least 1K") {
		t.Fatalf("refusal lost: %v", err)
	}
}

func TestGarageAPIRetriesTruncatedReads(t *testing.T) {
	calls := 0
	run := func(_ context.Context, _, _, _ string, argv []string) ([]byte, []byte, error) {
		calls++
		if len(argv) != 3 {
			t.Fatalf("a unit request carries no body: %v", argv)
		}

		if calls == 1 {
			return []byte(`{"version":12,"roles":[`), nil, nil
		}

		return []byte(`{"version":12,"roles":[]}`), nil, nil
	}

	layout, err := garageAPI[garageAPILayout](context.Background(), run, testGarageTarget, "GetClusterLayout", nil)
	if err != nil || layout.Version != 12 || calls != 2 {
		t.Fatalf("got %+v, %v after %d calls", layout, err, calls)
	}

	// A write is never sent twice.
	calls = 0
	if _, err := garageAPI[json.RawMessage](context.Background(), run, testGarageTarget, "RevertClusterLayout", nil); err == nil || calls != 1 {
		t.Fatalf("write retried: %v after %d calls", err, calls)
	}
}

func TestKubeGarageLayoutDemoAndGuards(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeGarageLayout(cfg, "", "", "garage", "garage-4kq7z")
	if err != nil {
		t.Fatal(err)
	}

	var r garageLayoutReport
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Version != 12 || r.NextVersion != 13 || r.Preview == nil || len(r.Nodes) != 4 || !r.Stuck {
		t.Fatalf("demo layout: %v %+v", err, r)
	}

	// The demo's staged change is the UI's example; the demo accepts no write.
	for name, write := range map[string]func() (string, error){
		"stage": func() (string, error) {
			return KubeGarageStageRole(cfg, "", "", "garage", "garage-4kq7z", nodeD, "zone-4", 1<<40, false, "")
		},
		"remove":     func() (string, error) { return KubeGarageStageRemoval(cfg, "", "", "garage", "garage-4kq7z", nodeD) },
		"redundancy": func() (string, error) { return KubeGarageSetZoneRedundancy(cfg, "", "", "garage", "garage-4kq7z", "2") },
		"apply":      func() (string, error) { return KubeGarageApplyLayout(cfg, "", "", "garage", "garage-4kq7z", 13) },
		"revert":     func() (string, error) { return KubeGarageRevertLayout(cfg, "", "", "garage", "garage-4kq7z") },
	} {
		if _, err := write(); !errors.Is(err, errDemoUnavailable) {
			t.Errorf("%s on the demo: %v", name, err)
		}
	}

	// Bad parameters are refused before any cluster is reached.
	if _, err := KubeGarageStageRole(cfg, "", "", "garage", "garage-4kq7z", "short", "zone-4", 1<<40, false, ""); err == nil || !strings.Contains(err.Error(), "node ID") {
		t.Errorf("short node id: %v", err)
	}

	if _, err := KubeGarageApplyLayout(cfg, "", "", "garage", "garage-4kq7z", 0); err == nil {
		t.Error("version 0 accepted")
	}

	if _, err := KubeGarageLayout(cfg, "", "", "garage", "not a pod name"); err == nil {
		t.Error("bad pod name accepted")
	}
}

func TestSkipGarageDeadNodes(t *testing.T) {
	answers := garageLayoutAnswers()
	answers["ClusterLayoutSkipDeadNodes"] = `{"ackUpdated":["` + nodeA + `"],"syncUpdated":[]}`
	f := &fakeGarageLayoutAPI{t: t, answers: answers}

	r, err := skipGarageDeadNodes(context.Background(), f.run, testGarageTarget, 12, false)
	if err != nil {
		t.Fatal(err)
	}

	if got := f.called("ClusterLayoutSkipDeadNodes"); !slices.Equal(got, []string{`ClusterLayoutSkipDeadNodes {"allowMissingData":false,"version":12}`}) {
		t.Errorf("request: %v", got)
	}

	// The node is named by its hostname, and the layout is read back.
	if len(r.Messages) != 1 || r.Messages[0] != "Acknowledgement tracker moved to v12 for garage-a" || r.Version != 12 {
		t.Errorf("report: %v (version %d)", r.Messages, r.Version)
	}

	// Nothing moved: an error, one for each flag, as Garage's CLI answers.
	answers["ClusterLayoutSkipDeadNodes"] = `{"ackUpdated":[],"syncUpdated":[]}`
	f = &fakeGarageLayoutAPI{t: t, answers: answers}

	if _, err := skipGarageDeadNodes(context.Background(), f.run, testGarageTarget, 12, false); err == nil || !strings.Contains(err.Error(), "allowing missing data") {
		t.Errorf("nothing done without the flag: %v", err)
	}

	if _, err := skipGarageDeadNodes(context.Background(), f.run, testGarageTarget, 12, true); err == nil || !strings.Contains(err.Error(), "nothing to skip") {
		t.Errorf("nothing done with the flag: %v", err)
	}

	// Garage's refusal (one live version, a bad version number) reaches the caller.
	f = &fakeGarageLayoutAPI{t: t, answers: answers, fail: map[string]string{"ClusterLayoutSkipDeadNodes": "BadRequest (400): Invalid version, you may use the following version numbers: 12"}}
	if _, err := skipGarageDeadNodes(context.Background(), f.run, testGarageTarget, 3, true); err == nil || !strings.Contains(err.Error(), "Invalid version") {
		t.Errorf("refusal lost: %v", err)
	}

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeGarageSkipDeadNodes(cfg, "", "", "garage", "garage-4kq7z", 12, false); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	if _, err := KubeGarageSkipDeadNodes(cfg, "", "", "garage", "garage-4kq7z", 0, false); err == nil {
		t.Error("version 0 accepted")
	}
}
