package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func readExternalDNSFixture(t *testing.T, version string, pods []dsPod) *externalDNSStatus {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/deployments":                      string(readFixture(t, "externaldns/deployments.json")),
		"GET /apis/externaldns.k8s.io/v1alpha1/dnsendpoints": string(readFixture(t, "externaldns/dnsendpoints.json")),
		"GET /apis/networking.k8s.io/v1/ingresses":           string(readFixture(t, "externaldns/ingresses.json")),
		"GET /api/v1/services":                               string(readFixture(t, "externaldns/services.json")),
		"GET /apis/gateway.networking.k8s.io/v1/httproutes":  string(readFixture(t, "externaldns/httproutes.json")),
		"GET /apis/gateway.networking.k8s.io/v1/gateways":    string(readFixture(t, "externaldns/gateways.json")),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return readExternalDNS(context.Background(), k, version, pods, nil, fixtureNow)
}

// externalDNSPods are the running controllers the pod listing finds.
func externalDNSPods() []dsPod {
	return []dsPod{
		fakePod("external-dns", "external-dns-0", "node-1", true, nil, "external-dns", "registry.k8s.io/external-dns/external-dns:v0.14.0"),
		fakePod("dns-2", "external-dns-legacy-0", "node-1", false, nil, "external-dns", "registry.k8s.io/external-dns/external-dns:v0.13.0"),
		fakePod("kube-system", "coredns-0", "node-1", true, nil, "coredns", "registry.k8s.io/coredns/coredns:v1.11.1"),
	}
}

