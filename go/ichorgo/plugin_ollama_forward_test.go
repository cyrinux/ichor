package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The AI's port-forward to the cluster's Ollama (plugin_ollama_forward.go) over the readers'
// fake API server (plugin_ollama_test.go): lab/ollama-cpu has one ready pod (ollama-cpu-1),
// ai/ollama-standby none. The forward itself is replaced by a "forward" straight to a fake
// Ollama /v1 on the loopback, which StartPortForward's own test covers.

const ollamaForwardPodPath = "GET /api/v1/namespaces/lab/pods/ollama-cpu-1"

const podReady = `{"metadata":{"name":"ollama-cpu-1"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}`

// fakeForwards replaces startForward: every forward "opens" on the address of ollama.
type fakeForwards struct {
	mu      sync.Mutex
	started []string // namespace/pod:port
	stopped int
}

func useFakeForwards(t *testing.T, ollama *httptest.Server) *fakeForwards {
	t.Helper()

	ff := &fakeForwards{}
	address := strings.TrimPrefix(ollama.URL, "http://")

	was := startForward
	startForward = func(_, _, _, namespace, pod string, port int, l PortForwardListener) *PortForwardRun {
		ff.mu.Lock()
		ff.started = append(ff.started, namespace+"/"+pod+":"+strconv.Itoa(port))
		ff.mu.Unlock()

		var once sync.Once

		go l.OnReady(address)

		return &PortForwardRun{cancel: func() {
			once.Do(func() {
				ff.mu.Lock()
				ff.stopped++
				ff.mu.Unlock()
				l.OnDone("")
			})
		}}
	}

	t.Cleanup(func() {
		StopOllamaForward()

		startForward = was
	})

	return ff
}

func (ff *fakeForwards) counts() (started []string, stopped int) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	return slices.Clone(ff.started), ff.stopped
}

