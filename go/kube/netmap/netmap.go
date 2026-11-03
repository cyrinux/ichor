// Package netmap draws how traffic reaches pods: a left-to-right layered graph whose back half
// is the same for every caller,
//
//	… caller's entry boxes … → Service → Pod → Node
//
// with each box coloured from the inside out and the deepest broken box named as the likely
// root cause. A caller (the Argo CD app's Network section, a Tailscale proxy) declares how many
// layers of its own come in front of the Services (Graph's front), adds its boxes there and
// links them to the Services AddBackend added. Layer 0 holds the entry points (a host, a load
// balancer address), never a root cause.
package netmap

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/kube"
)

// The kinds this package adds and judges itself; a caller's own kinds are its business.
const (
	KindService = "Service"
	KindPod     = "Pod"
	KindNode    = "Node"
)

// Map is the graph the apps draw.
type Map struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
	// Problem is null when every box is fine.
	Problem *Problem `json:"problem"`
}

// Problem is the deepest broken box, the likely root cause (a node down before the pods it
// stopped, before the Service left without endpoints), for the app to word.
type Problem struct {
	Kind      string `json:"kind"` // Node|Pod|Service, or one of the caller's kinds
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
}

// Node is one box of the graph.
type Node struct {
	ID        string `json:"id"`
	Layer     int    `json:"layer"`
	Kind      string `json:"kind"` // Service|Pod|Node, or one of the caller's kinds (Host, Ingress…)
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Detail    string `json:"detail"` // "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address
	Health    string `json:"health"` // critical|warning|ok|idle
	URL       string `json:"url"`    // what a host opens, "" otherwise
	// Managed: the caller's own resource (an Argo CD app's), false for pods and nodes.
	Managed bool `json:"managed"`
}

// Edge is one hop of traffic.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Health string `json:"health"` // that of the hop's target: traffic stops where it is red
}

// Service is the part of a Kubernetes Service the map reads.
type Service struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Type      string            `json:"type"`
		ClusterIP string            `json:"clusterIP"`
		Selector  map[string]string `json:"selector"`
		Ports     []struct {
			Name       string          `json:"name"`
			Port       int32           `json:"port"`
			NodePort   int32           `json:"nodePort"`
			TargetPort json.RawMessage `json:"targetPort"`
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// Pod is a pod with its labels, for the Services' selectors.
type Pod struct {
	kube.Pod
	Labels map[string]string
}

// NodeObject is the part of a Kubernetes Node the map reads: its readiness and cordon.
type NodeObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Unschedulable bool `json:"unschedulable"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}

// Graph collects boxes and hops, each once.
type Graph struct {
	front int                // layers the caller puts in front of the Services
	index map[string]int     // node id -> position in out.Nodes
	edges map[[2]string]bool // hops already added
	out   Map
}

// NewGraph starts a map whose caller draws front layers of its own (0 … front-1) before the
// Services.
func NewGraph(front int) *Graph {
	return &Graph{front: front, index: map[string]int{}, edges: map[[2]string]bool{}, out: Map{Nodes: []Node{}, Edges: []Edge{}}}
}

// ServiceLayer, PodLayer and NodeLayer are the columns of the back half.
func (g *Graph) ServiceLayer() int { return g.front }
func (g *Graph) PodLayer() int     { return g.front + 1 }
func (g *Graph) NodeLayer() int    { return g.front + 2 }

// Add adds n once, and returns its id; adding it again only keeps Managed if either was.
func (g *Graph) Add(n Node) string {
	if i, ok := g.index[n.ID]; ok {
		g.out.Nodes[i].Managed = g.out.Nodes[i].Managed || n.Managed

		return n.ID
	}

	g.index[n.ID] = len(g.out.Nodes)
	g.out.Nodes = append(g.out.Nodes, n)

	return n.ID
}

// Node is the box id, nil when there is none.
func (g *Graph) Node(id string) *Node {
	if i, ok := g.index[id]; ok {
		return &g.out.Nodes[i]
	}

	return nil
}

// Link adds the hop from → to once; an empty end adds nothing.
func (g *Graph) Link(from, to string) {
	if from != "" && to != "" && !g.edges[[2]string{from, to}] {
		g.edges[[2]string{from, to}] = true
		g.out.Edges = append(g.out.Edges, Edge{From: from, To: to})
	}
}

// ServiceID is the id of a Service's box, for the caller's routes to link to.
func ServiceID(namespace, name string) string { return "svc/" + namespace + "/" + name }

// AddBackend adds the Service, the pods it selects and the nodes they run on, and returns the
// Service's id. A Service without a selector (endpoints managed by hand, ExternalName) has no
// pods to judge it by: it is idle.
func AddBackend(g *Graph, svc Service, pods []Pod) string {
	ns, name := svc.Metadata.Namespace, svc.Metadata.Name

	sid := g.Add(Node{ID: ServiceID(ns, name), Layer: g.ServiceLayer(), Kind: KindService, Namespace: ns, Name: name, Detail: ServiceDetail(svc)})
	if len(svc.Spec.Selector) == 0 {
		g.Node(sid).Health = kube.HealthIdle
	}

	for _, p := range pods {
		if p.Namespace != ns || !Selects(svc.Spec.Selector, p.Labels) {
			continue
		}

		pid := g.Add(Node{ID: "pod/" + ns + "/" + p.Name, Layer: g.PodLayer(), Kind: KindPod, Namespace: ns, Name: p.Name, Detail: PodDetail(p.Pod), Health: PodHealth(p.Pod)})
		g.Link(sid, pid)

		if p.Node != "" {
			nid := g.Add(Node{ID: "node/" + p.Node, Layer: g.NodeLayer(), Kind: KindNode, Name: p.Node})
			g.Link(pid, nid)
		}
	}

	return sid
}

// Selects tells whether a Service selector matches labels; an empty one selects nothing (its
// endpoints are managed by hand).
func Selects(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}

	return true
}

// ServiceDetail is "ClusterIP 10.96.0.12 · 80→8080, 443→https".
func ServiceDetail(svc Service) string {
	ports := make([]string, 0, len(svc.Spec.Ports))

	for _, p := range svc.Spec.Ports {
		port := strconv.Itoa(int(p.Port))

		var target string
		if json.Unmarshal(p.TargetPort, &target) != nil {
			var n int
			if json.Unmarshal(p.TargetPort, &n) == nil && n != 0 {
				target = strconv.Itoa(n)
			}
		}

		if target != "" && target != port {
			port += "→" + target
		}

		if p.NodePort != 0 {
			port += fmt.Sprintf(" (node %d)", p.NodePort)
		}

		ports = append(ports, port)
	}

	head := strings.TrimSpace(cmp.Or(svc.Spec.Type, "ClusterIP") + " " + strings.ReplaceAll(svc.Spec.ClusterIP, "None", "headless"))

	if len(ports) == 0 {
		return head
	}

	return head + " · " + strings.Join(ports, ", ")
}

// PodDetail is the pod's status, with its ready containers when not all are.
func PodDetail(p kube.Pod) string {
	if p.Status == "Running" && p.Ready < p.Containers {
		return fmt.Sprintf("Running · %d/%d ready", p.Ready, p.Containers)
	}

	if p.Restarts > 0 {
		return fmt.Sprintf("%s · %d restarts", p.Status, p.Restarts)
	}

	return p.Status
}
