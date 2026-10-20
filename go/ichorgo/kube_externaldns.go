package ichorgo

import (
	"cmp"
	"context"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

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

const (
	// The value marking an object as this controller's: another value (a second ExternalDNS)
	// leaves the object out.
	extDNSControllerName = "dns-controller"
)

// Why a controller or a record is not ok, for the app to word.
const (
	extDNSReasonNotReady  = "notReady"  // controller: no replica ready
	extDNSReasonDryRun    = "dryRun"    // controller: --dry-run, it changes nothing
	extDNSReasonStale     = "stale"     // DNSEndpoint: a newer generation is not observed yet
	extDNSReasonNoTarget  = "noTarget"  // the source has no address to point at yet
	extDNSReasonUnmanaged = "unmanaged" // no controller's domain filters cover the name
)

// externalDNSRecordCap is how many records the section keeps; recordCount counts them all.
const externalDNSRecordCap = 500

// externalDNSStatus is the ExternalDNS section of Data services. ExternalDNS keeps no status
// on the records it publishes: health here is what the sources and the controllers say.
type externalDNSStatus struct {
	Version     string                  `json:"version"` // the DNSEndpoint CRD's, "" when absent
	Error       string                  `json:"error"`
	Controllers []externalDNSController `json:"controllers"`
	Records     []externalDNSRecord     `json:"records"`
	RecordCount int                     `json:"recordCount"`
	Truncated   bool                    `json:"truncated"`
}

// externalDNSController is one external-dns Deployment and the flags it runs with. Several
// controllers are normal (one per provider or zone).
type externalDNSController struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Ready         bool     `json:"ready"`
	Replicas      int      `json:"replicas"`
	ReadyReplicas int      `json:"readyReplicas"`
	Provider      string   `json:"provider"`
	Policy        string   `json:"policy"` // sync, upsert-only, create-only; "" is the controller's default
	Registry      string   `json:"registry"`
	OwnerID       string   `json:"ownerId"`
	TxtPrefix     string   `json:"txtPrefix"`
	TxtSuffix     string   `json:"txtSuffix"`
	DomainFilters []string `json:"domainFilters"`
	Sources       []string `json:"sources"`
	DryRun        bool     `json:"dryRun"`
	// IntervalSeconds is --interval, 60 (its default) when unset or unparsable.
	IntervalSeconds  int    `json:"intervalSeconds"`
	AnnotationFilter string `json:"annotationFilter"`
	// ManagedRecordTypes is --managed-record-types (CSV, repeatable), upper-cased; the
	// controller's default A, AAAA, CNAME when unset.
	ManagedRecordTypes []string `json:"managedRecordTypes"`
	Health             string   `json:"health"`
	Reasons            []string `json:"reasons"`
}

// externalDNSRecord is one hostname a controller is asked to publish, where it comes from and
// what it should point at. Never worded as "the record exists": only the DNS provider knows.
type externalDNSRecord struct {
	DNSName         string   `json:"dnsName"`
	RecordType      string   `json:"recordType"`
	Targets         []string `json:"targets"`
	TTL             int      `json:"ttl"`
	SourceKind      string   `json:"sourceKind"` // Ingress|Service|HTTPRoute|DNSEndpoint
	SourceNamespace string   `json:"sourceNamespace"`
	SourceName      string   `json:"sourceName"`
	// SourceGroup/Version/Resource open the source object's summary from the apps.
	SourceGroup    string   `json:"sourceGroup"`
	SourceVersion  string   `json:"sourceVersion"`
	SourceResource string   `json:"sourceResource"`
	Controller     string   `json:"controller"` // the covering controller's name, "" when none
	Health         string   `json:"health"`
	Reasons        []string `json:"reasons"`
}

