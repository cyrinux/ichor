package ichorgo

import (
	"net/http"
	"slices"
	"testing"
)

func tsExposedByName(t *testing.T, r tsReport, kind, ns, name string) tsExposed {
	t.Helper()

	for _, e := range r.Exposed {
		if e.Kind == kind && e.Namespace == ns && e.Name == name {
			return e
		}
	}

	t.Fatalf("no exposed %s %s/%s in %+v", kind, ns, name, r.Exposed)

	return tsExposed{}
}

func TestTailscaleExposedOrder(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))

	// The operator's Services and Ingresses only, problems first, then by namespace/name.
	var got []string
	for _, e := range r.Exposed {
		got = append(got, e.Kind+" "+e.Namespace+"/"+e.Name)
	}

	want := []string{
		"Service infra/api-mirror", "Service payments/legacy",
		"Ingress docs/drafts", "Service media/player", "Service shop/checkout",
		"Ingress docs/wiki", "Service ops/grafana", "Service payments/ledger-db", "Service shop/storefront",
	}
	if !slices.Equal(got, want) {
		t.Errorf("exposed order\n got %v\nwant %v", got, want)
	}
}

func TestTailscaleExposedRows(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))

	tests := []struct {
		name, kind, ns, obj string
		direction           string
		names, ports, tags  []string
		group               string
		health, reason      string
		message             string
	}{
		{
			name: "ingress on a ProxyGroup", kind: tsKindService, ns: "shop", obj: "storefront", direction: tsDirectionIngress,
			names: []string{"storefront.example-tailnet.ts.net", "100.64.0.51"}, ports: []string{"443", "80"},
			tags: []string{"tag:k8s", "tag:shop"}, group: "ingress-proxies", health: tsHealthOK, reason: "IngressSvcConfigured",
		},
		{
			name: "not reconciled yet", kind: tsKindService, ns: "shop", obj: "checkout", direction: tsDirectionIngress,
			names: []string{}, ports: []string{"443"}, tags: []string{}, group: "ingress-proxies",
			health: tsHealthWarning, reason: tsReasonNotReconciled,
		},
		{
			name: "names a missing ProxyGroup", kind: tsKindService, ns: "media", obj: "player", direction: tsDirectionIngress,
			names: []string{"player.example-tailnet.ts.net"}, ports: []string{"8096"}, tags: []string{}, group: "web-proxies",
			health: tsHealthWarning, reason: tsReasonGroupMissing,
		},
		{
			name: "its ProxyGroup runs no proxy", kind: tsKindService, ns: "infra", obj: "api-mirror", direction: tsDirectionIngress,
			names: []string{"api-mirror.example-tailnet.ts.net"}, ports: []string{"443"}, tags: []string{}, group: "api-proxies",
			health: tsHealthCritical, reason: tsReasonGroupDown, message: "0/2 ProxyGroup pods running",
		},
		{
			name: "exposed by annotation, a proxy of its own", kind: tsKindService, ns: "ops", obj: "grafana", direction: tsDirectionIngress,
			names: []string{"grafana"}, ports: []string{"3000"}, tags: []string{}, health: tsHealthOK, reason: "ProxyReady",
		},
		{
			name: "egress to a tailnet name", kind: tsKindService, ns: "payments", obj: "ledger-db", direction: tsDirectionEgress,
			names: []string{"ledger-db.example-tailnet.ts.net"}, ports: []string{"5432"}, tags: []string{}, group: "egress-proxies",
			health: tsHealthOK, reason: "EgressSvcReady",
		},
		{
			name: "egress to a tailnet IP, proxy failed", kind: tsKindService, ns: "payments", obj: "legacy", direction: tsDirectionEgress,
			names: []string{"100.64.0.77"}, ports: []string{"53/UDP"}, tags: []string{},
			health: tsHealthCritical, reason: "ProxyFailed", message: "error provisioning proxy",
		},
		{
			name: "Ingress with a tailnet name", kind: tsKindIngress, ns: "docs", obj: "wiki", direction: tsDirectionIngress,
			names: []string{"wiki.example-tailnet.ts.net"}, ports: []string{"443"}, tags: []string{"tag:docs"}, health: tsHealthOK,
		},
		{
			name: "Ingress without a name yet", kind: tsKindIngress, ns: "docs", obj: "drafts", direction: tsDirectionIngress,
			names: []string{}, ports: []string{"443"}, tags: []string{}, health: tsHealthWarning, reason: tsReasonNoTailnetName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := tsExposedByName(t, r, tc.kind, tc.ns, tc.obj)
			if e.Direction != tc.direction || e.ProxyGroup != tc.group || e.Health != tc.health || e.Reason != tc.reason ||
				e.Message != tc.message {
				t.Errorf("%+v", e)
			}
			if !slices.Equal(e.TailnetNames, tc.names) || !slices.Equal(e.Ports, tc.ports) || !slices.Equal(e.Tags, tc.tags) {
				t.Errorf("lists %+v", e)
			}
		})
	}
}

func TestTailscaleProxyGroupServes(t *testing.T) {
	r := tailscaleReadFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))

	for name, want := range map[string]int{"ingress-proxies": 2, "egress-proxies": 1, "api-proxies": 1, "old-proxies": 0} {
		if g := tsProxyGroupByName(t, r, name); g.Serves != want {
			t.Errorf("%s serves %d, want %d", name, g.Serves, want)
		}
	}

	if g := tsProxyGroupByName(t, r, "ingress-proxies"); g.HostnamePrefix != "ingress" {
		t.Errorf("hostname prefix %q", g.HostnamePrefix)
	}
}

func TestTailscaleExposedPartialRead(t *testing.T) {
	f := newFakeKubeAPI(t, tailscaleAnswers(t))
	f.answerWith("GET /apis/tailscale.com/v1alpha1/proxygroups", http.StatusForbidden, forbiddenStatus)

	// Without the ProxyGroups, neither a missing nor a down one can be told.
	r := tailscaleReadFrom(t, f)
	if e := tsExposedByName(t, r, tsKindService, "media", "player"); e.Health != tsHealthOK {
		t.Errorf("player without groups %+v", e)
	}
	if e := tsExposedByName(t, r, tsKindService, "infra", "api-mirror"); e.Health != tsHealthOK {
		t.Errorf("api-mirror without groups %+v", e)
	}

	// The Services are forbidden: the error says so, the Ingresses and the rest still show.
	f = newFakeKubeAPI(t, tailscaleAnswers(t))
	f.answerWith("GET /api/v1/services", http.StatusForbidden, forbiddenStatus)

	r = tailscaleReadFrom(t, f)
	if r.Error == "" || len(r.Exposed) != 2 || len(r.Connectors) != 6 {
		t.Errorf("services forbidden: error %q, %d exposed", r.Error, len(r.Exposed))
	}
}

func TestTailscaleCheckupSkipsExposed(t *testing.T) {
	f := newFakeKubeAPI(t, tailscaleAnswers(t))

	k, err := openKubeClient(t.Context(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// The checkup does not list every Service and Ingress for rows it has no finding for.
	r := readTailscaleWithin(t.Context(), k, map[string]string{groupTailscale: "v1alpha1"}, false)
	if len(r.Exposed) != 0 {
		t.Errorf("exposed %+v", r.Exposed)
	}
	for _, req := range f.recorded() {
		if req.path == "/api/v1/services" || req.path == "/apis/networking.k8s.io/v1/ingresses" {
			t.Errorf("checkup listed %s", req.path)
		}
	}
}
