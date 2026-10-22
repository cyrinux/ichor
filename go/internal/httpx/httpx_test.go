package httpx

import (
	"crypto/tls"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPClientDefaults(t *testing.T) {
	c := New(Opts{})

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T", c.Transport)
	}

	if tr.Proxy != nil {
		t.Error("a proxy is taken from the environment")
	}

	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("TLS setup %+v", tr.TLSClientConfig)
	}

	if tr.TLSHandshakeTimeout != handshakeTimeout || tr.IdleConnTimeout != idleTimeout || c.Timeout != 0 {
		t.Errorf("timeouts: handshake %s, idle %s, request %s", tr.TLSHandshakeTimeout, tr.IdleConnTimeout, c.Timeout)
	}
}

func TestNewHTTPClientOptions(t *testing.T) {
	cfg := BaseTLS(nil, true)
	c := New(Opts{TLS: cfg, Timeout: time.Second, DialTimeout: 3 * time.Second, IdleTimeout: time.Minute})
	tr := c.Transport.(*http.Transport)

	if tr.TLSClientConfig == cfg {
		t.Error("the TLS setup is shared, not cloned")
	}

	if !tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("TLS setup %+v", tr.TLSClientConfig)
	}

	if c.Timeout != time.Second || tr.TLSHandshakeTimeout != 3*time.Second || tr.IdleConnTimeout != time.Minute {
		t.Errorf("timeouts: request %s, handshake %s, idle %s", c.Timeout, tr.TLSHandshakeTimeout, tr.IdleConnTimeout)
	}
}

// redirectServer redirects /from to target and answers 200 on /to.
func redirectServer(t *testing.T, target func(srv *httptest.Server) string) *httptest.Server {
	t.Helper()

	var srv *httptest.Server

	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, target(srv), http.StatusFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func testClient(srv *httptest.Server, follow bool) *http.Client {
	roots := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	return New(Opts{TLS: BaseTLS(roots, false), Timeout: 5 * time.Second, FollowRedirects: follow})
}

func get(t *testing.T, c *http.Client, url string) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close() //nolint:errcheck
	}

	return resp, err
}

func TestNewHTTPClientRefusesRedirects(t *testing.T) {
	srv := redirectServer(t, func(srv *httptest.Server) string { return srv.URL + "/to" })

	resp, err := get(t, testClient(srv, false), srv.URL+"/from")
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusFound {
		t.Errorf("status %d, want the redirect itself", resp.StatusCode)
	}
}

func TestNewHTTPClientFollowsHTTPSRedirects(t *testing.T) {
	srv := redirectServer(t, func(srv *httptest.Server) string { return srv.URL + "/to" })

	resp, err := get(t, testClient(srv, true), srv.URL+"/from")
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status %d, want the target's 200", resp.StatusCode)
	}
}

func TestNewHTTPClientRefusesPlainHTTPRedirects(t *testing.T) {
	srv := redirectServer(t, func(*httptest.Server) string { return "http://127.0.0.1:1/to" })

	_, err := get(t, testClient(srv, true), srv.URL+"/from")
	if !errors.Is(err, ErrInsecureRedirect) {
		t.Errorf("err %v, want %v", err, ErrInsecureRedirect)
	}
}

// scannedDirs are the packages whose outbound clients must all come from New and BaseTLS: add
// each package that moves out of ichorgo and talks HTTP.
var scannedDirs = []string{"../../ichorgo"}

// TestOutboundClientsUseTheHelper fails on any http.Client, http.Transport or tls.Config built
// in scannedDirs, and on the default client or transport, whose redirect, proxy and TLS
// settings are not the core's.
func TestOutboundClientsUseTheHelper(t *testing.T) {
	fset := token.NewFileSet()

	var names []string

	for _, dir := range scannedDirs {
		found, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}

		if len(found) == 0 {
			t.Fatalf("no Go file in %s", dir)
		}

		names = append(names, found...)
	}

	literals := map[string]bool{"http.Client": true, "http.Transport": true, "tls.Config": true}
	uses := map[string]bool{
		"http.DefaultClient": true, "http.DefaultTransport": true,
		"http.Get": true, "http.Post": true, "http.Head": true, "http.PostForm": true,
	}

	files := 0

	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		files++

		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if what := selectorName(n.Type); literals[what] {
					t.Errorf("%s: a %s literal, use httpx.New / httpx.BaseTLS", fset.Position(n.Pos()), what)
				}
			case *ast.SelectorExpr:
				if what := selectorName(n); uses[what] {
					t.Errorf("%s: %s, use httpx.New", fset.Position(n.Pos()), what)
				}
			}

			return true
		})
	}

	if files < 100 {
		t.Fatalf("only %d files parsed", files)
	}
}

// selectorName is "pkg.Name" for a qualified identifier, "" otherwise.
func selectorName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}

	return pkg.Name + "." + sel.Sel.Name
}
