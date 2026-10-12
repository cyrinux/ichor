package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

const gib = 1 << 30

func lossNode(t *testing.T, name string, cpu, memoryGi float64, pods int) checkNodeObject {
	t.Helper()

	var n checkNodeObject

	raw := fmt.Sprintf(`{"metadata":{"name":%q},"status":{"allocatable":{"cpu":"%g","memory":"%gGi","pods":"%d"},
		"conditions":[{"type":"Ready","status":"True"}]}}`, name, cpu, memoryGi, pods)
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}

	return n
}

// lossPod is a running pod of 0.1 CPU and memoryGi on node, owned by owner ("StatefulSet/db").
func lossPod(ns, name, node, owner, class string, priority int32, memoryGi float64) checkPod {
	return checkPod{
		kubePod: kubePod{Namespace: ns, Name: name, Node: node, Owner: owner}, Phase: "Running",
		CPU: 0.1, Memory: memoryGi * gib, PriorityClassName: class, Priority: priority, prioritySet: true,
		QoS: qosBurstable, Static: len(owner) > 5 && owner[:5] == "Node/",
	}
}

func lossClass(name string, value int32, policy string) priorityClassObject {
	c := priorityClassObject{Value: value, PreemptionPolicy: policy}
	c.Metadata.Name = name

	return c
}

func lossPDB(t *testing.T, ns, name, app, rule string) pdbObject {
	t.Helper()

	var b pdbObject

	raw := fmt.Sprintf(`{"metadata":{"name":%q,"namespace":%q},"spec":{"selector":{"matchLabels":{"app":%q}},%s}}`, name, ns, app, rule)
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}

	return b
}

func podNames[T any](items []T, name func(T) string) []string {
	out := []string{}
	for _, i := range items {
		out = append(out, name(i))
	}

	return out
}

func pendingNames(p nodeLossPlan) []string {
	return podNames(p.Pending, func(x nodeLossPending) string { return x.Name })
}

func suggestionNames(p nodeLossPlan) []string {
	return podNames(p.Suggestions, func(x nodeLossSuggestion) string { return fmt.Sprintf("%s/%s→%d", x.Kind, x.Name, x.ScaleTo) })
}

func TestSimulateNodeLossEverythingFits(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 8, 10)},
		pods:  []checkPod{lossPod("web", "app-0", "n1", "StatefulSet/app", "", 0, 1)},
	}

	plan := simulateNodeLoss(in, "n1")

	if len(plan.Placed) != 1 || plan.Placed[0].To != "n2" || plan.Placed[0].Workload != "StatefulSet/app" {
		t.Errorf("placed %+v", plan.Placed)
	}

	if len(plan.Preempted) != 0 || len(plan.Pending) != 0 || len(plan.Suggestions) != 0 || !plan.Covered {
		t.Errorf("plan %+v", plan)
	}

	if plan.PodsLost != 1 || plan.MemoryLost != gib || plan.WorstCase || len(plan.Candidates) != 0 || len(plan.Assumptions) == 0 {
		t.Errorf("summary %+v", plan)
	}
}

func TestSimulateNodeLossPreemptsTheLowestPod(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10), lossNode(t, "n3", 4, 4, 10)},
		pods: []checkPod{
			lossPod("web", "high-0", "n1", "StatefulSet/high", "high", 1000, 2),
			// n2: 1 GiB free, evicting the priority-10 pod is enough.
			lossPod("web", "low-0", "n2", "StatefulSet/low", "low", 10, 2),
			lossPod("web", "mid-0", "n2", "StatefulSet/mid", "mid", 500, 1),
			// n3: 1 GiB free, but only priority-500 pods to evict.
			lossPod("web", "mid-1", "n3", "StatefulSet/mid", "mid", 500, 2),
			lossPod("web", "mid-2", "n3", "StatefulSet/mid", "mid", 500, 1),
		},
		classes: []priorityClassObject{lossClass("high", 1000, ""), lossClass("mid", 500, ""), lossClass("low", 10, "")},
	}

	plan := simulateNodeLoss(in, "n1")

	if len(plan.Preempted) != 1 {
		t.Fatalf("preempted %+v", plan.Preempted)
	}

	if p := plan.Preempted[0]; p.Name != "low-0" || p.Node != "n2" || p.By != "web/high-0" || p.PriorityClassName != "low" {
		t.Errorf("preempted %+v", p)
	}

	// The evicted pod finds no room nor anything lower to evict.
	if got := pendingNames(plan); len(got) != 1 || got[0] != "low-0" || plan.Pending[0].Reason != pendingMemory {
		t.Errorf("pending %+v", plan.Pending)
	}

	// 2 GiB short: one 2 GiB replica of mid covers it; high, of a higher priority, comes later.
	if got := suggestionNames(plan); len(got) != 1 || got[0] != "StatefulSet/mid→2" || !plan.Covered {
		t.Errorf("suggestions %v covered %v", got, plan.Covered)
	}
}

