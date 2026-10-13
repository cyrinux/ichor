package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

var lbNow = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func decodeAll[T any](t *testing.T, items string) []T {
	t.Helper()

	var out []T
	if err := json.Unmarshal([]byte(items), &out); err != nil {
		t.Fatal(err)
	}

	return out
}

const (
	lbOldService = `"creationTimestamp":"2026-10-01T00:00:00Z"`
	lbPending    = `[{"metadata":{"name":"web","namespace":"shop",` + lbOldService + `},"spec":{"type":"LoadBalancer"}}]`
	lbHolder     = `{"metadata":{"name":"edge","namespace":"shop",` + lbOldService + `},"spec":{"type":"LoadBalancer"},
		"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.9"}]}}}`
	lbTinyPool = `{"metadata":{"name":"tiny"},"spec":{"addresses":["192.0.2.9/32"]}}`
	lbFreePool = `{"metadata":{"name":"free","labels":{"tier":"edge"}},"spec":{"addresses":["192.0.2.16/30"]}}`
	lbAllL2    = `{"metadata":{"name":"all"},"spec":{}}`
)

func TestLBCauses(t *testing.T) {
	metallb := func(pools, ads, states string) *metallbState {
		m := &metallbState{
			pools:          decodeAll[metallbPool](t, pools),
			advertisements: decodeAll[metallbAdvertisement](t, ads),
			configStates:   decodeAll[metallbConfigState](t, states),
		}
		for i := range m.advertisements {
			m.advertisements[i].Kind = lbAdvertL2
		}

		return m
	}
	running := []lbPod{{Name: "controller", App: lbControllerMetalLB}}

	for _, tc := range []struct {
		name     string
		in       lbInputs
		services string
		want     string
	}{
		{"nothing installed", lbInputs{}, lbPending, lbCauseNoController},
		{"refused configuration", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[`+lbAllL2+`]`,
			`[{"metadata":{"name":"controller","labels":{"metallb.io/component-type":"controller"}},"status":{"result":"Invalid","errorSummary":"overlapping pools"}}]`)},
			lbPending, lbCauseConfigError},
		{"controller not running", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[]`, `[]`), pods: []lbPod{}}, lbPending, lbCauseControllerDown},
		{"no pool", lbInputs{metallb: metallb(`[]`, `[]`, `[]`), pods: running}, lbPending, lbCauseNoPool},
		{"no pool assigns on its own", lbInputs{metallb: metallb(`[{"metadata":{"name":"manual"},"spec":{"addresses":["192.0.2.0/30"],"autoAssign":false}}]`, `[`+lbAllL2+`]`, `[]`), pods: running},
			lbPending, lbCausePoolNotMatched},
		{"pool for another namespace", lbInputs{metallb: metallb(`[{"metadata":{"name":"lab"},"spec":{"addresses":["192.0.2.0/30"],"serviceAllocation":{"namespaces":["lab"]}}}]`, `[`+lbAllL2+`]`, `[]`), pods: running},
			lbPending, lbCausePoolNotMatched},
		{"pinned to a missing pool", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[`+lbAllL2+`]`, `[]`), pods: running},
			`[{"metadata":{"name":"web","namespace":"shop",` + lbOldService + `,"annotations":{"metallb.io/address-pool":"gone"}},"spec":{"type":"LoadBalancer"}}]`,
			lbCausePoolNotMatched},
		{"pool exhausted", lbInputs{metallb: metallb(`[`+lbTinyPool+`]`, `[`+lbAllL2+`]`, `[]`), pods: running},
			`[` + lbHolder + `,` + lbPending[1:], lbCausePoolExhausted},
		{"pool exhausted by its status", lbInputs{metallb: metallb(`[{"metadata":{"name":"full"},"spec":{"addresses":["192.0.2.0/30"]},"status":{"assignedIPv4":4,"availableIPv4":0}}]`, `[`+lbAllL2+`]`, `[]`), pods: running},
			lbPending, lbCausePoolExhausted},
		{"pool not advertised", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[{"metadata":{"name":"other"},"spec":{"ipAddressPools":["elsewhere"]}}]`, `[]`), pods: running},
			lbPending, lbCausePoolNotAdvertised},
		{"advertised by label: unknown", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[{"metadata":{"name":"edge"},"spec":{"ipAddressPoolSelectors":[{"matchLabels":{"tier":"edge"}}]}}]`, `[]`), pods: running},
			lbPending, lbCauseUnknown},
		{"class of another controller", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[`+lbAllL2+`]`, `[]`)},
			`[{"metadata":{"name":"web","namespace":"shop",` + lbOldService + `},"spec":{"type":"LoadBalancer","loadBalancerClass":"example.com/lb"}}]`,
			lbCauseUnknown},
		{"kube-vip class without kube-vip", lbInputs{metallb: metallb(`[`+lbFreePool+`]`, `[`+lbAllL2+`]`, `[]`)},
			`[{"metadata":{"name":"web","namespace":"shop",` + lbOldService + `},"spec":{"type":"LoadBalancer","loadBalancerClass":"kube-vip.io/kube-vip-class"}}]`,
			lbCauseNoController},
		{"kube-vip without range", lbInputs{kubeVip: &kubeVipState{configMap: true}, pods: []lbPod{{Name: "kube-vip-n1", App: lbControllerKubeVip}}},
			lbPending, lbCauseNoPool},
		{"control-plane kube-vip leaves Services to Cilium", lbInputs{groups: map[string]string{groupCilium: "v2"}, kubeVip: &kubeVipState{},
			cilium: decodeAll[ciliumPoolObject](t, `[{"metadata":{"name":"blue"},"spec":{"blocks":[{"cidr":"192.0.2.0/30"}],"serviceSelector":{"matchLabels":{"color":"blue"}}}}]`)},
			lbPending, lbCausePoolNotMatched},
		{"kube-vip range for another namespace", lbInputs{kubeVip: &kubeVipState{configMap: true, ranges: []lbKubeVipRange{{Scope: "lab", CIDRs: []string{"192.0.2.0/29"}}}}},
			lbPending, lbCausePoolNotMatched},
		{"cilium pool for other services", lbInputs{groups: map[string]string{groupCilium: "v2"},
			cilium: decodeAll[ciliumPoolObject](t, `[{"metadata":{"name":"blue"},"spec":{"blocks":[{"cidr":"192.0.2.0/30"}],"serviceSelector":{"matchLabels":{"color":"blue"}}}}]`)},
			lbPending, lbCausePoolNotMatched},
		{"cilium pool exhausted", lbInputs{groups: map[string]string{groupCilium: "v2"},
			cilium: decodeAll[ciliumPoolObject](t, `[{"metadata":{"name":"pool"},"spec":{"blocks":[{"start":"192.0.2.1","stop":"192.0.2.2"}]},
				"status":{"conditions":[{"type":"cilium.io/IPsAvailable","message":"0"},{"type":"cilium.io/IPsTotal","message":"2"}]}}]`)},
			lbPending, lbCausePoolExhausted},
		{"cilium without pool", lbInputs{groups: map[string]string{groupCilium: "v2"}}, lbPending, lbCauseNoPool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.services = decodeAll[lbServiceObject](t, tc.services)
			rows := buildLoadBalancing(tc.in, lbNow).Services

			if rows[0].Name != "web" || !rows[0].Pending || rows[0].Cause != tc.want || rows[0].Level != serviceCritical {
				t.Errorf("got %+v, want cause %s", rows[0], tc.want)
			}
		})
	}
}

