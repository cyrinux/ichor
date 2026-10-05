package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func netNode(t *testing.T, n argoNetwork, id string) argoNetNode {
	t.Helper()

	for _, node := range n.Nodes {
		if node.ID == id {
			return node
		}
	}

	t.Fatalf("no node %q in %+v", id, n.Nodes)

	return argoNetNode{}
}

func netEdges(n argoNetwork) string {
	var out []string
	for _, e := range n.Edges {
		out = append(out, e.From+">"+e.To+":"+e.Health)
	}

	return strings.Join(out, " ")
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
		services: []netService{demoNetObject[netService](`{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"ClusterIP"}}`)},
		ingresses: []ingressObject{demoNetObject[ingressObject](`{"metadata":{"name":"web","namespace":"shop"},
			"spec":{"ingressClassName":"tailscale","tls":[{"hosts":["web"]}],"defaultBackend":{"service":{"name":"web"}}},
			"status":{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}}`)},
	}
	in.app.Status.Resources = []argoResourceStatus{{Kind: "Service", Namespace: "shop", Name: "web"}, {Kind: "Ingress", Namespace: "shop", Name: "web"}}
	n := buildArgoNetwork(in)
	host := netNode(t, n, "host/https://web.tail123.ts.net")
	if host.URL != "https://web.tail123.ts.net" || host.Name != "web.tail123.ts.net" {
		t.Fatalf("host %+v", host)
	}
	if got := netEdges(n); !strings.Contains(got, "host/https://web.tail123.ts.net>ing/shop/web") || !strings.Contains(got, "ing/shop/web>svc/shop/web") {
		t.Fatalf("edges %s", got)
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
	if gw.Managed || gw.Detail != "10.0.0.240" || gw.Layer != argoLayerGateway {
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
	in.services = []netService{
		demoNetObject[netService](`{"metadata":{"name":"web","namespace":"web"},"spec":{"selector":{"app":"web"},"ports":[{"port":80,"nodePort":30080,"targetPort":80}]}}`),
		demoNetObject[netService](`{"metadata":{"name":"external","namespace":"web"},"spec":{"type":"ExternalName"}}`),
		demoNetObject[netService](`{"metadata":{"name":"other","namespace":"web"},"spec":{"selector":{"app":"x"}}}`), // not the app's
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

func TestPodHealth(t *testing.T) {
	cases := map[string]string{
		"Running": healthWarning, "Pending": healthWarning, "Init:0/1": healthWarning, "Completed": healthIdle,
		"CrashLoopBackOff": healthCritical, "ImagePullBackOff": healthCritical, "Init:CrashLoopBackOff": healthCritical, "Error": healthCritical,
	}

	for status, want := range cases {
		if got := podHealth(kubePod{Status: status}); got != want {
			t.Errorf("%s: %s, want %s", status, got, want)
		}
	}

	if podHealth(kubePod{Status: "Running", Healthy: true}) != healthOK {
		t.Error("healthy pod")
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

	var n argoNetwork
	if err := json.Unmarshal([]byte(out), &n); err != nil || len(n.Nodes) == 0 {
		t.Fatalf("demo: %v %s", err, out)
	}

	if _, err := KubeArgoNetwork(cfg, "", "", " ", "x"); err == nil {
		t.Fatal("empty namespace accepted")
	}
}
