package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const routesPods = `{"items":[
	{"metadata":{"name":"web-1","labels":{"app":"web","tier":"front"}}},
	{"metadata":{"name":"db-0","labels":{"app":"db"}}}]}`

// The same pods read one by one: an app's few pods are never found by listing their namespace.
const (
	routesWebPod = `{"metadata":{"name":"web-1","labels":{"app":"web","tier":"front"}}}`
	routesDBPod  = `{"metadata":{"name":"db-0","labels":{"app":"db"}}}`
)

const routesServices = `{"items":[
	{"metadata":{"name":"web"},"spec":{"selector":{"app":"web"}}},
	{"metadata":{"name":"web-admin"},"spec":{"selector":{"app":"web","tier":"front"}}},
	{"metadata":{"name":"db"},"spec":{"selector":{"app":"db"}}},
	{"metadata":{"name":"manual"},"spec":{}}]}`

func routeURLs(routes []kubeRoute) string {
	urls := make([]string, 0, len(routes))
	for _, r := range routes {
		urls = append(urls, r.Kind+" "+r.URL+" "+r.Service)
	}

	return strings.Join(urls, "\n")
}

func openRoutesKube(t *testing.T, answers map[string]string) *kubeClient {
	t.Helper()

	answers["GET /api/v1/namespaces/shop/pods/web-1"] = routesWebPod
	answers["GET /api/v1/namespaces/shop/pods/db-0"] = routesDBPod
	answers["GET /api/v1/namespaces/shop/services"] = routesServices
	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestAppRoutesIngress(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"tls":[{"hosts":["*.shop.example"]}],
				"rules":[
					{"host":"www.shop.example","http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"web"}}}]}},
					{"host":"admin.example","http":{"paths":[{"path":"/admin","pathType":"Prefix","backend":{"service":{"name":"web-admin"}}}]}},
					{"host":"*.wild.example","http":{"paths":[{"path":"/","backend":{"service":{"name":"web"}}}]}},
					{"host":"re.shop.example","http":{"paths":[{"path":"/api(/|$)(.*)","pathType":"ImplementationSpecific","backend":{"service":{"name":"web"}}}]}},
					{"http":{"paths":[{"path":"/","backend":{"service":{"name":"web"}}}]}}]},
				"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.10"}]}}},
			{"metadata":{"name":"db","namespace":"shop"},"spec":{"defaultBackend":{"service":{"name":"db"}}},
				"status":{"loadBalancer":{"ingress":[{"hostname":"lb.example"}]}}},
			{"metadata":{"name":"other","namespace":"elsewhere"},"spec":{"rules":[
				{"host":"other.example","http":{"paths":[{"path":"/","backend":{"service":{"name":"web"}}}]}}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}, {"shop", "gone"}})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"Ingress http://192.0.2.10 web",
		"Ingress http://admin.example/admin web-admin",
		"Ingress https://re.shop.example web",
		"Ingress https://www.shop.example web",
	}, "\n")
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// An HTTPRoute match on a method (a string, where a GRPCRoute's is an object) must not
// fail the decode: the route still yields its URL.
func TestAppRoutesHTTPRoute(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
				{"name":"http","port":80,"protocol":"HTTP"},
				{"name":"https","port":443,"protocol":"HTTPS","hostname":"*.shop.example"},
				{"name":"alt","port":8080,"protocol":"HTTP","hostname":"alt.example"}]},
				"status":{"addresses":[{"value":"198.51.100.7"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/httproutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw"}],
				"hostnames":["www.shop.example","plain.example"],
				"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/"},"method":"GET"}],"backendRefs":[{"name":"web","port":80}]}]}},
			{"metadata":{"name":"alt","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw","sectionName":"alt"}],
				"rules":[{"matches":[{"method":"POST"},{"path":{"type":"PathPrefix","value":"/app"}}],"backendRefs":[{"name":"web"}]}]}},
			{"metadata":{"name":"bare","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw","sectionName":"http"}],
				"rules":[{"backendRefs":[{"name":"db"},{"name":"web"}]}]}},
			{"metadata":{"name":"unknown-gw","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"missing"}],
				"hostnames":["nogw.example"],
				"rules":[{"backendRefs":[{"name":"web"}]}]}},
			{"metadata":{"name":"not-service","namespace":"shop"},"spec":{
				"hostnames":["bucket.example"],
				"rules":[{"backendRefs":[{"group":"example.io","kind":"Bucket","name":"web"}]}]}},
			{"metadata":{"name":"db","namespace":"shop"},"spec":{
				"hostnames":["db.example"],
				"rules":[{"backendRefs":[{"name":"db"}]}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"HTTPRoute http://198.51.100.7 web",
		"HTTPRoute http://alt.example:8080/app web",
		"HTTPRoute http://plain.example web",
		"HTTPRoute https://nogw.example web",
		"HTTPRoute https://www.shop.example web",
	}, "\n")
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A route carries the API resource it was read at, so the apps can open its object summary.
func TestAppRoutesCarryTheirResource(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{"rules":[
				{"host":"www.shop.example","http":{"paths":[{"path":"/","backend":{"service":{"name":"web"}}}]}}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/httproutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"hostnames":["web.example"],
				"rules":[{"backendRefs":[{"name":"web"}]}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]resourceRef{
		"Ingress":   {"networking.k8s.io", "v1", "ingresses"},
		"HTTPRoute": {"gateway.networking.k8s.io", "v1", "httproutes"},
	}

	seen := map[string]bool{}

	for _, r := range list.Routes {
		seen[r.Kind] = true

		if got := (resourceRef{r.Group, r.Version, r.Resource}); got != want[r.Kind] {
			t.Errorf("%s %s: got %+v, want %+v", r.Kind, r.Name, got, want[r.Kind])
		}
	}

	for kind := range want {
		if !seen[kind] {
			t.Errorf("no %s route was read", kind)
		}
	}
}

// A controller listening on 8443 behind a LoadBalancer Service on 443 (Traefik): the URL is
// the public port, 443 left out; a port two Services publish differently keeps the listener's.
func TestAppRoutesHTTPRoutePublicPort(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
				{"name":"web","port":8000,"protocol":"HTTP","hostname":"plain.example"},
				{"name":"websecure","port":8443,"protocol":"HTTPS","hostname":"*.shop.example"},
				{"name":"alt","port":9443,"protocol":"HTTPS","hostname":"alt.example"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/httproutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw"}],
				"hostnames":["www.shop.example","plain.example","alt.example"],
				"rules":[{"backendRefs":[{"name":"web"}]}]}}]}`,
		"GET /api/v1/services": `{"items":[
			{"metadata":{"name":"traefik","namespace":"gw"},"spec":{"type":"LoadBalancer","ports":[
				{"name":"web","port":80,"protocol":"TCP","targetPort":8000},
				{"name":"websecure","port":443,"protocol":"TCP","targetPort":"websecure"},
				{"name":"alt","port":443,"protocol":"TCP","targetPort":9443}]}},
			{"metadata":{"name":"other","namespace":"gw"},"spec":{"type":"LoadBalancer","ports":[
				{"name":"alt","port":10443,"targetPort":9443}]}}]}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/gw/endpointslices": `{"items":[
			{"ports":[{"name":"web","port":8000},{"name":"websecure","port":8443}]}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"HTTPRoute http://plain.example web",
		"HTTPRoute https://alt.example:9443 web",
		"HTTPRoute https://www.shop.example web",
	}, "\n")
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A GRPCRoute: the URL is the host root, its method matches name no path; the scheme comes
// from the listener.
func TestAppRoutesGRPCRoute(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
				{"name":"https","port":443,"protocol":"HTTPS","hostname":"*.shop.example"}]},
				"status":{"addresses":[{"value":"198.51.100.7"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/grpcroutes": `{"items":[
			{"metadata":{"name":"api","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw"}],
				"hostnames":["api.shop.example"],
				"rules":[{"matches":[{"method":{"service":"echo.Example","method":"Ping"}}],"backendRefs":[{"name":"web"}]}]}},
			{"metadata":{"name":"nogw","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"missing"}],
				"hostnames":["nogw.example"],
				"rules":[{"backendRefs":[{"name":"web"}]}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"GRPCRoute https://api.shop.example web",
		"GRPCRoute https://nogw.example web",
	}, "\n")
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A TLSRoute passes TLS through to the app: the URL is the listener's host at its public port
// (443 left out); without a matching listener there is no URL, since a bare address cannot be
// reached over SNI.
func TestAppRoutesTLSRoute(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
				{"name":"tls","port":8443,"protocol":"TLS","hostname":"registry.shop.example"}]},
				"status":{"addresses":[{"value":"198.51.100.7"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1alpha2/tlsroutes": `{"items":[
			{"metadata":{"name":"registry","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw","sectionName":"tls"}],
				"rules":[{"backendRefs":[{"name":"web"}]}]}},
			{"metadata":{"name":"nogw","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw","sectionName":"other"}],
				"rules":[{"backendRefs":[{"name":"web"}]}]}}]}`,
		"GET /api/v1/services": `{"items":[
			{"metadata":{"name":"traefik","namespace":"gw"},"spec":{"type":"LoadBalancer","ports":[
				{"name":"tls","port":443,"protocol":"TCP","targetPort":"tls"}]},
				"status":{"loadBalancer":{"ingress":[{"ip":"198.51.100.7"}]}}}]}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/gw/endpointslices": `{"items":[
			{"ports":[{"name":"tls","port":8443}]}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	// The "nogw" route's sectionName matches no listener: it gets no URL, not the Gateway's
	// address, which no browser can open for a TLSRoute.
	want := "TLSRoute https://registry.shop.example web"
	if got := routeURLs(list.Routes); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// A cluster on the standard channel has no tlsroutes CRD: the 404 is the normal case and the
