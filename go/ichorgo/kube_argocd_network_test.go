package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

func netNode(t *testing.T, n netmap.Map, id string) netmap.Node {
	t.Helper()

	for _, node := range n.Nodes {
		if node.ID == id {
			return node
		}
	}

	t.Fatalf("no node %q in %+v", id, n.Nodes)

	return netmap.Node{}
}

func netEdges(n netmap.Map) string {
	var out []string
	for _, e := range n.Edges {
		out = append(out, e.From+">"+e.To+":"+e.Health)
	}

	return strings.Join(out, " ")
}

// The demo Gateway names its controller next to the address.
func TestArgoNetworkGatewayController(t *testing.T) {
	n := demoArgoNetwork("grafana")

	if gw := netNode(t, n, "gw/traefik/homelab"); gw.Detail != "Traefik · 10.0.0.240" {
		t.Fatalf("gateway detail %q", gw.Detail)
	}
}

func TestArgoNetworkIngressHealthy(t *testing.T) {
	n := demoArgoNetwork("hello-ichor")

	host := netNode(t, n, "host/https://hello.homelab.lan")
	if host.Kind != "Host" || host.Name != "hello.homelab.lan" || host.Health != healthOK || host.Layer != argoLayerHost {
		t.Fatalf("host %+v", host)
	}

	ing := netNode(t, n, "ing/demo/hello-ichor")
	if !ing.Managed || ing.Detail != "10.0.0.240" || ing.Health != healthOK {
		t.Fatalf("ingress %+v", ing)
	}

	svc := netNode(t, n, "svc/demo/hello-ichor")
	if svc.Detail != "ClusterIP 10.96.14.20 · 80→8080" || svc.Health != healthOK {
		t.Fatalf("service %+v", svc)
	}

	if n.Problem != nil {
		t.Fatalf("problem %+v", n.Problem)
	}

	// Three pods on two nodes; every hop green.
	if got := netEdges(n); strings.Count(got, ":ok") != len(n.Edges) || strings.Count(got, "svc/demo/hello-ichor>pod/") != 3 || strings.Count(got, ">node/") != 3 {
		t.Fatalf("edges %s", got)
	}
}

func TestArgoNetworkTailscaleIngressURL(t *testing.T) {
	in := argoNetInput{
		services: []netmap.Service{demoNetObject[netmap.Service](`{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"ClusterIP"}}`)},
		ingresses: []ingressObject{demoNetObject[ingressObject](`{"metadata":{"name":"web","namespace":"shop"},
			"spec":{"ingressClassName":"tailscale","tls":[{"hosts":["web"]}],"defaultBackend":{"service":{"name":"web"}}},
			"status":{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}}`)},
	}
	in.app.Status.Resources = []argoResourceStatus{{Kind: "Service", Namespace: "shop", Name: "web"}, {Kind: "Ingress", Namespace: "shop", Name: "web"}}

	// Without the operator's API group, no matcher runs: a plain host box.
	n := buildArgoNetwork(in)
	host := netNode(t, n, "host/https://web.tail123.ts.net")
	if host.URL != "https://web.tail123.ts.net" || host.Name != "web.tail123.ts.net" {
		t.Fatalf("host %+v", host)
	}
	if got := netEdges(n); !strings.Contains(got, "host/https://web.tail123.ts.net>ing/shop/web") || !strings.Contains(got, "ing/shop/web>svc/shop/web") {
		t.Fatalf("edges %s", got)
	}

	// With it, the tailnet name is the entry, through the same helper as the Tailscale map.
	raw, _ := json.Marshal(in.ingresses[0])
	in.addExternal(tsKindIngress, "shop", "web", raw, map[string]string{groupTailscale: "v1alpha1"})
	n = buildArgoNetwork(in)

	name := netNode(t, n, "ext/Tailscale/web.tail123.ts.net")
	if name.Kind != netKindExternalName || name.URL != "https://web.tail123.ts.net" || name.Layer != argoLayerHost || name.Detail != "Tailscale" {
		t.Fatalf("name %+v", name)
	}
	if got := netEdges(n); strings.Contains(got, "host/") || !strings.Contains(got, "ext/Tailscale/web.tail123.ts.net>ing/shop/web") {
		t.Fatalf("edges %s", got)
	}
}

