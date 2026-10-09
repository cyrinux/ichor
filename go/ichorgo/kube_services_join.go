package ichorgo

import (
	"cmp"
	"slices"
	"strconv"
	"time"
)

// endpointCount is how many endpoints a Service's slices list, and how many are ready.
type endpointCount struct{ total, ready int }

// countEndpoints counts the endpoints of each Service (its slices' kubernetes.io/service-name
// label). A dual-stack Service has an IPv4 and an IPv6 slice for the same pods: the family
// with the most endpoints is counted, not both.
func countEndpoints(endpointSlices []endpointSliceObject) map[serviceRef]endpointCount {
	byFamily := map[serviceRef]map[string]endpointCount{}

	for _, s := range endpointSlices {
		service := s.Metadata.Labels["kubernetes.io/service-name"]
		if service == "" {
			continue
		}

		ref := serviceRef{s.Metadata.Namespace, service}
		if byFamily[ref] == nil {
			byFamily[ref] = map[string]endpointCount{}
		}

		c := byFamily[ref][s.AddressType]
		for _, e := range s.Endpoints {
			c.total++

			if e.Conditions.Ready == nil || *e.Conditions.Ready {
				c.ready++
			}
		}

		byFamily[ref][s.AddressType] = c
	}

	out := map[serviceRef]endpointCount{}

	for ref, families := range byFamily {
		for _, c := range families {
			if c.total > out[ref].total {
				out[ref] = c
			}
		}
	}

	return out
}

// joinServices builds the rows: counts nil when the slices could not be read.
func joinServices(services []serviceListObject, counts map[serviceRef]endpointCount, routes serviceRoutes, now time.Time) []serviceRow {
	out := []serviceRow{}

	for _, s := range services {
		ref := serviceRef{s.Metadata.Namespace, s.Metadata.Name}
		row := serviceRow{
			Namespace: ref.namespace, Name: ref.name, Type: cmp.Or(s.Spec.Type, "ClusterIP"),
			ClusterIP: s.Spec.ClusterIP, ExternalName: s.Spec.ExternalName, LBClass: s.Spec.LoadBalancerClass,
			Ports: servicePorts(s), Addresses: serviceAddresses(s), Selector: len(s.Spec.Selector) > 0,
			Routes: routesOf(ref, routes),
		}

		row.Pending = row.Type == "LoadBalancer" && len(s.Status.LoadBalancer.Ingress) == 0

		if counts != nil {
			c := counts[ref]
			row.EndpointsKnown, row.Endpoints, row.ReadyEndpoints = true, c.total, c.ready
		}

		row.Level = serviceLevel(row, olderThan(s.Metadata.CreationTimestamp, now, lbGrace))
		out = append(out, row)
	}

	sortServices(out)

	return out
}

// servicePorts is kubectl's PORT(S) column, one entry a port.
func servicePorts(s serviceListObject) []string {
	ports := []string{}

	for _, p := range s.Spec.Ports {
		port := strconv.Itoa(int(p.Port))
		if p.NodePort > 0 {
			port += ":" + strconv.Itoa(int(p.NodePort))
		}

		ports = append(ports, port+"/"+cmp.Or(p.Protocol, "TCP"))
	}

	return ports
}

// serviceAddresses are where clients outside reach a Service: the load balancer's IPs or
// hostnames, then the external IPs set by hand.
func serviceAddresses(s serviceListObject) []string {
	addresses := []string{}

	for _, in := range s.Status.LoadBalancer.Ingress {
		if a := cmp.Or(in.IP, in.Hostname); a != "" {
			addresses = append(addresses, a)
		}
	}

	for _, ip := range s.Spec.ExternalIPs {
		if !slices.Contains(addresses, ip) {
			addresses = append(addresses, ip)
		}
	}

	return addresses
}

// routesOf lists the URLs of the Ingresses and HTTPRoutes sending traffic to ref, the
// routes map's own matching (KubeAppRoutes) narrowed to this Service.
func routesOf(ref serviceRef, routes serviceRoutes) []kubeRoute {
	only := map[serviceRef]bool{ref: true}
	found := append(ingressRoutes(routes.ingresses, only), httpRouteRoutes(routes.httpRoutes, routes.gateways, only)...)

	return uniqueRoutes(found)
}

// serviceLevel: a load balancer still without an address after lbGrace is critical (as in
// the checkup); one still coming, a selector with no ready endpoint, or endpoints not ready
// are warnings.
func serviceLevel(s serviceRow, pastGrace bool) string {
	switch {
	case s.Pending && pastGrace:
		return serviceCritical
	case s.Pending, s.EndpointsKnown && s.Selector && s.ReadyEndpoints == 0, s.ReadyEndpoints < s.Endpoints:
		return serviceWarning
	default:
		return serviceOK
	}
}

// sortServices puts problems first, the worst first, then by namespace and name.
func sortServices(services []serviceRow) {
	slices.SortFunc(services, func(a, b serviceRow) int {
		return cmp.Or(
			cmp.Compare(levelRank(b.Level), levelRank(a.Level)),
			cmp.Compare(a.Namespace, b.Namespace),
			cmp.Compare(a.Name, b.Name),
		)
	})
}

func anyPending(services []serviceRow) bool {
	return slices.ContainsFunc(services, func(s serviceRow) bool { return s.Pending })
}