func TestAddrRanges(t *testing.T) {
	for _, tc := range []struct {
		lines []string
		size  int
		in    string
		out   string
	}{
		{[]string{"192.0.2.0/28"}, 16, "192.0.2.15", "192.0.2.16"},
		{[]string{"192.0.2.10-192.0.2.20"}, 11, "192.0.2.20", "192.0.2.21"},
		{[]string{"192.0.2.7", "2001:db8::/126"}, 5, "2001:db8::3", "2001:db8::4"},
		{[]string{"2001:db8::/64"}, maxPoolSize, "2001:db8::ffff", "2001:db9::"},
		{[]string{"2001:db8::/64", "2001:db9::/64", "192.0.2.1"}, maxPoolSize, "2001:db9::1", "2001:dba::"},
		{[]string{"not-an-address", "192.0.2.20-192.0.2.10"}, 0, "", "192.0.2.15"},
	} {
		ranges := poolRanges(tc.lines)
		if got := rangesSize(ranges); got != tc.size {
			t.Errorf("%v: size %d, want %d", tc.lines, got, tc.size)
		}

		if tc.in != "" && !rangesContain(ranges, tc.in) || rangesContain(ranges, tc.out) {
			t.Errorf("%v: contains %s %v, %s %v", tc.lines, tc.in, rangesContain(ranges, tc.in), tc.out, rangesContain(ranges, tc.out))
		}
	}
}

