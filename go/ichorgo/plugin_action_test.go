package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The actions fixture (go/plugin/testdata/plugins/actions.yaml) against a fake API server.
const (
	actWidgets = "/apis/actions.example.com/v1/namespaces/demo-shop/widgets"
	actAlpha   = actWidgets + "/alpha"
	actPool    = "/apis/actions.example.com/v1/pools/shared"
	actGroups  = `{"groups":[{"name":"actions.example.com","preferredVersion":{"version":"v1"}}]}`
	actAlphaJS = `{"apiVersion":"actions.example.com/v1","kind":"Widget","metadata":{"name":"alpha","namespace":"demo-shop","uid":"0c6f9a1e-0000-4000-8000-00000000000a","resourceVersion":"812"},"spec":{"size":3,"paused":false}}`
	actBetaJS  = `{"apiVersion":"actions.example.com/v1","kind":"Widget","metadata":{"name":"beta","namespace":"demo-shop"},"spec":{"size":5,"paused":true}}`
)

// actionCluster serves alpha, beta and the shared pool, and accepts every action's request.
func actionCluster(t *testing.T) (*fakeKubeAPI, string) {
	t.Helper()
	usePlugins(t, "actions.yaml")

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":                              actGroups,
		"GET " + actAlpha:                        actAlphaJS,
		"GET " + actWidgets + "/beta":            actBetaJS,
		"GET " + actPool:                         `{"metadata":{"name":"shared"}}`,
		"GET " + actWidgets:                      `{"metadata":{},"items":[` + actAlphaJS + `,` + actBetaJS + `]}`,
		"GET /apis/actions.example.com/v1/pools": `{"metadata":{},"items":[{"metadata":{"name":"shared"}}]}`,
		"PATCH " + actAlpha:                      actAlphaJS,
		"PUT " + actAlpha + "/restart":           `{}`,
		"PATCH " + actAlpha + "/scale":           `{}`,
		"DELETE " + actAlpha:                     `{}`,
		"POST " + actWidgets:                     `{}`,
		"DELETE " + actPool:                      `{}`,
	})

	return f, f.kubeconfigFor(f.URL)
}

// mutation is the request that is not a read.
func mutation(t *testing.T, f *fakeKubeAPI) fakeKubeRequest {
	t.Helper()

	var out []fakeKubeRequest

	for _, r := range f.recorded() {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}

	if len(out) != 1 {
		t.Fatalf("want one mutation, got %+v", out)
	}

	return out[0]
}

// TestKubePluginActionPerVerb: each verb sends its request, after reading the object.
func TestKubePluginActionPerVerb(t *testing.T) {
	now := strconv.FormatInt(time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC).UnixMilli(), 10)

	tests := []struct {
		action, namespace, name, params string
		method, path, contentType, body string
	}{
		{"pause", "demo-shop", "alpha", `{"reason":"maintenance"}`, "PATCH", actAlpha, "application/merge-patch+json",
			`{"metadata":{"resourceVersion":"812"},"spec":{"by":"demo-shop/alpha","paused":true,"reason":"maintenance"}}`},
		{"resize", "demo-shop", "alpha", `{"size":7}`, "PATCH", actAlpha, "application/json-patch+json",
			`[{"op":"test","path":"/metadata/resourceVersion","value":"812"},{"op":"test","path":"/spec/size","value":3},{"op":"replace","path":"/spec/size","value":7}]`},
		{"restart", "demo-shop", "alpha", "", "PUT", actAlpha + "/restart", "application/json", `{}`},
		{"scale", "demo-shop", "alpha", `{"replicas":4}`, "PATCH", actAlpha + "/scale", "application/merge-patch+json", `{"metadata":{"resourceVersion":"812"},"spec":{"replicas":4}}`},
		{"remove", "demo-shop", "alpha", "{}", "DELETE", actAlpha, "application/json",
			`{"apiVersion":"v1","kind":"DeleteOptions","preconditions":{"uid":"0c6f9a1e-0000-4000-8000-00000000000a"},"propagationPolicy":"Foreground"}`},
		{"tag", "demo-shop", "alpha", `{"mode":"slow"}`, "PATCH", actAlpha, "application/merge-patch+json",
			`{"metadata":{"annotations":{"actions.example.com/mode":"slow","actions.example.com/size.v1":"3","actions.example.com/stale":null,"actions.example.com/touched-at":"` + now + `"}}}`},
		{"clone", "demo-shop", "alpha", `{"paused":true}`, "POST", actWidgets, "application/json",
			`{"apiVersion":"actions.example.com/v1","kind":"Widget","metadata":{"labels":{"actions.example.com/copies.v1":"1","actions.example.com/copy-of":"alpha"},"name":"alpha-copy","namespace":"demo-shop"},"spec":{"paused":true,"size":3}}`},
		{"retire", "", "shared", "", "DELETE", actPool, "application/json", `{"apiVersion":"v1","kind":"DeleteOptions"}`},
	}

	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			f, cfg := actionCluster(t)

			if err := KubePluginAction(cfg, "admin@test", "", "demo-actions", tc.action, tc.namespace, tc.name, tc.params); err != nil {
				t.Fatal(err)
			}

			got := mutation(t, f)
			if got.method != tc.method || got.path != tc.path || got.contentType != tc.contentType || got.body != tc.body {
				t.Errorf("got %s %s %s\n%s\nwant %s %s %s\n%s", got.method, got.path, got.contentType, got.body, tc.method, tc.path, tc.contentType, tc.body)
			}

			// The object was read first, at its own path.
			if reads := f.recorded(); reads[len(reads)-2].method != http.MethodGet || !strings.HasSuffix(reads[len(reads)-2].path, "/"+tc.name) {
				t.Errorf("no read before the action: %+v", reads)
			}
		})
	}
}

