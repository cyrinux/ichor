package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

// The healthy demo address: tailnet name → ProxyGroup → one proxy per replica, each with its
// device, tailnet IP, node and zone → Service → pods → nodes, nothing to blame.
func TestTailscaleNetworkProxyGroup(t *testing.T) {
	n := demoTailscaleNetwork(tsKindService, "shop", "storefront")

	name := netNode(t, n, "ext/Tailscale/storefront.example-tailnet.ts.net")
	if name.Layer != 0 || name.Kind != netKindExternalName || name.Health != healthOK || name.Detail != "Tailscale · 100.64.0.51" ||
		name.URL != "https://storefront.example-tailnet.ts.net" {
		t.Fatalf("name %+v", name)
	}

	if pg := netNode(t, n, "pg/ingress-proxies"); pg.Layer != 1 || pg.Health != healthOK || pg.Detail != "ingress · 2/2 ready" {
		t.Fatalf("proxy group %+v", pg)
	}

	p := netNode(t, n, "proxy/tailscale/ingress-proxies-0")
	if p.Layer != 2 || p.Health != healthOK || p.Detail != "Running · ingress-0 · 100.64.0.31 · demo-worker-1/zone-a" {
		t.Fatalf("proxy %+v", p)
	}

	if svc := netNode(t, n, "svc/shop/storefront"); svc.Layer != 3 || !svc.Managed || svc.Health != healthOK {
		t.Fatalf("service %+v", svc)
	}

	got := netEdges(n)
	for _, want := range []string{
		"ext/Tailscale/storefront.example-tailnet.ts.net>pg/ingress-proxies:ok",
		"pg/ingress-proxies>proxy/tailscale/ingress-proxies-1:ok",
		"proxy/tailscale/ingress-proxies-1>svc/shop/storefront:ok",
		"svc/shop/storefront>pod/shop/storefront-7f9c4-h2kq8:ok",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("edges lack %s: %s", want, got)
		}
	}

	if n.Problem != nil {
		t.Fatalf("problem %+v", n.Problem)
	}
}

// An Ingress with a proxy of its own that crash-loops: no ProxyGroup column, the Ingress box
// between the proxy and its Service, and the proxy named as the root cause.
func TestTailscaleNetworkCrashingProxy(t *testing.T) {
	n := demoTailscaleNetwork(tsKindIngress, "docs", "wiki")

	layers := map[string]int{}
	for _, node := range n.Nodes {
		layers[node.Kind] = node.Layer
	}

	if layers[netKindExternalName] != 0 || layers[netKindProxy] != 1 || layers[tsKindIngress] != 2 || layers[netmap.KindService] != 3 {
		t.Fatalf("layers %v", layers)
	}

	if _, ok := layers[netKindProxyGroup]; ok {
		t.Fatalf("a proxy of its own has no ProxyGroup box: %+v", n.Nodes)
	}

	if n.Problem == nil || n.Problem.Kind != netKindProxy || n.Problem.Name != "ts-wiki-q9d4m-0" ||
		!strings.HasPrefix(n.Problem.Detail, "CrashLoopBackOff · 23 restarts") {
		t.Fatalf("problem %+v", n.Problem)
	}

	if name := netNode(t, n, "ext/Tailscale/wiki.example-tailnet.ts.net"); name.Health != healthCritical {
		t.Fatalf("name %+v", name)
	}
}

// Egress reads left to right as the traffic goes: Service → proxy → tailnet target.
func TestTailscaleNetworkEgressOrder(t *testing.T) {
	n := demoTailscaleNetwork(tsKindService, "payments", "ledger-db")

	want := "svc/payments/ledger-db>proxy/tailscale/ts-ledger-db-m2v8c-0:ok proxy/tailscale/ts-ledger-db-m2v8c-0>ext/Tailscale/ledger-db.example-tailnet.ts.net:ok"
	if got := netEdges(n); got != want || n.Problem != nil {
		t.Fatalf("edges\n got %s\nwant %s\nproblem %+v", got, want, n.Problem)
	}

	if target := netNode(t, n, "ext/Tailscale/ledger-db.example-tailnet.ts.net"); target.Layer != 2 || target.URL != "" {
		t.Fatalf("target %+v", target)
	}

	// No proxy was provisioned: the Service is broken, the target too (the operator says so).
	n = demoTailscaleNetwork(tsKindService, "payments", "legacy")
	if n.Problem == nil || n.Problem.Kind != netmap.KindService || n.Problem.Name != "legacy" {
		t.Fatalf("problem %+v", n.Problem)
	}

	if target := netNode(t, n, "ext/Tailscale/100.64.0.77"); target.Health != healthCritical || !strings.Contains(target.Detail, "ProxyFailed") {
		t.Fatalf("target %+v", target)
	}
}

