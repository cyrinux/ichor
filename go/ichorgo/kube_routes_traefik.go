package ichorgo

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"
)

// Traefik's own routes (traefik.io/v1alpha1, Traefik v3): IngressRoute for HTTP, IngressRouteTCP
// for TLS passthrough or TCP by SNI. A route names entryPoints, which Traefik's Helm chart
// publishes as the ports of its LoadBalancer Service under the same names (web on 80,
// websecure on 443).

// groupTraefik is Traefik's API group: IngressRoutes and IngressRouteTCPs.
const groupTraefik = "traefik.io"

// Traefik's default entryPoints, at the default ports of their scheme.
const (
	traefikWeb       = "web"
	traefikWebsecure = "websecure"
)

type traefikMeta struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type traefikBackend struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"` // Service (the default) or TraefikService
}

type ingressRouteObject struct {
	Metadata traefikMeta `json:"metadata"`
	Spec     struct {
		EntryPoints []string `json:"entryPoints"`
		Routes      []struct {
			Match    string           `json:"match"`
			Kind     string           `json:"kind"` // Rule, the only one
			Services []traefikBackend `json:"services"`
		} `json:"routes"`
		TLS *struct {
			SecretName string `json:"secretName"`
		} `json:"tls"`
	} `json:"spec"`
}

type ingressRouteTCPObject struct {
	Metadata traefikMeta `json:"metadata"`
	Spec     struct {
		EntryPoints []string `json:"entryPoints"`
		Routes      []struct {
			Match    string           `json:"match"`
			Services []traefikBackend `json:"services"`
		} `json:"routes"`
	} `json:"spec"`
}

// traefikEntry is where clients reach an entryPoint: the public port of the LoadBalancer
// Service port named after it (0 when unknown or ambiguous), and that Service's address.
type traefikEntry struct {
	port    int32
	address string
}

// traefikRouteSet is what the readers need to find the URLs of Traefik routes.
type traefikRouteSet struct {
	http    []ingressRouteObject
	tcp     []ingressRouteTCPObject
	entries map[string]traefikEntry
}

// readTraefikRoutes lists the IngressRoutes and IngressRouteTCPs of namespace ("" for all);
// without Traefik's CRDs, none. The entryPoints are looked up when a route uses one off the
// defaults, or always with withEntries (the network map shows the address).
func readTraefikRoutes(ctx context.Context, k *kubeClient, namespace string, withEntries bool) (traefikRouteSet, error) {
	var (
		set traefikRouteSet
		err error
	)

	base := "/apis/" + groupTraefik + "/v1alpha1"

	if set.http, err = listObjects[ingressRouteObject](ctx, k, scopedPath(base, namespace, "ingressroutes")); ignoreNotFound(err) != nil {
		return traefikRouteSet{}, err
	}

	if set.tcp, err = listObjects[ingressRouteTCPObject](ctx, k, scopedPath(base, namespace, "ingressroutetcps")); ignoreNotFound(err) != nil {
		return traefikRouteSet{}, err
	}

	if (withEntries && len(set.http)+len(set.tcp) > 0) || usesCustomEntryPoint(set) {
		set.entries = readTraefikEntries(ctx, k)
	}

	return set, nil
}

// usesCustomEntryPoint tells a route on an entryPoint off web and websecure, whose public port
// has to be looked up.
func usesCustomEntryPoint(set traefikRouteSet) bool {
	custom := func(eps []string) bool {
		return slices.ContainsFunc(eps, func(ep string) bool { return ep != traefikWeb && ep != traefikWebsecure })
	}

	return slices.ContainsFunc(set.http, func(r ingressRouteObject) bool { return custom(r.Spec.EntryPoints) }) ||
		slices.ContainsFunc(set.tcp, func(r ingressRouteTCPObject) bool { return custom(r.Spec.EntryPoints) })
}

// readTraefikEntries maps the port names of the LoadBalancer Services to their public ports.
// Best effort: without Services, no entry.
func readTraefikEntries(ctx context.Context, k *kubeClient) map[string]traefikEntry {
	services, err := listObjects[lbPortService](ctx, k, "/api/v1/services?fieldSelector=spec.type%3DLoadBalancer")
	if err != nil {
		return nil
	}

	return traefikEntries(services)
}

// traefikEntries: a name published at two different ports (two Traefik instances) has no port.
func traefikEntries(services []lbPortService) map[string]traefikEntry {
	entries := map[string]traefikEntry{}

	for _, svc := range services {
		address := ""
		for _, in := range svc.Status.LoadBalancer.Ingress {
			if address = cmp.Or(in.IP, in.Hostname); address != "" {
				break
			}
		}

		for _, p := range svc.Spec.Ports {
			if p.Name == "" || (p.Protocol != "" && p.Protocol != "TCP") {
				continue
			}

			e, seen := entries[p.Name]

			switch {
			case !seen:
				entries[p.Name] = traefikEntry{port: p.Port, address: address}
			case e.port != p.Port:
				e.port = 0
				entries[p.Name] = e
			}
		}
	}

	return entries
}

