package ichorgo

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestTraefikMatch(t *testing.T) {
	cases := []struct {
		match, hosts, path string
	}{
		{"Host(`www.shop.example`) && PathPrefix(`/shop`)", "www.shop.example", "/shop"},
		{"Host(`a.example`, `b.example`)", "a.example b.example", ""},
		{"Host(`a.example`) || Host(`b.example`) && Path(`/x`)", "a.example b.example", "/x"},
		{`Host("quoted.example")`, "quoted.example", ""},
		{"Host(`n.example`) && !Host(`x.example`) && !PathPrefix(`/private`)", "n.example", ""},
		{"HostRegexp(`^.+\\.example$`)", "", ""},
		{"Host(`*.wild.example`)", "", ""},
		{"Host(`api.example`) && PathPrefix(`/v{version:[0-9]+}`)", "api.example", ""},
		{"Host(`api.example`) && PathRegexp(`^/v[0-9]+`) && PathPrefix(`/docs`)", "api.example", "/docs"},
		{"HostSNI(`tcp.example`)", "tcp.example", ""},
		{"HostSNI(`*`)", "", ""},
		{"PathPrefix(`/only`)", "", "/only"},
		{"", "", ""},
	}

	for _, c := range cases {
		hosts, path := traefikMatch(c.match)
		if got := strings.Join(hosts, " "); got != c.hosts || path != c.path {
			t.Errorf("%s: got %q %q, want %q %q", c.match, got, path, c.hosts, c.path)
		}
	}
}

func TestAppRoutesTraefikIngressRoute(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/traefik.io/v1alpha1/ingressroutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{"entryPoints":["websecure"],"routes":[
				{"match":"Host(` + "`www.shop.example`" + `) && PathPrefix(` + "`/shop`" + `)","kind":"Rule","services":[{"name":"web","port":80}]}]}},
			{"metadata":{"name":"multi","namespace":"shop"},"spec":{"entryPoints":["web"],"routes":[
				{"match":"Host(` + "`a.example`, `b.example`" + `)","services":[{"name":"web-admin","port":80}]}]}},
			{"metadata":{"name":"tls","namespace":"shop"},"spec":{"routes":[
				{"match":"Host(` + "`secure.example`" + `)","services":[{"name":"web"}]}],"tls":{"secretName":"secure-tls"}}},
			{"metadata":{"name":"metrics","namespace":"shop"},"spec":{"entryPoints":["metrics"],"routes":[
				{"match":"Host(` + "`metrics.example`" + `)","services":[{"name":"web"}]}]}},
			{"metadata":{"name":"regex","namespace":"shop"},"spec":{"routes":[
				{"match":"HostRegexp(` + "`^.+\\\\.example$`" + `)","services":[{"name":"web"}]}]}},
			{"metadata":{"name":"weighted","namespace":"shop"},"spec":{"routes":[
				{"match":"Host(` + "`weighted.example`" + `)","services":[{"name":"web","kind":"TraefikService"}]}]}},
			{"metadata":{"name":"cross","namespace":"edge"},"spec":{"routes":[
				{"match":"Host(` + "`cross.example`" + `)","services":[{"name":"web","namespace":"shop"}]}]}},
			{"metadata":{"name":"unrelated","namespace":"shop"},"spec":{"routes":[
				{"match":"Host(` + "`db.example`" + `)","services":[{"name":"db"}]}]}}]}`,
		"GET /api/v1/services": `{"items":[
			{"metadata":{"name":"traefik","namespace":"traefik"},"spec":{"type":"LoadBalancer","ports":[
				{"name":"web","port":80,"protocol":"TCP"},{"name":"websecure","port":443,"protocol":"TCP"},
				{"name":"metrics","port":9100,"protocol":"TCP"}]},
			 "status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.20"}]}}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"IngressRoute http://a.example web-admin",
		"IngressRoute http://b.example web-admin",
		"IngressRoute http://cross.example web",
		"IngressRoute http://metrics.example:9100 web",
		"IngressRoute https://secure.example web",
		"IngressRoute https://www.shop.example/shop web",
	}, "\n")
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestAppRoutesTraefikIngressRouteTCP(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/traefik.io/v1alpha1/ingressroutetcps": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{"entryPoints":["websecure"],"routes":[
				{"match":"HostSNI(` + "`tcp.example`" + `)","services":[{"name":"web","port":443}]}],"tls":{"passthrough":true}}},
			{"metadata":{"name":"catch-all","namespace":"shop"},"spec":{"entryPoints":["websecure"],"routes":[
				{"match":"HostSNI(` + "`*`" + `)","services":[{"name":"web","port":443}]}]}},
			{"metadata":{"name":"db","namespace":"shop"},"spec":{"entryPoints":["postgres"],"routes":[
				{"match":"HostSNI(` + "`pg.example`" + `)","services":[{"name":"db","port":5432}]}]}}]}`,
		"GET /api/v1/services": `{"items":[
			{"metadata":{"name":"traefik","namespace":"traefik"},"spec":{"type":"LoadBalancer","ports":[{"name":"postgres","port":5432}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}, {"shop", "db-0"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := routeURLs(list.Routes); got != "IngressRouteTCP https://tcp.example web" {
		t.Fatalf("got %q", got)
	}
}

func TestAppRoutesWithoutTraefik(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods/db-0":    routesDBPod,
		"GET /api/v1/namespaces/shop/services":     routesServices,
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[{"metadata":{"name":"db","namespace":"shop"},"spec":{"defaultBackend":{"service":{"name":"db"}}},"status":{"loadBalancer":{"ingress":[{"hostname":"lb.example"}]}}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "db-0"}})
	if err != nil || routeURLs(list.Routes) != "Ingress http://lb.example db" {
		t.Fatalf("got %v %+v", err, list)
	}

	asked := slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return strings.Contains(r.path, "ingressroutes") })
	if !asked {
		t.Fatal("Traefik's routes were not asked")
	}
	// No route on a custom entryPoint: the LoadBalancer Services are not listed.
	if slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.path == "/api/v1/services" }) {
		t.Fatal("LoadBalancer Services listed without a custom entryPoint")
	}
}

