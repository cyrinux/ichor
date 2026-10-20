package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The widgets of the demo-widgets fixture: alpha failed (critical), beta is fine.
const (
	widgetsGroups    = `{"groups":[{"name":"kube-green.com","preferredVersion":{"version":"v1alpha1"}},{"name":"widgets.example.com","preferredVersion":{"version":"v1"}}]}`
	widgetsResources = `{"resources":[{"name":"widgets","namespaced":true,"kind":"Widget"},{"name":"gadgets","namespaced":false,"kind":"Gadget"}]}`
	widgetFailed     = `{"metadata":{"name":"alpha","namespace":"demo-shop"},"spec":{"size":"1Gi","replicas":2},"status":{"phase":"Failed","message":"disk full"}}`
	widgetFine       = `{"metadata":{"name":"beta","namespace":"demo-lab"},"spec":{"size":"1Gi","replicas":1},` +
		`"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`
)

// pluginsCluster serves kube-green (weekend asleep with no wake-up time: one finding) and
// demo-widgets, whose widgets answer widgets(w).
func pluginsCluster(t *testing.T, widgets func(w http.ResponseWriter) string) *kubeClient {
	t.Helper()

	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, _ url.Values) string {
		switch path {
		case "/apis":
			return widgetsGroups
		case "/apis/kube-green.com/v1alpha1":
			return kubeGreenResources
		case "/apis/kube-green.com/v1alpha1/sleepinfos":
			return `{"metadata":{},"items":[` + sleepInfoNightly + `,` + sleepInfoWeekend + `]}`
		case "/api/v1/secrets":
			return `{"metadata":{},"items":[]}`
		case "/apis/widgets.example.com/v1":
			return widgetsResources
		case "/apis/widgets.example.com/v1/widgets":
			return widgets(w)
		case "/apis/widgets.example.com/v1/gadgets":
			return `{"metadata":{},"items":[]}`
		default:
			w.WriteHeader(http.StatusNotFound)

			return `{"kind":"Status","reason":"NotFound","message":"not found","code":404}`
		}
	})
	useFakeKube(t, api.fakeKubeAPI)

	return k
}

func forbiddenWidgets(w http.ResponseWriter) string {
	w.WriteHeader(http.StatusForbidden)

	return forbiddenStatus
}

// pluginFindingsOf is KubePluginFindings' report.
func pluginFindingsOf(t *testing.T, config, contextName, locale string) pluginFindingsReport {
	t.Helper()

	out, err := KubePluginFindings(config, contextName, "", "", locale)
	if err != nil {
		t.Fatal(err)
	}

	var r pluginFindingsReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return r
}

func findingKeys(r pluginFindingsReport) []string {
	keys := make([]string, 0, len(r.Findings))
	for _, f := range r.Findings {
		keys = append(keys, f.Severity+" "+f.Key)
	}

	return keys
}

func TestKubePluginFindingsTwoPlugins(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")
	pluginsCluster(t, func(http.ResponseWriter) string {
		return `{"metadata":{},"items":[` + widgetFailed + `,` + widgetFine + `]}`
	})

	r := pluginFindingsOf(t, "cfg", "ctx", "")

	// Worst first, then by key.
	want := []string{"critical plugin:demo-widgets:widget-failed:demo-shop/alpha", "warning plugin:kube-green:asleep-no-wakeup:demo-lab/weekend"}
	if got := findingKeys(r); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("findings %v, want %v", got, want)
	}

	if r.Plugins != 2 || len(r.Errors) != 0 {
		t.Fatalf("plugins %d errors %+v", r.Plugins, r.Errors)
	}

	// No fixture plugin is native to the checkup: the field stays out of the JSON.
	if raw, _ := json.Marshal(r.Findings[0]); strings.Contains(string(raw), "checkupNative") {
		t.Errorf("%s", raw)
	}

	f := r.Findings[0]
	if f.Plugin != "demo-widgets" || f.PluginName != "Demo Widgets" || f.ID != "widget-failed" || f.Title != "alpha failed" ||
		f.Opens != "widgets" || f.Ref == nil || f.Ref.Resource != "widgets" || f.Ref.Namespace != "demo-shop" || !strings.Contains(f.Detail, "disk full") {
		t.Fatalf("finding %+v", f)
	}
}

