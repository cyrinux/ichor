package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"go.yaml.in/yaml/v4"
)

// The fixtures follow BGPPeerStatusSpec's yaml tags (pkg/machinery/resources/network/
// bgp_peer_status.go): v1.14.7 has no gracefulRestart/peerRestarting, v1.15.0-beta.1 does.
const (
	bgpEstablished114YAML = `peer: 198.51.100.1
localASN: 64601
peerASN: 64512
state: ESTABLISHED
routerID: 198.51.100.21
since: 2026-11-20T08:30:00Z
received: 14
advertised: 2
accepted: 14
instance: fabric
`
	bgpIdle115YAML = `peer: 198.51.100.2
localASN: 64601
peerASN: 64513
state: ACTIVE
received: 0
advertised: 0
accepted: 0
instance: fabric
gracefulRestart: true
`
	bgpRestarting115YAML = `peer: link/eth1
localASN: 64601
peerASN: 64514
state: IDLE
received: 9
advertised: 2
accepted: 9
instance: edge
gracefulRestart: true
peerRestarting: true
`
)

// rawBGPResource is a BGPPeerStatus as a Talos 1.15 node sends it, kept raw: decoding it with
// this client's 1.14 type would drop the 1.15 fields (what the reader avoids).
func rawBGPResource(t *testing.T, id, specYAML string) *protobuf.Resource {
	t.Helper()

	r, err := protobuf.Unmarshal(&v1alpha1.Resource{
		Metadata: &v1alpha1.Metadata{Namespace: network.NamespaceName, Type: bgpPeerStatusType, Id: id, Version: "1", Phase: "running"},
		Spec:     &v1alpha1.Spec{YamlSpec: specYAML},
	})
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func bgpSpecOf(t *testing.T, y string) map[string]any {
	t.Helper()

	spec := map[string]any{}
	if err := yaml.Unmarshal([]byte(y), &spec); err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestMapBGPPeer(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bgpPeer
		down bool
	}{
		{"established (1.14 fields)", bgpEstablished114YAML, bgpPeer{
			Instance: "fabric", Peer: "198.51.100.1", PeerASN: 64512, LocalASN: 64601, State: "ESTABLISHED",
			Established: true, Since: "2026-11-20T08:30:00Z", Received: 14, Advertised: 2,
		}, false},
		{"idle, graceful restart negotiated", bgpIdle115YAML, bgpPeer{
			Instance: "fabric", Peer: "198.51.100.2", PeerASN: 64513, LocalASN: 64601, State: "ACTIVE", GracefulRestart: true,
		}, true},
		{"peer restarting under graceful restart", bgpRestarting115YAML, bgpPeer{
			Instance: "edge", Peer: "link/eth1", PeerASN: 64514, LocalASN: 64601, State: "IDLE", Received: 9, Advertised: 2,
			GracefulRestart: true, PeerRestarting: true,
		}, false},
		{"empty spec", "{}", bgpPeer{State: "UNKNOWN"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapBGPPeer(bgpSpecOf(t, tt.yaml))
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}

			if got.down() != tt.down {
				t.Errorf("down = %v, want %v", got.down(), tt.down)
			}
		})
	}
}

func TestMapBGPPeersSortsAndCaps(t *testing.T) {
	specs := []map[string]any{bgpSpecOf(t, bgpIdle115YAML), bgpSpecOf(t, bgpRestarting115YAML), bgpSpecOf(t, bgpEstablished114YAML)}

	got := mapBGPPeers(specs)
	if len(got) != 3 || got[0].Instance != "edge" || got[1].Peer != "198.51.100.1" || got[2].Peer != "198.51.100.2" {
		t.Errorf("order = %+v", got)
	}

	many := make([]map[string]any, bgpPeersMax+5)
	for i := range many {
		many[i] = map[string]any{"peer": "198.51.100.9", "state": "IDLE", "instance": strings.Repeat("x", 500)}
	}

	got = mapBGPPeers(many)
	if len(got) != bgpPeersMax {
		t.Errorf("%d peers, want %d", len(got), bgpPeersMax)
	}

	if n := len([]rune(got[0].Instance)); n != lldpTextMax {
		t.Errorf("instance clipped to %d runes, want %d", n, lldpTextMax)
	}
}

