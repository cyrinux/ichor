package ichorgo

import (
	"bytes"
	"encoding/json"
	"flag"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

var updatePluginGolden = flag.Bool("update-plugin-golden", false, "rewrite go/plugin/testdata/render/*-cluster.json")

// usePlugins makes the catalog the fixture plugins of go/plugin/testdata/plugins, at a
// fixed clock.
func usePlugins(t *testing.T, files ...string) {
	t.Helper()

	var compiled []*plugin.Compiled

	for _, name := range files {
		src, err := os.ReadFile(filepath.Join("..", "plugin", "testdata", "plugins", name))
		if err != nil {
			t.Fatal(err)
		}

		c, err := plugin.CompileSource(name, src)
		if err != nil {
			t.Fatal(err)
		}

		compiled = append(compiled, c)
	}

	savedPlugins, savedNow := knownPlugins, pluginNow
	knownPlugins = func() []*plugin.Compiled { return compiled }
	pluginNow = func() time.Time { return time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC) }

	t.Cleanup(func() { knownPlugins, pluginNow = savedPlugins, savedNow })
}

const (
	kubeGreenGroups    = `{"groups":[{"name":"kube-green.com","preferredVersion":{"version":"v1alpha1"}}]}`
	kubeGreenResources = `{"resources":[{"name":"sleepinfos","namespaced":true,"kind":"SleepInfo"}]}`
	sleepInfoNightly   = `{"metadata":{"name":"nightly","namespace":"demo-shop","managedFields":[{"manager":"kubectl"}]},` +
		`"spec":{"weekdays":"1-5","sleepAt":"20:00","wakeUpAt":"08:00","timeZone":"Europe/Paris"},` +
		`"status":{"operation":"WAKE_UP","lastScheduleTime":"2026-10-15T06:00:00Z"}}`
	sleepInfoWeekend = `{"metadata":{"name":"weekend","namespace":"demo-lab"},"spec":{"weekdays":"6,0","sleepAt":"00:00"},` +
		`"status":{"operation":"SLEEP","lastScheduleTime":"2026-10-11T22:00:00Z"}}`
)

// kubeGreenCluster serves kube-green over two pages of SleepInfos; secrets answer
// secretsStatus with secretsBody.
func kubeGreenCluster(t *testing.T, secretsStatus int, secretsBody string) *pagingKubeAPI {
	t.Helper()

	api, _ := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, q url.Values) string {
		// A list scoped to demo-lab answers as the whole list does.
		path = strings.Replace(path, "/namespaces/demo-lab/", "/", 1)

		switch {
		case path == "/apis":
			return kubeGreenGroups
		case path == "/apis/kube-green.com/v1alpha1":
			return kubeGreenResources
		case path == "/apis/kube-green.com/v1alpha1/sleepinfos" && q.Get("continue") == "":
			return `{"metadata":{"continue":"page2"},"items":[` + sleepInfoNightly + `]}`
		case path == "/apis/kube-green.com/v1alpha1/sleepinfos":
			return `{"metadata":{},"items":[` + sleepInfoWeekend + `]}`
		case path == "/api/v1/secrets":
			w.WriteHeader(secretsStatus)

			return secretsBody
		default:
			w.WriteHeader(http.StatusNotFound)

			return `{"kind":"Status","reason":"NotFound","message":"not found","code":404}`
		}
	})
	useFakeKube(t, api.fakeKubeAPI)

	return api
}

// pluginData is KubePluginData's tree.
func pluginData(t *testing.T, config, contextName, kubeServer, id, scope, locale string) plugin.Tree {
	t.Helper()

	out, err := KubePluginData(config, contextName, kubeServer, id, scope, locale)

	return pluginTree(t, out, err)
}

func pluginTree(t *testing.T, out string, err error) plugin.Tree {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}

	var tree plugin.Tree
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return tree
}

