package ichorgo

import "strings"

type ingressBackend struct {
	Service *struct {
		Name string `json:"name"`
	} `json:"service"`
}

type ingressObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		IngressClassName string          `json:"ingressClassName"`
		DefaultBackend   *ingressBackend `json:"defaultBackend"`
		TLS              []struct {
			Hosts []string `json:"hosts"`
		} `json:"tls"`
		Rules []struct {
			Host string `json:"host"`
			HTTP *struct {
				Paths []struct {
					Path     string         `json:"path"`
					PathType string         `json:"pathType"`
					Backend  ingressBackend `json:"backend"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

// ingressRoutes lists a URL per host and path of the Ingresses sending traffic to one of
// services. A rule without host, or the default backend, is reached at the load balancer
// address the controller reports.
func ingressRoutes(ingresses []ingressObject, services map[serviceRef]bool) []kubeRoute {
	var routes []kubeRoute

	for _, ing := range ingresses {
		ns := ing.Metadata.Namespace
		route := func(host, path, service string) {
			if !openableHost(host) {
				return
			}

			url := routeURL(ingressScheme(ing, host), host, 0, path)
			routes = append(routes, kubeRoute{Kind: "Ingress", Namespace: ns, Name: ing.Metadata.Name, URL: url, Service: service})
		}

		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}

			for _, p := range rule.HTTP.Paths {
				if svc := p.Backend.Service; svc != nil && services[serviceRef{ns, svc.Name}] {
					route(ingressHost(ing, rule.Host), ingressPath(p.Path, p.PathType), svc.Name)
				}
			}
		}

		if b := ing.Spec.DefaultBackend; b != nil && b.Service != nil && services[serviceRef{ns, b.Service.Name}] {
			route(ingressHost(ing, ""), "", b.Service.Name)
		}
	}

	return routes
}

// ingressHost is the rule's host, or the load balancer address for a rule without one.
func ingressHost(ing ingressObject, host string) string {
	if host != "" {
		return host
	}

	for _, lb := range ing.Status.LoadBalancer.Ingress {
		if lb.Hostname != "" {
			return lb.Hostname
		}

		if lb.IP != "" {
			return lb.IP
		}
	}

	return ""
}

// ingressScheme is https for a host the Ingress has a TLS certificate for. Tailscale
// provisions HTTPS itself: tls.hosts contains a short device/service name, while status
// reports the full MagicDNS hostname, so those names need not match.
func ingressScheme(ing ingressObject, host string) string {
	if ing.Spec.IngressClassName == "tailscale" {
		return "https"
	}

	for _, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			if hostMatches(h, host) {
				return "https"
			}
		}
	}

	return "http"
}

// ingressPath keeps a plain path; a regular expression (ImplementationSpecific, as
// ingress-nginx allows) cannot be opened, so the host root is used instead.
func ingressPath(path, pathType string) string {
	if pathType == "ImplementationSpecific" && strings.ContainsAny(path, "()[]*+?^$|\\") {
		return ""
	}

	return path
}
