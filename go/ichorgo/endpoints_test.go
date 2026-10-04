package ichorgo

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

func TestSetContextEndpoints(t *testing.T) {
	out, err := SetContextEndpoints(mergeStored, "prod", " 192.168.1.10, 192.168.1.11:50001,192.168.1.10, talos.lan ")
	if err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, out)
	if got, want := cfg.Contexts["prod"].Endpoints, []string{"192.168.1.10", "192.168.1.11:50001", "talos.lan"}; !slices.Equal(got, want) {
		t.Errorf("prod endpoints = %v, want %v", got, want)
	}

	if got := cfg.Contexts["prod"].Nodes; !slices.Equal(got, []string{"10.0.0.1"}) {
		t.Errorf("prod nodes changed: %v", got)
	}

	if cfg.Contexts["prod"].Crt != "b2xkLWNydA==" || cfg.Context != "prod" {
		t.Error("credentials or current context changed")
	}

	if got := cfg.Contexts["lab"].Endpoints; !slices.Equal(got, []string{"10.1.0.1"}) {
		t.Errorf("another context changed: %v", got)
	}
}

func TestSetContextEndpointsErrors(t *testing.T) {
	for name, endpoints := range map[string]string{
		"empty":   " , ",
		"invalid": "10.0.0.3; rm -rf",
		"port":    "10.0.0.3:99999",
	} {
		if _, err := SetContextEndpoints(mergeStored, "prod", endpoints); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}

	if _, err := SetContextEndpoints(mergeStored, "missing", "10.0.0.3"); err == nil {
		t.Error("missing context: want an error")
	}
}

