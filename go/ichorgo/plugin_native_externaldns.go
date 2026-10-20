package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The ExternalDNS plugin's two Go readers (go/plugin/builtin/external-dns.yaml). ExternalDNS
// keeps no status on the records it publishes: what the plugin shows is what the controllers'
// flags and the sources say. Both return synthetic objects (ichor.dev/v1alpha1, a group no
// cluster serves) whose source map names the object a row opens (ref: $.source).
//   - native:external-dns-controllers: the Deployments running the catalog's external-dns
//     image, their flags parsed (ExternalDNSController);
//   - native:external-dns-records: one ExternalDNSRecord per hostname and target family of
//     every source the controllers enable, each matched with the controller that publishes it.

// externalDNSPluginVersion is the synthetic objects' API version.
const externalDNSPluginVersion = "ichor.dev/v1alpha1"

// externalDNSController is one external-dns Deployment and the flags it runs with, its spec
// as the plugin reads it. Several controllers are normal (one per provider or zone).
type externalDNSController struct {
	Namespace     string `json:"-"`
	Name          string `json:"-"`
	Replicas      int    `json:"-"`
	ReadyReplicas int    `json:"-"`

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
}

// key is how a record names its controller: <namespace>/<name>.
func (c externalDNSController) key() string { return c.Namespace + "/" + c.Name }

// externalDNSRecord is one hostname a controller is asked to publish, where it comes from and
// what it should point at. Never worded as "the record exists": only the DNS provider knows.
type externalDNSRecord struct {
	DNSName    string   `json:"dnsName"`
	RecordType string   `json:"recordType"`
	Targets    []string `json:"targets"`
	TTL        int      `json:"ttl"`
	SourceKind string   `json:"sourceKind"`
	// Controller is the covering controller's <namespace>/<name>, "" when none.
	Controller string `json:"controller"`

	stale  bool
	source extDNSSource
}

// warns tells whether the plugin's health rules make the record a warning (unmanaged, stale
// or without a target): those come first, so the list's limit keeps them.
func (r externalDNSRecord) warns() bool { return r.Controller == "" || r.stale || len(r.Targets) == 0 }

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

// externalDNSObject is a synthetic object of the plugin.
type externalDNSObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	} `json:"metadata"`
	Spec   any          `json:"spec"`
	Status any          `json:"status"`
	Source extDNSSource `json:"source"`
}

// The API paths the readers list.
var (
	extDNSDeploymentsRef = resourceRef{"apps", "v1", "deployments"}
	extDNSIngressesRef   = resourceRef{"networking.k8s.io", "v1", "ingresses"}
	extDNSServicesRef    = resourceRef{"", "v1", "services"}
	extDNSRoutesRef      = resourceRef{groupGatewayAPI, "v1", "httproutes"}
	extDNSGatewaysRef    = resourceRef{groupGatewayAPI, "v1", "gateways"}
)

// readExternalDNSControllers is native:external-dns-controllers.
func readExternalDNSControllers(ctx context.Context, k *kubeClient, _ *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	controllers, err := listExternalDNSControllers(ctx, k, scope)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(controllers))

	for _, c := range controllers {
		obj := externalDNSObject{
			APIVersion: externalDNSPluginVersion, Kind: "ExternalDNSController", Spec: c,
			Status: map[string]int{"replicas": c.Replicas, "readyReplicas": c.ReadyReplicas},
			Source: extDNSSource{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespace: c.Namespace, Name: c.Name},
		}
		obj.Metadata.Namespace, obj.Metadata.Name = c.Namespace, c.Name

		m, err := pluginObjectOf(obj)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, nil
}

