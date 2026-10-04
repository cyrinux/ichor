package ichorgo

import (
	"crypto/tls"
	"net/netip"
	"net/url"
	"slices"
	"testing"
)

func TestNormalizeEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		" 10.0.0.1 ":           "10.0.0.1",
		"10.0.0.1:50001":       "10.0.0.1:50001",
		"node.lan:50000":       "node.lan:50000",
		"fd00::1":              "fd00::1",
		"FD00:0:0::1":          "fd00::1",
		"[fd00::1]":            "fd00::1",
		"[FD00::1]:50001":      "[fd00::1]:50001",
		"fe80::1%eth0":         "fe80::1%eth0",
		"[fe80::1%eth0]:50000": "[fe80::1%eth0]:50000",
		"[10.0.0.1]:50000":     "10.0.0.1:50000",
		"[node.lan]":           "[node.lan]", // not an address: left for validEndpoint to refuse
	} {
		if got := normalizeEndpoint(in); got != want {
			t.Errorf("normalizeEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidEndpointIPv6(t *testing.T) {
	for _, ok := range []string{"[fd00::1]", "fe80::1%eth0", "[fe80::1%eth0]:50000", "::ffff:10.0.0.1"} {
		if !validEndpoint(ok) {
			t.Errorf("%q: want valid", ok)
		}
	}

	for _, bad := range []string{"[node.lan]", "[fd00::1", "fd00::1]", "[fd00::1]:", "[fd00::zz]"} {
		if validEndpoint(bad) {
			t.Errorf("%q: want invalid", bad)
		}
	}
}

func TestSameEndpointIPv6(t *testing.T) {
	for _, pair := range [][2]string{
		{"fd00::1", "[fd00::1]:50000"},
		{"fd00::1", "[FD00:0::1]"},
		{"[fd00::1]:50001", "[fd00:0::1]:50001"},
	} {
		if !sameEndpoint(pair[0], pair[1]) {
			t.Errorf("sameEndpoint(%q, %q) = false, want true", pair[0], pair[1])
		}
	}

	if sameEndpoint("fd00::1", "[fd00::1]:50001") {
		t.Error("different ports: want false")
	}

	if got := endpointHost("[fd00::1]"); got != "fd00::1" {
		t.Errorf("endpointHost([fd00::1]) = %q", got)
	}
}

func TestSetContextEndpointsIPv6(t *testing.T) {
	out, err := SetContextEndpoints(mergeStored, "prod", "[fd00::1], fd00::2,[FD00::3]:50001, fd00:0::2")
	if err != nil {
		t.Fatal(err)
	}

	// Stored as talosctl dials them: bare without a port, bracketed with one; duplicates once.
	if got, want := mustConfig(t, out).Contexts["prod"].Endpoints, []string{"fd00::1", "fd00::2", "[fd00::3]:50001"}; !slices.Equal(got, want) {
		t.Errorf("prod endpoints = %v, want %v", got, want)
	}
}

func TestAddContextEndpointIPv6(t *testing.T) {
	// lab has no nodes: the node it gets is the bare address, never bracketed.
	out, err := AddContextEndpoint(mergeStored, "lab", "[fd00::20]:50001")
	if err != nil {
		t.Fatal(err)
	}

	lab := mustConfig(t, out).Contexts["lab"]
	if want := []string{"fd00::20"}; !slices.Equal(lab.Nodes, want) {
		t.Errorf("lab nodes = %v, want %v", lab.Nodes, want)
	}

	// The same address written differently is listed once, first.
	out, err = AddContextEndpoint(out, "lab", "[FD00::20]:50001")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mustConfig(t, out).Contexts["lab"].Endpoints, []string{"[fd00::20]:50001", "10.1.0.1"}; !slices.Equal(got, want) {
		t.Errorf("lab endpoints = %v, want %v", got, want)
	}

	out, err = AddContextEndpoint(mergeStored, "lab", "[fd00::21]")
	if err != nil {
		t.Fatal(err)
	}

	if got := mustConfig(t, out).Contexts["lab"].Endpoints[0]; got != "fd00::21" {
		t.Errorf("first lab endpoint = %q, want fd00::21", got)
	}
}

func TestAddContextNodesIPv6(t *testing.T) {
	out, err := AddContextNodes(mergeStored, "prod", "[fd00::5], FD00::6, fd00::5")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mustConfig(t, out).Contexts["prod"].Nodes, []string{"10.0.0.1", "fd00::5", "fd00::6"}; !slices.Equal(got, want) {
		t.Errorf("prod nodes = %v, want %v", got, want)
	}
}

func TestClassifyMembersIPv6Targets(t *testing.T) {
	members := []clusterMember{{hostname: "cp-1", role: "controlplane", addresses: addrs("fd00::1")}}

	for _, target := range []string{"fd00::1", "FD00:0::1", "[fd00::1]", "[fd00::1]:50000"} {
		if got := classifyMembers([]string{target}, []string{""}, members); !got[0].Known {
			t.Errorf("target %q: member not known", target)
		}
	}
}

func TestScanHostsIPv6(t *testing.T) {
	hosts, err := scanHosts("fd00::/126, fd00::1/128")
	if err != nil {
		t.Fatal(err)
	}

	// No broadcast in IPv6: only the subnet-router anycast (the all-zero host) is skipped.
	want := []netip.Addr{netip.MustParseAddr("fd00::1"), netip.MustParseAddr("fd00::2"), netip.MustParseAddr("fd00::3")}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}

	if hosts, err := scanHosts("fd00:1::/120"); err != nil || len(hosts) != 255 {
		t.Errorf("/120: %d hosts, %v", len(hosts), err)
	}

	if hosts, err := scanHosts("fd00:1::1000/116"); err != nil || len(hosts) != 4096 {
		t.Errorf("/116 off the /64 start: %d hosts, %v", len(hosts), err)
	}

	if hosts, err := scanHosts("192.168.1.0/24,fd00::/120"); err != nil || len(hosts) != 254+255 {
		t.Errorf("mixed: %d hosts, %v", len(hosts), err)
	}

	for _, bad := range []string{"fd00::/64", "fd00::/115", "::/0", "fe80::/120%eth0"} {
		if _, err := scanHosts(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestFormatEndpointIPv6(t *testing.T) {
	if got := formatEndpoint(netip.MustParseAddrPort("[fd00::1]:50000")); got != "fd00::1" {
		t.Errorf("default port: %q", got)
	}

	if got := formatEndpoint(netip.MustParseAddrPort("[fd00::1]:50001")); got != "[fd00::1]:50001" {
		t.Errorf("other port: %q", got)
	}
}

func TestNormalizeKubeServerIPv6(t *testing.T) {
	for input, want := range map[string]string{
		"fd00::1":                "https://[fd00::1]",
		"FD00::1":                "https://[fd00::1]",
		"[fd00::1]":              "https://[fd00::1]",
		"https://[fd00::1]:6443": "https://[fd00::1]:6443",
		"https://fd00::1":        "https://[fd00::1]",
		"fd00::1/k8s/":           "https://[fd00::1]/k8s",
		"fe80::1%eth0":           "https://[fe80::1%25eth0]",
	} {
		got, err := NormalizeKubeServer(input)
		if err != nil || got != want {
			t.Errorf("NormalizeKubeServer(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
}

func TestApplyKubeServerIPv6KeepsPort(t *testing.T) {
	creds := &kubeCredentials{server: &url.URL{Scheme: "https", Host: "vip.lan:6443"}, tls: &tls.Config{}}

	if err := creds.applyKubeServer("fd00::1"); err != nil {
		t.Fatal(err)
	}

	if creds.server.Host != "[fd00::1]:6443" || creds.tls.ServerName != "vip.lan" {
		t.Fatalf("server %s, TLS name %q", creds.server, creds.tls.ServerName)
	}
}

func TestKubeServerCandidatesIPv6(t *testing.T) {
	server := &url.URL{Scheme: "https", Host: "vip.lan:6443"}

	var got []string
	for _, u := range kubeServerCandidates(server, []string{"fd00::1", "[fd00::2]:50000", "[fd00::3]"}) {
		got = append(got, u.Host)
	}

	if want := []string{"vip.lan:6443", "[fd00::1]:6443", "[fd00::2]:6443", "[fd00::3]:6443"}; !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want %v", got, want)
	}
}
