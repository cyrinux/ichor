package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The native-ref fixture (go/plugin/testdata/plugins/native-ref.yaml): its records come from
// a Go source under a group the cluster does not serve, and its tool runs in Go.

const recordsGroups = `{"groups":[{"name":"records.example.com","preferredVersion":{"version":"v1"}}]}`

// useRecordSource registers native:demo-records, returning objs; scopes records the scope
// each read was given.
func useRecordSource(t *testing.T, objs ...map[string]any) *[]string {
	t.Helper()

	var scopes []string

	saved := pluginNativeSources
	pluginNativeSources = map[string]pluginNativeSource{
		"demo-records": func(_ context.Context, _ *kubeClient, p *pluginProbe, decl plugin.Resource, scope string) ([]map[string]any, error) {
			if p == nil || decl.Kind != "DemoRecord" {
				return nil, errors.New("no probe or wrong resource")
			}

			scopes = append(scopes, scope)

			return objs, nil
		},
	}

	t.Cleanup(func() { pluginNativeSources = saved })

	return &scopes
}

// useCheckTool registers native:demo-check: one card echoing each resource's count.
func useCheckTool(t *testing.T, withDemo bool) {
	t.Helper()

	echo := func(_ context.Context, _ *kubeClient, c *plugin.Compiled, in plugin.Input, locale string) (plugin.ToolResult, error) {
		card := plugin.TreeCard{ID: "counts", Type: "list", Rows: []plugin.TreeRow{}}
		for _, key := range c.Resources {
			card.Rows = append(card.Rows, plugin.TreeRow{Title: key, Trailing: &plugin.TreeTrailing{Text: strconv.Itoa(len(in.Objects[key]))}})
		}

		return plugin.ToolResult{Title: c.Translate(locale, "Check names"), Health: "ok", Cards: []plugin.TreeCard{card}}, nil
	}

	funcs := pluginToolFuncs{run: echo}
	if withDemo {
		funcs.demo = echo
	}

	saved := pluginNativeTools
	pluginNativeTools = map[string]pluginToolFuncs{"demo-check": funcs}

	t.Cleanup(func() { pluginNativeTools = saved })
}

func record(name, namespace, kind, resource string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"spec":     map[string]any{"hostname": name + ".example.com"},
		"status":   map[string]any{"target": "192.0.2.1"},
		"source":   map[string]any{"group": "", "version": "v1", "resource": resource, "kind": kind, "namespace": namespace, "name": name + "-src"},
	}
}

// A native resource of a group the cluster does not serve reads through its Go source, with
// no error entry, and its rows open the objects the records name.
func TestPluginNativeSourceOfUnservedGroup(t *testing.T) {
	usePlugins(t, "native-ref.yaml")
	scopes := useRecordSource(t, record("shop", "demo-shop", "Service", "services"))
	useFakeKube(t, newFakeKubeAPI(t, map[string]string{"GET /apis": recordsGroups}))

	tree := pluginData(t, "cfg", "ctx", "", "demo-records", "demo-shop", "")

	if !tree.Active || len(tree.Errors) > 0 {
		t.Fatalf("active %v errors %+v", tree.Active, tree.Errors)
	}

	if strings.Join(*scopes, ",") != "demo-shop" {
		t.Errorf("source read with scopes %q", *scopes)
	}

	rows := tree.Cards[0].Rows
	want := plugin.Ref{Version: "v1", Resource: "services", Kind: "Service", Namespace: "demo-shop", Name: "shop-src"}

	if len(rows) != 1 || rows[0].Ref == nil || *rows[0].Ref != want {
		t.Fatalf("rows %+v", rows)
	}

	if len(tree.Tools) != 1 || tree.Tools[0].ID != "check" {
		t.Errorf("tools %+v", tree.Tools)
	}
}

