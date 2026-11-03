package ichorgo

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Ollama plugin's actions (verb native) over the readers' fake API server
// (plugin_ollama_test.go): in ai, the server ollama behind its Service has deepseek-r1 on
// disk, llama3.2 on disk and loaded, and mistral loaded but deleted from disk (an orphan).

// ollamaActionAPI serves the readers' routes, the namespaced lists the action reads, and
// answers the Ollama calls with answer (method, path, body → status, body).
func ollamaActionAPI(t *testing.T, answer func(method, path, body string) (int, string)) *fakeKubeAPI {
	t.Helper()

	useBuiltinPlugin(t, "ollama", fixtureNow)

	routes := ollamaRoutes(t)

	var all kubeList[map[string]any]
	if err := json.Unmarshal([]byte(routes["GET /apis/apps/v1/deployments"]), &all); err != nil {
		t.Fatal(err)
	}

	ai := slices.DeleteFunc(slices.Clone(all.Items), func(o map[string]any) bool { return stringAt(mapAt(o, "metadata"), "namespace") != "ai" })
	raw, _ := json.Marshal(map[string]any{"items": ai})
	routes["GET /apis/apps/v1/namespaces/ai/deployments"] = string(raw)
	routes["GET /apis/apps/v1/namespaces/ai/statefulsets"] = `{"items":[]}`

	f := newFakeKubeAPI(t, routes)

	for _, key := range []string{"POST " + ollamaSvcProxy + "/api/generate", "DELETE " + ollamaSvcProxy + "/api/delete"} {
		method, path, _ := strings.Cut(key, " ")
		code, out := answer(method, path, "")
		f.answerWith(key, code, out)
	}

	useFakeKube(t, f)

	return f
}

// ollamaWrites are the requests that are not reads.
func ollamaWrites(f *fakeKubeAPI) []fakeKubeRequest {
	return slices.DeleteFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method == http.MethodGet })
}

func okGenerate(reason string) func(string, string, string) (int, string) {
	return func(_, _, _ string) (int, string) {
		return http.StatusOK, `{"model":"m","created_at":"2026-11-03T10:00:00Z","response":"","done":true,"done_reason":"` + reason + `"}`
	}
}

