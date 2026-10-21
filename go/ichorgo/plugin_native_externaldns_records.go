package ichorgo

import (
	"net"
	"slices"
	"strconv"
	"strings"
)

// The hostnames ExternalDNS is asked to publish, read off its sources (DNSEndpoints, Ingresses,
// Services, HTTPRoutes) the way the controller reads them: one record per hostname and target
// family. native:external-dns-records (plugin_native_externaldns.go) matches them with the
// controllers.

// groupExternalDNS is ExternalDNS's API group, serving the optional DNSEndpoint CRD
// (v1alpha1): most installs publish only the Ingress and Service hosts they are given.
const groupExternalDNS = "externaldns.k8s.io"

// The annotation prefixes ExternalDNS reads: v0.22 renamed the alpha one, still honoured
// with --enable-legacy-annotation-prefix. Both are read, the current one first, since a
// cluster upgrading its controller carries both for a while.
const (
	extDNSAnnPrefix       = "external-dns.kubernetes.io/"
	extDNSAnnLegacyPrefix = "external-dns.alpha.kubernetes.io/"
)

// extDNSAnnKey is one ExternalDNS annotation under both prefixes, built once so the
// lookup in the per-object loops does not concatenate.
type extDNSAnnKey struct{ current, legacy string }

func newExtDNSAnnKey(name string) extDNSAnnKey {
	return extDNSAnnKey{current: extDNSAnnPrefix + name, legacy: extDNSAnnLegacyPrefix + name}
}

// The annotations the ExternalDNS sources share.
var (
	extDNSAnnHostname   = newExtDNSAnnKey("hostname")
	extDNSAnnInternal   = newExtDNSAnnKey("internal-hostname")
	extDNSAnnTarget     = newExtDNSAnnKey("target")
	extDNSAnnTTL        = newExtDNSAnnKey("ttl")
	extDNSAnnController = newExtDNSAnnKey("controller")
)

// extDNSAnn reads an ExternalDNS annotation, the current prefix winning over the legacy one.
func extDNSAnn(annotations map[string]string, key extDNSAnnKey) (string, bool) {
	if v, ok := annotations[key.current]; ok {
		return v, true
	}

	v, ok := annotations[key.legacy]
	return v, ok
}

// extDNSAnnValue is extDNSAnn without the presence flag: "" when absent.
func extDNSAnnValue(annotations map[string]string, key extDNSAnnKey) string {
	v, _ := extDNSAnn(annotations, key)
	return v
}

// extDNSControllerName is the controller annotation's value marking an object as this
// controller's: another value (a second ExternalDNS) leaves the object out.
const extDNSControllerName = "dns-controller"

type dnsEndpointObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
		Generation  int64             `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		Endpoints []struct {
			DNSName       string   `json:"dnsName"`
			RecordType    string   `json:"recordType"`
			Targets       []string `json:"targets"`
			RecordTTL     int      `json:"recordTTL"`
			SetIdentifier string   `json:"setIdentifier"`
		} `json:"endpoints"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration int64 `json:"observedGeneration"`
	} `json:"status"`
}

// extDNSSource is the object a record comes from, as a plugin row opens it (ref: $.source).
type extDNSSource struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// extDNSRawRecord is one hostname before the controllers are matched.
type extDNSRawRecord struct {
	dnsName    string
	recordType string
	targets    []string
	ttl        int
	stale      bool
	source     extDNSSource
}

