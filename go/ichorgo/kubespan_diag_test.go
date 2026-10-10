package ichorgo

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/siderolink"
)

var diagNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func ep(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }

func verdictKinds(p kubespanDiagPeer) []string {
	var out []string
	for _, v := range p.Verdicts {
		out = append(out, v.Kind)
	}

	return out
}

func peerByLabel(t *testing.T, d kubespanDiag, label string) kubespanDiagPeer {
	t.Helper()

	for _, p := range d.Peers {
		if p.Label == label {
			return p
		}
	}

	t.Fatalf("no peer %s in %+v", label, d.Peers)

	return kubespanDiagPeer{}
}

func TestKubeSpanDiagVerdicts(t *testing.T) {
	in := kubespanDiagInput{
		node: "203.0.113.1", hostname: "cp-1", now: diagNow, linkMTU: 1420,
		config: &kubespan.ConfigSpec{Enabled: true, MTU: 1420, SharedSecret: "must-not-leak", EndpointFilters: []string{"!10.0.0.0/8"}},
		specs: []kubespanSpecInput{
			{id: "up", spec: kubespan.PeerSpecSpec{Label: "cp-2", Endpoints: []netip.AddrPort{ep("203.0.113.2:51820")}}},
			{id: "stale", spec: kubespan.PeerSpecSpec{Label: "cp-3", Endpoints: []netip.AddrPort{ep("203.0.113.3:51820"), ep("10.0.0.3:51820")}}},
			{id: "none", spec: kubespan.PeerSpecSpec{Label: "w-1"}},
			{id: "flap", spec: kubespan.PeerSpecSpec{Label: "w-2", Endpoints: []netip.AddrPort{ep("203.0.113.5:51820")}}},
			{id: "new", spec: kubespan.PeerSpecSpec{Label: "w-3", Endpoints: []netip.AddrPort{ep("203.0.113.6:51820")}}},
		},
		statuses: []kubespanPeerInput{
			{id: "up", spec: kubespan.PeerStatusSpec{Label: "cp-2", State: kubespan.PeerStateUp, LastHandshakeTime: diagNow.Add(-30 * time.Second)}},
			{id: "stale", spec: kubespan.PeerStatusSpec{Label: "cp-3", State: kubespan.PeerStateDown, LastHandshakeTime: diagNow.Add(-12 * time.Minute)}},
			{id: "none", spec: kubespan.PeerStatusSpec{Label: "w-1", State: kubespan.PeerStateDown}},
			{id: "flap", spec: kubespan.PeerStatusSpec{
				Label: "w-2", State: kubespan.PeerStateDown, LastHandshakeTime: diagNow.Add(-time.Minute),
				LastEndpointChange: diagNow.Add(-90 * time.Second), LastUsedEndpoint: ep("203.0.113.5:51820"),
			}},
		},
	}

	d := buildKubeSpanDiag(in)

	if d.Config == nil || d.Config.MTU != 1420 || d.LinkMTU != 1420 || d.Siderolink != nil {
		t.Fatalf("diag = %+v", d)
	}

	if got := verdictKinds(peerByLabel(t, d, "cp-2")); len(got) != 0 {
		t.Errorf("an up peer has no verdict: %v", got)
	}

	stale := peerByLabel(t, d, "cp-3")
	if !slices.Equal(verdictKinds(stale), []string{verdictStaleHandshake}) ||
		stale.Verdicts[0].Message != "handshake stale for 12 min (tried 203.0.113.3:51820, 10.0.0.3:51820)" {
		t.Errorf("stale = %+v", stale.Verdicts)
	}

	if got := verdictKinds(peerByLabel(t, d, "w-1")); !slices.Equal(got, []string{verdictNoEndpoint, verdictStaleHandshake}) {
		t.Errorf("no endpoint = %v", got)
	}

	flap := peerByLabel(t, d, "w-2")
	if !slices.Equal(verdictKinds(flap), []string{verdictFlapping}) || flap.LastUsedEndpoint != "203.0.113.5:51820" {
		t.Errorf("flap = %+v", flap)
	}

	if got := verdictKinds(peerByLabel(t, d, "w-3")); !slices.Equal(got, []string{verdictNoStatus}) {
		t.Errorf("spec without a status = %v", got)
	}
}

