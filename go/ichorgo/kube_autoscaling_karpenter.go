package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// karpenterPendingAfter is how long a NodeClaim may wait for its node before it reads pending.
const karpenterPendingAfter = 2 * time.Minute

// karpenterVersions are the karpenter.sh versions with NodePools and NodeClaims: v1 (1.0+)
// and v1beta1 (0.32 to 0.37), read through the same loose structs.
var karpenterVersions = map[string]bool{"v1": true, "v1beta1": true}

type nodePoolObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Weight     *int32        `json:"weight"`
		Limits     vpaQuantities `json:"limits"`
		Disruption struct {
			ConsolidationPolicy string `json:"consolidationPolicy"`
			ConsolidateAfter    string `json:"consolidateAfter"`
			Budgets             []struct {
				Nodes    string   `json:"nodes"`
				Schedule string   `json:"schedule"`
				Duration string   `json:"duration"`
				Reasons  []string `json:"reasons"`
			} `json:"budgets"`
		} `json:"disruption"`
		Template struct {
			Spec struct {
				Requirements []struct {
					Key      string   `json:"key"`
					Operator string   `json:"operator"`
					Values   []string `json:"values"`
				} `json:"requirements"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Resources  vpaQuantities  `json:"resources"`
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

type nodeClaimObject struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels"`
		CreationTimestamp string            `json:"creationTimestamp"`
		DeletionTimestamp string            `json:"deletionTimestamp"`
	} `json:"metadata"`
	Status struct {
		NodeName   string         `json:"nodeName"`
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

// readKarpenter lists the NodePools and NodeClaims; a failed list (often a 403 on
// nodeclaims) keeps the other one.
func readKarpenter(ctx context.Context, k *kubeClient, version string) ([]nodePoolObject, []nodeClaimObject, error) {
	if !karpenterVersions[version] {
		return nil, nil, fmt.Errorf("karpenter.sh %s has no NodePools: Ichor reads v1 and v1beta1", version)
	}

	var (
		pools             kubeList[nodePoolObject]
		claims            kubeList[nodeClaimObject]
		poolErr, claimErr error
		wg                sync.WaitGroup
	)

	base := "/apis/" + groupKarpenter + "/" + version
	wg.Go(func() { poolErr = getList(ctx, k, base+"/nodepools", &pools) })
	wg.Go(func() { claimErr = getList(ctx, k, base+"/nodeclaims", &claims) })
	wg.Wait()

	return pools.Items, claims.Items, firstErr(poolErr, claimErr)
}

// karpenterRows maps the pools and claims; a pool's node count is its claims.
func karpenterRows(pools []nodePoolObject, claims []nodeClaimObject, now time.Time) ([]nodePoolRow, []nodeClaimRow) {
	claimRows := make([]nodeClaimRow, 0, len(claims))
	perPool := map[string]int{}

	for _, c := range claims {
		row := nodeClaimRowOf(c, now)
		perPool[row.Pool]++
		claimRows = append(claimRows, row)
	}

	poolRows := make([]nodePoolRow, 0, len(pools))
	for _, p := range pools {
		poolRows = append(poolRows, nodePoolRowOf(p, perPool[p.Metadata.Name]))
	}

	return poolRows, claimRows
}

func nodePoolRowOf(p nodePoolObject, nodes int) nodePoolRow {
	row := nodePoolRow{
		Name:          p.Metadata.Name,
		Limits:        nodePoolLimits{CPU: p.Spec.Limits["cpu"], Memory: p.Spec.Limits["memory"]},
		Resources:     nodePoolResources{CPU: p.Status.Resources["cpu"], Memory: p.Status.Resources["memory"], Nodes: nodes},
		CapacityTypes: []string{},
		Disruption: nodePoolDisruption{
			ConsolidationPolicy: p.Spec.Disruption.ConsolidationPolicy,
			ConsolidateAfter:    p.Spec.Disruption.ConsolidateAfter,
			Budgets:             []nodePoolBudget{},
		},
	}
	if p.Spec.Weight != nil {
		row.Weight = int(*p.Spec.Weight)
	}

	for _, b := range p.Spec.Disruption.Budgets {
		reasons := b.Reasons
		if reasons == nil {
			reasons = []string{}
		}

		row.Disruption.Budgets = append(row.Disruption.Budgets, nodePoolBudget{Nodes: b.Nodes, Schedule: b.Schedule, Duration: b.Duration, Reasons: reasons})
	}

	for _, req := range p.Spec.Template.Spec.Requirements {
		if req.Key == karpenterCapacityLabel && req.Operator == "In" {
			row.CapacityTypes = append(row.CapacityTypes, req.Values...)
		}
	}

	row.AtLimit = atLimit(row.Resources.CPU, row.Limits.CPU) || atLimit(row.Resources.Memory, row.Limits.Memory)

	// v1beta1 pools write no conditions: no Ready condition is not a problem.
	cond := p.Status.Conditions.get("Ready")
	row.Ready = cond.Status != "False" && cond.Status != "Unknown"
	if !row.Ready {
		row.Message = cond.Message
	}

	return row
}

// atLimit tells whether used reached a set limit.
func atLimit(used, limit string) bool {
	l := parseQuantity(limit)

	return l > 0 && parseQuantity(used) >= l
}

func nodeClaimRowOf(c nodeClaimObject, now time.Time) nodeClaimRow {
	conds := c.Status.Conditions
	row := nodeClaimRow{
		Name:         c.Metadata.Name,
		Pool:         c.Metadata.Labels[karpenterPoolLabel],
		NodeName:     c.Status.NodeName,
		InstanceType: c.Metadata.Labels["node.kubernetes.io/instance-type"],
		CapacityType: c.Metadata.Labels[karpenterCapacityLabel],
		Launched:     conds.is("Launched"),
		Registered:   conds.is("Registered"),
		Initialized:  conds.is("Initialized"),
		Ready:        conds.is("Ready"),
		Created:      unixMilli(c.Metadata.CreationTimestamp),
		Drifted:      conds.is("Drifted"),
		Disrupting: c.Metadata.DeletionTimestamp != "" || conds.is("Disrupted") || conds.is("InstanceTerminating") ||
			conds.is("Disrupting") || conds.is("Terminating"), // the last two: pre-v1 names
	}

	row.Pending = row.NodeName == "" && row.Created > 0 && now.Sub(time.UnixMilli(row.Created)) > karpenterPendingAfter

	for _, typ := range []string{"Launched", "Registered", "Initialized", "Ready"} {
		if cond := conds.get(typ); cond.Status == "False" || cond.Status == "Unknown" {
			row.Message = cond.Message

			break
		}
	}

	return row
}

// nodePoolLess orders the pools: not ready, then at their limit, then by name.
func nodePoolLess(a, b nodePoolRow) int {
	return cmp.Or(
		cmp.Compare(notReady(a.Ready), notReady(b.Ready)),
		cmp.Compare(notReady(!a.AtLimit), notReady(!b.AtLimit)),
		strings.Compare(a.Name, b.Name),
	)
}

// nodeClaimLess orders the claims: pending, then not ready, then drifted or disrupting,
// then by name.
func nodeClaimLess(a, b nodeClaimRow) int {
	rank := func(c nodeClaimRow) int {
		switch {
		case c.Pending:
			return 0
		case !c.Ready:
			return 1
		case c.Drifted || c.Disrupting:
			return 2
		default:
			return 3
		}
	}

	return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a.Name, b.Name))
}