// TestKubePluginActionEnabledWhen: the object as read now decides, not the app: beta is
// already paused, so pause is refused with its reason and nothing is sent. rebalance reads a
// data service, so the resources are listed: two widgets let it run.
func TestKubePluginActionEnabledWhen(t *testing.T) {
	withDataDir(t)
	SetAuditEventClusters("")

	f, cfg := actionCluster(t)

	err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "pause", "demo-shop", "beta", "")

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusConflict || apiErr.Message != "beta is already paused" {
		t.Fatalf("got %v", err)
	}

	if slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method != http.MethodGet }) {
		t.Errorf("sent anyway: %+v", f.recorded())
	}

	entries := readAudit(t, "", "plugin:demo-actions:pause")
	if len(entries) != 1 || entries[0].Outcome != auditFailed || !strings.Contains(entries[0].Error, "beta is already paused") {
		t.Errorf("audit %+v", entries)
	}

	if err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "rebalance", "demo-shop", "alpha", ""); err != nil {
		t.Fatal(err)
	}

	if got := mutation(t, f); got.path != actAlpha || got.body != `{"metadata":{"resourceVersion":"812"},"spec":{"rebalance":true}}` {
		t.Errorf("rebalance %+v", got)
	}

	if !slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.path == actWidgets }) {
		t.Errorf("rebalance did not list the widgets: %+v", f.recorded())
	}
}

// TestKubePluginActionForbidden: the API server's refusal is the error, not "it may have
// been applied".
func TestKubePluginActionForbidden(t *testing.T) {
	f, cfg := actionCluster(t)
	f.answerWith("PATCH "+actAlpha, http.StatusForbidden, forbiddenStatus)

	err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "pause", "demo-shop", "alpha", "")

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != http.StatusForbidden || strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("got %v", err)
	}
}

