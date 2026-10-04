package ichorgo

import (
	"encoding/json"
	"strings"
)

// demoArgoNetwork is the network view of a demo Argo CD app, built by the same code as a real
// one from demo objects: an Ingress with TLS, an HTTPRoute behind a shared Gateway, a
// LoadBalancer Service, a Service whose pods crash-loop, and a cordoned node.
func demoArgoNetwork(app string) argoNetwork {
	in := argoNetInput{nodes: demoNetNodes()}

	var resources []argoResourceStatus

	svc := func(ns, name, typ, ip, selector, ports, lb string) {
		resources = append(resources, argoResourceStatus{Kind: "Service", Namespace: ns, Name: name})
		in.services = append(in.services, demoNetObject[netService](`{"metadata":{"name":"`+name+`","namespace":"`+ns+`"},
		  "spec":{"type":"`+typ+`","clusterIP":"`+ip+`","selector":{"app":"`+selector+`"},"ports":[`+ports+`]},
		  "status":{"loadBalancer":{"ingress":[`+lb+`]}}}`))
	}
	pod := func(ns, name, app, node, status string, ready, restarts int) {
		p := kubePod{Namespace: ns, Name: name, Node: node, Status: status, Ready: ready, Containers: 1, Restarts: restarts}
		p.Healthy = status == "Running" && ready == 1
		in.pods = append(in.pods, netPod{kubePod: p, labels: map[string]string{"app": app}})
	}

	switch app {
	case "hello-ichor":
		svc("demo", "hello-ichor", "ClusterIP", "10.96.14.20", "hello-ichor", `{"port":80,"targetPort":8080}`, "")
		pod("demo", "hello-ichor-7d9c5-abcde", "hello-ichor", "demo-worker-1", "Running", 1, 0)
		pod("demo", "hello-ichor-7d9c5-fghij", "hello-ichor", "demo-worker-2", "Running", 1, 0)
		pod("demo", "hello-ichor-7d9c5-klmno", "hello-ichor", "demo-worker-1", "Running", 1, 0)
		resources = append(resources, argoResourceStatus{Group: "networking.k8s.io", Kind: "Ingress", Namespace: "demo", Name: "hello-ichor"})
		in.ingresses = []ingressObject{demoNetObject[ingressObject](`{"metadata":{"name":"hello-ichor","namespace":"demo"},
		  "spec":{"tls":[{"hosts":["hello.homelab.lan"]}],"rules":[{"host":"hello.homelab.lan","http":{"paths":[{"path":"/","pathType":"Prefix","backend":{"service":{"name":"hello-ichor"}}}]}}]},
		  "status":{"loadBalancer":{"ingress":[{"ip":"10.0.0.240"}]}}}`)}
	case "demo-worker":
		svc("demo", "worker", "ClusterIP", "10.96.31.7", "worker", `{"name":"metrics","port":9090,"targetPort":"metrics"}`, "")
		pod("demo", "worker-6f4b8-pqrst", "worker", "demo-worker-2", "Running", 1, 0)
		pod("demo", "worker-6f4b8-uvwxy", "worker", "demo-worker-3", "CrashLoopBackOff", 0, 14)
	case "grafana":
		svc("monitoring", "grafana", "ClusterIP", "10.96.80.3", "grafana", `{"name":"http","port":80,"targetPort":3000}`, "")
		pod("monitoring", "grafana-6d8b-9fz2t", "grafana", "demo-worker-2", "Running", 1, 0)
		resources = append(resources, argoResourceStatus{Group: "gateway.networking.k8s.io", Kind: "HTTPRoute", Namespace: "monitoring", Name: "grafana"})
		in.httpRoutes = []httpRouteObject{demoNetObject[httpRouteObject](`{"metadata":{"name":"grafana","namespace":"monitoring"},
		  "spec":{"parentRefs":[{"name":"homelab","namespace":"traefik"}],"hostnames":["grafana.homelab.lan"],"rules":[{"backendRefs":[{"name":"grafana","port":80}]}]}}`)}
		in.gateways = []gatewayObject{demoNetObject[gatewayObject](`{"metadata":{"name":"homelab","namespace":"traefik"},
		  "spec":{"listeners":[{"name":"https","hostname":"*.homelab.lan","port":443,"protocol":"HTTPS"}]},"status":{"addresses":[{"value":"10.0.0.240"}]}}`)}
	case "traefik":
		svc("traefik", "traefik", "LoadBalancer", "10.96.0.80", "traefik", `{"name":"web","port":80,"targetPort":"web"},{"name":"websecure","port":443,"targetPort":"websecure"}`, `{"ip":"10.0.0.240"}`)
		pod("traefik", "traefik-8c6d-w7r2m", "traefik", "demo-worker-1", "Running", 1, 0)
		pod("traefik", "traefik-8c6d-q4n8x", "traefik", "demo-worker-2", "Running", 1, 0)
	case "longhorn":
		svc("longhorn-system", "longhorn-frontend", "ClusterIP", "10.96.52.9", "longhorn-ui", `{"name":"http","port":80,"targetPort":"http"}`, "")
		pod("longhorn-system", "longhorn-ui-5b7c-k8z2p", "longhorn-ui", "demo-worker-1", "Running", 1, 0)
		pod("longhorn-system", "longhorn-ui-5b7c-r3m6v", "longhorn-ui", "demo-worker-3", "ContainerCreating", 0, 0)
	}

	in.app.Status.Resources = resources

	return buildArgoNetwork(in)
}

// demoNetNodes: demo-worker-3 is cordoned and not ready, as in the rest of the demo.
func demoNetNodes() []netNodeObject {
	var nodes []netNodeObject

	for _, n := range []string{"demo-worker-1", "demo-worker-2", "demo-worker-3"} {
		ready, cordoned := "True", "false"
		if n == "demo-worker-3" {
			ready, cordoned = "False", "true"
		}

		nodes = append(nodes, demoNetObject[netNodeObject](`{"metadata":{"name":"`+n+`"},"spec":{"unschedulable":`+cordoned+`},
		  "status":{"conditions":[{"type":"Ready","status":"`+ready+`"}]}}`))
	}

	return nodes
}

func demoNetObject[T any](js string) T {
	var out T
	if err := json.NewDecoder(strings.NewReader(js)).Decode(&out); err != nil {
		panic("demo network object: " + err.Error()) // fixed literals, checked by the tests
	}

	return out
}
