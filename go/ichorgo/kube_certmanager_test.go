package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// Shaped like cert-manager's objects on a real cluster (fixtureNow is 2026-10-03T12:00:00Z): an
// ACME ClusterIssuer, a CA Issuer whose secret is gone, and certificates in every state.
const (
	certificatesFixture = `{"items":[
  {"metadata":{"name":"web","namespace":"app"},
   "spec":{"secretName":"web-tls","commonName":"web.example.com","dnsNames":["web.example.com","www.example.com"],"issuerRef":{"name":"letsencrypt","kind":"ClusterIssuer","group":"cert-manager.io"}},
   "status":{"conditions":[{"type":"Ready","status":"True","reason":"Ready","message":"Certificate is up to date and has not expired"}],
     "notAfter":"2026-12-01T00:00:00Z","notBefore":"2026-09-02T00:00:00Z","renewalTime":"2026-11-01T00:00:00Z"}},
  {"metadata":{"name":"old","namespace":"app"},
   "spec":{"secretName":"old-tls","dnsNames":["old.example.com"],"issuerRef":{"name":"letsencrypt","kind":"ClusterIssuer"}},
   "status":{"conditions":[{"type":"Ready","status":"False","reason":"Expired","message":"Certificate expired on Thu, 01 Oct 2026 00:00:00 UTC"}],
     "notAfter":"2026-10-01T00:00:00Z","renewalTime":"2026-09-01T00:00:00Z","failedIssuanceAttempts":5}},
  {"metadata":{"name":"failing","namespace":"app"},
   "spec":{"secretName":"failing-tls","dnsNames":["failing.example.com"],"issuerRef":{"name":"letsencrypt","kind":"ClusterIssuer"}},
   "status":{"conditions":[{"type":"Ready","status":"False","reason":"Failed","message":"The certificate request has failed to complete and will be retried"},{"type":"Issuing","status":"False","reason":"Failed"}],
     "notAfter":"2026-10-08T00:00:00Z","renewalTime":"2026-09-08T00:00:00Z","failedIssuanceAttempts":3,"lastFailureTime":"2026-10-03T10:00:00Z"}},
  {"metadata":{"name":"stuck","namespace":"app"},
   "spec":{"secretName":"stuck-tls","dnsNames":["stuck.example.com"],"issuerRef":{"name":"letsencrypt","kind":"ClusterIssuer"}},
   "status":{"conditions":[{"type":"Ready","status":"True","reason":"Ready"}],
     "notAfter":"2026-10-12T00:00:00Z","renewalTime":"2026-09-12T00:00:00Z"}},
  {"metadata":{"name":"new","namespace":"app"},
   "spec":{"secretName":"new-tls","dnsNames":["new.example.com"],"issuerRef":{"name":"letsencrypt","kind":"ClusterIssuer"}},
   "status":{"conditions":[{"type":"Ready","status":"False","reason":"DoesNotExist","message":"Issuing certificate as Secret does not exist"},{"type":"Issuing","status":"True"}]}},
  {"metadata":{"name":"internal","namespace":"app"},
   "spec":{"secretName":"internal-tls","dnsNames":["a.example.com","b.example.com","c.example.com","d.example.com","e.example.com","f.example.com"],"issuerRef":{"name":"internal-ca"}},
   "status":{"conditions":[{"type":"Ready","status":"True"}],"notAfter":"2027-01-01T00:00:00Z","renewalTime":"2026-12-01T00:00:00Z"}},
  {"metadata":{"name":"short","namespace":"mesh"},
   "spec":{"secretName":"short-tls","dnsNames":["mesh.example.com"],"issuerRef":{"name":"step","kind":"StepClusterIssuer","group":"certmanager.step.sm"}},
   "status":{"conditions":[{"type":"Ready","status":"True"}],"notAfter":"2026-10-04T00:00:00Z","renewalTime":"2026-10-03T16:00:00Z"}}]}`

	issuersFixture = `{"items":[
  {"metadata":{"name":"internal-ca","namespace":"app"},"spec":{"ca":{"secretName":"internal-ca"}},
   "status":{"conditions":[{"type":"Ready","status":"False","reason":"ErrGetKeyPair","message":"Error getting keypair for CA issuer: secrets \"internal-ca\" not found"}]}}]}`

	clusterIssuersFixture = `{"items":[
  {"metadata":{"name":"letsencrypt"},"spec":{"acme":{"server":"https://acme-v02.api.letsencrypt.org/directory","email":"admin@example.com"}},
   "status":{"conditions":[{"type":"Ready","status":"True","reason":"ACMEAccountRegistered","message":"The ACME account was registered with the ACME server"}]}},
  {"metadata":{"name":"selfsigned"},"spec":{"selfSigned":{}},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`
)

func certManagerFixture(t *testing.T) *certManagerStatus {
	t.Helper()

	var (
		certs                   kubeList[certificateObject]
		issuers, clusterIssuers kubeList[issuerObject]
	)

	for raw, into := range map[string]any{certificatesFixture: &certs, issuersFixture: &issuers, clusterIssuersFixture: &clusterIssuers} {
		if err := json.Unmarshal([]byte(raw), into); err != nil {
			t.Fatal(err)
		}
	}

	return mapCertManager(certs.Items, issuers.Items, clusterIssuers.Items, fixtureNow)
}