func TestTailscaleNetworkMissingGroupAndUnknown(t *testing.T) {
	n := demoTailscaleNetwork(tsKindService, "media", "player")
	if pg := netNode(t, n, "pg/web-proxies"); pg.Health != healthCritical || pg.Detail != "not found" {
		t.Fatalf("proxy group %+v", pg)
	}

	if n.Problem == nil || n.Problem.Kind != netKindProxyGroup {
		t.Fatalf("problem %+v", n.Problem)
	}

	if n = demoTailscaleNetwork(tsKindService, "shop", "nope"); len(n.Nodes) != 0 || n.Problem != nil {
		t.Fatalf("unknown %+v", n)
	}
}

func tsTestProxy(name, node, status, parentType, parent, parentNS string) netmap.Pod {
	p := kubePod{Namespace: "tailscale", Name: name, Node: node, Status: status, Containers: 1}
	if status == "Running" {
		p.Ready, p.Healthy = 1, true
	}

	return netmap.Pod{Pod: p, Labels: map[string]string{
		tsLabelManaged: "true", tsLabelParentType: parentType, tsLabelParent: parent, tsLabelParentNS: parentNS,
	}}
}

// Proxies by their parent labels: a ProxyGroup's by its name, a proxy of its own by the
// object's kind, namespace and name.
func TestTailscaleProxiesOf(t *testing.T) {
	pods := []netmap.Pod{
		tsTestProxy("pg-1", "a", "Running", tsParentGroup, "pg", ""),
		tsTestProxy("pg-0", "a", "Running", tsParentGroup, "pg", ""),
		tsTestProxy("other-0", "a", "Running", tsParentGroup, "other", ""),
		tsTestProxy("ts-web-0", "a", "Running", "svc", "web", "shop"),
		tsTestProxy("ts-web-x", "a", "Running", "svc", "web", "blog"),
		tsTestProxy("ts-web-i", "a", "Running", "ingress", "web", "shop"),
	}

	names := func(ps []netmap.Pod) string {
		var out []string
		for _, p := range ps {
			out = append(out, p.Name)
		}

		return strings.Join(out, ",")
	}

	for _, tc := range []struct {
		row  tsExposed
		want string
	}{
		{tsExposed{Kind: tsKindService, Namespace: "shop", Name: "web", ProxyGroup: "pg"}, "pg-0,pg-1"},
		{tsExposed{Kind: tsKindService, Namespace: "shop", Name: "web"}, "ts-web-0"},
		{tsExposed{Kind: tsKindIngress, Namespace: "shop", Name: "web"}, "ts-web-i"},
		{tsExposed{Kind: tsKindService, Namespace: "docs", Name: "web"}, ""},
	} {
		if got := names(tsProxiesOf(tc.row, pods)); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.row, got, tc.want)
		}
	}
}

// Devices pair with pods by index under the hostname prefix, or by the pod's own name; what
// does not pair is not guessed.
func TestTailscaleMatchDevices(t *testing.T) {
	group := demoNetObject[tsProxyGroupObject](`{"metadata":{"name":"pg"},"spec":{"hostnamePrefix":"web"},
	  "status":{"devices":[{"hostname":"web-0","tailnetIPs":["100.64.1.1"]},{"hostname":"pg-1"},{"hostname":"web-7"}]}}`)
	pods := []netmap.Pod{
		tsTestProxy("pg-0", "a", "Running", tsParentGroup, "pg", ""),
		tsTestProxy("pg-1", "a", "Running", tsParentGroup, "pg", ""),
		tsTestProxy("pg-2", "a", "Running", tsParentGroup, "pg", ""),
	}

	paired, unmatched := tsMatchDevices(group, pods)
	if paired["pg-0"].Hostname != "web-0" || paired["pg-1"].Hostname != "pg-1" || len(paired) != 2 {
		t.Fatalf("paired %+v", paired)
	}

	if len(unmatched) != 1 || unmatched[0].Hostname != "web-7" {
		t.Fatalf("unmatched %+v", unmatched)
	}

	// Without a prefix, the group's name.
	group.Spec.HostnamePrefix = ""
	group.Status.Devices = []tsDeviceObject{{Hostname: "pg-2"}}

	if paired, _ = tsMatchDevices(group, pods); paired["pg-2"].Hostname != "pg-2" {
		t.Fatalf("paired %+v", paired)
	}
}

