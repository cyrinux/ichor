package talosmobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAPIKey = "sk-test-0123456789"

// recordedCall is what a fake provider saw.
type recordedCall struct {
	count  int
	path   string
	header http.Header
	body   map[string]any
}

// fakeProvider serves status and response for every request and records the last one.
func fakeProvider(t *testing.T, status int, contentType, response string) (*httptest.Server, *recordedCall) {
	t.Helper()

	call := &recordedCall{}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) //nolint:errcheck

		call.count++
		call.path, call.header, call.body = r.URL.Path, r.Header.Clone(), nil
		_ = json.Unmarshal(raw, &call.body) //nolint:errcheck

		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, response) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	// Keys are only sent over https, so the fakes speak TLS with their own certificate.
	was := aiHTTPClient
	aiHTTPClient = srv.Client()
	aiHTTPClient.CheckRedirect = refuseRedirect

	t.Cleanup(func() { aiHTTPClient = was })

	return srv, call
}

// A redirect could carry the key to another host: it is refused, not followed.
func TestRedirectIsNotFollowed(t *testing.T) {
	target, targetCall := fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("end_turn", "stolen"))
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")
	srv.Config.Handler = http.RedirectHandler(target.URL+"/v1/messages", http.StatusTemporaryRedirect)

	answer, err := collect(t, mustRequest(t, "anthropic", "", srv.URL))
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") || answer != "" || targetCall.count != 0 {
		t.Fatalf("answer %q, err %v, redirect target called %d times", answer, err, targetCall.count)
	}
}

// asProviderAPI makes srv stand for both providers' own APIs (no base URL in the settings).
func asProviderAPI(t *testing.T, srv *httptest.Server) {
	t.Helper()

	anthropicWas, openAIWas := anthropicAPIURL, openAIAPIURL
	anthropicAPIURL, openAIAPIURL = srv.URL, srv.URL+"/v1"

	t.Cleanup(func() { anthropicAPIURL, openAIAPIURL = anthropicWas, openAIWas })
}

// noRetryDelay makes the single retry of a busy provider immediate.
func noRetryDelay(t *testing.T) {
	t.Helper()

	was := aiRetryDelay
	aiRetryDelay = 0

	t.Cleanup(func() { aiRetryDelay = was })
}

func sse(events ...string) string {
	var b strings.Builder

	for _, e := range events {
		var typed struct {
			Type string `json:"type"`
		}

		if json.Unmarshal([]byte(e), &typed) == nil && typed.Type != "" {
			fmt.Fprintf(&b, "event: %s\n", typed.Type)
		}

		fmt.Fprintf(&b, "data: %s\n\n", e)
	}

	return b.String()
}

func anthropicStream(stopReason string, texts ...string) string {
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1}}}`,
		// A fallback marker and thinking are not part of the answer.
		`{"type":"content_block_start","index":0,"content_block":{"type":"fallback","from":{"model":"claude-opus-5-5"},"to":{"model":"claude-opus-4-8"}}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"hidden"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`,
	}

	for _, text := range texts {
		delta, _ := json.Marshal(text) //nolint:errcheck
		events = append(events, `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":`+string(delta)+`}}`)
	}

	events = append(events,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"`+stopReason+`","stop_sequence":null},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	)

	return sse(events...)
}

func openAIStream(finishReason string, texts ...string) string {
	var events []string

	for _, text := range texts {
		delta, _ := json.Marshal(text) //nolint:errcheck
		events = append(events, `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{"content":`+string(delta)+`},"finish_reason":null}]}`)
	}

	events = append(events,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5","choices":[{"index":0,"delta":{},"finish_reason":"`+finishReason+`"}]}`,
		`[DONE]`,
	)

	return sse(events...)
}

func collect(t *testing.T, req aiRequest) (string, error) {
	t.Helper()

	var answer strings.Builder

	err := streamAnswer(context.Background(), req, func(s string) { answer.WriteString(s) })

	return answer.String(), err
}

func mustRequest(t *testing.T, provider, model, baseURL string) aiRequest {
	t.Helper()

	req, err := newAIRequest(provider, testAPIKey, model, baseURL)
	if err != nil {
		t.Fatal(err)
	}

	req.system, req.user = "be brief", "what is wrong?"

	return req
}