func TestOllamaActionsSend(t *testing.T) {
	cases := []struct {
		name, action, model, params string
		answer                      func(string, string, string) (int, string)
		method, path                string
		body                        map[string]any
	}{
		{"load pinned", "load", "deepseek-r1:latest", `{"pin":true}`, okGenerate("load"),
			http.MethodPost, "/api/generate", map[string]any{"model": "deepseek-r1:latest", "stream": false, "keep_alive": -1.0}},
		{"load for the server's keep_alive", "load", "deepseek-r1:latest", "", okGenerate("load"),
			http.MethodPost, "/api/generate", map[string]any{"model": "deepseek-r1:latest", "stream": false}},
		{"unload", "unload", "llama3.2:latest", "", okGenerate("unload"),
			http.MethodPost, "/api/generate", map[string]any{"model": "llama3.2:latest", "stream": false, "keep_alive": 0.0}},
		{"unload, an older Ollama without a reason", "unload", "llama3.2:latest", "", okGenerate(""),
			http.MethodPost, "/api/generate", map[string]any{"model": "llama3.2:latest", "stream": false, "keep_alive": 0.0}},
		{"delete", "delete", "deepseek-r1:latest", "", func(string, string, string) (int, string) { return http.StatusOK, "" },
			http.MethodDelete, "/api/delete", map[string]any{"model": "deepseek-r1:latest"}},
		{"delete of a model already gone", "delete", "llama3.2:latest", "", func(string, string, string) (int, string) {
			return http.StatusNotFound, `{"error":"model 'llama3.2:latest' not found"}`
		}, http.MethodDelete, "/api/delete", map[string]any{"model": "llama3.2:latest"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDataDir(t)
			SetAuditEventClusters("")

			f := ollamaActionAPI(t, tc.answer)

			if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "ollama", tc.action, "ai", tc.model, tc.params); err != nil {
				t.Fatal(err)
			}

			writes := ollamaWrites(f)
			if len(writes) != 1 || writes[0].method != tc.method || writes[0].path != ollamaSvcProxy+tc.path {
				t.Fatalf("writes %+v", writes)
			}

			var body map[string]any
			if err := json.Unmarshal([]byte(writes[0].body), &body); err != nil || !equalJSON(t, body, tc.body) {
				t.Errorf("body %s, want %v (%v)", writes[0].body, tc.body, err)
			}

			entries := readAudit(t, "", "plugin:ollama:"+tc.action)
			if len(entries) != 1 || entries[0].Outcome != auditOK || entries[0].Namespace != "ai" || entries[0].Object != "OllamaModel/"+tc.model {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

func TestOllamaActionsRefuse(t *testing.T) {
	busy := func(string, string, string) (int, string) {
		return http.StatusServiceUnavailable, `{"error":"server busy, please try again.  maximum pending requests exceeded"}`
	}
	tooLarge := func(string, string, string) (int, string) {
		return http.StatusInternalServerError, `{"error":"model requires more system memory (11.2 GiB) than is available (7.6 GiB)"}`
	}
	notUnloaded := okGenerate("stop")
	never := func(string, string, string) (int, string) { return http.StatusOK, "{}" }

	cases := []struct {
		name, action, model string
		answer              func(string, string, string) (int, string)
		code                int
		message             string
		sent                bool
	}{
		{"load of a loaded model", "load", "llama3.2:latest", never, http.StatusConflict, "already loaded", false},
		{"unload of a model on disk only", "unload", "deepseek-r1:latest", never, http.StatusConflict, "not loaded", false},
		{"delete of an orphan", "delete", "mistral:latest", never, http.StatusConflict, "already deleted from disk", false},
		{"a model not listed", "load", "phi3:mini", never, http.StatusNotFound, "OllamaModel ai/phi3:mini was not read", false},
		{"a full queue", "load", "deepseek-r1:latest", busy, http.StatusServiceUnavailable, "Ollama server ollama is busy (its request queue is full): try to load deepseek-r1:latest again later", true},
		{"a model too large, in Ollama's words", "load", "deepseek-r1:latest", tooLarge, http.StatusInternalServerError, "model requires more system memory (11.2 GiB) than is available (7.6 GiB)", true},
		{"an unload Ollama did not do", "unload", "llama3.2:latest", notUnloaded, http.StatusConflict, `Ollama did not unload llama3.2:latest (it answered "stop")`, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDataDir(t)
			SetAuditEventClusters("")

			f := ollamaActionAPI(t, tc.answer)

			err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "ollama", tc.action, "ai", tc.model, "")

			var apiErr *kubeAPIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code || apiErr.Message != tc.message {
				t.Fatalf("got %v (%+v), want %d %q", err, apiErr, tc.code, tc.message)
			}

			if sent := len(ollamaWrites(f)) > 0; sent != tc.sent {
				t.Errorf("sent %v: %+v", sent, ollamaWrites(f))
			}

			if entries := readAudit(t, "", "plugin:ollama:"+tc.action); len(entries) != 1 || entries[0].Outcome != auditFailed {
				t.Errorf("audit %+v", entries)
			}
		})
	}
}

// The API server's refusal of the proxy stays its 403 (the app says the credentials may not).
func TestOllamaActionForbidden(t *testing.T) {
	withDataDir(t)
	SetAuditEventClusters("")

	f := ollamaActionAPI(t, func(string, string, string) (int, string) {
		return http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"services \"ollama:11434\" is forbidden: User \"viewer\" cannot create resource \"services/proxy\""}`
	})

	err := KubePluginAction(f.kubeconfigFor(f.URL), "viewer@test", "", "ollama", "load", "ai", "deepseek-r1:latest", "")
	if !isForbidden(err) || !strings.Contains(err.Error(), "cannot create resource") {
		t.Fatalf("got %v", err)
	}
}

// A name no model can have, or no namespace, is refused before any call.
func TestOllamaActionTargetChecked(t *testing.T) {
	withDataDir(t)
	SetAuditEventClusters("")

	f := ollamaActionAPI(t, okGenerate("load"))

	for _, tc := range []struct{ namespace, name string }{{"ai", "llama 3"}, {"ai", ""}, {"", "llama3.2:latest"}, {"a/b", "llama3.2:latest"}, {"ai", strings.Repeat("x", 254)}} {
		if err := KubePluginAction(f.kubeconfigFor(f.URL), "admin@test", "", "ollama", "load", tc.namespace, tc.name, ""); err == nil {
			t.Errorf("%q/%q accepted", tc.namespace, tc.name)
		}
	}

	if len(f.recorded()) != 0 {
		t.Errorf("called: %+v", f.recorded())
	}
}

// The model rows carry Load, Unload and Delete from disk with the model as their target (the
// row itself opens the Deployment); the can-I rows are the proxy verbs alone.
func TestOllamaActionsInTheTree(t *testing.T) {
	tree, _ := ollamaTree(t, ollamaRoutes(t), "")

	want := map[string]map[string]bool{
		"deepseek-r1:latest": {"load": true, "unload": false, "delete": true},
		"llama3.2:latest":    {"load": false, "unload": true, "delete": true},
		"mistral:latest":     {"load": false, "unload": true, "delete": false},
	}

	seen := 0

	for _, row := range rookCard(t, tree, "models").Rows {
		if row.Ref == nil || row.Ref.Kind != "Deployment" {
			continue
		}

		for _, s := range row.Detail {
			if s.Type != "actions" {
				continue
			}

			raw, _ := json.Marshal(s.Items)

			var buttons []plugin.TreeAction
			if err := json.Unmarshal(raw, &buttons); err != nil {
				t.Fatal(err)
			}

			for _, b := range buttons {
				if b.Target == nil || b.Target.Resource != "ollamamodels" || b.Target.Namespace != row.Ref.Namespace || b.Target.Name != row.Title {
					t.Errorf("%s %s: target %+v", row.Title, b.ID, b.Target)
				}

				if w, ok := want[row.Title]; ok && row.Ref.Namespace == "ai" {
					seen++

					if b.Enabled != w[b.ID] {
						t.Errorf("%s %s: enabled %v", row.Title, b.ID, b.Enabled)
					}
				}
			}
		}
	}

	if seen != 9 {
		t.Errorf("%d buttons checked", seen)
	}

	for action, verb := range map[string]string{"load": "create", "unload": "create", "delete": "delete"} {
		want := []accessNeed{{ssarAttributes{Verb: verb, Group: "", Resource: "services", Subresource: "proxy"}, true}}
		if got := kubeActionChecks["plugin:ollama:"+action]; !slices.Equal(got, want) {
			t.Errorf("%s can-I row %+v, want %+v", action, got, want)
		}
	}
}

// The demo shows the buttons over its samples and runs none.
func TestOllamaActionsDemo(t *testing.T) {
	useBuiltinPlugin(t, "ollama", fixtureNow)
	noKubeDial(t)

	demo := demoKubeconfigForTest(t)
	tree := pluginData(t, demo, demoKubeContext, "", "ollama", "", "")

	if len(rookCard(t, tree, "models").Rows) == 0 {
		t.Fatal("no demo model")
	}

	if err := KubePluginAction(demo, demoKubeContext, "", "ollama", "load", "demo-ai", "gemma3:4b", ""); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo: %v", err)
	}
}

// Every built-in native action names a Go function the core has.
func TestBuiltinNativeActionsKnown(t *testing.T) {
	n := 0

	for _, c := range plugin.Builtins() {
		for _, a := range c.Actions {
			if a.Verb != "native" {
				continue
			}

			n++

			if id, _ := strings.CutPrefix(a.Run, "native:"); pluginNativeActions[id] == nil {
				t.Errorf("%s actions.%s: no native action %q", c.Plugin.ID, a.Name, id)
			}
		}
	}

	if n < 3 {
		t.Errorf("%d native actions", n)
	}
}

// An imported plugin that declares a native action is refused, and kept off when stored.
func TestImportedNativeActionRefused(t *testing.T) {
	withDataDir(t)

	src, err := os.ReadFile("../plugin/testdata/plugins/native-action.yaml")
	if err != nil {
		t.Fatal(err)
	}

	for name, call := range map[string]func(string) (string, error){"check": PluginUserCheck, "import": PluginUserImport} {
		if _, err := call(string(src)); err == nil || !strings.Contains(err.Error(), "actions.evict: verb native runs code built into Ichor: only built-in plugins may use it") {
			t.Errorf("%s: %v", name, err)
		}
	}

	if got := userPluginProblem(src, builtinIDs()); !strings.Contains(got, "only built-in plugins") {
		t.Errorf("stored: %q", got)
	}
}
