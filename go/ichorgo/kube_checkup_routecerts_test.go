package ichorgo

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// routeCertsIngresses point at an expired plain certificate, an expiring one (shared by two
// Ingresses), one naming other hosts, an unreachable host and a cert-manager one.
const routeCertsIngresses = `{"items":[
	{"metadata":{"name":"old","namespace":"web"},"spec":{"tls":[{"hosts":["old.home.example"],"secretName":"old-tls"}]}},
	{"metadata":{"name":"soon","namespace":"web"},"spec":{"tls":[{"hosts":["soon.home.example"],"secretName":"soon-tls"}]}},
	{"metadata":{"name":"soon-too","namespace":"web"},"spec":{"tls":[{"hosts":["soon.home.example"],"secretName":"soon-tls"}]}},
	{"metadata":{"name":"lan","namespace":"web"},"spec":{"tls":[{"hosts":["paperless.home.example"],"secretName":"lan-tls"}]}},
	{"metadata":{"name":"gone","namespace":"web"},"spec":{"tls":[{"hosts":["gone.home.example"],"secretName":"gone-tls"}]}},
	{"metadata":{"name":"managed","namespace":"web"},"spec":{"tls":[{"hosts":["m.home.example"],"secretName":"managed-tls"}]}},
	{"metadata":{"name":"plain-http","namespace":"web"},"spec":{}}]}`

func routeCertsHosts(t *testing.T) {
	t.Helper()

	now := time.Now()
	testTLSHosts(t, map[string]testTLSCert{
		"old.home.example":       {now.Add(-24 * time.Hour), []string{"old.home.example"}},
		"soon.home.example":      {now.Add(5*24*time.Hour + time.Hour), []string{"soon.home.example"}},
		"paperless.home.example": {now.Add(90 * 24 * time.Hour), []string{"paperless.lan", "*.lan"}},
		"m.home.example":         {now.Add(-24 * time.Hour), []string{"m.home.example"}},
	})
}

func TestCheckupRouteCerts(t *testing.T) {
	routeCertsHosts(t)

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/networking.k8s.io/v1/ingresses":  routeCertsIngresses,
		"GET /apis/cert-manager.io/v1/certificates": `{"items":[{"metadata":{"name":"m","namespace":"web"},"spec":{"secretName":"managed-tls"}}]}`,
	})
	in := checkupInput{now: time.Now(), groups: map[string]string{groupCertManager: "v1"}}

	s := sectionFromParts(checkCertificates, checkupRouteCerts(context.Background(), openFakeKube(t, f), in))
	if s.Status != healthCritical || s.Error != "" || s.Checked != 3 {
		t.Fatalf("section %+v", s)
	}

	want := []string{
		"routeCertExpired:critical:old-tls",
		"routeCertHostMismatch:warning:lan-tls",
		"routeCertExpiring:warning:soon-tls",
	}
	if got := findingKinds(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v", got)
	}

	soon := s.Findings[2]
	if soon.Namespace != "web" || soon.Extra != "Ingress web/soon" || soon.Reason != "soon.home.example" || soon.Count != 5 || soon.Since == 0 {
		t.Errorf("expiring %+v", soon)
	}

	if lan := s.Findings[1]; lan.Message != "paperless.lan, *.lan" {
		t.Errorf("mismatch %+v", lan)
	}

	for _, r := range f.recorded() {
		if strings.Contains(r.path, "/secrets") {
			t.Errorf("read %s: certificates come from the handshake, never a Secret", r.path)
		}
	}
}

// No route at all: the part is still served, with nothing found.
func TestCheckupRouteCertsNoRoutes(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis/networking.k8s.io/v1/ingresses": `{"items":[]}`})

	p := checkupRouteCerts(context.Background(), openFakeKube(t, f), checkupInput{now: time.Now(), groups: map[string]string{}})
	if !p.served || p.checked != 0 || len(p.findings) != 0 || len(p.errs) != 0 {
		t.Fatalf("part %+v", p)
	}
}

func TestRouteTLSRefsOfGatewaysAndTraefik(t *testing.T) {
	var g gatewayObject
	if err := jsonUnmarshalString(`{"metadata":{"name":"public","namespace":"gw"},"spec":{"listeners":[
		{"name":"https","hostname":"*.home.example","protocol":"HTTPS","tls":{"certificateRefs":[{"name":"wild-tls"}]}}]}}`, &g); err != nil {
		t.Fatal(err)
	}

	var ir ingressRouteObject
	if err := jsonUnmarshalString(`{"metadata":{"name":"grafana","namespace":"monitoring"},
		"spec":{"tls":{"secretName":"grafana-tls"},"routes":[{"match":"Host(`+"`grafana.home.example`"+`)"}]}}`, &ir); err != nil {
		t.Fatal(err)
	}

	refs := routeTLSRefs(nil, []gatewayObject{g}, []ingressRouteObject{ir})
	if len(refs) != 2 || refs[0].secret != "gw/wild-tls" || refs[0].route != "Gateway gw/public" ||
		refs[1].secret != "monitoring/grafana-tls" || !reflect.DeepEqual(refs[1].hosts, []string{"grafana.home.example"}) {
		t.Fatalf("refs %+v", refs)
	}

	if hosts := probeableHosts(refs[0].hosts); len(hosts) != 0 {
		t.Errorf("a wildcard listener is probed: %v", hosts)
	}
}

func jsonUnmarshalString(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// A wildcard tls entry (or one without hosts) is read at the rule hosts it covers.
func TestIngressTLSHostsBehindAWildcard(t *testing.T) {
	var ing ingressObject
	if err := jsonUnmarshalString(`{"spec":{"rules":[{"host":"a.home.example"},{"host":"b.other.example"},{"host":"10.0.0.5"}]}}`, &ing); err != nil {
		t.Fatal(err)
	}

	if got := ingressTLSHosts(ing, []string{"*.home.example"}); !reflect.DeepEqual(got, []string{"a.home.example"}) {
		t.Errorf("wildcard %v", got)
	}

	if got := ingressTLSHosts(ing, nil); !reflect.DeepEqual(got, []string{"a.home.example", "b.other.example"}) {
		t.Errorf("no hosts %v", got)
	}
}
