package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const apisTailscale = `{"groups":[{"name":"tailscale.com","preferredVersion":{"version":"v1alpha1"}}]}`

func tailscaleFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "tailscale/"+name))
}

// tailscaleAnswers is a cluster running the Tailscale operator. The fake API answers by path,
// so the operator Deployment is found by the chart's label on the first list.
func tailscaleAnswers(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"GET /apis": apisTailscale,
		"GET /apis/tailscale.com/v1alpha1/connectors":   tailscaleFixture(t, "connectors.json"),
		"GET /apis/tailscale.com/v1alpha1/proxygroups":  tailscaleFixture(t, "proxygroups.json"),
		"GET /apis/tailscale.com/v1alpha1/proxyclasses": tailscaleFixture(t, "proxyclasses.json"),
		"GET /apis/tailscale.com/v1alpha1/dnsconfigs":   tailscaleFixture(t, "dnsconfigs.json"),
		"GET /apis/apps/v1/deployments":                 tailscaleFixture(t, "deployments.json"),
	}
}

func tailscaleReadFrom(t *testing.T, f *fakeKubeAPI) tsReport {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readTailscale(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func tsConnectorByName(t *testing.T, r tsReport, name string) tsConnector {
	t.Helper()

	for _, c := range r.Connectors {
		if c.Name == name {
			return c
		}
	}

	t.Fatalf("no connector %s in %+v", name, r.Connectors)

	return tsConnector{}
}

func tsProxyGroupByName(t *testing.T, r tsReport, name string) tsProxyGroup {
	t.Helper()

	for _, g := range r.ProxyGroups {
		if g.Name == name {
			return g
		}
	}

	t.Fatalf("no proxy group %s in %+v", name, r.ProxyGroups)

	return tsProxyGroup{}
}

func TestTailscaleRead(t *testing.T) {
	f := newFakeKubeAPI(t, tailscaleAnswers(t))
	r := tailscaleReadFrom(t, f)

	if !r.Installed || r.Version != "v1alpha1" || r.Error != "" || r.Truncated {
		t.Fatalf("report header %+v", r)
	}

	want := tsOperator{Namespace: "tailscale", Name: "operator", Ready: 1, Desired: 1, Image: "tailscale/k8s-operator:v1.90.0", Health: tsHealthOK}
	if r.Operator == nil || *r.Operator != want {
		t.Errorf("operator %+v", r.Operator)
	}

	// Problems first (critical, warning, ok), then by name.
	var names []string
	for _, c := range r.Connectors {
		names = append(names, c.Name)
	}
	if want := []string{"saas-connector", "edge-router", "lab-router", "stale-router", "exit-eu", "office-router"}; !slices.Equal(names, want) {
		t.Errorf("connector order %v", names)
	}

	names = nil
	for _, g := range r.ProxyGroups {
		names = append(names, g.Name)
	}
	if want := []string{"api-proxies", "old-proxies", "egress-proxies", "ingress-proxies"}; !slices.Equal(names, want) {
		t.Errorf("proxy group order %v", names)
	}

	if want := (tsCounts{OK: 6, Warning: 5, Critical: 3}); r.Counts != want {
		t.Errorf("counts %+v", r.Counts)
	}

	// Only the chart's label list was needed to find the operator.
	for _, req := range f.recorded() {
		if req.path == "/apis/apps/v1/deployments" && !strings.Contains(req.query, "labelSelector") {
			t.Errorf("unlabelled deployments list: %+v", req)
		}
	}
}

func TestTailscaleConnectorRows(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))

	office := tsConnectorByName(t, r, "office-router")
	if office.Hostname != "office-router" || office.SubnetRoutes != "10.0.0.0/24,10.0.1.0/24" || office.Replicas != 2 ||
		office.ProxyClass != "prod" || !office.Ready || office.Health != tsHealthOK || office.Reason != "ConnectorCreated" ||
		office.ExitNode || office.AppConnector {
		t.Errorf("office-router %+v", office)
	}
	if !slices.Equal(office.AdvertisedRoutes, []string{"10.0.0.0/24", "10.0.1.0/24"}) ||
		!slices.Equal(office.TailnetIPs, []string{"100.64.0.11", "fd7a:115c:a1e0::b"}) ||
		!slices.Equal(office.Tags, []string{"tag:k8s", "tag:office"}) {
		t.Errorf("office-router lists %+v", office)
	}
	if len(office.Devices) != 2 || office.Devices[0].Hostname != "office-router-0" ||
		!slices.Equal(office.Devices[0].StaticEndpoints, []string{"198.51.100.7:41641"}) || office.Devices[1].StaticEndpoints == nil {
		t.Errorf("office-router devices %+v", office.Devices)
	}

	// No status hostname: the spec's; replicas default to 1.
	exit := tsConnectorByName(t, r, "exit-eu")
	if !exit.ExitNode || exit.Hostname != "exit-eu-spec" || exit.Replicas != 1 || exit.Health != tsHealthOK || exit.Devices == nil {
		t.Errorf("exit-eu %+v", exit)
	}

	saas := tsConnectorByName(t, r, "saas-connector")
	if !saas.AppConnector || saas.Ready || saas.Health != tsHealthCritical || saas.Reason != "ConnectorCreationFailed" ||
		saas.Message != "error provisioning StatefulSet" || !slices.Equal(saas.AppConnectorRoutes, []string{"203.0.113.0/24"}) {
		t.Errorf("saas-connector %+v", saas)
	}

	// No condition: not reconciled, a warning; its missing ProxyClass does not change the reason.
	if lab := tsConnectorByName(t, r, "lab-router"); lab.Ready || lab.Health != tsHealthWarning || lab.Reason != tsReasonNotReconciled {
		t.Errorf("lab-router %+v", lab)
	}
	// Ready, but its ProxyClass does not exist.
	if edge := tsConnectorByName(t, r, "edge-router"); !edge.Ready || edge.Health != tsHealthWarning || edge.Reason != tsReasonProxyClassMissing {
		t.Errorf("edge-router %+v", edge)
	}
	// The condition was observed before the latest spec.
	if stale := tsConnectorByName(t, r, "stale-router"); stale.Ready || stale.Health != tsHealthWarning || stale.Reason != tsReasonReconciling {
		t.Errorf("stale-router %+v", stale)
	}
}