func TestReadExternalDNS(t *testing.T) {
	out := readExternalDNSFixture(t, "v1alpha1", externalDNSPods())

	if out.Version != "v1alpha1" || out.Error != "" {
		t.Fatalf("version %q error %q", out.Version, out.Error)
	}

	// Both controllers, the flags of both arg styles parsed, the broken one critical.
	if len(out.Controllers) != 2 {
		t.Fatalf("controllers: %+v", out.Controllers)
	}

	c, l := out.Controllers[0], out.Controllers[1]
	if c.Namespace != "dns-2" || c.Name != "external-dns-legacy" || c.Ready || c.Replicas != 2 || c.ReadyReplicas != 0 ||
		c.Provider != "inmemory" || !c.DryRun || c.OwnerID != "legacy" ||
		!slices.Equal(c.DomainFilters, []string{"example.org"}) ||
		c.AnnotationFilter != "app.kubernetes.io/managed-by=legacy" ||
		c.Health != healthCritical || !slices.Equal(c.Reasons, []string{extDNSReasonNotReady, extDNSReasonDryRun}) {
		t.Errorf("legacy controller: %+v", c)
	}

	if l.Namespace != "external-dns" || !l.Ready || l.Provider != "cloudflare" || l.Policy != "sync" ||
		l.Registry != "txt" || l.OwnerID != "homelab" || l.IntervalSeconds != 30 ||
		!slices.Equal(l.Sources, []string{"ingress", "service", "crd", "gateway-httproute"}) ||
		l.Health != healthOK {
		t.Errorf("main controller: %+v", l)
	}

	// Thirteen records: four warnings first, then the ok ones by name.
	names := make([]string, 0, len(out.Records))
	for _, r := range out.Records {
		names = append(names, r.DNSName)
	}

	want := []string{"*.example.net", "db.example.com", "pending.example.com", "status.example.com",
		"*.apps.example.com", "api.example.com", "app.example.com", "extra.example.com", "extra2.example.com",
		"internal.example.com", "override.example.com", "shop.example.com", "web.apps.example.com"}
	if !slices.Equal(names, want) {
		t.Fatalf("records %v, want %v", names, want)
	}

	byName := map[string]externalDNSRecord{}
	for _, r := range out.Records {
		byName[r.DNSName] = r
	}

	if r := byName["*.example.net"]; r.Health != healthWarning || !slices.Equal(r.Reasons, []string{extDNSReasonUnmanaged}) || r.Controller != "" {
		t.Errorf("unmanaged: %+v", r)
	}

	if r := byName["db.example.com"]; r.Health != healthWarning || !slices.Equal(r.Reasons, []string{extDNSReasonStale}) ||
		r.SourceKind != "DNSEndpoint" || r.SourceGroup != groupExternalDNS || r.SourceVersion != "v1alpha1" || r.TTL != 300 {
		t.Errorf("stale DNSEndpoint: %+v", r)
	}

	if r := byName["status.example.com"]; r.Health != healthWarning || !slices.Equal(r.Reasons, []string{extDNSReasonNoTarget}) || len(r.Targets) != 0 {
		t.Errorf("noTarget: %+v", r)
	}

	if r := byName["app.example.com"]; r.Health != healthOK || r.Controller != "external-dns" ||
		!slices.Equal(r.Targets, []string{"203.0.113.10"}) || r.RecordType != "A" || r.TTL != 300 ||
		r.SourceKind != "Ingress" || r.SourceGroup != "networking.k8s.io" {
		t.Errorf("ingress record: %+v", r)
	}

	// The target annotation replaces the load balancer address.
	if r := byName["override.example.com"]; !slices.Equal(r.Targets, []string{"203.0.113.70"}) {
		t.Errorf("target override: %+v", r)
	}

	// A hostname target makes a CNAME; the hostname annotation can hold two names.
	if r := byName["extra.example.com"]; r.RecordType != "CNAME" || !slices.Equal(r.Targets, []string{"lb.example.com"}) {
		t.Errorf("hostname annotation: %+v", r)
	}

	// A Service by its internal-hostname annotation, its externalIPs as targets.
	if r := byName["internal.example.com"]; r.SourceKind != "Service" || !slices.Equal(r.Targets, []string{"203.0.113.80"}) || r.TTL != 0 {
		t.Errorf("internal hostname: %+v", r)
	}

	// An HTTPRoute's targets are its parent Gateways' addresses.
	if r := byName["api.example.com"]; r.SourceKind != "HTTPRoute" || !slices.Equal(r.Targets, []string{"203.0.113.30"}) ||
		r.SourceGroup != groupGatewayAPI || r.Controller != "external-dns" {
		t.Errorf("httproute record: %+v", r)
	}

	// The Gateway's target annotation replaces its addresses: a CNAME to the load balancer.
	if r := byName["web.apps.example.com"]; r.SourceName != "web" || !slices.Equal(r.Targets, []string{"lb.example.net"}) ||
		r.RecordType != "CNAME" || r.Health != healthOK || len(r.Reasons) != 0 {
		t.Errorf("gateway target annotation: %+v", r)
	}

	// A route without hostnames takes its listener's; one no listener serves is left out.
	if r := byName["*.apps.example.com"]; r.SourceName != "catchall" || !slices.Equal(r.Targets, []string{"lb.example.net"}) {
		t.Errorf("listener hostname: %+v", r)
	}

	if _, ok := byName["other.example.net"]; ok {
		t.Error("hostname no listener serves kept")
	}

	// A Gateway with neither the annotation nor addresses has nothing to point at yet.
	if r := byName["pending.example.com"]; r.Health != healthWarning || !slices.Equal(r.Reasons, []string{extDNSReasonNoTarget}) {
		t.Errorf("gateway without address: %+v", r)
	}

	// The wildcard target set and the foreign-owned DNSEndpoint are left out.
	if _, ok := byName["wild.example.com"]; ok {
		t.Error("wildcard target kept")
	}

	if _, ok := byName["foreign.example.com"]; ok {
		t.Error("foreign DNSEndpoint kept")
	}

	if out.RecordCount != 13 || out.Truncated {
		t.Errorf("count %d truncated %v", out.RecordCount, out.Truncated)
	}
}

func TestReadExternalDNSCRDAbsent(t *testing.T) {
	out := readExternalDNSFixture(t, "", externalDNSPods())

	if out.Version != "" || out.Error != "" {
		t.Fatalf("version %q error %q", out.Version, out.Error)
	}

	for _, r := range out.Records {
		if r.SourceKind == "DNSEndpoint" {
			t.Errorf("DNSEndpoint record without the CRD: %+v", r)
		}
	}

	if out.RecordCount != 12 {
		t.Errorf("count %d", out.RecordCount)
	}
}

