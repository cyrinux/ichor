package ichorgo

import "time"

// demoCastAIPlans is the demo cluster's node consolidations, newest first: one running that
// retries a node the failed one before it could not drain, and two that saved money.
func demoCastAIPlans(now time.Time) []castAIPlan {
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	ev := func(ago time.Duration, status, description string) castAIPlanEvent {
		return castAIPlanEvent{At: ms(ago), Status: status, Description: description}
	}
	saved := 54.75

	return []castAIPlan{
		{
			Name: "consolidation-demo-4", CreatedAt: ms(4 * time.Minute), Mode: "delete-empty", State: "Running", Execute: true,
			Currency: "USD", BeforeMonthly: 59.86, SavingsPercent: 100, ClusterMonthly: 1490, ClusterNodes: 9,
			Warnings: []string{},
			Removing: []castAIPlanNode{{Name: "edge-gw-7k2pq", Status: castAINodeInProgress, Events: []castAIPlanEvent{
				ev(4*time.Minute, "NodeCordoned", "Node cordoned ahead of deletion"),
				ev(4*time.Minute, "InProgress", "Starting deletion process"),
			}}},
			Adding:  []castAIPlanNode{},
			Budgets: []castAINodeBudget{{NodePool: "edge-gateway", Allowed: 1, Disrupting: 1, Nodes: 4}},
		},
		{
			Name: "consolidation-demo-3", CreatedAt: ms(65 * time.Minute), EndedAt: ms(5 * time.Minute), Mode: "full", State: "Failed", Execute: true,
			Currency: "USD", BeforeMonthly: 238.44, AfterMonthly: 117.53, SavingsPercent: 50.71, ClusterMonthly: 1550, ClusterNodes: 10,
			FailureReason: "Timeout", FailurePhase: "Deletion", MissedMonthly: 59.86,
			Message:  "rebalance timed out after 1h0m0s during Deletion phase, waiting for: edge-gw-7k2pq(InProgress)",
			Warnings: []string{},
			Removing: []castAIPlanNode{
				{Name: "edge-gw-7k2pq", Status: castAINodeFailed, InstanceType: "c8gn.xlarge", Spot: true, PriceHourly: 0.082, Events: []castAIPlanEvent{
					ev(64*time.Minute, "NodeCordoned", "Node cordoned ahead of deletion"),
					ev(64*time.Minute, "Blocked", "NodePool disruption budget exhausted, waiting for budget to free up"),
					ev(52*time.Minute, "InProgress", "Starting deletion process"),
					ev(5*time.Minute, "NodeUncordoned", "Node uncordoned ahead of deletion"),
				}},
				{Name: "edge-gw-m4x8d", Status: castAINodeSuccess, Events: []castAIPlanEvent{
					ev(64*time.Minute, "NodeCordoned", "Node cordoned ahead of deletion"),
					ev(56*time.Minute, "InProgress", "Starting deletion process"),
					ev(50*time.Minute, "Success", "NodeClaim already deleted (not found in cluster)"),
				}},
			},
			Adding: []castAIPlanNode{{Name: "cast-edge-gw-0", Status: castAINodeSuccess, InstanceType: "c7g.xlarge", Spot: true, Zone: "eu-west-1a", PriceHourly: 0.0804, Events: []castAIPlanEvent{
				ev(64*time.Minute, "InProgress", "Creating NodeClaim with instance type c7g.xlarge"),
				ev(63*time.Minute, "Success", "NodeClaim ready with instance type c7g.xlarge"),
			}}},
			Budgets: []castAINodeBudget{{NodePool: "edge-gateway", Allowed: 1, Disrupting: 0, Nodes: 5}},
		},
		{
			Name: "consolidation-demo-2", CreatedAt: ms(3 * time.Hour), EndedAt: ms(3*time.Hour - 3*time.Minute), Mode: "delete-empty", State: "Done", Execute: true,
			Currency: "USD", BeforeMonthly: 112.42, SavingsPercent: 100, ClusterMonthly: 1662, ClusterNodes: 12,
			Warnings: []string{},
			Removing: []castAIPlanNode{
				{Name: "general-spot-2xd9w", Status: castAINodeSuccess, Events: []castAIPlanEvent{ev(3*time.Hour-3*time.Minute, "Success", "NodeClaim already deleted (not found in cluster)")}},
				{Name: "general-spot-q8v4n", Status: castAINodeSuccess, Events: []castAIPlanEvent{ev(3*time.Hour-3*time.Minute, "Success", "NodeClaim already deleted (not found in cluster)")}},
			},
			Adding: []castAIPlanNode{}, Budgets: []castAINodeBudget{},
		},
		{
			Name: "consolidation-demo-1", CreatedAt: ms(5 * time.Hour), EndedAt: ms(5*time.Hour - 2*time.Minute), Mode: "full", State: "Done", Execute: true,
			Currency: "USD", BeforeMonthly: 131.4, AfterMonthly: 76.36, SavingsPercent: 41.89, AchievedMonthly: &saved, ClusterMonthly: 1717, ClusterNodes: 13,
			Warnings: []string{},
			Removing: []castAIPlanNode{
				{Name: "general-spot-h7c2m", Status: castAINodeSuccess, Events: []castAIPlanEvent{ev(5*time.Hour-2*time.Minute, "Success", "NodeClaim already deleted (not found in cluster)")}},
				{Name: "general-spot-w5r1k", Status: castAINodeSuccess, Events: []castAIPlanEvent{ev(5*time.Hour-2*time.Minute, "Success", "NodeClaim already deleted (not found in cluster)")}},
			},
			Adding: []castAIPlanNode{{Name: "cast-general-spot-0", Status: castAINodeSuccess, InstanceType: "r7g.xlarge", Spot: true, Zone: "eu-west-1c", PriceHourly: 0.1046, Events: []castAIPlanEvent{
				ev(5*time.Hour, "Success", "NodeClaim ready with instance type r7g.xlarge"),
			}}},
			Budgets: []castAINodeBudget{},
		},
	}
}
