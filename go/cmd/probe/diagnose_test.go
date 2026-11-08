package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// `probe diagnose` on the demo cluster, with a fake model on loopback: what the model asks for
// is printed.
func TestDiagnosePrintsToolCalls(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function",`+
				`"function":{"name":"object_yaml","arguments":"{\"namespace\":\"demo\",\"kind\":\"Secret\",\"name\":\"x\"}"}}]}}]}`+"\n\n"+
				`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")

			return
		}

		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
	}))
	t.Cleanup(srv.Close)

	t.Setenv("ICHOR_AI_BASE_URL", srv.URL)
	t.Setenv("OPENAI_API_KEY", "")

	cfg, err := ichorgo.DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	was := os.Stderr
	os.Stderr = write

	t.Cleanup(func() { os.Stderr = was })

	err = diagnose(cfg, "", "openai", "")

	os.Stderr = was

	_ = write.Close()

	if err != nil {
		t.Fatal(err)
	}

	raw, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"[tool] object_yaml {", `"kind":"Secret"`, "[tool] object_yaml refused: this kind cannot be read"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("stderr lacks %q:\n%s", want, raw)
		}
	}
}
