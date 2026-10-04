package ichorgo

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func ciliumFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "cilium/"+name))
}

func readFixtureNetPolicies(t *testing.T) netPolicyReport {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"cilium.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /apis/networking.k8s.io/v1/networkpolicies":          ciliumFixture(t, "networkpolicies.json"),
		"GET /apis/cilium.io/v2/ciliumnetworkpolicies":            ciliumFixture(t, "ciliumnetworkpolicies.json"),
		"GET /apis/cilium.io/v2/ciliumclusterwidenetworkpolicies": ciliumFixture(t, "ciliumclusterwidenetworkpolicies.json"),
		"GET /api/v1/pods":       ciliumFixture(t, "pods.json"),
		"GET /api/v1/namespaces": ciliumFixture(t, "namespaces.json"),
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

func findPolicy(t *testing.T, r netPolicyReport, namespace, name string) netPolicy {
	t.Helper()

	for _, p := range r.Policies {
		if p.Namespace == namespace && p.Name == name {
			return p
		}
	}

	t.Fatalf("no policy %s/%s", namespace, name)

	return netPolicy{}
}

func TestNetPolicyReportSelectionAndIsolation(t *testing.T) {
	r := readFixtureNetPolicies(t)

	if !r.Cilium || len(r.Policies) != 9 || r.Error != "" {
		t.Fatalf("cilium %v, %d policies, error %q", r.Cilium, len(r.Policies), r.Error)
	}

	pods := map[string][]string{
		"shop/db-allow-api":        {"shop/db-0"},
		"jobs/default-deny-egress": {"jobs/batch-28761234-abcde"},
		"shop/allow-monitoring":    {"shop/db-0", "shop/api-5f6d-k8s2p"},
		"shop/api-l7":              {"shop/api-5f6d-k8s2p"},
		"shop/audit-only":          {"shop/db-0", "shop/api-5f6d-k8s2p", "shop/frontend-7d9c8b6f5-x2k4q"},
		"/allow-dns":               {"shop/db-0", "shop/api-5f6d-k8s2p", "shop/frontend-7d9c8b6f5-x2k4q", "jobs/batch-28761234-abcde", "team-a-web/web-1"},
		"/host-fw":                 {},
		"/team-a-isolation":        {"team-a-web/web-1"},
		"jobs/deny-external-dns":   {"jobs/batch-28761234-abcde"},
	}

	for _, p := range r.Policies {
		want := pods[p.Namespace+"/"+p.Name]
		if len(want) != p.PodCount || (len(want) > 0 && !sameItems(want, p.Pods)) {
			t.Errorf("%s/%s selects %v", p.Namespace, p.Name, p.Pods)
		}
	}

	want := []netPolicyNSRow{
		{Namespace: "jobs", Pods: 1, EgressIsolated: 1, Policies: 2},
		{Namespace: "kube-system", Pods: 1},
		{Namespace: "shop", Pods: 3, IngressIsolated: 2, Policies: 4},
		{Namespace: "team-a-web", Pods: 1, IngressIsolated: 1},
	}
	if !reflect.DeepEqual(r.Namespaces, want) {
		t.Fatalf("namespaces %+v", r.Namespaces)
	}
}

func sameItems(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}

	for _, s := range b {
		seen[s]--
	}

	for _, n := range seen {
		if n != 0 {
			return false
		}
	}

	return true
}

func TestNetworkPolicyRules(t *testing.T) {
	p := findPolicy(t, readFixtureNetPolicies(t), "shop", "allow-monitoring")

	if p.Subject != "tier in (backend,data)" || !p.Ingress || p.Egress || len(p.IngressRules) != 1 {
		t.Fatalf("policy %+v", p)
	}

	rule := p.IngressRules[0]
	wantPeers := []netPeer{
		{Kind: "pods", Selector: "app=prometheus", NamespaceSelector: "team=observability"},
		{Kind: "namespaces"},
		{Kind: "cidr", Value: "10.0.0.0/8", Except: []string{"10.1.0.0/16"}},
	}
	wantPorts := []netPort{{Protocol: "TCP", Port: "metrics"}, {Protocol: "UDP", Port: "8000", EndPort: 8100}}

	if !reflect.DeepEqual(rule.Peers, wantPeers) || !reflect.DeepEqual(rule.Ports, wantPorts) {
		t.Fatalf("rule %+v", rule)
	}

	deny := findPolicy(t, readFixtureNetPolicies(t), "jobs", "default-deny-egress")
	if deny.Subject != "" || deny.Ingress || !deny.Egress || len(deny.EgressRules) != 0 {
		t.Fatalf("default deny %+v", deny)
	}
}

