package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type recordedTool struct{ name, args string }

type recordedToolDone struct {
	name   string
	bytes  int
	result string
	err    string
}

// toolModel answers each call with the next response, repeating the last, and keeps every
// request body: this is the model, and it asks what these tests make it ask.
type toolModel struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (s *toolModel) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.bodies)
}

func (s *toolModel) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, _ := json.Marshal(s.bodies) //nolint:errcheck

	return string(raw)
}

func newToolModel(t *testing.T, responses ...string) (*httptest.Server, *toolModel) {
	t.Helper()

	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")
	s := &toolModel{}

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) //nolint:errcheck

		var body map[string]any
		_ = json.Unmarshal(raw, &body) //nolint:errcheck

		s.mu.Lock()
		i := min(len(s.bodies), len(responses)-1)
		s.bodies = append(s.bodies, body)
		s.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, responses[i]) //nolint:errcheck
	})

	return srv, s
}

// A tool_use streamed in fragments, after thinking that has to come back as it was.
func anthropicToolStream(calls ...aiToolCall) string {
	events := []string{
		`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[]}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me look"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`{"type":"content_block_stop","index":0}`,
	}

	for i, c := range calls {
		start, _ := json.Marshal(map[string]any{ //nolint:errcheck
			"type": "content_block_start", "index": i + 1,
			"content_block": map[string]any{"type": "tool_use", "id": c.id, "name": c.name, "input": map[string]any{}},
		})
		events = append(events, string(start))

		mid := len(c.args) / 2
		for _, part := range []string{c.args[:mid], c.args[mid:]} {
			delta, _ := json.Marshal(map[string]any{ //nolint:errcheck
				"type": "content_block_delta", "index": i + 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": part},
			})
			events = append(events, string(delta))
		}

		events = append(events, fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i+1))
	}

	return sse(append(events,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"}}`, `{"type":"message_stop"}`)...)
}

// The same in OpenAI's chunks: only the first fragment of a call has its id and name.
func openAIToolStream(calls ...aiToolCall) string {
	var events []string

	for i, c := range calls {
		mid := len(c.args) / 2

		first, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{ //nolint:errcheck
			"index": i, "id": c.id, "type": "function", "function": map[string]any{"name": c.name, "arguments": c.args[:mid]},
		}}}}}})
		rest, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{ //nolint:errcheck
			"index": i, "function": map[string]any{"arguments": c.args[mid:]},
		}}}}}})
		events = append(events, string(first), string(rest))
	}

	return sse(append(events, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, `[DONE]`)...)
}

// wireFormats is both providers, as the tests drive them.
var wireFormats = []struct {
	provider string
	tools    func(...aiToolCall) string
	answer   func(string) string
}{
	{aiAnthropic, anthropicToolStream, func(text string) string { return anthropicStream("end_turn", text) }},
	{aiOpenAI, openAIToolStream, func(text string) string { return openAIStream("stop", text) }},
}

func askWithTools(t *testing.T, d *Diagnosis, provider, baseURL string, allow bool) (*answerRecorder, string) {
	t.Helper()

	l := &answerRecorder{done: make(chan string, 1)}
	d.Ask(provider, testAPIKey, "", baseURL, "en", "", allow, l)

	return l, <-l.done
}

const (
	billingLogPath = "GET /api/v1/namespaces/acme-billing/pods/invoices-7d9f-x2x4z/log"
	// A real log says more than the report: a host, a credential, an address, a word in JSON.
	billingLog = `connecting to db.acme.example:5432 from 192.0.2.77` + "\n" +
		`password=hunter2 Authorization: Bearer abc123def456` + "\n" +
		`panic: the acme-ledger service refused the invoice` + "\n"
)

func billingAnswers() map[string]string {
	answers := map[string]string{billingLogPath: billingLog}
	for k, v := range kubeDiagnosisAnswers {
		answers[k] = v
	}

	return answers
}

