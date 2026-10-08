package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
)

// Shaped like the RebalancePlans CAST AI's Karpenter controller writes: a full consolidation
// that timed out removing one node, then an empty-node one that retries that node.
const castAIPlansFixture = `{"items":[
  {"metadata":{"name":"consolidation-20260101100000","creationTimestamp":"2026-01-01T10:00:00Z",
     "labels":{"rebalancer.cast.ai/consolidation-mode":"full"}},
   "spec":{"execute":true,
     "nodeClaimsToAdd":[{"metadata":{"name":"cast-edge-0"}},{"metadata":{"name":"cast-edge-1"}}],
     "nodeClaimsToDelete":[{"nodeClaimName":"edge-aaaa"},{"nodeClaimName":"edge-bbbb"},{"nodeClaimName":"edge-cccc"}]},
   "status":{"state":"Failed","failureReason":"Timeout","failurePhase":"Deletion",
     "failureMessage":"rebalance timed out after 1h0m0s during Deletion phase, waiting for: edge-aaaa(InProgress)",
     "savings":{"currency":"USD","currentMonthlyCost":"318.35","projectedMonthlyCost":"177.54","projectedSavingsPercent":"44.23",
       "currentClusterMonthlyCost":"4841.00","currentClusterNodes":38,
       "greenNodes":[{"name":"cast-edge-0","instanceType":"c7g.2xlarge","isSpot":true,"priceHourly":"0.160800"}]},
     "achievedOutcome":{"nodes":[{"name":"cast-edge-0","instanceType":"c7g.2xlarge","isSpot":true,"priceHourly":"0.160800"}]},
     "nodesCreation":{"cast-edge-0":{"name":"cast-edge-0","attempts":[
       {"status":"InProgress","instanceType":"c7g.2xlarge","capacityType":"spot","zone":"eu-west-1a","timestamp":"2026-01-01T10:00:00Z"},
       {"status":"Success","instanceType":"c7g.2xlarge","capacityType":"spot","zone":"eu-west-1a","timestamp":"2026-01-01T10:00:30Z"}]}},
     "nodesDeletion":{
       "edge-aaaa":{"name":"edge-aaaa","events":[
         {"status":"NodeCordoned","description":"Node cordoned ahead of deletion","timestamp":"2026-01-01T10:00:30Z"},
         {"status":"Blocked","description":"NodePool disruption budget exhausted, waiting for budget to free up","timestamp":"2026-01-01T10:00:30Z"},
         {"status":"InProgress","description":"Starting deletion process","timestamp":"2026-01-01T10:13:00Z"},
         {"status":"NodeUncordoned","description":"Node uncordoned ahead of deletion","timestamp":"2026-01-01T11:00:00Z"}]},
       "edge-bbbb":{"name":"edge-bbbb","events":[
         {"status":"Success","description":"NodeClaim already deleted (not found in cluster)","timestamp":"2026-01-01T10:09:00Z"},
         {"status":"NodeCordoned","description":"Node cordoned ahead of deletion","timestamp":"2026-01-01T10:00:30Z"}]}},
     "nodePoolBudgets":[{"nodePoolName":"edge","allowedDisruptions":1,"disruptingCount":0,"remainingDisruptions":1,"totalNodes":5}],
     "conditions":[{"type":"Created","status":"True","lastTransitionTime":"2026-01-01T10:00:00Z"},
       {"type":"Fail","status":"True","reason":"Timeout","lastTransitionTime":"2026-01-01T11:00:00Z"}]}},
  {"metadata":{"name":"consolidation-20260101110100","creationTimestamp":"2026-01-01T11:01:00Z",
     "labels":{"rebalancer.cast.ai/consolidation-mode":"delete-empty"}},
   "spec":{"execute":true,"nodeClaimsToDelete":[{"nodeClaimName":"edge-aaaa"}]},
   "status":{"state":"Running",
     "savings":{"currency":"USD","currentMonthlyCost":"60.00","projectedMonthlyCost":"0.00","projectedSavingsPercent":"100.00"},
     "nodesDeletion":{"edge-aaaa":{"name":"edge-aaaa","events":[
       {"status":"InProgress","description":"Starting deletion process","timestamp":"2026-01-01T11:01:00Z"}]}},
     "conditions":[{"type":"InProgress","status":"True","lastTransitionTime":"2026-01-01T11:01:00Z"}]}},
  {"metadata":{"name":"consolidation-20260101090000","creationTimestamp":"2026-01-01T09:00:00Z",
     "labels":{"rebalancer.cast.ai/consolidation-mode":"full"}},
   "spec":{"execute":true},
   "status":{"state":"Done",
     "savings":{"currency":"USD","currentMonthlyCost":"128.19","projectedMonthlyCost":"76.36","projectedSavingsPercent":"40.43"},
     "achievedOutcome":{"diff":{"priceMonthly":"51.83","savingsPercentage":"40.43"}},
     "conditions":[{"type":"Done","status":"True","lastTransitionTime":"2026-01-01T09:02:00Z"}]}}]}`

