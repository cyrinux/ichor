package ichorgo

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// diagStopCause is the cause a loopback stop() reported to the diagnostics ring, and the
// error text with it ("" when the entry carries none).
func diagStopCause(t *testing.T) (string, string) {
	t.Helper()

	e, ok := findDiag(diagEntriesOf(t, 0), "loopback stop")
	if !ok {
		t.Fatal("stopLoopback did not say anything to the diagnostics ring")
	}

	cause, _ := e.Attrs["cause"].(string)

	return cause, e.Err
}

// fakeListener is a net.Listener whose Close answers with a fixed error: enough to make
// srv.Shutdown fail the way a listener closed under the server does, without a real socket.
type fakeListener struct {
	closeErr error
}

func (f *fakeListener) Accept() (net.Conn, error) {
	return nil, errors.New("the fake listener never accepts")
}

func (f *fakeListener) Close() error { return f.closeErr }

func (f *fakeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1} }

// TestLoopbackStopDiagnostics is CYR-597: when the graceful close does not work, the ring
// must tell the three causes apart - Shutdown returned an error, the deadline ran out, or
// the server or listener was already closed - rather than swallow the failure.
//
// A successful stop stays silent: the ring is for what went wrong, and every sign-in stops
// its loopback. And the three failures are recorded as three distinct entries, which is why
// each case takes a fresh ring.
func TestLoopbackStopDiagnostics(t *testing.T) {
	t.Run("a clean stop says nothing", func(t *testing.T) {
		withDiagRing(t)

		m := &oidcMethod{method: authOIDC, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}

		codes := make(chan url.Values, 1)

		redirect, stop, err := m.serveLoopbackTo(context.Background(), codes, "")
		if err != nil {
			t.Fatal(err)
		}

		// The sign-in's request, answered synchronously before stop() runs: what stop()
		// owes the browser is a flushed answer, not a request in flight.
		resp, err := (&http.Client{}).Get(redirect + "/?code=good-code&state=s-1") //nolint:noctx
		if err != nil {
			t.Fatalf("the browser's request: %v", err)
		}

		_, _ = io.ReadAll(resp.Body)

		_ = resp.Body.Close()

		<-codes

		stop()

		if _, ok := findDiag(diagEntriesOf(t, 0), "loopback stop"); ok {
			t.Error("a clean stop() recorded an entry")
		}
	})

	t.Run("shutdown error", func(t *testing.T) {
		withDiagRing(t)

		want := errors.New("loopback listener gone")
		srv := &http.Server{Handler: http.NotFoundHandler()}

		stopLoopback(srv, &fakeListener{closeErr: want})()

		cause, text := diagStopCause(t)
		if cause != "shutdown error" || !strings.Contains(text, want.Error()) {
			t.Errorf("cause = %q, err = %q; want the Shutdown error named", cause, text)
		}
	})

	t.Run("deadline reached", func(t *testing.T) {
		withDiagRing(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		stall := make(chan struct{})
		handlerIn := make(chan struct{})
		var stallTaken atomic.Bool

		srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(handlerIn)

			select {
			case <-stall:
				stallTaken.Store(true) // the handler was released, not cut
			case <-time.After(30 * time.Second): // the stuck handler outlives stop()
			}
		})}

		go func() { _ = srv.Serve(ln) }() //nolint:errcheck

		conn, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}

		defer conn.Close() //nolint:errcheck

		if _, err := io.WriteString(conn, "GET /?state=s-1 HTTP/1.1\r\nHost: "+ln.Addr().String()+"\r\n\r\n"); err != nil {
			t.Fatal(err)
		}

		// The handler is on the clock before stop() runs: Shutdown counts it as active
		// from the moment it enters, so the deadline is what stop() has to wait for.
		select {
		case <-handlerIn:
		case <-time.After(3 * time.Second):
			t.Fatal("the handler never started")
		}

		done := make(chan struct{})

		go func() { stopLoopback(srv, ln)(); close(done) }()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("stop() outlived even its own deadline plus a margin")
		}

		close(stall) // let the stuck handler (and the server) go

		cause, text := diagStopCause(t)
		if cause != "deadline reached" || !strings.Contains(text, context.DeadlineExceeded.Error()) {
			t.Errorf("cause = %q, err = %q; want the deadline named", cause, text)
		}

		if stallTaken.Load() {
			t.Error("the deadline released the handler before Close cut it")
		}
	})

	t.Run("already closed", func(t *testing.T) {
		withDiagRing(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		srv := &http.Server{Handler: http.NotFoundHandler()}

		stop := stopLoopback(srv, ln)

		stop() // the first stop is clean: nothing to say
		stop() // the second was the silence of CYR-597

		cause, _ := diagStopCause(t)
		if cause != "already closed" {
			t.Errorf("cause = %q; want %q", cause, "already closed")
		}
	})

	t.Run("listener already closed", func(t *testing.T) {
		withDiagRing(t)

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}

		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}

		// Shutdown on a live server whose listener is gone fails with the listener's
		// "use of closed" error: the third cause, told apart from the Shutdown error above
		// by that error being net.ErrClosed, and recorded without alarming.
		stopLoopback(&http.Server{Handler: http.NotFoundHandler()}, ln)()

		e, ok := findDiag(diagEntriesOf(t, 0), "loopback stop")
		if !ok {
			t.Fatal("no entry for a loopback whose listener was already closed")
		}

		if cause, _ := e.Attrs["cause"].(string); cause != "already closed" {
			t.Errorf("cause = %q; want %q", cause, "already closed")
		}

		if e.Level != "info" {
			t.Errorf("level = %q; want info (a closed listener is benign)", e.Level)
		}
	})
}

// TestLoopbackStopIsStillBounded keeps the CYR-582 promise while stop() learns to talk: the
// probe Close and the extra entry must not make stop() wait - nor a double stop(), which is
// what the ring now calls "already closed".
func TestLoopbackStopIsStillBounded(t *testing.T) {
	withDiagRing(t)

	m := &oidcMethod{method: authOIDC, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}

	codes := make(chan url.Values, 1)

	redirect, stop, err := m.serveLoopbackTo(context.Background(), codes, "")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := (&http.Client{}).Get(redirect + "/?code=good-code&state=s-1") //nolint:noctx
	if err != nil {
		t.Fatalf("the browser's request: %v", err)
	}

	_, _ = io.ReadAll(resp.Body)

	_ = resp.Body.Close()

	<-codes

	first := make(chan struct{})

	go func() { stop(); close(first) }()

	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("stop() hangs")
	}

	second := make(chan struct{})

	go func() { stop(); close(second) }()

	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("the second stop() waits although the server is closed")
	}

	if _, ok := findDiag(diagEntriesOf(t, 0), "loopback stop"); !ok {
		t.Error("the double stop() was not reported as already closed")
	}
}