func TestKubePluginDataMatchesGolden(t *testing.T) {
	usePlugins(t, "valid.yaml")
	api := kubeGreenCluster(t, http.StatusForbidden, forbiddenStatus)

	out, err := KubePluginData("cfg", "ctx", "", "kube-green", "", "fr")
	pluginTree(t, out, err)

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(out), "", "  "); err != nil {
		t.Fatal(err)
	}

	pretty.WriteByte('\n')

	path := filepath.Join("..", "plugin", "testdata", "render", "kube-green-cluster.json")
	if *updatePluginGolden {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(want, pretty.Bytes()) {
		t.Errorf("%s differs (go test ./ichorgo -run KubePluginDataMatchesGolden -update-plugin-golden); got:\n%s", path, pretty.Bytes())
	}

	// Each list is paged at the declared limit with the declared selectors.
	var sleepPages int

	for _, r := range api.requests() {
		switch r.path {
		case "/apis/kube-green.com/v1alpha1/sleepinfos":
			sleepPages++

			if r.query.Get("limit") != "500" {
				t.Errorf("sleepinfos query %v", r.query)
			}
		case "/api/v1/secrets":
			if r.query.Get("fieldSelector") != "type=Opaque" {
				t.Errorf("secrets query %v", r.query)
			}
		}
	}

	if sleepPages != 2 {
		t.Errorf("read %d pages of sleepinfos, want 2", sleepPages)
	}
}

func TestKubePluginDataForbiddenStillRenders(t *testing.T) {
	usePlugins(t, "valid.yaml")
	kubeGreenCluster(t, http.StatusForbidden, forbiddenStatus)

	tree := pluginData(t, "cfg", "ctx", "", "kube-green", "", "")

	if !tree.Active || tree.DetectedVia != "api" || tree.APIVersion != "v1alpha1" || tree.Summary.Title != "2 sleep schedules" {
		t.Fatalf("tree %+v", tree)
	}

	if len(tree.Errors) != 1 || tree.Errors[0].Resource != "secrets" || !strings.Contains(strings.ToLower(tree.Errors[0].Message), "forbidden") {
		t.Fatalf("errors %+v", tree.Errors)
	}

	// The relation to the unreadable Secret is left out, the rest of the row stays.
	for _, row := range tree.Cards[0].Rows {
		for _, s := range row.Detail {
			if s.Type == "relations" {
				t.Errorf("row %s has relations %+v", row.Title, s)
			}
		}
	}
}

func TestKubePluginDataJoinsAReadableSecret(t *testing.T) {
	usePlugins(t, "valid.yaml")
	kubeGreenCluster(t, http.StatusOK, `{"items":[{"metadata":{"name":"sleepinfo-weekend","namespace":"demo-lab"},"type":"Opaque","data":{"k":"c2VjcmV0"}}]}`)

	tree := pluginData(t, "cfg", "ctx", "", "kube-green", "demo-lab", "")
	if len(tree.Errors) != 0 {
		t.Fatalf("errors %+v", tree.Errors)
	}

	weekend := tree.Cards[0].Rows[0]
	if weekend.Title != "weekend" || weekend.Ref.Namespace != "demo-lab" {
		t.Fatalf("first row %+v", weekend)
	}

	var rel *plugin.Ref

	for _, s := range weekend.Detail {
		if items, ok := s.Items.([]any); ok && s.Type == "relations" {
			ref := items[0].(map[string]any)["ref"].(map[string]any)
			rel = &plugin.Ref{Resource: ref["resource"].(string), Namespace: ref["namespace"].(string), Name: ref["name"].(string)}
		}
	}

	if rel == nil || *rel != (plugin.Ref{Resource: "secrets", Namespace: "demo-lab", Name: "sleepinfo-weekend"}) {
		t.Fatalf("relation %+v", rel)
	}
}

