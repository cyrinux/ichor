package ichorgo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// fakeDebugAPI serves a pod that takes an ephemeral container, reports it waiting with
// waitReason (then running unless failMessage is set), and attaches to it: the terminal echoes
// stdin until "exit\r", then closes without a status, as an attach stream does.
type fakeDebugAPI struct {
	mu          sync.Mutex
	patchType   string
	patch       map[string]any
	name        string
	gets        int
	waitReason  string
	failMessage string
	attachQuery url.Values
}

func (f *fakeDebugAPI) serve(t *testing.T) *kubeClient {
	t.Helper()

	attach := websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			f.mu.Lock()
			f.attachQuery = r.URL.Query()
			f.mu.Unlock()

			cfg.Protocol = []string{"v4.channel.k8s.io"}

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame

			for {
				var f []byte
				if err := websocket.Message.Receive(conn, &f); err != nil {
					return
				}

				if f[0] != execStdin {
					continue
				}

				if string(f[1:]) == "exit\r" {
					return
				}

				_ = websocket.Message.Send(conn, frame(execStdout, string(f[1:])))
			}
		},
	}

	const podPath = "/api/v1/namespaces/apps/pods/web-0"

	mux := http.NewServeMux()
	mux.Handle(podPath+"/attach", attach)
	mux.HandleFunc("PATCH "+podPath+"/ephemeralcontainers", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		defer f.mu.Unlock()

		f.patchType = r.Header.Get("Content-Type")
		_ = json.Unmarshal(body, &f.patch)

		containers, _ := f.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)
		if len(containers) == 1 {
			f.name, _ = containers[0].(map[string]any)["name"].(string)
		}

		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("GET "+podPath, func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.gets++

		state := `{"waiting":{"reason":"` + f.waitReason + `"}}`

		switch {
		case f.failMessage != "":
			state = `{"waiting":{"reason":"ErrImagePull","message":"` + f.failMessage + `"}}`
		case f.gets > 2: // the check before the patch, then one wait
			state = `{"running":{"startedAt":"2026-10-09T10:00:00Z"}}`
		}

		_, _ = io.WriteString(w, `{"spec":{"containers":[{"name":"app"}]},"status":{"ephemeralContainerStatuses":[{"name":"`+f.name+`","state":`+state+`}]}}`)
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL)

	return &kubeClient{base: base, http: srv.Client(), tls: srv.Client().Transport.(*http.Transport).TLSClientConfig}
}

func fastDebugPoll(t *testing.T) {
	t.Helper()

	poll := podDebugPoll
	podDebugPoll = 5 * time.Millisecond

	t.Cleanup(func() { podDebugPoll = poll })
}

func TestPodDebugAddsAnEphemeralContainerThenAttaches(t *testing.T) {
	fastDebugPoll(t)

	api := &fakeDebugAPI{waitReason: "ContainerCreating"}
	k := api.serve(t)
	l := newShellRecorder()
	d, ctx := newDebugSession(l)

	var statuses []string

	name, err := addDebugContainer(ctx, k, "apps", "web-0", "app", "busybox:1.37", func(s string) { statuses = append(statuses, s) })
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(name, "debugger-") || name != api.name {
		t.Fatalf("container %q, patched %q", name, api.name)
	}

	if api.patchType != "application/strategic-merge-patch+json" {
		t.Errorf("patch type %q", api.patchType)
	}

	c := api.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)[0].(map[string]any)
	if c["image"] != "busybox:1.37" || c["targetContainerName"] != "app" || c["stdin"] != true || c["tty"] != true {
		t.Errorf("ephemeral container %v", c)
	}

	if len(statuses) == 0 || !strings.Contains(strings.Join(statuses, "\n"), "ContainerCreating") {
		t.Errorf("statuses %q", statuses)
	}

	ws, err := k.dialAttach(ctx, "apps", "web-0", name)
	if err != nil {
		t.Fatal(err)
	}

	d.Write([]byte("ls\r"))
	d.Write([]byte("exit\r"))

	go d.serveTTY(ctx, ws, 80, 24, true)

	l.wait(t)

	if l.code != 0 || l.message != "" || !strings.Contains(l.output.String(), "ls") {
		t.Errorf("exit %d %q, output %q", l.code, l.message, l.output.String())
	}

	if api.attachQuery.Get("container") != name || api.attachQuery.Get("tty") != "true" || api.attachQuery.Has("command") {
		t.Errorf("attach query %v", api.attachQuery)
	}
}

func TestPodDebugReportsAnImageThatCannotBePulled(t *testing.T) {
	fastDebugPoll(t)

	api := &fakeDebugAPI{failMessage: "pull access denied for nope"}
	k := api.serve(t)

	_, err := addDebugContainer(context.Background(), k, "apps", "web-0", "", "nope", func(string) {})
	if err == nil || !strings.Contains(err.Error(), "pull access denied for nope") {
		t.Fatalf("got %v", err)
	}

	if _, has := api.patch["spec"].(map[string]any)["ephemeralContainers"].([]any)[0].(map[string]any)["targetContainerName"]; has {
		t.Error("an empty target container was sent")
	}
}

func TestStartPodDebugRefusals(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, image, want string }{
		{"demo", "", errDemoUnavailable.Error()},
		{"bad image", "-rm", "invalid image reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newShellRecorder()
			StartPodDebug(cfg, "", "", "demo", "web-0", "", tc.image, 80, 24, l)
			l.wait(t)

			if l.code != -1 || !strings.Contains(l.message, tc.want) {
				t.Errorf("exit %d %q, want %q", l.code, l.message, tc.want)
			}
		})
	}
}

func TestDebuggerNamesAreValidAndDistinct(t *testing.T) {
	a, b := debuggerName(), debuggerName()
	if a == b || !kubeNamePattern.MatchString(a) || !strings.HasPrefix(a, "debugger-") {
		t.Errorf("names %q %q", a, b)
	}
}
