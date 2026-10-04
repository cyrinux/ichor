package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func collectLines(t *testing.T, k *kubeClient) ([]string, error) {
	t.Helper()

	var lines []string

	err := k.execLines(context.Background(), "kube-system", "cilium-abcde", "cilium-agent", []string{"hubble", "observe"}, nil, func(b []byte) {
		lines = append(lines, string(b))
	})

	return lines, err
}

func TestExecLinesSplitsAcrossFrames(t *testing.T) {
	k, _ := fakeExecServer(t,
		frame(execStdout, `{"a":1}`+"\n"+`{"b":`),
		frame(execStdout, `2}`+"\n\n"),
		frame(execStdout, `{"c":3}`),
		frame(execStatus, `{"metadata":{},"status":"Success"}`),
	)

	lines, err := collectLines(t, k)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(lines, []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}) {
		t.Fatalf("lines %q", lines)
	}
}

func TestExecLinesDropsOverlongLine(t *testing.T) {
	long := strings.Repeat("x", kubeExecMaxLine+10)
	k, _ := fakeExecServer(t,
		frame(execStdout, "first\n"+long[:kubeExecMaxLine/2]),
		frame(execStdout, long[kubeExecMaxLine/2:]),
		frame(execStdout, "tail-of-long\nlast\n"),
	)

	lines, err := collectLines(t, k)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(lines, []string{"first", "last"}) {
		t.Fatalf("lines %d %q", len(lines), lines[0])
	}
}

func TestExecLinesReportsStderrOnFailure(t *testing.T) {
	k, _ := fakeExecServer(t,
		frame(execStderr, "failed to connect to 'unix:///var/run/cilium/hubble.sock'\n"),
		frame(execStatus, `{"status":"Failure","details":{"causes":[{"reason":"ExitCode","message":"1"}]}}`),
	)

	_, err := collectLines(t, k)

	var execErr *kubeExecError
	if !errors.As(err, &execErr) || execErr.ExitCode != 1 || !strings.Contains(execErr.Message, "hubble.sock") {
		t.Fatalf("got %v", err)
	}
}

func TestExecLinesStopsOnCancel(t *testing.T) {
	ws := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			cfg.Protocol = []string{"v4.channel.k8s.io"}

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame
			_ = websocket.Message.Send(conn, frame(execStdout, "one\n"))
			time.Sleep(5 * time.Second) // a follow that never ends
		},
	}

	srv := httptest.NewTLSServer(ws)
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL)
	k := &kubeClient{base: base, http: srv.Client(), tls: srv.Client().Transport.(*http.Transport).TLSClientConfig}

	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan string, 1)
	start := time.Now()

	err := k.execLines(ctx, "kube-system", "cilium-abcde", "cilium-agent", []string{"hubble"}, nil, func(b []byte) {
		got <- string(b)

		cancel()
	})

	if !errors.Is(err, context.Canceled) || <-got != "one" || time.Since(start) > 3*time.Second {
		t.Fatalf("err %v after %s", err, time.Since(start))
	}
}

func TestExecLinesValidates(t *testing.T) {
	k := &kubeClient{}

	if err := k.execLines(context.Background(), "Bad NS", "p", "c", []string{"x"}, nil, nil); err == nil {
		t.Fatal("expected a name error")
	}

	if err := k.execLines(context.Background(), "ns", "p", "c", nil, nil, nil); err == nil {
		t.Fatal("expected an argv error")
	}
}