func TestKubePluginDataScopesNamespacedLists(t *testing.T) {
	usePlugins(t, "valid.yaml")
	api := kubeGreenCluster(t, http.StatusForbidden, forbiddenStatus)

	pluginData(t, "cfg", "ctx", "", "kube-green", "demo-lab", "")

	for _, r := range api.requests() {
		if strings.Contains(r.path, "sleepinfos") && r.path != "/apis/kube-green.com/v1alpha1/namespaces/demo-lab/sleepinfos" {
			t.Errorf("unscoped list %s", r.path)
		}
	}
}

func TestKubePluginDataNotInstalled(t *testing.T) {
	usePlugins(t, "valid.yaml")

	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})
	useFakeKube(t, f)

	tree := pluginData(t, "cfg", "ctx", "", "kube-green", "", "")
	if tree.Active || tree.DetectedVia != "" || tree.Summary.Title != "0 sleep schedules" || len(tree.Cards[0].Rows) != 0 {
		t.Fatalf("tree %+v", tree)
	}

	for _, r := range f.recorded() {
		if r.path != "/apis" && r.path != "/version" {
			t.Errorf("listed %s for a plugin that is not installed", r.path)
		}
	}
}

func TestKubePluginDataRefusesBadArguments(t *testing.T) {
	usePlugins(t, "valid.yaml")

	if _, err := KubePluginData("cfg", "ctx", "", "nope", "", ""); err == nil || !strings.Contains(err.Error(), `unknown plugin "nope"`) {
		t.Errorf("unknown plugin: %v", err)
	}

	if _, err := KubePluginData("cfg", "ctx", "", "kube-green", "Bad_NS", ""); err == nil {
		t.Error("bad scope accepted")
	}
}

func TestKubePluginDataDemo(t *testing.T) {
	usePlugins(t, "valid.yaml")
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)

	tree := pluginData(t, demo, demoKubeContext, "", "kube-green", "", "fr")
	if !tree.Active || tree.DetectedVia != "demo" || len(tree.Cards[0].Rows) != 2 || tree.Cards[0].Empty != "Aucun horaire de veille." {
		t.Fatalf("demo tree %+v", tree)
	}

	scoped := pluginData(t, demo, demoKubeContext, "", "kube-green", "demo-shop", "")
	if rows := scoped.Cards[0].Rows; len(rows) != 1 || rows[0].Title != "nightly" {
		t.Fatalf("scoped demo rows %+v", rows)
	}
}