// HTTPRoutes and GRPCRoutes are kept.
func TestAppRoutesWithoutExperimentalChannel(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
				{"name":"https","port":443,"protocol":"HTTPS","hostname":"*.shop.example"}]},
				"status":{"addresses":[{"value":"198.51.100.7"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/httproutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw"}],
				"hostnames":["www.shop.example"],
				"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/"}}],"backendRefs":[{"name":"web"}]}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/grpcroutes": `{"items":[]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := routeURLs(list.Routes); got != "HTTPRoute https://www.shop.example web" {
		t.Fatalf("got %q", got)
	}
}

// kgateway's proxy Service (one LoadBalancer per Gateway, named after it) publishes the
// listener's port under the Gateway's address: the URL shows it.
func TestAppRoutesHTTPRouteKgatewayProxy(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`,
		"GET /apis/gateway.networking.k8s.io/v1/gateways": `{"items":[
			{"metadata":{"name":"edge","namespace":"shop"},"spec":{"gatewayClassName":"kgateway","listeners":[
				{"name":"http","port":8080,"protocol":"HTTP"}]},"status":{"addresses":[{"value":"198.51.100.7"}]}}]}`,
		"GET /apis/gateway.networking.k8s.io/v1/httproutes": `{"items":[
			{"metadata":{"name":"web","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"edge"}],
				"rules":[{"backendRefs":[{"name":"web"}]}]}}]}`,
		"GET /api/v1/services": `{"items":[
			{"metadata":{"name":"edge","namespace":"shop"},"spec":{"type":"LoadBalancer","ports":[
				{"name":"http","port":8080,"protocol":"TCP","targetPort":8080}]},
			 "status":{"loadBalancer":{"ingress":[{"ip":"198.51.100.7"}]}}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := routeURLs(list.Routes), "HTTPRoute http://198.51.100.7:8080 web"; got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestGatewayController(t *testing.T) {
	classes := []gatewayClassObject{
		demoNetObject[gatewayClassObject](`{"metadata":{"name":"kgateway"},"spec":{"controllerName":"kgateway.dev/kgateway"}}`),
		demoNetObject[gatewayClassObject](`{"metadata":{"name":"traefik"},"spec":{"controllerName":"traefik.io/gateway-controller"}}`),
		demoNetObject[gatewayClassObject](`{"metadata":{"name":"unknown"},"spec":{"controllerName":"example.io/gateway"}}`),
		demoNetObject[gatewayClassObject](`{"metadata":{"name":"bare"},"spec":{"controllerName":"justname"}}`),
	}

	for _, tc := range []struct{ class, want string }{
		{"kgateway", "kgateway"},
		{"traefik", "Traefik"},
		{"unknown", "example.io"},
		{"bare", "justname"},
		{"missing", ""},
	} {
		if got := gatewayController(tc.class, classes); got != tc.want {
			t.Errorf("gatewayController(%q) = %q, want %q", tc.class, got, tc.want)
		}
	}

	if got := gatewayController("kgateway", nil); got != "" {
		t.Errorf("no classes: %q", got)
	}
}

func TestAppRoutesTailscaleIngress(t *testing.T) {
	for _, tc := range []struct {
		name, spec, status, want string
	}{
		{"standalone", `"ingressClassName":"tailscale","tls":[{"hosts":["web"]}],"rules":[{"http":{"paths":[{"path":"/app","pathType":"Prefix","backend":{"service":{"name":"web"}}}]}}]`,
			`{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}`, "Ingress https://web.tail123.ts.net/app web"},
		{"shared-proxies", `"ingressClassName":"tailscale","tls":[{"hosts":["web"]}],"defaultBackend":{"service":{"name":"web"}}`,
			`{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}`, "Ingress https://web.tail123.ts.net web"},
		{"automatic-tls", `"ingressClassName":"tailscale","defaultBackend":{"service":{"name":"web"}}`,
			`{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}`, "Ingress https://web.tail123.ts.net web"},
		{"pending-address", `"ingressClassName":"tailscale","tls":[{"hosts":["web"]}],"defaultBackend":{"service":{"name":"web"}}`,
			`{}`, ""},
		{"other-controller", `"ingressClassName":"nginx","tls":[{"hosts":["web"]}],"defaultBackend":{"service":{"name":"web"}}`,
			`{"loadBalancer":{"ingress":[{"hostname":"web.tail123.ts.net"}]}}`, "Ingress http://web.tail123.ts.net web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			annotations := ""
			if tc.name == "shared-proxies" {
				annotations = `,"annotations":{"tailscale.com/proxy-group":"ingress-proxies"}`
			}
			k := openRoutesKube(t, map[string]string{
				"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[{"metadata":{"name":"web","namespace":"shop"` + annotations + `},"spec":{` + tc.spec + `},"status":` + tc.status + `}]}`,
			})
			list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}})
			if err != nil {
				t.Fatal(err)
			}
			if got := routeURLs(list.Routes); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAppRoutesWithoutGatewayAPI(t *testing.T) {
	k := openRoutesKube(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[{"metadata":{"name":"db","namespace":"shop"},"spec":{"rules":[
			{"host":"db.example","http":{"paths":[{"path":"/","backend":{"service":{"name":"db"}}}]}}]}}]}`,
	})

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "db-0"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := routeURLs(list.Routes); got != "Ingress http://db.example db" {
		t.Fatalf("got %q", got)
	}
}

func TestAppRoutesNoServiceSkipsRouteLists(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods/web-1": routesWebPod,
		"GET /api/v1/namespaces/shop/services":   `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := appRoutes(context.Background(), k, []routePod{{"shop", "web-1"}, {"../x", "y"}})
	if err != nil || len(list.Routes) != 0 {
		t.Fatalf("got %v %+v", err, list)
	}

	for _, r := range f.recorded() {
		if strings.Contains(r.path, "ingresses") || strings.Contains(r.path, "..") {
			t.Fatalf("unexpected request %s", r.path)
		}
	}
}

func TestRouteURL(t *testing.T) {
	cases := map[string]string{
		routeURL("https", "a.example", 443, "/"):     "https://a.example",
		routeURL("http", "a.example", 8080, "/x"):    "http://a.example:8080/x",
		routeURL("http", "2001:db8::1", 0, ""):       "http://[2001:db8::1]",
		routeURL("https", "2001:db8::1", 8443, "/y"): "https://[2001:db8::1]:8443/y",
	}

	for got, want := range cases {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

func TestKubeAppRoutesDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeAppRoutes(cfg, "", "", `[{"namespace":"demo","pod":"hello-ichor","node":"10.5.0.2"}]`)
	if err != nil {
		t.Fatal(err)
	}

	var list kubeRouteList
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Routes) != 1 || list.Routes[0].URL != "https://hello.home.example" {
		t.Fatalf("demo routes: %v %s", err, out)
	}

	if _, err := KubeAppRoutes(cfg, "", "", "not json"); err == nil {
		t.Fatal("invalid pod list accepted")
	}
}

func TestAppRoutesManyPodsListTheirNamespace(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/pods":         routesPods,
		"GET /api/v1/namespaces/shop/services":     routesServices,
		"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[{"metadata":{"name":"db","namespace":"shop"},"spec":{"defaultBackend":{"service":{"name":"db"}}},"status":{"loadBalancer":{"ingress":[{"hostname":"lb.example"}]}}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	pods := []routePod{{"shop", "db-0"}}
	for i := range routePodsByName {
		pods = append(pods, routePod{"shop", fmt.Sprintf("gone-%d", i)})
	}

	list, err := appRoutes(context.Background(), k, pods)
	if err != nil || routeURLs(list.Routes) != "Ingress http://lb.example db" {
		t.Fatalf("got %v %+v", err, list)
	}

	for _, r := range f.recorded() {
		if strings.Contains(r.path, "/pods/") {
			t.Fatalf("read one by one: %s", r.path)
		}
	}
}
