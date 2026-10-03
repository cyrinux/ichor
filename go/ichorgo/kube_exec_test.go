package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"golang.org/x/net/websocket"
)

// fakeExecServer answers exec requests with the given frames (channel byte first) and
// records the query of the last one.
func fakeExecServer(t *testing.T, frames ...[]byte) (*kubeClient, *url.Values) {
	t.Helper()

	var query url.Values

	ws := websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			query = r.URL.Query()

			if !slices.Contains(cfg.Protocol, "v4.channel.k8s.io") {
				return errors.New("no channel protocol")
			}

			cfg.Protocol = []string{"v4.channel.k8s.io"}

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame
			for _, f := range frames {
				_ = websocket.Message.Send(conn, f)
			}
		},
	}

	srv := httptest.NewTLSServer(ws)
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL)
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig

	return &kubeClient{base: base, http: srv.Client(), tls: tlsCfg}, &query
}

func frame(channel byte, s string) []byte { return append([]byte{channel}, s...) }

func TestKubeExecDemultiplexes(t *testing.T) {
	k, query := fakeExecServer(t,
		[]byte{execStdout}, []byte{execStderr}, // channel announcements
		frame(execStderr, "INFO connected\n"),
		frame(execStdout, `{"status":`),
		frame(execStdout, `"healthy"}`),
		frame(execStatus, `{"metadata":{},"status":"Success"}`),
	)

	stdout, stderr, err := k.exec(context.Background(), "garage", "garage-0", "garage", []string{"/garage", "json-api", "GetClusterHealth"})
	if err != nil {
		t.Fatal(err)
	}

	if string(stdout) != `{"status":"healthy"}` || string(stderr) != "INFO connected\n" {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}

	q := *query
	if q.Get("container") != "garage" || !slices.Equal(q["command"], []string{"/garage", "json-api", "GetClusterHealth"}) ||
		q.Get("stdout") != "true" || q.Get("stderr") != "true" || q.Has("stdin") {
		t.Fatalf("unexpected query %v", q)
	}
}

func TestKubeExecNonZeroExit(t *testing.T) {
	k, _ := fakeExecServer(t,
		frame(execStderr, "Error: JSON error: missing field `body`\n"),
		frame(execStatus, `{"metadata":{},"status":"Failure","message":"command terminated with non-zero exit code: error executing command [/garage], exit code 1","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"1"}]}}`),
	)

	_, stderr, err := k.exec(context.Background(), "garage", "garage-0", "garage", []string{"/garage"})

	var execErr *kubeExecError
	if !errors.As(err, &execErr) || execErr.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %v", err)
	}

	if len(stderr) == 0 {
		t.Fatal("stderr lost on failure")
	}
}

func TestKubeExecValidates(t *testing.T) {
	k := &kubeClient{base: &url.URL{Scheme: "https", Host: "invalid"}}

	for _, tc := range []struct{ ns, pod, container string }{
		{"../x", "p", "c"}, {"ns", "", "c"}, {"ns", "p", "C/x"},
	} {
		if _, _, err := k.exec(context.Background(), tc.ns, tc.pod, tc.container, []string{"/garage"}); err == nil {
			t.Errorf("%v: expected a validation error", tc)
		}
	}

	if _, _, err := k.exec(context.Background(), "ns", "p", "c", nil); err == nil {
		t.Error("empty argv accepted")
	}
}

func TestKubeExecRefused(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	base, _ := url.Parse(srv.URL)
	k := &kubeClient{base: base, http: srv.Client(), tls: srv.Client().Transport.(*http.Transport).TLSClientConfig}

	if _, _, err := k.exec(context.Background(), "ns", "p", "c", []string{"/garage"}); !errors.Is(err, errExecRefused) {
		t.Fatalf("expected errExecRefused, got %v", err)
	}
}

func TestExecStatusError(t *testing.T) {
	if err := execStatusError(nil); err != nil {
		t.Errorf("no status: %v", err)
	}

	var e *kubeExecError
	if err := execStatusError([]byte("not json")); !errors.As(err, &e) || e.ExitCode != -1 {
		t.Errorf("garbage status: %v", err)
	}
}