type externalDNSDeployment struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
		Template struct {
			Spec struct {
				Containers []struct {
					Image string   `json:"image"`
					Args  []string `json:"args"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

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

// readExternalDNS builds the ExternalDNS section: the controllers (the Deployments whose
// images match the catalog's external-dns one) and every hostname their enabled sources ask
// them to publish. version is the DNSEndpoint CRD's API version, "" when the CRD is not
// served. pods (podsErr set when they could not be listed) tell which namespaces run a
// controller; without them every matching Deployment counts.
func readExternalDNS(ctx context.Context, k *kubeClient, version string, pods []dsPod, podsErr error, now time.Time) *externalDNSStatus {
	out := &externalDNSStatus{Version: version, Controllers: []externalDNSController{}, Records: []externalDNSRecord{}}

	catalog := loadAppCatalog()
	known := map[string]bool{}
	if podsErr == nil {
		for _, p := range pods {
			if p.Status.Phase != "Running" {
				continue
			}

			for _, c := range p.Spec.Containers {
				if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "external-dns" {
					known[p.Metadata.Namespace] = true
				}
			}
		}
	}

	var deps kubeList[externalDNSDeployment]
	depErr := getList(ctx, k, "/apis/apps/v1/deployments", &deps)

	var controllers []externalDNSController
	for _, d := range deps.Items {
		if !extDNSIsController(d, catalog) {
			continue
		}

		if len(known) > 0 && !known[d.Metadata.Namespace] {
			continue
		}

		controllers = append(controllers, newExternalDNSController(d))
	}

	if controllers == nil {
		controllers = []externalDNSController{}
	}

	slices.SortFunc(controllers, func(a, b externalDNSController) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	var (
		endpoints kubeList[dnsEndpointObject]
		ingresses kubeList[ingressObject]
		services  kubeList[serviceListObject]
		routes    kubeList[gatewayRouteObject]
		gateways  kubeList[gatewayObject]
		errs      = make([]error, 5)
		wg        sync.WaitGroup
	)

	if version != "" {
		wg.Go(func() { errs[0] = getList(ctx, k, "/apis/"+groupExternalDNS+"/"+version+"/dnsendpoints", &endpoints) })
	}

	wg.Go(func() { errs[1] = getList(ctx, k, "/apis/networking.k8s.io/v1/ingresses", &ingresses) })
	wg.Go(func() { errs[2] = getList(ctx, k, "/api/v1/services", &services) })

	// The Gateway API objects are only asked for when a controller publishes HTTPRoutes;
	// a missing CRD (the experimental channel) is not an error.
	if slices.ContainsFunc(controllers, func(c externalDNSController) bool { return slices.Contains(c.Sources, "gateway-httproute") }) {
		wg.Go(func() { errs[3] = ignoreNotFound(getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/httproutes", &routes)) })
		wg.Go(func() { errs[4] = ignoreNotFound(getList(ctx, k, "/apis/"+groupGatewayAPI+"/v1/gateways", &gateways)) })
	}

	wg.Wait()

	out.Controllers = controllers
	out.Error = sectionError(append([]error{depErr}, errs...)...)

	// The routes readers tag each route with its kind; this listing only asks HTTPRoutes.
	raw := extDNSRawRecords(endpoints.Items, ingresses.Items, services.Items, withGatewayKind(routes.Items, "HTTPRoute"), gateways.Items, version)
	out.Records, out.RecordCount, out.Truncated = extDNSMatch(controllers, raw)

	return out
}

// extDNSIsController tells whether the Deployment runs an external-dns container: its images
// match the catalog's external-dns entry.
func extDNSIsController(d externalDNSDeployment, catalog *appCatalog) bool {
	for _, c := range d.Spec.Template.Spec.Containers {
		if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "external-dns" {
			return true
		}
	}

	return false
}

// newExternalDNSController reads the controller's flags, both "--flag=value" and "--flag value".
func newExternalDNSController(d externalDNSDeployment) externalDNSController {
	args := map[string][]string{}
	for _, c := range d.Spec.Template.Spec.Containers {
		for i := 0; i < len(c.Args); i++ {
			a := c.Args[i]
			if !strings.HasPrefix(a, "--") {
				continue
			}

			name, value, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
			if !hasValue && i+1 < len(c.Args) && !strings.HasPrefix(c.Args[i+1], "--") {
				i++
				value, hasValue = c.Args[i], true
			}

			args[name] = append(args[name], value)
		}
	}

	last := func(name string) string {
		if v := args[name]; len(v) > 0 {
			return v[len(v)-1]
		}

		return ""
	}

	replicas := 1
	if d.Spec.Replicas != nil {
		replicas = *d.Spec.Replicas
	}

	sources := args["source"]
	if len(sources) == 0 {
		sources = []string{"ingress", "service"}
	}

	c := externalDNSController{
		Namespace: d.Metadata.Namespace, Name: d.Metadata.Name,
		Replicas: replicas, ReadyReplicas: d.Status.ReadyReplicas,
		Provider: last("provider"), Policy: last("policy"), Registry: orDefault(last("registry"), "txt"),
		OwnerID:   orDefault(last("txt-owner-id"), "default"),
		TxtPrefix: last("txt-prefix"), TxtSuffix: last("txt-suffix"),
		DomainFilters:   append([]string{}, args["domain-filter"]...),
		Sources:         sources,
		IntervalSeconds: 60, AnnotationFilter: last("annotation-filter"),
		ManagedRecordTypes: extDNSManagedRecordTypes(args["managed-record-types"]),
		Health:             healthOK, Reasons: []string{},
	}

	if d.Status.ReadyReplicas >= replicas && replicas > 0 {
		c.Ready = true
	}

	if d.Status.ReadyReplicas == 0 {
		c.Health, c.Reasons = healthCritical, append(c.Reasons, extDNSReasonNotReady)
	}

	// --dry-run is a boolean flag: present unless written "--dry-run=false".
	if v, ok := args["dry-run"]; ok && (len(v) == 0 || v[len(v)-1] != "false") {
		c.DryRun = true
		if c.Health == healthOK {
			c.Health = healthWarning
		}

		c.Reasons = append(c.Reasons, extDNSReasonDryRun)
	}

	if s := last("interval"); s != "" {
		if d, err := time.ParseDuration(s); err == nil && d > 0 {
			c.IntervalSeconds = int(d.Seconds())
		}
	}

	return c
}

// extDNSDefaultRecordTypes is what a controller manages without --managed-record-types.
var extDNSDefaultRecordTypes = []string{"A", "AAAA", "CNAME"}

// extDNSManagedRecordTypes reads every --managed-record-types value (each one a CSV list),
// upper-cased and without duplicates; the default set when none is given.
func extDNSManagedRecordTypes(values []string) []string {
	var out []string

	for _, v := range values {
		for _, t := range splitCSV(strings.ToUpper(v)) {
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
	}

	if len(out) == 0 {
		return slices.Clone(extDNSDefaultRecordTypes)
	}

	return out
}

// extDNSManages tells whether the controller publishes records of this type. A controller
// read without the field (an older JSON, a hand-built one) manages the default set.
func extDNSManages(c externalDNSController, recordType string) bool {
	types := c.ManagedRecordTypes
	if len(types) == 0 {
		types = extDNSDefaultRecordTypes
	}

	return slices.Contains(types, strings.ToUpper(strings.TrimSpace(recordType)))
}

// extDNSRawRecord is one hostname before the controllers are matched.
type extDNSRawRecord struct {
	dnsName    string
	recordType string
	targets    []string
	ttl        int
	stale      bool
	source     externalDNSRecord // the identity fields, filled by the caller
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
				source: externalDNSRecord{
					SourceKind: "DNSEndpoint", SourceNamespace: ep.Metadata.Namespace, SourceName: ep.Metadata.Name,
					SourceGroup: groupExternalDNS, SourceVersion: version, SourceResource: "dnsendpoints",
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

// extDNSRecordType names the record a target set makes: A for an IPv4 address, AAAA for
// IPv6 only, CNAME when the targets are names (a load balancer with a hostname).
func extDNSRecordType(targets []string) string {
	v6, address := false, false

	for _, t := range targets {
		if addr := net.ParseIP(t); addr != nil {
			address = true
			if addr.To4() == nil {
				v6 = true
			}
		}
	}

	switch {
	case !address && len(targets) > 0:
		return "CNAME"
	case v6:
		return "AAAA"
	default:
		return "A"
	}
}

// extDNSAppend adds one record per hostname, skipping the empty ones and the ones whose
// target set is a wildcard.
func extDNSAppend(out []extDNSRawRecord, hosts, targets []string, ttl int, kind, namespace, name, group, version, resource string) []extDNSRawRecord {
	if extDNSWildcardTarget(targets) {
		return out
	}

	for _, host := range hosts {
		if host == "" {
			continue
		}

		out = append(out, extDNSRawRecord{
			dnsName: host, recordType: extDNSRecordType(targets), targets: targets, ttl: ttl,
			source: externalDNSRecord{
				SourceKind: kind, SourceNamespace: namespace, SourceName: name,
				SourceGroup: group, SourceVersion: version, SourceResource: resource,
			},
		})
	}

	return out
}

// extDNSFilterCovers tells whether a controller's domain filter covers a hostname: an exact
// one, a "*.example.com" wildcard, or a bare domain (its subdomains too).
func extDNSFilterCovers(filter, host string) bool {
	if filter == "" {
		return true
	}

	if suffix, ok := strings.CutPrefix(filter, "*."); ok {
		return strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(suffix))
	}

	return strings.EqualFold(filter, host) || strings.HasSuffix(strings.ToLower(host), "."+strings.ToLower(filter))
}

// extDNSControllerFor is the index of the controller that publishes a record: the first
// whose domain filters cover the name and whose managed types include the record's (an
// A-only and a CNAME-only controller on one zone), else the first whose filters cover it,
// so a type no controller lists does not turn the record unmanaged. -1 when none covers it.
func extDNSControllerFor(controllers []externalDNSController, dnsName, recordType string) int {
	covers := func(c externalDNSController) bool {
		return len(c.DomainFilters) == 0 ||
			slices.ContainsFunc(c.DomainFilters, func(f string) bool { return extDNSFilterCovers(f, dnsName) })
	}

	if i := slices.IndexFunc(controllers, func(c externalDNSController) bool {
		return covers(c) && extDNSManages(c, recordType)
	}); i >= 0 {
		return i
	}

	return slices.IndexFunc(controllers, covers)
}

// extDNSMatch fills each record's controller (extDNSControllerFor) and health, sorts them
// worst first and caps the list.
func extDNSMatch(controllers []externalDNSController, raw []extDNSRawRecord) (records []externalDNSRecord, count int, truncated bool) {
	count = len(raw)

	for _, r := range raw {
		rec := r.source
		rec.DNSName, rec.RecordType, rec.Targets, rec.TTL = r.dnsName, r.recordType, r.targets, r.ttl
		rec.Health, rec.Reasons = healthOK, []string{}

		if i := extDNSControllerFor(controllers, r.dnsName, r.recordType); i >= 0 {
			rec.Controller = controllers[i].Name
		} else {
			rec.Reasons = append(rec.Reasons, extDNSReasonUnmanaged)
		}

		if r.stale {
			rec.Reasons = append(rec.Reasons, extDNSReasonStale)
		}

		if len(rec.Targets) == 0 {
			rec.Reasons = append(rec.Reasons, extDNSReasonNoTarget)
		}

		if len(rec.Reasons) > 0 {
			rec.Health = healthWarning
		}

		records = append(records, rec)
	}

	slices.SortFunc(records, byHealthThenKey(func(r externalDNSRecord) (string, string) {
		return r.Health, r.DNSName + "/" + r.SourceKind + "/" + r.SourceNamespace + "/" + r.SourceName
	}))

	if len(records) > externalDNSRecordCap {
		records, truncated = records[:externalDNSRecordCap], true
	}

	return records, count, truncated
}