// The placeholders of a report, as a model would use them.
func podLogCall(d *Diagnosis, id string) aiToolCall {
	return aiToolCall{id: id, name: toolPodLogs, args: fmt.Sprintf(
		`{"namespace":%q,"pod":%q,"tail":100000}`, d.names.reals["acme-billing"], d.names.reals["invoices-7d9f-x2x4z"],
	)}
}

// The whole path on both wire formats: the model asks with placeholders, Go reads the real pod,
// what goes back is scrubbed and masked, the person sees placeholders in the request and the real
// names in the answer.
func TestAIToolsRoundTrip(t *testing.T) {
	for _, wire := range wireFormats {
		t.Run(wire.provider, func(t *testing.T) {
			f := newFakeKubeAPI(t, billingAnswers())

			d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
			if err != nil {
				t.Fatal(err)
			}

			pod := d.names.reals["invoices-7d9f-x2x4z"]
			srv, model := newToolModel(t,
				wire.tools(podLogCall(d, "call-1")),
				wire.answer("The log of "+pod+" shows the panic."),
			)

			l, errMessage := askWithTools(t, d, wire.provider, srv.URL, true)
			if errMessage != "" {
				t.Fatal(errMessage)
			}

			if got := l.answers[len(l.answers)-1]; got != "The log of invoices-7d9f-x2x4z shows the panic." {
				t.Fatalf("answer: %q", got)
			}

			if len(l.tools) != 1 || l.tools[0].name != toolPodLogs || strings.Contains(l.tools[0].args, "acme") ||
				strings.Contains(l.tools[0].args, "invoices") || !strings.Contains(l.tools[0].args, pod) {
				t.Fatalf("request shown: %+v", l.tools)
			}

			if len(l.toolsDone) != 1 || l.toolsDone[0].err != "" || l.toolsDone[0].bytes != len(l.toolsDone[0].result) {
				t.Fatalf("result shown: %+v", l.toolsDone)
			}

			// What the screen says was sent is what the second call holds.
			sent := model.sent()
			if !strings.Contains(sent, jsonText(l.toolsDone[0].result)) {
				t.Fatalf("the result shown is not the one sent:\n%s\n%s", l.toolsDone[0].result, sent)
			}

			for _, private := range []string{
				"acme", "invoices", "hunter2", "abc123def456", "192.0.2.77", "db.acme.example", testAPIKey,
			} {
				if strings.Contains(sent, private) || strings.Contains(l.toolsDone[0].result, private) {
					t.Errorf("%q left the phone:\n%s", private, sent)
				}
			}

			if !strings.Contains(sent, "panic") || !strings.Contains(sent, "[redacted]") {
				t.Errorf("the log's message is lost:\n%s", l.toolsDone[0].result)
			}

			if _, tools := model.bodies[0]["tools"]; !tools {
				t.Error("no tools offered")
			}

			if wire.provider == aiAnthropic && !strings.Contains(sent, "sig-1") {
				t.Error("the model's thinking did not come back with its result")
			}

			// The requested tail was far over the limit, and it is a read.
			for _, r := range f.recorded() {
				if strings.HasSuffix(r.path, "/log") && !strings.Contains(r.query, "tailLines=200") {
					t.Errorf("tail not bounded: %s", r.query)
				}

				if r.method != http.MethodGet {
					t.Errorf("a %s reached the cluster: %s", r.method, r.path)
				}
			}

			// The mask a question learns stays with that question.
			if _, leaked := d.names.reals["db.acme.example:5432"]; leaked {
				t.Error("the report's mask changed")
			}
		})
	}
}

func jsonText(s string) string {
	raw, _ := json.Marshal(s) //nolint:errcheck

	return strings.Trim(string(raw), `"`)
}