func castAIPlansFromFixture(t *testing.T) []castAIPlan {
	t.Helper()

	var list kubeList[castAIPlanObject]
	if err := json.Unmarshal([]byte(castAIPlansFixture), &list); err != nil {
		t.Fatal(err)
	}

	return mapCastAIPlans(list.Items, nil)
}

func TestMapCastAIPlans(t *testing.T) {
	plans := castAIPlansFromFixture(t)

	var names []string
	for _, p := range plans {
		names = append(names, p.Name)
	}

	if want := []string{"consolidation-20260101110100", "consolidation-20260101100000", "consolidation-20260101090000"}; !slices.Equal(names, want) {
		t.Fatalf("newest first: %v", names)
	}

	failed := plans[1]
	if failed.Mode != "full" || failed.State != "Failed" || !failed.Execute || failed.FailureReason != "Timeout" || failed.FailurePhase != "Deletion" {
		t.Fatalf("failed plan: %+v", failed)
	}

	if failed.BeforeMonthly != 318.35 || failed.AfterMonthly != 177.54 || failed.SavingsPercent != 44.23 || failed.ClusterNodes != 38 || failed.ClusterMonthly != 4841 {
		t.Fatalf("failed plan costs: %+v", failed)
	}

	if failed.AchievedMonthly != nil {
		t.Errorf("no diff, no achieved saving: %v", *failed.AchievedMonthly)
	}

	if failed.CreatedAt != 1767261600000 || failed.EndedAt != 1767265200000 {
		t.Errorf("times: %d %d", failed.CreatedAt, failed.EndedAt)
	}

	// Failed first, then the one never reached, then the removed one.
	var removing []string
	for _, n := range failed.Removing {
		removing = append(removing, n.Name+":"+n.Status)
	}

	if want := []string{"edge-aaaa:failed", "edge-cccc:pending", "edge-bbbb:success"}; !slices.Equal(removing, want) {
		t.Fatalf("removing: %v", removing)
	}

	if ev := failed.Removing[0].Events; len(ev) != 4 || ev[1].Status != "Blocked" || ev[3].Status != "NodeUncordoned" {
		t.Fatalf("edge-aaaa events: %+v", ev)
	}

	if ev := failed.Removing[2].Events; ev[0].Status != "NodeCordoned" || ev[1].Status != "Success" {
		t.Errorf("events are sorted by time: %+v", ev)
	}

	if len(failed.Adding) != 2 {
		t.Fatalf("adding: %+v", failed.Adding)
	}

	if a := failed.Adding[1]; a.Name != "cast-edge-0" || a.Status != castAINodeSuccess || a.InstanceType != "c7g.2xlarge" || !a.Spot || a.Zone != "eu-west-1a" || a.PriceHourly != 0.1608 {
		t.Fatalf("created node: %+v", a)
	}

	if a := failed.Adding[0]; a.Name != "cast-edge-1" || a.Status != castAINodePending {
		t.Fatalf("node never created: %+v", a)
	}

	if b := failed.Budgets; len(b) != 1 || b[0] != (castAINodeBudget{NodePool: "edge", Allowed: 1, Nodes: 5}) {
		t.Fatalf("budgets: %+v", b)
	}

	if running := plans[0]; running.Removing[0].Status != castAINodeInProgress || running.EndedAt != 0 {
		t.Fatalf("running plan: %+v", running)
	}

	if done := plans[2]; done.AchievedMonthly == nil || *done.AchievedMonthly != 51.83 || done.Removing == nil || done.Adding == nil {
		t.Fatalf("done plan: %+v", done)
	}
}

