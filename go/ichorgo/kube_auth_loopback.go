package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"
)

// The loopback server a browser sign-in comes back to: OIDC (serveLoopback here) and the
// Teleport callback (serveTeleportLoopback) both listen through listenFirst and stop
// through stopLoopback, so the CYR-582 flush-on-stop and the CYR-597 shutdown diagnostics
// hold for either.

// serveLoopback listens on the first free m.listen address and hands each answer with a
// state to codes; it returns the redirect URI and how to stop.
func (m *oidcMethod) serveLoopback(ctx context.Context, codes chan<- url.Values) (string, func(), error) {
	return m.serveLoopbackTo(ctx, codes, "")
}

// serveLoopbackTo is serveLoopback with a donePage an app-side test can check: what the
// browser is left with once its redirect is answered (empty: signInDonePage).
func (m *oidcMethod) serveLoopbackTo(ctx context.Context, codes chan<- url.Values, donePage string) (string, func(), error) {
	if donePage == "" {
		donePage = signInDonePage
	}

	ln, err := listenFirst(ctx, m.listen)
	if err != nil {
		return "", nil, err
	}

	_, port, _ := net.SplitHostPort(ln.Addr().String())

	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") == "" {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, donePage)

		select {
		case codes <- r.URL.Query():
		default:
		}
	})}

	go func() { _ = srv.Serve(ln) }() //nolint:errcheck

	stop := stopLoopback(srv, ln)

	return "http://" + net.JoinHostPort(m.redirectHost, port), stop, nil
}

// stopLoopback closes a loopback server so that the response being written when the sign-in
// is received is fully flushed first: Shutdown waits for active handlers to finish their
// answer, Close would cut the connection and leave the browser with an EOF on a sign-in that
// worked. A handler stuck on anything else is given loopbackStopTimeout, then closed at once.
//
// When the graceful close does not work, the diagnostics ring says which of the three
// causes it was (CYR-597): Shutdown returned an error, the deadline was reached, or the
// server or listener was already closed - the last one is not worth an alarm, a double
// stop() is benign.
func stopLoopback(srv *http.Server, ln net.Listener) func() {
	return func() {
		// "already closed" is known before Shutdown runs: on a closed server Shutdown only
		// returns ErrServerClosed, so it has nothing left to diagnose. A live server whose
		// listener is gone still reaches Shutdown below, where the listener's "use of
		// closed" error names the cause. And once Close has run, Shutdown has nothing to
		// wait for: the second stop() of a double stop returns at once rather than holding
		// the sign-in's return.
		alreadyClosed := errors.Is(srv.Close(), net.ErrClosed)

		if alreadyClosed {
			_ = ln.Close() //nolint:errcheck

			diag(slog.LevelInfo, "auth", "loopback stop", nil, "cause", "already closed", "stage", "server")

			return
		}

		started := time.Now()

		ctx, cancel := context.WithTimeout(context.Background(), loopbackStopTimeout)
		defer cancel()

		err := srv.Shutdown(ctx)

		_ = srv.Close() //nolint:errcheck
		_ = ln.Close() //nolint:errcheck

		// A stuck handler is closed at once after the deadline, so Shutdown can still return
		// nothing here: a stop that took the whole budget without a clean early return was
		// cut by the deadline, whether or not Shutdown says so (Go has no documented promise
		// about that), and the same budget rules out a clock that merely ran late.
		if err == nil {
			if time.Since(started) >= loopbackStopTimeout {
				diag(slog.LevelWarn, "auth", "loopback stop", context.DeadlineExceeded, "cause", "deadline reached")
			}

			return
		}

		switch {
		case errors.Is(err, net.ErrClosed):
			diag(slog.LevelInfo, "auth", "loopback stop", nil, "cause", "already closed", "stage", "listener")
		default:
			diag(slog.LevelWarn, "auth", "loopback stop", err, "cause", "shutdown error")
		}
	}
}

// loopbackStopTimeout is how long stopLoopback gives Shutdown before closing at once.
const loopbackStopTimeout = time.Second

// listenFirst listens on the first free address of addrs (kubelogin tries them in order).
func listenFirst(ctx context.Context, addrs []string) (net.Listener, error) {
	var errs []error

	for _, addr := range addrs {
		ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
		if err == nil {
			return ln, nil
		}

		errs = append(errs, err)
	}

	return nil, fmt.Errorf("no free sign-in address: %w", errors.Join(errs...))
}

const signInDonePage = `<!doctype html><meta name="viewport" content="width=device-width">` +
	`<body style="font-family:sans-serif;text-align:center;padding-top:20vh">` +
	`<h1>Signed in</h1><p>You can go back to Ichor.</p>`
