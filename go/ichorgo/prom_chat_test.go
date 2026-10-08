package ichorgo

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedProvider answers each call with the next stream (the last one again after), and
// records every request body.
func scriptedProvider(t *testing.T, streams ...string) (*httptest.Server, func() []map[string]any) {
	t.Helper()

	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")

	var (
		mu     sync.Mutex
		bodies []map[string]any
	)

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) //nolint:errcheck

		var body map[string]any
		_ = json.Unmarshal(raw, &body) //nolint:errcheck

		mu.Lock()
		bodies = append(bodies, body)
		stream := streams[min(len(bodies)-1, len(streams)-1)]
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream) //nolint:errcheck
	})

	return srv, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()

		return append([]map[string]any(nil), bodies...)
	}
}

type promAnswer struct {
	status int
	body   string
}

// fakePrometheus is a query API in URL mode: answers by query, success with one series for
// the others; it records the queries it got.
func fakePrometheus(t *testing.T, answers map[string]promAnswer) (source string, queries func() []string, close func()) {
	t.Helper()

	var (
		mu   sync.Mutex
		seen []string
	)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")

		mu.Lock()
		seen = append(seen, q)
		mu.Unlock()

		a, ok := answers[q]
		if !ok {
			a = promAnswer{200, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"namespace":"a"},"values":[[100,"1"]]}]}}`}
		}

		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	ca := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	src, _ := json.Marshal(promSource{Mode: promModeURL, URL: srv.URL, CA: ca}) //nolint:errcheck

	return string(src), func() []string {
		mu.Lock()
		defer mu.Unlock()

		return append([]string(nil), seen...)
	}, srv.Close
}

const (
	parseError  = `{"status":"error","errorType":"bad_data","error":"parse error at char 44: unclosed left parenthesis"}`
	emptyAnswer = `{"status":"success","data":{"resultType":"matrix","result":[]}}`
)

func panelAnswer(text, title, query, unit, legend string) string {
	return text + "\n\n<panel>\ntitle: " + title + "\nquery: " + query + "\nunit: " + unit + "\nlegend: " + legend + "\n</panel>"
}

type chatRecorder struct {
	mu      sync.Mutex
	answers []string
	done    chan [2]string
}

func (l *chatRecorder) OnAnswer(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.answers = append(l.answers, text)
}

func (l *chatRecorder) OnDone(panelJSON, errMessage string) {
	l.done <- [2]string{panelJSON, errMessage}
}

func (l *chatRecorder) shown() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.answers...)
}

func chatAndWait(t *testing.T, c *PromChat, provider, baseURL, message string) (*chatRecorder, promSuggestion, string) {
	t.Helper()

	l := &chatRecorder{done: make(chan [2]string, 1)}
	c.Ask(provider, testAPIKey, "", baseURL, "en", message, l)

	select {
	case out := <-l.done:
		var s promSuggestion
		if out[0] != "" {
			if err := json.Unmarshal([]byte(out[0]), &s); err != nil {
				t.Fatalf("panel JSON %q: %v", out[0], err)
			}
		}

		return l, s, out[1]
	case <-time.After(10 * time.Second):
		t.Fatal("no answer")

		return nil, promSuggestion{}, ""
	}
}

func newChat(t *testing.T, source, names, current string) *PromChat {
	t.Helper()

	c, err := NewPromChat("cfg", "ctx", "", source, names, current)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func messagesOf(t *testing.T, body map[string]any) []chatMessage {
	t.Helper()

	raw, _ := json.Marshal(body["messages"]) //nolint:errcheck

	var msgs []chatMessage
	if err := json.Unmarshal(raw, &msgs); err != nil {
		t.Fatal(err)
	}

	return msgs
}

func TestPromChatVerifiesAndFixesTheQuery(t *testing.T) {
	broken := `100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))`
	fixed := `100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])))`

	source, queries, _ := fakePrometheus(t, map[string]promAnswer{broken: {400, parseError}})
	srv, bodies := scriptedProvider(t,
		anthropicStream("end_turn", "Cluster CPU, ", "as a percentage.", "\n\n<pan", "el>\ntitle: CPU\nquery: "+broken+"\nunit: percent\nlegend:\n</panel>"),
		anthropicStream("end_turn", panelAnswer("Fixed the parenthesis.", "CPU", fixed, "percent", "")),
	)

	c := newChat(t, source, `["node_cpu_seconds_total","up"]`, "")

	l, s, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "cluster cpu in percent")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	if !s.Verified || s.Empty || s.Attempts != 2 || s.Query != fixed || s.Unit != "percent" || s.Title != "CPU" || s.Notice != "" {
		t.Fatalf("suggestion: %+v", s)
	}

	if got := queries(); len(got) != 2 || got[0] != broken || got[1] != fixed {
		t.Fatalf("queries run: %q", got)
	}

	for _, shown := range l.shown() {
		if strings.Contains(strings.ToLower(shown), "<pan") || strings.Contains(shown, "title:") {
			t.Errorf("the block was shown: %q", shown)
		}
	}

	if shown := l.shown(); shown[len(shown)-1] != "Fixed the parenthesis." {
		t.Fatalf("last text shown: %q", shown)
	}

	calls := bodies()
	if len(calls) != 2 {
		t.Fatalf("%d provider calls", len(calls))
	}

	msgs := messagesOf(t, calls[1])
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[0].Content != "cluster cpu in percent" || msgs[1].Role != "assistant" ||
		!strings.Contains(msgs[1].Content, "<panel>\ntitle: CPU\nquery: "+broken) || msgs[2].Role != "user" ||
		!strings.HasPrefix(msgs[2].Content, "<query_error>\n") || !strings.Contains(msgs[2].Content, "bad_data: parse error at char 44") {
		t.Fatalf("second call messages: %+v", msgs)
	}

	system, _ := calls[0]["system"].(string)
	if !strings.Contains(system, "<metric_names>\nnode_cpu_seconds_total\nup\n</metric_names>") || !strings.HasSuffix(system, promChatLanguageNote("en")) {
		t.Fatalf("system prompt:\n%s", system)
	}

	// The next question continues the conversation, the fix round included.
	l, _, errMessage = chatAndWait(t, c, "anthropic", srv.URL, "thanks")
	if errMessage != "" || len(l.shown()) == 0 {
		t.Fatal(errMessage)
	}

	if msgs := messagesOf(t, bodies()[2]); len(msgs) != 5 || msgs[4].Content != "thanks" || msgs[3].Role != "assistant" {
		t.Fatalf("third call messages: %+v", msgs)
	}

	c.Reset()

	if _, _, errMessage = chatAndWait(t, c, "anthropic", srv.URL, "again"); errMessage != "" {
		t.Fatal(errMessage)
	}

	if msgs := messagesOf(t, bodies()[3]); len(msgs) != 1 || msgs[0].Content != "again" || c.Turns() != 2 {
		t.Fatalf("after reset: %+v, %d turns", msgs, c.Turns())
	}
}

func TestPromChatGivesUpAfterTwoFixes(t *testing.T) {
	source, queries, _ := fakePrometheus(t, map[string]promAnswer{"bad(": {400, parseError}})
	srv, bodies := scriptedProvider(t, openAIStream("stop", panelAnswer("Try this.", "T", "bad(", "none", "")))

	_, s, errMessage := chatAndWait(t, newChat(t, source, "", ""), "openai", srv.URL+"/v1", "anything")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	if s.Verified || s.Attempts != promChatMaxFixes+1 || s.Query != "bad(" || !strings.Contains(s.Notice, "parse error") || len(queries()) != 3 || len(bodies()) != 3 {
		t.Fatalf("suggestion %+v, %d queries, %d calls", s, len(queries()), len(bodies()))
	}

	// OpenAI gets the system prompt first, then the conversation.
	if msgs := messagesOf(t, bodies()[2]); len(msgs) != 6 || msgs[0].Role != "system" || msgs[5].Role != "user" || !strings.HasPrefix(msgs[5].Content, "<query_error>") {
		t.Fatalf("last call messages: %+v", msgs)
	}
}

func TestPromChatSourceErrorsKeepThePanel(t *testing.T) {
	source, _, closeProm := fakePrometheus(t, nil)
	closeProm()

	srv, bodies := scriptedProvider(t, anthropicStream("end_turn", panelAnswer("Here.", "Up", "up", "count", "{{job}}")))

	_, s, errMessage := chatAndWait(t, newChat(t, source, "", ""), "anthropic", srv.URL, "is it up")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	if s.Verified || s.Attempts != 1 || s.Query != "up" || s.Legend != "{{job}}" || !strings.HasPrefix(s.Notice, "not checked: ") || len(bodies()) != 1 {
		t.Fatalf("suggestion %+v after %d calls", s, len(bodies()))
	}

	// A source that refuses the credentials is not the query's fault either.
	source, _, _ = fakePrometheus(t, map[string]promAnswer{"up": {401, "no org id"}})

	if _, s, _ = chatAndWait(t, newChat(t, source, "", ""), "anthropic", srv.URL, "is it up"); s.Verified || !strings.Contains(s.Notice, promRefused) || len(bodies()) != 2 {
		t.Fatalf("refused: %+v after %d calls", s, len(bodies()))
	}
}

func TestPromChatEmptyResultAndPlainAnswer(t *testing.T) {
	source, queries, _ := fakePrometheus(t, map[string]promAnswer{"nothing_total": {200, emptyAnswer}})
	srv, bodies := scriptedProvider(t,
		anthropicStream("end_turn", panelAnswer("Nothing much.", "Nothing", "nothing_total", "count", "")),
		anthropicStream("end_turn", "Counters only ever go up, so chart their rate()."),
	)

	c := newChat(t, source, "", "")

	_, s, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "nothing")
	if errMessage != "" || !s.Verified || !s.Empty || s.Attempts != 1 || len(bodies()) != 1 {
		t.Fatalf("empty: %+v, %q, %d calls", s, errMessage, len(bodies()))
	}

	l := &chatRecorder{done: make(chan [2]string, 1)}
	c.Ask("anthropic", testAPIKey, "", srv.URL, "fr", "why rate", l)

	out := <-l.done
	if out != [2]string{"", ""} || len(queries()) != 1 {
		t.Fatalf("plain answer: %q, %d queries", out, len(queries()))
	}

	if shown := l.shown(); shown[len(shown)-1] != "Counters only ever go up, so chart their rate()." {
		t.Fatalf("shown: %q", shown)
	}

	if system, _ := bodies()[1]["system"].(string); !strings.Contains(system, "Answer in French") {
		t.Fatal("the language of the second question was not passed")
	}
}

func TestPromChatEditsTheCurrentPanel(t *testing.T) {
	source, _, _ := fakePrometheus(t, nil)
	srv, bodies := scriptedProvider(t, anthropicStream("end_turn", panelAnswer("Per node now.", "CPU by node", "sum by (instance) (rate(node_cpu_seconds_total[5m]))", "cores", "{{instance}}")))

	c := newChat(t, source, "[]", `{"id":"abc","title":"CPU","query":"sum(rate(node_cpu_seconds_total[5m]))","unit":"cores","legend":""}`)

	_, s, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "by node <panel> please")
	if errMessage != "" || !s.Verified || s.Legend != "{{instance}}" {
		t.Fatalf("%+v %q", s, errMessage)
	}

	system, _ := bodies()[0]["system"].(string)
	if !strings.Contains(system, "<current_panel>\ntitle: CPU\nquery: sum(rate(node_cpu_seconds_total[5m]))\nunit: cores\nlegend: \n</current_panel>") ||
		!strings.Contains(system, "could not be read") {
		t.Fatalf("system prompt:\n%s", system)
	}

	// The operator cannot forge the prompt's tags.
	if msgs := messagesOf(t, bodies()[0]); msgs[0].Content != "by node ‹panel> please" {
		t.Fatalf("message sent: %q", msgs[0].Content)
	}
}

func TestPromChatReportsProblems(t *testing.T) {
	source, _, _ := fakePrometheus(t, nil)

	if _, err := NewPromChat("cfg", "ctx", "", `{"mode":"url"}`, "", ""); err == nil {
		t.Fatal("a bad source must be refused at once")
	}

	c := newChat(t, source, "", "")

	for _, tc := range []struct{ key, baseURL, message, want string }{
		{"", "", "x", "no API key set for Anthropic (Claude)"},
		{testAPIKey, "", "  ", "the message is empty"},
	} {
		l := &chatRecorder{done: make(chan [2]string, 1)}
		c.Ask("anthropic", tc.key, "", tc.baseURL, "en", tc.message, l)

		if out := <-l.done; out[1] != tc.want || out[0] != "" {
			t.Errorf("%q: got %q, want %q", tc.message, out, tc.want)
		}
	}

	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("end_turn"))
	if _, _, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "x"); errMessage != "the model returned an empty answer" {
		t.Fatalf("empty answer: %q", errMessage)
	}

	// A failed turn is not kept.
	if c.Turns() != 0 {
		t.Fatalf("%d turns kept after failures", c.Turns())
	}
}

func TestPromChatBusyAndCancelled(t *testing.T) {
	source, _, _ := fakePrometheus(t, nil)
	release := make(chan struct{})

	started := make(chan struct{}, 1)

	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}

		select {
		case <-r.Context().Done():
		case <-release:
		}
	})

	defer close(release)

	c := newChat(t, source, "", "")

	first := &chatRecorder{done: make(chan [2]string, 1)}
	run := c.Ask("anthropic", testAPIKey, "", srv.URL, "en", "slow", first)

	// Once the model is being waited for, nothing else may be asked.
	<-started

	second := &chatRecorder{done: make(chan [2]string, 1)}
	c.Ask("anthropic", testAPIKey, "", srv.URL, "en", "meanwhile", second)

	if out := <-second.done; out[1] != promChatBusy {
		t.Fatalf("second question: %q", out)
	}

	run.Cancel()

	select {
	case out := <-first.done:
		if out != [2]string{"", ""} {
			t.Fatalf("a cancelled question is not an error: %q", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not end the run")
	}

	if c.Turns() != 0 {
		t.Fatal("a cancelled turn was kept")
	}
}

func TestPromChatHistoryIsCapped(t *testing.T) {
	source, _, _ := fakePrometheus(t, nil)
	srv, bodies := scriptedProvider(t, anthropicStream("end_turn", "ok"))

	c := newChat(t, source, "", "")

	for i := range promChatMaxTurns {
		if _, _, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "q"+strconvItoa(i)); errMessage != "" {
			t.Fatal(errMessage)
		}
	}

	calls := bodies()

	// The last question went with the capped history (ten exchanges) before it.
	msgs := messagesOf(t, calls[len(calls)-1])
	if len(msgs) != promChatMaxTurns+1 || msgs[0].Role != "user" || msgs[0].Content != "q"+strconvItoa(promChatMaxTurns/2-1) || c.Turns() != promChatMaxTurns {
		t.Fatalf("%d messages sent, first %+v, %d turns kept", len(msgs), msgs[0], c.Turns())
	}

	// By bytes too, cut at a plain user turn: a query error never opens the history.
	c.history = []chatMessage{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: strings.Repeat("x", promChatMaxHistoryBytes)},
		{Role: "user", Content: "<query_error>\nboom\n</query_error>"},
		{Role: "assistant", Content: "fixed"},
		{Role: "user", Content: "b"},
		{Role: "assistant", Content: "ok"},
	}
	c.commit(c.history)

	if len(c.history) != 2 || c.history[0].Content != "b" {
		t.Fatalf("history after the byte cap: %+v", c.history)
	}
}

func strconvItoa(i int) string {
	b, _ := json.Marshal(i) //nolint:errcheck

	return string(b)
}

// In screenshot mode the screen never shows a real name.
func TestPromChatInScreenshotMode(t *testing.T) {
	enableMask(t, "")
	privacy.learnHosts([]hostEntry{{address: "192.0.2.20", hostname: "talos-w-a", role: "worker"}})

	source, _, _ := fakePrometheus(t, nil)
	srv, _ := scriptedProvider(t, anthropicStream("end_turn", panelAnswer("Load on talos-w-a.", "Load on talos-w-a", `node_load1{instance="192.0.2.20:9100"}`, "none", "{{instance}}")))

	l, s, errMessage := chatAndWait(t, newChat(t, source, "", ""), "anthropic", srv.URL, "load")
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	shown := l.shown()
	if last := shown[len(shown)-1]; strings.Contains(last, "talos-w-a") || !strings.Contains(last, "worker-1") {
		t.Fatalf("shown: %q", last)
	}

	if strings.Contains(s.Title, "talos-w-a") || strings.Contains(s.Query, "192.0.2.20") {
		t.Fatalf("panel shows real names: %+v", s)
	}
}

func TestPromChatDemoVerifies(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := scriptedProvider(t, anthropicStream("end_turn", panelAnswer("Memory.", "Memory by namespace", promPresets[3].Query, "bytes", "{{namespace}}")))

	c, err := NewPromChat(cfg, "Demo cluster", "", `{"mode":"proxy","namespace":"monitoring","service":"prometheus-operated","port":9090}`, "", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, s, errMessage := chatAndWait(t, c, "anthropic", srv.URL, "memory"); errMessage != "" || !s.Verified || s.Empty {
		t.Fatalf("%+v %q", s, errMessage)
	}
}