func TestCastAIStuckNodes(t *testing.T) {
	stuck := castAIStuckNodes(castAIPlansFromFixture(t))

	if want := []castAIStuckNode{{Node: "edge-aaaa", Failures: 1, Retrying: true}}; !slices.Equal(stuck, want) {
		t.Fatalf("stuck: %+v", stuck)
	}

	// A failure no later plan comes back to is not stuck.
	plans := castAIPlansFromFixture(t)[1:]
	if stuck := castAIStuckNodes(plans); len(stuck) != 0 {
		t.Fatalf("not retried: %+v", stuck)
	}
}

func TestCastAIMissedCountsOnlyNodesLeft(t *testing.T) {
	removing := []castAIPlanNode{
		{Name: "claim-a", Status: castAINodeFailed},
		{Name: "claim-b", Status: castAINodeSuccess},
		{Name: "ip-c", Status: castAINodeSuccess},
	}
	blue := []castAIPlanVMRaw{
		{Name: "ip-a", InstanceType: "m7g.metal", IsSpot: true, PriceHourly: "0.802700"},
		{Name: "ip-b", PriceHourly: "0.094100"},
		{Name: "ip-c", PriceHourly: "0.078900"},
	}

	// Priced through its NodeClaim: only the node left counts, 0.8027 × 730.
	plan := castAIPlan{BeforeMonthly: 841.84, Removing: castAIPriceRemoving(removing, blue, map[string]string{"claim-a": "ip-a"})}
	if missed, est := castAIMissed(plan); missed != 585.97 || est {
		t.Fatalf("priced: %v %v", missed, est)
	}

	if n := plan.Removing[0]; n.InstanceType != "m7g.metal" || !n.Spot || n.PriceHourly != 0.8027 {
		t.Fatalf("priced node: %+v", n)
	}

	// Without the NodeClaims: a third of the planned saving, flagged as an estimate.
	plan.Removing = castAIPriceRemoving(removing, blue, nil)
	if missed, est := castAIMissed(plan); missed != 280.61 || !est {
		t.Fatalf("estimated: %v %v", missed, est)
	}

	// A node already gone is priced from the instance type in its claim name, when one matches.
	gone := []castAIPlanNode{{Name: "cast-spot-1791209079-0-m7g-m-ab75", Status: castAINodeFailed}}
	plan.Removing = castAIPriceRemoving(gone, blue, nil)
	if missed, est := castAIMissed(plan); missed != 585.97 || est {
		t.Fatalf("by type: %v %v", missed, est)
	}
}

func TestCastAIClaimHasType(t *testing.T) {
	for _, c := range []struct {
		claim, typ string
		want       bool
	}{
		{"cast-gateway-1791462078-0-c8gn-xlarge-eu-w-23c0", "c8gn.xlarge", true},
		{"cast-default-spot-arm64-1791209079-0-m7g-m-ab75", "m7g.metal", true},
		{"cast-default-spot-arm64-1791209079-0-m7g-m-ab75", "m7g.xlarge", false},
		{"default-spot-arm64-5vqtm", "c7g.xlarge", false},
	} {
		if got := castAIClaimHasType(c.claim, c.typ); got != c.want {
			t.Errorf("%s %s: %v", c.claim, c.typ, got)
		}
	}
}

