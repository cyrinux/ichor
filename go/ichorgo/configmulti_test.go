package ichorgo

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeMultiCluster is a few fakeConfigNodes; cp names the control planes.
type fakeMultiCluster struct {
	nodes     map[string]*fakeConfigNode
	cp        map[string]bool
	waited    []string
	quorumErr error
	quorums   int
}

func (c *fakeMultiCluster) applier(node string) configApplier { return c.nodes[node] }

func (c *fakeMultiCluster) describe(_ context.Context, node string) (string, bool) {
	return "host-" + node[strings.LastIndex(node, ".")+1:], c.cp[node]
}

func (c *fakeMultiCluster) waitBack(_ context.Context, node string, _ func(string, string)) error {
	c.waited = append(c.waited, node)

	return nil
}

func (c *fakeMultiCluster) quorum(context.Context) error {
	c.quorums++

	return c.quorumErr
}

// newFakeMultiCluster: every node holds a node label map (so the zone edit fits), except
// those listed in bare.
func newFakeMultiCluster(t *testing.T, nodes []string, cp []string, bare ...string) *fakeMultiCluster {
	t.Helper()

	c := &fakeMultiCluster{nodes: map[string]*fakeConfigNode{}, cp: map[string]bool{}}

	for _, n := range nodes {
		node := newFakeConfigNode(t)

		if !slices.Contains(bare, n) {
			base := node.base()
			if err := runConfigApply(context.Background(), node, configApply{
				base: base, draft: withLabel(t, base, "seed"), mode: applyModeAuto, emit: func(string, string) {},
			}); err != nil {
				t.Fatal(err)
			}

			node.calls = nil
		}

		c.nodes[n] = node
	}

	for _, n := range cp {
		c.cp[n] = true
	}

	return c
}

var zoneEdits = []configEdit{{Doc: 0, Path: []string{"machine", "nodeLabels"}, Op: configOpAdd, Key: "ichor.test/zone", Type: "string", Value: "b"}}

func TestReplayEdits(t *testing.T) {
	node := newFakeConfigNode(t)
	base := node.base()

	if _, err := replayEdits(base, zoneEdits); err == nil {
		t.Error("an edit under a missing map must fail")
	}

	withMap, err := replayEdits(base, []configEdit{
		{Doc: 0, Path: []string{"machine"}, Op: configOpAdd, Key: "nodeLabels", Type: "object"},
		zoneEdits[0],
	})
	if err != nil || !strings.Contains(withMap, "ichor.test/zone: b") {
		t.Errorf("replayed = %v\n%s", err, withMap)
	}

	if _, err := replayEdits(base, []configEdit{{Doc: 3, Path: []string{"machine"}, Op: configOpSet}}); err == nil {
		t.Error("a missing document must fail")
	}
}

func TestConfigMultiPreview(t *testing.T) {
	nodes := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	c := newFakeMultiCluster(t, nodes, nil, "10.0.0.2")
	c.nodes["10.0.0.3"].reboot = true

	p := previewMultiConfig(context.Background(), c, nodes, zoneEdits)

	if len(p.Nodes) != 3 || !p.AnyReboot {
		t.Fatalf("preview = %+v", p)
	}

	first := p.Nodes[0]
	if first.Hostname != "host-1" || !first.Changed || first.Error != "" || first.NeedsReboot {
		t.Errorf("node 1 = %+v", first)
	}

	added := false
	for _, l := range first.Lines {
		added = added || l.Kind == diffLineAdded && strings.Contains(l.Text, "ichor.test/zone: b")
	}

	if !added {
		t.Errorf("node 1's diff = %+v", first.Lines)
	}

	// The edit does not fit node 2: its own error, not the call's.
	if second := p.Nodes[1]; second.Error == "" || second.Changed {
		t.Errorf("node 2 = %+v", second)
	}

	if !p.Nodes[2].NeedsReboot {
		t.Errorf("node 3 = %+v", p.Nodes[2])
	}

	for _, n := range c.nodes {
		for _, call := range n.calls {
			if !call.dryRun {
				t.Fatal("a preview applied something")
			}
		}
	}
}