// traefikRoutes lists a URL per host and path of the Traefik routes sending traffic to one of
// services. A route whose rule names no openable host gives none.
func traefikRoutes(set traefikRouteSet, services map[serviceRef]bool) []kubeRoute {
	var routes []kubeRoute

	for _, ir := range set.http {
		ns := ir.Metadata.Namespace
		scheme, port := traefikScheme(ir.Spec.EntryPoints, ir.Spec.TLS != nil, set.entries)

		for _, r := range ir.Spec.Routes {
			if r.Kind != "" && r.Kind != "Rule" {
				continue
			}

			hosts, path := traefikMatch(r.Match)
			for _, service := range traefikTargets(r.Services, ns, services) {
				for _, host := range hosts {
					routes = append(routes, kubeRoute{Kind: "IngressRoute", Namespace: ns, Name: ir.Metadata.Name,
						URL: routeURL(scheme, host, port, path), Service: service})
				}
			}
		}
	}

	for _, ir := range set.tcp {
		// Only a TLS route on the HTTPS port is likely a web page; a database by SNI is not.
		if !traefikOnHTTPS(ir.Spec.EntryPoints, set.entries) {
			continue
		}

		ns := ir.Metadata.Namespace

		for _, r := range ir.Spec.Routes {
			hosts, _ := traefikMatch(r.Match)
			for _, service := range traefikTargets(r.Services, ns, services) {
				for _, host := range hosts {
					routes = append(routes, kubeRoute{Kind: "IngressRouteTCP", Namespace: ns, Name: ir.Metadata.Name,
						URL: routeURL("https", host, 0, ""), Service: service})
				}
			}
		}
	}

	return routes
}

// traefikTargets are the names of the backends among services; a TraefikService (weighted or
// mirrored) is not followed.
func traefikTargets(backends []traefikBackend, namespace string, services map[serviceRef]bool) []string {
	var out []string

	for _, b := range backends {
		if b.Kind != "" && b.Kind != "Service" {
			continue
		}

		if services[serviceRef{orDefault(b.Namespace, namespace), b.Name}] && !slices.Contains(out, b.Name) {
			out = append(out, b.Name)
		}
	}

	return out
}

// traefikScheme picks how a route is reached: websecure, then web, at their default ports;
// else the first other entryPoint whose public port is known (https on 443). A route without
// entryPoints listens on all of them. tls makes it https.
func traefikScheme(entryPoints []string, tls bool, entries map[string]traefikEntry) (string, int32) {
	scheme := "http"
	if tls {
		scheme = "https"
	}

	switch {
	case slices.Contains(entryPoints, traefikWebsecure):
		return "https", 0
	case len(entryPoints) == 0 || slices.Contains(entryPoints, traefikWeb):
		return scheme, 0
	}

	for _, ep := range entryPoints {
		if port := entries[ep].port; port > 0 {
			if port == 443 {
				scheme = "https"
			}

			return scheme, port
		}
	}

	return scheme, 0
}

// traefikOnHTTPS tells a route served on the HTTPS port: websecure, all entryPoints, or one
// published at 443.
func traefikOnHTTPS(entryPoints []string, entries map[string]traefikEntry) bool {
	return len(entryPoints) == 0 || slices.ContainsFunc(entryPoints, func(ep string) bool {
		return ep == traefikWebsecure || entries[ep].port == 443
	})
}

// traefikAddress is the load balancer address of the entryPoints of the named route, "" when
// unknown.
func traefikAddress(set traefikRouteSet, kind, namespace, name string) string {
	var entryPoints []string

	switch kind {
	case "IngressRoute":
		if i := slices.IndexFunc(set.http, func(r ingressRouteObject) bool { return r.Metadata == traefikMeta{name, namespace} }); i >= 0 {
			entryPoints = set.http[i].Spec.EntryPoints
		}
	case "IngressRouteTCP":
		if i := slices.IndexFunc(set.tcp, func(r ingressRouteTCPObject) bool { return r.Metadata == traefikMeta{name, namespace} }); i >= 0 {
			entryPoints = set.tcp[i].Spec.EntryPoints
		}
	}

	if len(entryPoints) == 0 {
		entryPoints = []string{traefikWebsecure, traefikWeb}
	}

	for _, ep := range entryPoints {
		if a := set.entries[ep].address; a != "" {
			return a
		}
	}

	return ""
}

var (
	// traefikMatcher is one matcher of a rule: an optional negation, a name and its arguments.
	traefikMatcher = regexp.MustCompile(`(!?)\s*([A-Za-z]+)\s*\(([^()]*)\)`)
	// traefikArg is a backtick- or double-quoted argument.
	traefikArg = regexp.MustCompile("`([^`]*)`|\"([^\"]*)\"")
)

// traefikMatch reads the hosts a browser can open and the first path of a router rule
// ("Host(`a`) && PathPrefix(`/x`)"). Only Host, HostSNI, Path and PathPrefix are read; regular
// expressions, wildcards and negated matchers give nothing, a path with variables the root.
func traefikMatch(match string) (hosts []string, path string) {
	pathSeen := false

	for _, m := range traefikMatcher.FindAllStringSubmatch(match, -1) {
		if m[1] == "!" {
			continue
		}

		for _, a := range traefikArg.FindAllStringSubmatch(m[3], -1) {
			arg := strings.TrimSpace(a[1] + a[2])

			switch m[2] {
			case "Host", "HostSNI", "HostHeader":
				if openableHost(arg) && !strings.ContainsAny(arg, "{}()[]^$|\\") && !slices.Contains(hosts, arg) {
					hosts = append(hosts, arg)
				}
			case "Path", "PathPrefix":
				if !pathSeen {
					pathSeen = true
					if !strings.ContainsAny(arg, "{}()[]*+?^$|\\") {
						path = arg
					}
				}
			}
		}
	}

	return hosts, path
}
