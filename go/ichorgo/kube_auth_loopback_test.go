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
// wait for anything else. The stale keep-alive connection a real browser leaves behind is
// dropped at once rather than hold the sign-in's return, and once stop() returns the address
// is free: a later request is refused cleanly, not left hanging.
//
// The old version raced itself: it asked for a request "while stop() ran" through a goroutine
// and a sleep, and lost on a loaded runner (the listener could close first: connection
// refused). There is nothing to race here - what stop() owes is a bounded return, not an
// answer to a request launched on a timer.
func TestLoopbackStopDoesNotHang(t *testing.T) {
	m := &oidcMethod{method: authOIDC, listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1"}

	codes := make(chan url.Values, 1)

	redirect, stop, err := m.serveLoopbackTo(context.Background(), codes, "")
	if err != nil {
		t.Fatal(err)
	}

	// A connection the client leaves idle, the way a browser leaves its page open: the
	// server's Shutdown must close it rather than wait on its keep-alive timeout. Its own
	// client (never closed) keeps the pooled connection idle in the server's set when stop() runs.
	keepClient := &http.Client{} //nolint:noctx

	keep, err := keepClient.Get(redirect + "/?state=s-1") //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.ReadAll(keep.Body)

	_ = keep.Body.Close()

	// The browser's request is answered and its handler returns, exactly as in a sign-in.
	resp, err := (&http.Client{}).Get(redirect + "/?code=good-code&state=s-2") //nolint:noctx
	if err != nil {
		t.Fatalf("the browser's request: %v", err)
	}

	_, _ = io.ReadAll(resp.Body)

	_ = resp.Body.Close()

	select {
	case <-codes:
	case <-time.After(3 * time.Second):
		t.Fatal("the browser's answer did not reach the loopback")
	}

	// stop() returns within its own deadline even with the idle connection still pooled.
	done := make(chan struct{})

	go func() { stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop() hangs on an idle connection or an in-flight request")
	}

	// The listener is closed: a later request is refused, not left hanging.
	if _, err := http.Get(redirect + "/"); err == nil { //nolint:noctx
		t.Error("the listener is still open after stop()")
	}
}
