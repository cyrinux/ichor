package ichorgo

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The kube-green row lists its plugin's group and keeps the plugin, whose detect decides.
func TestPluginSpecs(t *testing.T) {
	for _, s := range integrationSpecs() {
		if s.ID != "kube-green" {
			continue
		}

		if s.fromPlugin == nil || s.fromPlugin.Plugin.ID != "kube-green" || !reflect.DeepEqual(s.Groups, []string{groupKubeGreen}) ||
			s.Name != "kube-green" || s.Website != "https://kube-green.dev" {
			t.Fatalf("kube-green row %+v", s)
		}

		return
	}

	t.Fatal("no kube-green row")
}

// demoDetectPlugin detects by its resource in a shared group, another group, its pods or a
// Prometheus Service.
const demoDetectPlugin = `schemaVersion: ichor.dev/plugin/v1alpha1
id: demo-detect
name: Demo Detect
website: https://example.com
catalogId: demo-app
detect:
  - { group: a.example.com, resource: things }
  - { group: b.example.com }
  - { pods: demo-app }
  - { service: prometheus }
resources:
  thing: { group: a.example.com, version: v1, resource: things, kind: Thing, scope: Cluster }
cards:
  summary: { title: { count: 1, one: "%d thing", other: "%d things" } }
samples:
  thing: [{ metadata: { name: one } }]
`

// Settings detects a plugin's row as the plugin's tile does: any detect entry, in order.
func TestDetectIntegrationsUsesThePluginDetect(t *testing.T) {
	c, err := plugin.CompileSource("detect.yaml", []byte(demoDetectPlugin))
	if err != nil {
		t.Fatal(err)
	}

	specs := pluginSpecs([]*plugin.Compiled{c})
	if !reflect.DeepEqual(specs[0].Groups, []string{"a.example.com", "b.example.com"}) || specs[0].ID != "demo-app" {
		t.Fatalf("spec %+v", specs[0])
	}

	noServices := `{"items":[]}`
	promService := `{"items":[{"metadata":{"name":"prometheus-operated","namespace":"monitoring"},"spec":{"ports":[{"name":"web","port":9090}]}}]}`

	cases := []struct {
		name, groups, aResources, services string
		podsForbidden                      bool
		hints                              string
		want                               detection
	}{
		{name: "the second group", groups: `{"name":"b.example.com","preferredVersion":{"version":"v2"}}`, services: noServices,
			want: detection{via: detectedByAPI, version: "v2"}},
		{name: "the shared group with its resource", groups: `{"name":"a.example.com","preferredVersion":{"version":"v1"}}`,
			aResources: `{"resources":[{"name":"things"}]}`, services: noServices, want: detection{via: detectedByAPI, version: "v1"}},
		{name: "the shared group without its resource", groups: `{"name":"a.example.com","preferredVersion":{"version":"v1"}}`,
			aResources: `{"resources":[{"name":"others"}]}`, services: noServices},
		{name: "a Prometheus Service", services: promService, want: detection{via: detectedByServices, namespace: "monitoring"}},
		{name: "unlistable pods: the inventory's hint", services: noServices, podsForbidden: true, hints: "demo-app",
			want: detection{via: detectedByInventory}},
		{name: "nothing", services: noServices},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answers := map[string]string{
				"GET /apis":            `{"groups":[` + tc.groups + `]}`,
				"GET /api/v1/pods":     `{"items":[]}`,
				"GET /api/v1/services": tc.services,
			}
			if tc.aResources != "" {
				answers["GET /apis/a.example.com/v1"] = tc.aResources
			}

			f := newFakeKubeAPI(t, answers)
			if tc.podsForbidden {
				f.answerWith("GET /api/v1/pods", http.StatusForbidden, forbiddenStatus)
			}

			k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
			if err != nil {
				t.Fatal(err)
			}

			res, err := detectIntegrations(context.Background(), k, parseHints(tc.hints), specs)
			if err != nil {
				t.Fatal(err)
			}

			got := res.Items[0]
			if got.Detected != (tc.want.via != "") || got.Via != tc.want.via || got.Version != tc.want.version || got.Namespace != tc.want.namespace {
				t.Errorf("%+v, want %+v", got, tc.want)
			}
		})
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