func TestReadExternalDNSIngressForbidden(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/deployments":                      string(readFixture(t, "externaldns/deployments.json")),
		"GET /apis/externaldns.k8s.io/v1alpha1/dnsendpoints": string(readFixture(t, "externaldns/dnsendpoints.json")),
		"GET /api/v1/services":                               string(readFixture(t, "externaldns/services.json")),
	})
	f.answerWith("GET /apis/networking.k8s.io/v1/ingresses", 403, `{"kind":"Status","reason":"Forbidden","message":"ingresses is forbidden"}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	out := readExternalDNS(context.Background(), k, "v1alpha1", externalDNSPods(), nil, fixtureNow)

	if !strings.Contains(out.Error, "forbidden") {
		t.Errorf("error %q", out.Error)
	}

	// The refused listing takes its records away; the rest still shows.
	for _, r := range out.Records {
		if r.SourceKind == "Ingress" {
			t.Errorf("ingress record despite 403: %+v", r)
		}
	}

	if out.RecordCount != 3 {
		t.Errorf("count %d", out.RecordCount)
	}
}

// ExternalDNS runs without its CRD: readDataServices then finds it by its pods, but only
// when the hints let it list them.
func TestReadDataServicesExternalDNSByPods(t *testing.T) {
	pods := podListJSON(t, externalDNSPods()...)
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                                `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`,
		"GET /api/v1/pods":                         pods,
		"GET /apis/apps/v1/deployments":            string(readFixture(t, "externaldns/deployments.json")),
		"GET /apis/networking.k8s.io/v1/ingresses": string(readFixture(t, "externaldns/ingresses.json")),
		"GET /api/v1/services":                     string(readFixture(t, "externaldns/services.json")),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	if res.ExternalDNS == nil || res.ExternalDNS.Version != "" || len(res.ExternalDNS.Controllers) != 2 {
		t.Fatalf("by pods: %+v", res.ExternalDNS)
	}

	// Hints without external-dns skip the pod listing: no section without the CRD.
	res, err = readDataServices(context.Background(), k, nil, parseHints("longhorn"), fixtureNow)
	if err != nil || res.ExternalDNS != nil {
		t.Fatalf("hinted: %+v %v", res.ExternalDNS, err)
	}
}

func TestExternalDNSFilterCovers(t *testing.T) {
	cases := []struct {
		filter, host string
		want         bool
	}{
		{"", "anything.example.com", true},
		{"example.com", "example.com", true},
		{"example.com", "app.example.com", true},
		{"example.com", "notexample.com", false},
		{"example.com", "example.org", false},
		{"*.example.com", "app.example.com", true},
		{"*.example.com", "example.com", false},
		{"Example.com", "APP.EXAMPLE.COM", true},
	}

	for _, c := range cases {
		if got := extDNSFilterCovers(c.filter, c.host); got != c.want {
			t.Errorf("extDNSFilterCovers(%q, %q) = %v", c.filter, c.host, got)
		}
	}
}

func TestExtDNSMatchingHost(t *testing.T) {
	for _, c := range []struct {
		route, listener, want string
		ok                    bool
	}{
		{"app.example.com", "", "app.example.com", true},
		{"", "*.example.com", "*.example.com", true},
		{"App.Example.com", "app.example.com", "app.example.com", true},
		{"app.example.com", "*.example.com", "app.example.com", true},
		{"*.example.com", "app.example.com", "app.example.com", true},
		{"*.apps.example.com", "*.example.com", "*.apps.example.com", true},
		{"*.example.com", "*.apps.example.com", "*.apps.example.com", true},
		{"example.com", "*.example.com", "", false},
		{"app.example.net", "*.example.com", "", false},
		{"app.example.com", "web.example.com", "", false},
	} {
		got, ok := extDNSMatchingHost(c.route, c.listener)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("extDNSMatchingHost(%q, %q) = %q, %v", c.route, c.listener, got, ok)
		}
	}
}

func TestExternalDNSDemo(t *testing.T) {
	out := demoExternalDNS(fixtureNow)

	if out.Version != "v1alpha1" || len(out.Controllers) != 1 || out.Controllers[0].Provider != "cloudflare" ||
		out.Controllers[0].Policy != "sync" || out.Controllers[0].OwnerID != "homelab" ||
		!slices.Equal(out.Controllers[0].DomainFilters, []string{"example.com"}) ||
		!slices.Equal(out.Controllers[0].ManagedRecordTypes, []string{"A", "AAAA", "CNAME"}) {
		t.Fatalf("controller: %+v", out.Controllers)
	}

	seen := map[string]bool{}
	for _, r := range out.Records {
		seen[r.SourceKind+":"+strings.Join(r.Reasons, ",")] = true
	}

	for _, want := range []string{
		"Ingress:", "Service:", "HTTPRoute:", "DNSEndpoint:" + extDNSReasonStale,
		"Ingress:" + extDNSReasonNoTarget, "Ingress:" + extDNSReasonUnmanaged,
	} {
		if !seen[want] {
			t.Errorf("demo lacks %s (seen %v)", want, seen)
		}
	}

	if out.RecordCount != 8 || len(out.Records) != 8 || out.Truncated {
		t.Errorf("count %d truncated %v", out.RecordCount, out.Truncated)
	}

	// The demo section is part of the demo cluster's answer.
	var res dataServices
	data, err := toJSON(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(data), &res); err != nil {
		t.Fatal(err)
	}

	if res.ExternalDNS == nil || res.ExternalDNS.RecordCount != 8 {
		t.Errorf("demo data services: %+v", res.ExternalDNS)
	}
}

// The demo's supported integrations list ExternalDNS as detected.
func TestExternalDNSDemoIntegrations(t *testing.T) {
	res := demoSupportedIntegrations()
	for _, it := range res.Items {
		if it.ID == "external-dns" && (!it.Detected || it.Via != detectedByAPI) {
			t.Errorf("external-dns: %+v", it)
		}
	}
}

// The interval flag accepts both spellings and falls back to its default.
func TestExternalDNSInterval(t *testing.T) {
	var d externalDNSDeployment
	d.Metadata.Namespace, d.Metadata.Name = "external-dns", "external-dns"
	d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, struct {
		Image string   `json:"image"`
		Args  []string `json:"args"`
	}{"registry.k8s.io/external-dns/external-dns:v0.14.0", []string{"--interval", "2m30s"}})

	if c := newExternalDNSController(d); c.IntervalSeconds != 150 {
		t.Errorf("interval %d", c.IntervalSeconds)
	}

	d.Spec.Template.Spec.Containers[0].Args = nil
	if c := newExternalDNSController(d); c.IntervalSeconds != 60 {
		t.Errorf("default interval %d", c.IntervalSeconds)
	}
}

// --managed-record-types reads as CSV and repeated, upper-cased, the default set when unset.
func TestExternalDNSManagedRecordTypes(t *testing.T) {
	tests := []struct {
		args []string
		want []string
	}{
		{nil, []string{"A", "AAAA", "CNAME"}},
		{[]string{"--managed-record-types=cname"}, []string{"CNAME"}},
		{[]string{"--managed-record-types", "A,AAAA"}, []string{"A", "AAAA"}},
		{[]string{"--managed-record-types=A", "--managed-record-types=TXT, a"}, []string{"A", "TXT"}},
	}

	for _, tt := range tests {
		var d externalDNSDeployment
		d.Metadata.Namespace, d.Metadata.Name = "external-dns", "external-dns"
		d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, struct {
			Image string   `json:"image"`
			Args  []string `json:"args"`
		}{"registry.k8s.io/external-dns/external-dns:v0.14.0", tt.args})

		if got := newExternalDNSController(d).ManagedRecordTypes; !slices.Equal(got, tt.want) {
			t.Errorf("args %v: managed types %v, want %v", tt.args, got, tt.want)
		}
	}
}

// With an A-only and a CNAME-only controller on one zone, each record goes to the one that
// publishes its type; a type neither lists still falls back to the domain-filter match.
func TestExternalDNSMatchByRecordType(t *testing.T) {
	controllers := []externalDNSController{
		{Name: "dns-addresses", DomainFilters: []string{"example.com"}, ManagedRecordTypes: []string{"A", "AAAA"}},
		{Name: "dns-aliases", DomainFilters: []string{"example.com"}, ManagedRecordTypes: []string{"CNAME"}},
		{Name: "dns-other", DomainFilters: []string{"example.net"}},
	}

	raw := []extDNSRawRecord{
		{dnsName: "app.example.com", recordType: "A", targets: []string{"203.0.113.10"}},
		{dnsName: "v6.example.com", recordType: "AAAA", targets: []string{"2001:db8::10"}},
		{dnsName: "www.example.com", recordType: "CNAME", targets: []string{"infra.example.com"}},
		{dnsName: "mail.example.com", recordType: "MX", targets: []string{"mx.example.com"}},
		{dnsName: "www.example.net", recordType: "CNAME", targets: []string{"infra.example.com"}},
	}

	records, _, _ := extDNSMatch(controllers, raw)

	got := map[string]string{}
	for _, r := range records {
		got[r.DNSName] = r.Controller
	}

	want := map[string]string{
		"app.example.com":  "dns-addresses",
		"v6.example.com":   "dns-addresses",
		"www.example.com":  "dns-aliases",
		"mail.example.com": "dns-addresses",
		"www.example.net":  "dns-other",
	}

	for name, controller := range want {
		if got[name] != controller {
			t.Errorf("%s by %q, want %q", name, got[name], controller)
		}
	}
}

// The record cap keeps the section bounded; recordCount still counts them all.
func TestExternalDNSRecordCap(t *testing.T) {
	raw := make([]extDNSRawRecord, 0, externalDNSRecordCap+5)
	for i := range externalDNSRecordCap + 5 {
		raw = append(raw, extDNSRawRecord{
			dnsName: "host" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".example.com", recordType: "A",
			source: externalDNSRecord{SourceKind: "DNSEndpoint", SourceNamespace: "demo", SourceName: "many",
				SourceGroup: groupExternalDNS, SourceVersion: "v1alpha1", SourceResource: "dnsendpoints"},
		})
	}

	records, count, truncated := extDNSMatch(nil, raw)
	if count != externalDNSRecordCap+5 || !truncated || len(records) != externalDNSRecordCap {
		t.Errorf("count %d truncated %v records %d", count, truncated, len(records))
	}
}

// A record with no covering controller and no targets carries both reasons.
func TestExternalDNSRecordReasons(t *testing.T) {
	raw := []extDNSRawRecord{{
		dnsName: "lost.example.net", recordType: "A",
		source: externalDNSRecord{SourceKind: "Ingress", SourceNamespace: "demo", SourceName: "lost",
			SourceGroup: "networking.k8s.io", SourceVersion: "v1", SourceResource: "ingresses"},
	}}

	records, _, _ := extDNSMatch([]externalDNSController{{Name: "c", DomainFilters: []string{"example.com"}}}, raw)
	if len(records) != 1 || records[0].Health != healthWarning ||
		!slices.Equal(records[0].Reasons, []string{extDNSReasonUnmanaged, extDNSReasonNoTarget}) {
		t.Errorf("records: %+v", records)
	}
}

// ExternalDNS v0.22 reads external-dns.kubernetes.io/: those annotations count like the
// legacy alpha ones, and the current prefix wins when an object carries both.
func TestExternalDNSAnnotationPrefixes(t *testing.T) {
	var ingresses []ingressObject
	if err := json.Unmarshal([]byte(`[
	  {"metadata": {"name": "web", "namespace": "demo",
	                "annotations": {"external-dns.kubernetes.io/hostname": "extra.example.com",
	                                "external-dns.kubernetes.io/target": "lb.example.net",
	                                "external-dns.kubernetes.io/ttl": "120"}},
	   "spec": {"rules": [{"host": "web.example.com"}]},
	   "status": {"loadBalancer": {"ingress": [{"ip": "203.0.113.10"}]}}},
	  {"metadata": {"name": "other", "namespace": "demo",
	                "annotations": {"external-dns.kubernetes.io/controller": "other"}},
	   "spec": {"rules": [{"host": "other.example.com"}]}}
	]`), &ingresses); err != nil {
		t.Fatal(err)
	}

	var services []serviceListObject
	if err := json.Unmarshal([]byte(`[
	  {"metadata": {"name": "shop", "namespace": "demo",
	                "annotations": {"external-dns.kubernetes.io/hostname": "shop.example.com",
	                                "external-dns.alpha.kubernetes.io/hostname": "old.example.com",
	                                "external-dns.alpha.kubernetes.io/ttl": "60"}},
	   "spec": {"type": "LoadBalancer"},
	   "status": {"loadBalancer": {"ingress": [{"ip": "203.0.113.20"}]}}},
	  {"metadata": {"name": "internal", "namespace": "demo",
	                "annotations": {"external-dns.kubernetes.io/internal-hostname": "internal.example.com"}},
	   "spec": {"type": "LoadBalancer", "externalIPs": ["203.0.113.80"]}},
	  {"metadata": {"name": "skipped", "namespace": "demo",
	                "annotations": {"external-dns.kubernetes.io/hostname": "skipped.example.com",
	                                "external-dns.kubernetes.io/controller": "other"}},
	   "spec": {"type": "LoadBalancer"}}
	]`), &services); err != nil {
		t.Fatal(err)
	}

	byName := map[string]extDNSRawRecord{}
	for _, r := range extDNSRawRecords(nil, ingresses, services, nil, nil, "") {
		byName[r.dnsName] = r
	}

	if len(byName) != 4 {
		t.Fatalf("records: %+v", byName)
	}

	for _, host := range []string{"web.example.com", "extra.example.com"} {
		if r := byName[host]; !slices.Equal(r.targets, []string{"lb.example.net"}) || r.ttl != 120 || r.recordType != "CNAME" {
			t.Errorf("%s: %+v", host, r)
		}
	}

	if r := byName["shop.example.com"]; !slices.Equal(r.targets, []string{"203.0.113.20"}) || r.ttl != 60 {
		t.Errorf("shop: %+v", r)
	}

	if r := byName["internal.example.com"]; !slices.Equal(r.targets, []string{"203.0.113.80"}) {
		t.Errorf("internal: %+v", r)
	}
}

// The demo clock keeps the fixture times stable.
var _ = time.Now

// A dual-stack load balancer makes an A record with its IPv4 address and an AAAA record with
// its IPv6 one, as ExternalDNS publishes it, both from the same Service.
func TestExternalDNSDualStackSplitsRecords(t *testing.T) {
	var svc serviceListObject
	if err := json.Unmarshal([]byte(`{
		"metadata": {"namespace": "demo", "name": "web", "annotations": {"external-dns.alpha.kubernetes.io/hostname": "web.example.com"}},
		"spec": {"type": "LoadBalancer"},
		"status": {"loadBalancer": {"ingress": [{"ip": "203.0.113.10"}, {"ip": "2001:db8::10"}, {"hostname": "lb.example.net"}]}}
	}`), &svc); err != nil {
		t.Fatal(err)
	}

	controllers := []externalDNSController{{Name: "external-dns", DomainFilters: []string{"example.com"}}}
	records, count, _ := extDNSMatch(controllers, extDNSRawRecords(nil, nil, []serviceListObject{svc}, nil, nil, "v1alpha1"))

	if count != 3 || len(records) != 3 {
		t.Fatalf("count %d, records %+v", count, records)
	}

	want := []struct {
		recordType string
		targets    []string
	}{{"A", []string{"203.0.113.10"}}, {"AAAA", []string{"2001:db8::10"}}, {"CNAME", []string{"lb.example.net"}}}

	for i, w := range want {
		r := records[i]
		if r.DNSName != "web.example.com" || r.RecordType != w.recordType || !slices.Equal(r.Targets, w.targets) ||
			r.SourceKind != "Service" || r.SourceNamespace != "demo" || r.SourceName != "web" ||
			r.Controller != "external-dns" || r.Health != healthOK {
			t.Errorf("record %d = %+v, want %s %v", i, r, w.recordType, w.targets)
		}
	}
}

// A hostname with no target yet stays one A record, flagged noTarget.
func TestExternalDNSSplitTargetsEmpty(t *testing.T) {
	groups := extDNSSplitTargets(nil)
	if len(groups) != 1 || groups[0].recordType != "A" || len(groups[0].targets) != 0 {
		t.Errorf("groups %+v", groups)
	}
}
