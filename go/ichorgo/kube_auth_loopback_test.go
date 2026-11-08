package ichorgo

import (
	"context"
	"fmt"
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

	// One connection the browser keeps alive: Shutdown must close it, not wait on it. Its own
	// client (never closed) keeps the pooled connection idle in the server's set when stop() runs.
	keepClient := &http.Client{} //nolint:noctx

	keep, err := keepClient.Get(redirect + "/?state=s-1") //nolint:noctx
	if err != nil {
		t.Fatal(err)
	}

	_, _ = io.ReadAll(keep.Body)

	_ = keep.Body.Close()

	// The browser's request goes first and its answer reaches the test before anything else:
	// codes is buffered, so the handler returns even though nothing reads the body yet.
	inflight := make(chan string, 1)

	resp, err := (&http.Client{}).Get(redirect + "/?code=good-code&state=s-2") //nolint:noctx
	if err != nil {
		t.Fatalf("the browser's request: %v", err)
	}

	inflight <- fmt.Sprintf("response %v", resp.StatusCode)

	select {
	case <-codes:
	case <-time.After(3 * time.Second):
		t.Fatal("the browser's answer did not reach the loopback")
	}

	// The request that lands just after stop() was asked is answered before the server goes,
	// so it starts while stop() still holds the listener open.
	stopped := make(chan error, 1)

	go func() {
		time.Sleep(20 * time.Millisecond)

		late, err := (&http.Client{}).Get(redirect + "/favicon.ico") //nolint:noctx
		if err != nil {
			stopped <- err

			return
		}

		_, _ = io.ReadAll(late.Body)

		_ = late.Body.Close()

		if late.StatusCode != http.StatusNotFound {
			stopped <- fmt.Errorf("favicon: %d", late.StatusCode)

			return
		}

		stopped <- nil
	}()

	done := make(chan struct{})

	go func() { stop(); close(done) }()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop() hangs on an idle connection or an in-flight request")
	}

	if got := <-inflight; got != "response 200" {
		t.Errorf("the browser's answer: %s", got)
	}

	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("the request that arrived during stop(): %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the request that arrived during stop() is never answered")
	}

	// stop() returned: the address is free again, so a later request is refused, not left hanging.
	if _, err := http.Get(redirect + "/"); err == nil { //nolint:noctx
		t.Error("the listener is still open after stop()")
	}
}
