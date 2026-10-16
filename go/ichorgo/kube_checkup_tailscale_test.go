package ichorgo

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func tailscaleCheckupFrom(t *testing.T, f *fakeKubeAPI) checkupSection {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return checkupTailscale(context.Background(), k, checkupInput{now: fixtureNow, groups: map[string]string{groupTailscale: "v1alpha1"}})
}

func TestCheckupTailscale(t *testing.T) {
	s := tailscaleCheckupFrom(t, newFakeKubeAPI(t, tailscaleAnswers(t)))
	if s.ID != checkTailscale || s.Status != healthCritical || s.Error != "" || s.Checked != 13 {
		t.Fatalf("section %+v", s)
	}

	want := []string{
		"tailscaleConnectorNotReady:critical:saas-connector",
		"tailscaleProxyGroupDown:critical:api-proxies",
		"tailscaleProxyGroupDown:critical:old-proxies",
		"tailscaleConnectorNotReady:warning:edge-router",
		"tailscaleConnectorNotReady:warning:lab-router",
		"tailscaleProxyGroupDown:warning:egress-proxies",
	}
	if got := findingKinds(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v", got)
	}

	saas, api, edge := s.Findings[0], s.Findings[1], s.Findings[3]
	if saas.Extra != tsRoleAppConnector || saas.Reason != "ConnectorCreationFailed" || saas.Message == "" {
		t.Errorf("connector %+v", saas)
	}

	if edge.Extra != tsRoleSubnetRouter || edge.Reason != tsReasonProxyClassMissing {
		t.Errorf("connector naming a missing class %+v", edge)
	}

	if api.Extra != "kube-apiserver" {
		t.Errorf("proxy group %+v", api)
	}

	for _, f := range s.Findings {
		if f.Name == "stale-router" {
			t.Errorf("a connector reconciling a changed spec is a finding: %+v", f)
		}
	}
}

// A ProxyClass that is not ready is a finding only while something names it.
func TestCheckupTailscaleProxyClassInUse(t *testing.T) {
	answers := tailscaleAnswers(t)
	answers["GET /apis/tailscale.com/v1alpha1/connectors"] = strings.Replace(answers["GET /apis/tailscale.com/v1alpha1/connectors"], `"proxyClass":"prod"`, `"proxyClass":"metrics"`, 1)

	s := tailscaleCheckupFrom(t, newFakeKubeAPI(t, answers))
	found := false
	for _, f := range s.Findings {
		if f.Kind == findTailscaleProxyClassNotReady {
			found = f.Name == "metrics" && f.Count == 1 && f.Severity == sevWarning
		}
	}

	if !found {
		t.Fatalf("findings %v", findingKinds(s))
	}
}

func TestCheckupTailscaleOperatorDown(t *testing.T) {
	answers := tailscaleAnswers(t)
	answers["GET /apis/apps/v1/deployments"] = strings.Replace(answers["GET /apis/apps/v1/deployments"], `"readyReplicas":1`, `"readyReplicas":0`, 1)

	s := tailscaleCheckupFrom(t, newFakeKubeAPI(t, answers))
	if s.Findings[0].Kind != findTailscaleOperatorDown || s.Findings[0].Severity != sevCritical || s.Findings[0].Extra != "0/1" {
		t.Fatalf("operator %+v", s.Findings[0])
	}
}

func TestCheckupTailscaleAbsent(t *testing.T) {
	s := checkupTailscale(context.Background(), nil, checkupInput{now: fixtureNow, groups: map[string]string{"cilium.io": "v2"}})
	if s.Status != checkAbsent || s.Findings == nil {
		t.Fatalf("absent %+v", s)
	}
}

// A forbidden listing is the section's error; what the other lists show still counts.
func TestCheckupTailscaleForbidden(t *testing.T) {
	f := newFakeKubeAPI(t, tailscaleAnswers(t))
	f.answerWith("GET /apis/tailscale.com/v1alpha1/proxygroups", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"listing denied"}`)

	s := tailscaleCheckupFrom(t, f)
	if !strings.Contains(s.Error, "listing denied") || s.Status != healthCritical {
		t.Fatalf("section %+v", s)
	}

	for _, f := range s.Findings {
		if f.Kind == findTailscaleProxyGroupDown {
			t.Errorf("a proxy group from a refused list: %+v", f)
		}
	}
}

func TestTailscaleConnectorRoles(t *testing.T) {
	for _, c := range []struct {
		row  tsConnector
		want []string
	}{
		{tsConnector{AdvertisedRoutes: []string{"10.0.0.0/24"}}, []string{tsRoleSubnetRouter}},
		{tsConnector{SubnetRoutes: "10.0.0.0/24", ExitNode: true}, []string{tsRoleSubnetRouter, tsRoleExitNode}},
		{tsConnector{AppConnector: true}, []string{tsRoleAppConnector}},
		{tsConnector{}, []string{}},
	} {
		if got := tsConnectorRoles(c.row); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v", c.row, got)
		}
	}
}
