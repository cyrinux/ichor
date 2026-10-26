package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeLocalModel stands for the app's engine: it records what it was asked and writes
// pieces, or blocks until cancelled.
type fakeLocalModel struct {
	mu        sync.Mutex
	modelID   string
	options   localModelOptions
	system    string
	messages  []chatMessage
	pieces    []string
	result    string
	block     bool
	cancelled chan struct{}
	started   chan struct{}
}

func (f *fakeLocalModel) Generate(modelID, optionsJSON, system, messagesJSON string, sink LocalModelSink) string {
	f.mu.Lock()
	f.modelID, f.system = modelID, system
	_ = json.Unmarshal([]byte(optionsJSON), &f.options)   //nolint:errcheck // checked by the tests' assertions
	_ = json.Unmarshal([]byte(messagesJSON), &f.messages) //nolint:errcheck // idem
	f.mu.Unlock()

	if f.started != nil {
		close(f.started)
	}

	for _, p := range f.pieces {
		sink.OnPiece(p)
	}

	if f.block {
		<-f.cancelled
	}

	return f.result
}

func (f *fakeLocalModel) Cancel() {
	if f.cancelled != nil {
		select {
		case <-f.cancelled:
		default:
			close(f.cancelled)
		}
	}
}

func useLocalModel(t *testing.T, m LocalModel) {
	t.Helper()
	SetLocalModel(m)
	t.Cleanup(func() { SetLocalModel(nil) })
}

func deviceRequest(t *testing.T) aiRequest {
	t.Helper()

	req, err := newAIRequest("device", "a key that is ignored", "file:mine.gguf", "https://ignored.example")
	if err != nil {
		t.Fatal(err)
	}

	req.system, req.user = "be brief", "what is wrong?"

	return req
}

func TestNewAIRequestDevice(t *testing.T) {
	req := deviceRequest(t)
	if req.apiKey != "" || req.model != "file:mine.gguf" || req.baseURL != "" {
		t.Fatalf("device request: %+v", req)
	}

	// With no catalog model pinned and none imported, there is nothing to ask yet.
	if defaultLocalModel() == "" {
		if _, err := newAIRequest("device", "", "", ""); err == nil || !strings.Contains(err.Error(), "no model on this phone") {
			t.Fatalf("no model: %v", err)
		}
	}
}

func TestStreamDevice(t *testing.T) {
	m := &fakeLocalModel{pieces: []string{"<thi", "nk>\nlet me see", "</th", "ink>\n\nRestart ", "kubelet."}}
	useLocalModel(t, m)

	answer, err := collect(t, deviceRequest(t))
	if err != nil || answer != "Restart kubelet." {
		t.Fatalf("answer %q, err %v", answer, err)
	}

	if m.modelID != "file:mine.gguf" || m.system != "be brief" ||
		len(m.messages) != 1 || m.messages[0] != (chatMessage{Role: "user", Content: "what is wrong?"}) {
		t.Fatalf("asked %q / %q / %+v", m.modelID, m.system, m.messages)
	}

	// An imported file gets the defaults: no prefix, the usual window.
	if m.options != (localModelOptions{ContextTokens: defaultLocalContext, AnswerTokens: defaultLocalAnswer, Temperature: 0.3}) {
		t.Fatalf("options: %+v", m.options)
	}
}

func TestStreamDeviceErrors(t *testing.T) {
	for _, tc := range []struct{ result, want string }{
		{"not-downloaded", "not on this phone yet"},
		{"too-long 9120 8192", "too long for the model on this phone (9120 tokens, it reads 8192)"},
		{"too-long", "too long for the model on this phone:"},
		{"out-of-memory", "not have enough free memory"},
		{"no-engine", deviceUnavailable},
		{"ggml_abort: bad tensor", "the model on this phone failed: ggml_abort: bad tensor"},
	} {
		useLocalModel(t, &fakeLocalModel{result: tc.result})

		if _, err := collect(t, deviceRequest(t)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v, want %q", tc.result, err, tc.want)
		}
	}

	SetLocalModel(nil)

	if _, err := collect(t, deviceRequest(t)); err == nil || err.Error() != deviceUnavailable {
		t.Fatalf("no engine: %v", err)
	}
}

func TestStreamDeviceCancel(t *testing.T) {
	m := &fakeLocalModel{block: true, pieces: []string{"Rest"}, cancelled: make(chan struct{}), started: make(chan struct{})}
	useLocalModel(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)

	go func() { errs <- streamAnswer(ctx, deviceRequest(t), func(string) {}) }()

	<-m.started
	cancel()

	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel never reached the engine")
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	m = &fakeLocalModel{block: true, cancelled: make(chan struct{})}
	useLocalModel(t, m)

	if err := streamAnswer(ctx, deviceRequest(t), func(string) {}); err == nil || err.Error() != deviceTimedOut {
		t.Fatalf("timed out run: %v", err)
	}
}

func TestThinkFilter(t *testing.T) {
	for _, tc := range []struct {
		pieces []string
		want   string
	}{
		{[]string{"plain answer"}, "plain answer"},
		{[]string{"<think>\n\n</think>\n\nAnswer"}, "Answer"},
		{[]string{"<", "think>a</think", ">", "\n", "B"}, "B"},
		{[]string{"a < b and <thin"}, "a < b and <thin"}, // a held-back "<thin" was text
		{[]string{"x <think>never closed"}, "x "},        // reasoning cut off: dropped
		{[]string{"one ", "<think>", "two", "</think>", " three"}, "one  three"},
	} {
		var (
			f   thinkFilter
			out strings.Builder
		)

		for _, p := range tc.pieces {
			out.WriteString(f.feed(p))
		}

		out.WriteString(f.flush())

		if out.String() != tc.want {
			t.Errorf("%q: %q, want %q", tc.pieces, out.String(), tc.want)
		}
	}
}