// The demo grafana app's Service is also on the tailnet: its name box sends traffic to it; the
// other demo apps have none.
func TestArgoNetworkTailnetService(t *testing.T) {
	n := demoArgoNetwork("grafana")

	if name := netNode(t, n, "ext/Tailscale/grafana"); name.Layer != argoLayerHost || name.Health != healthOK || name.Detail != "Tailscale" {
		t.Fatalf("name %+v", name)
	}
	if got := netEdges(n); !strings.Contains(got, "ext/Tailscale/grafana>svc/monitoring/grafana:ok") {
		t.Fatalf("edges %s", got)
	}

	for _, app := range []string{"hello-ichor", "demo-worker", "traefik", "longhorn"} {
		for _, node := range demoArgoNetwork(app).Nodes {
			if node.Kind == netKindExternalName {
				t.Errorf("%s has %+v", app, node)
			}
		}
	}
}

// A tailnet LoadBalancer Service: its address is drawn once, as the tailnet name.
func TestArgoNetworkTailnetLoadBalancer(t *testing.T) {
	raw := `{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"LoadBalancer","loadBalancerClass":"tailscale"},
	  "status":{"loadBalancer":{"ingress":[{"hostname":"web.example-tailnet.ts.net","ip":"100.64.0.9"}]},"conditions":[{"type":"TailscaleProxyReady","status":"False","reason":"ProxyFailed"}]}}`
	in := argoNetInput{services: []netmap.Service{demoNetObject[netmap.Service](raw)}}
	in.app.Status.Resources = []argoResourceStatus{{Kind: "Service", Namespace: "shop", Name: "web"}}
	in.addExternal(tsKindService, "shop", "web", json.RawMessage(raw), map[string]string{groupTailscale: "v1alpha1"})

	n := buildArgoNetwork(in)
	for _, node := range n.Nodes {
		if node.Kind == "LoadBalancer" {
			t.Fatalf("load balancer box %+v", node)
		}
	}

	if name := netNode(t, n, "ext/Tailscale/web.example-tailnet.ts.net"); name.Health != healthCritical || name.Detail != "Tailscale · ProxyFailed" {
		t.Fatalf("name %+v", name)
	}
}

func TestArgoNetworkRootCauseIsTheNode(t *testing.T) {
	n := demoArgoNetwork("demo-worker")

	// One pod crashes on a node that is down: the Service still has a ready pod (warning), the
	// root cause is the node.
	if svc := netNode(t, n, "svc/demo/worker"); svc.Health != healthWarning || svc.Detail != "ClusterIP 10.96.31.7 · 9090→metrics" {
		t.Fatalf("service %+v", svc)
	}

	if p := netNode(t, n, "pod/demo/worker-6f4b8-uvwxy"); p.Health != healthCritical || p.Detail != "CrashLoopBackOff · 14 restarts" {
		t.Fatalf("pod %+v", p)
	}

	if node := netNode(t, n, "node/demo-worker-3"); node.Health != healthCritical || node.Detail != "NotReady" {
		t.Fatalf("node %+v", node)
	}

	if n.Problem == nil || n.Problem.Kind != "Node" || n.Problem.Name != "demo-worker-3" {
		t.Fatalf("problem %+v", n.Problem)
	}

	// Worst first within a layer.
	pods := []string{}
	for _, node := range n.Nodes {
		if node.Kind == "Pod" {
			pods = append(pods, node.Name)
		}
	}

	if pods[0] != "worker-6f4b8-uvwxy" {
		t.Fatalf("pods order %v", pods)
	}
}

