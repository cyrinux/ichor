package ichorgo

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A model pull (POST /api/pull, streamed) over the readers' fake API server
// (plugin_ollama_test.go): the server ollama in ai behind its Service.

// modelPullRecorder is an OllamaPullListener that keeps what it heard.
type modelPullRecorder struct {
	progress []ollamaPullProgress
	done     chan string
}

func newModelPullRecorder() *modelPullRecorder { return &modelPullRecorder{done: make(chan string, 1)} }

func (r *modelPullRecorder) OnProgress(status string, completed, total int64) {
	r.progress = append(r.progress, ollamaPullProgress{status, completed, total})
}

func (r *modelPullRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *modelPullRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("the pull did not end")

		return ""
	}
}

// pullTwoLayers is a pull of a model of two layers, as Ollama streams it.
const pullTwoLayers = `{"status":"pulling manifest"}
{"status":"pulling c5ad996bda6e","digest":"sha256:c5ad996bda6e","total":397807936}
{"status":"pulling c5ad996bda6e","digest":"sha256:c5ad996bda6e","total":397807936,"completed":100000000}
{"status":"pulling c5ad996bda6e","digest":"sha256:c5ad996bda6e","total":397807936,"completed":397807936}
{"status":"pulling 66b9ea09bd5b","digest":"sha256:66b9ea09bd5b","total":68}
{"status":"pulling 66b9ea09bd5b","digest":"sha256:66b9ea09bd5b","total":68,"completed":68}
{"status":"verifying sha256 digest"}
{"status":"writing manifest"}
{"status":"success"}
`

const pullSum = 397807936 + 68

