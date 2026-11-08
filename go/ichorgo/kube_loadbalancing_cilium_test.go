package ichorgo

import (
	"context"
	"reflect"
	"testing"
)

const (
	lbCiliumServices = `[
	  {"metadata":{"name":"edge","namespace":"shop","labels":{"color":"blue"},` + lbOldService + `},"spec":{"type":"LoadBalancer"},
	    "status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.2"}]}}},
	  {"metadata":{"name":"plain","namespace":"shop",` + lbOldService + `},"spec":{"type":"LoadBalancer"},
	    "status":{"loadBalancer":{"ingress":[{"ip":"198.51.100.1"}]}}}]`
	lbCiliumPools = `[
	  {"metadata":{"name":"blue"},"spec":{"blocks":[{"cidr":"192.0.2.0/29"}]}},
	  {"metadata":{"name":"grey"},"spec":{"blocks":[{"cidr":"198.51.100.0/30"}]}}]`
	lbCiliumPolicies = `[
	  {"metadata":{"name":"blue"},"spec":{"serviceSelector":{"matchLabels":{"color":"blue"}},"loadBalancerIPs":true,"interfaces":["^eth[0-9]+"],
	    "nodeSelector":{"matchLabels":{"l2":"yes"}}}},
	  {"metadata":{"name":"external-only"},"spec":{"externalIPs":true}},
	  {"metadata":{"name":"nobody"},"spec":{"serviceSelector":{"matchLabels":{"app":"none"}},"loadBalancerIPs":true}}]`
)