func TestNewAIRequest(t *testing.T) {
	req, err := newAIRequest("anthropic", " "+testAPIKey+"\n", " ", "")
	if err != nil || req.model != defaultAnthropicModel || req.apiKey != testAPIKey || req.baseURL != "" {
		t.Fatalf("defaults: %+v, %v", req, err)
	}

	if req, err = newAIRequest("openai", "", "llama3", "http://192.0.2.9:11434/v1"); err != nil ||
		req.model != "llama3" || req.baseURL != "http://192.0.2.9:11434/v1" {
		t.Fatalf("local server without a key: %+v, %v", req, err)
	}

	for _, tc := range []struct{ provider, key, baseURL, want string }{
		{"gemini", testAPIKey, "", "unknown AI provider"},
		{"openai", "", "", "no API key set for OpenAI"},
		{"anthropic", testAPIKey, "ftp://host", "base URL"},
		{"anthropic", testAPIKey, "not a url", "base URL"},
		{"openai", testAPIKey, "http://192.0.2.9:11434/v1", "only sent over https"},
	} {
		if _, err := newAIRequest(tc.provider, tc.key, "", tc.baseURL); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("newAIRequest(%q, key=%q, url=%q) = %v, want %q", tc.provider, tc.key, tc.baseURL, err, tc.want)
		}
	}
}

func TestAIProvidersListsDefaults(t *testing.T) {
	out, err := AIProviders()
	if err != nil {
		t.Fatal(err)
	}

	var got []aiProvider
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 {
		t.Fatalf("providers: %s, %v", out, err)
	}

	if got[0].ID != "anthropic" || got[0].DefaultModel != "claude-opus-5-5" || got[1].ID != "openai" || got[1].KeyURL == "" {
		t.Fatalf("unexpected providers: %+v", got)
	}
}

func TestStreamAnthropic(t *testing.T) {
	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("end_turn", "Restart ", "kubelet."))
	asProviderAPI(t, srv)

	answer, err := collect(t, mustRequest(t, "anthropic", "", ""))
	if err != nil || answer != "Restart kubelet." {
		t.Fatalf("answer %q, err %v", answer, err)
	}

	if call.path != "/v1/messages" || call.header.Get("X-Api-Key") != testAPIKey {
		t.Fatalf("path %q, key header %q", call.path, call.header.Get("X-Api-Key"))
	}

	want := map[string]any{
		"model":         "claude-opus-5-5",
		"max_tokens":    float64(anthropicMaxTokens),
		"stream":        true,
		"system":        "be brief",
		"messages":      []any{map[string]any{"role": "user", "content": "what is wrong?"}},
		"output_config": map[string]any{"effort": "high"},
		"fallbacks":     "default",
	}
	if !equalJSON(t, call.body, want) {
		t.Fatal("unexpected request body")
	}

	if beta := call.header.Get("Anthropic-Beta"); beta != anthropicFallbackBeta || call.header.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatalf("beta header %q, version %q", beta, call.header.Get("Anthropic-Version"))
	}
}

// A model that is not known to take them, or another server, gets the plain request.
func TestStreamAnthropicPlainRequest(t *testing.T) {
	for _, tc := range []struct{ name, model string }{
		{"other model", "claude-haiku-4-5"},
		{"custom server", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("end_turn", "ok"))

			baseURL := srv.URL
			if tc.model != "" {
				asProviderAPI(t, srv)

				baseURL = ""
			}

			if _, err := collect(t, mustRequest(t, "anthropic", tc.model, baseURL)); err != nil {
				t.Fatal(err)
			}

			if _, ok := call.body["fallbacks"]; ok || call.header.Get("Anthropic-Beta") != "" {
				t.Fatalf("fallback sent: body %v, beta %q", call.body["fallbacks"], call.header.Get("Anthropic-Beta"))
			}

			if _, ok := call.body["output_config"]; ok != (tc.model == "") {
				t.Fatalf("output_config present = %v for model %q", ok, call.body["model"])
			}

			if _, ok := call.body["thinking"]; ok {
				t.Fatal("thinking must be left to the model's default")
			}
		})
	}
}

