package ichorgo

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Ollama built-in plugin over a fake API server: three Ollama Deployments (one behind a
// Service with a GPU, one reached through the pod proxy, one with no pod ready) and a
// Deployment of another app; the Ollama API answers with the upstream docs' examples
// (testdata/ollama/), through the Service proxy and the pod proxy.

const (
	ollamaSvcProxy = "/api/v1/namespaces/ai/services/ollama:11434/proxy"
	ollamaPodProxy = "/api/v1/namespaces/lab/pods/ollama-cpu-1:11434/proxy"
)

func ollamaRoutes(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"GET /apis": `{"groups":[]}`,
		"GET /api/v1/pods": podListJSON(t,
			fakePod("ai", "ollama-0", "node-1", true, nil, "ollama", "ollama/ollama:0.12.6"),
			fakePod("kube-system", "coredns-0", "node-1", true, nil, "coredns", "registry.k8s.io/coredns/coredns:v1.11.1")),
		"GET /apis/apps/v1/deployments":  string(readFixture(t, "ollama/deployments.json")),
		"GET /apis/apps/v1/statefulsets": `{"items":[]}`,
		"GET /api/v1/namespaces/ai/services": `{"items":[
			{"metadata":{"name":"open-webui"},"spec":{"selector":{"app":"open-webui"},"ports":[{"name":"http","port":8080}]}},
			{"metadata":{"name":"ollama"},"spec":{"selector":{"app.kubernetes.io/name":"ollama","app.kubernetes.io/instance":"ollama"},"ports":[{"name":"http","port":11434,"targetPort":"http"}]}}]}`,
		"GET /api/v1/namespaces/ai/pods":      `{"items":[{"metadata":{"name":"ollama-standby-0"},"status":{"phase":"Pending"}}]}`,
		"GET /api/v1/namespaces/lab/services": `{"items":[]}`,
		"GET /api/v1/namespaces/lab/pods": `{"items":[
			{"metadata":{"name":"ollama-cpu-1"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},
			{"metadata":{"name":"ollama-cpu-0"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"False"}]}}]}`,
		"GET " + ollamaSvcProxy + "/api/version": `{"version":"0.12.6"}`,
		"GET " + ollamaSvcProxy + "/api/tags":    string(readFixture(t, "ollama/tags.json")),
		"GET " + ollamaSvcProxy + "/api/ps":      string(readFixture(t, "ollama/ps.json")),
		"GET " + ollamaPodProxy + "/api/version": `{"version":"0.11.0"}`,
		"GET " + ollamaPodProxy + "/api/tags":    `{"models":[]}`,
		"GET " + ollamaPodProxy + "/api/ps":      `{"models":[]}`,
	}
}