func TestTailscaleProxyRows(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))

	if g := tsProxyGroupByName(t, r, "ingress-proxies"); g.Type != "ingress" || g.Replicas != 3 || !g.Ready || !g.Available ||
		g.Health != tsHealthOK || len(g.Devices) != 1 || !slices.Equal(g.Tags, []string{"tag:k8s"}) {
		t.Errorf("ingress-proxies %+v", g)
	}
	// Some proxies run: a warning; replicas default to 2.
	if g := tsProxyGroupByName(t, r, "egress-proxies"); g.Ready || !g.Available || g.Health != tsHealthWarning || g.Replicas != 2 ||
		g.Message != "1/2 ProxyGroup pods running" {
		t.Errorf("egress-proxies %+v", g)
	}
	if g := tsProxyGroupByName(t, r, "api-proxies"); g.Type != "kube-apiserver" || g.URL != "https://api-proxies.example.ts.net" ||
		g.Available || g.Health != tsHealthCritical {
		t.Errorf("api-proxies %+v", g)
	}
	// An older operator without ProxyGroupAvailable: ProxyGroupReady False alone is critical.
	if g := tsProxyGroupByName(t, r, "old-proxies"); g.Available || g.Health != tsHealthCritical {
		t.Errorf("old-proxies %+v", g)
	}

	// Two users of prod (a connector and a proxy group); metrics does not validate.
	if want := []tsProxyClass{
		{Name: "metrics", tsState: tsState{Reason: "ProxyClassInvalid", Message: "ServiceMonitor CRD not installed", Health: tsHealthWarning}},
		{Name: "prod", UsedBy: 2, tsState: tsState{Ready: true, Reason: "ProxyClassValid", Health: tsHealthOK}},
	}; !slices.Equal(r.ProxyClasses, want) {
		t.Errorf("proxy classes %+v", r.ProxyClasses)
	}

	if want := (tsDNSConfig{Name: "ts-dns", NameserverIP: "10.96.0.53", tsState: tsState{Ready: true, Reason: "NameserverCreated", Health: tsHealthOK}}); r.DNSConfig == nil || *r.DNSConfig != want {
		t.Errorf("dns config %+v", r.DNSConfig)
	}
}

