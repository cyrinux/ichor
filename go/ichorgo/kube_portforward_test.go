package ichorgo

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

type recForward struct {
	ready chan string
	errs  chan string
	done  chan string
}

func (r recForward) OnReady(address string)              { r.ready <- address }
func (r recForward) OnConnectionError(errMessage string) { r.errs <- errMessage }
func (r recForward) OnDone(errMessage string)            { r.done <- errMessage }

// fakePortForwardAPI answers /version and a port-forward that echoes "pong:" + what it gets.
func fakePortForwardAPI(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()

	paths := make(chan string, 4)
	ws := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			cfg.Protocol = []string{kubePortForwardProtocol}

			return nil
		},
		Handler: func(c *websocket.Conn) {
			c.PayloadType = websocket.BinaryFrame
			_ = websocket.Message.Send(c, []byte{0, 0x90, 0x1f})
			_ = websocket.Message.Send(c, []byte{1, 0x90, 0x1f})

			for {
				var frame []byte
				if websocket.Message.Receive(c, &frame) != nil {
					return
				}

				if len(frame) > 1 && frame[0] == 0 {
					_ = websocket.Message.Send(c, append([]byte("\x00pong:"), frame[1:]...))
				}
			}
		},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

			return
		}

		paths <- r.URL.Path + "?" + r.URL.RawQuery
		ws.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	return srv, paths
}

func TestStartPortForward(t *testing.T) {
	srv, paths := fakePortForwardAPI(t)

	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	added := strings.Replace(singleTokenKubeconfig(srv.URL, "t"), "cluster: {server:", fmt.Sprintf("cluster: {certificate-authority-data: %s, server:", ca), 1)

	stored, err := MergeKubeconfig("", "", added, "")
	if err != nil {
		t.Fatal(err)
	}

	rec := recForward{ready: make(chan string, 1), errs: make(chan string, 4), done: make(chan string, 1)}
	run := StartPortForward(stored, "x", "", "web", "grafana-0", 3000, rec)

	var addr string

	select {
	case addr = <-rec.ready:
	case msg := <-rec.done:
		t.Fatalf("ended: %s", msg)
	case <-time.After(10 * time.Second):
		t.Fatal("not ready")
	}

	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("listens on %s", addr)
	}

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = conn.Write([]byte("ping"))
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, 64)

	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "pong:ping" {
		t.Fatalf("read %q, %v", buf[:n], err)
	}

	_ = conn.Close()

	if p := <-paths; p != "/api/v1/namespaces/web/pods/grafana-0/portforward?ports=3000" {
		t.Errorf("path %s", p)
	}

	run.Stop()

	select {
	case msg := <-rec.done:
		if msg != "" {
			t.Fatalf("stop reported %q", msg)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("stop did not end the forward")
	}
}

func TestStartPortForwardValidates(t *testing.T) {
	rec := recForward{ready: make(chan string, 1), errs: make(chan string, 1), done: make(chan string, 1)}
	StartPortForward("", "x", "", "web", "p", 70000, rec)

	if msg := <-rec.done; !strings.Contains(msg, "invalid port") {
		t.Fatalf("got %q", msg)
	}
}
