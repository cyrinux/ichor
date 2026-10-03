package talosmobile

import (
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

func peerIn(label, state, endpoint string) kubespanPeerInput {
	spec := kubespan.PeerStatusSpec{Label: label}

	switch state {
	case "up":
		spec.State = kubespan.PeerStateUp
	case "down":
		spec.State = kubespan.PeerStateDown
	}

	if endpoint != "" {
		spec.Endpoint = netip.MustParseAddrPort(endpoint)
	}

	return kubespanPeerInput{id: "pk-" + label, spec: spec}
}

func topoMember(host, role string, addrs ...string) clusterMember {
	m := clusterMember{hostname: host, role: role}
	for _, a := range addrs {
		m.addresses = append(m.addresses, netip.MustParseAddr(a))
	}

	return m
}

func nodeByID(t *testing.T, topo clusterTopology, id string) topologyNode {
	t.Helper()

	for _, n := range topo.Nodes {
		if n.ID == id {
			return n
		}
	}

	t.Fatalf("no node %q in %+v", id, topo.Nodes)

	return topologyNode{}
}

func TestBuildTopologyLanAndRemoteSites(t *testing.T) {
	members := []clusterMember{
		topoMember("cp-1", "controlplane", "192.168.1.10"),
		topoMember("cp-2", "controlplane", "192.168.1.11"),
		topoMember("w-1", "worker", "192.168.1.20"),
		topoMember("vps", "worker", "203.0.113.5"),
	}
	obs := []topologyObservation{
		{node: "192.168.1.10", hostname: "cp-1", peers: []kubespanPeerInput{
			peerIn("cp-2", "up", "192.168.1.11:51820"),
			peerIn("w-1", "up", "192.168.1.20:51820"),
			peerIn("vps", "up", "203.0.113.5:51820"),
		}},
		{node: "203.0.113.5", hostname: "vps", peers: []kubespanPeerInput{
			peerIn("cp-1", "down", "198.51.100.1:51820"),
			peerIn("cp-2", "up", "198.51.100.1:51821"),
		}},
	}

	topo := buildTopology(members, obs)

	if len(topo.Nodes) != 4 || topo.Nodes[0].ID != "cp-1" || topo.Nodes[3].Role != "worker" {
		t.Fatalf("nodes = %+v", topo.Nodes)
	}

	if len(topo.Sites) != 2 {
		t.Fatalf("sites = %+v", topo.Sites)
	}

	lan, remote := topo.Sites[0], topo.Sites[1]
	if lan.Kind != "lan" || lan.Label != "192.168.1.0/24" || len(lan.Nodes) != 3 {
		t.Errorf("lan = %+v", lan)
	}

	if remote.Kind != "node" || len(remote.Nodes) != 1 || remote.Nodes[0] != "vps" || nodeByID(t, topo, "vps").Site != remote.ID {
		t.Errorf("remote = %+v", remote)
	}

	states := map[string]string{}
	for _, l := range topo.Links {
		states[l.A+"-"+l.B] = l.State
	}

	// cp-1 says up, vps says down: degraded. cp-2/vps only seen from vps.
	want := map[string]string{"cp-1-cp-2": "up", "cp-1-w-1": "up", "cp-1-vps": "degraded", "cp-2-vps": "up"}
	for k, v := range want {
		if states[k] != v {
			t.Errorf("link %s = %q, want %q (all %v)", k, states[k], v, states)
		}
	}

	if !nodeByID(t, topo, "cp-1").Queried || nodeByID(t, topo, "cp-2").Queried || !nodeByID(t, topo, "cp-1").KubeSpan {
		t.Errorf("queried/kubespan flags wrong: %+v", topo.Nodes)
	}
}

func TestBuildTopologyZonesWinAndKeepApart(t *testing.T) {
	members := []clusterMember{
		topoMember("a", "controlplane", "10.0.0.1"),
		topoMember("b", "controlplane", "10.0.0.2"),
		topoMember("c", "worker", "10.0.0.3"),
	}
	obs := []topologyObservation{
		{node: "10.0.0.1", hostname: "a", labels: map[string]string{zoneLabel: "fr-par-1"}},
		{node: "10.0.0.2", hostname: "b", labels: map[string]string{zoneLabel: "nl-ams-1"}},
		{node: "10.0.0.3", hostname: "c", platformZone: "zone-b", platformRegion: "eu-west-3"},
	}

	topo := buildTopology(members, obs)

	// Same /24 but different zones: three sites; c's zone comes from the platform metadata.
	if len(topo.Sites) != 3 {
		t.Fatalf("sites = %+v", topo.Sites)
	}

	if s := topo.Sites[0]; s.Kind != "zone" || s.Label != "fr-par-1" || s.Country != "FR" {
		t.Errorf("site a = %+v", s)
	}

	if s := topo.Sites[1]; s.Label != "nl-ams-1" || s.Country != "NL" {
		t.Errorf("site b = %+v", s)
	}

	if n := nodeByID(t, topo, "c"); n.Zone != "zone-b" || n.Region != "eu-west-3" || n.Country != "FR" {
		t.Errorf("c = %+v", n)
	}
}

func TestBuildTopologyUnzonedNodeJoinsItsLan(t *testing.T) {
	members := []clusterMember{topoMember("a", "controlplane", "10.1.0.1"), topoMember("b", "worker", "10.1.0.2")}
	obs := []topologyObservation{{node: "10.1.0.1", hostname: "a", labels: map[string]string{zoneLabel: "home-de"}}}

	topo := buildTopology(members, obs)

	if len(topo.Sites) != 1 || topo.Sites[0].Label != "home-de" || topo.Sites[0].Country != "DE" || len(topo.Sites[0].Nodes) != 2 {
		t.Errorf("sites = %+v", topo.Sites)
	}
}

func TestBuildTopologyWithoutDiscovery(t *testing.T) {
	// No members: nodes come from the targets and their peers' labels (FQDN matched to short).
	obs := []topologyObservation{
		{node: "10.2.0.1", hostname: "a.lan", peers: []kubespanPeerInput{peerIn("b", "up", "10.2.0.2:51820")}},
		{node: "10.2.0.9", err: errors.New("connection refused")},
	}

	topo := buildTopology(nil, obs)

	if len(topo.Nodes) != 3 {
		t.Fatalf("nodes = %+v", topo.Nodes)
	}

	if n := nodeByID(t, topo, "10.2.0.9"); n.Error == "" || !n.Queried {
		t.Errorf("unreachable target = %+v", n)
	}

	if len(topo.Links) != 1 || topo.Links[0].A != "a.lan" || topo.Links[0].B != "b" || !topo.Links[0].Sides[0].Private {
		t.Errorf("links = %+v", topo.Links)
	}

	if nodeByID(t, topo, "a.lan").Site != nodeByID(t, topo, "b").Site {
		t.Errorf("a private endpoint puts both ends on one site: %+v", topo.Sites)
	}
}

func TestBuildTopologyMatchesPeerLabelToFQDNMember(t *testing.T) {
	members := []clusterMember{topoMember("node-1.example.lan", "worker", "10.3.0.1"), topoMember("node-2", "worker", "10.3.0.2")}
	obs := []topologyObservation{{node: "10.3.0.2", hostname: "node-2", peers: []kubespanPeerInput{peerIn("node-1", "up", "")}}}

	topo := buildTopology(members, obs)

	if len(topo.Nodes) != 2 || len(topo.Links) != 1 || topo.Links[0].A != "node-1.example.lan" {
		t.Errorf("topology = %+v", topo)
	}
}

func TestBuildTopologyPeerMatchedByEndpointWhenLabelDiffers(t *testing.T) {
	members := []clusterMember{topoMember("alpha", "controlplane", "10.4.0.1"), topoMember("beta", "worker", "10.4.0.2")}
	nameless := peerIn("", "up", "10.9.9.9:51820")
	nameless.id = "pubkey-x"
	obs := []topologyObservation{{node: "10.4.0.1", hostname: "alpha", peers: []kubespanPeerInput{
		peerIn("beta-k8s-name", "up", "10.4.0.2:51820"),
		nameless,
	}}}

	topo := buildTopology(members, obs)

	if len(topo.Nodes) != 3 {
		t.Fatalf("want beta matched by endpoint plus one nameless peer: %+v", topo.Nodes)
	}

	if topo.Links[0].B != "beta" || nodeByID(t, topo, "pubkey-x").ID == "" {
		t.Errorf("links = %+v", topo.Links)
	}
}

func TestBuildTopologyTargetTwiceCountsOnce(t *testing.T) {
	members := []clusterMember{topoMember("a", "controlplane", "10.5.0.1"), topoMember("b", "worker", "10.5.0.2")}
	peers := []kubespanPeerInput{peerIn("b", "up", "10.5.0.2:51820")}
	obs := []topologyObservation{
		{node: "10.5.0.1", hostname: "a", peers: peers},
		{node: "a.example.lan", hostname: "a", peers: peers},
	}

	topo := buildTopology(members, obs)

	if len(topo.Links) != 1 || len(topo.Links[0].Sides) != 1 || nodeByID(t, topo, "a").Node != "10.5.0.1" {
		t.Errorf("duplicate target counted twice: %+v", topo)
	}
}

func TestBuildTopologyPublicLinkKeepsSameSubnetApart(t *testing.T) {
	// Two homes both on 192.168.1.0/24, reaching each other over their public addresses.
	members := []clusterMember{topoMember("home-a", "controlplane", "192.168.1.10"), topoMember("home-b", "worker", "192.168.1.10")}
	obs := []topologyObservation{{node: "192.168.1.10", hostname: "home-a", peers: []kubespanPeerInput{peerIn("home-b", "up", "203.0.113.9:51820")}}}

	topo := buildTopology(members, obs)

	if len(topo.Sites) != 2 {
		t.Errorf("public link must keep the sites apart: %+v", topo.Sites)
	}
}

func TestScreenshotModeHidesLocations(t *testing.T) {
	yaml := demoConfigForTest(t)

	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	out, err := ClusterTopology(yaml, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, leak := range []string{"fr-par-1", "nl-ams-1", "fsn1", `"country"`, `"region"`} {
		if strings.Contains(out, leak) {
			t.Errorf("screenshot mode shows %s: %s", leak, out)
		}
	}

	var topo clusterTopology
	if err := json.Unmarshal([]byte(out), &topo); err != nil {
		t.Fatal(err)
	}

	// Still three sites, now zone-1..3; the same zone gets the same fake.
	if len(topo.Sites) != 3 || topo.Sites[0].Label != "zone-1" || topo.Sites[2].Label != "zone-3" {
		t.Errorf("sites = %+v", topo.Sites)
	}

	if topo.Nodes[0].Zone != topo.Nodes[1].Zone {
		t.Errorf("one zone, two fakes: %+v", topo.Nodes)
	}
}

func TestDemoTopology(t *testing.T) {
	topo := demoTopology()

	if len(topo.Nodes) != 5 || len(topo.Links) != 10 || len(topo.Sites) != 3 {
		t.Fatalf("demo topology = %+v", topo)
	}

	countries := []string{topo.Sites[0].Country, topo.Sites[1].Country, topo.Sites[2].Country}
	if countries[0] != "FR" || countries[1] != "NL" || countries[2] != "DE" {
		t.Errorf("site countries = %v", countries)
	}

	degraded := 0

	for _, l := range topo.Links {
		if l.State == "degraded" {
			degraded++
		}
	}

	if degraded != 1 {
		t.Errorf("want one degraded link, got %d: %+v", degraded, topo.Links)
	}

	if ks := demoKubeSpan(); ks.Nodes[4].Down != 1 || ks.Nodes[0].Up != 4 {
		t.Errorf("demo KubeSpan = %+v", ks.Nodes)
	}
}