func TestSimulateNodeLossNeverTouchesSystemClasses(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods: []checkPod{
			lossPod("kube-system", "agent-0", "n1", "StatefulSet/agent", "system-node-critical", 2000001000, 2),
			// Strictly lower, yet a system class: never evicted, never suggested.
			lossPod("kube-system", "coredns-0", "n2", "StatefulSet/coredns", "system-cluster-critical", 2000000000, 3),
		},
	}

	plan := simulateNodeLoss(in, "n1")

	if len(plan.Preempted) != 0 || len(plan.Suggestions) != 0 || plan.Covered {
		t.Errorf("plan %+v", plan)
	}

	if got := pendingNames(plan); len(got) != 1 || got[0] != "agent-0" {
		t.Errorf("pending %v", got)
	}
}

func TestSimulateNodeLossHonoursPDBs(t *testing.T) {
	for _, tc := range []struct {
		rule      string
		scaleTo   int
		minAvail  int
		pdbBlocks bool
	}{
		{rule: `"minAvailable":2`, scaleTo: 2, minAvail: 2},
		{rule: `"maxUnavailable":"34%"`, scaleTo: 1, minAvail: 1}, // 34 % of 3 rounds up to 2
		{rule: `"minAvailable":"100%"`, scaleTo: 3, minAvail: 3, pdbBlocks: true},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			web := func(name string) checkPod {
				p := lossPod("web", name, "n2", "StatefulSet/web", "", 0, 1)
				p.Labels = labelSet{"app": "web"}

				return p
			}
			in := capacityInput{
				nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
				pods:  []checkPod{lossPod("batch", "big-0", "n1", "StatefulSet/big", "", 0, 3), web("web-0"), web("web-1"), web("web-2")},
				pdbs:  []pdbObject{lossPDB(t, "web", "web", "web", tc.rule)},
			}

			plan := simulateNodeLoss(in, "n1")

			if len(plan.Suggestions) != 1 {
				t.Fatalf("suggestions %+v", plan.Suggestions)
			}

			s := plan.Suggestions[0]
			if s.Name != "web" || s.PDB != "web" || s.ScaleTo != tc.scaleTo || s.PDBMinAvailable != tc.minAvail || s.PDBBlocks != tc.pdbBlocks {
				t.Errorf("suggestion %+v", s)
			}

			// 2 GiB short (3 asked, 1 free), 1 GiB a replica: two replicas cover it.
			if s.Frees.Pods != 3-tc.scaleTo || plan.Covered != (tc.scaleTo == 1) {
				t.Errorf("frees %+v covered %v", s.Frees, plan.Covered)
			}
		})
	}
}

func TestSimulateNodeLossLeavesDaemonSetAndStaticPods(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods: []checkPod{
			lossPod("kube-system", "proxy-a", "n1", "DaemonSet/proxy", "", 0, 1),
			lossPod("kube-system", "etcd-n1", "n1", "Node/n1", "", 0, 1),
			lossPod("web", "app-0", "n1", "StatefulSet/app", "", 0, 3),
			lossPod("kube-system", "proxy-b", "n2", "DaemonSet/proxy", "", 0, 2),
		},
	}

	plan := simulateNodeLoss(in, "n1")

	if plan.DaemonSetPods != 2 || plan.PodsLost != 1 || plan.MemoryLost != 3*gib {
		t.Errorf("lost %+v", plan)
	}

	if got := pendingNames(plan); len(got) != 1 || got[0] != "app-0" {
		t.Errorf("pending %v", got)
	}

	if len(plan.Preempted) != 0 || len(plan.Suggestions) != 0 || plan.Covered {
		t.Errorf("preempted %+v suggestions %+v", plan.Preempted, plan.Suggestions)
	}
}

