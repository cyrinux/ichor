package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

// The reason a pod stays Pending is the first resource no remaining node has: a slot, memory,
// ephemeral storage (on nodes that say theirs), then CPU.
func TestPendingReason(t *testing.T) {
	pod := simPod{checkPod: checkPod{CPU: 1, Memory: gib, Ephemeral: 6 * gib}}
	noDisk := simPod{checkPod: checkPod{CPU: 1, Memory: gib}}
	node := func(cpu, memoryGi, ephemeralGi float64, known bool, slots int) simNode {
		return simNode{cpu: cpu, memory: memoryGi * gib, ephemeral: ephemeralGi * gib, ephemeralKnown: known, slots: slots}
	}

	cases := []struct {
		name string
		room []simNode
		pod  simPod
		want string
	}{
		{"no node", nil, pod, pendingPods},
		{"no slot", []simNode{node(4, 8, 20, true, 0)}, pod, pendingPods},
		{"no memory", []simNode{node(4, 0.5, 20, true, 3)}, pod, pendingMemory},
		{"memory only where no slot", []simNode{node(4, 8, 20, true, 0), node(4, 0.5, 20, true, 3)}, pod, pendingMemory},
		{"no ephemeral storage", []simNode{node(4, 8, 4, true, 3), node(4, 8, 2, true, 3)}, pod, pendingEphemeral},
		{"ephemeral only where no memory", []simNode{node(4, 8, 4, true, 3), node(4, 0.5, 20, true, 3)}, pod, pendingEphemeral},
		// A node that does not say its ephemeral storage is no limit: never the reason.
		{"ephemeral unknown", []simNode{node(0.5, 8, 0, false, 3)}, pod, pendingCPU},
		{"one node unknown", []simNode{node(4, 8, 4, true, 3), node(0.5, 8, 0, false, 3)}, pod, pendingCPU},
		{"no ephemeral request", []simNode{node(0.5, 8, 0, true, 3)}, noDisk, pendingCPU},
		{"cpu", []simNode{node(0.5, 8, 20, true, 3)}, pod, pendingCPU},
	}

	for _, c := range cases {
		if got := pendingReason(c.room, c.pod); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// lossNodeWithDisk is lossNode with ephemeral-storage in its allocatable.
func lossNodeWithDisk(t *testing.T, name string, ephemeral string) checkNodeObject {
	t.Helper()

	raw, err := json.Marshal(lossNode(t, name, 4, 8, 10))
	if err != nil {
		t.Fatal(err)
	}

	patched := strings.Replace(string(raw), `"pods":"10"`, `"pods":"10","ephemeral-storage":"`+ephemeral+`"`, 1)

	var n checkNodeObject
	if err := json.Unmarshal([]byte(patched), &n); err != nil {
		t.Fatal(err)
	}

	return n
}

// A pod kept Pending by disk alone says so; a node with no ephemeral-storage in allocatable
// never refuses it for that.
func TestSimulateNodeLossPendingForEphemeralStorage(t *testing.T) {
	cache := lossPod("web", "cache-0", "n1", "StatefulSet/cache", "", 0, 1)
	cache.Ephemeral = 6 * gib

	lose := func(n2 checkNodeObject) nodeLossPlan {
		in := capacityInput{nodes: []checkNodeObject{lossNodeWithDisk(t, "n1", "20Gi"), n2}, pods: []checkPod{cache}}

		return simulateNodeLoss(in, []string{"n1"})
	}

	plan := lose(lossNodeWithDisk(t, "n2", "4Gi"))
	if len(plan.Pending) != 1 || plan.Pending[0].Reason != pendingEphemeral {
		t.Errorf("pending %+v", plan.Pending)
	}

	if plan = lose(lossNode(t, "n2", 4, 8, 10)); len(plan.Placed) != 1 || len(plan.Pending) != 0 {
		t.Errorf("unknown disk: placed %+v pending %+v", plan.Placed, plan.Pending)
	}
}