func TestStreamAnthropicStopReasons(t *testing.T) {
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("max_tokens", "Step 1"))

	answer, err := collect(t, mustRequest(t, "anthropic", "", srv.URL))
	if err != nil || answer != "Step 1"+answerCutNotice {
		t.Fatalf("max_tokens: answer %q, err %v", answer, err)
	}

	srv, _ = fakeProvider(t, http.StatusOK, "text/event-stream", anthropicStream("refusal"))

	if _, err := collect(t, mustRequest(t, "anthropic", "", srv.URL)); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("refusal: err %v", err)
	}
}

func TestStreamOpenAI(t *testing.T) {
	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", openAIStream("stop", "Free ", "disk space."))

	answer, err := collect(t, mustRequest(t, "openai", "", srv.URL+"/v1"))
	if err != nil || answer != "Free disk space." {
		t.Fatalf("answer %q, err %v", answer, err)
	}

	if call.path != "/v1/chat/completions" || call.header.Get("Authorization") != "Bearer "+testAPIKey {
		t.Fatalf("path %q, authorization %q", call.path, call.header.Get("Authorization"))
	}

	want := map[string]any{
		"model":  defaultOpenAIModel,
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": "be brief"},
			map[string]any{"role": "user", "content": "what is wrong?"},
		},
	}
	if !equalJSON(t, call.body, want) {
		t.Fatal("unexpected request body")
	}
}

func TestStreamOpenAIFinishReasons(t *testing.T) {
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", openAIStream("length", "Step 1"))

	answer, err := collect(t, mustRequest(t, "openai", "", srv.URL))
	if err != nil || answer != "Step 1"+answerCutNotice {
		t.Fatalf("length: answer %q, err %v", answer, err)
	}

	srv, _ = fakeProvider(t, http.StatusOK, "text/event-stream", openAIStream("content_filter"))

	if _, err := collect(t, mustRequest(t, "openai", "", srv.URL)); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("content filter: err %v", err)
	}
}