func ciliumL2API(t *testing.T) *fakeKubeAPI {
	t.Helper()

	return newFakeKubeAPI(t, map[string]string{
		"GET /apis":            `{"groups":[{"name":"cilium.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /api/v1/services": `{"items":` + lbCiliumServices + `}`,
		"GET /api/v1/pods":     `{"items":[]}`,
		"GET /apis/cilium.io/v2/ciliumloadbalancerippools":            `{"items":` + lbCiliumPools + `}`,
		"GET /apis/cilium.io/v2alpha1/ciliuml2announcementpolicies":   `{"items":` + lbCiliumPolicies + `}`,
		"GET /api/v1/namespaces/kube-system/configmaps/cilium-config": `{"data":{"enable-l2-announcements":"true","enable-bgp-control-plane":"false"}}`,
		"GET /apis/coordination.k8s.io/v1/namespaces/kube-system/leases": `{"items":[
		  {"metadata":{"name":"cilium-l2announce-shop-edge"},"spec":{"holderIdentity":"n1"}},
		  {"metadata":{"name":"cilium-operator-resource-lock"},"spec":{"holderIdentity":"n2"}}]}`,
	})
}

func TestReadCiliumL2Announcements(t *testing.T) {
	lb, err := readLoadBalancing(context.Background(), openFakeKube(t, ciliumL2API(t)), lbNow)
	if err != nil {
		t.Fatal(err)
	}

	if lb.Controller != lbControllerCilium || lb.PartialAccess || len(lb.Pools) != 2 {
		t.Fatalf("got %+v", lb)
	}

	// Problems first: grey holds plain's address and no policy announces plain.
	grey, blue := lb.Pools[0], lb.Pools[1]
	if grey.Name != "grey" || len(grey.AdvertisedBy) != 0 || grey.Level != serviceWarning {
		t.Errorf("grey %+v", grey)
	}

	if blue.Name != "blue" || !reflect.DeepEqual(blue.AdvertisedBy, []string{"l2:blue"}) || blue.Level != serviceOK {
		t.Errorf("blue %+v", blue)
	}

	byName := map[string]lbAdvertisement{}
	for _, a := range lb.Advertisements {
		byName[a.Name] = a
	}

	if a := byName["blue"]; a.Controller != lbControllerCilium || a.Kind != lbAdvertL2 || a.Inactive != "" || a.AllPools ||
		!reflect.DeepEqual(a.Pools, []string{"blue"}) || !reflect.DeepEqual(a.Interfaces, []string{"^eth[0-9]+"}) || a.NodeSelectors != 1 {
		t.Errorf("blue policy %+v", a)
	}

	if a := byName["external-only"]; a.Inactive != lbAdvertNoLBIPs || !a.AllPools {
		t.Errorf("external-only policy %+v", a)
	}

	if a := byName["nobody"]; a.Inactive != lbAdvertNoServiceMatch || len(a.Pools) != 0 {
		t.Errorf("nobody policy %+v", a)
	}

	announced := map[string]string{}
	for _, s := range lb.Services {
		announced[s.Name] = s.AnnouncedBy
	}

	if !reflect.DeepEqual(announced, map[string]string{"edge": "n1", "plain": ""}) {
		t.Errorf("announcers %v", announced)
	}
}

func TestCiliumL2Findings(t *testing.T) {
	services := decodeAll[lbServiceObject](t, lbCiliumServices)
	pools := decodeAll[ciliumPoolObject](t, lbCiliumPools)
	policies := decodeAll[ciliumL2Policy](t, lbCiliumPolicies)

	for _, tc := range []struct {
		name string
		l2   *ciliumL2State
		want []string
	}{
		{"grey unannounced", &ciliumL2State{policies: policies, configKnown: true, l2Enabled: true}, []string{"lbCiliumPoolUnannounced:warning:grey"}},
		{"announcements off", &ciliumL2State{policies: policies, configKnown: true},
			[]string{"lbCiliumPoolUnannounced:warning:blue", "lbCiliumPoolUnannounced:warning:grey"}},
		{"BGP may announce", &ciliumL2State{policies: policies, configKnown: true, l2Enabled: true, bgpEnabled: true}, []string{}},
		{"cilium-config unread", &ciliumL2State{policies: policies}, []string{}},
		{"L2 not used", &ciliumL2State{configKnown: true}, []string{}},
		{"no L2 state", nil, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := lbInputs{services: services, cilium: pools, ciliumL2: tc.l2}

			if got := findingKinds(newSection(checkLoadBalancers, 0, poolFindings(in, false))); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCiliumL2PolicyRows(t *testing.T) {
	in := lbInputs{
		services: decodeAll[lbServiceObject](t, lbCiliumServices),
		cilium:   decodeAll[ciliumPoolObject](t, lbCiliumPools),
		ciliumL2: &ciliumL2State{policies: decodeAll[ciliumL2Policy](t, `[{"metadata":{"name":"all"},"spec":{"loadBalancerIPs":true}}]`), configKnown: true},
	}

	// Off in cilium-config: the policy announces nothing, whatever it selects.
	if rows := lbCiliumAdvertisementRows(in); len(rows) != 1 || rows[0].Inactive != lbAdvertL2Disabled || !reflect.DeepEqual(rows[0].Pools, []string{"blue", "grey"}) {
		t.Errorf("rows %+v", rows)
	}

	in.ciliumL2.l2Enabled = true

	if got := in.ciliumL2.advertisers(in.cilium[1], in.services); !reflect.DeepEqual(got, []string{"l2:all"}) {
		t.Errorf("advertisers %v", got)
	}
}

func TestCiliumL2PolicySelects(t *testing.T) {
	policy := decodeAll[ciliumL2Policy](t, `[{"metadata":{"name":"p"},"spec":{"serviceSelector":{"matchLabels":{"io.kubernetes.service.namespace":"shop"}},"loadBalancerIPs":true}}]`)[0]

	for _, tc := range []struct {
		service string
		want    bool
	}{
		{`{"metadata":{"name":"a","namespace":"shop"},"spec":{"type":"LoadBalancer"}}`, true},
		{`{"metadata":{"name":"a","namespace":"lab"},"spec":{"type":"LoadBalancer"}}`, false},
		{`{"metadata":{"name":"a","namespace":"shop"},"spec":{"type":"LoadBalancer","loadBalancerClass":"io.cilium/l2-announcer"}}`, true},
		{`{"metadata":{"name":"a","namespace":"shop"},"spec":{"type":"LoadBalancer","loadBalancerClass":"io.cilium/bgp-control-plane"}}`, false},
	} {
		if got := policy.selects(decodeAll[lbServiceObject](t, `[`+tc.service+`]`)[0]); got != tc.want {
			t.Errorf("%s: got %v", tc.service, got)
		}
	}
}

// Cilium installed elsewhere than kube-system: cilium-config and the leases are in its agents' namespace.
func TestReadCiliumL2InItsNamespace(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/pods": `{"items":[{"metadata":{"name":"cilium-x1","namespace":"net"},"spec":{"nodeName":"n1",
		  "containers":[{"name":"cilium-agent","image":"quay.io/cilium/cilium:v1.18.2"}]},"status":{"phase":"Running"}}]}`,
		"GET /apis/cilium.io/v2alpha1/ciliuml2announcementpolicies":      `{"items":` + lbCiliumPolicies + `}`,
		"GET /api/v1/namespaces/net/configmaps/cilium-config":            `{"data":{"enable-l2-announcements":"true"}}`,
		"GET /apis/coordination.k8s.io/v1/namespaces/net/leases":         `{"items":[{"metadata":{"name":"cilium-l2announce-shop-edge"},"spec":{"holderIdentity":"n1"}}]}`,
		"GET /apis/coordination.k8s.io/v1/namespaces/kube-system/leases": `{"items":[]}`,
	})

	s, err := readCiliumL2(context.Background(), openFakeKube(t, f))
	if err != nil {
		t.Fatal(err)
	}

	edge := decodeAll[lbServiceObject](t, lbCiliumServices)[0]
	if !s.configKnown || !s.l2Enabled || len(s.policies) != 3 || s.holderOf(edge) != "n1" {
		t.Errorf("got %+v", s)
	}
}