func TestKubeAppRoutesDemoIngressRoute(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeAppRoutes(cfg, "", "", `[{"namespace":"monitoring","pod":"grafana-6d8b-9fz2t"}]`)
	if err != nil || !strings.Contains(out, `"kind":"IngressRoute"`) || !strings.Contains(out, "https://grafana.home.example") {
		t.Fatalf("demo routes: %v %s", err, out)
	}
}

func TestTraefikEntriesAmbiguousPort(t *testing.T) {
	services := []lbPortService{
		demoNetObject[lbPortService](`{"metadata":{"name":"a"},"spec":{"ports":[{"name":"web","port":80},{"name":"metrics","port":9100}]},"status":{"loadBalancer":{"ingress":[{"hostname":"a.lb.example"}]}}}`),
		demoNetObject[lbPortService](`{"metadata":{"name":"b"},"spec":{"ports":[{"name":"metrics","port":9200},{"name":"dns","port":53,"protocol":"UDP"}]}}`),
	}

	entries := traefikEntries(services)
	if entries["web"] != (traefikEntry{80, "a.lb.example"}) || entries["metrics"].port != 0 {
		t.Fatalf("entries %+v", entries)
	}
	if _, ok := entries["dns"]; ok {
		t.Fatal("a UDP port became an entryPoint")
	}
}

func TestRoutesOfServiceTraefik(t *testing.T) {
	set := traefikRouteSet{http: []ingressRouteObject{
		demoNetObject[ingressRouteObject](`{"metadata":{"name":"web","namespace":"shop"},"spec":{"entryPoints":["websecure"],"routes":[
			{"match":"Host(` + "`www.shop.example`" + `)","services":[{"name":"web"},{"name":"web-admin"}]}]}}`),
	}}

	got := routeURLs(routesOf(serviceRef{"shop", "web-admin"}, serviceRoutes{traefik: set}))
	if got != "IngressRoute https://www.shop.example web-admin" {
		t.Fatalf("got %q", got)
	}
}

func TestArgoNetworkIngressRoute(t *testing.T) {
	n := demoArgoNetwork("grafana")

	ir := netNode(t, n, "ir/monitoring/grafana")
	if !ir.Managed || ir.Layer != argoLayerRoute || ir.Kind != "IngressRoute" || ir.Detail != "10.0.0.240" {
		t.Fatalf("route %+v", ir)
	}

	got := netEdges(n)
	for _, want := range []string{"host/https://grafana.home.example>ir/monitoring/grafana", "ir/monitoring/grafana>svc/monitoring/grafana"} {
		if !strings.Contains(got, want) {
			t.Errorf("edges lack %s: %s", want, got)
		}
	}

	// A route the app does not own is drawn as shared.
	in := argoNetInput{traefik: traefikRouteSet{tcp: []ingressRouteTCPObject{demoNetObject[ingressRouteTCPObject](`{"metadata":{"name":"edge","namespace":"web"},
	  "spec":{"routes":[{"match":"HostSNI(` + "`tls.example`" + `)","services":[{"name":"web"}]}]}}`)}}}
	in.app.Status.Resources = []argoResourceStatus{{Kind: "Service", Namespace: "web", Name: "web"}}
	in.services = []netService{demoNetObject[netService](`{"metadata":{"name":"web","namespace":"web"},"spec":{"type":"ClusterIP","selector":{"app":"web"},"ports":[{"port":443}]}}`)}

	shared := netNode(t, buildArgoNetwork(in), "irtcp/web/edge")
	if shared.Managed || shared.Kind != "IngressRouteTCP" {
		t.Fatalf("shared route %+v", shared)
	}
}