func TestArgoNetworkGateway(t *testing.T) {
	n := demoArgoNetwork("grafana")

	gw := netNode(t, n, "gw/traefik/homelab")
	if gw.Managed || gw.Detail != "Traefik · 10.0.0.240" || gw.Layer != argoLayerGateway {
		t.Fatalf("gateway %+v", gw)
	}

	hr := netNode(t, n, "hr/monitoring/grafana")
	if !hr.Managed || hr.Layer != argoLayerRoute {
		t.Fatalf("route %+v", hr)
	}

	got := netEdges(n)
	for _, want := range []string{"host/https://grafana.homelab.lan>gw/traefik/homelab", "gw/traefik/homelab>hr/monitoring/grafana", "hr/monitoring/grafana>svc/monitoring/grafana"} {
		if !strings.Contains(got, want) {
			t.Errorf("edges lack %s: %s", want, got)
		}
	}
}

// The demo TLSRoute passes through the same Gateway on its TLS listener: host → Gateway →
// TLSRoute → Service, managed by the app.
func TestArgoNetworkTLSRoute(t *testing.T) {
	n := demoArgoNetwork("grafana")

	tr := netNode(t, n, "tr/monitoring/grafana")
	if !tr.Managed || tr.Kind != "TLSRoute" || tr.Layer != argoLayerRoute {
		t.Fatalf("route %+v", tr)
	}

	got := netEdges(n)
	for _, want := range []string{
		"host/https://grafana.homelab.lan:8443>gw/traefik/homelab",
		"gw/traefik/homelab>tr/monitoring/grafana",
		"tr/monitoring/grafana>svc/monitoring/grafana",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("edges lack %s: %s", want, got)
		}
	}
}

// An HTTPRoute and a GRPCRoute may share a namespace and a name (the demo's grafana pair is an
// HTTPRoute and a TLSRoute): each attaches to its own Gateways, so a parent lookup matches the
// kind too.
func TestGatewayRouteParentsMatchKind(t *testing.T) {
	http := demoNetObject[gatewayRouteObject](`{"metadata":{"name":"grafana","namespace":"monitoring"},
	  "spec":{"parentRefs":[{"name":"web","namespace":"traefik"}]}}`)
	http.Kind = "HTTPRoute"

	grpc := demoNetObject[gatewayRouteObject](`{"metadata":{"name":"grafana","namespace":"monitoring"},
	  "spec":{"parentRefs":[{"name":"grpc","namespace":"traefik"}]}}`)
	grpc.Kind = "GRPCRoute"

	routes := []gatewayRouteObject{http, grpc}
	gateways := []gatewayObject{
		demoNetObject[gatewayObject](`{"metadata":{"name":"web","namespace":"traefik"}}`),
		demoNetObject[gatewayObject](`{"metadata":{"name":"grpc","namespace":"traefik"}}`),
	}

	for _, tc := range []struct{ kind, want string }{
		{"HTTPRoute", "web"},
		{"GRPCRoute", "grpc"},
	} {
		got := gatewayRouteParents(routes, gateways, tc.kind, "monitoring", "grafana")
		if len(got) != 1 || got[0].Metadata.Name != tc.want {
			t.Errorf("%s parents %+v, want %s", tc.kind, got, tc.want)
		}
	}

	if got := gatewayRouteParents(routes, gateways, "TLSRoute", "monitoring", "grafana"); got != nil {
		t.Errorf("TLSRoute parents %+v, want none", got)
	}
}

func TestArgoNetworkLoadBalancerAndWarnings(t *testing.T) {
	n := demoArgoNetwork("traefik")
	if lb := netNode(t, n, "lb/10.0.0.240"); lb.Kind != "LoadBalancer" || lb.Detail != "LoadBalancer" {
		t.Fatalf("lb %+v", lb)
	}

	if svc := netNode(t, n, "svc/traefik/traefik"); svc.Detail != "LoadBalancer 10.96.0.80 · 80→web, 443→websecure" {
		t.Fatalf("service %+v", svc)
	}

	n = demoArgoNetwork("longhorn")
	if n.Problem == nil || n.Problem.Kind != "Node" {
		t.Fatalf("problem %+v", n.Problem)
	}

	if p := netNode(t, n, "pod/longhorn-system/longhorn-ui-5b7c-r3m6v"); p.Health != healthWarning {
		t.Fatalf("pod %+v", p)
	}

	if n = demoArgoNetwork("unknown"); len(n.Nodes) != 0 || n.Problem != nil {
		t.Fatalf("unknown app %+v", n)
	}
}

