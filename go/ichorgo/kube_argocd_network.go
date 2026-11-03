package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

// The network view of an Argo CD app, like the Network tab of Argo CD's own UI, with one more
// column it cannot draw: how traffic reaches the app's pods and which nodes they run on.
//
//	host → Gateway → Ingress/HTTPRoute/GRPCRoute/TLSRoute → Service → Pod → node
//
// Built from the app's own resources (its Services, Ingresses and HTTPRoutes) plus the routes
// elsewhere that send traffic to its Services (a shared Gateway's HTTPRoute), the pods those
// Services select and the nodes those pods run on.

// The Argo layers in front of the Services, left to right; netmap draws Service → Pod → node
// behind them.
const (
	argoLayerHost = iota
	argoLayerGateway
	argoLayerRoute
	argoNetFront // the number of Argo layers
)

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

	demo := func() netmap.Map { return demoArgoNetwork(name) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (netmap.Map, error) {
		return readArgoNetwork(ctx, k, namespace, name)
	})
}

// argoNetInput is what the graph is built from, read from the API server.
type argoNetInput struct {
	app            argoObject
	services       []netmap.Service
	pods           []netmap.Pod
	ingresses      []ingressObject
	gatewayRoutes  []gatewayRouteObject // HTTPRoutes, GRPCRoutes and TLSRoutes
	gateways       []gatewayObject
	gatewayClasses []gatewayClassObject
	traefik        traefikRouteSet
	nodes          []netmap.NodeObject
}