func TestSimulateNodeLossCascadingPreemptionEnds(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 3, 10), lossNode(t, "n3", 4, 2, 10)},
		pods: []checkPod{
			lossPod("web", "top-0", "n1", "StatefulSet/top", "", 300, 3),
			lossPod("web", "a-0", "n2", "StatefulSet/a", "", 200, 2),
			lossPod("web", "b-0", "n3", "StatefulSet/b", "", 100, 2),
		},
	}

	plan := simulateNodeLoss(in, "n1")

	got := podNames(plan.Preempted, func(p nodeLossPreempted) string { return p.Name + " by " + p.By + " on " + p.Node })
	if len(got) != 2 || got[0] != "a-0 by web/top-0 on n2" || got[1] != "b-0 by web/a-0 on n3" {
		t.Errorf("preempted %v", got)
	}

	if p := pendingNames(plan); len(p) != 1 || p[0] != "b-0" {
		t.Errorf("pending %v", p)
	}
}

func TestSimulateNodeLossWorstCase(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10), lossNode(t, "n3", 4, 4, 10)},
		pods: []checkPod{
			lossPod("web", "big-0", "n1", "StatefulSet/big", "", 0, 6),
			lossPod("web", "small-0", "n2", "StatefulSet/small", "", 0, 1),
			lossPod("web", "small-1", "n3", "StatefulSet/small", "", 0, 1),
		},
	}

	plan := simulateNodeLoss(in, "")

	if !plan.WorstCase || plan.Node != "n1" || len(plan.Pending) != 1 {
		t.Errorf("plan %+v", plan)
	}

	got := podNames(plan.Candidates, func(c nodeLossCandidate) string { return fmt.Sprintf("%s:%d", c.Name, c.Pending) })
	if len(got) != 3 || got[0] != "n1:1" || got[1] != "n2:0" || got[2] != "n3:0" {
		t.Errorf("candidates %v", got)
	}
}

func TestSimulateNodeLossPreemptionPolicyNever(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods: []checkPod{
			lossPod("batch", "job-0", "n1", "StatefulSet/job", "batch", 1000, 2),
			lossPod("web", "low-0", "n2", "StatefulSet/low", "", 0, 3),
		},
		classes: []priorityClassObject{lossClass("batch", 1000, "Never")},
	}

	plan := simulateNodeLoss(in, "n1")

	if len(plan.Preempted) != 0 {
		t.Errorf("preempted %+v", plan.Preempted)
	}

	if got := pendingNames(plan); len(got) != 1 || got[0] != "job-0" {
		t.Errorf("pending %v", got)
	}
}

func TestSimulateNodeLossReprievesWhatIsNotNeeded(t *testing.T) {
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods: []checkPod{
			lossPod("web", "high-0", "n1", "StatefulSet/high", "", 100, 3),
			// Evicting tiny-0 alone is not enough; once mid-0 goes, tiny-0 may stay.
			lossPod("web", "tiny-0", "n2", "StatefulSet/tiny", "", 1, 0.5),
			lossPod("web", "mid-0", "n2", "StatefulSet/mid", "", 50, 3.5),
		},
	}

	plan := simulateNodeLoss(in, "n1")

	if got := podNames(plan.Preempted, func(p nodeLossPreempted) string { return p.Name }); len(got) != 1 || got[0] != "mid-0" {
		t.Errorf("preempted %v", got)
	}
}

func TestSimulateNodeLossSpendsPDBsAcrossEvictions(t *testing.T) {
	web := func(name string) checkPod {
		p := lossPod("web", name, "n2", "StatefulSet/web", "", 0, 1)
		p.Labels = labelSet{"app": "web"}

		return p
	}
	pdb := lossPDB(t, "web", "web", "web", `"maxUnavailable":1`)
	pdb.Status.DisruptionsAllowed = 1
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 2, 10)},
		pods:  []checkPod{lossPod("db", "db-0", "n1", "StatefulSet/db", "", 100, 2), web("web-0"), web("web-1")},
		pdbs:  []pdbObject{pdb},
	}

	plan := simulateNodeLoss(in, "n1")

	got := podNames(plan.Preempted, func(p nodeLossPreempted) string { return fmt.Sprintf("%s:%v", p.Name, p.PDBViolated) })
	if len(got) != 2 || got[0] != "web-0:false" && got[0] != "web-1:false" || got[1] != "web-0:true" && got[1] != "web-1:true" {
		t.Errorf("preempted %v", got)
	}
}

