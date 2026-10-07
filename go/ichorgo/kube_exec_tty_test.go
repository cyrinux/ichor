package ichorgo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// shellRecorder collects what a shell reports; done closes on exit.
type shellRecorder struct {
	mu      sync.Mutex
	output  strings.Builder
	code    int
	message string
	done    chan struct{}
}

func newShellRecorder() *shellRecorder { return &shellRecorder{done: make(chan struct{})} }

func (l *shellRecorder) OnStatus(string) {}

func (l *shellRecorder) OnOutput(data []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output.Write(data)
}

func (l *shellRecorder) OnExit(code int, message string) {
	l.code, l.message = code, message
	close(l.done)
}

func (l *shellRecorder) wait(t *testing.T) {
	t.Helper()

	select {
	case <-l.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the shell never exited")
	}
}

// fakeTTYServer echoes stdin frames back on stdout until "exit\r", then reports exit code 3.
// It records the query and the frames it received.
func fakeTTYServer(t *testing.T) (*kubeClient, *url.Values, func() [][]byte) {
	t.Helper()

	var (
		mu       sync.Mutex
		query    url.Values
		received [][]byte
	)

	ws := websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			query = r.URL.Query()
			cfg.Protocol = []string{"v4.channel.k8s.io"}

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame
			_ = websocket.Message.Send(conn, frame(execStdout, "$ "))

			for {
				var f []byte
				if err := websocket.Message.Receive(conn, &f); err != nil {
					return
				}

				mu.Lock()
				received = append(received, f)
				mu.Unlock()

				if f[0] != execStdin {
					continue
				}

				if string(f[1:]) == "exit\r" {
					_ = websocket.Message.Send(conn, frame(execStatus, `{"status":"Failure","details":{"causes":[{"reason":"ExitCode","message":"3"}]}}`))

					return
				}

				_ = websocket.Message.Send(conn, frame(execStdout, string(f[1:])))
			}
		},
	}

	srv := httptest.NewTLSServer(ws)
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL)
	k := &kubeClient{base: base, http: srv.Client(), tls: srv.Client().Transport.(*http.Transport).TLSClientConfig}

	return k, &query, func() [][]byte {
		mu.Lock()
		defer mu.Unlock()

		return slices.Clone(received)
	}
}

func TestPodShellRoundTrip(t *testing.T) {
	k, query, received := fakeTTYServer(t)
	l := newShellRecorder()
	d, ctx := newDebugSession(l)

	d.Write([]byte("ls\r"))
	d.Write(nil) // nothing to send

	go func() {
		defer d.cancel()

		ws, err := k.dialExec(ctx, "apps", "web-0", "", podShellArgs(""), true)
		if err != nil {
			d.exit(-1, err.Error())

			return
		}
		defer ws.Close() //nolint:errcheck

		d.Resize(100, 30)
		go d.forwardExecInput(ctx, ws)

		time.AfterFunc(200*time.Millisecond, func() { d.Write([]byte("exit\r")) })
		d.receiveExec(ctx, ws)
	}()

	l.wait(t)

	if l.code != 3 || l.message != "" {
		t.Fatalf("exit %d %q", l.code, l.message)
	}

	l.mu.Lock()
	out := l.output.String()
	l.mu.Unlock()

	if out != "$ ls\r" {
		t.Fatalf("output %q", out)
	}

	q := *query
	if q.Get("stdin") != "true" || q.Get("tty") != "true" || q.Get("stdout") != "true" || q.Has("stderr") || q.Has("container") ||
		!slices.Equal(q["command"], podShellAuto) {
		t.Fatalf("unexpected query %v", q)
	}

	if !slices.ContainsFunc(received(), func(f []byte) bool { return string(f) == "\x04"+`{"Width":100,"Height":30}` }) {
		t.Fatalf("no resize frame in %q", received())
	}
}

func TestPodShellClose(t *testing.T) {
	k, _, _ := fakeTTYServer(t)
	l := newShellRecorder()
	d, ctx := newDebugSession(l)

	go func() {
		ws, err := k.dialExec(ctx, "apps", "web-0", "web", []string{"sh"}, true)
		if err != nil {
			d.exit(-1, err.Error())

			return
		}

		stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
		defer stop()

		d.receiveExec(ctx, ws)
	}()

	time.Sleep(100 * time.Millisecond)
	d.Close()
	l.wait(t)

	if l.code != -1 || l.message != "closed" {
		t.Fatalf("exit %d %q", l.code, l.message)
	}
}

func TestExecExit(t *testing.T) {
	cases := []struct {
		status string
		code   int
		msg    string
	}{
		{"", 0, ""},
		{`{"status":"Success"}`, 0, ""},
		{`{"status":"Failure","details":{"causes":[{"reason":"ExitCode","message":"127"}]}}`, 127, ""},
		{`{"status":"Failure","message":"executable file not found in $PATH"}`, -1, "exec failed: executable file not found in $PATH"},
	}

	for _, c := range cases {
		if code, msg := execExit([]byte(c.status)); code != c.code || msg != c.msg {
			t.Errorf("%s: got %d %q", c.status, code, msg)
		}
	}
}

func TestPodShellArgsAndValidation(t *testing.T) {
	if got := podShellArgs("  bash -l "); !slices.Equal(got, []string{"bash", "-l"}) {
		t.Fatalf("args %q", got)
	}

	l := newShellRecorder()
	StartPodShell("", "", "", "Bad NS", "web-0", "", "", 80, 24, l)
	l.wait(t)

	if l.code != -1 || l.message == "" {
		t.Fatalf("expected a validation error, got %d %q", l.code, l.message)
	}
}

// TestPodShellAutoPrefersBash runs the default command with a real sh, in a PATH with and
// without bash: bash when the image has it, sh otherwise (which then reads stdin).
func TestPodShellAutoPrefersBash(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on this host")
	}

	run := func(t *testing.T, withBash bool) string {
		t.Helper()

		dir := t.TempDir()
		if err := os.Symlink(sh, filepath.Join(dir, "sh")); err != nil {
			t.Fatal(err)
		}

		if withBash {
			fake := "#!" + sh + "\necho ran bash\n"
			if err := os.WriteFile(filepath.Join(dir, "bash"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
		}

		cmd := exec.Command(filepath.Join(dir, podShellAuto[0]), podShellAuto[1:]...)
		cmd.Env = []string{"PATH=" + dir}
		cmd.Stdin = strings.NewReader("echo ran sh\n")

		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}

		return strings.TrimSpace(string(out))
	}

	if got := run(t, true); got != "ran bash" {
		t.Errorf("with bash: %q", got)
	}

	if got := run(t, false); got != "ran sh" {
		t.Errorf("without bash: %q", got)
	}
}

func TestPodShellDroppedConnection(t *testing.T) {
	k, _ := fakeExecServer(t, frame(execStdout, "$ ")) // closes without a status

	l := newShellRecorder()
	d, ctx := newDebugSession(l)

	ws, err := k.dialExec(ctx, "apps", "web-0", "", []string{"sh"}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close() //nolint:errcheck

	d.receiveExec(ctx, ws)
	l.wait(t)

	if l.code != -1 || l.message != "connection closed" {
		t.Fatalf("exit %d %q", l.code, l.message)
	}
}