// extDNSRawRecords collects the hostnames of every source the controllers enable. Objects
// another ExternalDNS owns (the controller annotation) and hostnames whose target set is a
// wildcard are left out.
func extDNSRawRecords(endpoints []dnsEndpointObject, ingresses []ingressObject, services []serviceListObject, routes []gatewayRouteObject, gateways []gatewayObject, version string) []extDNSRawRecord {
	var out []extDNSRawRecord

	for _, ep := range endpoints {
		if extDNSForeign(ep.Metadata.Annotations) {
			continue
		}

		// observedGeneration is only kept up when the controller lists the crd source.
		stale := ep.Metadata.Generation > ep.Status.ObservedGeneration
		for _, e := range ep.Spec.Endpoints {
			if e.DNSName == "" || extDNSWildcardTarget(e.Targets) {
				continue
			}

			out = append(out, extDNSRawRecord{
				dnsName: e.DNSName, recordType: e.RecordType, targets: e.Targets, ttl: e.RecordTTL, stale: stale,
				source: extDNSSource{
					Group: groupExternalDNS, Version: version, Resource: "dnsendpoints", Kind: "DNSEndpoint",
					Namespace: ep.Metadata.Namespace, Name: ep.Metadata.Name,
				},
			})
		}
	}

	for _, ing := range ingresses {
		if extDNSForeign(ing.Metadata.Annotations) {
			continue
		}

		hosts := extDNSIngressHosts(ing)
		targets, override := extDNSAnnTargets(ing.Metadata.Annotations)
		if !override {
			targets = extDNSLBAddresses(ing.Status.LoadBalancer.Ingress)
		}

		out = extDNSAppend(out, hosts, targets, extDNSAnnTTLValue(ing.Metadata.Annotations), "Ingress", ing.Metadata.Namespace, ing.Metadata.Name,
			"networking.k8s.io", "v1", "ingresses")
	}

	for _, svc := range services {
		if extDNSForeign(svc.Metadata.Annotations) {
			continue
		}

		hosts := splitCSV(orDefault(extDNSAnnValue(svc.Metadata.Annotations, extDNSAnnHostname), extDNSAnnValue(svc.Metadata.Annotations, extDNSAnnInternal)))
		if len(hosts) == 0 {
			continue
		}

		targets, override := extDNSAnnTargets(svc.Metadata.Annotations)
		if !override {
			targets = extDNSLBAddresses(svc.Status.LoadBalancer.Ingress)
			if len(targets) == 0 {
				targets = append(targets, svc.Spec.ExternalIPs...)
			}
		}

		out = extDNSAppend(out, hosts, targets, extDNSAnnTTLValue(svc.Metadata.Annotations), "Service", svc.Metadata.Namespace, svc.Metadata.Name,
			"", "v1", "services")
	}

	for _, r := range routes {
		if r.Kind != "HTTPRoute" || extDNSForeign(r.Metadata.Annotations) {
			continue
		}

		hosts, targets := extDNSRouteRecords(r, gateways)
		out = extDNSAppend(out, hosts, targets, extDNSAnnTTLValue(r.Metadata.Annotations), "HTTPRoute", r.Metadata.Namespace, r.Metadata.Name,
			groupGatewayAPI, "v1", "httproutes")
	}

	return out
}

// extDNSRouteRecords are the hostnames ExternalDNS publishes for an HTTPRoute and their
// targets. Hosts: the route's hostnames and hostname annotation (else its listeners'), kept
// only where an attached listener serves them, the more specific name winning. Targets: each
// serving Gateway's target annotation, which replaces its status addresses (the annotation is
// read on the Gateway; on a route it is ignored).
func extDNSRouteRecords(r gatewayRouteObject, gateways []gatewayObject) (hosts, targets []string) {
	routeHosts := append(slices.Clone(r.Spec.Hostnames), splitCSV(extDNSAnnValue(r.Metadata.Annotations, extDNSAnnHostname))...)
	if len(routeHosts) == 0 {
		routeHosts = []string{""} // the listeners' hostnames
	}

	for _, ref := range r.Spec.ParentRefs {
		gw, ok := parentGateway(r, ref, gateways)
		if !ok {
			continue
		}

		served := false

		for _, l := range gw.Spec.Listeners {
			if !refSelects(ref, l) {
				continue
			}

			for _, h := range routeHosts {
				if host, ok := extDNSMatchingHost(h, l.Hostname); ok && host != "" {
					hosts, served = append(hosts, host), true
				}
			}
		}

		if !served {
			continue
		}

		gwTargets, override := extDNSAnnTargets(gw.Metadata.Annotations)
		if !override {
			for _, a := range gw.Status.Addresses {
				gwTargets = append(gwTargets, a.Value)
			}
		}

		targets = append(targets, gwTargets...)
	}

	slices.Sort(hosts)
	slices.Sort(targets)

	return slices.Compact(hosts), slices.Compact(targets)
}

