package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The ExternalDNS built-in plugin against what the Data services reader it replaced said
// about the same objects (its fixtures, testdata/externaldns/): the controllers and their
// flags, the thirteen records in the reader's order with their first reason, the rows opening
// their source objects, the findings for the monitor, and the hostname check.

const externalDNSGroups = `{"groups":[{"name":"externaldns.k8s.io","preferredVersion":{"version":"v1alpha1"}}]}`

func externalDNSRoutes(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"GET /apis":                     externalDNSGroups,
		"GET /apis/apps/v1/deployments": string(readFixture(t, "externaldns/deployments.json")),
		"GET /apis/externaldns.k8s.io/v1alpha1/dnsendpoints": string(readFixture(t, "externaldns/dnsendpoints.json")),
		"GET /apis/networking.k8s.io/v1/ingresses":           string(readFixture(t, "externaldns/ingresses.json")),
		"GET /api/v1/services":                               string(readFixture(t, "externaldns/services.json")),
		"GET /apis/gateway.networking.k8s.io/v1/httproutes":  string(readFixture(t, "externaldns/httproutes.json")),
		"GET /apis/gateway.networking.k8s.io/v1/gateways":    string(readFixture(t, "externaldns/gateways.json")),
	}
}

// externalDNSTree renders the plugin, alone and at fixtureNow, over routes in scope; refused
// answers 403 to those requests.
func externalDNSTree(t *testing.T, routes map[string]string, scope string, refused ...string) (plugin.Tree, *fakeKubeAPI) {
	t.Helper()

	useBuiltinPlugin(t, "external-dns", fixtureNow)

	f := newFakeKubeAPI(t, routes)
	for _, r := range refused {
		f.answerWith(r, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"read refused"}`)
	}

	useFakeKube(t, f)

	return pluginData(t, "cfg", "ctx", "", "external-dns", scope, ""), f
}

// rowsByTitle are a card's rows by "<title> <subtitle>" (a hostname has a row per family).
func rowsByTitle(rows []plugin.TreeRow) map[string]plugin.TreeRow {
	out := map[string]plugin.TreeRow{}
	for _, r := range rows {
		out[r.Title] = r
		out[r.Title+" "+r.Subtitle] = r
	}

	return out
}

func rowTitles(rows []plugin.TreeRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Title)
	}

	return out
}

// Both controllers with the flags of both arg styles, the legacy one critical; the thirteen
// records in the reader's order: the four warnings first, then the rest by name.
func TestExternalDNSPluginMatchesTheReader(t *testing.T) {
	tree, _ := externalDNSTree(t, externalDNSRoutes(t), "")

	if !tree.Active || len(tree.Errors) != 0 || tree.Summary.Title != "13 hostnames" || tree.Summary.Health != healthCritical {
		t.Fatalf("active %v, summary %+v, errors %+v", tree.Active, tree.Summary, tree.Errors)
	}

	controllers := rookCard(t, tree, "controllers").Rows
	if len(controllers) != 2 {
		t.Fatalf("controllers %+v", controllers)
	}

	legacy, main := controllers[0], controllers[1]
	kv := rowItems(legacy, "keyValues")

	// One reason per object: the reader listed notReady and dryRun.
	if legacy.Title != "external-dns-legacy" || legacy.Health.Tone != healthCritical || legacy.Health.Reason != "notReady" ||
		legacy.Status.Text != "not ready" || legacy.Trailing.Text != "0/2" ||
		kv["Provider"]["value"] != "inmemory" || kv["Owner id"]["value"] != "legacy" || kv["Domain filters"]["value"] != "example.org" ||
		kv["Annotation filter"]["value"] != "app.kubernetes.io/managed-by=legacy" {
		t.Errorf("legacy controller %+v %v", legacy, kv)
	}

	want := plugin.Ref{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespace: "dns-2", Name: "external-dns-legacy"}
	if legacy.Ref == nil || *legacy.Ref != want {
		t.Errorf("legacy ref %+v", legacy.Ref)
	}

	kv = rowItems(main, "keyValues")
	if main.Health.Tone != healthOK || main.Status.Text != "ready" || main.Subtitle != "external-dns · cloudflare · sync · homelab" ||
		kv["Policy"]["value"] != "sync" || kv["Registry"]["value"] != "txt" || kv["Interval"]["value"] != "30 s" ||
		kv["Sources"]["value"] != "ingress, service, crd, gateway-httproute" || kv["Record types"]["value"] != "A, AAAA, CNAME" {
		t.Errorf("main controller %+v %v", main, kv)
	}

	records := rookCard(t, tree, "records").Rows

	wantNames := []string{"*.example.net", "db.example.com", "pending.example.com", "status.example.com",
		"*.apps.example.com", "api.example.com", "app.example.com", "extra.example.com", "extra2.example.com",
		"internal.example.com", "override.example.com", "shop.example.com", "web.apps.example.com"}
	if got := rowTitles(records); !slices.Equal(got, wantNames) {
		t.Fatalf("records %v, want %v", got, wantNames)
	}

	byName := rowsByTitle(records)

	checkRecord := func(name, tone, reason, controller, targets string) plugin.TreeRow {
		t.Helper()

		r := byName[name]
		kv := rowItems(r, "keyValues")
		value := func(label string) string { s, _ := kv[label]["value"].(string); return s }

		if r.Health == nil || r.Health.Tone != tone || r.Health.Reason != reason || value("Controller") != controller || value("Targets") != targets {
			t.Errorf("%s: health %+v, keyValues %v", name, r.Health, kv)
		}

		return r
	}

	checkRecord("*.example.net", healthWarning, "unmanaged", "", "203.0.113.50")

	db := checkRecord("db.example.com", healthWarning, "stale", "external-dns/external-dns", "203.0.113.40")
	if db.Trailing.Text != "TTL 300" || db.Ref == nil ||
		*db.Ref != (plugin.Ref{Group: groupExternalDNS, Version: "v1alpha1", Resource: "dnsendpoints", Kind: "DNSEndpoint", Namespace: "demo", Name: "db"}) {
		t.Errorf("stale DNSEndpoint %+v", db)
	}

	checkRecord("status.example.com", healthWarning, "noTarget", "external-dns/external-dns", "")

	app := checkRecord("app.example.com", healthOK, "", "external-dns/external-dns", "203.0.113.10")
	if app.Subtitle != "A · Ingress demo/app" || app.Trailing.Text != "TTL 300" ||
		*app.Ref != (plugin.Ref{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespace: "demo", Name: "app"}) {
		t.Errorf("ingress record %+v", app)
	}

	// The target annotation replaces the load balancer address.
	checkRecord("override.example.com", healthOK, "", "external-dns/external-dns", "203.0.113.70")

	// A hostname target makes a CNAME; the hostname annotation can hold two names.
	if r := checkRecord("extra.example.com", healthOK, "", "external-dns/external-dns", "lb.example.com"); !strings.HasPrefix(r.Subtitle, "CNAME · ") {
		t.Errorf("hostname annotation %+v", r)
	}

	// A Service by its internal-hostname annotation, its externalIPs as targets.
	if r := checkRecord("internal.example.com", healthOK, "", "external-dns/external-dns", "203.0.113.80"); r.Ref.Kind != "Service" || r.Trailing.Text != "" {
		t.Errorf("internal hostname %+v", r)
	}

	// An HTTPRoute's targets are its parent Gateways' addresses.
	if r := checkRecord("api.example.com", healthOK, "", "external-dns/external-dns", "203.0.113.30"); r.Ref.Kind != "HTTPRoute" || r.Ref.Group != groupGatewayAPI {
		t.Errorf("httproute record %+v", r)
	}

	// The Gateway's target annotation replaces its addresses: a CNAME to the load balancer.
	if r := checkRecord("web.apps.example.com", healthOK, "", "external-dns/external-dns", "lb.example.net"); r.Ref.Name != "web" || !strings.HasPrefix(r.Subtitle, "CNAME · ") {
		t.Errorf("gateway target annotation %+v", r)
	}

	// A route without hostnames takes its listener's; a Gateway without address has no target.
	if r := checkRecord("*.apps.example.com", healthOK, "", "external-dns/external-dns", "lb.example.net"); r.Ref.Name != "catchall" {
		t.Errorf("listener hostname %+v", r)
	}

	checkRecord("pending.example.com", healthWarning, "noTarget", "external-dns/external-dns", "")

	// Left out: a hostname no listener serves, a wildcard target set, a foreign DNSEndpoint.
	for _, name := range []string{"other.example.net", "wild.example.com", "foreign.example.com"} {
		if _, ok := byName[name]; ok {
			t.Errorf("%s kept", name)
		}
	}

	wantBadges := map[string]string{"controllers down": "1", "dry-run": "1", "unmanaged": "1", "without target": "2"}
	for _, b := range tree.Summary.Badges {
		if wantBadges[b.Label] != b.Value {
			t.Errorf("badge %+v", b)
		}
	}
}

// The DNSEndpoint CRD absent: ExternalDNS is found by its pods, its records come from the
// Ingresses, Services and HTTPRoutes only, and the CRD is never asked for.
func TestExternalDNSPluginWithoutTheCRD(t *testing.T) {
	routes := externalDNSRoutes(t)
	routes["GET /apis"] = `{"groups":[]}`
	routes["GET /api/v1/pods"] = podListJSON(t,
		fakePod("external-dns", "external-dns-0", "node-1", true, nil, "external-dns", "registry.k8s.io/external-dns/external-dns:v0.14.0"),
		fakePod("kube-system", "coredns-0", "node-1", true, nil, "coredns", "registry.k8s.io/coredns/coredns:v1.11.1"))

	tree, f := externalDNSTree(t, routes, "")
	if !tree.Active || tree.DetectedVia != detectedByPods || len(tree.Errors) != 0 {
		t.Fatalf("active %v via %q errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
	}

	records := rookCard(t, tree, "records").Rows
	if len(records) != 12 {
		t.Errorf("records %v", rowTitles(records))
	}

	for _, r := range records {
		if r.Ref.Kind == "DNSEndpoint" {
			t.Errorf("DNSEndpoint record without the CRD: %+v", r)
		}
	}

	for _, r := range f.recorded() {
		if strings.Contains(r.path, "dnsendpoints") {
			t.Errorf("asked for %s", r.path)
		}
	}

	// Without the CRD nor a running pod, it is not there.
	routes["GET /api/v1/pods"] = `{"items":[]}`
	if tree, _ := externalDNSTree(t, routes, ""); tree.Active {
		t.Error("active without ExternalDNS")
	}
}

// A refused Ingress list is the records' error, as it was the reader's: its records go, the
// Service, DNSEndpoint and HTTPRoute ones still show, and so do the controllers.
func TestExternalDNSPluginIngressForbidden(t *testing.T) {
	tree, _ := externalDNSTree(t, externalDNSRoutes(t), "", "GET /apis/networking.k8s.io/v1/ingresses")

	if len(tree.Errors) != 1 || tree.Errors[0].Resource != "externaldnsrecords.ichor.dev" || !strings.Contains(tree.Errors[0].Message, "permission denied: read refused") {
		t.Fatalf("errors %+v", tree.Errors)
	}

	if rows := rookCard(t, tree, "controllers").Rows; len(rows) != 2 {
		t.Errorf("controllers %+v", rows)
	}

	records := rookCard(t, tree, "records").Rows
	byName := rowsByTitle(records)

	for _, name := range []string{"db.example.com", "shop.example.com", "internal.example.com", "api.example.com"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("%s lost: %v", name, rowTitles(records))
		}
	}

	for _, r := range records {
		if r.Ref.Kind == "Ingress" {
			t.Errorf("ingress record despite 403: %+v", r)
		}
	}

	if len(records) != 7 {
		t.Errorf("records %v", rowTitles(records))
	}

	// Two refused lists: one entry naming both.
	tree, _ = externalDNSTree(t, externalDNSRoutes(t), "", "GET /apis/networking.k8s.io/v1/ingresses", "GET /api/v1/services")
	if len(tree.Errors) != 1 || strings.Count(tree.Errors[0].Message, "read refused") != 1 || len(rookCard(t, tree, "records").Rows) != 5 {
		t.Errorf("errors %+v, records %v", tree.Errors, rowTitles(rookCard(t, tree, "records").Rows))
	}

	// The Gateway API CRDs missing is not an error.
	routes := externalDNSRoutes(t)
	delete(routes, "GET /apis/gateway.networking.k8s.io/v1/httproutes")
	delete(routes, "GET /apis/gateway.networking.k8s.io/v1/gateways")

	if tree, _ := externalDNSTree(t, routes, ""); len(tree.Errors) != 0 || len(rookCard(t, tree, "records").Rows) != 9 {
		t.Errorf("without Gateway API: errors %+v, records %v", tree.Errors, rowTitles(rookCard(t, tree, "records").Rows))
	}
}

// In a namespace, the records still match the controllers that run elsewhere.
func TestExternalDNSPluginScoped(t *testing.T) {
	routes := externalDNSRoutes(t)
	for _, res := range []string{"/apis/externaldns.k8s.io/v1alpha1/%sdnsendpoints", "/apis/networking.k8s.io/v1/%singresses", "/api/v1/%sservices", "/apis/gateway.networking.k8s.io/v1/%shttproutes"} {
		routes["GET "+fmt.Sprintf(res, "namespaces/demo/")] = routes["GET "+fmt.Sprintf(res, "")]
	}

	routes["GET /apis/apps/v1/namespaces/demo/deployments"] = `{"items":[]}`

	tree, _ := externalDNSTree(t, routes, "demo")
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	app := rowsByTitle(rookCard(t, tree, "records").Rows)["app.example.com"]
	if kv := rowItems(app, "keyValues"); kv["Controller"]["value"] != "external-dns/external-dns" || app.Health.Tone != healthOK {
		t.Errorf("app %+v %v", app.Health, kv)
	}
}

// One reason per record, the first rule's (a departure from the reader, which listed them
// all); the badges still count each.
func TestExternalDNSPluginOneReasonPerRecord(t *testing.T) {
	routes := externalDNSRoutes(t)
	routes["GET /apis/networking.k8s.io/v1/ingresses"] = `{"items":[{"metadata":{"name":"lost","namespace":"demo"},"spec":{"rules":[{"host":"lost.example.net"}]}}]}`

	tree, _ := externalDNSTree(t, routes, "")

	lost := rowsByTitle(rookCard(t, tree, "records").Rows)["lost.example.net"]
	if lost.Health == nil || lost.Health.Reason != "unmanaged" || lost.Status.Text != "unmanaged" {
		t.Fatalf("lost %+v", lost)
	}

	if got := findingsOf(tree); !slices.Contains(got, "info record-unmanaged lost.example.net") || !slices.Contains(got, "warning record-no-target lost.example.net") {
		t.Errorf("findings %v", got)
	}
}

// More records than the list keeps: the first 500, worst first, and the tree says partial.
func TestExternalDNSPluginRecordCap(t *testing.T) {
	items := make([]string, 0, 505)
	for i := range 505 {
		items = append(items, fmt.Sprintf(`{"metadata":{"name":"svc-%d","namespace":"demo","annotations":{"external-dns.alpha.kubernetes.io/hostname":"host-%03d.example.com"}},"spec":{"externalIPs":["203.0.113.1"]}}`, i, i))
	}

	// One without a target sorts first, so the cut keeps it.
	items = append(items, `{"metadata":{"name":"zzz","namespace":"demo","annotations":{"external-dns.alpha.kubernetes.io/hostname":"zzz.example.com"}},"spec":{}}`)

	routes := externalDNSRoutes(t)
	routes["GET /api/v1/services"] = `{"items":[` + strings.Join(items, ",") + `]}`
	routes["GET /apis/networking.k8s.io/v1/ingresses"] = `{"items":[]}`
	routes["GET /apis/externaldns.k8s.io/v1alpha1/dnsendpoints"] = `{"items":[]}`
	routes["GET /apis/gateway.networking.k8s.io/v1/httproutes"] = `{"items":[]}`

	tree, _ := externalDNSTree(t, routes, "")

	records := rookCard(t, tree, "records").Rows
	if !tree.Partial || !tree.Summary.Partial || len(records) != 500 || records[0].Title != "zzz.example.com" {
		t.Fatalf("partial %v, %d records, first %s", tree.Partial, len(records), records[0].Title)
	}
}

// What the monitor and the checkup read: a controller with no replica ready is critical, a
// record without a target a warning, the rest is shown, not notified.
func TestExternalDNSPluginFindingsForTheMonitor(t *testing.T) {
	useBuiltinPlugin(t, "external-dns", fixtureNow)
	useFakeKube(t, newFakeKubeAPI(t, externalDNSRoutes(t)))

	raw, err := KubePluginFindings("cfg", "ctx", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var report pluginFindingsReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, f := range report.Findings {
		got = append(got, f.Severity+" "+f.Key)
	}

	want := []string{
		"critical plugin:external-dns:controller-down:dns-2/external-dns-legacy",
		"warning plugin:external-dns:record-no-target:demo/status",
		"warning plugin:external-dns:record-no-target:demo/waiting",
		"info plugin:external-dns:controller-dry-run:dns-2/external-dns-legacy",
		"info plugin:external-dns:record-stale:demo/db",
		"info plugin:external-dns:record-unmanaged:demo/legacy",
	}
	if !slices.Equal(got, want) || len(report.Errors) != 0 {
		t.Fatalf("findings %v, errors %+v", got, report.Errors)
	}
}

// The hostname check over the fixture, against a stub resolver: each name's verdict, its
// row opening the record's source, the owner read from the TXT registry.
func TestExternalDNSPluginCheckNames(t *testing.T) {
	useBuiltinPlugin(t, "external-dns", fixtureNow)
	useFakeKube(t, newFakeKubeAPI(t, externalDNSRoutes(t)))

	saved := dnsCheckResolver
	dnsCheckResolver = stubDNS{
		addresses: map[string][]string{"app.example.com": {"203.0.113.10"}, "shop.example.com": {"198.51.100.7"}},
		txts: map[string][]string{
			"a-app.example.com":  {"heritage=external-dns,external-dns/owner=homelab"},
			"a-shop.example.com": {"heritage=external-dns,external-dns/owner=other-cluster"},
		},
	}

	t.Cleanup(func() { dnsCheckResolver = saved })

	out, err := KubePluginTool("cfg", "ctx", "", "external-dns", "checkNames", "", "fr")
	if err != nil {
		t.Fatal(err)
	}

	var res plugin.ToolResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Title != "1 noms sur 13 pointent vers ce cluster" || res.Health != healthCritical || len(res.Cards) != 1 {
		t.Fatalf("result %s", out)
	}

	rows := rowsByTitle(res.Cards[0].Rows)

	app := rows["app.example.com"]
	if app.Status.Tone != healthOK || app.Status.Text != "Résout ici" || rowItems(app, "keyValues")["Propriétaire"]["value"] != "homelab" ||
		*app.Ref != (plugin.Ref{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Kind: "Ingress", Namespace: "demo", Name: "app"}) {
		t.Errorf("app %+v", app)
	}

	shop := rows["shop.example.com"]
	if shop.Status.Tone != healthWarning || shop.Status.Text != "Pointe ailleurs: 198.51.100.7" ||
		rowItems(shop, "keyValues")["Propriétaire"]["value"] != "appartient à other-cluster" || rowSection(shop, "text") == nil {
		t.Errorf("shop %+v", shop)
	}

	if db := rows["db.example.com"]; db.Status.Tone != healthCritical || db.Status.Text != "Introuvable" || res.Cards[0].Rows[0].Status.Tone != healthCritical {
		t.Errorf("db %+v", db)
	}
}

// Data services no longer carry ExternalDNS: the plugin does, and Settings lists it from it.
func TestDataServicesHaveNoExternalDNS(t *testing.T) {
	raw, err := json.Marshal(demoDataServices(fixtureNow))
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(jsonKeys(raw), "externalDns") {
		t.Error("the Data services JSON still has an externalDns key")
	}

	found := false

	for _, it := range demoSupportedIntegrations().Items {
		if it.ID == "external-dns" {
			found = it.Detected
		}
	}

	if !found {
		t.Error("the demo's integrations lack ExternalDNS")
	}
}

// The demo renders the plugin's samples, as the former demo, and its check gives the canned
// verdicts offline.
func TestExternalDNSPluginDemo(t *testing.T) {
	useBuiltinPlugin(t, "external-dns", fixtureNow)
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)

	tree := pluginData(t, demo, demoKubeContext, "", "external-dns", "", "")
	if tree.Summary.Title != "8 hostnames" || tree.Summary.Health != healthWarning || len(tree.Errors) != 0 {
		t.Fatalf("summary %+v, errors %+v", tree.Summary, tree.Errors)
	}

	want := []string{"info record-stale db.example.com", "info record-unmanaged *.example.org", "warning record-no-target status.example.com"}
	if got := findingsOf(tree); !slices.Equal(got, want) {
		t.Fatalf("findings %v", got)
	}

	out, err := KubePluginTool(demo, demoKubeContext, "", "external-dns", "checkNames", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var res plugin.ToolResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Title != "4 of 8 names point at this cluster" || res.Health != healthCritical || len(res.Cards[0].Rows) != 8 {
		t.Errorf("demo check %s", out)
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

// extDNSTestDeployment is an external-dns Deployment running with args.
func extDNSTestDeployment(args ...string) externalDNSDeployment {
	var d externalDNSDeployment
	d.Metadata.Namespace, d.Metadata.Name = "external-dns", "external-dns"
	d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, struct {
		Image string   `json:"image"`
		Args  []string `json:"args"`
	}{"registry.k8s.io/external-dns/external-dns:v0.14.0", args})

	return d
}

// The interval flag accepts both spellings and falls back to its default.
func TestExternalDNSInterval(t *testing.T) {
	if c := newExternalDNSController(extDNSTestDeployment("--interval", "2m30s")); c.IntervalSeconds != 150 {
		t.Errorf("interval %d", c.IntervalSeconds)
	}

	if c := newExternalDNSController(extDNSTestDeployment()); c.IntervalSeconds != 60 {
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
		if got := newExternalDNSController(extDNSTestDeployment(tt.args...)).ManagedRecordTypes; !slices.Equal(got, tt.want) {
			t.Errorf("args %v: managed types %v, want %v", tt.args, got, tt.want)
		}
	}
}

// With an A-only and a CNAME-only controller on one zone, each record goes to the one that
// publishes its type; a type neither lists still falls back to the domain-filter match.
func TestExternalDNSMatchByRecordType(t *testing.T) {
	controllers := []externalDNSController{
		{Namespace: "dns", Name: "dns-addresses", DomainFilters: []string{"example.com"}, ManagedRecordTypes: []string{"A", "AAAA"}},
		{Namespace: "dns", Name: "dns-aliases", DomainFilters: []string{"example.com"}, ManagedRecordTypes: []string{"CNAME"}},
		{Namespace: "dns", Name: "dns-other", DomainFilters: []string{"example.net"}},
	}

	raw := []extDNSRawRecord{
		{dnsName: "app.example.com", recordType: "A", targets: []string{"203.0.113.10"}},
		{dnsName: "v6.example.com", recordType: "AAAA", targets: []string{"2001:db8::10"}},
		{dnsName: "www.example.com", recordType: "CNAME", targets: []string{"infra.example.com"}},
		{dnsName: "mail.example.com", recordType: "MX", targets: []string{"mx.example.com"}},
		{dnsName: "www.example.net", recordType: "CNAME", targets: []string{"infra.example.com"}},
	}

	got := map[string]string{}
	for _, r := range extDNSMatch(controllers, raw) {
		got[r.DNSName] = r.Controller
	}

	want := map[string]string{
		"app.example.com":  "dns/dns-addresses",
		"v6.example.com":   "dns/dns-addresses",
		"www.example.com":  "dns/dns-aliases",
		"mail.example.com": "dns/dns-addresses",
		"www.example.net":  "dns/dns-other",
	}

	for name, controller := range want {
		if got[name] != controller {
			t.Errorf("%s by %q, want %q", name, got[name], controller)
		}
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

	controllers := []externalDNSController{{Namespace: "external-dns", Name: "external-dns", DomainFilters: []string{"example.com"}}}
	records := extDNSMatch(controllers, extDNSRawRecords(nil, nil, []serviceListObject{svc}, nil, nil, "v1alpha1"))

	if len(records) != 3 {
		t.Fatalf("records %+v", records)
	}

	want := []struct {
		recordType string
		targets    []string
	}{{"A", []string{"203.0.113.10"}}, {"AAAA", []string{"2001:db8::10"}}, {"CNAME", []string{"lb.example.net"}}}

	for i, w := range want {
		r := records[i]
		if r.DNSName != "web.example.com" || r.RecordType != w.recordType || !slices.Equal(r.Targets, w.targets) ||
			r.SourceKind != "Service" || r.source.Namespace != "demo" || r.source.Name != "web" ||
			r.Controller != "external-dns/external-dns" || r.warns() {
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
