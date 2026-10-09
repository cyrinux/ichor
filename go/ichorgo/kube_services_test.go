package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	servicesBody = `{"items":[
		{"metadata":{"name":"web","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},
		 "spec":{"type":"ClusterIP","clusterIP":"10.96.0.10","selector":{"app":"web"},"ports":[{"port":80,"protocol":"TCP"}]}},
		{"metadata":{"name":"web-public","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},
		 "spec":{"type":"LoadBalancer","clusterIP":"10.96.0.11","selector":{"app":"web"},"ports":[{"port":443,"nodePort":30443}]}},
		{"metadata":{"name":"fresh-lb","namespace":"shop","creationTimestamp":"2026-10-09T11:59:30Z"},
		 "spec":{"type":"LoadBalancer","clusterIP":"10.96.0.12","selector":{"app":"web"},"ports":[{"port":80,"protocol":"TCP"}]}},
		{"metadata":{"name":"edge","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},
		 "spec":{"type":"LoadBalancer","clusterIP":"10.96.0.13","externalIPs":["198.51.100.7"],"selector":{"app":"edge"},"ports":[{"port":80,"protocol":"TCP"}]},
		 "status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.50"}]}}},
		{"metadata":{"name":"queue","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},
		 "spec":{"clusterIP":"None","selector":{"app":"queue"},"ports":[{"port":5672,"protocol":"TCP"}]}},
		{"metadata":{"name":"legacy","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},
		 "spec":{"type":"ExternalName","externalName":"db.example.net"}}]}`
	serviceSlicesBody = `{"items":[
		{"metadata":{"name":"web-v4","namespace":"shop","labels":{"kubernetes.io/service-name":"web"}},"addressType":"IPv4",
		 "endpoints":[{"conditions":{"ready":true}},{"conditions":{}},{"conditions":{"ready":false}}]},
		{"metadata":{"name":"web-v6","namespace":"shop","labels":{"kubernetes.io/service-name":"web"}},"addressType":"IPv6",
		 "endpoints":[{"conditions":{"ready":true}},{"conditions":{"ready":true}},{"conditions":{"ready":false}}]},
		{"metadata":{"name":"edge-a","namespace":"shop","labels":{"kubernetes.io/service-name":"edge"}},"addressType":"IPv4",
		 "endpoints":[{"conditions":{"ready":true}}]},
		{"metadata":{"name":"queue-a","namespace":"shop","labels":{"kubernetes.io/service-name":"queue"}},"addressType":"IPv4",
		 "endpoints":[{"conditions":{"ready":false}}]}]}`
	serviceIngressesBody = `{"items":[
		{"metadata":{"name":"web","namespace":"shop"},"spec":{"tls":[{"hosts":["shop.example.org"]}],
		 "rules":[{"host":"shop.example.org","http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"web"}}}]}}]}}]}`
	serviceHTTPRoutesBody = `{"items":[
		{"metadata":{"name":"edge","namespace":"shop"},"spec":{"parentRefs":[{"name":"gw","namespace":"gateways"}],
		 "hostnames":["edge.example.org"],"rules":[{"backendRefs":[{"name":"edge"}]}]}}]}`
	serviceGatewaysBody = `{"items":[{"metadata":{"name":"gw","namespace":"gateways"},
		"spec":{"listeners":[{"name":"https","port":443,"protocol":"HTTPS"}]}}]}`
)

var servicesNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func servicesAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	return newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/services":                              servicesBody,
		"GET /apis/discovery.k8s.io/v1/namespaces/shop/endpointslices":      serviceSlicesBody,
		"GET /apis/networking.k8s.io/v1/namespaces/shop/ingresses":          serviceIngressesBody,
		"GET /apis/gateway.networking.k8s.io/v1/namespaces/shop/httproutes": serviceHTTPRoutesBody,
		"GET /apis/gateway.networking.k8s.io/v1/gateways":                   serviceGatewaysBody,
		"GET /apis": `{"groups":[{"name":"metallb.io","preferredVersion":{"version":"v1beta1"}}]}`,
	})
}

func serviceNamed(t *testing.T, s kubeServices, name string) serviceRow {
	t.Helper()

	for _, row := range s.Services {
		if row.Name == name {
			return row
		}
	}

	t.Fatalf("no service %q in %+v", name, s.Services)

	return serviceRow{}
}