func TestMapCertManager(t *testing.T) {
	out := certManagerFixture(t)

	type summary struct {
		Name, Health string
		Reasons      []string
	}

	var got []summary
	for _, c := range out.Certificates {
		got = append(got, summary{c.Name, c.Health, c.Reasons})
	}

	want := []summary{
		{"old", healthCritical, []string{certReasonExpired}},
		{"failing", healthCritical, []string{certReasonNotReady, certReasonExpiring, certReasonRenewalOverdue}},
		{"stuck", healthWarning, []string{certReasonExpiring, certReasonRenewalOverdue}},
		{"internal", healthWarning, []string{certReasonIssuer}},
		// Not issued yet: a warning, never issued sorts last.
		{"new", healthWarning, []string{certReasonNotReady}},
		// Renewed hours ahead by an external issuer: not expiring.
		{"short", healthOK, []string{}},
		{"web", healthOK, []string{}},
	}

	if !equalJSON(t, got, want) {
		t.Fatal("certificates differ")
	}

	web := out.Certificates[6]
	if web.Issuer != "ClusterIssuer/letsencrypt" || web.SecretName != "web-tls" || !web.Ready || web.Message != "" ||
		!slices.Equal(web.DNSNames, []string{"web.example.com", "www.example.com"}) ||
		web.NotAfter != time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("web: %+v", web)
	}

	failing := out.Certificates[1]
	if failing.FailedAttempts != 3 || failing.Message != "The certificate request has failed to complete and will be retried" {
		t.Errorf("failing: %+v", failing)
	}

	// An Issuer is looked up in the certificate's namespace; the names are capped.
	internal := out.Certificates[3]
	if internal.Issuer != "Issuer/internal-ca" || len(internal.DNSNames) != certMaxDNSNames || internal.DNSNameCount != 6 {
		t.Errorf("internal: %+v", internal)
	}
}

func TestMapCertManagerIssuers(t *testing.T) {
	out := certManagerFixture(t)

	want := []certIssuer{
		{Kind: "Issuer", Namespace: "app", Name: "internal-ca", Type: "ca", Health: healthWarning,
			Message: `Error getting keypair for CA issuer: secrets "internal-ca" not found`},
		{Kind: "ClusterIssuer", Name: "letsencrypt", Type: "acme", Server: "acme-v02.api.letsencrypt.org", Ready: true, Health: healthOK,
			Message: "The ACME account was registered with the ACME server"},
		{Kind: "ClusterIssuer", Name: "selfsigned", Type: "selfSigned", Ready: true, Health: healthOK},
	}

	if !equalJSON(t, out.Issuers, want) {
		t.Fatal("issuers differ")
	}
}

func TestCertHealthNotReadyNearExpiry(t *testing.T) {
	// Not ready with 10 days left is a warning; with 5 left it is critical.
	for days, want := range map[int]string{10: healthWarning, 5: healthCritical} {
		cert := certManagerCert{NotAfter: fixtureNow.Add(time.Duration(days) * 24 * time.Hour).UnixMilli()}
		if got, _ := certHealth(cert, true, fixtureNow); got != want {
			t.Errorf("%d days: %s, want %s", days, got, want)
		}
	}

	// Within the hour of its renewal time: renewing, not overdue.
	cert := certManagerCert{Ready: true, NotAfter: fixtureNow.Add(30 * 24 * time.Hour).UnixMilli(), RenewalTime: fixtureNow.Add(-30 * time.Minute).UnixMilli()}
	if got, reasons := certHealth(cert, true, fixtureNow); got != healthOK {
		t.Errorf("renewing: %s %v", got, reasons)
	}
}

func TestReadDataServicesCertManager(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"cert-manager.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/cert-manager.io/v1/certificates":   certificatesFixture,
		"GET /apis/cert-manager.io/v1/issuers":        issuersFixture,
		"GET /apis/cert-manager.io/v1/clusterissuers": clusterIssuersFixture,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, nil, parseHints("cert-manager"), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	cm := res.CertManager
	if cm == nil || cm.Error != "" || len(cm.Certificates) != 7 || len(cm.Issuers) != 3 || cm.Version != "v1" {
		t.Fatalf("cert-manager: %+v", cm)
	}

	if res.Longhorn != nil || res.CNPG != nil || res.Garage != nil || res.Dragonfly != nil {
		t.Errorf("only cert-manager is installed: %+v", res)
	}
}

func TestCertManagerDemoHasEveryState(t *testing.T) {
	demo := demoDataServices(fixtureNow)
	if demo.CertManager == nil || len(demo.CertManager.Certificates) < 3 || len(demo.CertManager.Issuers) < 2 {
		t.Fatal("demo misses cert-manager")
	}

	seen := map[string]bool{}
	for _, c := range demo.CertManager.Certificates {
		seen[c.Health] = true
	}

	if !seen[healthCritical] || !seen[healthWarning] || !seen[healthOK] {
		t.Errorf("demo states: %v", seen)
	}
}