// A plugin the account may not read is an errors entry; the other plugins' findings stay.
func TestKubePluginFindingsForbiddenPlugin(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")
	pluginsCluster(t, forbiddenWidgets)

	r := pluginFindingsOf(t, "cfg", "ctx", "")

	if got := findingKeys(r); len(got) != 1 || got[0] != "warning plugin:kube-green:asleep-no-wakeup:demo-lab/weekend" {
		t.Fatalf("findings %v", got)
	}

	if len(r.Errors) != 1 || r.Errors[0].Plugin != "demo-widgets" || r.Errors[0].PluginName != "Demo Widgets" ||
		r.Errors[0].Resource != "widgets.widgets.example.com" || r.Errors[0].Message == "" {
		t.Fatalf("errors %+v", r.Errors)
	}
}

// A plugin that does not answer within the budget is an errors entry, and the call returns
// in time with the others' findings.
func TestKubePluginFindingsBudget(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")

	release := make(chan struct{})

	pluginsCluster(t, func(http.ResponseWriter) string {
		<-release

		return `{"metadata":{},"items":[]}`
	})
	t.Cleanup(func() { close(release) }) // before the server closes

	saved := pluginFindingsBudget
	pluginFindingsBudget = 300 * time.Millisecond

	t.Cleanup(func() { pluginFindingsBudget = saved })

	start := time.Now()
	r := pluginFindingsOf(t, "cfg", "ctx", "")

	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("took %s", took)
	}

	if got := findingKeys(r); len(got) != 1 || !strings.Contains(got[0], "kube-green") {
		t.Fatalf("findings %v", got)
	}

	if len(r.Errors) != 1 || r.Errors[0].Plugin != "demo-widgets" {
		t.Fatalf("errors %+v", r.Errors)
	}
}

// The demo reports the samples' findings, in the locale asked.
func TestKubePluginFindingsDemo(t *testing.T) {
	usePlugins(t, "valid.yaml")

	r := pluginFindingsOf(t, demoKubeconfigForTest(t), demoKubeContext, "fr")

	if got := findingKeys(r); len(got) != 1 || got[0] != "warning plugin:kube-green:asleep-no-wakeup:demo-lab/weekend" {
		t.Fatalf("findings %v", got)
	}

	if r.Plugins != 1 || len(r.Errors) != 0 || r.Findings[0].Title != "weekend sleeps with no wake-up time" {
		t.Fatalf("%+v", r)
	}
}

// The built-in plugins' samples all raise findings that the monitor can key.
func TestKubePluginFindingsBuiltinDemo(t *testing.T) {
	r := pluginFindingsOf(t, demoKubeconfigForTest(t), demoKubeContext, "")

	plugins := map[string]bool{}

	for _, f := range r.Findings {
		plugins[f.Plugin] = true

		if !strings.HasPrefix(f.Key, "plugin:"+f.Plugin+":"+f.ID+":") || f.Title == "" || f.PluginName == "" {
			t.Errorf("finding %+v", f)
		}

		// External Secrets is the checkup's secrets section's: the apps skip it when they watch the checkup.
		if f.CheckupNative != (f.Plugin == "external-secrets") {
			t.Errorf("%s: checkupNative %v", f.Key, f.CheckupNative)
		}
	}

	if !plugins["kube-green"] || !plugins["external-secrets"] {
		t.Fatalf("plugins with findings %v", plugins)
	}
}

