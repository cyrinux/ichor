package ichorgo

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The kube-green row comes from its plugin's detect entries; Settings finds it by its API group
// and the resource it lists.
func TestPluginSpecs(t *testing.T) {
	var green *integrationSpec

	for _, s := range integrationSpecs() {
		if s.ID == "kube-green" {
			green = &s
		}
	}

	want := integrationSpec{ID: "kube-green", Name: "kube-green", Website: "https://kube-green.dev", Groups: []string{groupKubeGreen}, Resource: "sleepinfos"}
	if green == nil || !reflect.DeepEqual(*green, want) {
		t.Fatalf("kube-green row %+v, want %+v", green, want)
	}

	src := `schemaVersion: ichor.dev/plugin/v1alpha1
id: demo-detect
name: Demo Detect
website: https://example.com
catalogId: demo-app
detect:
  - { group: a.example.com, resource: things }
  - { group: b.example.com }
  - { group: a.example.com }
  - { pods: demo-app }
  - { service: prometheus }
resources:
  thing: { group: a.example.com, version: v1, resource: things, kind: Thing, scope: Cluster }
cards:
  summary: { title: { count: 1, one: "%d thing", other: "%d things" } }
samples:
  thing: [{ metadata: { name: one } }]
`

	c, err := plugin.CompileSource("detect.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	got := pluginSpecs([]*plugin.Compiled{c})
	want = integrationSpec{
		ID: "demo-app", Name: "Demo Detect", Website: "https://example.com", Groups: []string{"a.example.com", "b.example.com"},
		AnyGroup: true, Resource: "things", ServiceKind: "prometheus", PodsFallback: true,
	}

	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("spec %+v, want %+v", got, want)
	}
}

// A cluster serving kube-green's SleepInfos shows kube-green as detected, through the derived
// row; so does the demo, whose plugins render their samples.
func TestReadSupportedIntegrationsKubeGreenPlugin(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                         kubeGreenGroups,
		"GET /apis/kube-green.com/v1alpha1": kubeGreenResources,
		"GET /api/v1/pods":                  `{"items":[]}`,
		"GET /api/v1/services":              `{"items":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readSupportedIntegrations(context.Background(), k, parseHints(""))
	if err != nil {
		t.Fatal(err)
	}

	for _, it := range res.Items {
		switch {
		case it.ID == "kube-green" && (!it.Detected || it.Via != detectedByAPI || it.Version != "v1alpha1" || it.Icon != "kube-green"):
			t.Errorf("kube-green %+v", it)
		case it.ID != "kube-green" && it.Detected:
			t.Errorf("%s detected", it.ID)
		}
	}

	if !detectedIn(demoSupportedIntegrations(), "kube-green") {
		t.Error("the demo does not show kube-green")
	}
}

func detectedIn(res supportedIntegrations, id string) bool {
	for _, it := range res.Items {
		if it.ID == id {
			return it.Detected
		}
	}

	return false
}

// PluginCatalog lists the built-in kube-green plugin.
func TestPluginCatalogListsKubeGreen(t *testing.T) {
	js, err := PluginCatalog()
	if err != nil || !strings.Contains(js, `{"id":"kube-green","name":"kube-green","icon":"kube-green"`) {
		t.Fatalf("%s, %v", js, err)
	}
}
