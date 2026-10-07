package ichorgo

import (
	"context"
	"reflect"
	"testing"
)

func TestCalicoSelector(t *testing.T) {
	db := labelSet{"app": "db", "tier": "data", calicoNamespaceLabel: "shop", calicoServiceAccountLabel: "db"}
	web := labelSet{"app": "web-frontend", calicoNamespaceLabel: "team-a-web"}

	tests := []struct {
		src     string
		db, web bool
	}{
		{"", true, true},
		{"all()", true, true},
		{"global()", false, false},
		{"app == 'db'", true, false},
		{`app == "db"`, true, false},
		{"app != 'db'", false, true},
		{"tier != 'data'", false, true},
		{"has(tier)", true, false},
		{"!has(tier)", false, true},
		{"app in {'db', 'api'}", true, false},
		{"app not in {'db', 'api'}", false, true},
		{"tier not in {'data'}", false, true},
		{"app contains 'front'", false, true},
		{"app starts with 'web'", false, true},
		{"app ends with 'end'", false, true},
		{"app == 'db' && tier == 'data'", true, false},
		{"app == 'db' || app == 'web-frontend'", true, true},
		{"!(app == 'db') && has(app)", false, true},
		{"(app == 'db' || app == 'web-frontend') && tier == 'data'", true, false},
		{"projectcalico.org/namespace == 'shop' && projectcalico.org/serviceaccount == 'db'", true, false},
		{"app == 'db' &&", false, false}, // refused by Calico: selects nothing
		{"app = 'db'", false, false},
		{"has(", false, false},
		{"app in {'db'", false, false},
	}

	for _, tt := range tests {
		s := parseCalicoSelector(tt.src)
		if got := s.matches(db); got != tt.db {
			t.Errorf("%q on db: %v", tt.src, got)
		}

		if got := s.matches(web); got != tt.web {
			t.Errorf("%q on web: %v", tt.src, got)
		}
	}

	var none *calicoSelector
	if none.matches(db) || none.String() != "" || none.pins("app") != "" {
		t.Error("a nil selector selects nothing")
	}

	s := parseCalicoSelector("  tier == 'data' && projectcalico.org/namespace == 'shop' ")
	if s.String() != "tier == 'data' && projectcalico.org/namespace == 'shop'" || s.pins(calicoNamespaceLabel) != "shop" || s.pins("tier") != "data" {
		t.Errorf("pins: %q %q", s.String(), s.pins(calicoNamespaceLabel))
	}

	if parseCalicoSelector("app == 'db' || projectcalico.org/namespace == 'shop'").pins(calicoNamespaceLabel) != "" {
		t.Error("an alternative pins nothing")
	}

	if !parseCalicoSelector("global()").isGlobal() || parseCalicoSelector("all()").isGlobal() {
		t.Error("global()")
	}
}

func calicoFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "calico/"+name))
}