// readExternalDNSRecords is native:external-dns-records: the sources in scope (DNSEndpoints
// when the CRD is served, Ingresses, Services, and HTTPRoutes with their Gateways when a
// controller publishes them), read at once, fanned out into records and matched with the
// controllers, worst first. A refused source is the resource's error, next to the records
// of the others (a partial read); a missing Gateway API CRD (the experimental channel) is not.
func readExternalDNSRecords(ctx context.Context, k *kubeClient, p *pluginProbe, _ plugin.Resource, scope string) ([]map[string]any, error) {
	// Every controller counts for the matching, wherever it runs; an account that may only
	// read its namespace matches with the ones it sees there.
	controllers, err := listExternalDNSControllers(ctx, k, "")
	if err != nil && scope != "" && isForbidden(err) {
		controllers, err = listExternalDNSControllers(ctx, k, scope)
	}

	if err != nil {
		return nil, err
	}

	version := p.groups[groupExternalDNS]

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
		wg.Go(func() {
			errs[0] = getList(ctx, k, resourceRef{groupExternalDNS, version, "dnsendpoints"}.path(scope), &endpoints)
		})
	}

	wg.Go(func() { errs[1] = getList(ctx, k, extDNSIngressesRef.path(scope), &ingresses) })
	wg.Go(func() { errs[2] = getList(ctx, k, extDNSServicesRef.path(scope), &services) })

	// The Gateway API objects are only asked for when a controller publishes HTTPRoutes. A
	// route's Gateway may run in another namespace than the scope.
	if slices.ContainsFunc(controllers, func(c externalDNSController) bool { return slices.Contains(c.Sources, "gateway-httproute") }) {
		wg.Go(func() { errs[3] = ignoreNotFound(getList(ctx, k, extDNSRoutesRef.path(scope), &routes)) })
		wg.Go(func() { errs[4] = ignoreNotFound(listWide(ctx, k, extDNSGatewaysRef, scope, &gateways)) })
	}

	wg.Wait()

	// The routes readers tag each route with its kind; this listing only asks HTTPRoutes.
	raw := extDNSRawRecords(endpoints.Items, ingresses.Items, services.Items, withGatewayKind(routes.Items, "HTTPRoute"), gateways.Items, version)
	records := extDNSMatch(controllers, raw)
	out := make([]map[string]any, 0, len(records))

	for _, r := range records {
		obj := externalDNSObject{
			APIVersion: externalDNSPluginVersion, Kind: "ExternalDNSRecord", Spec: r,
			Status: map[string]bool{"stale": r.stale}, Source: r.source,
		}
		obj.Metadata.Namespace = r.source.Namespace
		obj.Metadata.Name = r.DNSName + "/" + r.RecordType + "/" + r.SourceKind + "/" + r.source.Name

		m, err := pluginObjectOf(obj)
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	// A refused list takes its records away; the others still show, with its error.
	return out, errors.Join(errs...)
}

// listWide lists ref in every namespace, else in scope when the account may only read there.
func listWide[T any](ctx context.Context, k *kubeClient, ref resourceRef, scope string, out *kubeList[T]) error {
	err := getList(ctx, k, ref.path(""), out)
	if err != nil && scope != "" && isForbidden(err) {
		return getList(ctx, k, ref.path(scope), out)
	}

	return err
}

// listExternalDNSControllers are the Deployments in scope whose images match the catalog's
// external-dns one, their flags read, by namespace then name (the matching's order).
func listExternalDNSControllers(ctx context.Context, k *kubeClient, scope string) ([]externalDNSController, error) {
	var deps kubeList[externalDNSDeployment]
	if err := getList(ctx, k, extDNSDeploymentsRef.path(scope), &deps); err != nil {
		return nil, err
	}

	catalog := loadAppCatalog()
	controllers := []externalDNSController{}

	for _, d := range deps.Items {
		if extDNSIsController(d, catalog) {
			controllers = append(controllers, newExternalDNSController(d))
		}
	}

	slices.SortFunc(controllers, func(a, b externalDNSController) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	return controllers, nil
}

// pluginObjectOf is v as a plugin reads an object: the shape a list call decodes to.
func pluginObjectOf(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode object: %w", err)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode object: %w", err)
	}

	return out, nil
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
	}

	// --dry-run is a boolean flag: present unless written "--dry-run=false".
	if v, ok := args["dry-run"]; ok && (len(v) == 0 || v[len(v)-1] != "false") {
		c.DryRun = true
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
// built without the field (a hand-built one) manages the default set.
func extDNSManages(c externalDNSController, recordType string) bool {
	types := c.ManagedRecordTypes
	if len(types) == 0 {
		types = extDNSDefaultRecordTypes
	}

	return slices.Contains(types, strings.ToUpper(strings.TrimSpace(recordType)))
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

// extDNSMatch fills each record's controller (extDNSControllerFor) and sorts them worst
// first (see warns), then by name, type and source.
func extDNSMatch(controllers []externalDNSController, raw []extDNSRawRecord) []externalDNSRecord {
	records := make([]externalDNSRecord, 0, len(raw))

	for _, r := range raw {
		rec := externalDNSRecord{
			DNSName: r.dnsName, RecordType: r.recordType, Targets: r.targets, TTL: r.ttl, SourceKind: r.source.Kind,
			stale: r.stale, source: r.source,
		}
		if rec.Targets == nil {
			rec.Targets = []string{}
		}

		if i := extDNSControllerFor(controllers, r.dnsName, r.recordType); i >= 0 {
			rec.Controller = controllers[i].key()
		}

		records = append(records, rec)
	}

	slices.SortFunc(records, byHealthThenKey(func(r externalDNSRecord) (string, string) {
		health := healthOK
		if r.warns() {
			health = healthWarning
		}

		return health, r.DNSName + "/" + r.RecordType + "/" + r.SourceKind + "/" + r.source.Namespace + "/" + r.source.Name
	}))

	return records
}
