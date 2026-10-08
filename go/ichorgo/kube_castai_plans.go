package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strconv"
)

// castAIPlansVersion serves RebalancePlan, the record CAST AI's Karpenter controller (kentroller)
// writes for each node consolidation it runs: the nodes it adds and removes, the cost before and
// after, and how each node's creation or deletion went. Clusters without it answer 404.
const castAIPlansVersion = "v1alpha"

// castAILabelMode names the kind of consolidation: full, delete-empty or drain-only.
const castAILabelMode = "rebalancer.cast.ai/consolidation-mode"

// castAIMaxPlans bounds the plans sent to the app, newest first.
const castAIMaxPlans = 30

// Plan node outcomes, for the app to word.
const (
	castAINodePending    = "pending"    // listed in the plan, not started
	castAINodeInProgress = "inProgress" // cordoned, draining, deleting or creating
	castAINodeBlocked    = "blocked"    // waiting, e.g. on a NodePool disruption budget
	castAINodeSuccess    = "success"
	castAINodeFailed     = "failed" // failed, or given back (uncordoned) when the plan gave up
)

type castAIPlan struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"` // unix ms
	EndedAt   int64  `json:"endedAt"`   // when it reached Done or failed, 0 while it runs
	Mode      string `json:"mode"`      // full|delete-empty|drain-only|""
	// State as CAST AI writes it: Pending, Created, Running, Done, Failed, Skipped, Canceled, Expired.
	State   string `json:"state"`
	Execute bool   `json:"execute"` // auto-executed; false waits for an approval
	// Monthly cost of the nodes the plan touches, before and after, and the projected saving.
	Currency       string  `json:"currency"`
	BeforeMonthly  float64 `json:"beforeMonthly"`
	AfterMonthly   float64 `json:"afterMonthly"`
	SavingsPercent float64 `json:"savingsPercent"`
	// What the plan actually saved per month, when CAST AI measured it.
	AchievedMonthly *float64 `json:"achievedMonthly,omitempty"`
	ClusterMonthly  float64  `json:"clusterMonthly"`
	ClusterNodes    int      `json:"clusterNodes"`
	FailureReason   string   `json:"failureReason"` // e.g. Timeout
	FailurePhase    string   `json:"failurePhase"`  // Creation|Deletion
	// The failure or skip message, in CAST AI's words.
	Message  string             `json:"message"`
	Warnings []string           `json:"warnings"`
	Removing []castAIPlanNode   `json:"removing"`
	Adding   []castAIPlanNode   `json:"adding"`
	Budgets  []castAINodeBudget `json:"budgets"`
}

type castAIPlanNode struct {
	Name         string            `json:"name"`
	Status       string            `json:"status"` // see castAINode*
	InstanceType string            `json:"instanceType"`
	Spot         bool              `json:"spot"`
	Zone         string            `json:"zone"`
	PriceHourly  float64           `json:"priceHourly"`
	Events       []castAIPlanEvent `json:"events"`
}

type castAIPlanEvent struct {
	At          int64  `json:"at"` // unix ms
	Status      string `json:"status"`
	Description string `json:"description"`
}

type castAINodeBudget struct {
	NodePool   string `json:"nodePool"`
	Allowed    int    `json:"allowed"`
	Disrupting int    `json:"disrupting"`
	Nodes      int    `json:"nodes"`
}

// castAIStuckNode is a node a plan failed to remove that a later plan tried again.
type castAIStuckNode struct {
	Node     string `json:"node"`
	Failures int    `json:"failures"`
	Retrying bool   `json:"retrying"` // a running plan is on it now
}

