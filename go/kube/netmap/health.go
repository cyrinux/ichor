package netmap

import (
	"cmp"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// Finish colours the graph, names its likely root cause and orders it; nodes are the cluster's,
// for the readiness of the last column (none: every node box is ok).
func Finish(g *Graph, nodes []NodeObject) Map {
	health(g, nodes)
	sortMap(g)

	return g.out
}

// health colours the graph from the inside out: pods by their status, nodes by their
// readiness, a Service by the pods it selects, and each box in front of it (the caller's
// routes, Gateways, hosts) by the best of what it sends traffic to: it serves as long as one
// path works.
func health(g *Graph, nodes []NodeObject) {
	ready := map[string]string{}

	for _, n := range nodes {
		h := kube.HealthOK
		if n.Spec.Unschedulable {
			h = kube.HealthWarning
		}

		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status != "True" {
				h = kube.HealthCritical
			}
		}

		ready[n.Metadata.Name] = h
	}

	children := map[string][]string{}
	for _, e := range g.out.Edges {
		children[e.From] = append(children[e.From], e.To)
	}

	for i := range g.out.Nodes {
		if n := &g.out.Nodes[i]; n.Kind == KindNode {
			n.Health = cmp.Or(ready[n.Name], kube.HealthOK)
			if n.Health == kube.HealthCritical {
				n.Detail = "NotReady"
			} else if n.Health == kube.HealthWarning {
				n.Detail = "SchedulingDisabled"
			}
		}
	}

	// Services, then each of the caller's layers towards the entry points: each from the layer
	// behind it.
	for layer := g.ServiceLayer(); layer >= 0; layer-- {
		for i := range g.out.Nodes {
			n := &g.out.Nodes[i]
			if n.Layer != layer || n.Health != "" {
				continue
			}

			n.Health = frontHealth(g, n, children[n.ID])
		}
	}

	for i := range g.out.Edges {
		if to := g.Node(g.out.Edges[i].To); to != nil {
			g.out.Edges[i].Health = to.Health
		}
	}

	g.out.Problem = rootCause(g)
}

// frontHealth: a Service is critical with no ready pod, warning with some not ready; any box in
// front of it is as good as its best target, critical with none.
func frontHealth(g *Graph, n *Node, targets []string) string {
	var hs []string

	for _, id := range targets {
		if t := g.Node(id); t != nil && t.Kind != KindNode {
			hs = append(hs, t.Health)
		}
	}

	if len(hs) == 0 {
		return kube.HealthCritical
	}

	if n.Kind == KindService {
		ok := 0

		for _, h := range hs {
			if h == kube.HealthOK || h == kube.HealthIdle {
				ok++
			}
		}

		switch {
		case ok == 0:
			return kube.HealthCritical
		case ok < len(hs):
			return kube.HealthWarning
		default:
			return kube.HealthOK
		}
	}

	best := hs[0]
	for _, h := range hs[1:] {
		if kube.HealthRank(h) > kube.HealthRank(best) {
			best = h
		}
	}

	if best == kube.HealthIdle {
		return kube.HealthOK
	}

	return best
}

// rootCause picks the deepest critical box, else the deepest warning one. The entry points
// (layer 0, when the caller has layers of its own) only show where traffic comes in.
func rootCause(g *Graph) *Problem {
	for _, level := range []string{kube.HealthCritical, kube.HealthWarning} {
		var found *Node

		for i := range g.out.Nodes {
			n := &g.out.Nodes[i]
			if n.Health == level && (g.front == 0 || n.Layer > 0) && (found == nil || n.Layer > found.Layer) {
				found = n
			}
		}

		if found != nil {
			return &Problem{Kind: found.Kind, Namespace: found.Namespace, Name: found.Name, Detail: found.Detail}
		}
	}

	return nil
}

// PodHealth: running and ready (or completed) is ok, waiting to start or not ready yet is a
// warning, crashing or failing is critical.
func PodHealth(p kube.Pod) string {
	switch {
	case p.Status == "Completed":
		return kube.HealthIdle
	case p.Healthy:
		return kube.HealthOK
	case p.Status == "Running", p.Status == "Pending", p.Status == "ContainerCreating", p.Status == "PodInitializing",
		p.Status == "Terminating", strings.HasPrefix(p.Status, "Init:") && !strings.Contains(p.Status, "Err") && !strings.Contains(p.Status, "BackOff"):
		return kube.HealthWarning
	default:
		return kube.HealthCritical
	}
}

// sortMap orders each layer: worst first, then by name, so the trouble is on top.
func sortMap(g *Graph) {
	slices.SortStableFunc(g.out.Nodes, func(a, b Node) int {
		return cmp.Or(a.Layer-b.Layer, kube.HealthRank(a.Health)-kube.HealthRank(b.Health), strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name))
	})

	for i, n := range g.out.Nodes {
		g.index[n.ID] = i
	}

	slices.SortStableFunc(g.out.Edges, func(a, b Edge) int {
		return cmp.Or(g.index[a.From]-g.index[b.From], g.index[a.To]-g.index[b.To])
	})
}
