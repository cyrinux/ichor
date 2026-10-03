package ichorgo

import (
	"encoding/json"
	"net/netip"
	"slices"
	"testing"
)

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, a := range s {
		out[i] = netip.MustParseAddr(a)
	}

	return out
}

func TestClassifyMembers(t *testing.T) {
	members := []clusterMember{
		{hostname: "worker-b", role: "worker", addresses: addrs("fe80::1", "10.0.0.12")},
		{hostname: "cp-1", role: "controlplane", addresses: addrs("10.0.0.1")},
		{hostname: "worker-a", role: "worker", addresses: addrs("2001:db8::11", "10.0.0.11")},
		// Behind the VIP endpoint: known by hostname.
		{hostname: "cp-2", role: "controlplane", addresses: addrs("10.0.0.2")},
	}

	got := classifyMembers([]string{"10.0.0.1", "10.0.0.100"}, []string{"cp-1", "CP-2"}, members)

	want := []discoveredNode{
		{Address: "10.0.0.1", Addresses: []string{"10.0.0.1"}, Hostname: "cp-1", Role: "controlplane", Known: true},
		{Address: "10.0.0.2", Addresses: []string{"10.0.0.2"}, Hostname: "cp-2", Role: "controlplane", Known: true},
		{Address: "10.0.0.11", Addresses: []string{"2001:db8::11", "10.0.0.11"}, Hostname: "worker-a", Role: "worker"},
		{Address: "10.0.0.12", Addresses: []string{"fe80::1", "10.0.0.12"}, Hostname: "worker-b", Role: "worker"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d nodes, want %d: %+v", len(got), len(want), got)
	}

	for i := range want {
		g, w := got[i], want[i]
		if g.Address != w.Address || g.Hostname != w.Hostname || g.Role != w.Role || g.Known != w.Known || !slices.Equal(g.Addresses, w.Addresses) {
			t.Errorf("node %d = %+v, want %+v", i, g, w)
		}
	}
}

func TestPreferredAddress(t *testing.T) {
	cases := map[string][]netip.Addr{
		"10.0.0.5":     addrs("fe80::1", "2001:db8::5", "10.0.0.5"),
		"2001:db8::5":  addrs("fe80::1", "2001:db8::5"),
		"fe80::1":      addrs("fe80::1"),
		"":             nil,
		"192.168.1.10": addrs("192.168.1.10", "10.0.0.5"),
	}

	for want, in := range cases {
		if got := preferredAddress(in); got != want {
			t.Errorf("preferredAddress(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAddContextNodes(t *testing.T) {
	// prod has nodes: they are kept, duplicates are skipped.
	out, err := AddContextNodes(mergeStored, "prod", "10.0.0.1, 10.0.0.3,node-4.example.com")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)
	if got, want := cfg.Contexts["prod"].Nodes, []string{"10.0.0.1", "10.0.0.3", "node-4.example.com"}; !slices.Equal(got, want) {
		t.Errorf("prod nodes = %v, want %v", got, want)
	}

	if got := cfg.Contexts["prod"].Endpoints; !slices.Equal(got, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Errorf("prod endpoints changed: %v", got)
	}

	if cfg.Contexts["prod"].Crt != "b2xkLWNydA==" || cfg.Context != "prod" {
		t.Error("credentials or current context changed")
	}

	if len(cfg.Contexts["lab"].Nodes) != 0 {
		t.Error("another context changed")
	}

	// lab has no nodes: its endpoint stays targeted.
	out, err = AddContextNodes(mergeStored, "lab", "10.1.0.2")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mustConfig(t, out).Contexts["lab"].Nodes, []string{"10.1.0.1", "10.1.0.2"}; !slices.Equal(got, want) {
		t.Errorf("lab nodes = %v, want %v", got, want)
	}
}

func TestAddContextNodesErrors(t *testing.T) {
	if _, err := AddContextNodes(mergeStored, "missing", "10.0.0.3"); err == nil {
		t.Error("missing context: want an error")
	}

	if _, err := AddContextNodes(mergeStored, "prod", "10.0.0.3; rm -rf"); err == nil {
		t.Error("invalid address: want an error")
	}

	if _, err := AddContextNodes("not: [yaml", "prod", "10.0.0.3"); err == nil {
		t.Error("invalid yaml: want an error")
	}
}

func TestDemoDiscovery(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := DiscoverNodes(cfg, "")
	if err != nil {
		t.Fatal(err)
	}

	var d nodeDiscovery
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}

	if len(d.Nodes) != len(demoNodes()) {
		t.Fatalf("got %d demo nodes", len(d.Nodes))
	}

	for _, n := range d.Nodes {
		if !n.Known {
			t.Errorf("demo node %s reported as new", n.Address)
		}
	}
}