// Two proxies running, both on one node: the ProxyGroup is a warning, and the root cause when
// nothing deeper is broken. An unpaired device shows, idle.
func TestTailscaleNetworkOneNode(t *testing.T) {
	in := tsNetInput{
		kind: tsKindService,
		object: demoTSService(`"name":"web","namespace":"shop","annotations":{"tailscale.com/proxy-group":"pg"}`,
			`"type":"LoadBalancer","loadBalancerClass":"tailscale"`,
			`"loadBalancer":{"ingress":[{"hostname":"web.example-tailnet.ts.net"}]},"conditions":[{"type":"TailscaleIngressSvcConfigured","status":"True"}]`),
		services: []netmap.Service{demoNetObject[netmap.Service](`{"metadata":{"name":"web","namespace":"shop"},"spec":{"selector":{"app":"web"}}}`)},
		pods:     []netmap.Pod{{Pod: kubePod{Namespace: "shop", Name: "web-1", Node: "b", Status: "Running", Healthy: true}, Labels: map[string]string{"app": "web"}}},
		proxies: []netmap.Pod{
			tsTestProxy("pg-0", "a", "Running", tsParentGroup, "pg", ""),
			tsTestProxy("pg-1", "a", "Running", tsParentGroup, "pg", ""),
		},
		groups: []tsProxyGroupObject{demoNetObject[tsProxyGroupObject](`{"metadata":{"name":"pg"},"spec":{"type":"ingress","replicas":2},
		  "status":{"devices":[{"hostname":"pg-0"},{"hostname":"pg-1"},{"hostname":"pg-9","tailnetIPs":["100.64.9.9"]}]}}`)},
	}

	n := buildTailscaleNetwork(in)

	if pg := netNode(t, n, "pg/pg"); pg.Health != healthWarning || pg.Detail != "ingress · 2/2 ready · all on a" {
		t.Fatalf("proxy group %+v", pg)
	}

	if n.Problem == nil || n.Problem.Kind != netKindProxyGroup {
		t.Fatalf("problem %+v", n.Problem)
	}

	if d := netNode(t, n, "device/pg-9"); d.Health != healthIdle || d.Detail != "100.64.9.9" {
		t.Fatalf("device %+v", d)
	}

	// One replica down: the group is as good as its best proxy, and no one-node warning.
	in.proxies[1] = tsTestProxy("pg-1", "a", "CrashLoopBackOff", tsParentGroup, "pg", "")
	if pg := netNode(t, buildTailscaleNetwork(in), "pg/pg"); pg.Health != healthOK || pg.Detail != "ingress · 1/2 ready" {
		t.Fatalf("proxy group %+v", pg)
	}
}

// The name (the operator's condition) is the root cause only when nothing in the cluster is.
func TestTailscaleRootCauseNameLast(t *testing.T) {
	m := netmap.Map{Nodes: []netmap.Node{
		{Kind: netKindExternalName, Name: "x", Layer: 3, Health: healthCritical},
		{Kind: netKindProxy, Name: "p", Layer: 1, Health: healthWarning},
	}}

	if p := tsRootCause(m); p == nil || p.Name != "p" {
		t.Fatalf("problem %+v", p)
	}

	m.Nodes[1].Health = healthOK
	if p := tsRootCause(m); p == nil || p.Name != "x" {
		t.Fatalf("problem %+v", p)
	}
}