func TestCiliumPolicyRules(t *testing.T) {
	r := readFixtureNetPolicies(t)

	api := findPolicy(t, r, "shop", "api-l7")
	if api.Kind != kindCiliumPolicy || api.Subject != "app=api" || api.Description != "Only the frontend may call the API" ||
		!reflect.DeepEqual(api.IngressRules[0].L7, []string{"HTTP GET /cart.*"}) ||
		!reflect.DeepEqual(api.IngressRules[0].Ports, []netPort{{Protocol: "TCP", Port: "8080"}}) {
		t.Fatalf("api-l7 %+v", api)
	}

	dns := findPolicy(t, r, "jobs", "deny-external-dns")
	if !dns.Egress || dns.Ingress || len(dns.EgressRules) != 3 {
		t.Fatalf("deny-external-dns %+v", dns)
	}

	toDNS, other, denied := dns.EgressRules[0], dns.EgressRules[1], dns.EgressRules[2]

	if !reflect.DeepEqual(toDNS.Peers, []netPeer{{Kind: "pods", Selector: "k8s-app=kube-dns", Namespace: "kube-system"}}) ||
		!reflect.DeepEqual(toDNS.Ports, []netPort{{Protocol: "ANY", Port: "53"}}) || !reflect.DeepEqual(toDNS.L7, []string{"DNS *"}) {
		t.Fatalf("to DNS %+v", toDNS)
	}

	wantOther := []netPeer{
		{Kind: "cidr", Value: "192.0.2.0/24", Except: []string{"192.0.2.1/32"}},
		{Kind: "fqdn", Value: "*.example.org"},
		{Kind: "service", Value: "shop/store"},
	}
	if !reflect.DeepEqual(other.Peers, wantOther) || !reflect.DeepEqual(other.Ports, []netPort{{Protocol: "ICMPv4", Port: "8"}}) {
		t.Fatalf("other %+v", other)
	}

	if !denied.Deny || !reflect.DeepEqual(denied.Peers, []netPeer{{Kind: "entity", Value: "world"}}) {
		t.Fatalf("denied %+v", denied)
	}

	if audit := findPolicy(t, r, "shop", "audit-only"); audit.Ingress || audit.Subject != "" {
		t.Fatalf("audit-only %+v", audit)
	}

	if host := findPolicy(t, r, "", "host-fw"); !host.Nodes || !host.Ingress || host.Subject != "node-role.kubernetes.io/control-plane=" {
		t.Fatalf("host-fw %+v", host)
	}

	if ccnp := findPolicy(t, r, "", "allow-dns"); ccnp.Kind != kindCiliumClusterPolicy || ccnp.Egress || ccnp.Subject != "io.kubernetes.pod.namespace notin (kube-system)" {
		t.Fatalf("allow-dns %+v", ccnp)
	}
}

