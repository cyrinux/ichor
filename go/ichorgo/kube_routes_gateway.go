package ichorgo

import (
	"context"
	"slices"
	"strings"
)

// Gateway API objects (gateway.networking.k8s.io, HTTPRoute and GRPCRoute v1, TLSRoute
// v1alpha2), the fields the app reads.

type gatewayParentRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   string  `json:"namespace"`
	Name        string  `json:"name"`
	SectionName string  `json:"sectionName"`
	Port        int32   `json:"port"`
}

// gatewayRouteObject is a Gateway API route: HTTPRoute (v1), GRPCRoute (v1) or TLSRoute
// (v1alpha2, the experimental channel), the fields the app reads. Kind is set by the reader;
// a GRPCRoute's matches name a gRPC Service and Method (no URL path), a TLSRoute's spec has
// neither matches nor hostnames (the URL is the listener's host at its TLS port).
type gatewayRouteObject struct {
	Kind     string `json:"-"`
	Metadata struct {
		Name string `json:"name"`
		// Annotations: the ExternalDNS reader reads the controller and ttl ones.
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		ParentRefs []gatewayParentRef `json:"parentRefs"`
		Hostnames  []string           `json:"hostnames"`
		Rules      []struct {
			Matches []struct {
				Path *struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"path"`
				Method *struct {
					Service string `json:"service"`
					Method  string `json:"method"`
				} `json:"method"`
			} `json:"matches"`
			BackendRefs []struct {
				Group     *string `json:"group"`
				Kind      *string `json:"kind"`
				Namespace string  `json:"namespace"`
				Name      string  `json:"name"`
			} `json:"backendRefs"`
		} `json:"rules"`
	} `json:"spec"`
}

type gatewayListener struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"` // HTTP, HTTPS, TLS...
}

type gatewayObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		GatewayClassName string            `json:"gatewayClassName"`
		Listeners        []gatewayListener `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
	} `json:"status"`
	// publicPorts maps listener ports to the ports clients reach them at (withPublicPorts).
	publicPorts map[int32]int32
}

// gatewayClassObject is a GatewayClass: which controller serves its Gateways.
type gatewayClassObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		ControllerName string `json:"controllerName"`
	} `json:"spec"`
}

// gatewayControllerNames maps a GatewayClass's controllerName to the short name the network
// map shows next to the Gateway's address (no version; the box is narrow).
var gatewayControllerNames = map[string]string{
	"kgateway.dev/kgateway":                         "kgateway",
	"traefik.io/gateway-controller":                 "Traefik",
	"io.cilium/gateway-controller":                  "Cilium",
	"gateway.envoyproxy.io/gatewayclass-controller": "Envoy Gateway",
	"istio.io/gateway-controller":                   "Istio",
	"gateway.nginx.org/nginx-gateway-controller":    "NGINX Gateway Fabric",
}

// gatewayController names the controller serving the Gateways of className, "" when the
// class is unknown to the cluster or to the app; an unknown controller shows its domain.
func gatewayController(className string, classes []gatewayClassObject) string {
	i := slices.IndexFunc(classes, func(c gatewayClassObject) bool { return c.Metadata.Name == className })
	if i < 0 {
		return ""
	}

	if name, ok := gatewayControllerNames[classes[i].Spec.ControllerName]; ok {
		return name
	}

	if domain, _, ok := strings.Cut(classes[i].Spec.ControllerName, "/"); ok {
		return domain
	}

	return classes[i].Spec.ControllerName
}

// routeTarget is a path of an HTTPRoute and the Service it sends it to.
type routeTarget struct{ path, service string }

// gatewayRoutes lists a URL per hostname of the Gateway API routes sending traffic to one of
// services. The scheme and port come from the listener of the parent Gateway serving the
// hostname (https when it is unknown); a route without hostnames uses its listeners' ones,
// else the Gateway's addresses. A GRPCRoute's and a TLSRoute's URL is the host root: only an
// HTTPRoute match narrows it to a path.
func gatewayRoutes(routes []gatewayRouteObject, gateways []gatewayObject, services map[serviceRef]bool) []kubeRoute {
	var out []kubeRoute

	for _, r := range routes {
		targets := gatewayRouteTargets(r, services)
		if len(targets) == 0 {
			continue
		}

		listeners, addresses := routeParents(r, gateways)

		for _, host := range gatewayRouteHosts(r, listeners, addresses) {
			scheme, port := listenerScheme(listeners, host)

			for _, t := range targets {
				path := ""
				if r.Kind == "HTTPRoute" {
					path = t.path
				}

				out = append(out, routeRow(r.Kind, r.Metadata.Namespace, r.Metadata.Name,
					routeURL(scheme, host, port, path), t.service))
			}
		}
	}

	return out
}

// gatewayRouteTargets lists the rules of r whose backends include one of services.
func gatewayRouteTargets(r gatewayRouteObject, services map[serviceRef]bool) []routeTarget {
	var targets []routeTarget

	for _, rule := range r.Spec.Rules {
		service := ""

		for _, b := range rule.BackendRefs {
			if !isCoreService(b.Group, b.Kind) {
				continue
			}

			if services[serviceRef{orDefault(b.Namespace, r.Metadata.Namespace), b.Name}] {
				service = b.Name

				break
			}
		}

		if service == "" {
			continue
		}

		path := ""

		for _, m := range rule.Matches {
			// A regular expression cannot be opened: the hostname root instead.
			if m.Path != nil && m.Path.Type != "RegularExpression" {
				path = m.Path.Value

				break
			}
		}

		targets = append(targets, routeTarget{path, service})
	}

	return targets
}

// routeParents returns the listeners r attaches to (sectionName and port narrow them), at
// their public ports when known, and the addresses of its Gateways.
func routeParents(r gatewayRouteObject, gateways []gatewayObject) ([]gatewayListener, []string) {
	var (
		listeners []gatewayListener
		addresses []string
	)

	for _, ref := range r.Spec.ParentRefs {
		if !isGatewayRef(ref) {
			continue
		}

		ns := orDefault(ref.Namespace, r.Metadata.Namespace)
		i := slices.IndexFunc(gateways, func(g gatewayObject) bool {
			return g.Metadata.Namespace == ns && g.Metadata.Name == ref.Name
		})

		if i < 0 {
			continue
		}

		for _, l := range gateways[i].Spec.Listeners {
			if (ref.SectionName == "" || ref.SectionName == l.Name) && (ref.Port == 0 || ref.Port == l.Port) {
				if public, ok := gateways[i].publicPorts[l.Port]; ok {
					l.Port = public
				}

				listeners = append(listeners, l)
			}
		}

		for _, a := range gateways[i].Status.Addresses {
			addresses = append(addresses, a.Value)
		}
	}

	return listeners, addresses
}

// gatewayRouteHosts are the route's hostnames a browser can open, else its listeners', else
// the Gateway addresses.
func gatewayRouteHosts(r gatewayRouteObject, listeners []gatewayListener, addresses []string) []string {
	var listenerHosts []string
	for _, l := range listeners {
		listenerHosts = append(listenerHosts, l.Hostname)
	}

	for _, candidates := range [][]string{r.Spec.Hostnames, listenerHosts, addresses} {
		hosts := slices.DeleteFunc(slices.Clone(candidates), func(h string) bool { return !openableHost(h) })
		if len(hosts) > 0 {
			return slices.Compact(hosts)
		}
	}

	return nil
}

// listenerScheme picks the listener serving host, HTTPS before HTTP; without one, https on
// its default port.
func listenerScheme(listeners []gatewayListener, host string) (string, int32) {
	scheme, port := "", int32(0)

	for _, l := range listeners {
		if l.Hostname != "" && !hostMatches(l.Hostname, host) {
			continue
		}

		switch l.Protocol {
		case "HTTPS", "TLS":
			return "https", l.Port
		case "HTTP":
			if scheme == "" {
				scheme, port = "http", l.Port
			}
		}
	}

	if scheme == "" {
		return "https", 0
	}

	return scheme, port
}

// isCoreService tells a backendRef to a Service: group "" and kind Service, the defaults.
func isCoreService(group, kind *string) bool {
	return (group == nil || *group == "") && (kind == nil || *kind == "Service")
}

// isGatewayRef tells a parentRef to a Gateway, the default.
func isGatewayRef(ref gatewayParentRef) bool {
	return (ref.Group == nil || *ref.Group == "gateway.networking.k8s.io") && (ref.Kind == nil || *ref.Kind == "Gateway")
}

// withGatewayKind tags each route with the kind that was read, so gatewayRoutes puts it on
// the kubeRoute and the network map on its node.
func withGatewayKind(routes []gatewayRouteObject, kind string) []gatewayRouteObject {
	for i := range routes {
		routes[i].Kind = kind
	}

	return routes
}

// readGatewayRoutes lists one Gateway API route kind (tolerating a missing CRD, the
// experimental channel in particular), each tagged with its kind.
func readGatewayRoutes(ctx context.Context, k *kubeClient, path, kind string) ([]gatewayRouteObject, error) {
	var list kubeList[gatewayRouteObject]
	if err := ignoreNotFound(getList(ctx, k, path, &list)); err != nil {
		return nil, err
	}

	return withGatewayKind(list.Items, kind), nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}

	return s
}