type castAIPlanObject struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Execute         bool `json:"execute"`
		NodeClaimsToAdd []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"nodeClaimsToAdd"`
		NodeClaimsToDelete []struct {
			NodeClaimName string `json:"nodeClaimName"`
		} `json:"nodeClaimsToDelete"`
	} `json:"spec"`
	Status struct {
		State          string `json:"state"`
		FailureReason  string `json:"failureReason"`
		FailurePhase   string `json:"failurePhase"`
		FailureMessage string `json:"failureMessage"`
		SkippedMessage string `json:"skippedMessage"`
		Warnings       []struct {
			Description string `json:"description"`
		} `json:"warnings"`
		Savings struct {
			Currency                  string            `json:"currency"`
			CurrentMonthlyCost        string            `json:"currentMonthlyCost"`
			ProjectedMonthlyCost      string            `json:"projectedMonthlyCost"`
			ProjectedSavingsPercent   string            `json:"projectedSavingsPercent"`
			CurrentClusterMonthlyCost string            `json:"currentClusterMonthlyCost"`
			CurrentClusterNodes       int               `json:"currentClusterNodes"`
			GreenNodes                []castAIPlanVMRaw `json:"greenNodes"`
		} `json:"savings"`
		AchievedOutcome *struct {
			Nodes []castAIPlanVMRaw `json:"nodes"`
			Diff  *struct {
				PriceMonthly string `json:"priceMonthly"`
			} `json:"diff"`
		} `json:"achievedOutcome"`
		NodesCreation map[string]struct {
			Attempts []struct {
				Status       string `json:"status"`
				InstanceType string `json:"instanceType"`
				CapacityType string `json:"capacityType"`
				Zone         string `json:"zone"`
				Description  string `json:"description"`
				Timestamp    string `json:"timestamp"`
			} `json:"attempts"`
		} `json:"nodesCreation"`
		NodesDeletion map[string]struct {
			Events []struct {
				Status      string `json:"status"`
				Description string `json:"description"`
				Timestamp   string `json:"timestamp"`
			} `json:"events"`
		} `json:"nodesDeletion"`
		NodePoolBudgets []struct {
			NodePoolName       string `json:"nodePoolName"`
			AllowedDisruptions int    `json:"allowedDisruptions"`
			DisruptingCount    int    `json:"disruptingCount"`
			TotalNodes         int    `json:"totalNodes"`
		} `json:"nodePoolBudgets"`
		Conditions []struct {
			Type               string `json:"type"`
			Status             string `json:"status"`
			LastTransitionTime string `json:"lastTransitionTime"`
		} `json:"conditions"`
	} `json:"status"`
}

// castAIPlanVMRaw is a node of the plan's cost estimate.
type castAIPlanVMRaw struct {
	Name         string `json:"name"`
	InstanceType string `json:"instanceType"`
	IsSpot       bool   `json:"isSpot"`
	PriceHourly  string `json:"priceHourly"`
}

// readCastAIPlans lists the RebalancePlans; a cluster without them (no Karpenter controller) has none.
func readCastAIPlans(ctx context.Context, k *kubeClient) ([]castAIPlan, error) {
	var list kubeList[castAIPlanObject]

	err := ignoreNotFound(getList(ctx, k, "/apis/"+groupCastAI+"/"+castAIPlansVersion+"/rebalanceplans", &list))

	return mapCastAIPlans(list.Items), err
}

// mapCastAIPlans maps the newest castAIMaxPlans plans, newest first.
func mapCastAIPlans(objects []castAIPlanObject) []castAIPlan {
	plans := make([]castAIPlan, 0, len(objects))
	for _, obj := range objects {
		plans = append(plans, mapCastAIPlan(obj))
	}

	slices.SortFunc(plans, func(a, b castAIPlan) int {
		return cmp.Or(cmp.Compare(b.CreatedAt, a.CreatedAt), cmp.Compare(a.Name, b.Name))
	})

	return plans[:min(len(plans), castAIMaxPlans)]
}

func mapCastAIPlan(obj castAIPlanObject) castAIPlan {
	st := obj.Status
	plan := castAIPlan{
		Name: obj.Metadata.Name, CreatedAt: unixMilli(obj.Metadata.CreationTimestamp),
		Mode: obj.Metadata.Labels[castAILabelMode], State: st.State, Execute: obj.Spec.Execute,
		Currency:      st.Savings.Currency,
		BeforeMonthly: castMoney(st.Savings.CurrentMonthlyCost), AfterMonthly: castMoney(st.Savings.ProjectedMonthlyCost),
		SavingsPercent: castMoney(st.Savings.ProjectedSavingsPercent),
		ClusterMonthly: castMoney(st.Savings.CurrentClusterMonthlyCost), ClusterNodes: st.Savings.CurrentClusterNodes,
		FailureReason: st.FailureReason, FailurePhase: st.FailurePhase,
		Message:  cmp.Or(st.FailureMessage, st.SkippedMessage),
		Warnings: []string{}, Budgets: []castAINodeBudget{},
	}

	for _, w := range st.Warnings {
		plan.Warnings = append(plan.Warnings, w.Description)
	}

	for _, c := range st.Conditions {
		if (c.Type == "Done" || c.Type == "Fail") && c.Status == "True" {
			plan.EndedAt = unixMilli(c.LastTransitionTime)
		}
	}

	if a := st.AchievedOutcome; a != nil && a.Diff != nil && a.Diff.PriceMonthly != "" {
		saved := castMoney(a.Diff.PriceMonthly)
		plan.AchievedMonthly = &saved
	}

	for _, b := range st.NodePoolBudgets {
		plan.Budgets = append(plan.Budgets, castAINodeBudget{NodePool: b.NodePoolName, Allowed: b.AllowedDisruptions, Disrupting: b.DisruptingCount, Nodes: b.TotalNodes})
	}

	plan.Removing = castAIRemoving(obj, st.State == "Failed", plan.EndedAt > 0 || st.State == "Done")
	plan.Adding = castAIAdding(obj)

	return plan
}

// castAIRemoving: the nodes the plan deletes, with their events; one it never reached is pending.
func castAIRemoving(obj castAIPlanObject, failed, over bool) []castAIPlanNode {
	out := []castAIPlanNode{}
	seen := map[string]bool{}

	for name, d := range obj.Status.NodesDeletion {
		node := castAIPlanNode{Name: name, Status: castAINodePending, Events: []castAIPlanEvent{}}
		for _, e := range d.Events {
			node.Events = append(node.Events, castAIPlanEvent{At: unixMilli(e.Timestamp), Status: e.Status, Description: e.Description})
		}

		slices.SortStableFunc(node.Events, func(a, b castAIPlanEvent) int { return cmp.Compare(a.At, b.At) })

		if n := len(node.Events); n > 0 {
			node.Status = castAIDeletionStatus(node.Events[n-1].Status, failed, over)
		}

		seen[name] = true
		out = append(out, node)
	}

	for _, c := range obj.Spec.NodeClaimsToDelete {
		if !seen[c.NodeClaimName] {
			out = append(out, castAIPlanNode{Name: c.NodeClaimName, Status: castAINodePending, Events: []castAIPlanEvent{}})
		}
	}

	slices.SortFunc(out, byPlanNodeStatus)

	return out
}

// castAIDeletionStatus reads a node's last deletion event. A node uncordoned once its plan is
// over was given back: the plan could not remove it; one still moving in a failed plan failed.
func castAIDeletionStatus(last string, planFailed, planOver bool) string {
	switch last {
	case "Success":
		return castAINodeSuccess
	case "Failed":
		return castAINodeFailed
	case "Blocked":
		return castAINodeBlocked
	case "NodeUncordoned":
		if planFailed || planOver {
			return castAINodeFailed
		}

		return castAINodeInProgress
	default:
		if planFailed {
			return castAINodeFailed
		}

		return castAINodeInProgress
	}
}

// castAIAdding: the nodes the plan creates, from their latest attempt, priced from the estimate.
func castAIAdding(obj castAIPlanObject) []castAIPlanNode {
	st := obj.Status
	prices := map[string]castAIPlanVMRaw{}

	for _, n := range st.Savings.GreenNodes {
		prices[n.Name] = n
	}

	if st.AchievedOutcome != nil {
		for _, n := range st.AchievedOutcome.Nodes {
			prices[n.Name] = n
		}
	}

	out := []castAIPlanNode{}
	seen := map[string]bool{}

	for name, c := range st.NodesCreation {
		node := castAIPlanNode{Name: name, Status: castAINodePending, Events: []castAIPlanEvent{}}

		for _, a := range c.Attempts {
			node.Events = append(node.Events, castAIPlanEvent{At: unixMilli(a.Timestamp), Status: a.Status, Description: a.Description})
			node.InstanceType, node.Spot, node.Zone = a.InstanceType, a.CapacityType == "spot", a.Zone
		}

		slices.SortStableFunc(node.Events, func(a, b castAIPlanEvent) int { return cmp.Compare(a.At, b.At) })

		if n := len(node.Events); n > 0 {
			node.Status = castAICreationStatus(node.Events[n-1].Status)
		}

		if p, ok := prices[name]; ok {
			node.PriceHourly = castMoney(p.PriceHourly)
			node.InstanceType = cmp.Or(node.InstanceType, p.InstanceType)
			node.Spot = node.Spot || p.IsSpot
		}

		seen[name] = true
		out = append(out, node)
	}

	for _, c := range obj.Spec.NodeClaimsToAdd {
		if name := c.Metadata.Name; !seen[name] {
			p := prices[name]
			out = append(out, castAIPlanNode{Name: name, Status: castAINodePending, InstanceType: p.InstanceType, Spot: p.IsSpot, PriceHourly: castMoney(p.PriceHourly), Events: []castAIPlanEvent{}})
		}
	}

	slices.SortFunc(out, byPlanNodeStatus)

	return out
}

func castAICreationStatus(last string) string {
	switch last {
	case "Success":
		return castAINodeSuccess
	case "Failed":
		return castAINodeFailed
	default:
		return castAINodeInProgress
	}
}

// byPlanNodeStatus puts failed nodes first, then the ones still moving, then the done ones.
func byPlanNodeStatus(a, b castAIPlanNode) int {
	rank := map[string]int{castAINodeFailed: 0, castAINodeBlocked: 1, castAINodeInProgress: 2, castAINodePending: 3, castAINodeSuccess: 4}

	return cmp.Or(cmp.Compare(rank[a.Status], rank[b.Status]), cmp.Compare(a.Name, b.Name))
}

// castAIStuckNodes finds the nodes a plan failed to remove that a later plan lists again: the
// consolidation keeps retrying a node it cannot drain. plans is newest first.
func castAIStuckNodes(plans []castAIPlan) []castAIStuckNode {
	out := []castAIStuckNode{}
	failures := map[string]int{}
	retried := map[string]bool{}
	running := map[string]bool{}

	// Oldest first: a failure counts as retried when a later plan lists the node.
	for i := len(plans) - 1; i >= 0; i-- {
		p := plans[i]
		for _, n := range p.Removing {
			if n.Status == castAINodeSuccess {
				// Removed at last: no longer stuck.
				delete(failures, n.Name)
				delete(retried, n.Name)
				delete(running, n.Name)

				continue
			}

			if failures[n.Name] > 0 {
				retried[n.Name] = true
				running[n.Name] = p.State == "Running"
			}

			if n.Status == castAINodeFailed {
				failures[n.Name]++
			}
		}
	}

	for name, count := range failures {
		if retried[name] {
			out = append(out, castAIStuckNode{Node: name, Failures: count, Retrying: running[name]})
		}
	}

	slices.SortFunc(out, func(a, b castAIStuckNode) int {
		return cmp.Or(cmp.Compare(b.Failures, a.Failures), cmp.Compare(a.Node, b.Node))
	})

	return out
}

// castMoney parses CAST AI's decimal strings ("128.19"); 0 when empty or malformed.
func castMoney(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}

	return v
}