// A Go reader that read only part returns its objects with the error: the rows show, the
// error is the resource's entry, and the resource stays readable (part was read).
func TestPluginNativeSourcePartialRead(t *testing.T) {
	usePlugins(t, "native-ref.yaml")

	saved := pluginNativeSources
	pluginNativeSources = map[string]pluginNativeSource{
		"demo-records": func(context.Context, *kubeClient, *pluginProbe, plugin.Resource, string) ([]map[string]any, error) {
			return []map[string]any{record("shop", "demo-shop", "Service", "services")}, errors.New("ingresses refused")
		},
	}

	t.Cleanup(func() { pluginNativeSources = saved })
	useFakeKube(t, newFakeKubeAPI(t, map[string]string{"GET /apis": recordsGroups}))

	tree := pluginData(t, "cfg", "ctx", "", "demo-records", "", "")

	if len(tree.Errors) != 1 || tree.Errors[0].Resource != "demorecords.ichor.dev" || !strings.Contains(tree.Errors[0].Message, "ingresses refused") {
		t.Fatalf("errors %+v", tree.Errors)
	}

	if rows := tree.Cards[0].Rows; len(rows) != 1 || rows[0].Ref == nil || rows[0].Ref.Name != "shop-src" {
		t.Fatalf("rows %+v", rows)
	}

	// Without objects, the error alone: no rows.
	pluginNativeSources = map[string]pluginNativeSource{
		"demo-records": func(context.Context, *kubeClient, *pluginProbe, plugin.Resource, string) ([]map[string]any, error) {
			return nil, errors.New("records refused")
		},
	}

	tree = pluginData(t, "cfg", "ctx", "", "demo-records", "", "")
	if len(tree.Errors) != 1 || len(tree.Cards[0].Rows) != 0 {
		t.Fatalf("errors %+v, rows %+v", tree.Errors, tree.Cards[0].Rows)
	}
}

// The catalog says a native resource is read by Go, not by a list call.
func TestPluginCatalogNativeSource(t *testing.T) {
	usePlugins(t, "native-ref.yaml")

	cat := pluginCatalogOf(knownPlugins(), nil)
	if res := cat.Items[0].Resources; len(res) != 1 || res[0].Source != "native" {
		t.Errorf("resources %+v", res)
	}
}

func TestKubePluginTool(t *testing.T) {
	usePlugins(t, "native-ref.yaml")
	useRecordSource(t, record("shop", "demo-shop", "Service", "services"), record("lab", "demo-lab", "Service", "services"))
	useCheckTool(t, false)
	useFakeKube(t, newFakeKubeAPI(t, map[string]string{"GET /apis": recordsGroups}))

	out, err := KubePluginTool("cfg", "ctx", "", "demo-records", "check", "", "fr")
	if err != nil {
		t.Fatal(err)
	}

	var res plugin.ToolResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Title != "Vérifier les noms" || res.Health != "ok" || len(res.Cards) != 1 {
		t.Fatalf("result %s", out)
	}

	if rows := res.Cards[0].Rows; len(rows) != 1 || rows[0].Title != "record" || rows[0].Trailing.Text != "2" {
		t.Errorf("rows %s", out)
	}

	for _, tt := range []struct{ plugin, tool, want string }{
		{"demo-records", "nothing", `unknown tool "nothing"`},
		{"kube-green", "check", `unknown plugin "kube-green"`},
	} {
		if _, err := KubePluginTool("cfg", "ctx", "", tt.plugin, tt.tool, "", ""); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s %s: %v, want %s", tt.plugin, tt.tool, err, tt.want)
		}
	}

	// A run id no Go function has fails when the tool runs.
	pluginNativeTools = map[string]pluginToolFuncs{}
	if _, err := KubePluginTool("cfg", "ctx", "", "demo-records", "check", "", ""); err == nil || !strings.Contains(err.Error(), `unknown native tool "demo-check"`) {
		t.Errorf("unregistered run: %v", err)
	}
}

// The demo context runs the tool's demo over the samples, or says it is unavailable.
func TestKubePluginToolDemo(t *testing.T) {
	usePlugins(t, "native-ref.yaml")

	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	useCheckTool(t, false)

	if _, err := KubePluginTool(demo, demoKubeContext, "", "demo-records", "check", "", ""); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("no demo: %v", err)
	}

	useCheckTool(t, true)

	out, err := KubePluginTool(demo, demoKubeContext, "", "demo-records", "check", "demo-lab", "")
	if err != nil {
		t.Fatal(err)
	}

	// The samples kept to demo-lab: one record.
	if !strings.Contains(out, `"title":"record"`) || !strings.Contains(out, `"trailing":{"text":"1"}`) {
		t.Errorf("demo result %s", out)
	}
}
