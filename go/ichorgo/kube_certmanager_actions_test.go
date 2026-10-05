package ichorgo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const certPath = "/apis/cert-manager.io/v1/namespaces/web/certificates/site"

var certRenewTime = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func certManagerActionAPI(t *testing.T, cert string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                     `{"groups":[{"name":"cert-manager.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + certPath:               cert,
		"PATCH " + certPath + "/status": `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func TestRenewCertificateAddsTheIssuingCondition(t *testing.T) {
	f, k := certManagerActionAPI(t, `{"metadata":{"resourceVersion":"12","generation":3},"status":{"conditions":[
		{"type":"Ready","status":"True","reason":"Ready","lastTransitionTime":"2026-09-01T00:00:00Z","observedGeneration":3}]}}`)

	if err := renewCertificate(context.Background(), k, "web", "site", certRenewTime); err != nil {
		t.Fatal(err)
	}

	want := `{"metadata":{"resourceVersion":"12"},"status":{"conditions":[` +
		`{"lastTransitionTime":"2026-09-01T00:00:00Z","observedGeneration":3,"reason":"Ready","status":"True","type":"Ready"},` +
		`{"lastTransitionTime":"2026-10-05T12:00:00Z","message":"Certificate re-issuance manually triggered","observedGeneration":3,"reason":"ManuallyTriggered","status":"True","type":"Issuing"}]}}`

	last := lastRequest(f)
	if last.method != "PATCH" || last.path != certPath+"/status" || last.contentType != "application/merge-patch+json" || last.body != want {
		t.Fatalf("patch %+v", last)
	}
}

func TestRenewCertificateReplacesAFalseIssuingCondition(t *testing.T) {
	f, k := certManagerActionAPI(t, `{"metadata":{"resourceVersion":"5","generation":1},"status":{"conditions":[
		{"type":"Issuing","status":"False","reason":"Failed"}]}}`)

	if err := renewCertificate(context.Background(), k, "web", "site", certRenewTime); err != nil {
		t.Fatal(err)
	}

	last := lastRequest(f)
	if strings.Count(last.body, `"type":"Issuing"`) != 1 || !strings.Contains(last.body, `"status":"True"`) || strings.Contains(last.body, "Failed") {
		t.Fatalf("patch %+v", last)
	}
}

func TestRenewCertificateRefusesWhileIssuing(t *testing.T) {
	f, k := certManagerActionAPI(t, `{"metadata":{"resourceVersion":"5"},"status":{"conditions":[{"type":"Issuing","status":"True"}]}}`)

	if err := renewCertificate(context.Background(), k, "web", "site", certRenewTime); !errors.Is(err, errCertIssuing) {
		t.Fatalf("got %v", err)
	}

	if last := lastRequest(f); last.method != "GET" {
		t.Fatalf("nothing may be sent: %+v", last)
	}
}

func TestRenewCertificateWithoutCertManager(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := renewCertificate(context.Background(), k, "web", "site", certRenewTime); !errors.Is(err, errCertManagerMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestKubeCertManagerRenewValidates(t *testing.T) {
	for _, c := range [][2]string{{"web", ""}, {"", "site"}, {"web", "../x"}} {
		if err := KubeCertManagerRenew("", "", "", c[0], c[1]); err == nil {
			t.Errorf("%v: accepted", c)
		}
	}
}

func TestKubeCertManagerRenewDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeCertManagerRenew(cfg, "", "", "web", "site"); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestMapCertificateIssuing(t *testing.T) {
	var obj certificateObject
	obj.Status.Conditions = []kubeCondition{{Type: "Ready", Status: "True"}, {Type: "Issuing", Status: "True"}}

	if cert := mapCertificate(obj, nil, certRenewTime); !cert.Issuing {
		t.Fatalf("issuing: %+v", cert)
	}

	obj.Status.Conditions[1].Status = "False"
	if cert := mapCertificate(obj, nil, certRenewTime); cert.Issuing {
		t.Fatalf("not issuing: %+v", cert)
	}
}
