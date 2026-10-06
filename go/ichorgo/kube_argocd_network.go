package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// The network view of an Argo CD app, like the Network tab of Argo CD's own UI, with one more
// column it cannot draw: how traffic reaches the app's pods and which nodes they run on.
//
//	host → Gateway → Ingress/HTTPRoute → Service → Pod → node
//
// Built from the app's own resources (its Services, Ingresses and HTTPRoutes) plus the routes
// elsewhere that send traffic to its Services (a shared Gateway's HTTPRoute), the pods those
// Services select and the nodes those pods run on.

// Layers of the graph, left to right.
const (
	argoLayerHost = iota
	argoLayerGateway
	argoLayerRoute
	argoLayerService
	argoLayerPod
	argoLayerNode
)

type argoNetwork struct {
	Nodes []argoNetNode `json:"nodes"`
	Edges []argoNetEdge `json:"edges"`
	// Problem is null when every box is fine.
	Problem *argoNetProblem `json:"problem"`
}

// argoNetProblem is the deepest broken box, the likely root cause (a node down before the
// pods it stopped, before the Service left without endpoints), for the app to word.
type argoNetProblem struct {
	Kind      string `json:"kind"` // Node|Pod|Service|Ingress|Gateway
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Detail    string `json:"detail"`
}

type argoNetNode struct {
	ID        string `json:"id"`
	Layer     int    `json:"layer"`
	Kind      string `json:"kind"` // Host|LoadBalancer|Gateway|Ingress|HTTPRoute|Service|Pod|Node
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Detail    string `json:"detail"` // "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address
	Health    string `json:"health"` // critical|warning|ok|idle
	URL       string `json:"url"`    // what a host opens, "" otherwise
	// Managed: the app's own resource (false for a shared Gateway or route, pods, nodes).
	Managed bool `json:"managed"`
}

type argoNetEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Health string `json:"health"` // that of the hop's target: traffic stops where it is red
}

// KubeArgoNetwork draws how traffic reaches the Argo CD Application namespace/name (os:admin):
// {"nodes":[{id,layer,kind,namespace,name,detail,health,url,managed}],"edges":[{from,to,health}],
// "problem":null|{kind,namespace,name,detail}}. kubeServer: see KubePods.
func KubeArgoNetwork(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if err := validateKubeName("application", namespace, name); err != nil {
		return "", err
	}

	demo := func() argoNetwork { return demoArgoNetwork(name) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (argoNetwork, error) {
		return readArgoNetwork(ctx, k, namespace, name)
	})
}

