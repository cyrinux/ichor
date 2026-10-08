package ichorgo

import (
	"cmp"
	"slices"
)

// Gateway API objects (gateway.networking.k8s.io/v1), the fields the app reads.

type gatewayParentRef struct {
	Group       *string `json:"group"`
	Kind        *string `json:"kind"`
	Namespace   string  `json:"namespace"`
	Name        string  `json:"name"`
	SectionName string  `json:"sectionName"`
	Port        int32   `json:"port"`
}

type httpRouteObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
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
		Listeners []gatewayListener `json:"listeners"`
	} `json:"spec"`
	Status struct {
		Addresses []struct {
			Value string `json:"value"`
		} `json:"addresses"`
	} `json:"status"`
	// publicPorts maps listener ports to the ports clients reach them at (withPublicPorts).
	publicPorts map[int32]int32
}

// routeTarget is a path of an HTTPRoute and the Service it sends it to.
type routeTarget struct{ path, service string }

// httpRouteRoutes lists a URL per hostname and path of the HTTPRoutes sending traffic to one
// of services. The scheme and port come from the listener of the parent Gateway serving the
// hostname (https when it is unknown); a route without hostnames uses its listeners' ones,
// else the Gateway's addresses.
func httpRouteRoutes(httpRoutes []httpRouteObject, gateways []gatewayObject, services map[serviceRef]bool) []kubeRoute {
	var routes []kubeRoute

	for _, hr := range httpRoutes {
		targets := httpRouteTargets(hr, services)
		if len(targets) == 0 {
			continue
		}

		listeners, addresses := routeParents(hr, gateways)

		for _, host := range httpRouteHosts(hr, listeners, addresses) {
			scheme, port := listenerScheme(listeners, host)
			for _, t := range targets {
				routes = append(routes, kubeRoute{
					Kind: "HTTPRoute", Namespace: hr.Metadata.Namespace, Name: hr.Metadata.Name,
					URL: routeURL(scheme, host, port, t.path), Service: t.service,
				})
			}
		}
	}

	return routes
}

// httpRouteTargets lists the rules of hr whose backends include one of services.
func httpRouteTargets(hr httpRouteObject, services map[serviceRef]bool) []routeTarget {
	var targets []routeTarget

	for _, rule := range hr.Spec.Rules {
		service := ""

		for _, b := range rule.BackendRefs {
			if !isCoreService(b.Group, b.Kind) {
				continue
			}

			if services[serviceRef{cmp.Or(b.Namespace, hr.Metadata.Namespace), b.Name}] {
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

// routeParents returns the listeners hr attaches to (sectionName and port narrow them), at
// their public ports when known, and the addresses of its Gateways.
func routeParents(hr httpRouteObject, gateways []gatewayObject) ([]gatewayListener, []string) {
	var (
		listeners []gatewayListener
		addresses []string
	)

	for _, ref := range hr.Spec.ParentRefs {
		if !isGatewayRef(ref) {
			continue
		}

		ns := cmp.Or(ref.Namespace, hr.Metadata.Namespace)
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

// httpRouteHosts are the route's hostnames a browser can open, else its listeners', else
// the Gateway addresses.
func httpRouteHosts(hr httpRouteObject, listeners []gatewayListener, addresses []string) []string {
	var listenerHosts []string
	for _, l := range listeners {
		listenerHosts = append(listenerHosts, l.Hostname)
	}

	for _, candidates := range [][]string{hr.Spec.Hostnames, listenerHosts, addresses} {
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