// fakeOllamaV1 is Ollama's OpenAI-compatible API: two models, a streamed answer; it records
// the chat requests' model and Authorization header.
func fakeOllamaV1(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()

	var (
		mu    sync.Mutex
		calls []string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[
				{"id":"llama3.2:3b","object":"model","created":100,"owned_by":"library"},
				{"id":"qwen2.5:0.5b","object":"model","created":200,"owned_by":"library"}]}`)
		case "/v1/chat/completions":
			var body struct {
				Model string `json:"model"`
			}

			_ = json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

			mu.Lock()
			calls = append(calls, body.Model+" auth="+r.Header.Get("Authorization"))
			mu.Unlock()

			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, openAIStream("stop", "Restart ", "the pod."))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

// ollamaForwardAPI is the readers' fake server with the namespaced lists the forward reads,
// the forward's pod, and the access review answering ssar.
func ollamaForwardAPI(t *testing.T, ssar string) *fakeKubeAPI {
	t.Helper()

	routes := ollamaRoutes(t)

	var all kubeList[map[string]any]
	if err := json.Unmarshal([]byte(routes["GET /apis/apps/v1/deployments"]), &all); err != nil {
		t.Fatal(err)
	}

	for _, ns := range []string{"ai", "lab"} {
		in := slices.DeleteFunc(slices.Clone(all.Items), func(o map[string]any) bool { return stringAt(mapAt(o, "metadata"), "namespace") != ns })
		raw, _ := json.Marshal(map[string]any{"items": in}) //nolint:errcheck
		routes["GET /apis/apps/v1/namespaces/"+ns+"/deployments"] = string(raw)
		routes["GET /apis/apps/v1/namespaces/"+ns+"/statefulsets"] = `{"items":[]}`
	}

	routes[ollamaForwardPodPath] = podReady

	f := newFakeKubeAPI(t, routes)
	f.answerWith("POST "+ssarPath, http.StatusCreated, ssar)
	useFakeKube(t, f)

	return f
}

func TestKubeOllamaForward(t *testing.T) {
	denied := `{"status":{"allowed":false,"reason":"no RBAC policy matched"}}`

	cases := []struct {
		name, namespace, server, ssar string
		wantErr                       string
		wantPod                       string
	}{
		{"a ready pod", "lab", "ollama-cpu", ssarAllowed, "", "lab/ollama-cpu-1:11434"},
		{"no pod ready", "ai", "ollama-standby", ssarAllowed, "Ollama server ollama-standby has no pod ready", ""},
		{"no such server", "ai", "nope", ssarAllowed, "no Ollama server nope in ai", ""},
		{"another app", "ai", "open-webui", ssarAllowed, "no Ollama server open-webui in ai", ""},
		{"refused by the access review", "lab", "ollama-cpu", denied, "these credentials may not create pods/portforward in lab (no RBAC policy matched)", ""},
		{"a bad name", "lab", "Ollama CPU", ssarAllowed, "invalid", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ollamaForwardAPI(t, tc.ssar)
			ollama, _ := fakeOllamaV1(t)
			ff := useFakeForwards(t, ollama)

			out, err := KubeOllamaForward("cfg", "ctx", "", tc.namespace, tc.server)

			started, _ := ff.counts()

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want %q", err, tc.wantErr)
				}

				if len(started) != 0 {
					t.Errorf("a forward started: %v", started)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var info ollamaForwardInfo
			if err := json.Unmarshal([]byte(out), &info); err != nil {
				t.Fatal(err)
			}

			if info.BaseURL != ollama.URL+"/v1" || info.Pod != "ollama-cpu-1" || info.Server != tc.server || info.Port != 11434 {
				t.Errorf("info %+v", info)
			}

			if !slices.Equal(started, []string{tc.wantPod}) {
				t.Errorf("forwards %v", started)
			}
		})
	}
}

// A forward still running to a ready pod is kept; a pod gone, a forward ended, another server
// or leaving the cluster open a new one or stop it.
func TestKubeOllamaForwardLifetime(t *testing.T) {
	f := ollamaForwardAPI(t, ssarAllowed)
	ollama, _ := fakeOllamaV1(t)
	ff := useFakeForwards(t, ollama)

	ensure := func() {
		t.Helper()

		if _, err := KubeOllamaForward("cfg", "ctx", "", "lab", "ollama-cpu"); err != nil {
			t.Fatal(err)
		}
	}

	ensure()
	ensure()

	if started, stopped := ff.counts(); len(started) != 1 || stopped != 0 {
		t.Fatalf("kept: %d started, %d stopped", len(started), stopped)
	}

	if state, _ := OllamaForwardState(); !strings.Contains(state, `"pod":"ollama-cpu-1"`) {
		t.Errorf("state %q", state)
	}

	// The pod was replaced by a rollout: a new forward, the old one stopped.
	f.answerWith(ollamaForwardPodPath, http.StatusNotFound, `{"kind":"Status","reason":"NotFound","code":404}`)
	ensure()

	if started, stopped := ff.counts(); len(started) != 2 || stopped != 1 {
		t.Fatalf("pod gone: %d started, %d stopped", len(started), stopped)
	}

	f.answerWith(ollamaForwardPodPath, http.StatusOK, podReady)

	// The forward ended on its own (the app was in the background): opened again.
	ollamaForwardMu.Lock()
	ollamaForwardCur.run.Stop()
	ollamaForwardMu.Unlock()

	if state, _ := OllamaForwardState(); state != "" {
		t.Errorf("state after the end %q", state)
	}

	ensure()

	if started, _ := ff.counts(); len(started) != 3 {
		t.Fatalf("ended: %d started", len(started))
	}

	// Leaving another cluster leaves it; leaving its own stops it.
	if err := CancelCalls("cfg", "other"); err != nil {
		t.Fatal(err)
	}

	if state, _ := OllamaForwardState(); state == "" {
		t.Error("stopped by leaving another cluster")
	}

	if err := CancelCalls("cfg", "ctx"); err != nil {
		t.Fatal(err)
	}

	if state, _ := OllamaForwardState(); state != "" {
		t.Errorf("still running after leaving its cluster: %q", state)
	}

	ensure()
	StopOllamaForward()

	if started, stopped := ff.counts(); len(started) != 4 || stopped != 4 {
		t.Fatalf("stop: %d started, %d stopped", len(started), stopped)
	}
}

// Through the forward, the provider lists the server's models and answers, with no key sent;
// with no model chosen the newest of the server's answers, not a cloud default.
func TestOllamaForwardAIRequest(t *testing.T) {
	ollamaForwardAPI(t, ssarAllowed)
	ollama, calls := fakeOllamaV1(t)
	useFakeForwards(t, ollama)

	out, err := KubeOllamaForward("cfg", "ctx", "", "lab", "ollama-cpu")
	if err != nil {
		t.Fatal(err)
	}

	var info ollamaForwardInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}

	models, err := AIModels(aiOllamaCluster, testAPIKey, info.BaseURL)
	if err != nil || models != `[{"id":"qwen2.5:0.5b","name":"qwen2.5:0.5b"},{"id":"llama3.2:3b","name":"llama3.2:3b"}]` {
		t.Fatalf("models %s, %v", models, err)
	}

	for _, tc := range []struct{ model, want string }{
		{"", "qwen2.5:0.5b auth="},
		{"llama3.2:3b", "llama3.2:3b auth="},
	} {
		*calls = nil

		req, err := newAIRequest(aiOllamaCluster, testAPIKey, tc.model, info.BaseURL)
		if err != nil {
			t.Fatal(err)
		}

		req.system, req.user = "be brief", "what is wrong?"

		answer, err := collect(t, req)
		if err != nil || answer != "Restart the pod." {
			t.Fatalf("model %q: answer %q, %v", tc.model, answer, err)
		}

		if !slices.Equal(*calls, []string{tc.want}) {
			t.Errorf("model %q: sent %v", tc.model, *calls)
		}
	}
}

func TestOllamaClusterProviderRequest(t *testing.T) {
	if _, err := newAIRequest(aiOllamaCluster, "", "", ""); err == nil || !strings.Contains(err.Error(), "no Ollama server chosen") {
		t.Errorf("no server: %v", err)
	}

	if _, err := newAIRequest(aiOllamaCluster, testAPIKey, "", "http://ai.example.net/v1"); err == nil || !strings.Contains(err.Error(), "plain http is for a server on your own network") {
		t.Errorf("a remote http server: %v", err)
	}

	req, err := newAIRequest(aiOllamaCluster, testAPIKey, "", "http://127.0.0.1:41234/v1")
	if err != nil || req.apiKey != "" || req.model != "" {
		t.Errorf("req %+v, %v", req, err)
	}

	// The cloud providers keep their defaults.
	if req, err := newAIRequest(aiOpenAI, testAPIKey, "", ""); err != nil || req.model != defaultOpenAIModel {
		t.Errorf("openai %+v, %v", req, err)
	}
}

// A server with no model says so instead of asking for none.
func TestFirstServerModelEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	t.Cleanup(srv.Close)

	req, err := newAIRequest(aiOllamaCluster, "", "", srv.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := collect(t, req); err == nil || !strings.Contains(err.Error(), "has no model yet") {
		t.Errorf("err %v", err)
	}
}

func TestKubeOllamaForwardDemo(t *testing.T) {
	noKubeDial(t)

	if _, err := KubeOllamaForward(demoKubeconfigForTest(t), demoKubeContext, "", "demo-ai", "ollama-gpu"); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Errorf("demo: %v", err)
	}
}