func TestReadTailscaleNetwork(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"tailscale.com","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET /api/v1/namespaces/shop/services/web": `{"metadata":{"name":"web","namespace":"shop","annotations":{"tailscale.com/proxy-group":"pg"}},
		  "spec":{"type":"LoadBalancer","loadBalancerClass":"tailscale","selector":{"app":"web"}},
		  "status":{"loadBalancer":{"ingress":[{"hostname":"web.example-tailnet.ts.net"}]},"conditions":[{"type":"TailscaleIngressSvcConfigured","status":"True"}]}}`,
		"GET /api/v1/namespaces/shop/services": `{"items":[{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"LoadBalancer","clusterIP":"10.96.0.9","selector":{"app":"web"},"ports":[{"port":80}]}}]}`,
		"GET /api/v1/namespaces/shop/pods": `{"items":[{"metadata":{"name":"web-1","namespace":"shop","labels":{"app":"web"}},"spec":{"nodeName":"node-1","containers":[{"image":"web:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}}]}`,
		"GET /api/v1/pods": `{"items":[{"metadata":{"name":"pg-0","namespace":"tailscale","labels":{"tailscale.com/managed":"true","tailscale.com/parent-resource":"pg","tailscale.com/parent-resource-type":"proxygroup"}},
		   "spec":{"nodeName":"node-1","containers":[{"image":"tailscale:1"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}}]}`,
		"GET /apis/tailscale.com/v1alpha1/proxygroups": `{"items":[{"metadata":{"name":"pg"},"spec":{"type":"ingress","replicas":1},
		  "status":{"devices":[{"hostname":"pg-0","tailnetIPs":["100.64.0.5"]}]}}]}`,
		"GET /api/v1/nodes": `{"items":[{"metadata":{"name":"node-1","labels":{"topology.kubernetes.io/zone":"z1"}},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	n, err := readTailscaleNetwork(context.Background(), k, tsKindService, "shop", "web")
	if err != nil {
		t.Fatal(err)
	}

	got := netEdges(n)
	want := "ext/Tailscale/web.example-tailnet.ts.net>pg/pg:ok pg/pg>proxy/tailscale/pg-0:ok proxy/tailscale/pg-0>svc/shop/web:ok svc/shop/web>pod/shop/web-1:ok pod/shop/web-1>node/node-1:ok"

	if got != want || n.Problem != nil {
		t.Fatalf("edges\n got %s\nwant %s\nproblem %+v", got, want, n.Problem)
	}

	if p := netNode(t, n, "proxy/tailscale/pg-0"); p.Detail != "Running · pg-0 · 100.64.0.5 · node-1/z1" {
		t.Fatalf("proxy %+v", p)
	}
}

func TestKubeTailscaleNetworkDemoAndValidation(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	// Every exposed demo row has a map.
	for _, e := range demoTailscale().Exposed {
		out, err := KubeTailscaleNetwork(cfg, "", "", e.Kind, e.Namespace, e.Name)
		if err != nil {
			t.Fatal(err)
		}

		var n netmap.Map
		if err := json.Unmarshal([]byte(out), &n); err != nil || len(n.Nodes) == 0 {
			t.Fatalf("%s/%s: %v %s", e.Namespace, e.Name, err, out)
		}
	}

	if _, err := KubeTailscaleNetwork(cfg, "", "", "Pod", "shop", "web"); err == nil {
		t.Fatal("a Pod accepted")
	}

	if _, err := KubeTailscaleNetwork(cfg, "", "", tsKindService, " ", "x"); err == nil {
		t.Fatal("empty namespace accepted")
	}
}

// The matchers run only for the API groups the cluster serves, and a tailnet name opens over
// HTTPS for an Ingress or a Service on 443.
func TestExternalEntries(t *testing.T) {
	raw := json.RawMessage(`{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"LoadBalancer","loadBalancerClass":"tailscale","ports":[{"port":443}]},
	  "status":{"loadBalancer":{"ingress":[{"hostname":"web.example-tailnet.ts.net","ip":"100.64.0.9"}]},"conditions":[{"type":"TailscaleProxyReady","status":"True"}]}}`)

	if got := externalEntries(tsKindService, raw, map[string]string{}); len(got) != 0 {
		t.Fatalf("without tailscale.com %+v", got)
	}

	got := externalEntries(tsKindService, raw, map[string]string{groupTailscale: "v1alpha1"})
	want := externalEntry{
		Provider: tsProvider, Kind: externalIngress, Name: "web.example-tailnet.ts.net", URL: "https://web.example-tailnet.ts.net",
		Health: healthOK, Detail: "Tailscale · 100.64.0.9", Addresses: []string{"web.example-tailnet.ts.net", "100.64.0.9"},
	}

	if len(got) != 1 || got[0].Name != want.Name || got[0].URL != want.URL || got[0].Detail != want.Detail || got[0].Kind != want.Kind || len(got[0].Addresses) != 2 {
		t.Fatalf("entries %+v", got)
	}

	plain := json.RawMessage(`{"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"ClusterIP"}}`)
	if got := externalEntries(tsKindService, plain, map[string]string{groupTailscale: "v1alpha1"}); len(got) != 0 {
		t.Fatalf("not the operator's %+v", got)
	}
}