func TestBGPSessions(t *testing.T) {
	native := "# HELP metallb_bgp_session_up BGP session state\n" +
		`metallb_bgp_session_up{peer="192.0.2.1:179"} 1` + "\n" +
		`metallb_bgp_session_up{peer="2001:db8::1:179"} 0` + "\n" +
		`metallb_bgp_updates_total{peer="192.0.2.1:179"} 12` + "\n"
	frr := `frrk8s_bgp_session_up{peer="192.0.2.1",vrf="red"} 0` + "\n" + `frrk8s_bgp_session_up{peer="192.0.2.10"} 1` + "\n"

	sessions := bgpSessions(append(parseBGPSessions(native, "n1"), parseBGPSessions(frr, "n2")...))

	if got := sessions.of("192.0.2.1"); !reflect.DeepEqual(got, []lbBGPSession{{Node: "n1", Up: true}, {Node: "n2", Up: false}}) {
		t.Errorf("192.0.2.1: %+v", got)
	}

	if got := sessions.of("2001:db8::1"); !reflect.DeepEqual(got, []lbBGPSession{{Node: "n1", Up: false}}) {
		t.Errorf("2001:db8::1: %+v", got)
	}

	if got := sessions.of("192.0.2.10"); len(got) != 1 || !got[0].Up {
		t.Errorf("192.0.2.10 must not take 192.0.2.1's sessions: %+v", got)
	}

	// FRR-K8s labels the bare address: 192.0.2.10's session is not 192.0.2.1's on port 0.
	frrOnly := bgpSessions(parseBGPSessions(`frrk8s_bgp_session_up{peer="192.0.2.10"} 0`+"\n"+`metallb_bgp_session_up{peer="192.0.2.1:179"} `+"\n", "n3"))
	if got := frrOnly.of("192.0.2.1"); len(got) != 0 {
		t.Errorf("192.0.2.1 took another peer's session, or a line without value: %+v", got)
	}
}

// In FRR-K8s mode the sessions come from the frr-k8s pods' frr-metrics, over HTTPS; the
// speaker answering nothing does not hide them.
func TestReadBGPSessionsFromFRRK8s(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/metallb-system/pods/https:metallb-frr-k8s-x1:9141/proxy/metrics": `frrk8s_bgp_session_up{peer="192.0.2.1",vrf="default"} 0` + "\n",
	})
	pods := []lbPod{
		{Namespace: "metallb-system", Name: "metallb-speaker-a1", Node: "n1", App: lbControllerMetalLB},
		{Namespace: "metallb-system", Name: "metallb-frr-k8s-x1", Node: "n1", App: lbControllerMetalLB},
		{Namespace: "metallb-system", Name: "metallb-frr-k8s-webhook-server-0", Node: "n2", App: lbControllerMetalLB},
		{Namespace: "metallb-system", Name: "metallb-controller-b2", Node: "n2", App: lbControllerMetalLB},
	}

	sessions := readBGPSessions(context.Background(), openFakeKube(t, f), pods)
	if got := sessions.of("192.0.2.1"); !reflect.DeepEqual(got, []lbBGPSession{{Node: "n1", Up: false}}) {
		t.Errorf("sessions %+v", got)
	}

	for _, r := range f.requests {
		if strings.Contains(r.path, "controller") || strings.Contains(r.path, "webhook") {
			t.Errorf("asked a pod without BGP metrics: %s", r.path)
		}
	}
}