func TestTailscaleNotInstalled(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`}))

	out, err := toJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Installed || !strings.Contains(out, `"installed":false`) || !strings.Contains(out, `"connectors":[]`) ||
		!strings.Contains(out, `"operator":null`) {
		t.Errorf("not installed: %s", out)
	}
}

func TestTailscalePartialRead(t *testing.T) {
	f := newFakeKubeAPI(t, tailscaleAnswers(t))
	f.answerWith("GET /apis/tailscale.com/v1alpha1/proxygroups", http.StatusForbidden, forbiddenStatus)

	// The proxy groups are forbidden: the rest shows and the error says why.
	r := tailscaleReadFrom(t, f)
	if r.Error == "" || len(r.ProxyGroups) != 0 || len(r.Connectors) != 6 || r.DNSConfig == nil {
		t.Errorf("proxy groups forbidden %+v", r)
	}

	// Without the ProxyClasses, a missing one cannot be told: edge-router stays ok.
	f.answerWith("GET /apis/tailscale.com/v1alpha1/proxyclasses", http.StatusForbidden, forbiddenStatus)
	r = tailscaleReadFrom(t, f)
	if edge := tsConnectorByName(t, r, "edge-router"); edge.Health != tsHealthOK {
		t.Errorf("edge-router without classes %+v", edge)
	}
}

func TestTailscaleOlderOperator(t *testing.T) {
	answers := tailscaleAnswers(t)
	delete(answers, "GET /apis/tailscale.com/v1alpha1/proxygroups")
	delete(answers, "GET /apis/tailscale.com/v1alpha1/dnsconfigs")

	// The kinds an older operator does not serve (404) are none, not an error.
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, answers))
	if r.Error != "" || len(r.ProxyGroups) != 0 || r.DNSConfig != nil || len(r.Connectors) != 6 {
		t.Errorf("older operator %+v", r)
	}
}

func TestTailscaleOperator(t *testing.T) {
	answers := tailscaleAnswers(t)
	answers["GET /apis/apps/v1/deployments"] = `{"items":[]}`

	// No operator Deployment: null, and the counts are the objects' only.
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, answers))
	if r.Operator != nil || r.Error != "" || r.Counts != (tsCounts{OK: 5, Warning: 5, Critical: 3}) {
		t.Errorf("no operator %+v %+v", r.Operator, r.Counts)
	}

	answers["GET /apis/apps/v1/deployments"] = strings.Replace(tailscaleFixture(t, "deployments.json"), `"readyReplicas":1`, `"readyReplicas":0`, 1)
	if r := tailscaleReadFrom(t, newFakeKubeAPI(t, answers)); r.Operator == nil || r.Operator.Health != tsHealthCritical {
		t.Errorf("operator down %+v", r.Operator)
	}
}

func TestPickTailscaleOperator(t *testing.T) {
	dep := func(ns, name, image string) tsDeploymentObject {
		var d tsDeploymentObject
		d.Metadata.Namespace, d.Metadata.Name = ns, name
		d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, struct {
			Image string `json:"image"`
		}{image})

		return d
	}

	// Without the chart's label: named like an operator and running the tailscale image. A
	// proxy runs the tailscale image too, but is no operator.
	all := []tsDeploymentObject{
		dep("web", "operator", "nginx:1.29"),
		dep("tailscale", "ts-proxy", "tailscale/tailscale:v1.90.0"),
		dep("tailscale", "operator", "ghcr.io/tailscale/k8s-operator:v1.90.0"),
	}
	if got := pickTailscaleOperator(all, true); got == nil || got.Metadata.Namespace != "tailscale" || got.Metadata.Name != "operator" {
		t.Errorf("picked %+v", got)
	}
	if got := pickTailscaleOperator(all[:2], true); got != nil {
		t.Errorf("picked a non-operator %+v", got)
	}
	// Labelled by the chart: taken as is.
	if got := pickTailscaleOperator(all[:1], false); got == nil {
		t.Error("labelled deployment not taken")
	}
}

func TestTailscaleTruncated(t *testing.T) {
	var raw tsRaw
	for i := range tsListMax + 1 {
		var c tsConnectorObject
		c.Metadata.Name = "router-" + strconv.Itoa(i)
		raw.connectors = append(raw.connectors, c)
	}

	if r := buildTailscale(raw); !r.Truncated || len(r.Connectors) != tsListMax || r.Counts.Warning != tsListMax {
		t.Errorf("truncated %v, %d connectors, counts %+v", r.Truncated, len(r.Connectors), r.Counts)
	}
}

func TestTailscaleDemo(t *testing.T) {
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeTailscale(demo, demoKubeContext, "")
	if err != nil {
		t.Fatal(err)
	}

	var r tsReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Installed || r.Operator == nil || len(r.Connectors) != 3 || r.Connectors[0].Name != "saas-connector" ||
		len(r.ProxyGroups) != 2 || r.DNSConfig == nil || r.DNSConfig.NameserverIP != "100.100.100.100" {
		t.Errorf("demo %+v", r)
	}
	if r.ProxyClasses[0].Name != "metrics" || r.ProxyClasses[1].UsedBy != 2 {
		t.Errorf("demo proxy classes %+v", r.ProxyClasses)
	}
	if want := (tsCounts{OK: 7, Warning: 1, Critical: 1}); r.Counts != want {
		t.Errorf("demo counts %+v", r.Counts)
	}
}
