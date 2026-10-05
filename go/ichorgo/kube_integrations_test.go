package ichorgo

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestReadIntegrations(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[
  {"name":"apps","preferredVersion":{"version":"v1"}},
  {"name":"networking.k8s.io","preferredVersion":{"version":"v1"}},
  {"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}},
  {"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}},
  {"name":"networking.istio.io","preferredVersion":{"version":"v1"}},
  {"name":"security.istio.io","preferredVersion":{"version":"v1"}},
  {"name":"cluster.x-k8s.io","preferredVersion":{"version":"v1beta1"}},
  {"name":"infrastructure.cluster.x-k8s.io","preferredVersion":{"version":"v1beta1"}},
  {"name":"kueue.x-k8s.io","preferredVersion":{"version":"v1beta1"}},
  {"name":"broken.example.io","preferredVersion":{"version":"v1"}}]}`,
		"GET /apis/networking.istio.io/v1": `{"resources":[
  {"name":"virtualservices","kind":"VirtualService"},{"name":"virtualservices/status","kind":"VirtualService"},
  {"name":"gateways","kind":"Gateway"}]}`,
		"GET /apis/security.istio.io/v1":                    `{"resources":[{"name":"authorizationpolicies","kind":"AuthorizationPolicy"}]}`,
		"GET /apis/cluster.x-k8s.io/v1beta1":                `{"resources":[{"name":"clusters","kind":"Cluster"}]}`,
		"GET /apis/infrastructure.cluster.x-k8s.io/v1beta1": `{"resources":[{"name":"talosclusters","kind":"TalosCluster"}]}`,
		"GET /apis/kueue.x-k8s.io/v1beta1":                  `{"resources":[{"name":"localqueues","kind":"LocalQueue"}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readIntegrations(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(res.Supported, []string{"argoproj.io", "longhorn.io"}) {
		t.Errorf("supported: %v", res.Supported)
	}

	var ids []string
	for _, fam := range res.Families {
		ids = append(ids, fam.ID)
	}

	if !slices.Equal(ids, []string{"cluster.x-k8s.io", "example.io", "istio.io", "kueue.x-k8s.io"}) {
		t.Fatalf("families: %v", ids)
	}

	istio := res.Families[2].Groups
	if len(istio) != 2 || istio[0].Name != "networking.istio.io" || !slices.Equal(istio[0].Kinds, []string{"Gateway", "VirtualService"}) {
		t.Errorf("istio: %+v", istio)
	}

	if capi := res.Families[0].Groups; len(capi) != 2 || !slices.Equal(capi[1].Kinds, []string{"TalosCluster"}) {
		t.Errorf("cluster api: %+v", capi)
	}

	// A group whose discovery fails is still listed, without kinds.
	if broken := res.Families[1].Groups[0]; broken.Name != "broken.example.io" || broken.Kinds == nil || len(broken.Kinds) != 0 {
		t.Errorf("broken: %+v", broken)
	}
}

func TestIntegrationFamilyOf(t *testing.T) {
	for group, want := range map[string]string{
		"traefik.io":                     "traefik.io",
		"monitoring.coreos.com":          "coreos.com",
		"generators.external-secrets.io": "external-secrets.io",
		"bootstrap.cluster.x-k8s.io":     "cluster.x-k8s.io",
		"foo.bar.github.io":              "bar.github.io",
		"localhost":                      "localhost",
	} {
		if got := integrationFamilyOf(group); got != want {
			t.Errorf("%s: %s, want %s", group, got, want)
		}
	}
}

func TestIsBuiltinGroup(t *testing.T) {
	for _, g := range []string{"apps", "batch", "policy", "networking.k8s.io", "metrics.k8s.io", "snapshot.storage.k8s.io"} {
		if !isBuiltinGroup(g) {
			t.Errorf("%s should be built in", g)
		}
	}

	for _, g := range []string{"cluster.x-k8s.io", "traefik.io", "notk8s.io"} {
		if isBuiltinGroup(g) {
			t.Errorf("%s should not be built in", g)
		}
	}
}

func TestIntegrationIssueURL(t *testing.T) {
	family, _ := json.Marshal(integrationFamily{ID: "istio.io", Groups: []integrationGroup{
		{Name: "networking.istio.io", Version: "v1", Kinds: []string{"Gateway", "VirtualService"}},
		{Name: "telemetry.istio.io", Version: "v1", Kinds: []string{}},
	}})

	raw, err := IntegrationIssueURL("owner/repo", string(family), "Ichor 1.0 (Android)")
	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	q := u.Query()
	if u.Host != "github.com" || u.Path != "/owner/repo/issues/new" || q.Get("template") != "integration.yml" ||
		q.Get("title") != "Integration: istio.io" || q.Get("operator") != "istio.io" || q.Get("app") != "Ichor 1.0 (Android)" {
		t.Errorf("url: %s", raw)
	}

	if want := "networking.istio.io/v1: Gateway, VirtualService\ntelemetry.istio.io/v1"; q.Get("groups") != want {
		t.Errorf("groups: %q", q.Get("groups"))
	}

	if _, err := IntegrationIssueURL("owner/repo", "{", ""); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestIntegrationGroupsTextIsCapped(t *testing.T) {
	kinds := make([]string, 200)
	for i := range kinds {
		kinds[i] = "SomeRatherLongKindName"
	}

	groups := []integrationGroup{{Name: "a.example.io", Version: "v1", Kinds: kinds[:10]}, {Name: "b.example.io", Version: "v1", Kinds: kinds}}

	text := integrationGroupsText(groups)
	if len(text) > integrationIssueText+len("…") || !strings.HasPrefix(text, "a.example.io/v1: ") || !strings.HasSuffix(text, "…") {
		t.Errorf("text (%d): %q", len(text), text)
	}
}

func TestIntegrationSearchURL(t *testing.T) {
	u, err := url.Parse(IntegrationSearchURL("owner/repo", "istio.io"))
	if err != nil || u.Path != "/owner/repo/issues" || u.Query().Get("q") != "is:issue label:integration istio.io" {
		t.Errorf("search: %v %v", u, err)
	}
}

func TestIntegrationsDemoSorted(t *testing.T) {
	d := demoIntegrations()
	if !slices.IsSortedFunc(d.Families, func(a, b integrationFamily) int { return strings.Compare(a.ID, b.ID) }) || !slices.IsSorted(d.Supported) {
		t.Errorf("demo not sorted: %+v", d)
	}

	for _, fam := range d.Families {
		for _, g := range fam.Groups {
			if integrationSupported[g.Name] || isBuiltinGroup(g.Name) || integrationFamilyOf(g.Name) != fam.ID {
				t.Errorf("demo group %s in %s", g.Name, fam.ID)
			}
		}
	}
}
