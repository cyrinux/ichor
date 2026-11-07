package ichorgo

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestLoopbackKeepsTheDonePage is the CYR-582 regression: the done page must reach the
// browser even when the token exchange is faster than the handler's return. Before the fix
// stop() was srv.Close(), which cut the in-flight response as soon as the code came back:
// the browser (gcloudBrowserFollow in TestGKEGcloudSignIn) read an EOF on a sign-in that
// had worked. It fails on the old code, where the response body is truncated to nothing.
func TestLoopbackKeepsTheDonePage(t *testing.T) {
	const page = `<!doctype html>signed in, nothing more to do`

	m := &oidcMethod{method: authOIDC, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}

	codes := make(chan url.Values, 1)

	redirect, stop, err := m.serveLoopbackTo(context.Background(), codes, page)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(redirect + "/?code=good-code&state=s-1") //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}

	body, readErr := io.ReadAll(resp.Body)

	_ = resp.Body.Close()

	// The exchange answers, browserSignIn returns and its deferred stop() runs while this
	// response is still being read: with the old Close the connection breaks mid-answer.
	<-codes
	stop()

	if readErr != nil {
		t.Fatalf("the browser loses the done page: %v", readErr)
	}

	if !strings.Contains(string(body), "signed in, nothing more to do") {
		t.Fatalf("done page: %q", string(body))
	}
}

// TestLoopbackStopDoesNotHang is the other half: stop() waits for the answer, so it must not
// wait for anything else. An in-flight request (the browser's, whose handler answers first)
// is finished, a stale keep-alive connection is dropped at once rather than hold the
// sign-in's return, and the listener stays open until stop() returns so a late request
// (favicon) is refused cleanly, not mid-connection.
func TestLoopbackStopDoesNotHang(t *testing.T) {
	m := &oidcMethod{method: authOIDC, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}

	codes := make(chan url.Values, 1)

	redirect, stop, err := m.serveLoopbackTo(context.Background(), codes, "")
	if err != nil {
		t.Fatal(err)
	}

	// One connection the browser keeps alive: Shutdown must close it, not wait on it.
	keep, err := (&http.Client{}).Get(redirect + "/?state=s-1") //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.ReadAll(keep.Body)

	_ = keep.Body.Close()

	// Keep-alive: no Close(), so the connection stays in the pool and in the server's idle set.

	inflight := make(chan int, 1)
	stopAsked := make(chan struct{})

	go func() {
		// The browser's request: answered (200) and handed over, its response still in flight
		// when stop() is asked. Read after stop() is called (below): with the old Close the
		// body read fails with an EOF on a request the server had already answered.
		resp, err := http.Get(redirect + "/?code=good-code&state=s-2") //nolint:noctx
		if err != nil {
			t.Error(err)

			inflight <- 0

			return
		}

		select {
		case <-stopAsked:
		default:
		}

		_, _ = io.ReadAll(resp.Body)

		_ = resp.Body.Close()

		inflight <- resp.StatusCode
	}()

	select {
	case <-codes:
	case <-time.After(3 * time.Second):
		t.Fatal("the browser's answer did not reach the loopback")
	}

	stopped := make(chan struct{})

	go func() {
		// The request that lands just after stop() was asked is answered before the server goes.
		time.Sleep(20 * time.Millisecond)

		resp, err := http.Get(redirect + "/favicon.ico") //nolint:noctx
		if err != nil {
			t.Error(err)

			return
		}

		_, _ = io.ReadAll(resp.Body)

		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("favicon: %d", resp.StatusCode)
		}

		close(stopped)
	}()

	done := make(chan struct{})

	go func() { close(stopAsked); stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop() hangs on an idle connection or an in-flight request")
	}

	if code := <-inflight; code != http.StatusOK {
		t.Errorf("the browser's answer: %d", code)
	}

	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("the request that arrived during stop() is never answered")
	}

	// stop() returned: the address is free again, so a later request is refused, not left hanging.
	if _, err := http.Get(redirect + "/"); err == nil { //nolint:noctx
		t.Error("the listener is still open after stop()")
	}
}