// TestKubePluginActionRefusedBeforeTheCluster: the demo, an unknown plugin or action, a bad
// name or namespace, and bad params never reach a cluster. A name or namespace that would
// add a path segment is refused; a parameter that looks like a path only reaches the body.
func TestKubePluginActionRefusedBeforeTheCluster(t *testing.T) {
	usePlugins(t, "actions.yaml")
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)
	if err := KubePluginAction(demo, demoKubeContext, "", "demo-actions", "pause", "demo-shop", "alpha", ""); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}

	tests := []struct {
		plugin, action, namespace, name, params, want string
	}{
		{"kube-green", "pause", "demo-shop", "alpha", "", `unknown plugin "kube-green"`},
		{"demo-actions", "explode", "demo-shop", "alpha", "", `no action "explode"`},
		{"demo-actions", "pause", "demo-shop", "alpha/../../../../api/v1/namespaces/kube-system/secrets/x", "", "invalid Kubernetes name"},
		{"demo-actions", "pause", "demo-shop", "alpha/restart", "", "invalid Kubernetes name"},
		{"demo-actions", "pause", "demo-shop", "..", "", "invalid Kubernetes name"},
		{"demo-actions", "pause", "../kube-system", "alpha", "", "invalid Kubernetes name"},
		{"demo-actions", "pause", "", "alpha", "", "no widget namespace or name"},
		{"demo-actions", "retire", "demo-shop", "shared", "", "cluster-scoped"},
		{"demo-actions", "retire", "", "shared/x", "", "invalid Kubernetes name"},
		{"demo-actions", "resize", "demo-shop", "alpha", "", `parameter "size": is required`},
		{"demo-actions", "resize", "demo-shop", "alpha", `{"size":"big"}`, `parameter "size": want an integer`},
		{"demo-actions", "tag", "demo-shop", "alpha", `{"mode":"turbo"}`, `parameter "mode": want one of fast, slow`},
		{"demo-actions", "pause", "demo-shop", "alpha", `{"path":"/api"}`, `parameter "path": the action declares no such parameter`},
		{"demo-actions", "pause", "demo-shop", "alpha", `["x"]`, "not a JSON object"},
		{"demo-actions", "pause", "demo-shop", "alpha", `{} {}`, "not a JSON object"},
	}

	for _, tc := range tests {
		err := KubePluginAction("cfg", "ctx", "", tc.plugin, tc.action, tc.namespace, tc.name, tc.params)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s %s/%s %s: %v, want %s", tc.plugin, tc.action, tc.namespace, tc.name, tc.params, err, tc.want)
		}
	}
}

// TestKubePluginActionParamStaysInTheBody: a parameter shaped like a path changes the body
// only.
func TestKubePluginActionParamStaysInTheBody(t *testing.T) {
	f, cfg := actionCluster(t)

	reason := `../../../../api/v1/namespaces/kube-system/secrets/x?dryRun=All#`
	params, _ := json.Marshal(map[string]string{"reason": reason})

	if err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "pause", "demo-shop", "alpha", string(params)); err != nil {
		t.Fatal(err)
	}

	got := mutation(t, f)
	if got.path != actAlpha || got.query != "" || !strings.Contains(got.body, `"reason":"../../../../api/v1/namespaces/kube-system/secrets/x?dryRun=All#"`) {
		t.Errorf("got %+v", got)
	}
}

// TestKubePluginActionAudit: the entry names the plugin and action, the object as
// Kind/name, and the params as compact JSON; one readable by the audit screen.
func TestKubePluginActionAudit(t *testing.T) {
	withDataDir(t)
	SetAuditEventClusters("")

	_, cfg := actionCluster(t)

	if err := KubePluginAction(cfg, "admin@test", "", "demo-actions", "scale", "demo-shop", "alpha", ` { "replicas" : 4 } `); err != nil {
		t.Fatal(err)
	}

	entries := readAudit(t, "", "plugin:demo-actions:scale")
	if len(entries) != 1 || entries[0].Action != "plugin:demo-actions:scale" || entries[0].Namespace != "demo-shop" ||
		entries[0].Object != "Widget/alpha" || entries[0].Params != `{"replicas":4}` || entries[0].Outcome != auditOK || entries[0].Cluster != "admin@test" {
		t.Errorf("audit %+v", entries)
	}

	// A refused attempt is recorded too.
	_ = KubePluginAction(cfg, "admin@test", "", "demo-actions", "resize", "demo-shop", "alpha", `{"size":"big"}`) //nolint:errcheck // checked in the entry

	entries = readAudit(t, "", "plugin:demo-actions:resize")
	if len(entries) != 1 || entries[0].Outcome != auditFailed || entries[0].Params != `{"size":"big"}` || !strings.Contains(entries[0].Error, `parameter "size"`) {
		t.Errorf("refused audit %+v", entries)
	}
}