// Whatever a model asks that it should not get, nothing is read, and it is told.
func TestAIToolsRefuseWhatTheModelShouldNotGet(t *testing.T) {
	for _, wire := range wireFormats {
		f := newFakeKubeAPI(t, billingAnswers())

		d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
		if err != nil {
			t.Fatal(err)
		}

		ns, pod := d.names.reals["acme-billing"], d.names.reals["invoices-7d9f-x2x4z"]
		before := len(f.recorded())

		for _, tc := range []struct {
			name string
			call aiToolCall
			want string
		}{
			{"a secret", aiToolCall{name: toolObjectYAML, args: fmt.Sprintf(`{"namespace":%q,"kind":"Secret","name":%q}`, ns, pod)}, "cannot be read"},
			{"a configmap", aiToolCall{name: toolObjectYAML, args: fmt.Sprintf(`{"namespace":%q,"kind":"ConfigMap","name":%q}`, ns, pod)}, "cannot be read"},
			{"a secret's events", aiToolCall{name: toolObjectEvents, args: fmt.Sprintf(`{"namespace":%q,"kind":"secret","name":%q}`, ns, pod)}, "cannot be read"},
			{"a mutation", aiToolCall{name: "delete_pod", args: fmt.Sprintf(`{"namespace":%q,"pod":%q}`, ns, pod)}, "no such tool"},
			{"a shell", aiToolCall{name: "exec; rm -rf /", args: `{}`}, "no such tool"},
			{"a real name it guessed", aiToolCall{name: toolPodLogs, args: `{"namespace":"acme-billing","pod":"invoices-7d9f-x2x4z"}`}, "not in the report"},
			{"a pod it was not shown", aiToolCall{name: toolPodLogs, args: `{"namespace":"kube-system","pod":"vault-0"}`}, "not in the report"},
			{"a path", aiToolCall{name: toolObjectYAML, args: fmt.Sprintf(`{"namespace":%q,"kind":"Pod","name":"../../secrets/x"}`, ns)}, "not in the report"},
			{"a Talos node", aiToolCall{name: toolServiceLogs, args: `{"node":"192.0.2.1","service":"etcd"}`}, "no such tool"},
			{"another argument", aiToolCall{name: toolPodLogs, args: fmt.Sprintf(`{"namespace":%q,"pod":%q,"previous":true}`, ns, pod)}, "not valid"},
			{"not json", aiToolCall{name: toolPodLogs, args: `{{{`}, "not valid"},
		} {
			t.Run(wire.provider+" "+tc.name, func(t *testing.T) {
				tc.call.id = "call-1"
				srv, model := newToolModel(t, wire.tools(tc.call), wire.answer("Answered anyway."))

				l, errMessage := askWithTools(t, d, wire.provider, srv.URL, true)
				if errMessage != "" {
					t.Fatal(errMessage)
				}

				if len(l.toolsDone) != 1 || !strings.Contains(l.toolsDone[0].err, tc.want) || l.toolsDone[0].result != "" {
					t.Fatalf("result: %+v", l.toolsDone)
				}

				if !strings.Contains(model.sent(), tc.want) || l.answers[len(l.answers)-1] != "Answered anyway." {
					t.Fatalf("the model was not told, or did not answer: %v\n%s", l.answers, model.sent())
				}

				// Each request stays visible, and a name the model invented is not displayed as sent.
				if len(l.tools) != 1 || strings.Contains(l.tools[0].args, "acme-billing") && tc.name != "a real name it guessed" {
					t.Fatalf("request: %+v", l.tools)
				}
			})
		}

		for _, r := range f.recorded()[before:] {
			t.Errorf("%s: the cluster was called: %s %s", wire.provider, r.method, r.path)
		}
	}
}