func TestKubePluginDetect(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")
	api := kubeGreenCluster(t, http.StatusForbidden, forbiddenStatus)

	out, err := KubePluginDetect("cfg", "ctx", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var list pluginDetectList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	if len(list.Items) != 2 {
		t.Fatalf("items %+v", list.Items)
	}

	green, widgets := list.Items[0], list.Items[1]
	if green.ID != "kube-green" || !green.Active || green.DetectedVia != "api" || green.Summary == nil ||
		green.Summary.Title != "2 sleep schedules" || green.Summary.Health != "warning" {
		t.Errorf("kube-green %+v", green)
	}

	if widgets.ID != "demo-widgets" || widgets.Active || widgets.Summary != nil || widgets.Category != "workloads" {
		t.Errorf("widgets %+v", widgets)
	}

	apis := 0

	for _, r := range api.requests() {
		if r.path == "/apis" {
			apis++
		}
	}

	if apis != 1 {
		t.Errorf("read /apis %d times, want once", apis)
	}
}

func TestKubePluginDetectDemo(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")
	noKubeDial(t)

	out, err := KubePluginDetect(demoKubeconfigForTest(t), demoKubeContext, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var list pluginDetectList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	for _, item := range list.Items {
		if !item.Active || item.DetectedVia != "demo" || item.Summary == nil {
			t.Errorf("demo item %+v", item)
		}
	}

	if list.Items[1].Summary.Title != "3 widgets" || list.Items[1].Summary.Health != "critical" {
		t.Errorf("widgets summary %+v", list.Items[1].Summary)
	}
}

func TestPluginListStopsAtItsLimit(t *testing.T) {
	usePlugins(t, "widgets.yaml")

	widget := func(name string) string { return `{"metadata":{"name":"` + name + `","namespace":"demo-lab"}}` }
	api, _ := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, q url.Values) string {
		switch {
		case path == "/apis":
			return `{"groups":[{"name":"widgets.example.com","preferredVersion":{"version":"v2"}}]}`
		case path == "/apis/widgets.example.com/v2":
			return `{"resources":[{"name":"widgets"}]}`
		case path == "/apis/widgets.example.com/v2/widgets" && q.Get("continue") == "":
			return `{"metadata":{"continue":"p2"},"items":[` + widget("a") + `,` + widget("b") + `]}`
		case path == "/apis/widgets.example.com/v2/widgets":
			return `{"metadata":{"continue":"p3"},"items":[` + widget("c") + `,` + widget("d") + `]}`
		case path == "/apis/widgets.example.com/v1/gadgets":
			return `{"items":[]}`
		default:
			w.WriteHeader(http.StatusNotFound)

			return `{}`
		}
	})
	useFakeKube(t, api.fakeKubeAPI)

	tree := pluginData(t, "cfg", "ctx", "", "demo-widgets", "", "")

	// limit 3: the second page is cut to one object, the third is never asked for.
	if len(tree.Cards[0].Rows) != 3 || tree.APIVersion != "v2" || tree.Cards[0].Rows[0].Ref.Version != "v2" {
		t.Fatalf("rows %+v", tree.Cards[0].Rows)
	}

	// The fixture's broken field is the second error.
	if len(tree.Errors) != 2 || tree.Errors[0] != (plugin.TreeError{Resource: "widgets.widgets.example.com", Message: "showing the first 3 widgets"}) {
		t.Fatalf("errors %+v", tree.Errors)
	}

	for _, r := range api.requests() {
		if r.query.Get("continue") == "p3" {
			t.Error("read past the limit")
		}

		if strings.HasSuffix(r.path, "/widgets") && r.query.Get("limit") != "3" {
			t.Errorf("widgets page size %v", r.query)
		}
	}
}

func TestPluginSecretsAreMasked(t *testing.T) {
	obj := map[string]any{"metadata": map[string]any{"name": "s", "managedFields": []any{}}, "data": map[string]any{"k": "c2VjcmV0"}}

	tidyPluginObject(obj, plugin.Resource{Resource: "secrets"})

	if obj["data"].(map[string]any)["k"] != "<hidden, 8 characters>" || mapAt(obj, "metadata")["managedFields"] != nil {
		t.Fatalf("not masked %+v", obj)
	}

	revealed := map[string]any{"data": map[string]any{"k": "c2VjcmV0"}}
	tidyPluginObject(revealed, plugin.Resource{Resource: "secrets", Reveal: true})

	if revealed["data"].(map[string]any)["k"] != "c2VjcmV0" {
		t.Fatalf("reveal masked %+v", revealed)
	}
}

func TestCoreHeuristics(t *testing.T) {
	h := coreHeuristics{}

	tests := []struct {
		obj  map[string]any
		want string
	}{
		{map[string]any{}, "ok"},
		{map[string]any{"status": map[string]any{"phase": "Failed"}}, "critical"},
		{map[string]any{"status": map[string]any{"phase": "Pending"}}, "warning"},
		{map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "False"}}}}, "critical"},
		{map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}, "ok"},
	}

	for _, tt := range tests {
		if got, _ := h.ObjectHealth(tt.obj); got != tt.want {
			t.Errorf("ObjectHealth(%v) = %s, want %s", tt.obj, got, tt.want)
		}
	}

	if got := h.ConditionTone(map[string]any{"type": "Degraded", "status": "True"}); got != "critical" {
		t.Errorf("Degraded=True is %s", got)
	}

	if got := h.ConditionTone(map[string]any{"type": "Ready", "status": "Unknown"}); got != "warning" {
		t.Errorf("Ready=Unknown is %s", got)
	}
}
