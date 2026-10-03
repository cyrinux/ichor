package talosmobile

import (
	"reflect"
	"testing"
)

func nodeUp(node, hostname, role string) nodeOverview {
	return nodeOverview{Node: node, Hostname: hostname, Role: role, Reachable: true}
}

func nodeDown(node string) nodeOverview {
	return nodeOverview{Node: node, Hostname: node, Role: roleUnknown}
}

func withDataDir(t *testing.T) {
	t.Helper()
	SetDataDir(t.TempDir())
	t.Cleanup(func() { SetDataDir("") })
}

func TestRememberNodeNamesFillsDownNode(t *testing.T) {
	withDataDir(t)

	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.2", "cp-1", "controlplane"), nodeUp("10.0.0.3", "w-1", "worker")})

	nodes := []nodeOverview{nodeUp("10.0.0.2", "cp-1", "controlplane"), nodeDown("10.0.0.3")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[1].Hostname != "w-1" || nodes[1].Role != "worker" {
		t.Errorf("down node = %+v, want its last known names", nodes[1])
	}

	if nodes[1].Reachable {
		t.Error("filling names must not mark the node reachable")
	}
}

func TestRememberNodeNamesWithoutDataDir(t *testing.T) {
	SetDataDir("")

	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.3", "w-1", "worker")})

	nodes := []nodeOverview{nodeDown("10.0.0.3")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[0].Hostname != "10.0.0.3" {
		t.Errorf("hostname = %q, want the address: nothing is remembered without a data dir", nodes[0].Hostname)
	}
}

func TestRememberNodeNamesUnknownNode(t *testing.T) {
	withDataDir(t)

	nodes := []nodeOverview{nodeDown("10.0.0.9")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[0].Hostname != "10.0.0.9" || nodes[0].Role != roleUnknown {
		t.Errorf("never seen node = %+v, want it unchanged", nodes[0])
	}
}

func TestMergeKnownNodes(t *testing.T) {
	saved := knownNodes{
		"fp":      {"10.0.0.2": {Hostname: "cp-1", Role: "controlplane"}, "10.0.0.4": {Hostname: "old"}},
		"other":   {"10.1.0.2": {Hostname: "x"}},
		"removed": {"10.2.0.2": {Hostname: "y"}},
	}

	// 10.0.0.2 answered without its hostname; 10.0.0.4 left the talosconfig.
	nodes := []nodeOverview{
		{Node: "10.0.0.2", Hostname: "10.0.0.2", Role: "controlplane", Reachable: true},
		nodeUp("10.0.0.3", "w-1", "worker"),
	}

	got := mergeKnownNodes(saved, []string{"fp", "other"}, "fp", nodes)
	want := knownNodes{
		"fp":    {"10.0.0.2": {Hostname: "cp-1", Role: "controlplane"}, "10.0.0.3": {Hostname: "w-1", Role: "worker"}},
		"other": {"10.1.0.2": {Hostname: "x"}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMergeKnownNodesRenamedNode(t *testing.T) {
	saved := knownNodes{"fp": {"10.0.0.2": {Hostname: "old", Role: "worker"}}}

	got := mergeKnownNodes(saved, []string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.2", "new", "worker")})

	if got["fp"]["10.0.0.2"].Hostname != "new" {
		t.Errorf("hostname = %q, want the one the node says now", got["fp"]["10.0.0.2"].Hostname)
	}
}