func TestSimulateNodeLossShortfallIsWhatIsMissing(t *testing.T) {
	small := func(name string) checkPod {
		p := lossPod("web", name, "n2", "StatefulSet/w", "", 0, 0.9)
		p.CPU = 0.01

		return p
	}
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods:  []checkPod{lossPod("batch", "big-0", "n1", "StatefulSet/big", "", 0, 3), small("w-0"), small("w-1"), small("w-2"), small("w-3")},
	}

	plan := simulateNodeLoss(in, "n1")

	// n2 has CPU to spare: only memory is short (3 GiB asked, 0.4 free), three replicas cover it.
	if got := suggestionNames(plan); len(got) != 1 || got[0] != "StatefulSet/w→1" || !plan.Covered {
		t.Errorf("suggestions %v covered %v", got, plan.Covered)
	}
}

func TestSimulateNodeLossUnscheduledReplicasFreeNothing(t *testing.T) {
	unscheduled := lossPod("web", "w-1", "", "StatefulSet/w", "", 0, 3.5)
	unscheduled.Phase = "Pending"
	in := capacityInput{
		nodes: []checkNodeObject{lossNode(t, "n1", 4, 8, 10), lossNode(t, "n2", 4, 4, 10)},
		pods:  []checkPod{lossPod("batch", "big-0", "n1", "StatefulSet/big", "", 0, 3), lossPod("web", "w-0", "n2", "StatefulSet/w", "", 0, 3.5), unscheduled},
	}

	plan := simulateNodeLoss(in, "n1")

	if len(plan.Suggestions) != 1 {
		t.Fatalf("suggestions %+v", plan.Suggestions)
	}

	// The unscheduled replica goes first: scaling 2 → 0 frees the one that runs.
	if s := plan.Suggestions[0]; s.ScaleTo != 0 || s.Frees.Pods != 1 || s.Frees.Memory != 3.5*gib || !plan.Covered {
		t.Errorf("suggestion %+v covered %v", s, plan.Covered)
	}
}

func TestKubeCapacityNodeLossReadsTheCluster(t *testing.T) {
	in, err := readCapacityInput(context.Background(), openFakeKube(t, capacityFake(t)))
	if err != nil {
		t.Fatal(err)
	}

	plan := simulateNodeLoss(in, "w1")

	// db-0 (priority 1000) takes w2's free room first; web-a has nothing lower to evict.
	if len(plan.Placed) != 1 || plan.Placed[0].Name != "db-0" || plan.DaemonSetPods != 1 {
		t.Errorf("placed %+v", plan)
	}

	if got := pendingNames(plan); len(got) != 1 || got[0] != "web-5d8f-a" || plan.Pending[0].Workload != "Deployment/web" {
		t.Errorf("pending %+v", plan.Pending)
	}

	// The DaemonSet, the bare pod and the Pending Deployment are skipped: web-a asks 1 CPU and
	// 2 GiB, w2 keeps 0.5 and 1 GiB free, db's one replica frees the rest.
	if got := suggestionNames(plan); len(got) != 1 || got[0] != "StatefulSet/db→0" || !plan.Covered {
		t.Errorf("suggestions %v covered %v", got, plan.Covered)
	}
}

func TestKubeCapacityNodeLossRefusesABadNodeName(t *testing.T) {
	if _, err := KubeCapacityNodeLoss("", "", "", "../etc"); err == nil {
		t.Error("no error for ../etc")
	}
}

func TestDemoKubeNodeLoss(t *testing.T) {
	worst := demoKubeNodeLoss("")
	if !worst.WorstCase || worst.Node != "demo-worker-1" || len(worst.Candidates) != 2 {
		t.Errorf("worst %+v", worst)
	}

	plan := demoKubeNodeLoss("demo-worker-2")
	if plan.WorstCase || plan.Node != "demo-worker-2" || plan.Placed[0].To != "demo-worker-1" {
		t.Errorf("plan %+v", plan)
	}

	if len(plan.Preempted) != 1 || len(plan.Pending) != 2 || len(plan.Suggestions) != 3 || !plan.Suggestions[0].NoPriorityClass {
		t.Errorf("demo %+v", plan)
	}

	for _, s := range plan.Suggestions {
		if s.Priority >= systemPriority {
			t.Errorf("system suggestion %+v", s)
		}
	}
}