func TestArgoNetworkServiceWithoutPods(t *testing.T) {
	in := argoNetInput{}
	in.app.Status.Resources = []argoResourceStatus{{Kind: "Service", Namespace: "web", Name: "web"}, {Kind: "Service", Namespace: "web", Name: "external"}}
	in.services = []netmap.Service{
		demoNetObject[netmap.Service](`{"metadata":{"name":"web","namespace":"web"},"spec":{"selector":{"app":"web"},"ports":[{"port":80,"nodePort":30080,"targetPort":80}]}}`),
		demoNetObject[netmap.Service](`{"metadata":{"name":"external","namespace":"web"},"spec":{"type":"ExternalName"}}`),
		demoNetObject[netmap.Service](`{"metadata":{"name":"other","namespace":"web"},"spec":{"selector":{"app":"x"}}}`), // not the app's
	}

	n := buildArgoNetwork(in)

	if len(n.Nodes) != 2 {
		t.Fatalf("nodes %+v", n.Nodes)
	}

	if web := netNode(t, n, "svc/web/web"); web.Health != healthCritical || web.Detail != "ClusterIP · 80 (node 30080)" {
		t.Fatalf("web %+v", web)
	}

	if ext := netNode(t, n, "svc/web/external"); ext.Health != healthIdle {
		t.Fatalf("external %+v", ext)
	}

	if n.Problem == nil || n.Problem.Kind != "Service" || n.Problem.Name != "web" {
		t.Fatalf("problem %+v", n.Problem)
	}
}

func TestReadArgoNetwork(t *testing.T) {
	const app = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/web"

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET " + app: `{"metadata":{"name":"web","namespace":"argocd"},"spec":{"destination":{"namespace":"web"}},
		  "status":{"resources":[{"kind":"Service","namespace":"web","name":"web"},{"group":"networking.k8s.io","kind":"Ingress","namespace":"web","name":"web"}]}}`,
		"GET /api/v1/namespaces/web/services": `{"items":[{"metadata":{"name":"web","namespace":"web"},"spec":{"type":"ClusterIP","clusterIP":"10.96.0.9","selector":{"app":"web"},"ports":[{"port":80,"targetPort":8080}]}}]}`,
		"GET /api/v1/namespaces/web/pods": `{"items":[
		  {"metadata":{"name":"web-1","namespace":"web","labels":{"app":"web"}},"spec":{"nodeName":"node-1","containers":[{"image":"web:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}},
		  {"metadata":{"name":"db-0","namespace":"web","labels":{"app":"db"}},"spec":{"nodeName":"node-2","containers":[{"image":"db:1"}]},"status":{"phase":"Running"}}]}`,
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[{"metadata":{"name":"web","namespace":"web"},
		  "spec":{"rules":[{"host":"web.example.com","http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"web"}}}]}}]}}]}`,
		"GET /api/v1/nodes": `{"items":[{"metadata":{"name":"node-1"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	n, err := readArgoNetwork(context.Background(), k, "argocd", "web")
	if err != nil {
		t.Fatal(err)
	}

	got := netEdges(n)
	want := "host/http://web.example.com>ing/web/web:ok ing/web/web>svc/web/web:ok svc/web/web>pod/web/web-1:ok pod/web/web-1>node/node-1:ok"

	if got != want || n.Problem != nil {
		t.Fatalf("edges\n got %s\nwant %s\nproblem %+v", got, want, n.Problem)
	}
}

func TestKubeArgoNetworkDemoAndValidation(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoNetwork(cfg, "", "", "argocd", "hello-ichor")
	if err != nil {
		t.Fatal(err)
	}

	var n netmap.Map
	if err := json.Unmarshal([]byte(out), &n); err != nil || len(n.Nodes) == 0 {
		t.Fatalf("demo: %v %s", err, out)
	}

	if _, err := KubeArgoNetwork(cfg, "", "", " ", "x"); err == nil {
		t.Fatal("empty namespace accepted")
	}
}