func TestReadOllamaPull(t *testing.T) {
	still := func() time.Time { return time.Unix(0, 0) }

	cases := []struct {
		name, body string
		wantErr    string
		want       []ollamaPullProgress
	}{
		{"two layers, one clock tick: each step once, the sums over the layers seen", pullTwoLayers, "", []ollamaPullProgress{
			{"pulling manifest", 0, 0},
			{"pulling c5ad996bda6e", 0, 397807936},
			{"pulling 66b9ea09bd5b", 397807936, pullSum},
			{"verifying sha256 digest", pullSum, pullSum},
			{"writing manifest", pullSum, pullSum},
			{"success", pullSum, pullSum},
		}},
		{"Ollama's error", `{"status":"pulling manifest"}` + "\n" + `{"error":"pull model manifest: file does not exist"}` + "\n",
			"pull model manifest: file does not exist", []ollamaPullProgress{{"pulling manifest", 0, 0}}},
		{"cut before success", `{"status":"pulling manifest"}` + "\n", "the pull ended before Ollama reported success",
			[]ollamaPullProgress{{"pulling manifest", 0, 0}}},
		{"not JSON", "<html>bad gateway</html>\n", "decode Ollama pull progress", nil},
		{"a line too long", `{"status":"` + strings.Repeat("x", ollamaPullLineMax) + `"}` + "\n", "longer than", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []ollamaPullProgress

			err := readOllamaPull(strings.NewReader(tc.body), still, func(p ollamaPullProgress) { got = append(got, p) })

			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("err %v, want %q", err, tc.wantErr)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("progress %+v, want %+v", got, tc.want)
			}

			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("progress %d %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Within one step, the bytes are sent at most every ollamaPullEvery.
func TestOllamaPullCoalesces(t *testing.T) {
	var body strings.Builder

	for i := range 100 {
		body.WriteString(`{"status":"pulling aa","digest":"sha256:aa","total":1000,"completed":` + strconv.Itoa(i*10) + "}\n")
	}

	body.WriteString(`{"status":"success"}` + "\n")

	at := time.Unix(0, 0)
	clock := func() time.Time {
		at = at.Add(50 * time.Millisecond)

		return at
	}

	n := 0
	if err := readOllamaPull(strings.NewReader(body.String()), clock, func(ollamaPullProgress) { n++ }); err != nil {
		t.Fatal(err)
	}

	// 100 lines over 5 s: the first, then one per 250 ms, then success.
	if n < 15 || n > 25 {
		t.Errorf("%d callbacks for 100 lines", n)
	}
}

const ollamaPullRoute = "POST " + ollamaSvcProxy + "/api/pull"

// ollamaPullAPI is the actions' fake server answering the pull with code and body, and the
// access review with ssar.
func ollamaPullAPI(t *testing.T, code int, body, ssar string) *fakeKubeAPI {
	t.Helper()
	withDataDir(t)
	SetAuditEventClusters("")

	f := ollamaActionAPI(t, okGenerate("load"))
	f.answerWith(ollamaPullRoute, code, body)
	f.answerWith("POST "+ssarPath, http.StatusCreated, ssar)

	return f
}

func TestStartOllamaPull(t *testing.T) {
	forbidden := `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"services \"ollama:11434\" is forbidden: User \"dev\" cannot create resource \"services/proxy\" in API group \"\" in the namespace \"ai\"","reason":"Forbidden","code":403}`
	denied := `{"status":{"allowed":false,"reason":"no RBAC policy matched"}}`

	cases := []struct {
		name, model, body, ssar string
		code                    int
		wantErr, outcome        string
		sent                    bool
	}{
		{"two layers", "qwen2.5:0.5b", pullTwoLayers, ssarAllowed, http.StatusOK, "", auditOK, true},
		{"Ollama's own error", "qwen2.5:typo", `{"status":"pulling manifest"}` + "\n" + `{"error":"pull model manifest: file does not exist"}` + "\n",
			ssarAllowed, http.StatusOK, "pull model manifest: file does not exist", auditFailed, true},
		{"the API server's 403", "qwen2.5:0.5b", forbidden, ssarAllowed, http.StatusForbidden, "cannot create resource", auditFailed, true},
		{"refused before the call", "qwen2.5:0.5b", "", denied, http.StatusOK, "permission denied: these credentials may not create services/proxy in ai (no RBAC policy matched)", auditFailed, false},
		{"a name Ollama could not pull", "qwen2.5 && rm", "", ssarAllowed, http.StatusOK, "invalid model name", auditFailed, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := ollamaPullAPI(t, tc.code, tc.body, tc.ssar)
			rec := newModelPullRecorder()

			StartOllamaPull(f.kubeconfigFor(f.URL), "admin@test", "", "ai", "ollama", tc.model, rec)

			msg := rec.wait(t)
			if tc.wantErr == "" && msg != "" || tc.wantErr != "" && !strings.Contains(msg, tc.wantErr) {
				t.Fatalf("OnDone %q, want %q", msg, tc.wantErr)
			}

			if strings.Contains(msg, "proxy/api") {
				t.Errorf("the proxy path shows: %q", msg)
			}

			pulls := 0

			for _, r := range f.recorded() {
				if r.method+" "+r.path == ollamaPullRoute {
					pulls++

					if r.body != `{"model":"`+tc.model+`","stream":true}` {
						t.Errorf("body %s", r.body)
					}
				}
			}

			if (pulls == 1) != tc.sent {
				t.Errorf("%d pulls sent", pulls)
			}

			entries := readAudit(t, "", ollamaPullAudit)
			if len(entries) != 1 || entries[0].Outcome != tc.outcome || entries[0].Namespace != "ai" || entries[0].Object != "OllamaModel/"+tc.model {
				t.Errorf("audit %+v", entries)
			}

			if tc.wantErr == "" && (len(rec.progress) == 0 || rec.progress[len(rec.progress)-1] != (ollamaPullProgress{"success", pullSum, pullSum})) {
				t.Errorf("progress %+v", rec.progress)
			}
		})
	}
}

// Cancel during the pull closes the call: OnDone without an error, a partial audit entry.
func TestStartOllamaPullCancel(t *testing.T) {
	f := ollamaPullAPI(t, http.StatusOK, pullTwoLayers, ssarAllowed)

	arrived, release := make(chan struct{}), make(chan struct{})
	f.beforeAnswer = func(r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api/pull") {
			close(arrived)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
	}

	defer close(release)

	rec := newModelPullRecorder()
	run := StartOllamaPull(f.kubeconfigFor(f.URL), "admin@test", "", "ai", "ollama", "qwen2.5:0.5b", rec)

	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the pull was not sent")
	}

	run.Cancel()

	if msg := rec.wait(t); msg != "" {
		t.Errorf("OnDone %q after Cancel", msg)
	}

	entries := readAudit(t, "", ollamaPullAudit)
	if len(entries) != 1 || entries[0].Outcome != auditPartial || !strings.HasPrefix(entries[0].Error, "stopped after") {
		t.Errorf("audit %+v", entries)
	}
}

func TestStartOllamaPullDemo(t *testing.T) {
	noKubeDial(t)
	withDataDir(t)

	rec := newModelPullRecorder()
	StartOllamaPull(demoKubeconfigForTest(t), demoKubeContext, "", "demo-ai", "ollama-gpu", "qwen2.5:0.5b", rec)

	if msg := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo: %q", msg)
	}
}

func TestValidateOllamaModel(t *testing.T) {
	for model, ok := range map[string]bool{
		"qwen2.5:0.5b": true, "llama3.2": true, "hf.co/org/model:q4_k_m": true, "registry.example/lib/m:v1.2-q4": true,
		"": false, "a b": false, "m:t:x": false, "../etc/passwd": false, "m;rm": false, strings.Repeat("a", 129): false,
	} {
		if err := validateOllamaModel(model); (err == nil) != ok {
			t.Errorf("%q: %v", model, err)
		}
	}

	if err := validateOllamaModel("x y"); err == nil || errors.Is(err, errDemoUnavailable) {
		t.Errorf("err %v", err)
	}
}