func TestLocalModelCatalogFile(t *testing.T) {
	all, err := loadLocalModels()
	if err != nil || len(all) == 0 {
		t.Fatalf("catalog: %v", err)
	}

	hexRe := regexp.MustCompile(`^[0-9a-f]+$`)
	ids := map[string]bool{}
	recommended := 0

	for _, m := range all {
		if m.ID == "" || ids[m.ID] || strings.HasPrefix(m.ID, localModelFilePrefix) {
			t.Errorf("bad or repeated id %q", m.ID)
		}

		ids[m.ID] = true

		if m.Recommended {
			recommended++
		}

		if m.Name == "" || m.Repo == "" || !strings.HasSuffix(m.File, ".gguf") || m.License == "" || !strings.HasPrefix(m.LicenseURL, "https://") ||
			m.MinMemoryMiB <= 0 || m.MinMemoryMiBIOS <= 0 || m.ContextTokens < 4096 {
			t.Errorf("%s: incomplete entry %+v", m.ID, m)
		}

		// Pinned all the way or not at all: a half-pinned entry is a mistake in the file.
		some := m.Revision != "" || m.SHA256 != "" || m.Bytes != 0
		if some && (!m.pinned() || !hexRe.MatchString(m.Revision) || !hexRe.MatchString(m.SHA256)) {
			t.Errorf("%s: half-pinned (revision %q, sha256 %q, bytes %d)", m.ID, m.Revision, m.SHA256, m.Bytes)
		}
	}

	if recommended != 1 {
		t.Errorf("%d recommended models, want 1", recommended)
	}

	out, err := LocalModelCatalog()
	if err != nil {
		t.Fatal(err)
	}

	var listed []localModelEntry
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}

	for _, m := range listed {
		if !m.pinned() || m.URL != m.url() {
			t.Errorf("listed unpinned or without URL: %+v", m)
		}
	}
}

func TestLocalModelPins(t *testing.T) {
	m := localModelEntry{Repo: "org/repo", File: "m.gguf", Revision: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64), Bytes: 1}
	if !m.pinned() || m.url() != "https://huggingface.co/org/repo/resolve/"+strings.Repeat("a", 40)+"/m.gguf" {
		t.Fatalf("pinned entry: %v %s", m.pinned(), m.url())
	}

	m.Bytes = 0
	if m.pinned() {
		t.Fatal("an entry without its size counts as pinned")
	}

	if got := localModelList("file:mine.gguf"); len(got) == 0 || got[len(got)-1] != (aiModel{ID: "file:mine.gguf", Name: "mine.gguf"}) {
		t.Fatalf("imported file not listed: %+v", got)
	}

	if _, ok := localModelByID("nope"); ok {
		t.Fatal("unknown id found")
	}
}

func TestAIModelsDevice(t *testing.T) {
	out, err := AIModels("device", "", "")
	if defaultLocalModel() == "" {
		if err == nil {
			t.Fatalf("no model and no error: %s", out)
		}

		return
	}

	if err != nil || !strings.Contains(out, defaultLocalModel()) {
		t.Fatalf("models: %s, %v", out, err)
	}
}

// The model on the phone gets the shorter instructions, the masked report, and its
// answer comes back with the real names, like any provider's.
func TestAskDeviceUsesSmallPromptAndMask(t *testing.T) {
	m := &fakeLocalModel{pieces: []string{"Restart kubelet on wor", "ker-1."}}
	useLocalModel(t, m)

	l := &answerRecorder{done: make(chan string, 1)}
	anonymizedDiagnosis().Ask("device", "", "file:mine.gguf", "", "fr", "talos-w-a flaps", l)

	select {
	case errMessage := <-l.done:
		if errMessage != "" {
			t.Fatal(errMessage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no answer")
	}

	if last := l.answers[len(l.answers)-1]; last != "Restart kubelet on talos-w-a." {
		t.Fatalf("answers: %q", l.answers)
	}

	if !strings.HasPrefix(m.system, diagnosisInstructionsSmall) || !strings.Contains(m.system, "Answer in French") ||
		!strings.Contains(m.system, diagnosisAnonymizedNote) {
		t.Fatalf("system prompt: %s", m.system)
	}

	if sent := m.messages[0].Content; strings.Contains(sent, "talos-w-a") || !strings.Contains(sent, "worker-1 flaps") {
		t.Fatalf("sent: %s", sent)
	}

	health := &Diagnosis{kind: diagnosisHealth}
	if got := health.systemPromptFor("device", "en"); !strings.HasPrefix(got, healthExplainInstructionsSmall) {
		t.Fatalf("health prompt: %s", got)
	}

	if got := health.systemPromptFor("openai", "en"); !strings.HasPrefix(got, healthExplainInstructions) {
		t.Fatalf("health prompt for a cloud model: %s", got)
	}
}

func TestPromChatDeviceListsFewerNames(t *testing.T) {
	names := make([]string, 0, 2000)
	for i := range 2000 {
		names = append(names, "node_metric_"+strings.Repeat("x", i%40)+"_"+time.Duration(i).String())
	}

	raw, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}

	c, err := NewPromChat("cfg", "ctx", "", `{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}`, string(raw), "")
	if err != nil {
		t.Fatal(err)
	}

	if len(c.systemSmall) >= len(c.system) || len(c.systemSmall) > len(promChatInstructions)+promChatDeviceMaxMetricBytes+8<<10 {
		t.Fatalf("small prompt %d bytes, full %d", len(c.systemSmall), len(c.system))
	}
}