// A guess must not come back as "that one is ns-1": the refusal is the same for any name.
func TestAIToolRefusalIsNoOracle(t *testing.T) {
	d := kubeDiagnosisFor(t, billingAnswers(), true)
	real, fake := "acme-billing", d.names.reals["acme-billing"]

	srv, _ := newToolModel(t, openAIToolStream(
		aiToolCall{id: "1", name: toolPodLogs, args: fmt.Sprintf(`{"namespace":%q,"pod":"x-1"}`, real)},
		aiToolCall{id: "2", name: toolPodLogs, args: `{"namespace":"never-heard-of","pod":"x-1"}`},
	), openAIStream("stop", "ok"))

	l, _ := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if len(l.toolsDone) != 2 || l.toolsDone[0].err == "" || l.toolsDone[0].err != l.toolsDone[1].err ||
		strings.Contains(l.toolsDone[0].err, fake) {
		t.Fatalf("results: %+v", l.toolsDone)
	}
}

// service_logs reads only a service the report lists, on a node of the report.
func TestAIToolServiceLogs(t *testing.T) {
	report := "- talos-w-a [192.0.2.20]: worker\n- 192.0.2.99 is mentioned in a log\nkubelet\nnot-a-service"
	target := &diagnosisToolTarget{
		kube:  kubeTarget{config: mustDemoConfig(t)},
		nodes: []toolNode{{address: "192.0.2.20", names: []string{"192.0.2.20", "talos-w-a"}, services: []string{"kubelet"}}},
	}

	for _, tc := range []struct{ args, want string }{
		{`{"node":"talos-w-a","service":"kubelet","tail":100000}`, ""},
		{`{"node":"192.0.2.99","service":"kubelet"}`, "not a node of this report"},
		{`{"node":"talos-w-a","service":"not-a-service"}`, "not on this node"},
		{`{"node":"10.9.9.9","service":"kubelet"}`, "not in the report"},
		{`{"node":"talos-w-a","service":"kubelet","pod":"x"}`, "takes node, service and tail"},
	} {
		srv, _ := newToolModel(t, anthropicToolStream(aiToolCall{id: "1", name: toolServiceLogs, args: tc.args}), anthropicStream("end_turn", "ok"))

		l, _ := askWithTools(t, &Diagnosis{report: report, tools: target}, aiAnthropic, srv.URL, true)
		if len(l.toolsDone) != 1 {
			t.Fatalf("%s: %+v", tc.args, l.toolsDone)
		}

		if got := l.toolsDone[0]; tc.want == "" && (got.err != "" || got.result == "") || tc.want != "" && !strings.Contains(got.err, tc.want) {
			t.Errorf("%s: %+v", tc.args, got)
		}
	}
}

func TestAIToolNameShownIsBounded(t *testing.T) {
	d := &Diagnosis{report: "r", tools: &diagnosisToolTarget{}}
	srv, _ := newToolModel(t,
		openAIToolStream(aiToolCall{id: "1", name: "exec; rm -rf /", args: `{}`}), openAIStream("stop", "ok"))

	l, _ := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if len(l.tools) != 1 || l.tools[0].name != toolUnknownName {
		t.Fatalf("tools: %+v", l.tools)
	}
}

// Three requests, however the model spreads them: a fourth is refused, the next call cannot use
// tools, and it answers.
func TestAIToolsCapAtThree(t *testing.T) {
	for _, wire := range wireFormats {
		t.Run(wire.provider, func(t *testing.T) {
			f := newFakeKubeAPI(t, billingAnswers())

			d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
			if err != nil {
				t.Fatal(err)
			}

			call := podLogCall(d, "c")
			srv, model := newToolModel(t,
				wire.tools(call, call), wire.tools(call, call), wire.answer("The answer."))

			l, errMessage := askWithTools(t, d, wire.provider, srv.URL, true)
			if errMessage != "" {
				t.Fatal(errMessage)
			}

			read, refused := 0, 0

			for _, done := range l.toolsDone {
				switch {
				case done.err == "":
					read++
				case strings.Contains(done.err, "limit"):
					refused++
				}
			}

			if read != 3 || refused != 1 || len(l.tools) != 4 {
				t.Fatalf("read %d, refused %d, requests %d: %+v", read, refused, len(l.tools), l.toolsDone)
			}

			logs := 0

			for _, r := range f.recorded() {
				if strings.HasSuffix(r.path, "/log") {
					logs++
				}
			}

			if logs != 3 {
				t.Fatalf("%d logs read", logs)
			}

			if model.calls() != 3 || !strings.Contains(fmt.Sprint(model.bodies[2]["tool_choice"]), "none") {
				t.Fatalf("calls %d, last tool_choice %v", model.calls(), model.bodies[2]["tool_choice"])
			}
		})
	}
}