func TestServicesJoinEndpointsAddressesAndRoutes(t *testing.T) {
	s, err := readServices(context.Background(), openFakeKube(t, servicesAPI(t)), "shop", servicesNow)
	if err != nil {
		t.Fatal(err)
	}

	// Dual stack: the family with the most endpoints counts, not both.
	web := serviceNamed(t, s, "web")
	if web.Endpoints != 3 || web.ReadyEndpoints != 2 || !web.EndpointsKnown || web.Level != serviceWarning {
		t.Errorf("web endpoints %+v", web)
	}

	if routeURLs(web.Routes) != "Ingress https://shop.example.org web" || strings.Join(web.Ports, ",") != "80/TCP" {
		t.Errorf("web routes %v, ports %v", web.Routes, web.Ports)
	}

	public := serviceNamed(t, s, "web-public")
	if !public.Pending || public.Level != serviceCritical || len(public.Addresses) != 0 || strings.Join(public.Ports, ",") != "443:30443/TCP" {
		t.Errorf("pending load balancer %+v", public)
	}

	// Still within the controller's grace: a warning, not yet critical.
	if fresh := serviceNamed(t, s, "fresh-lb"); !fresh.Pending || fresh.Level != serviceWarning {
		t.Errorf("fresh load balancer %+v", fresh)
	}

	edge := serviceNamed(t, s, "edge")
	if strings.Join(edge.Addresses, ",") != "192.0.2.50,198.51.100.7" || edge.Pending || edge.Level != serviceOK {
		t.Errorf("edge %+v", edge)
	}

	if routeURLs(edge.Routes) != "HTTPRoute https://edge.example.org edge" {
		t.Errorf("edge routes %v", edge.Routes)
	}

	if q := serviceNamed(t, s, "queue"); q.Type != "ClusterIP" || q.ClusterIP != "None" || q.ReadyEndpoints != 0 || q.Level != serviceWarning {
		t.Errorf("headless queue %+v", q)
	}

	// No selector: nobody expects endpoints.
	if legacy := serviceNamed(t, s, "legacy"); legacy.ExternalName != "db.example.net" || legacy.Level != serviceOK || legacy.Selector {
		t.Errorf("external name %+v", legacy)
	}

	if s.LBController != lbControllerMetalLB || s.PartialAccess {
		t.Errorf("controller %q, partial %v", s.LBController, s.PartialAccess)
	}

	var order []string
	for _, row := range s.Services {
		order = append(order, row.Name)
	}

	if strings.Join(order, ",") != "web-public,fresh-lb,queue,web,edge,legacy" {
		t.Errorf("order %v", order)
	}
}

func TestServicesWithoutSlicesOrRoutes(t *testing.T) {
	f := servicesAPI(t)
	for _, key := range []string{
		"GET /apis/discovery.k8s.io/v1/namespaces/shop/endpointslices",
		"GET /apis/networking.k8s.io/v1/namespaces/shop/ingresses",
	} {
		f.answerWith(key, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)
	}

	s, err := readServices(context.Background(), openFakeKube(t, f), "shop", servicesNow)
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Services) != 6 || !s.PartialAccess {
		t.Fatalf("services %d, partial %v", len(s.Services), s.PartialAccess)
	}

	// Unknown endpoints are no problem of their own.
	if web := serviceNamed(t, s, "web"); web.EndpointsKnown || web.Level != serviceOK || len(web.Routes) != 0 {
		t.Errorf("web %+v", web)
	}
}

func TestServicesWithoutGatewayAPI(t *testing.T) {
	f := servicesAPI(t)
	delete(f.answers, "GET /apis/gateway.networking.k8s.io/v1/namespaces/shop/httproutes")
	delete(f.answers, "GET /apis/gateway.networking.k8s.io/v1/gateways")

	s, err := readServices(context.Background(), openFakeKube(t, f), "shop", servicesNow)
	if err != nil {
		t.Fatal(err)
	}

	if s.PartialAccess || len(serviceNamed(t, s, "edge").Routes) != 0 {
		t.Errorf("without the Gateway API: partial %v, %+v", s.PartialAccess, serviceNamed(t, s, "edge"))
	}
}

func TestServicesRefusedIsAnError(t *testing.T) {
	f := servicesAPI(t)
	f.answerWith("GET /api/v1/namespaces/shop/services", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)

	if _, err := readServices(context.Background(), openFakeKube(t, f), "shop", servicesNow); err == nil {
		t.Fatal("no error")
	}
}

func TestLBControllerOf(t *testing.T) {
	for _, tc := range []struct {
		groups []string
		want   string
	}{
		{[]string{"metallb.io", "cilium.io"}, lbControllerMetalLB},
		{[]string{"cilium.io"}, lbControllerCilium},
		{[]string{"apps"}, ""},
	} {
		groups := map[string]string{}
		for _, g := range tc.groups {
			groups[g] = "v1"
		}

		if got := lbControllerOf(groups); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.groups, got, tc.want)
		}
	}
}

func TestKubeServicesDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeServices(cfg, "", "", "demo")
	if err != nil {
		t.Fatal(err)
	}

	var s kubeServices
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Services) == 0 {
		t.Fatalf("demo: %v %s", err, out)
	}

	for _, row := range s.Services {
		if row.Namespace != "demo" {
			t.Errorf("service of %s in the demo namespace's list", row.Namespace)
		}
	}

	// The checkup demo's pending load balancer comes first, with its pool hint.
	if first := s.Services[0]; first.Name != "hello-ichor-public" || first.Level != serviceCritical || s.LBController != lbControllerCilium {
		t.Errorf("demo first %+v, controller %q", first, s.LBController)
	}

	if hello := serviceNamed(t, s, "hello-ichor"); len(hello.Routes) == 0 {
		t.Errorf("the demo's routed app has no route: %+v", hello)
	}

	if _, err := KubeServices(cfg, "", "", "Bad Namespace"); err == nil {
		t.Error("a bad namespace was accepted")
	}
}