func readFixtureCalicoPolicies(t *testing.T, apiServer bool) netPolicyReport {
	t.Helper()

	groups := `{"groups":[{"name":"crd.projectcalico.org","preferredVersion":{"version":"v1"}}]}`
	api := "/apis/crd.projectcalico.org/v1"

	if apiServer {
		groups = `{"groups":[{"name":"projectcalico.org","preferredVersion":{"version":"v3"}}]}`
		api = "/apis/projectcalico.org/v3"
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": groups,
		"GET /apis/networking.k8s.io/v1/networkpolicies": `{"items":[]}`,
		"GET " + api + "/networkpolicies":                calicoFixture(t, "networkpolicies.json"),
		"GET " + api + "/globalnetworkpolicies":          calicoFixture(t, "globalnetworkpolicies.json"),
		"GET /api/v1/pods":                               calicoFixture(t, "pods.json"),
		"GET /api/v1/namespaces":                         calicoFixture(t, "namespaces.json"),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := readNetPolicyReport(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	return report
}

func TestCalicoPolicyReport(t *testing.T) {
	r := readFixtureCalicoPolicies(t, false)

	if !r.Calico || r.Cilium || len(r.Policies) != 7 || r.Error != "" {
		t.Fatalf("calico %v cilium %v, %d policies, error %q", r.Calico, r.Cilium, len(r.Policies), r.Error)
	}

	pods := map[string][]string{
		"shop/default.db-allow-api": {"shop/db-0"},
		"shop/default.api-egress":   {"shop/api-5f6d-k8s2p"},
		"shop/default.audit-only":   {"shop/db-0", "shop/api-5f6d-k8s2p", "shop/frontend-7d9c8b6f5-x2k4q"},
		"jobs/default.ping":         {"jobs/batch-28761234-abcde"},
		"/default.allow-dns":        {"shop/db-0", "shop/api-5f6d-k8s2p", "shop/frontend-7d9c8b6f5-x2k4q", "jobs/batch-28761234-abcde", "kube-system/coredns-1", "team-a-web/web-1"},
		"/default.team-a-isolation": {"team-a-web/web-1"},
		"/default.host-fw":          {},
	}

	for _, p := range r.Policies {
		want, ok := pods[p.Namespace+"/"+p.Name]
		if !ok {
			t.Errorf("unexpected policy %s/%s (mirrored and staged ones are skipped)", p.Namespace, p.Name)
		}

		if len(want) != p.PodCount || (len(want) > 0 && !sameItems(want, p.Pods)) {
			t.Errorf("%s/%s selects %v", p.Namespace, p.Name, p.Pods)
		}
	}

	db := findPolicy(t, r, "shop", "default.db-allow-api")
	if db.Kind != kindCalicoPolicy || !db.Ingress || db.Egress || db.Subject != "app == 'db'" || db.Description != "Only the API may talk to the database" {
		t.Errorf("db-allow-api: %+v", db)
	}

	want := netRule{Peers: []netPeer{{Kind: "pods", Selector: "app == 'api'"}}, Ports: []netPort{{Protocol: "TCP", Port: "5432"}}}
	if !reflect.DeepEqual(db.IngressRules, []netRule{want}) {
		t.Errorf("db-allow-api rules: %+v", db.IngressRules)
	}

	api := findPolicy(t, r, "shop", "default.api-egress")
	if api.Ingress || !api.Egress || len(api.EgressRules) != 4 {
		t.Fatalf("api-egress: egress rules only isolate egress: %+v", api)
	}

	wantRules := []netRule{
		{Peers: []netPeer{{Kind: "pods", Selector: "app == 'db'"}}, Ports: []netPort{{Protocol: "TCP", Port: "5432"}}},
		{Peers: []netPeer{{Kind: "pods", Selector: "k8s-app == 'kube-dns'", Namespace: "kube-system"}}, Ports: []netPort{{Protocol: "UDP", Port: "53"}}},
		{Deny: true, Peers: []netPeer{{Kind: "cidr", Value: "0.0.0.0/0", Except: []string{"10.0.0.0/8"}}}, Ports: []netPort{{Protocol: "TCP", Port: "25"}, {Protocol: "TCP", Port: "8000", EndPort: 8100}}},
		{Action: "pass", Peers: []netPeer{{Kind: "service", Value: "shop/frontend"}}, Ports: []netPort{}},
	}
	if !reflect.DeepEqual(api.EgressRules, wantRules) {
		t.Errorf("api-egress rules: %+v", api.EgressRules)
	}

	audit := findPolicy(t, r, "shop", "default.audit-only")
	wantAudit := netRule{Action: "log", Peers: []netPeer{{Kind: "cidr", Value: "192.168.0.0/16"}, {Kind: "other", Value: "only to tier == 'backend'"}}, Ports: []netPort{{Protocol: "TCP", Port: "8080"}}}
	if audit.Subject != "all()" || !reflect.DeepEqual(audit.IngressRules, []netRule{wantAudit}) {
		t.Errorf("audit-only: %q %+v", audit.Subject, audit.IngressRules)
	}

	ping := findPolicy(t, r, "jobs", "default.ping")
	wantPing := netRule{
		Peers: []netPeer{{Kind: "pods", Selector: "projectcalico.org/serviceaccount in {'monitoring'}", NamespaceSelector: "team == 'ops'"}},
		Ports: []netPort{{Protocol: "ICMPv4", Port: "8/0"}},
	}
	if !reflect.DeepEqual(ping.IngressRules, []netRule{wantPing}) {
		t.Errorf("ping: %+v", ping.IngressRules)
	}

	dns := findPolicy(t, r, "", "default.allow-dns")
	if dns.Kind != kindCalicoGlobalPolicy || dns.Subject != "namespaces: has(projectcalico.org/name)" || dns.SubjectNS != "" || dns.Ingress || !dns.Egress {
		t.Errorf("allow-dns: %+v", dns)
	}

	if dns.Order == nil || *dns.Order != 10 || dns.Tier != "" || db.Tier != "default" || db.Order == nil || *db.Order != 100 {
		t.Errorf("order and tier: %+v %+v", dns.Order, db.Order)
	}

	if peer := dns.EgressRules[0].Peers[0]; peer.Namespace != "kube-system" || peer.NamespaceSelector != "" || peer.Selector != "k8s-app == 'kube-dns'" {
		t.Errorf("allow-dns peer: %+v", peer)
	}

	team := findPolicy(t, r, "", "default.team-a-isolation")
	if team.SubjectNS != "team-a-web" || team.Subject != "all()" || !team.Ingress || !team.Egress {
		t.Errorf("team-a-isolation: %+v", team)
	}

	if in := team.IngressRules[0].Peers; !reflect.DeepEqual(in, []netPeer{{Kind: "pods", Namespace: "team-a-web"}}) {
		t.Errorf("team-a-isolation ingress: %+v", in)
	}

	if out := team.EgressRules[0].Peers; !reflect.DeepEqual(out, []netPeer{{Kind: "pods", Namespace: "*", Selector: "!(has(secret))"}}) {
		t.Errorf("team-a-isolation egress: %+v", out)
	}

	host := findPolicy(t, r, "", "default.host-fw")
	if !host.Ingress || host.Egress || host.Nodes {
		t.Errorf("host-fw: %+v", host)
	}

	if peers := host.IngressRules[0].Peers; !reflect.DeepEqual(peers, []netPeer{{Kind: "other", Value: "role == 'bastion' global()"}}) {
		t.Errorf("host-fw bastion: %+v", peers)
	}

	if l7 := host.IngressRules[1].L7; !reflect.DeepEqual(l7, []string{"HTTP GET /api/* /health"}) {
		t.Errorf("host-fw http: %v", l7)
	}

	rows := map[string]netPolicyNSRow{}
	for _, row := range r.Namespaces {
		rows[row.Namespace] = row
	}

	wantRows := map[string]netPolicyNSRow{
		"shop":        {Namespace: "shop", Pods: 3, IngressIsolated: 3, EgressIsolated: 3, Policies: 3},
		"jobs":        {Namespace: "jobs", Pods: 1, IngressIsolated: 1, EgressIsolated: 1, Policies: 1},
		"kube-system": {Namespace: "kube-system", Pods: 1, IngressIsolated: 0, EgressIsolated: 1},
		"team-a-web":  {Namespace: "team-a-web", Pods: 1, IngressIsolated: 1, EgressIsolated: 1},
	}
	if !reflect.DeepEqual(rows, wantRows) {
		t.Errorf("namespaces: %+v", rows)
	}
}

// Without the CRDs (the etcd datastore), the Calico API server serves the same policies.
func TestCalicoPolicyReportAPIServer(t *testing.T) {
	r := readFixtureCalicoPolicies(t, true)

	if !r.Calico || len(r.Policies) != 7 || r.Error != "" {
		t.Fatalf("calico %v, %d policies, error %q", r.Calico, len(r.Policies), r.Error)
	}
}

func TestCalicoPolicyTypesAndRules(t *testing.T) {
	var o calicoPolicyObject
	o.Metadata.Name = "default.p"
	o.Metadata.Namespace = "ns"

	// Neither rules nor types: Ingress, like a Kubernetes policy.
	p := mapCalicoPolicy(o, false)
	if !p.Ingress || p.Egress || p.Subject != "" || len(p.subjects) != 1 {
		t.Errorf("empty: %+v", p)
	}

	o.Spec.Ingress = []calicoRule{{Action: "Allow"}}
	o.Spec.Egress = []calicoRule{{Action: "Deny", NotProtocol: "UDP", Destination: calicoEntityRule{NotPorts: []any{float64(22)}}}}

	p = mapCalicoPolicy(o, false)
	if !p.Ingress || !p.Egress {
		t.Errorf("both: %+v", p)
	}

	// An empty rule matches anyone, unlike Cilium's.
	if !reflect.DeepEqual(p.IngressRules, []netRule{{Peers: []netPeer{}, Ports: []netPort{}}}) {
		t.Errorf("empty rule: %+v", p.IngressRules)
	}

	wantEgress := netRule{Deny: true, Peers: []netPeer{{Kind: "other", Value: "not ports 22"}, {Kind: "other", Value: "not protocol UDP"}}, Ports: []netPort{}}
	if !reflect.DeepEqual(p.EgressRules, []netRule{wantEgress}) {
		t.Errorf("negations: %+v", p.EgressRules)
	}

	// A protocol without ports is every port of it; a numeric protocol is kept as is.
	r := calicoRule{Protocol: "SCTP"}
	if ports := r.ports(); !reflect.DeepEqual(ports, []netPort{{Protocol: "SCTP"}}) {
		t.Errorf("sctp: %+v", ports)
	}

	r = calicoRule{Protocol: float64(47)}
	if ports := r.ports(); !reflect.DeepEqual(ports, []netPort{{Protocol: "47"}}) {
		t.Errorf("gre: %+v", ports)
	}

	r = calicoRule{Protocol: "ICMPv6", ICMP: &calicoICMP{Type: ptr(128)}}
	if ports := r.ports(); !reflect.DeepEqual(ports, []netPort{{Protocol: "ICMPv6", Port: "128"}}) {
		t.Errorf("icmpv6: %+v", ports)
	}

	// A global policy's selector pins a namespace through Calico's own label; a peer without
	// namespace selector in a global policy is any namespace.
	o.Spec.Selector = "projectcalico.org/namespace == 'shop' && app == 'db'"
	o.Spec.Ingress = []calicoRule{{Action: "Allow", Source: calicoEntityRule{Selector: "app == 'api'"}}}
	g := mapCalicoPolicy(o, true)
	if g.Kind != kindCalicoGlobalPolicy || g.Namespace != "" || g.SubjectNS != "shop" || g.IngressRules[0].Peers[0].Namespace != "*" {
		t.Errorf("global: %+v", g)
	}

	// A service account selector matches the account's name only.
	o.Spec.Selector = ""
	o.Spec.ServiceAccountSelector = "projectcalico.org/name == 'db'"
	sa := mapCalicoPolicy(o, false)
	labels := podLabelSet(map[string]string{"app": "db"}, "ns", "db", nil)
	if selected, _, _ := sa.isolation("ns", labels); !selected || sa.Subject != "service accounts: projectcalico.org/name == 'db'" {
		t.Errorf("service account: %v %q", selected, sa.Subject)
	}

	if selected, _, _ := sa.isolation("ns", podLabelSet(map[string]string{"app": "db"}, "ns", "other", nil)); selected {
		t.Error("another service account is not selected")
	}
}

// Calico ANDs the fields of one entity rule: the app words such a rule as one peer.
func TestCalicoConjunctions(t *testing.T) {
	sa := &struct {
		Names    []string `json:"names"`
		Selector string   `json:"selector"`
	}{Names: []string{"sa"}}

	tests := []struct {
		name string
		rule calicoEntityRule
		want []netPeer
	}{
		{"nets and selector", calicoEntityRule{Nets: []string{"10.0.0.0/8"}, Selector: "app == 'api'"},
			[]netPeer{{Kind: "other", Value: "app == 'api' && 10.0.0.0/8"}}},
		{"nets and namespaces", calicoEntityRule{Nets: []string{"10.0.0.0/8", "10.1.0.0/16"}, NotNets: []string{"10.0.1.0/24"}, NamespaceSelector: "team == 'ops'"},
			[]netPeer{{Kind: "other", Value: "all() in namespaces team == 'ops' && in {10.0.0.0/8, 10.1.0.0/16} except 10.0.1.0/24"}}},
		{"nets alone", calicoEntityRule{Nets: []string{"10.0.0.0/8", "10.1.0.0/16"}, NotNets: []string{"10.0.1.0/24"}},
			[]netPeer{{Kind: "cidr", Value: "10.0.0.0/8", Except: []string{"10.0.1.0/24"}}, {Kind: "cidr", Value: "10.1.0.0/16", Except: []string{"10.0.1.0/24"}}}},
		{"alternatives and a service account", calicoEntityRule{Selector: "app == 'a' || app == 'b'", ServiceAccounts: sa},
			[]netPeer{{Kind: "pods", Selector: "(app == 'a' || app == 'b') && projectcalico.org/serviceaccount in {'sa'}"}}},
		{"not selector", calicoEntityRule{Selector: "app == 'a'", NotSelector: "has(secret)"},
			[]netPeer{{Kind: "pods", Selector: "app == 'a' && !(has(secret))"}}},
		{"service account labels", calicoEntityRule{Selector: "app == 'a'", ServiceAccounts: &struct {
			Names    []string `json:"names"`
			Selector string   `json:"selector"`
		}{Selector: "team == 'ops'"}},
			[]netPeer{{Kind: "other", Value: "app == 'a' && service accounts team == 'ops'"}}},
		{"empty", calicoEntityRule{}, []netPeer{}},
	}

	for _, tt := range tests {
		if got := tt.rule.peers(false); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %+v", tt.name, got)
		}
	}

	// The side that is the policy's own endpoints narrows them, whatever it names.
	narrowing := calicoEntityRule{Services: &struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}{Name: "web"}}
	if got := narrowing.narrowing(true); got != "only to Service web" {
		t.Errorf("narrowing: %q", got)
	}

	r := calicoRule{Protocol: "ICMP", NotICMP: &calicoICMP{Type: ptr(8)}, Source: calicoEntityRule{Ports: []any{"1024:65535"}}}
	if got := r.toRule(true, false).Peers; !reflect.DeepEqual(got, []netPeer{{Kind: "other", Value: "source ports 1024:65535"}, {Kind: "other", Value: "not ICMP 8"}}) {
		t.Errorf("notes: %+v", got)
	}
}

// With the Kubernetes datastore, calicoctl keeps a policy's own annotations in one CRD annotation.
func TestCalicoDescription(t *testing.T) {
	var o calicoPolicyObject
	o.Metadata.Annotations = map[string]string{calicoMetadataAnnotation: `{"annotations":{"description":"  From calicoctl "}}`}

	if d := mapCalicoPolicy(o, false).Description; d != "From calicoctl" {
		t.Errorf("description %q", d)
	}

	o.Metadata.Annotations["description"] = "Plain"
	if d := mapCalicoPolicy(o, false).Description; d != "Plain" {
		t.Errorf("description %q", d)
	}
}

func ptr[T any](v T) *T { return &v }