// A model that never stops asking ends the question, within a bounded number of calls.
func TestAIToolsModelThatNeverAnswers(t *testing.T) {
	for _, wire := range wireFormats {
		t.Run(wire.provider, func(t *testing.T) {
			f := newFakeKubeAPI(t, billingAnswers())

			d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
			if err != nil {
				t.Fatal(err)
			}

			srv, model := newToolModel(t, wire.tools(podLogCall(d, "c")))

			_, errMessage := askWithTools(t, d, wire.provider, srv.URL, true)
			if errMessage != answerKeptAsking {
				t.Fatalf("ended with %q", errMessage)
			}

			if model.calls() != maxAIToolRounds+1 {
				t.Fatalf("%d calls", model.calls())
			}
		})
	}
}

// A denial is the reader's, and it goes back as the result; the name in it stays masked.
func TestAIToolRefusedByRBAC(t *testing.T) {
	for _, wire := range wireFormats {
		t.Run(wire.provider, func(t *testing.T) {
			answers := billingAnswers()
			delete(answers, billingLogPath)

			f := newFakeKubeAPI(t, answers)

			d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
			if err != nil {
				t.Fatal(err)
			}

			f.api.AnswerWith(billingLogPath, http.StatusForbidden, `{"kind":"Status","status":"Failure","code":403,"reason":"Forbidden",`+
				`"message":"pods \"invoices-7d9f-x2x4z\" is forbidden: User \"acme-admin\" cannot get resource \"pods/log\""}`)

			srv, model := newToolModel(t, wire.tools(podLogCall(d, "c")), wire.answer("I may not read it."))

			l, errMessage := askWithTools(t, d, wire.provider, srv.URL, true)
			if errMessage != "" {
				t.Fatal(errMessage)
			}

			if len(l.toolsDone) != 1 || l.toolsDone[0].err == "" || l.toolsDone[0].bytes != 0 {
				t.Fatalf("result: %+v", l.toolsDone)
			}

			if sent := model.sent(); !strings.Contains(sent, jsonText(l.toolsDone[0].err)) ||
				strings.Contains(sent, "invoices") || strings.Contains(sent, "acme") {
				t.Fatalf("what the model got:\n%s", sent)
			}

			if wire.provider == aiAnthropic && !strings.Contains(model.sent(), `"is_error":true`) {
				t.Error("not marked as an error")
			}
		})
	}
}

