package ichorgo

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/kubespan"
)

const (
	meshNodeA, meshNodeB = "192.0.2.91", "192.0.2.92"
	meshHostA, meshHostB = "host-192-0-2-91", "host-192-0-2-92"
)

// meshPeer is a node's report of a peer it last shook hands with ago.
func meshPeer(label string, state kubespan.PeerState, ago time.Duration) *kubespan.PeerStatus {
	p := kubespan.NewPeerStatus(kubespan.NamespaceName, "pk-"+label)
	p.TypedSpec().Label, p.TypedSpec().State = label, state
	p.TypedSpec().LastHandshakeTime = time.Now().Add(-ago)

	return p
}

func TestKubeSpanMeshHealthBrokenLink(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, meshNodeA, "v1.14.0", machine.TypeControlPlane)
	f.addNode(t, meshNodeB, "v1.14.0", machine.TypeWorker)
	f.put(meshNodeA, meshPeer(meshHostB, kubespan.PeerStateDown, 10*time.Minute))
	f.put(meshNodeB, meshPeer(meshHostA, kubespan.PeerStateDown, 10*time.Minute))

	out, err := KubeSpanMeshHealth(f.start(t, meshNodeA, meshNodeB), "fake")
	got := decodeJSON[kubespanMeshHealth](t, out, err)

	if !got.Checked || got.Partial || got.Links != 1 || got.Broken != 1 {
		t.Fatalf("health = %+v", got)
	}

	value, ok := got.Issues[meshHostA+"|"+meshHostB]
	if !ok {
		t.Fatalf("issues = %v", got.Issues)
	}

	if !strings.HasPrefix(value, "critical|staleHandshake|") || !strings.HasSuffix(value, "|"+meshHostA+"|"+meshHostB) {
		t.Errorf("value = %q", value)
	}
}

func TestKubeSpanMeshHealthHealthyAndOff(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, meshNodeA, "v1.14.0", machine.TypeControlPlane)
	f.addNode(t, meshNodeB, "v1.14.0", machine.TypeWorker)
	cfg := f.start(t, meshNodeA, meshNodeB)

	// KubeSpan off: no node lists a peer, nothing is known.
	out, err := KubeSpanMeshHealth(cfg, "fake")
	if got := decodeJSON[kubespanMeshHealth](t, out, err); got.Checked || got.Broken != 0 || len(got.Issues) != 0 {
		t.Errorf("off = %+v", got)
	}

	f.put(meshNodeA, meshPeer(meshHostB, kubespan.PeerStateUp, time.Minute))
	f.put(meshNodeB, meshPeer(meshHostA, kubespan.PeerStateUp, time.Minute))

	out, err = KubeSpanMeshHealth(cfg, "fake")
	if got := decodeJSON[kubespanMeshHealth](t, out, err); !got.Checked || got.Links != 1 || got.Broken != 0 || len(got.Issues) != 0 {
		t.Errorf("healthy = %+v", got)
	}
}

// The down node's hostname cannot be read and no member list says who it is: its peers name it by
// hostname, so the link must not read as a broken tunnel between two known nodes.
func TestKubeSpanMeshHealthUnreachableEndIsNoIssue(t *testing.T) {
	const down = "192.0.2.93"

	f := newFakeTalos()
	f.addNode(t, meshNodeA, "v1.14.0", machine.TypeControlPlane)
	f.put(meshNodeA, meshPeer("host-192-0-2-93", kubespan.PeerStateDown, 10*time.Minute))

	out, err := KubeSpanMeshHealth(f.start(t, meshNodeA, down), "fake")
	got := decodeJSON[kubespanMeshHealth](t, out, err)

	if !got.Checked || !got.Partial || got.Broken != 0 || len(got.Issues) != 0 {
		t.Errorf("health = %+v", got)
	}
}

func TestKubeSpanMeshHealthDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeSpanMeshHealth(cfg, "")
	got := decodeJSON[kubespanMeshHealth](t, out, err)

	if !got.Checked || got.Partial || got.Broken != 2 || len(got.Issues) != 2 {
		t.Fatalf("demo = %+v", got)
	}

	for _, key := range slices.Collect(maps.Keys(got.Issues)) {
		if value := got.Issues[key]; fleetSeverity(value) != fleetCritical {
			t.Errorf("%s = %q", key, value)
		}
	}
}