// ollamaTree renders the plugin, alone and at fixtureNow; refused answers 403 to those
// requests.
func ollamaTree(t *testing.T, routes map[string]string, scope string, refused ...string) (plugin.Tree, *fakeKubeAPI) {
	t.Helper()

	useBuiltinPlugin(t, "ollama", fixtureNow)

	f := newFakeKubeAPI(t, routes)
	for _, r := range refused {
		f.answerWith(r, http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"services \"ollama\" is forbidden"}`)
	}

	useFakeKube(t, f)

	return pluginData(t, "cfg", "ctx", "", "ollama", scope, ""), f
}

func TestOllamaPlugin(t *testing.T) {
	type server struct{ tone, reason, status, trailing, subtitle, text string }

	type model struct{ title, tone, status, subtitle, trailing string }

	cases := []struct {
		name    string
		edit    func(map[string]string)
		refused []string
		summary string
		health  string
		servers map[string]server
		models  []model
		errors  int
	}{
		{
			name:    "service proxy, pod proxy and a server with no pod ready",
			summary: "3 Ollama servers", health: healthCritical,
			servers: map[string]server{
				"ollama":         {tone: healthOK, status: "ready", trailing: "2/2", subtitle: "ai · 0.12.6"},
				"ollama-cpu":     {tone: healthOK, status: "ready", trailing: "0/0", subtitle: "lab · 0.11.0"},
				"ollama-standby": {tone: healthCritical, reason: "notReady", status: "not ready", subtitle: "ai", text: "no Service selects its pods and no pod is ready"},
			},
			// Loaded first (worst, then by name), then on disk. The orphan is pinned: no countdown.
			models: []model{
				{"llama3.2:latest", healthWarning, "partly on CPU", "ollama · 48%/52% CPU/GPU · in 4m", "1.9 GiB"},
				{"mistral:latest", healthWarning, "deleted, still loaded", "ollama · 100% GPU", "4.8 GiB"},
				{"deepseek-r1:latest", healthIdle, "on disk", "ollama", "4.4 GiB"},
			},
		},
		{
			name:    "the Service proxy refused",
			refused: []string{"GET " + ollamaSvcProxy + "/api/version"},
			summary: "3 Ollama servers", health: healthCritical,
			servers: map[string]server{
				"ollama":     {tone: "unknown", reason: "forbidden", status: "no access", subtitle: "ai", text: "the credentials may not read services/proxy"},
				"ollama-cpu": {tone: healthOK, status: "ready", trailing: "0/0"},
			},
			models: []model{},
		},
		{
			name: "the API answers its version only",
			edit: func(r map[string]string) {
				delete(r, "GET "+ollamaSvcProxy+"/api/ps")
			},
			summary: "3 Ollama servers", health: healthCritical,
			servers: map[string]server{
				"ollama": {tone: healthWarning, reason: "partial", status: "ready", trailing: "0/2", text: "Kubernetes API: not found: /api/ps not found"},
			},
			models: []model{
				{"deepseek-r1:latest", healthIdle, "on disk", "ollama", "4.4 GiB"},
				{"llama3.2:latest", healthIdle, "on disk", "ollama", "1.9 GiB"},
			},
		},
		{
			name: "no Service and no pod list: the server says why",
			edit: func(r map[string]string) {
				r["GET /apis/apps/v1/deployments"] = strings.Replace(r["GET /apis/apps/v1/deployments"], `"namespace": "ai"`, `"namespace": "ml"`, 1)
			},
			refused: []string{"GET /api/v1/namespaces/ml/services", "GET /api/v1/namespaces/ml/pods"},
			summary: "3 Ollama servers", health: healthCritical,
			servers: map[string]server{
				"ollama": {tone: "unknown", reason: "forbidden", status: "no access", subtitle: "ml", text: "the credentials may not read services"},
			},
			models: []model{},
		},
		{
			name:    "the Deployments refused: the plugin's error",
			refused: []string{"GET /apis/apps/v1/deployments"},
			summary: "0 Ollama servers", health: healthOK,
			servers: map[string]server{}, models: []model{}, errors: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := ollamaRoutes(t)
			if tc.edit != nil {
				tc.edit(routes)
			}

			tree, _ := ollamaTree(t, routes, "", tc.refused...)

			if !tree.Active || tree.DetectedVia != detectedByPods || len(tree.Errors) != tc.errors {
				t.Fatalf("active %v via %q, errors %+v", tree.Active, tree.DetectedVia, tree.Errors)
			}

			if tree.Summary.Title != tc.summary || tree.Summary.Health != tc.health {
				t.Errorf("summary %+v", tree.Summary)
			}

			rows := rowsByTitle(rookCard(t, tree, "servers").Rows)
			for name, want := range tc.servers {
				r, ok := rows[name]
				if !ok {
					t.Errorf("no server %s", name)

					continue
				}

				text := ""
				for _, s := range r.Detail {
					if s.Type == "text" {
						text = s.Text
					}
				}

				if r.Health == nil || r.Health.Tone != want.tone || r.Health.Reason != want.reason || r.Status.Text != want.status ||
					(want.trailing != "" && r.Trailing.Text != want.trailing) || (want.subtitle != "" && r.Subtitle != want.subtitle) || text != want.text {
					t.Errorf("server %s: health %+v, status %q, trailing %q, subtitle %q, text %q", name, r.Health, r.Status.Text, r.Trailing.Text, r.Subtitle, text)
				}
			}

			var got []model
			for _, r := range rookCard(t, tree, "models").Rows {
				got = append(got, model{r.Title, r.Health.Tone, r.Status.Text, r.Subtitle, r.Trailing.Text})
			}

			if !slices.Equal(got, tc.models) && (len(got) != 0 || len(tc.models) != 0) {
				t.Errorf("models\n got %v\nwant %v", got, tc.models)
			}
		})
	}
}

// The full server row: its totals, its Service, its GPU, the row opening the Deployment; a
// model's details, joined with /api/ps on its digest.
func TestOllamaPluginDetails(t *testing.T) {
	tree, f := ollamaTree(t, ollamaRoutes(t), "")

	srv := rowsByTitle(rookCard(t, tree, "servers").Rows)["ollama"]
	kv := rowItems(srv, "keyValues")
	want := map[string]string{
		"Version": "0.12.6", "Service": "ollama:11434", "GPUs": "1", "Loaded models": "2",
		"Models on disk": "2", "Disk": "6.2 GiB", "VRAM in use": "6.2 GiB",
	}

	for label, v := range want {
		if kv[label]["value"] != v {
			t.Errorf("server %s = %v, want %s", label, kv[label]["value"], v)
		}
	}

	if srv.Ref == nil || *srv.Ref != (plugin.Ref{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespace: "ai", Name: "ollama"}) {
		t.Errorf("server ref %+v", srv.Ref)
	}

	llama := rowsByTitle(rookCard(t, tree, "models").Rows)["llama3.2:latest"]
	kv = rowItems(llama, "keyValues")
	want = map[string]string{
		"Server": "ollama", "Family": "llama", "Parameters": "3.2B", "Quantization": "Q4_K_M", "Format": "gguf",
		"Digest": "a80c4f17acd5", "Processor": "48%/52% CPU/GPU", "Loaded size": "2.8 GiB", "VRAM": "1.5 GiB", "Unloads": "in 4m",
	}

	for label, v := range want {
		if kv[label]["value"] != v {
			t.Errorf("model %s = %v, want %s", label, kv[label]["value"], v)
		}
	}

	// A pinned model (keep_alive < 0) has no countdown.
	mistral := rowsByTitle(rookCard(t, tree, "models").Rows)["mistral:latest"]
	if kv := rowItems(mistral, "keyValues"); kv["Unloads"]["value"] != nil || kv["Modified"]["value"] != nil || kv["VRAM"]["value"] != "4.8 GiB" {
		t.Errorf("pinned orphan %v", kv)
	}

	// No other app's Service is asked, and the pod proxy only for the server without one.
	for _, r := range f.recorded() {
		if strings.Contains(r.path, "open-webui") || strings.Contains(r.path, "/pods/ollama-cpu-0") {
			t.Errorf("asked %s", r.path)
		}
	}
}

// In a namespace scope, only that namespace's workloads are listed.
func TestOllamaPluginScoped(t *testing.T) {
	routes := ollamaRoutes(t)
	routes["GET /apis/apps/v1/namespaces/lab/deployments"] = `{"items":[{"metadata":{"name":"ollama-cpu","namespace":"lab"},"spec":{"selector":{"matchLabels":{"app":"ollama-cpu"}},"template":{"metadata":{"labels":{"app":"ollama-cpu"}},"spec":{"containers":[{"image":"ollama/ollama"}]}}},"status":{"readyReplicas":1}}]}`

	tree, _ := ollamaTree(t, routes, "lab")
	if rows := rookCard(t, tree, "servers").Rows; len(rows) != 1 || rows[0].Title != "ollama-cpu" || rows[0].Health.Tone != healthOK {
		t.Fatalf("servers %+v, errors %+v", rows, tree.Errors)
	}
}

func TestOllamaProcessor(t *testing.T) {
	cases := []struct {
		size, vram int64
		share      int
		text       string
	}{
		{100, 100, 100, "100% GPU"},
		{100, 0, 0, "100% CPU"},
		{3000000000, 1560000000, 52, "48%/52% CPU/GPU"},
		{100, 200, 0, "unknown"},
		{0, 5, 0, "unknown"},
	}

	for _, tc := range cases {
		if share, text := ollamaProcessor(tc.size, tc.vram); share != tc.share || text != tc.text {
			t.Errorf("ollamaProcessor(%d, %d) = %d %q, want %d %q", tc.size, tc.vram, share, text, tc.share, tc.text)
		}
	}
}

func TestOllamaShortDigest(t *testing.T) {
	cases := map[string]string{
		"sha256:a80c4f17acd55265feec403c7aef86be0c25983ab279d83f3bcd3abbcb5b8b72": "a80c4f17acd5",
		"a80c4f17acd55265feec403c7aef86be":                                        "a80c4f17acd5",
		"abc":                                                                     "abc",
		"":                                                                        "",
	}

	for in, want := range cases {
		if got := ollamaShortDigest(in); got != want {
			t.Errorf("ollamaShortDigest(%q) = %q, want %q", in, got, want)
		}
	}
}

// The demo renders every health rule: a server not answering, a spilled model, a model
// deleted while loaded, a pinned one and models on disk.
func TestOllamaPluginDemo(t *testing.T) {
	useBuiltinPlugin(t, "ollama", fixtureNow)

	tree := pluginData(t, demoKubeconfigForTest(t), demoKubeContext, "", "ollama", "", "")
	if !tree.Active || len(tree.Errors) != 0 || tree.Summary.Title != "2 Ollama servers" {
		t.Fatalf("demo %+v, errors %+v", tree.Summary, tree.Errors)
	}

	reasons := map[string]bool{}
	for _, card := range tree.Cards {
		for _, r := range card.Rows {
			if r.Health != nil {
				reasons[r.Health.Reason] = true
			}

			if r.Status.Text == "pinned" {
				reasons["pinned"] = true
			}
		}
	}

	for _, want := range []string{"unreachable", "spilled", "orphan", "onDisk", "pinned"} {
		if !reasons[want] {
			t.Errorf("demo lacks %s: %v", want, reasons)
		}
	}
}