// A manifest holds credentials in plain words, and names the sweep would let through.
func TestAIToolObjectYAMLHidesValues(t *testing.T) {
	answers := billingAnswers()
	answers["GET /apis/apps/v1/namespaces/acme-billing/deployments/invoices"] = `{
		"apiVersion":"apps/v1","kind":"Deployment",
		"metadata":{"name":"invoices","namespace":"acme-billing","labels":{"team":"payments"},
			"managedFields":[{"manager":"kubectl-acme"}],
			"annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"value\":\"hunter2\"}","note":"see wiki"}},
		"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"acme-api","image":"registry.acme.example/billing/invoices:1",
			"command":["/bin/api","--db-password","sw0rdfish","--port=8080"],
			"env":[{"name":"DB_PASSWORD","value":"hunter2"},{"name":"REF","valueFrom":{"secretKeyRef":{"name":"payroll","key":"k"}}}]}]}}}}`

	f := newFakeKubeAPI(t, answers)

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
	if err != nil {
		t.Fatal(err)
	}

	srv, model := newToolModel(t, openAIToolStream(aiToolCall{id: "c", name: toolObjectYAML, args: fmt.Sprintf(
		`{"namespace":%q,"kind":"deployment","name":%q}`, d.names.reals["acme-billing"], d.names.reals["invoices"],
	)}), openAIStream("stop", "ok"))

	l, errMessage := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if errMessage != "" {
		t.Fatal(errMessage)
	}

	if len(l.toolsDone) != 1 || l.toolsDone[0].err != "" {
		t.Fatalf("result: %+v", l.toolsDone)
	}

	result := l.toolsDone[0].result

	for _, private := range []string{"hunter2", "sw0rdfish", "payments", "payroll", "acme", "invoices", "registry", "last-applied"} {
		if strings.Contains(result, private) || strings.Contains(model.sent(), private) {
			t.Errorf("%q left the phone:\n%s", private, result)
		}
	}

	for _, want := range []string{"DB_PASSWORD", hiddenToolValue, "replicas: 3", "kind: Deployment"} {
		if !strings.Contains(result, want) {
			t.Errorf("result lacks %q:\n%s", want, result)
		}
	}
}

func TestAIToolResultIsCut(t *testing.T) {
	answers := billingAnswers()
	answers[billingLogPath] = strings.Repeat("a line of the log that repeats\n", 5000) + "the last line\n"

	f := newFakeKubeAPI(t, answers)

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", true)
	if err != nil {
		t.Fatal(err)
	}

	srv, _ := newToolModel(t, openAIToolStream(podLogCall(d, "c")), openAIStream("stop", "ok"))

	l, _ := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if len(l.toolsDone) != 1 || l.toolsDone[0].bytes > maxToolResult || !strings.Contains(l.toolsDone[0].result, "the last line") {
		t.Fatalf("result of %d bytes", l.toolsDone[0].bytes)
	}
}

// With real names asked for, names go as they are; the scrubber still runs.
func TestAIToolsWithRealNamesStillScrub(t *testing.T) {
	f := newFakeKubeAPI(t, billingAnswers())

	d, err := CollectDiagnosis(f.kubeconfigFor(f.URL), "admin@test", "", false)
	if err != nil {
		t.Fatal(err)
	}

	srv, model := newToolModel(t, openAIToolStream(aiToolCall{
		id: "c", name: toolPodLogs, args: `{"namespace":"acme-billing","pod":"invoices-7d9f-x2x4z"}`,
	}), openAIStream("stop", "ok"))

	l, errMessage := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if errMessage != "" || len(l.toolsDone) != 1 || l.toolsDone[0].err != "" {
		t.Fatalf("%q %+v", errMessage, l.toolsDone)
	}

	if sent := model.sent(); strings.Contains(sent, "hunter2") || strings.Contains(sent, "abc123def456") || !strings.Contains(sent, "acme-ledger") {
		t.Fatalf("sent:\n%s", sent)
	}
}

// The switch off, the phone's model and the health helper are the single shot they were.
func TestAIToolsOff(t *testing.T) {
	for _, tc := range []struct {
		name  string
		d     *Diagnosis
		allow bool
	}{
		{"switch off", &Diagnosis{report: "r", tools: &diagnosisToolTarget{}}, false},
		{"report without tools", &Diagnosis{report: "r", kind: diagnosisHealth}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, model := newToolModel(t, anthropicStream("end_turn", "ok"))

			l, errMessage := askWithTools(t, tc.d, aiAnthropic, srv.URL, tc.allow)
			if errMessage != "" || len(l.tools) != 0 || model.calls() != 1 {
				t.Fatalf("%q, %+v, %d calls", errMessage, l.tools, model.calls())
			}

			if _, tools := model.bodies[0]["tools"]; tools {
				t.Fatal("tools offered")
			}

			if strings.Contains(fmt.Sprint(model.bodies[0]["system"]), toolInstructions) {
				t.Fatal("the prompt talks about tools")
			}
		})
	}
}