func TestKubeSpanDiagNeverReturnsTheSharedSecret(t *testing.T) {
	d := buildKubeSpanDiag(kubespanDiagInput{node: "n", now: diagNow, config: &kubespan.ConfigSpec{Enabled: true, SharedSecret: "must-not-leak"}})

	out, err := toJSON(d)
	if err != nil || strings.Contains(out, "must-not-leak") || strings.Contains(strings.ToLower(out), "secret") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestKubeSpanDiagAllComparesNodes(t *testing.T) {
	a := kubespanDiagInput{
		node: "203.0.113.1", hostname: "cp-1", now: diagNow,
		config: &kubespan.ConfigSpec{Enabled: true, MTU: 1420},
		specs:  []kubespanSpecInput{{id: "b", spec: kubespan.PeerSpecSpec{Label: "w-1"}}},
		statuses: []kubespanPeerInput{
			{id: "b", spec: kubespan.PeerStatusSpec{Label: "w-1", State: kubespan.PeerStateDown}},
		},
	}
	b := kubespanDiagInput{
		node: "203.0.113.8", hostname: "w-1", now: diagNow,
		config: &kubespan.ConfigSpec{Enabled: true, MTU: 1380, EndpointFilters: []string{"!0.0.0.0/0"}},
	}

	all := buildKubeSpanDiagAll([]kubespanDiagInput{a, b})

	got := verdictKinds(peerByLabel(t, all.Nodes[0], "w-1"))
	if !slices.Contains(got, verdictMTUMismatch) || !slices.Contains(got, verdictFiltered) {
		t.Fatalf("verdicts = %v", got)
	}

	msg := peerByLabel(t, all.Nodes[0], "w-1").Verdicts
	if !slices.ContainsFunc(msg, func(v kubespanVerdict) bool { return v.Message == "KubeSpan MTU 1420 here, 1380 on w-1" }) {
		t.Errorf("messages = %+v", msg)
	}
}

func TestKubeSpanDiagnosticsFake(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.71", "v1.11.0", machine.TypeControlPlane)

	cfg := kubespan.NewConfig("config", kubespan.ConfigID)
	cfg.TypedSpec().Enabled, cfg.TypedSpec().MTU, cfg.TypedSpec().SharedSecret = true, 1420, "must-not-leak"

	spec := kubespan.NewPeerSpec(kubespan.NamespaceName, "peer-key")
	spec.TypedSpec().Label = "host-192-0-2-72"

	status := kubespan.NewPeerStatus(kubespan.NamespaceName, "peer-key")
	status.TypedSpec().Label, status.TypedSpec().State = "host-192-0-2-72", kubespan.PeerStateDown

	link := network.NewLinkStatus(network.NamespaceName, kubespanLink)
	link.TypedSpec().MTU = 1420

	f.put("192.0.2.71", cfg, spec, status, link)

	cfgYAML := f.start(t, "192.0.2.71")

	out, err := KubeSpanDiagnostics(cfgYAML, "fake", "192.0.2.71")
	d := decodeJSON[kubespanDiag](t, out, err)

	if d.Hostname != "host-192-0-2-71" || d.Config == nil || !d.Config.Enabled || d.LinkMTU != 1420 || d.Siderolink != nil {
		t.Fatalf("diag = %s", out)
	}

	if strings.Contains(out, "must-not-leak") {
		t.Fatal("the shared secret leaked")
	}

	if got := verdictKinds(peerByLabel(t, d, "host-192-0-2-72")); !slices.Contains(got, verdictNoEndpoint) {
		t.Errorf("verdicts = %v", got)
	}

	// An Omni-managed node: the SideroLink row.
	sidero := siderolink.NewStatus()
	sidero.TypedSpec().Host, sidero.TypedSpec().Connected = "omni.example.invalid:8090", true
	f.put("192.0.2.71", sidero)

	out, err = KubeSpanDiagnostics(cfgYAML, "fake", "192.0.2.71")
	if d = decodeJSON[kubespanDiag](t, out, err); d.Siderolink == nil || !d.Siderolink.Connected || d.Siderolink.Host != "omni.example.invalid:8090" {
		t.Fatalf("siderolink = %s", out)
	}
}

func TestKubeSpanDiagnosticsDemo(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	worker := demoNodes()[4]

	out, err := KubeSpanDiagnostics(demo, "", worker.Node)
	d := decodeJSON[kubespanDiag](t, out, err)

	got := verdictKinds(peerByLabel(t, d, demoNodes()[2].Hostname))
	if !slices.Contains(got, verdictNoEndpoint) || !slices.Contains(got, verdictStaleHandshake) {
		t.Fatalf("demo verdicts = %v", got)
	}

	out, err = KubeSpanDiagnosticsAll(demo, "")
	all := decodeJSON[kubespanDiagAll](t, out, err)

	if len(all.Nodes) != 5 || all.Nodes[2].Siderolink == nil || all.Nodes[0].Siderolink != nil {
		t.Fatalf("all = %s", out)
	}
}

func TestRoundAgo(t *testing.T) {
	for d, want := range map[time.Duration]string{45 * time.Second: "45 s", 12 * time.Minute: "12 min", 3 * time.Hour: "3 h"} {
		if got := roundAgo(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
}
