package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"
)

const (
	cmDetailsBase = "/apis/cert-manager.io/v1/namespaces/web/"
	acmeBase      = "/apis/acme.cert-manager.io/v1/namespaces/web/"
)

// A certificate with two requests (the newest stuck on an HTTP-01 challenge), a request and an
// order of another certificate, events of both and a controller log naming both.
func certDetailsAPI(t *testing.T, withACME bool) *kubeClient {
	t.Helper()

	groups := `{"groups":[{"name":"cert-manager.io","preferredVersion":{"version":"v1"}}]}`
	if withACME {
		groups = `{"groups":[{"name":"cert-manager.io","preferredVersion":{"version":"v1"}},{"name":"acme.cert-manager.io","preferredVersion":{"version":"v1"}}]}`
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": groups,
		"GET " + cmDetailsBase + "certificates/site": `{"metadata":{"name":"site"},"status":{"conditions":[
			{"type":"Ready","status":"False","reason":"Failed","message":"order failed","lastTransitionTime":"2026-10-05T10:00:00Z"}]}}`,
		"GET " + cmDetailsBase + "certificaterequests": `{"items":[
			{"metadata":{"name":"site-1","creationTimestamp":"2026-09-01T00:00:00Z","annotations":{"cert-manager.io/certificate-name":"site"}},
			 "status":{"conditions":[{"type":"Ready","status":"True","reason":"Issued"}]}},
			{"metadata":{"name":"site-2","creationTimestamp":"2026-10-05T09:00:00Z","ownerReferences":[{"kind":"Certificate","name":"site"}]},
			 "status":{"conditions":[{"type":"Ready","status":"False","reason":"Pending"}]}},
			{"metadata":{"name":"site-2b-1","creationTimestamp":"2026-10-05T09:30:00Z","annotations":{"cert-manager.io/certificate-name":"site-2b"}}}]}`,
		"GET " + acmeBase + "orders": `{"items":[
			{"metadata":{"name":"site-2-77","ownerReferences":[{"kind":"CertificateRequest","name":"site-2"}]},"status":{"state":"pending"}},
			{"metadata":{"name":"site-2b-1-88","ownerReferences":[{"kind":"CertificateRequest","name":"site-2b-1"}]},"status":{"state":"valid"}}]}`,
		"GET " + acmeBase + "challenges": `{"items":[
			{"metadata":{"name":"site-2-77-1","ownerReferences":[{"kind":"Order","name":"site-2-77"}]},
			 "spec":{"type":"HTTP-01","dnsName":"site.example.com"},"status":{"state":"pending","reason":"wrong status code '404'","presented":true}}]}`,
		"GET /api/v1/namespaces/web/events": `{"items":[
			{"involvedObject":{"kind":"Challenge","name":"site-2-77-1"},"type":"Warning","reason":"PresentError","message":"404","count":5,"lastTimestamp":"2026-10-05T10:05:00Z"},
			{"involvedObject":{"kind":"Certificate","name":"site"},"type":"Normal","reason":"Requested","eventTime":"2026-10-05T09:00:00.123456Z",
			 "series":{"count":2,"lastObservedTime":"2026-10-05T09:10:00.000000Z"}},
			{"involvedObject":{"kind":"Certificate","name":"site-2b"},"type":"Normal","reason":"Requested","lastTimestamp":"2026-10-05T10:10:00Z"},
			{"involvedObject":{"kind":"Pod","name":"site"},"type":"Normal","reason":"Started","lastTimestamp":"2026-10-05T10:20:00Z"}]}`,
		"GET /api/v1/pods": `{"items":[
			{"metadata":{"name":"cm-0","namespace":"cert-manager"},"status":{"phase":"Running"}},
			{"metadata":{"name":"cm-1","namespace":"cert-manager"},"status":{"phase":"Pending"}}]}`,
		"GET /api/v1/namespaces/cert-manager/pods/cm-0/log": "I1 \"msg\"=\"sync\" \"resource_name\"=\"site\" \"resource_namespace\"=\"web\"\n" +
			"I2 propagation check failed resource_name=\"site-2-77-1\" resource_namespace=\"web\"\n" +
			"I3 \"resource_name\"=\"site-2b\" \"resource_namespace\"=\"web\"\n" +
			"I4 \"resource_name\"=\"site\" \"resource_namespace\"=\"other\"\n" +
			"I5 leader election\n",
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestReadCertDetailsFollowsTheChain(t *testing.T) {
	d, err := readCertDetails(context.Background(), certDetailsAPI(t, true), "web", "site")
	if err != nil {
		t.Fatal(err)
	}

	if d.Error != "" {
		t.Fatalf("error: %s", d.Error)
	}

	if len(d.Conditions) != 1 || d.Conditions[0].Message != "order failed" || d.Conditions[0].Time == 0 {
		t.Errorf("conditions: %+v", d.Conditions)
	}

	if names := []string{d.Requests[0].Name, d.Requests[1].Name}; len(d.Requests) != 2 || !slices.Equal(names, []string{"site-2", "site-1"}) {
		t.Fatalf("requests: %+v", d.Requests)
	}

	orders := d.Requests[0].Orders
	if len(orders) != 1 || orders[0].Name != "site-2-77" || len(orders[0].Challenges) != 1 {
		t.Fatalf("orders: %+v", orders)
	}

	if c := orders[0].Challenges[0]; c.Type != "HTTP-01" || c.DNSName != "site.example.com" || c.Reason != "wrong status code '404'" || !c.Presented {
		t.Errorf("challenge: %+v", c)
	}

	if len(d.Requests[1].Orders) != 0 {
		t.Errorf("site-1 has no order: %+v", d.Requests[1])
	}

	if len(d.Events) != 2 || d.Events[0].Object != "Challenge/site-2-77-1" || d.Events[0].Count != 5 {
		t.Fatalf("events: %+v", d.Events)
	}

	if e := d.Events[1]; e.Count != 2 || e.Time != time.Date(2026, 10, 5, 9, 10, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("series event: %+v", e)
	}

	if len(d.Log) != 2 || d.Log[0][:2] != "I1" || d.Log[1][:2] != "I2" {
		t.Errorf("log: %q", d.Log)
	}
}

func TestReadCertDetailsWithoutACME(t *testing.T) {
	d, err := readCertDetails(context.Background(), certDetailsAPI(t, false), "web", "site")
	if err != nil {
		t.Fatal(err)
	}

	if len(d.Requests) != 2 || len(d.Requests[0].Orders) != 0 || d.Error != "" {
		t.Fatalf("details: %+v", d)
	}
}

func TestReadCertDetailsMissingCertificate(t *testing.T) {
	_, err := readCertDetails(context.Background(), certDetailsAPI(t, true), "web", "gone")
	if err == nil {
		t.Fatal("a missing certificate must fail")
	}
}

func TestMentionsName(t *testing.T) {
	cases := map[string]bool{
		`"resource_name"="site"`:   true,
		`resource_name="site"`:     true,
		`{"resource_name":"site"}`: true,
		`certificate web/site is`:  true,
		`web/site`:                 true,
		`"resource_name"="site-2"`: false,
		`website"`:                 false,
		`"mysite"`:                 false,
	}

	for line, want := range cases {
		if got := mentionsName(line, "site"); got != want {
			t.Errorf("%q: got %v", line, got)
		}
	}
}

func TestKubeCertManagerDetailsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeCertManagerDetails(cfg, "", "", "demo", "shop-tls")
	if err != nil {
		t.Fatal(err)
	}

	var d certDetails
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}

	if len(d.Requests) != 1 || len(d.Requests[0].Orders[0].Challenges) != 2 || len(d.Events) == 0 || len(d.Log) == 0 {
		t.Fatalf("demo: %+v", d)
	}
}

func TestKubeCertManagerDetailsValidates(t *testing.T) {
	if _, err := KubeCertManagerDetails("", "", "", "web", "../x"); err == nil {
		t.Fatal("accepted")
	}

	if _, err := KubeCertManagerDetails("", "", "", "web", ""); err == nil || errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}