func TestConfigApplyMultiOrderSkipAndReboot(t *testing.T) {
	nodes := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}
	c := newFakeMultiCluster(t, nodes, []string{"10.0.0.1", "10.0.0.2"}, "10.0.0.4")

	var last multiConfigProgress

	order := []string{}

	err := runConfigApplyMulti(context.Background(), c, nodes, zoneEdits, applyModeReboot, func(p multiConfigProgress) {
		last = p
		if p.Phase == applyPhaseApplying && (len(order) == 0 || order[len(order)-1] != p.Node) {
			order = append(order, p.Node)
		}
	})
	if err != nil {
		t.Fatal(err)
	}

	// Workers first, control planes last, each in the given order.
	if want := []string{"10.0.0.3", "10.0.0.4", "10.0.0.1", "10.0.0.2"}; !slices.Equal(order, want) {
		t.Errorf("order = %v", order)
	}

	states := map[string]string{}
	for _, n := range last.Nodes {
		states[n.Node] = n.State
	}

	if states["10.0.0.4"] != multiStateSkipped || states["10.0.0.1"] != multiStateDone || states["10.0.0.3"] != multiStateDone {
		t.Errorf("states = %v", states)
	}

	// The skipped node got nothing; each node applied went down and came back; etcd was
	// checked before each control plane.
	if len(c.nodes["10.0.0.4"].calls) != 0 {
		t.Error("the skipped node was applied")
	}

	if want := []string{"10.0.0.3", "10.0.0.1", "10.0.0.2"}; !slices.Equal(c.waited, want) {
		t.Errorf("waited for %v", c.waited)
	}

	if c.quorums != 2 {
		t.Errorf("quorum checks = %d", c.quorums)
	}

	if last.Phase != applyPhaseDone || !strings.Contains(last.Message, "applied to: host-3, host-1, host-2; skipped: host-4") {
		t.Errorf("last = %+v", last)
	}
}

func TestConfigApplyMultiStopsOnFirstFailure(t *testing.T) {
	nodes := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	c := newFakeMultiCluster(t, nodes, nil)
	c.nodes["10.0.0.2"].failures = []error{errors.New("validation failed")}

	err := runConfigApplyMulti(context.Background(), c, nodes, zoneEdits, applyModeStaged, func(multiConfigProgress) {})
	if err == nil || !strings.Contains(err.Error(), "host-2: validation failed") || !strings.Contains(err.Error(), "applied to: host-1") {
		t.Fatalf("err = %v", err)
	}

	if len(c.nodes["10.0.0.3"].calls) != 0 {
		t.Error("the node after the failure was applied")
	}
}

func TestConfigApplyMultiQuorumStopsBeforeAControlPlane(t *testing.T) {
	nodes := []string{"10.0.0.1", "10.0.0.2"}
	c := newFakeMultiCluster(t, nodes, []string{"10.0.0.1"})
	c.quorumErr = errors.New("etcd on 10.0.0.9 is not healthy")

	err := runConfigApplyMulti(context.Background(), c, nodes, zoneEdits, applyModeReboot, func(multiConfigProgress) {})
	if err == nil || !strings.Contains(err.Error(), "not rebooting a control plane") || !strings.Contains(err.Error(), "applied to: host-2") {
		t.Fatalf("err = %v", err)
	}

	if len(c.nodes["10.0.0.1"].calls) != 0 {
		t.Error("the control plane was applied")
	}
}

func TestConfigApplyMultiUnchangedIsNotAFailure(t *testing.T) {
	nodes := []string{"10.0.0.1"}
	c := newFakeMultiCluster(t, nodes, nil)

	// The seeded label set again to the same value: nothing to change.
	same := []configEdit{{Doc: 0, Path: []string{"machine", "nodeLabels", "ichor.test/label"}, Op: configOpSet, Type: "string", Value: "seed"}}

	var last multiConfigProgress
	if err := runConfigApplyMulti(context.Background(), c, nodes, same, applyModeAuto, func(p multiConfigProgress) { last = p }); err != nil {
		t.Fatal(err)
	}

	if last.Nodes[0].State != multiStateUnchanged {
		t.Errorf("last = %+v", last)
	}
}

func TestConfigApplyMultiRefusals(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	edits := `[{"doc":0,"path":["machine","network","hostname"],"op":"set","type":"string","value":"renamed"}]`

	for _, tt := range []struct{ nodes, edits, mode, want string }{
		{"", edits, applyModeAuto, "no node"},
		{"10.5.0.2", `[]`, applyModeAuto, "no edit"},
		{"10.5.0.2", `{`, applyModeAuto, "invalid edits"},
		{"10.5.0.2", edits, "try", "one node at a time"},
		{"10.5.0.2", edits, "later", "unknown apply mode"},
		{"10.5.0.2", edits, applyModeAuto, errDemoUnavailable.Error()},
	} {
		err := startConfigApplyMulti(context.Background(), cfg, "", tt.nodes, tt.edits, tt.mode, func(multiConfigProgress) {})
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%q %q %q: %v", tt.nodes, tt.edits, tt.mode, err)
		}
	}
}

func TestConfigMultiPreviewDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	nodes := demoNodes()
	csv := nodes[0].Node + "," + nodes[3].Node
	edits := `[{"doc":0,"path":["machine","network","hostname"],"op":"set","type":"string","value":"renamed"}]`

	out, err := MachineConfigMultiPreview(cfg, "", csv, edits)
	p := decodeJSON[multiConfigPreview](t, out, err)

	if len(p.Nodes) != 2 || p.Nodes[1].Hostname != nodes[3].Hostname {
		t.Fatalf("preview = %+v", p)
	}

	for _, n := range p.Nodes {
		if n.Error != "" || !n.Changed {
			t.Errorf("%s: %+v", n.Node, n)
		}
	}
}
