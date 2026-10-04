package ichorgo

import (
	"cmp"
	"slices"
	"strings"
)

// argoNetHealth colours the graph from the inside out: pods by their status, nodes by their
// readiness, a Service by the pods it selects, and each box in front of it (route, Gateway,
// host) by the best of what it sends traffic to: it serves as long as one path works.
func argoNetHealth(g *argoNetGraph, in argoNetInput) {
	ready := map[string]string{}

	for _, n := range in.nodes {
		h := healthOK
		if n.Spec.Unschedulable {
			h = healthWarning
		}

		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status != "True" {
				h = healthCritical
			}
		}

		ready[n.Metadata.Name] = h
	}

	children := map[string][]string{}
	for _, e := range g.out.Edges {
		children[e.From] = append(children[e.From], e.To)
	}

	for i := range g.out.Nodes {
		if n := &g.out.Nodes[i]; n.Kind == "Node" {
			n.Health = cmp.Or(ready[n.Name], healthOK)
			if n.Health == healthCritical {
				n.Detail = "NotReady"
			} else if n.Health == healthWarning {
				n.Detail = "SchedulingDisabled"
			}
		}
	}

	// Services, then routes and Gateways, then hosts: each from the layer behind it.
	for _, layer := range []int{argoLayerService, argoLayerRoute, argoLayerGateway, argoLayerHost} {
		for i := range g.out.Nodes {
			n := &g.out.Nodes[i]
			if n.Layer != layer || n.Health != "" {
				continue
			}

			n.Health = argoNetFrontHealth(g, n, children[n.ID])
		}
	}

	for i := range g.out.Edges {
		if to := g.node(g.out.Edges[i].To); to != nil {
			g.out.Edges[i].Health = to.Health
		}
	}

	g.out.Problem = argoNetRootCause(g)
}

// argoNetFrontHealth: a Service is critical with no ready pod, warning with some not ready; a
// route, Gateway or host is as good as its best target, critical with none.
func argoNetFrontHealth(g *argoNetGraph, n *argoNetNode, targets []string) string {
	var hs []string

	for _, id := range targets {
		if t := g.node(id); t != nil && t.Kind != "Node" {
			hs = append(hs, t.Health)
		}
	}

	if len(hs) == 0 {
		return healthCritical
	}

	if n.Kind == "Service" {
		ok := 0

		for _, h := range hs {
			if h == healthOK || h == healthIdle {
				ok++
			}
		}

		switch {
		case ok == 0:
			return healthCritical
		case ok < len(hs):
			return healthWarning
		default:
			return healthOK
		}
	}

	best := hs[0]
	for _, h := range hs[1:] {
		if healthRank(h) > healthRank(best) {
			best = h
		}
	}

	if best == healthIdle {
		return healthOK
	}

	return best
}

// argoNetRootCause picks the deepest critical box, else the deepest warning one.
func argoNetRootCause(g *argoNetGraph) *argoNetProblem {
	for _, level := range []string{healthCritical, healthWarning} {
		var found *argoNetNode

		for i := range g.out.Nodes {
			n := &g.out.Nodes[i]
			if n.Health == level && n.Kind != "Host" && n.Kind != "LoadBalancer" && (found == nil || n.Layer > found.Layer) {
				found = n
			}
		}

		if found != nil {
			return &argoNetProblem{Kind: found.Kind, Namespace: found.Namespace, Name: found.Name, Detail: found.Detail}
		}
	}

	return nil
}

// podHealth: running and ready (or completed) is ok, waiting to start or not ready yet is a
// warning, crashing or failing is critical.
func podHealth(p kubePod) string {
	switch {
	case p.Status == "Completed":
		return healthIdle
	case p.Healthy:
		return healthOK
	case p.Status == "Running", p.Status == "Pending", p.Status == "ContainerCreating", p.Status == "PodInitializing",
		p.Status == "Terminating", strings.HasPrefix(p.Status, "Init:") && !strings.Contains(p.Status, "Err") && !strings.Contains(p.Status, "BackOff"):
		return healthWarning
	default:
		return healthCritical
	}
}

// argoNetSort orders each layer: worst first, then by name, so the trouble is on top.
func argoNetSort(g *argoNetGraph) {
	slices.SortStableFunc(g.out.Nodes, func(a, b argoNetNode) int {
		return cmp.Or(a.Layer-b.Layer, healthRank(a.Health)-healthRank(b.Health), strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name))
	})

	for i, n := range g.out.Nodes {
		g.index[n.ID] = i
	}

	slices.SortStableFunc(g.out.Edges, func(a, b argoNetEdge) int {
		return cmp.Or(g.index[a.From]-g.index[b.From], g.index[a.To]-g.index[b.To])
	})
}