func TestCastAIStuckNodeClearsOnceRemoved(t *testing.T) {
	plans := []castAIPlan{
		{State: "Done", Removing: []castAIPlanNode{{Name: "x", Status: castAINodeSuccess}}},
		{State: "Failed", Removing: []castAIPlanNode{{Name: "x", Status: castAINodeFailed}}},
		{State: "Failed", Removing: []castAIPlanNode{{Name: "x", Status: castAINodeFailed}}},
	}
	if stuck := castAIStuckNodes(plans); len(stuck) != 0 {
		t.Fatalf("removed at last: %+v", stuck)
	}

	if stuck := castAIStuckNodes(plans[1:]); len(stuck) != 1 || stuck[0].Failures != 2 || stuck[0].Retrying {
		t.Fatalf("failed twice: %+v", stuck)
	}
}

func TestCastAIDeletionStatusOnceOver(t *testing.T) {
	if got := castAIDeletionStatus("NodeUncordoned", false, true); got != castAINodeFailed {
		t.Errorf("given back in a finished plan: %s", got)
	}

	if got := castAIDeletionStatus("NodeUncordoned", false, false); got != castAINodeInProgress {
		t.Errorf("uncordoned while running: %s", got)
	}
}

func TestReadCastAIPlansForbiddenKeepsRecommendations(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"autoscaling.cast.ai","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/autoscaling.cast.ai/v1/recommendations": castAIFixture,
	})
	f.answerWith("GET /apis/autoscaling.cast.ai/v1alpha/rebalanceplans", 403, `{"kind":"Status","message":"rebalanceplans is forbidden","reason":"Forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, nil, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if c := res.CastAI; c.Error != "" || c.PlansError == "" || len(c.Recommendations) != 3 {
		t.Fatalf("castai: %+v", c)
	}
}

func TestCastAITotalsKeepAnUnrecommendedOriginal(t *testing.T) {
	rec := withCastAITotals(castAIRecommendation{Containers: []castAIContainer{{Name: "a", Memory: "256Mi", OriginalCPU: "500m", OriginalMemory: "512Mi"}}})
	if rec.CPUMilli != 500 || rec.OriginalCPUMilli != 500 || rec.CPUDeltaMilli != 0 || rec.MemoryBytes != 256<<20 || rec.OriginalMemoryBytes != 512<<20 {
		t.Fatalf("totals: %+v", rec)
	}
}

func TestCastAIPlansCapped(t *testing.T) {
	objects := make([]castAIPlanObject, castAIMaxPlans+5)
	for i := range objects {
		objects[i].Metadata.Name = "p"
	}

	if got := len(mapCastAIPlans(objects, nil)); got != castAIMaxPlans {
		t.Fatalf("plans: %d", got)
	}
}

func TestReadCastAIWithPlans(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"autoscaling.cast.ai","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/autoscaling.cast.ai/v1/recommendations":     castAIFixture,
		"GET /apis/autoscaling.cast.ai/v1alpha/rebalanceplans": castAIPlansFixture,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, nil, fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if c := res.CastAI; c == nil || c.Error != "" || len(c.Recommendations) != 3 || len(c.Plans) != 3 || len(c.Stuck) != 1 {
		t.Fatalf("castai: %+v", res.CastAI)
	}
}

func TestCastAIDemoHasPlans(t *testing.T) {
	c := demoDataServices(fixtureNow).CastAI

	var states []string
	for _, p := range c.Plans {
		states = append(states, p.State)
	}

	for _, want := range []string{"Running", "Failed", "Done"} {
		if !slices.Contains(states, want) {
			t.Errorf("demo has no %s plan: %v", want, states)
		}
	}

	if len(c.Stuck) != 1 || !c.Stuck[0].Retrying {
		t.Errorf("demo stuck node: %+v", c.Stuck)
	}
}