func TestIsolatingPoliciesForAFlow(t *testing.T) {
	r := readFixtureNetPolicies(t)

	f, _ := parseHubbleLine(readHubbleFixture(t)[0])
	dst := f.flow.Destination

	got := isolatingPolicies(r.Policies, dst.Namespace, flowLabels(dst.Labels), f.flow.Direction)
	if want := []policyRef{{Kind: kindNetworkPolicy, Namespace: "shop", Name: "db-allow-api"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("isolating %+v", got)
	}

	if got := isolatingPolicies(r.Policies, "jobs", labelSet{"app": "batch", ciliumNamespaceLabel: "jobs"}, "EGRESS"); len(got) != 2 {
		t.Fatalf("batch egress %+v", got)
	}
}

func TestLabelSelectorMatches(t *testing.T) {
	labels := labelSet{"app": "db", "tier": "data", ciliumNamespaceLabel: "shop"}

	sel := func(js string) *labelSelector {
		var s labelSelector
		if err := json.Unmarshal([]byte(js), &s); err != nil {
			t.Fatal(err)
		}

		return &s
	}

	cases := map[string]bool{
		`{}`:                                true,
		`{"matchLabels":{"k8s:app":"db"}}`:  true,
		`{"matchLabels":{"any:app":"api"}}`: false,
		`{"matchLabels":{"k8s:io.kubernetes.pod.namespace":"shop"}}`:                                             true,
		`{"matchExpressions":[{"key":"tier","operator":"NotIn","values":["web"]}]}`:                              true,
		`{"matchExpressions":[{"key":"gone","operator":"DoesNotExist"}]}`:                                        true,
		`{"matchExpressions":[{"key":"tier","operator":"Exists"},{"key":"app","operator":"In","values":["x"]}]}`: false,
		`{"matchExpressions":[{"key":"tier","operator":"Bogus"}]}`:                                               false,
	}

	for js, want := range cases {
		if got := sel(js).matches(labels); got != want {
			t.Errorf("%s: got %v", js, got)
		}
	}

	if (*labelSelector)(nil).matches(labels) {
		t.Error("a nil selector selects nothing")
	}
}

func TestFlowLabels(t *testing.T) {
	got := flowLabels([]string{"k8s:app=db", "reserved:host", "cidr:10.0.0.0/8", "k8s:io.kubernetes.pod.namespace=shop"})
	if !reflect.DeepEqual(got, labelSet{"app": "db", ciliumNamespaceLabel: "shop"}) {
		t.Fatalf("got %v", got)
	}
}

func TestCiliumPolicySemantics(t *testing.T) {
	var list kubeList[cnpObject]

	err := json.Unmarshal([]byte(`{"items":[
	 {"metadata":{"name":"deny-all","namespace":"shop"},"spec":{"endpointSelector":{},"ingress":[{}]}},
	 {"metadata":{"name":"empty-list","namespace":"shop"},"spec":{"endpointSelector":{},"ingress":[]}},
	 {"metadata":{"name":"split","namespace":"shop"},"specs":[
	   {"endpointSelector":{"matchLabels":{"app":"a"}},"ingress":[{"fromEntities":["cluster"]}]},
	   {"endpointSelector":{"matchLabels":{"app":"b"}},"egress":[{"toPorts":[{"ports":[{"port":"443"}]}]},
	                                                           {"toGroups":[{"aws":{"labels":{"x":"y"}}}]}]}]}
	]}`), &list)
	if err != nil {
		t.Fatal(err)
	}

	denyAll, emptyList, split := mapCiliumPolicy(list.Items[0], false), mapCiliumPolicy(list.Items[1], false), mapCiliumPolicy(list.Items[2], false)

	if !denyAll.Ingress || !reflect.DeepEqual(denyAll.IngressRules[0].Peers, []netPeer{{Kind: "none"}}) {
		t.Fatalf("an empty Cilium rule is a deny-all: %+v", denyAll)
	}

	if emptyList.Ingress {
		t.Fatal("an empty rule list does not isolate")
	}

	a := labelSet{"app": "a", ciliumNamespaceLabel: "shop"}
	b := labelSet{"app": "b", ciliumNamespaceLabel: "shop"}

	if !split.isolates("shop", a, "INGRESS") || split.isolates("shop", a, "EGRESS") ||
		split.isolates("shop", b, "INGRESS") || !split.isolates("shop", b, "EGRESS") {
		t.Fatal("each spec isolates its own pods in its own directions")
	}

	// A ports-only rule allows any peer on those ports; an undescribed peer is not "any".
	if len(split.EgressRules[0].Peers) != 0 || !reflect.DeepEqual(split.EgressRules[1].Peers, []netPeer{{Kind: "other", Value: "toGroups"}}) {
		t.Fatalf("egress rules %+v", split.EgressRules)
	}
}
