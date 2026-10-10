package ichorgo

import (
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
)

func TestClusterOverviewCordonFake(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.81", "v1.11.0", machine.TypeWorker)
	f.addNode(t, "192.0.2.82", "v1.11.0", machine.TypeWorker)

	status := k8s.NewNodeStatus(k8s.NamespaceName, "w-81")
	status.TypedSpec().Nodename, status.TypedSpec().NodeReady, status.TypedSpec().Unschedulable = "w-81", true, true
	f.put("192.0.2.81", status)

	cfg := f.start(t, "192.0.2.81", "192.0.2.82")

	out, err := ClusterOverview(cfg, "fake")
	overview := decodeJSON[clusterOverview](t, out, err)

	byNode := map[string]nodeOverview{}
	for _, n := range overview.Nodes {
		byNode[n.Node] = n
	}

	if n := byNode["192.0.2.81"]; !n.Cordoned || !n.CordonKnown {
		t.Errorf("cordoned node = %+v", n)
	}

	// No Kubernetes node status: unknown, never schedulable.
	if n := byNode["192.0.2.82"]; n.Cordoned || n.CordonKnown {
		t.Errorf("node without a status = %+v", n)
	}
}

func TestDemoHasOneCordonedNode(t *testing.T) {
	cordoned := 0

	for _, n := range demoNodes() {
		if n.Cordoned {
			cordoned++
		}
	}

	if cordoned != 1 {
		t.Errorf("cordoned demo nodes = %d", cordoned)
	}
}