// TestPluginActionChecks: each action becomes a can-I row: its rbac verbs, then the get of
// the object, namespaced as its resource.
func TestPluginActionChecks(t *testing.T) {
	usePlugins(t, "actions.yaml")

	checks := pluginActionChecks(knownPlugins())
	if len(checks) != 9 {
		t.Fatalf("%d rows: %v", len(checks), checks)
	}

	want := map[string][]accessNeed{
		"plugin:demo-actions:scale": {
			{ssarAttributes{Verb: "patch", Group: "actions.example.com", Resource: "widgets", Subresource: "scale"}, true},
			{ssarAttributes{Verb: "get", Group: "actions.example.com", Resource: "widgets"}, true},
		},
		"plugin:demo-actions:retire": {
			{ssarAttributes{Verb: "delete", Group: "actions.example.com", Resource: "pools"}, false},
			{ssarAttributes{Verb: "get", Group: "actions.example.com", Resource: "pools"}, false},
		},
	}

	for key, needs := range want {
		if !slices.Equal(checks[key], needs) {
			t.Errorf("%s: %+v, want %+v", key, checks[key], needs)
		}
	}

	// The built-in catalog's actions are all in kubeActionChecks.
	for key := range pluginActionChecks(plugin.Builtins()) {
		if _, ok := kubeActionChecks[key]; !ok {
			t.Errorf("%s is not a can-I row", key)
		}
	}
}

// TestPluginActionAccessFillsTheTree: KubeActionAccess lists the plugin rows, and a denied
// review makes the render tree's button allowed:false; an allowed one allowed:true.
func TestPluginActionAccessFillsTheTree(t *testing.T) {
	f, cfg := actionCluster(t)

	saved, savedCache := kubeActionChecks, actionAccess
	kubeActionChecks = map[string][]accessNeed{}

	for key, needs := range pluginActionChecks(knownPlugins()) {
		kubeActionChecks[key] = needs
	}

	actionAccess = newAccessCache(accessCacheTTL)

	t.Cleanup(func() { kubeActionChecks, actionAccess = saved, savedCache })

	// Deletes are denied, the rest allowed.
	f.mu.Lock()
	f.answerFunc = func(method, path, body string) (string, bool) {
		switch {
		case method != http.MethodPost || path != ssarPath:
			return "", false
		case strings.Contains(body, `"verb":"delete"`):
			return `{"status":{"allowed":false,"reason":"no deletes here"}}`, true
		default:
			return `{"status":{"allowed":true}}`, true
		}
	}
	f.mu.Unlock()

	out, err := KubeActionAccess(cfg, "admin@test", "", "demo-shop")
	if err != nil {
		t.Fatal(err)
	}

	var access kubeActionAccess
	if err := json.Unmarshal([]byte(out), &access); err != nil {
		t.Fatal(err)
	}

	if a := access.Actions["plugin:demo-actions:remove"]; a.Allowed || a.Reason != "no deletes here" {
		t.Errorf("remove %+v", a)
	}

	if a := access.Actions["plugin:demo-actions:pause"]; !a.Allowed {
		t.Errorf("pause %+v", a)
	}

	tree, err := KubePluginData(cfg, "admin@test", "", "demo-actions", "demo-shop", "")
	if err != nil {
		t.Fatal(err)
	}

	var parsed struct {
		Cards []struct {
			Rows []struct {
				Detail []struct {
					Items []plugin.TreeAction `json:"items"`
				} `json:"detail"`
			} `json:"rows"`
		} `json:"cards"`
	}

	if err := json.Unmarshal([]byte(tree), &parsed); err != nil {
		t.Fatal(err)
	}

	seen := 0

	for _, card := range parsed.Cards {
		for _, row := range card.Rows {
			for _, s := range row.Detail {
				for _, a := range s.Items {
					seen++

					wantAllowed := a.ID != "remove" && a.ID != "retire"
					if a.Allowed == nil || *a.Allowed != wantAllowed {
						t.Errorf("%s allowed %v, want %v", a.ID, a.Allowed, wantAllowed)
					}
				}
			}
		}
	}

	if seen != 2*8+1 {
		t.Errorf("%d buttons in %s", seen, tree)
	}
}