func TestAddContextEndpoint(t *testing.T) {
	// prod has nodes: they are kept, the endpoint goes first.
	out, err := AddContextEndpoint(mergeStored, "prod", "192.168.1.10")
	if err != nil {
		t.Fatal(err)
	}

	prod := mustConfig(t, out).Contexts["prod"]
	if want := []string{"192.168.1.10", "10.0.0.1", "10.0.0.2"}; !slices.Equal(prod.Endpoints, want) {
		t.Errorf("prod endpoints = %v, want %v", prod.Endpoints, want)
	}

	if !slices.Equal(prod.Nodes, []string{"10.0.0.1"}) {
		t.Errorf("prod nodes changed: %v", prod.Nodes)
	}

	// An endpoint already listed moves first, once.
	out, err = AddContextEndpoint(mergeStored, "prod", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mustConfig(t, out).Contexts["prod"].Endpoints, []string{"10.0.0.2", "10.0.0.1"}; !slices.Equal(got, want) {
		t.Errorf("prod endpoints = %v, want %v", got, want)
	}

	// lab has no nodes: it targets the found member, not its unreachable endpoint.
	out, err = AddContextEndpoint(mergeStored, "lab", "192.168.1.20:50001")
	if err != nil {
		t.Fatal(err)
	}

	lab := mustConfig(t, out).Contexts["lab"]
	if want := []string{"192.168.1.20:50001", "10.1.0.1"}; !slices.Equal(lab.Endpoints, want) {
		t.Errorf("lab endpoints = %v, want %v", lab.Endpoints, want)
	}

	if want := []string{"192.168.1.20"}; !slices.Equal(lab.Nodes, want) {
		t.Errorf("lab nodes = %v, want %v", lab.Nodes, want)
	}

	if _, err := AddContextEndpoint(mergeStored, "prod", "not an endpoint"); err == nil {
		t.Error("invalid endpoint: want an error")
	}
}

func TestValidEndpoint(t *testing.T) {
	for _, ok := range []string{"10.0.0.1", "10.0.0.1:50000", "fd00::1", "[fd00::1]:50000", "node-1.lan", "node-1.lan:50000"} {
		if !validEndpoint(ok) {
			t.Errorf("%q: want valid", ok)
		}
	}

	for _, bad := range []string{"", "10.0.0.1:0", "10.0.0.1:x", "a b", "node;1", "[fd00::1]:70000"} {
		if validEndpoint(bad) {
			t.Errorf("%q: want invalid", bad)
		}
	}
}

func TestScanHosts(t *testing.T) {
	hosts, err := scanHosts("192.168.1.0/30, 192.168.1.1/32,10.0.0.7/31")
	if err != nil {
		t.Fatal(err)
	}

	want := []netip.Addr{
		netip.MustParseAddr("192.168.1.1"), netip.MustParseAddr("192.168.1.2"),
		netip.MustParseAddr("10.0.0.6"), netip.MustParseAddr("10.0.0.7"),
	}
	if !slices.Equal(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}

	if hosts, err := scanHosts("192.168.0.0/24"); err != nil || len(hosts) != 254 {
		t.Errorf("/24: %d hosts, %v", len(hosts), err)
	}

	if hosts, err := scanHosts("10.1.0.0/20"); err != nil || len(hosts) != 4094 {
		t.Errorf("/20: %d hosts, %v", len(hosts), err)
	}

	for _, bad := range []string{"10.0.0.0/8", "10.0.0.0/19", "fd00::/64", "nope", "10.0.0.0/20,10.1.0.0/24"} {
		if _, err := scanHosts(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestContextPorts(t *testing.T) {
	got := contextPorts(map[string]*clientconfig.Context{
		"a": {Endpoints: []string{"10.0.0.1", "10.0.0.2:50001"}},
		"b": {Endpoints: []string{"[fd00::1]:50001", "h:50000", "h:bad"}},
	})
	if want := []uint16{50000, 50001}; !slices.Equal(got, want) {
		t.Errorf("ports = %v, want %v", got, want)
	}
}

func TestScanPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			_ = c.Close()
		}
	}()

	open := netip.MustParseAddrPort(ln.Addr().String())

	// A second, closed port on the same host.
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	closedPort := netip.MustParseAddrPort(closed.Addr().String()).Port()
	_ = closed.Close()

	got := scanPorts(context.Background(), []netip.Addr{open.Addr()}, []uint16{closedPort, open.Port()}, time.Second)
	if !slices.Equal(got, []netip.AddrPort{open}) {
		t.Errorf("open = %v, want %v", got, open)
	}
}

func TestMatchEndpoints(t *testing.T) {
	contexts := map[string]*clientconfig.Context{
		"prod": {CA: "prod"},
		"lab":  {CA: "lab"},
		"home": {CA: "home"},
	}
	open := []netip.AddrPort{
		netip.MustParseAddrPort("192.168.1.10:50000"),
		netip.MustParseAddrPort("192.168.1.11:50000"),
		netip.MustParseAddrPort("192.168.1.12:50001"),
	}

	// .10 is a node of prod and lab (two contexts of one cluster), .11 another Talos, .12 of home.
	answers := map[string][]string{"192.168.1.10": {"prod", "lab"}, "192.168.1.12:50001": {"home"}}
	probe := func(_ context.Context, c *clientconfig.Context, endpoint string) (endpointProbe, error) {
		if slices.Contains(answers[endpoint], c.CA) {
			return endpointProbe{Endpoint: endpoint, Hostname: "node", Version: "v1.11.0"}, nil
		}

		return endpointProbe{}, errors.New("x509: certificate signed by unknown authority")
	}

	got := matchEndpoints(context.Background(), contexts, open, probe)
	want := []endpointMatch{
		{Endpoint: "192.168.1.10", Hostname: "node", Version: "v1.11.0", Contexts: []string{"lab", "prod"}},
		{Endpoint: "192.168.1.12:50001", Hostname: "node", Version: "v1.11.0", Contexts: []string{"home"}},
	}

	if !equalJSON(t, got, want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}
}

func TestPreferControlPlanes(t *testing.T) {
	got := preferControlPlanes([]endpointMatch{
		{Endpoint: "192.168.1.10", Role: "controlplane", Contexts: []string{"prod"}},
		{Endpoint: "192.168.1.11", Role: "worker", Contexts: []string{"lab", "prod"}},
		{Endpoint: "192.168.1.12", Role: "worker", Contexts: []string{"home"}},
	})
	want := []endpointMatch{
		{Endpoint: "192.168.1.10", Role: "controlplane", Contexts: []string{"prod"}},
		{Endpoint: "192.168.1.11", Role: "worker", Contexts: []string{"lab"}},
		{Endpoint: "192.168.1.12", Role: "worker", Contexts: []string{"home"}},
	}

	if !equalJSON(t, got, want) {
		t.Errorf("matches = %+v, want %+v", got, want)
	}
}

// A search that finds nothing answers [], which the apps decode as a list, never null.
func TestPreferControlPlanesNoMatchIsEmptyList(t *testing.T) {
	for _, matches := range [][]endpointMatch{nil, {{Endpoint: "192.168.1.10", Role: "worker"}}} {
		out, err := toJSON(preferControlPlanes(matches))
		if err != nil {
			t.Fatal(err)
		}

		if out != "[]" {
			t.Errorf("%+v: got %s, want []", matches, out)
		}
	}
}

func TestAddContextEndpointDefaultPort(t *testing.T) {
	out, err := AddContextEndpoint(mergeStored, "prod", "10.0.0.2:50000")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := mustConfig(t, out).Contexts["prod"].Endpoints, []string{"10.0.0.2:50000", "10.0.0.1"}; !slices.Equal(got, want) {
		t.Errorf("prod endpoints = %v, want %v", got, want)
	}
}

func TestScanHostsRejectsWholeSpace(t *testing.T) {
	for _, bad := range []string{"0.0.0.0/0", "128.0.0.0/1"} {
		if _, err := scanHosts(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestFindEndpointsRejectsLargeNetworks(t *testing.T) {
	if _, err := FindEndpoints(mergeStored, "10.0.0.0/8"); err == nil {
		t.Error("want an error")
	}
}

func TestProbeEndpointDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ProbeEndpoint(cfg, "", "192.168.1.10"); err == nil || err.Error() != demoUnavailable.Error() {
		t.Errorf("err = %v, want the demo error", err)
	}
}