func TestKubeVipRanges(t *testing.T) {
	got := kubeVipRanges(map[string]string{
		"range-lab": "192.0.2.10-192.0.2.20", "cidr-global": "198.51.100.0/29, 198.51.100.64/29", "unrelated": "x", "cidr-": "y",
	})
	want := []lbKubeVipRange{
		{Scope: "global", CIDRs: []string{"198.51.100.0/29", "198.51.100.64/29"}},
		{Scope: "lab", CIDRs: []string{"192.0.2.10-192.0.2.20"}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func loadBalancingAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"metallb.io","preferredVersion":{"version":"v1beta1"}}]}`,
		"GET /api/v1/services": `{"items":[` + lbHolder + `,
		  {"metadata":{"name":"pinned","namespace":"shop",` + lbOldService + `,"annotations":{"metallb.io/address-pool":"gone"}},"spec":{"type":"LoadBalancer"}},
		  {"metadata":{"name":"web","namespace":"shop"},"spec":{"type":"ClusterIP"}}]}`,
		"GET /apis/metallb.io/v1beta1/ipaddresspools": `{"items":[
		  {"metadata":{"name":"main"},"spec":{"addresses":["192.0.2.8/30"]},"status":{"assignedIPv4":1,"availableIPv4":3}},
		  {"metadata":{"name":"spare"},"spec":{"addresses":["198.51.100.10-198.51.100.12"],"autoAssign":false}}]}`,
		"GET /apis/metallb.io/v1beta1/l2advertisements":    `{"items":[{"metadata":{"name":"all"},"spec":{"interfaces":["eth0"]}}]}`,
		"GET /apis/metallb.io/v1beta2/bgppeers":            `{"items":[{"metadata":{"name":"router"},"spec":{"peerAddress":"192.0.2.254","peerASN":64512,"myASN":64513}}]}`,
		"GET /apis/metallb.io/v1beta1/servicel2statuses":   `{"items":[{"status":{"node":"n1","serviceName":"edge","serviceNamespace":"shop","interfaces":[{"name":"eth0"}]}}]}`,
		"GET /apis/metallb.io/v1beta1/configurationstates": `{"items":[{"metadata":{"name":"controller"},"status":{"result":"Valid"}}]}`,
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"metallb-speaker-a1","namespace":"metallb-system"},"spec":{"nodeName":"n1","containers":[{"name":"speaker","image":"quay.io/metallb/speaker:v0.15.2"}]},"status":{"phase":"Running"}},
		  {"metadata":{"name":"metallb-controller-b2","namespace":"metallb-system"},"spec":{"nodeName":"n2","containers":[{"name":"controller","image":"quay.io/metallb/controller:v0.15.2"}]},"status":{"phase":"Running"}},
		  {"metadata":{"name":"web-1","namespace":"shop"},"spec":{"nodeName":"n2","containers":[{"name":"web","image":"web:1"}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/kube-system/configmaps/kubevip": `{"data":{"cidr-global":"198.51.100.64/29"}}`,
		"GET /apis/coordination.k8s.io/v1/namespaces/kube-system/leases": `{"items":[
		  {"metadata":{"name":"plndr-svcs-lock"},"spec":{"holderIdentity":"n2","renewTime":"2026-10-13T11:59:58.000000Z"}},
		  {"metadata":{"name":"kube-scheduler"},"spec":{"holderIdentity":"cp-1"}}]}`,
	})
	f.answerWith("GET /api/v1/namespaces/metallb-system/pods/metallb-speaker-a1:7472/proxy/metrics", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden"}`)

	return f
}

