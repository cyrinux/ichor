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
				"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/"}}],"backendRefs":[{"name":"web","port":80}]}]}},
			{"metadata":{"name":"alt","namespace":"shop"},"spec":{
				"parentRefs":[{"name":"public","namespace":"gw","sectionName":"alt"}],
				"rules":[{"matches":[{"path":{"type":"PathPrefix","value":"/app"}}],"backendRefs":[{"name":"web"}]}]}},
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