type netService struct {
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

type netPod struct {
	kubePod
	labels map[string]string
}

type netNodeObject struct {
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

// argoNetInput is what the graph is built from, read from the API server.
type argoNetInput struct {
	app        argoObject
	services   []netService
	pods       []netPod
	ingresses  []ingressObject
	httpRoutes []httpRouteObject
	gateways   []gatewayObject
	nodes      []netNodeObject
}

func readArgoNetwork(ctx context.Context, k *kubeClient, namespace, name string) (argoNetwork, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return argoNetwork{}, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return argoNetwork{}, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	in := argoNetInput{}

	path := "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)
	if err := k.get(ctx, path, &in.app); err != nil {
		return argoNetwork{}, err
	}

	namespaces := argoNetNamespaces(in.app)

	var (
		mu    sync.Mutex
		errs  []error
		tasks []func()
	)

	fail := func(err error) {
		if err != nil {
			mu.Lock()
			errs = append(errs, err)
			mu.Unlock()
		}
	}

	for _, ns := range namespaces {
		base := "/api/v1/namespaces/" + url.PathEscape(ns)

		tasks = append(tasks, func() {
			var list kubeList[netService]
			if err := getList(ctx, k, base+"/services", &list); err != nil {
				fail(err)

				return
			}

			mu.Lock()
			in.services = append(in.services, list.Items...)
			mu.Unlock()
		})
		tasks = append(tasks, func() {
			pods, err := readNetPods(ctx, k, base+"/pods")
			fail(err)
			mu.Lock()
			in.pods = append(in.pods, pods...)
			mu.Unlock()
		})
	}

	var (
		ingresses  kubeList[ingressObject]
		httpRoutes kubeList[httpRouteObject]
		gateways   kubeList[gatewayObject]
		nodes      kubeList[netNodeObject]
	)

	tasks = append(tasks,
		func() { fail(getList(ctx, k, "/apis/networking.k8s.io/v1/ingresses", &ingresses)) },
		func() { fail(ignoreNotFound(getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/httproutes", &httpRoutes))) },
		func() { _ = getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways", &gateways) },
		// Node readiness only colours the last column: without it the graph still shows.
		func() { _ = getList(ctx, k, "/api/v1/nodes", &nodes) },
	)
	forEachNode(tasks, func(_ int, read func()) { read() })

	if len(errs) > 0 {
		return argoNetwork{}, errs[0]
	}

	in.ingresses, in.httpRoutes, in.gateways, in.nodes = ingresses.Items, httpRoutes.Items, gateways.Items, nodes.Items

	return buildArgoNetwork(in), nil
}

// readNetPods lists pods with their labels (podObject leaves them out).
func readNetPods(ctx context.Context, k *kubeClient, path string) ([]netPod, error) {
	var list kubeList[json.RawMessage]
	if err := getList(ctx, k, path, &list); err != nil {
		return nil, err
	}

	pods := make([]netPod, 0, len(list.Items))

	for _, raw := range list.Items {
		var (
			obj    podObject
			labels labeledObject
		)

		if json.Unmarshal(raw, &obj) != nil || json.Unmarshal(raw, &labels) != nil {
			continue
		}

		pods = append(pods, netPod{kubePod: mapPod(obj), labels: labels.Metadata.Labels})
	}

	return pods, nil
}

// argoNetNamespaces are the namespaces of the app's Services, else its destination's.
func argoNetNamespaces(app argoObject) []string {
	var out []string

	for _, r := range app.Status.Resources {
		if r.Group == "" && r.Kind == "Service" && r.Namespace != "" {
			out = append(out, r.Namespace)
		}
	}

	if len(out) == 0 && app.Spec.Destination.Namespace != "" {
		out = append(out, app.Spec.Destination.Namespace)
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// argoNetGraph collects nodes and edges, each once.
type argoNetGraph struct {
	index map[string]int // node id -> position in out.Nodes
	edges map[[2]string]bool
	out   argoNetwork
}

func (g *argoNetGraph) add(n argoNetNode) string {
	if i, ok := g.index[n.ID]; ok {
		g.out.Nodes[i].Managed = g.out.Nodes[i].Managed || n.Managed

		return n.ID
	}

	g.index[n.ID] = len(g.out.Nodes)
	g.out.Nodes = append(g.out.Nodes, n)

	return n.ID
}

func (g *argoNetGraph) node(id string) *argoNetNode {
	if i, ok := g.index[id]; ok {
		return &g.out.Nodes[i]
	}

	return nil
}

func (g *argoNetGraph) link(from, to string) {
	if from != "" && to != "" && !g.edges[[2]string{from, to}] {
		g.edges[[2]string{from, to}] = true
		g.out.Edges = append(g.out.Edges, argoNetEdge{From: from, To: to})
	}
}

func buildArgoNetwork(in argoNetInput) argoNetwork {
	g := &argoNetGraph{index: map[string]int{}, edges: map[[2]string]bool{}}
	g.out = argoNetwork{Nodes: []argoNetNode{}, Edges: []argoNetEdge{}}

	managed := map[string]bool{} // kind/namespace/name of the app's resources
	for _, r := range in.app.Status.Resources {
		managed[r.Kind+"/"+r.Namespace+"/"+r.Name] = true
	}

	// The app's Services, and the pods each selects.
	services := map[serviceRef]bool{}

	for _, svc := range in.services {
		ns, name := svc.Metadata.Namespace, svc.Metadata.Name
		if !managed["Service/"+ns+"/"+name] {
			continue
		}

		services[serviceRef{ns, name}] = true
		sid := g.add(argoNetNode{ID: "svc/" + ns + "/" + name, Layer: argoLayerService, Kind: "Service", Namespace: ns, Name: name, Detail: serviceDetail(svc), Managed: true})
		if len(svc.Spec.Selector) == 0 {
			// Endpoints managed by hand (or ExternalName): no pods to judge it by.
			g.node(sid).Health = healthIdle
		}

		for _, lb := range svc.Status.LoadBalancer.Ingress {
			addr := cmp.Or(lb.Hostname, lb.IP)
			lid := g.add(argoNetNode{ID: "lb/" + addr, Layer: argoLayerHost, Kind: "LoadBalancer", Name: addr, Detail: svc.Spec.Type})
			g.link(lid, sid)
		}

		for _, p := range in.pods {
			if p.Namespace != ns || !selects(svc.Spec.Selector, p.labels) {
				continue
			}

			pid := g.add(argoNetNode{ID: "pod/" + ns + "/" + p.Name, Layer: argoLayerPod, Kind: "Pod", Namespace: ns, Name: p.Name, Detail: podDetail(p.kubePod), Health: podHealth(p.kubePod)})
			g.link(sid, pid)

			if p.Node != "" {
				nid := g.add(argoNetNode{ID: "node/" + p.Node, Layer: argoLayerNode, Kind: "Node", Name: p.Node})
				g.link(pid, nid)
			}
		}
	}

	argoNetRoutes(g, in, services, managed)
	argoNetHealth(g, in)
	argoNetSort(g)

	return g.out
}

// argoNetRoutes adds the Ingresses and HTTPRoutes sending traffic to the app's Services, their
// hosts, and the Gateways the HTTPRoutes attach to.
func argoNetRoutes(g *argoNetGraph, in argoNetInput, services map[serviceRef]bool, managed map[string]bool) {
	for _, r := range ingressRoutes(in.ingresses, services) {
		rid := g.add(argoNetNode{ID: "ing/" + r.Namespace + "/" + r.Name, Layer: argoLayerRoute, Kind: "Ingress", Namespace: r.Namespace, Name: r.Name,
			Detail: ingressAddress(in.ingresses, r.Namespace, r.Name), Managed: managed["Ingress/"+r.Namespace+"/"+r.Name]})
		hid := g.add(argoNetNode{ID: "host/" + r.URL, Layer: argoLayerHost, Kind: "Host", Name: hostOf(r.URL), Detail: r.URL, URL: r.URL})
		g.link(hid, rid)
		g.link(rid, "svc/"+r.Namespace+"/"+r.Service)
	}

	for _, r := range httpRouteRoutes(in.httpRoutes, in.gateways, services) {
		rid := g.add(argoNetNode{ID: "hr/" + r.Namespace + "/" + r.Name, Layer: argoLayerRoute, Kind: "HTTPRoute", Namespace: r.Namespace, Name: r.Name,
			Managed: managed["HTTPRoute/"+r.Namespace+"/"+r.Name]})
		hid := g.add(argoNetNode{ID: "host/" + r.URL, Layer: argoLayerHost, Kind: "Host", Name: hostOf(r.URL), Detail: r.URL, URL: r.URL})
		g.link(rid, "svc/"+r.Namespace+"/"+r.Service)

		parents := httpRouteGateways(in.httpRoutes, in.gateways, r.Namespace, r.Name)
		if len(parents) == 0 {
			g.link(hid, rid)
		}

		for _, gw := range parents {
			gid := g.add(argoNetNode{ID: "gw/" + gw.Metadata.Namespace + "/" + gw.Metadata.Name, Layer: argoLayerGateway, Kind: "Gateway",
				Namespace: gw.Metadata.Namespace, Name: gw.Metadata.Name, Detail: gatewayAddress(gw), Managed: managed["Gateway/"+gw.Metadata.Namespace+"/"+gw.Metadata.Name]})
			g.link(hid, gid)
			g.link(gid, rid)
		}
	}
}

func httpRouteGateways(routes []httpRouteObject, gateways []gatewayObject, namespace, name string) []gatewayObject {
	i := slices.IndexFunc(routes, func(hr httpRouteObject) bool { return hr.Metadata.Namespace == namespace && hr.Metadata.Name == name })
	if i < 0 {
		return nil
	}

	var out []gatewayObject

	for _, ref := range routes[i].Spec.ParentRefs {
		if !isGatewayRef(ref) {
			continue
		}

		ns := orDefault(ref.Namespace, namespace)
		if j := slices.IndexFunc(gateways, func(gw gatewayObject) bool { return gw.Metadata.Namespace == ns && gw.Metadata.Name == ref.Name }); j >= 0 {
			out = append(out, gateways[j])
		}
	}

	return out
}

func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host + strings.TrimSuffix(u.Path, "/")
	}

	return rawURL
}

func ingressAddress(ingresses []ingressObject, namespace, name string) string {
	for _, ing := range ingresses {
		if ing.Metadata.Namespace == namespace && ing.Metadata.Name == name {
			return ingressHost(ing, "")
		}
	}

	return ""
}

func gatewayAddress(gw gatewayObject) string {
	if len(gw.Status.Addresses) > 0 {
		return gw.Status.Addresses[0].Value
	}

	return ""
}

// serviceDetail is "ClusterIP 10.96.0.12 · 80→8080, 443→https".
func serviceDetail(svc netService) string {
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

// podDetail is the pod's status, with its ready containers when not all are.
func podDetail(p kubePod) string {
	if p.Status == "Running" && p.Ready < p.Containers {
		return fmt.Sprintf("Running · %d/%d ready", p.Ready, p.Containers)
	}

	if p.Restarts > 0 {
		return fmt.Sprintf("%s · %d restarts", p.Status, p.Restarts)
	}

	return p.Status
}