// extDNSMatchingHost matches a route hostname with a listener's (ExternalDNS's
// gwMatchingHost): an empty one matches anything, a "*." wildcard matches the names below
// it (not its own domain), and the more specific of the two is the one published.
func extDNSMatchingHost(route, listener string) (string, bool) {
	route = strings.ToLower(strings.TrimSuffix(route, "."))
	listener = strings.ToLower(strings.TrimSuffix(listener, "."))

	switch {
	case route == "":
		return listener, true
	case listener == "":
		return route, true
	}

	routeWild, listenerWild := strings.HasPrefix(route, "*."), strings.HasPrefix(listener, "*.")

	switch {
	case routeWild && listenerWild:
		if strings.HasSuffix(route, listener[1:]) {
			return route, true
		}

		return listener, strings.HasSuffix(listener, route[1:])
	case listenerWild:
		return route, strings.HasSuffix(route, listener[1:])
	case routeWild:
		return listener, strings.HasSuffix(listener, route[1:])
	default:
		return route, route == listener
	}
}

// extDNSForeign tells whether another ExternalDNS owns the object.
func extDNSForeign(annotations map[string]string) bool {
	v, ok := extDNSAnn(annotations, extDNSAnnController)
	return ok && v != extDNSControllerName
}

// extDNSIngressHosts are the rule hosts and the ones the hostname annotation adds.
func extDNSIngressHosts(ing ingressObject) []string {
	hosts := []string{}
	for _, rule := range ing.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
	}

	return append(hosts, splitCSV(extDNSAnnValue(ing.Metadata.Annotations, extDNSAnnHostname))...)
}

// extDNSLBAddresses flattens a load balancer's ingress entries.
func extDNSLBAddresses(ingress []struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
}) []string {
	var out []string
	for _, lb := range ingress {
		if lb.IP != "" {
			out = append(out, lb.IP)
		}

		if lb.Hostname != "" {
			out = append(out, lb.Hostname)
		}
	}

	return out
}

// extDNSAnnTargets are the target annotation's values; override tells whether it was set
// (it replaces the source's own addresses).
func extDNSAnnTargets(annotations map[string]string) (targets []string, override bool) {
	if t := extDNSAnnValue(annotations, extDNSAnnTarget); t != "" {
		return splitCSV(t), true
	}

	return nil, false
}

// extDNSAnnTTLValue is the ttl annotation in seconds, 0 when absent or unparsable.
func extDNSAnnTTLValue(annotations map[string]string) int {
	ttl, _ := strconv.Atoi(extDNSAnnValue(annotations, extDNSAnnTTL))
	return ttl
}

// extDNSWildcardTarget tells whether a target set holds a wildcard: not an address.
func extDNSWildcardTarget(targets []string) bool {
	return slices.ContainsFunc(targets, func(t string) bool { return strings.HasPrefix(t, "*") })
}

// extDNSTargetGroup is the part of a target set one record carries.
type extDNSTargetGroup struct {
	recordType string
	targets    []string
}

// extDNSSplitTargets splits a target set the way ExternalDNS publishes it: IPv4 addresses
// make an A record, IPv6 addresses an AAAA record and names (a load balancer with a
// hostname) a CNAME, each with only its own targets. An empty set is one A record with no
// target, so the hostname still shows (noTarget).
func extDNSSplitTargets(targets []string) []extDNSTargetGroup {
	var v4, v6, names []string

	for _, t := range targets {
		switch addr := net.ParseIP(t); {
		case addr == nil:
			names = append(names, t)
		case addr.To4() != nil:
			v4 = append(v4, t)
		default:
			v6 = append(v6, t)
		}
	}

	var groups []extDNSTargetGroup

	for _, g := range []extDNSTargetGroup{{"A", v4}, {"AAAA", v6}, {"CNAME", names}} {
		if len(g.targets) > 0 {
			groups = append(groups, g)
		}
	}

	if len(groups) == 0 {
		return []extDNSTargetGroup{{recordType: "A", targets: targets}}
	}

	return groups
}

// extDNSAppend adds the records of each hostname (one per target family, see
// extDNSSplitTargets), skipping the empty hostnames and the target sets that hold a wildcard.
func extDNSAppend(out []extDNSRawRecord, hosts, targets []string, ttl int, kind, namespace, name, group, version, resource string) []extDNSRawRecord {
	if extDNSWildcardTarget(targets) {
		return out
	}

	groups := extDNSSplitTargets(targets)

	for _, host := range hosts {
		if host == "" {
			continue
		}

		for _, g := range groups {
			out = append(out, extDNSRawRecord{
				dnsName: host, recordType: g.recordType, targets: g.targets, ttl: ttl,
				source: extDNSSource{Group: group, Version: version, Resource: resource, Kind: kind, Namespace: namespace, Name: name},
			})
		}
	}

	return out
}