func readArgoNetwork(ctx context.Context, k *kubeClient, namespace, name string) (netmap.Map, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return netmap.Map{}, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return netmap.Map{}, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	in := argoNetInput{}

	path := "/apis/" + groupArgo + "/" + version + "/namespaces/" + url.PathEscape(namespace) + "/applications/" + url.PathEscape(name)
	if err := k.get(ctx, path, &in.app); err != nil {
		return netmap.Map{}, err
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
			var list kubeList[netmap.Service]
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
		ingresses      kubeList[ingressObject]
		gwRoutes       []gatewayRouteObject
		gateways       kubeList[gatewayObject]
		gatewayClasses kubeList[gatewayClassObject]
		nodes          kubeList[netmap.NodeObject]
	)

	tasks = append(tasks,
		func() { fail(getList(ctx, k, "/apis/networking.k8s.io/v1/ingresses", &ingresses)) },
		// The Gateway API is optional: without its CRDs, no routes. GRPCRoute came with v1.1;
		// TLSRoute is the experimental channel (v1alpha2), absent on most clusters.
		func() {
			http, err := readGatewayRoutes(ctx, k, "/apis/"+groupGatewayAPI+"/v1/httproutes", "HTTPRoute")
			fail(err)
			grpc, err := readGatewayRoutes(ctx, k, "/apis/"+groupGatewayAPI+"/v1/grpcroutes", "GRPCRoute")
			fail(err)
			tls, err := readGatewayRoutes(ctx, k, "/apis/"+groupGatewayAPI+"/v1alpha2/tlsroutes", "TLSRoute")
			fail(err)
			mu.Lock()
			gwRoutes = slices.Concat(http, grpc, tls)
			mu.Unlock()
		},
		func() { _ = getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways", &gateways) },
		// The controller name on the Gateway boxes is a nice-to-have: a restricted
		// kubeconfig keeps the graph.
		func() { _ = getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/gatewayclasses", &gatewayClasses) },
		func() {
			traefik, err := readTraefikRoutes(ctx, k, "", true)
			fail(err)
			mu.Lock()
			in.traefik = traefik
			mu.Unlock()
		},
		// Node readiness only colours the last column: without it the graph still shows.
		func() { _ = getList(ctx, k, "/api/v1/nodes", &nodes) },
	)
	forEachNode(tasks, func(_ int, read func()) { read() })

	if len(errs) > 0 {
		return netmap.Map{}, errs[0]
	}

	in.ingresses, in.gatewayRoutes, in.gateways, in.nodes = ingresses.Items, gwRoutes, gateways.Items, nodes.Items
	in.gatewayClasses = gatewayClasses.Items
	if len(in.gatewayRoutes) > 0 {
		in.gateways = withPublicPorts(ctx, k, in.gateways)
	}

	return buildArgoNetwork(in), nil
}

// readNetPods lists pods with their labels (podObject leaves them out).
func readNetPods(ctx context.Context, k *kubeClient, path string) ([]netmap.Pod, error) {
	var list kubeList[json.RawMessage]
	if err := getList(ctx, k, path, &list); err != nil {
		return nil, err
	}

	pods := make([]netmap.Pod, 0, len(list.Items))

	for _, raw := range list.Items {
		var (
			obj    podObject
			labels labeledObject
		)

		if json.Unmarshal(raw, &obj) != nil || json.Unmarshal(raw, &labels) != nil {
			continue
		}

		pods = append(pods, netmap.Pod{Pod: mapPod(obj), Labels: labels.Metadata.Labels})
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

func buildArgoNetwork(in argoNetInput) netmap.Map {
	g := netmap.NewGraph(argoNetFront)

	managed := map[string]bool{} // kind/namespace/name of the app's resources
	for _, r := range in.app.Status.Resources {
		managed[r.Kind+"/"+r.Namespace+"/"+r.Name] = true
	}

	// The app's Services, the pods each selects and their nodes, and the load balancer
	// addresses in front of them.
	services := map[serviceRef]bool{}

	for _, svc := range in.services {
		ns, name := svc.Metadata.Namespace, svc.Metadata.Name
		if !managed["Service/"+ns+"/"+name] {
			continue
		}

		services[serviceRef{ns, name}] = true
		sid := netmap.AddBackend(g, svc, in.pods)
		g.Node(sid).Managed = true

		for _, lb := range svc.Status.LoadBalancer.Ingress {
			addr := cmp.Or(lb.Hostname, lb.IP)
			lid := g.Add(netmap.Node{ID: "lb/" + addr, Layer: argoLayerHost, Kind: "LoadBalancer", Name: addr, Detail: svc.Spec.Type})
			g.Link(lid, sid)
		}
	}

	argoNetRoutes(g, in, services, managed)

	return netmap.Finish(g, in.nodes)
}

// argoNetRoutes adds the Ingresses, Gateway API routes and Traefik routes sending traffic to
// the app's Services, their hosts, and the Gateways the Gateway routes attach to.
func argoNetRoutes(g *netmap.Graph, in argoNetInput, services map[serviceRef]bool, managed map[string]bool) {
	for _, r := range ingressRoutes(in.ingresses, services) {
		rid := g.Add(netmap.Node{ID: "ing/" + r.Namespace + "/" + r.Name, Layer: argoLayerRoute, Kind: "Ingress", Namespace: r.Namespace, Name: r.Name,
			Detail: ingressAddress(in.ingresses, r.Namespace, r.Name), Managed: managed["Ingress/"+r.Namespace+"/"+r.Name]})
		hid := g.Add(netmap.Node{ID: "host/" + r.URL, Layer: argoLayerHost, Kind: "Host", Name: hostOf(r.URL), Detail: r.URL, URL: r.URL})
		g.Link(hid, rid)
		g.Link(rid, netmap.ServiceID(r.Namespace, r.Service))
	}

	for _, r := range traefikRoutes(in.traefik, services) {
		prefix := "ir/"
		if r.Kind == "IngressRouteTCP" {
			prefix = "irtcp/"
		}

		rid := g.Add(netmap.Node{ID: prefix + r.Namespace + "/" + r.Name, Layer: argoLayerRoute, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
			Detail: traefikAddress(in.traefik, r.Kind, r.Namespace, r.Name), Managed: managed[r.Kind+"/"+r.Namespace+"/"+r.Name]})
		hid := g.Add(netmap.Node{ID: "host/" + r.URL, Layer: argoLayerHost, Kind: "Host", Name: hostOf(r.URL), Detail: r.URL, URL: r.URL})
		g.Link(hid, rid)
		g.Link(rid, netmap.ServiceID(r.Namespace, r.Service))
	}

	argoNetGatewayRoutes(g, in, services, managed)
}

// argoNetGatewayRoutes adds the Gateway API routes sending traffic to the app's Services and
// the Gateways they attach to, with a node id prefix per kind (hr, gr, tr).
func argoNetGatewayRoutes(g *netmap.Graph, in argoNetInput, services map[serviceRef]bool, managed map[string]bool) {
	prefixes := map[string]string{"HTTPRoute": "hr/", "GRPCRoute": "gr/", "TLSRoute": "tr/"}

	for _, r := range gatewayRoutes(in.gatewayRoutes, in.gateways, services) {
		prefix := prefixes[r.Kind]

		rid := g.Add(netmap.Node{ID: prefix + r.Namespace + "/" + r.Name, Layer: argoLayerRoute, Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
			Managed: managed[r.Kind+"/"+r.Namespace+"/"+r.Name]})
		hid := g.Add(netmap.Node{ID: "host/" + r.URL, Layer: argoLayerHost, Kind: "Host", Name: hostOf(r.URL), Detail: r.URL, URL: r.URL})
		g.Link(rid, netmap.ServiceID(r.Namespace, r.Service))

		parents := gatewayRouteParents(in.gatewayRoutes, in.gateways, r.Kind, r.Namespace, r.Name)
		if len(parents) == 0 {
			g.Link(hid, rid)
		}

		for _, gw := range parents {
			gid := g.Add(netmap.Node{ID: "gw/" + gw.Metadata.Namespace + "/" + gw.Metadata.Name, Layer: argoLayerGateway, Kind: "Gateway",
				Namespace: gw.Metadata.Namespace, Name: gw.Metadata.Name, Detail: gatewayDetail(gw, in.gatewayClasses), Managed: managed["Gateway/"+gw.Metadata.Namespace+"/"+gw.Metadata.Name]})
			g.Link(hid, gid)
			g.Link(gid, rid)
		}
	}
}

// gatewayRouteParents are the Gateways the route kind/namespace/name attaches to. The kind
// matters: an HTTPRoute and a GRPCRoute may share a namespace and a name, and each attaches to
// its own Gateways.
func gatewayRouteParents(routes []gatewayRouteObject, gateways []gatewayObject, kind, namespace, name string) []gatewayObject {
	i := slices.IndexFunc(routes, func(r gatewayRouteObject) bool {
		return r.Kind == kind && r.Metadata.Namespace == namespace && r.Metadata.Name == name
	})
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

// gatewayDetail is "Traefik · 10.0.0.240": the controller serving the Gateway, then its
// address; the address alone when the class is unknown, the controller alone when the
// Gateway has no address yet.
func gatewayDetail(gw gatewayObject, classes []gatewayClassObject) string {
	controller, address := gatewayController(gw.Spec.GatewayClassName, classes), gatewayAddress(gw)

	switch {
	case controller == "":
		return address
	case address == "":
		return controller
	default:
		return controller + " · " + address
	}
}