func TestReadLoadBalancing(t *testing.T) {
	lb, err := readLoadBalancing(context.Background(), openFakeKube(t, loadBalancingAPI(t)), lbNow)
	if err != nil {
		t.Fatal(err)
	}

	if lb.Controller != lbControllerMetalLB || !reflect.DeepEqual(lb.Controllers, []string{lbControllerMetalLB, lbControllerKubeVip}) || lb.PartialAccess {
		t.Errorf("controllers %q %v, partial %v", lb.Controller, lb.Controllers, lb.PartialAccess)
	}

	if len(lb.Pools) != 2 {
		t.Fatalf("pools %+v", lb.Pools)
	}

	main, spare := lb.Pools[0], lb.Pools[1]
	if main.Name != "main" || !main.CountsKnown || main.Total != 4 || main.Available != 3 || !reflect.DeepEqual(main.Services, []string{"shop/edge"}) ||
		!reflect.DeepEqual(main.AdvertisedBy, []string{"l2:all"}) || main.Level != serviceOK {
		t.Errorf("main %+v", main)
	}

	if spare.CountsKnown || spare.Total != 3 || spare.Available != 3 || spare.AutoAssign {
		t.Errorf("spare %+v", spare)
	}

	if len(lb.Services) != 2 {
		t.Fatalf("services %+v", lb.Services)
	}

	if pinned := lb.Services[0]; pinned.Name != "pinned" || pinned.Cause != lbCausePoolNotMatched || pinned.Level != serviceCritical {
		t.Errorf("pinned %+v", pinned)
	}

	if edge := lb.Services[1]; edge.Pool != "main" || edge.AnnouncedBy != "n1" || !reflect.DeepEqual(edge.Interfaces, []string{"eth0"}) || edge.Cause != "" {
		t.Errorf("edge %+v", edge)
	}

	// The speaker's metrics are forbidden: the session state is unknown, never down.
	if p := lb.BGPPeers; len(p) != 1 || p[0].SessionsKnown || len(p[0].Sessions) != 0 || p[0].Level != serviceOK {
		t.Errorf("peers %+v", p)
	}

	if v := lb.KubeVip; v == nil || len(v.Ranges) != 1 || len(v.Leases) != 1 || v.Leases[0].Holder != "n2" || v.Leases[0].Renewed == 0 {
		t.Errorf("kube-vip %+v", v)
	}
}

func TestReadLoadBalancingWithoutControllers(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":            `{"groups":[]}`,
		"GET /api/v1/services": `{"items":[` + lbPending[1:] + `}`,
		"GET /api/v1/pods":     `{"items":[]}`,
	})

	lb, err := readLoadBalancing(context.Background(), openFakeKube(t, f), lbNow)
	if err != nil {
		t.Fatal(err)
	}

	if lb.Controller != "" || len(lb.Controllers) != 0 || lb.KubeVip != nil || len(lb.Pools) != 0 || lb.Services[0].Cause != lbCauseNoController {
		t.Errorf("got %+v", lb)
	}
}

func TestMetalLBFindings(t *testing.T) {
	in := lbInputs{
		metallb: &metallbState{
			pools:          decodeAll[metallbPool](t, `[`+lbTinyPool+`,`+lbFreePool+`]`),
			advertisements: []metallbAdvertisement{{Kind: lbAdvertL2, Metadata: checkMeta{Name: "tiny-only"}}},
			peers:          decodeAll[metallbPeer](t, `[{"metadata":{"name":"router"},"spec":{"peerAddress":"192.0.2.1"}}]`),
			configStates: decodeAll[metallbConfigState](t,
				`[{"metadata":{"name":"speaker-n2","labels":{"metallb.io/component-type":"speaker","metallb.io/node-name":"n2"}},"status":{"result":"Invalid","errorSummary":"bad peer"}}]`),
		},
		services: decodeAll[lbServiceObject](t, `[`+lbHolder+`]`),
		sessions: bgpSessions{{peer: "192.0.2.1:179", node: "n1", up: true}, {peer: "192.0.2.1:179", node: "n2", up: false}},
	}
	in.metallb.advertisements[0].Spec.IPAddressPools = []string{"tiny"}

	got := findingKinds(newSection(checkLoadBalancers, 0, metallbFindings(in, true)))
	want := []string{"lbPoolExhausted:critical:tiny", "lbConfigError:critical:speaker", "lbBGPPeerDown:critical:router", "lbPoolUnadvertised:warning:free"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestKubeLoadBalancingDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeLoadBalancing(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var lb kubeLoadBalancing
	if err := json.Unmarshal([]byte(out), &lb); err != nil {
		t.Fatal(err)
	}

	// The Services and checkup demos' pending Service, with the pool they name.
	if first := lb.Services[0]; first.Name != "hello-ichor-public" || first.Cause != lbCausePoolExhausted || lb.Pools[0].Name != "default-pool" || lb.Pools[0].Available != 0 {
		t.Errorf("demo %+v %+v", first, lb.Pools[0])
	}
}