func TestBGPClusterDown(t *testing.T) {
	established := mapBGPPeer(bgpSpecOf(t, bgpEstablished114YAML))
	idle := mapBGPPeer(bgpSpecOf(t, bgpIdle115YAML))
	restarting := mapBGPPeer(bgpSpecOf(t, bgpRestarting115YAML))

	got := bgpClusterOf("lab", []bgpNodePeers{
		{Node: "192.0.2.21", Hostname: "w1", Supported: true, Peers: []bgpPeer{established, idle, restarting}},
		// Unreadable: says nothing about its sessions.
		{Node: "192.0.2.22", Hostname: "w2", Supported: true, Peers: []bgpPeer{idle}, Error: "unavailable"},
		{Node: "192.0.2.23", Hostname: "w3", Peers: []bgpPeer{}},
	})

	want := []bgpDown{{Node: "192.0.2.21", Hostname: "w1", Instance: "fabric", Peer: "198.51.100.2", State: "ACTIVE"}}
	if len(got.Down) != 1 || got.Down[0] != want[0] {
		t.Errorf("down = %+v, want %+v", got.Down, want)
	}
}

func TestBGPASNsMaskedInScreenshotMode(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	p := mapBGPPeer(bgpSpecOf(t, bgpEstablished114YAML))
	if p.PeerASN < bgpDocASNBase || p.PeerASN >= bgpDocASNBase+16 || p.LocalASN < bgpDocASNBase || p.LocalASN >= bgpDocASNBase+16 {
		t.Errorf("ASNs not masked: %+v", p)
	}

	if out := privacy.mask(`{"peer":"198.51.100.1"}`); strings.Contains(out, "198.51.100.1") {
		t.Errorf("peer address not masked: %s", out)
	}
}

func TestBGPJSONShape(t *testing.T) {
	b, err := json.Marshal(nodeNetwork{BGPPeers: demoBGPPeers(), BGPSupported: true})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		`"bgpSupported":true`, `"instance":"fabric"`, `"peer":"192.0.2.252"`, `"peerAsn":64512`, `"localAsn":64601`,
		`"state":"ESTABLISHED"`, `"established":true`, `"since":"`, `"received":12`, `"advertised":3`,
		`"gracefulRestart":true`, `"peerRestarting":false`,
	} {
		if !strings.Contains(string(b), key) {
			t.Errorf("%s missing from %s", key, b)
		}
	}
}

func TestDemoBGPPeers(t *testing.T) {
	cfg := demoConfigForTest(t)

	out, err := ClusterBGPPeers(cfg, "")
	got := decodeJSON[bgpCluster](t, out, err)

	peering := 0

	for _, n := range got.Nodes {
		if len(n.Peers) > 0 {
			peering++

			if n.Hostname != demoBGPHost || len(n.Peers) != 2 || !n.Peers[0].Established || !n.Peers[1].Established {
				t.Errorf("demo peers = %+v", n)
			}
		}
	}

	if peering != 1 || len(got.Down) != 0 {
		t.Errorf("demo cluster = %s", out)
	}
}

// TestClusterBGPPeersFake reads the sessions through the raw COSI list: the 1.15 fields of an
// untyped spec survive, a node without the type is unsupported and silent.
func TestClusterBGPPeersFake(t *testing.T) {
	const withBGP, without = "192.0.2.71", "192.0.2.72"

	f := newFakeTalos()
	f.addNode(t, withBGP, "v1.15.0", machine.TypeWorker)
	f.addNode(t, without, "v1.13.0", machine.TypeWorker)
	f.put(withBGP,
		resourceDefinition(t, network.NewBGPPeerStatus(network.NamespaceName, "x").ResourceDefinition()),
		rawBGPResource(t, "fabric/198.51.100.1", bgpEstablished114YAML),
		rawBGPResource(t, "fabric/198.51.100.2", bgpIdle115YAML),
		rawBGPResource(t, "edge/link/eth1", bgpRestarting115YAML),
	)

	cfg := f.start(t, withBGP, without)

	out, err := ClusterBGPPeers(cfg, "fake")
	got := decodeJSON[bgpCluster](t, out, err)

	if len(got.Nodes) != 2 {
		t.Fatalf("nodes = %s", out)
	}

	byNode := map[string]bgpNodePeers{}
	for _, n := range got.Nodes {
		byNode[n.Node] = n
	}

	w := byNode[withBGP]
	if !w.Supported || len(w.Peers) != 3 || w.Hostname != "host-192-0-2-71" || !w.Peers[0].PeerRestarting {
		t.Errorf("node with BGP = %+v", w)
	}

	if o := byNode[without]; o.Supported || len(o.Peers) != 0 || o.Error != "" {
		t.Errorf("node without BGP = %+v", o)
	}

	if len(got.Down) != 1 || got.Down[0].Peer != "198.51.100.2" || got.Down[0].Hostname != "host-192-0-2-71" {
		t.Errorf("down = %+v", got.Down)
	}

	out, err = NodeNetwork(cfg, "fake", withBGP)
	if n := decodeJSON[nodeNetwork](t, out, err); !n.BGPSupported || len(n.BGPPeers) != 3 || n.Errors["bgp"] != "" {
		t.Errorf("network = %s", out)
	}
}