// A model that asks without tools on offer gets nothing run.
func TestAIToolRequestWithoutToolsIsIgnored(t *testing.T) {
	srv, model := newToolModel(t, openAIToolStream(aiToolCall{id: "c", name: toolPodLogs, args: `{}`}))

	l, errMessage := askWithTools(t, &Diagnosis{report: "r"}, aiOpenAI, srv.URL, true)
	if len(l.tools) != 0 || model.calls() != 1 {
		t.Fatalf("%q, %+v, %d calls", errMessage, l.tools, model.calls())
	}
}

// A gateway that does not know the parameter: asked again without it, the person told once.
func TestAIToolsFallBackOnServerThatRefusesThem(t *testing.T) {
	srv, _ := fakeProvider(t, http.StatusOK, "text/event-stream", "")

	var bodies []map[string]any

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body) //nolint:errcheck

		var body map[string]any
		_ = json.Unmarshal(raw, &body) //nolint:errcheck

		bodies = append(bodies, body)

		if _, tools := body["tools"]; tools {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"unknown field: tools"}}`) //nolint:errcheck

			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openAIStream("stop", "from the report")) //nolint:errcheck
	})

	l, errMessage := askWithTools(t, &Diagnosis{report: "r", tools: &diagnosisToolTarget{}}, aiOpenAI, srv.URL, true)
	if errMessage != "" || l.answers[len(l.answers)-1] != "from the report" || len(bodies) != 2 {
		t.Fatalf("%q %v %d", errMessage, l.answers, len(bodies))
	}

	if len(l.toolsDone) != 1 || l.toolsDone[0].name != "" || l.toolsDone[0].err != aiToolsUnsupported {
		t.Fatalf("told: %+v", l.toolsDone)
	}
}

// Another refusal stays an error.
func TestToolsRefused(t *testing.T) {
	provider := aiProvider{Name: "X"}

	for _, tc := range []struct {
		status  int
		message string
		want    bool
	}{
		{400, "tools is not supported", true},
		{422, "functions are unknown", true},
		{400, "max_tokens too large", false},
		{403, "tools are not allowed", false},
	} {
		err := aiError(t.Context(), provider, tc.status, tc.message, nil)
		if toolsRefused(err) != tc.want {
			t.Errorf("%d %q: %v", tc.status, tc.message, !tc.want)
		}
	}
}

func TestShownWord(t *testing.T) {
	for _, tc := range []struct {
		text, word string
		want       bool
	}{
		{"pod-1 is down", "pod-1", true},
		{"see ns-1/pod-1.", "pod-1", true},
		{"pod-12 is down", "pod-1", false},
		{"my-pod-1 is down", "pod-1", false},
		{"anything", "", false},
	} {
		if got := shownWord(tc.text, tc.word); got != tc.want {
			t.Errorf("shownWord(%q, %q) = %v", tc.text, tc.word, got)
		}
	}
}

func TestDemoContextAnswersTools(t *testing.T) {
	cfg := demoKubeconfigForTest(t)

	d, err := CollectDiagnosis(cfg, "", "", true)
	if err != nil {
		t.Fatal(err)
	}

	var ns, pod string

	for real, fake := range d.names.reals {
		switch {
		case strings.HasPrefix(fake, "ns-") && ns == "":
			ns = fake
		case strings.HasPrefix(fake, "pod-") && pod == "":
			pod = fake
		}

		_ = real
	}

	srv, _ := newToolModel(t, openAIToolStream(aiToolCall{
		id: "c", name: toolObjectEvents, args: fmt.Sprintf(`{"namespace":%q,"kind":"Pod","name":%q}`, ns, pod),
	}), openAIStream("stop", "ok"))

	l, errMessage := askWithTools(t, d, aiOpenAI, srv.URL, true)
	if errMessage != "" || len(l.toolsDone) != 1 {
		t.Fatalf("%q %+v", errMessage, l.toolsDone)
	}
}