func TestAIErrorsAreShortAndKeepTheKeyOut(t *testing.T) {
	for _, tc := range []struct {
		provider string
		status   int
		body     string
		want     string
	}{
		{"anthropic", 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key ` + testAPIKey + `"}}`, "Anthropic (Claude) rejected the API key"},
		{"openai", 401, `{"error":{"message":"Incorrect API key provided: ` + testAPIKey + `","type":"invalid_request_error","code":"invalid_api_key"}}`, "OpenAI rejected the API key"},
		{"anthropic", 404, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-x"}}`, "Anthropic (Claude): model not found (check the model name): model: claude-x"},
		{"openai", 400, `{"error":{"message":"Unsupported parameter","type":"invalid_request_error"}}`, "OpenAI: the request was refused (HTTP 400): Unsupported parameter"},
		// A gateway echoing the credential in another error than 401.
		{"openai", 403, `{"error":{"message":"key ` + testAPIKey + ` has no access"}}`, "OpenAI: the API key may not use this model: key [API key] has no access"},
	} {
		srv, _ := fakeProvider(t, tc.status, "application/json", tc.body)

		_, err := collect(t, mustRequest(t, tc.provider, "", srv.URL))
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s %d: got %v, want %q", tc.provider, tc.status, err, tc.want)
		}

		if err != nil && strings.Contains(err.Error(), testAPIKey) {
			t.Errorf("%s %d: the error quotes the API key", tc.provider, tc.status)
		}
	}
}

func TestStreamEndsEarly(t *testing.T) {
	// The connection drops before the model says it is done.
	cut := sse(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Step 1"}}`)
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", cut)

	if answer, err := collect(t, mustRequest(t, "anthropic", "", srv.URL)); err == nil || err.Error() != answerIncomplete || answer != "Step 1" {
		t.Fatalf("anthropic cut: answer %q, err %v", answer, err)
	}

	srv, _ = fakeProvider(t, http.StatusOK, "text/event-stream", sse(`{"choices":[{"index":0,"delta":{"content":"Step 1"},"finish_reason":null}]}`))

	if _, err := collect(t, mustRequest(t, "openai", "", srv.URL)); err == nil || err.Error() != answerIncomplete {
		t.Fatalf("openai cut: err %v", err)
	}

	// The provider reports a failure inside the stream.
	srv, _ = fakeProvider(t, http.StatusOK, "text/event-stream", sse(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))

	_, err := collect(t, mustRequest(t, "anthropic", "", srv.URL))
	if want := "Anthropic (Claude): the service is overloaded or unavailable, try again later: Overloaded"; err == nil || err.Error() != want {
		t.Fatalf("stream error: %v", err)
	}
}

func TestBusyProviderIsRetriedOnce(t *testing.T) {
	noRetryDelay(t)

	srv, call := fakeProvider(t, http.StatusOK, "text/event-stream", "")
	answers := []struct {
		status int
		body   string
	}{{529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`}, {200, anthropicStream("end_turn", "ok")}}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := answers[min(call.count, len(answers)-1)]
		call.count++

		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body) //nolint:errcheck
	})

	if answer, err := collect(t, mustRequest(t, "anthropic", "", srv.URL)); err != nil || answer != "ok" || call.count != 2 {
		t.Fatalf("answer %q, err %v after %d calls", answer, err, call.count)
	}

	// Still busy on the second try: reported. A refused request is never retried.
	answers, call.count = answers[:1], 0

	if _, err := collect(t, mustRequest(t, "anthropic", "", srv.URL)); err == nil || !strings.Contains(err.Error(), "overloaded") || call.count != 2 {
		t.Fatalf("err %v after %d calls", err, call.count)
	}

	answers[0].status, call.count = 400, 0

	if _, err := collect(t, mustRequest(t, "anthropic", "", srv.URL)); err == nil || call.count != 1 {
		t.Fatalf("err %v after %d calls", err, call.count)
	}
}

func TestAIErrorUnreachableAndCancelled(t *testing.T) {
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")
	srv.Close()

	_, err := collect(t, mustRequest(t, "openai", "", srv.URL))
	if err == nil || !strings.HasPrefix(err.Error(), "could not reach OpenAI: ") || strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("unreachable: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := streamAnswer(ctx, mustRequest(t, "anthropic", "", srv.URL), func(string) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

func TestAIModels(t *testing.T) {
	srv, call := fakeProvider(t, http.StatusOK, "application/json", `{"data":[
		{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-08-01T00:00:00Z"},
		{"type":"model","id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5","created_at":"2025-10-01T00:00:00Z"}
	],"has_more":false,"first_id":"claude-opus-5-5","last_id":"claude-haiku-4-5"}`)

	out, err := AIModels("anthropic", testAPIKey, srv.URL)
	if err != nil || call.path != "/v1/models" {
		t.Fatalf("anthropic models: path %q, err %v", call.path, err)
	}

	if want := `[{"id":"claude-opus-5-5","name":"Claude Opus 5.5"},{"id":"claude-haiku-4-5","name":"Claude Haiku 4.5"}]`; out != want {
		t.Fatalf("anthropic models: %s", out)
	}

	openAIList := `{"object":"list","data":[
		{"id":"gpt-4o","object":"model","created":100,"owned_by":"openai"},
		{"id":"text-embedding-3-large","object":"model","created":300,"owned_by":"openai"},
		{"id":"gpt-5","object":"model","created":200,"owned_by":"openai"},
		{"id":"gpt-4o-realtime-preview","object":"model","created":250,"owned_by":"openai"},
		{"id":"llama3","object":"model","created":50,"owned_by":"library"}
	]}`

	// As api.openai.com, whose list needs filtering.
	srv, _ = fakeProvider(t, http.StatusOK, "application/json", openAIList)
	asProviderAPI(t, srv)

	if out, err = AIModels("openai", testAPIKey, ""); err != nil || out != `[{"id":"gpt-5","name":"gpt-5"},{"id":"gpt-4o","name":"gpt-4o"}]` {
		t.Fatalf("openai models: %s, %v", out, err)
	}

	// Another server's models are all kept.
	if out, err = AIModels("openai", "", srv.URL+"/v1"); err != nil || !strings.Contains(out, `"llama3"`) || !strings.Contains(out, "embedding") {
		t.Fatalf("custom server models: %s, %v", out, err)
	}

	if _, err = AIModels("openai", "", ""); err == nil {
		t.Fatal("a missing key must be reported before any request")
	}
}