func TestPluginFindingKey(t *testing.T) {
	cases := []struct {
		ref  *plugin.Ref
		want string
	}{
		{&plugin.Ref{Namespace: "ns", Name: "a"}, "plugin:p:f:ns/a"},
		{&plugin.Ref{Name: "a"}, "plugin:p:f:a"},
		{nil, "plugin:p:f"},
	}

	for _, c := range cases {
		if got := pluginFindingKey("p", plugin.TreeFind{ID: "f", Ref: c.ref}); got != c.want {
			t.Errorf("%+v: %q, want %q", c.ref, got, c.want)
		}
	}
}

// The same finding raised twice keeps one entry, the worst.
func TestFindingsReportDedupes(t *testing.T) {
	c := &plugin.Compiled{Plugin: &plugin.Plugin{ID: "p", Name: "P"}}
	ref := &plugin.Ref{Namespace: "ns", Name: "a"}
	runs := []pluginRun{{c: c, active: true, findings: []plugin.TreeFind{
		{ID: "f", Severity: "warning", Ref: ref}, {ID: "f", Severity: "critical", Ref: ref}, {ID: "g", Severity: "bogus", Ref: ref},
	}}}

	r := findingsReport(runs)
	if got := findingKeys(r); strings.Join(got, ",") != "critical plugin:p:f:ns/a,warning plugin:p:g:ns/a" {
		t.Fatalf("%v", got)
	}
}

// The checkup's plugins section: the findings as kind "plugin" with the plugin's words, a
// plugin it could not read in its error, the native plugins left out.
func TestCheckupPluginsSection(t *testing.T) {
	usePlugins(t, "valid.yaml", "widgets.yaml")
	k := pluginsCluster(t, forbiddenWidgets)

	groups, err := readAPIGroups(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	in := checkupInput{now: checkupNow, groups: groups}

	s := checkupPlugins(context.Background(), k, in, "")
	if s.ID != checkPlugins || s.Status != healthWarning || len(s.Findings) != 1 || s.Checked != 2 ||
		!strings.Contains(s.Error, "Demo Widgets: widgets.widgets.example.com: ") {
		t.Fatalf("section %+v", s)
	}

	f := s.Findings[0]
	if f.Kind != findPluginFinding || f.Severity != sevWarning || f.Namespace != "demo-lab" || f.Name != "weekend" ||
		f.Plugin != "kube-green" || f.Key != "plugin:kube-green:asleep-no-wakeup:demo-lab/weekend" {
		t.Fatalf("finding %+v", f)
	}

	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{`"kind":"plugin"`, `"plugin":"kube-green"`, `"pluginName":"kube-green"`, `"id":"asleep-no-wakeup"`, `"opens":"list"`, `"ref":{`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("%s lacks %s", raw, field)
		}
	}

	// The other kinds carry none of the plugin's fields.
	if other, _ := json.Marshal(checkupFinding{Kind: findPodCrashLoop, Severity: sevCritical, Name: "a"}); strings.Contains(string(other), "plugin") || strings.Contains(string(other), "key") {
		t.Errorf("%s", other)
	}

	// A plugin a section reports natively is left out.
	saved := checkupNativePlugins
	checkupNativePlugins = map[string]bool{"demo-widgets": true}

	t.Cleanup(func() { checkupNativePlugins = saved })

	if s := checkupPlugins(context.Background(), k, in, ""); s.Error != "" || len(s.Findings) != 1 {
		t.Fatalf("native left in: %+v", s)
	}
}

// The demo checkup's plugins section is the built-in plugins' samples, External Secrets left
// to the secrets section.
func TestDemoCheckupPlugins(t *testing.T) {
	s := sectionByID(t, demoCheckup(checkupNow, "fr"), checkPlugins)

	if len(s.Findings) == 0 || s.Status == checkAbsent || s.Error != "" {
		t.Fatalf("%+v", s)
	}

	for _, f := range s.Findings {
		if f.Kind != findPluginFinding || f.Plugin == "external-secrets" || f.PluginName == "" || f.Title == "" {
			t.Errorf("finding %+v", f)
		}
	}
}
