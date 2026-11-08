package httpx

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseProxyURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"http://bastion.example.org:3128":        true,
		"https://bastion.example.org":            true,
		"socks5://127.0.0.1:1080":                true,
		"socks5h://u:secret@bastion.example.org": true,
		"ftp://bastion.example.org":              false,
		"bastion.example.org":                    false,
		"http://":                                false,
		"socks5://:1080":                         false,
		"http://u:secret@bad host":               false,
	} {
		_, err := ParseProxyURL(raw)
		if (err == nil) != ok {
			t.Errorf("%s: err %v, want ok %v", raw, err, ok)
		}

		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("%s: the error quotes the password: %v", raw, err)
		}
	}
}

func TestProxyDisplayDropsCredentials(t *testing.T) {
	u, err := ParseProxyURL("socks5h://u:secret@bastion.example.org:1080/ignored")
	if err != nil {
		t.Fatal(err)
	}

	if got := ProxyDisplay(u); got != "socks5h://bastion.example.org:1080" {
		t.Errorf("display %q", got)
	}

	if password, _ := u.User.Password(); password != "secret" {
		t.Errorf("the transport lost the password: %q", password)
	}
}

// connectProxy is a forward proxy: it records the CONNECT requests and pipes each to its target.
type connectProxy struct {
	mu       sync.Mutex
	targets  []string
	auth     []string
	refusing int
}

func (p *connectProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.targets = append(p.targets, r.Method+" "+r.Host)
	p.auth = append(p.auth, r.Header.Get("Proxy-Authorization"))
	refusing := p.refusing
	p.mu.Unlock()

	if refusing != 0 {
		w.WriteHeader(refusing)

		return
	}

	upstream, err := net.Dial("tcp", r.Host)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)

		return
	}
	defer upstream.Close() //nolint:errcheck

	conn, buf, err := http.NewResponseController(w).Hijack()
	if err != nil {
		return
	}
	defer conn.Close() //nolint:errcheck

	if _, err := buf.WriteString("HTTP/1.1 200 OK\r\n\r\n"); err != nil || buf.Flush() != nil {
		return
	}

	pipe(conn, upstream)
}

// pipe copies both ways until one side closes.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)

	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()

	<-done
}

func (p *connectProxy) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string{}, p.targets...)
}

func clusterServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("from the cluster"))
	}))
	t.Cleanup(srv.Close)

	return srv
}

func serverOpts(srv *httptest.Server) Opts {
	roots := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs

	return Opts{TLS: BaseTLS(roots, false), Timeout: 5 * time.Second}
}

func readBody(t *testing.T, c *http.Client, target string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func mustParseProxy(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := ParseProxyURL(raw)
	if err != nil {
		t.Fatal(err)
	}

	return u
}

func TestClientGoesThroughHTTPProxy(t *testing.T) {
	srv := clusterServer(t)
	proxy := &connectProxy{}

	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)

	opts := serverOpts(srv)
	opts.ProxyURL = mustParseProxy(t, "http://user:secret@"+strings.TrimPrefix(front.URL, "http://"))

	if got := readBody(t, New(opts), srv.URL); got != "from the cluster" {
		t.Errorf("body %q", got)
	}

	target := "CONNECT " + strings.TrimPrefix(srv.URL, "https://")
	if seen := proxy.seen(); len(seen) != 1 || seen[0] != target {
		t.Errorf("the proxy saw %v, want %q", seen, target)
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
	if len(proxy.auth) != 1 || proxy.auth[0] != want {
		t.Errorf("Proxy-Authorization %v", proxy.auth)
	}
}

// The cluster's CA must not be what an https proxy is checked against. httptest serves one
// certificate for both servers, so the cluster's roots would accept the proxy: without roots of
// its own, the hop has to fail anyway.
func TestHTTPSProxyHopHasItsOwnRoots(t *testing.T) {
	srv := clusterServer(t)
	proxy := &connectProxy{}

	front := httptest.NewUnstartedServer(proxy)
	front.StartTLS()
	t.Cleanup(front.Close)

	opts := serverOpts(srv)
	opts.ProxyURL = mustParseProxy(t, front.URL)
	opts.proxyTLS = BaseTLS(front.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs, false)

	if got := readBody(t, New(opts), srv.URL); got != "from the cluster" {
		t.Errorf("body %q", got)
	}

	if len(proxy.seen()) != 1 {
		t.Errorf("the proxy saw %v", proxy.seen())
	}

	opts.proxyTLS = nil

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = New(opts).Do(req)

	var proxyErr *ProxyError
	if !errors.As(err, &proxyErr) {
		t.Fatalf("err %v, want a ProxyError", err)
	}

	if len(proxy.seen()) != 1 {
		t.Errorf("a CONNECT went out over an unverified proxy: %v", proxy.seen())
	}
}

func TestProxyRefusalIsOneSentence(t *testing.T) {
	front := httptest.NewServer(&connectProxy{refusing: http.StatusProxyAuthRequired})
	t.Cleanup(front.Close)

	host := strings.TrimPrefix(front.URL, "http://")
	dialer := NewProxyDialer(mustParseProxy(t, "http://user:secret@"+host), time.Second)

	_, err := dialer.DialContext(t.Context(), "tcp", "k8s.example.org:6443")

	var proxyErr *ProxyError
	if !errors.As(err, &proxyErr) {
		t.Fatalf("err %v, want a ProxyError", err)
	}

	if want := "The proxy " + host + " refused the connection"; err.Error() != want {
		t.Errorf("message %q, want %q", err, want)
	}
}

func TestProxyDownIsAProxyError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	for _, scheme := range []string{"http", "socks5"} {
		_, err := NewProxyDialer(mustParseProxy(t, scheme+"://"+addr), time.Second).DialContext(t.Context(), "tcp", "k8s.example.org:6443")

		var proxyErr *ProxyError
		if !errors.As(err, &proxyErr) || proxyErr.Host != addr {
			t.Errorf("%s: err %v, want a ProxyError for %s", scheme, err, addr)
		}
	}
}

// socksProxy is the no-auth CONNECT of RFC 1928, what `ssh -D` speaks. It reports the target.
func socksProxy(t *testing.T) (addr string, targets <-chan string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	seen := make(chan string, 1)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go serveSOCKS(conn, seen)
		}
	}()

	return listener.Addr().String(), seen
}

func serveSOCKS(conn net.Conn, seen chan<- string) {
	defer conn.Close() //nolint:errcheck

	r := bufio.NewReader(conn)
	head := make([]byte, 2)

	if _, err := io.ReadFull(r, head); err != nil {
		return
	}

	if _, err := io.CopyN(io.Discard, r, int64(head[1])); err != nil {
		return
	}

	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}

	target, err := readSOCKSTarget(r)
	if err != nil {
		return
	}

	seen <- target

	upstream, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer upstream.Close() //nolint:errcheck

	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	pipe(conn, upstream)
}

func readSOCKSTarget(r *bufio.Reader) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(r, head); err != nil {
		return "", err
	}

	var host string

	switch head[3] {
	case 1:
		ip := make(net.IP, net.IPv4len)
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}

		host = ip.String()
	case 3:
		size, err := r.ReadByte()
		if err != nil {
			return "", err
		}

		name := make([]byte, size)
		if _, err := io.ReadFull(r, name); err != nil {
			return "", err
		}

		host = string(name)
	default:
		return "", errors.New("unexpected address type")
	}

	port := make([]byte, 2)
	if _, err := io.ReadFull(r, port); err != nil {
		return "", err
	}

	return net.JoinHostPort(host, strconv.Itoa(int(port[0])<<8|int(port[1]))), nil
}

func TestClientGoesThroughSOCKS5(t *testing.T) {
	srv := clusterServer(t)

	for _, scheme := range []string{"socks5", "socks5h"} {
		addr, targets := socksProxy(t)

		opts := serverOpts(srv)
		opts.ProxyURL = mustParseProxy(t, scheme+"://"+addr)

		if got := readBody(t, New(opts), srv.URL); got != "from the cluster" {
			t.Errorf("%s: body %q", scheme, got)
		}

		if got, want := <-targets, strings.TrimPrefix(srv.URL, "https://"); got != want {
			t.Errorf("%s: the proxy was asked for %q, want %q", scheme, got, want)
		}
	}
}

func TestNoProxyDialsDirectly(t *testing.T) {
	c := New(Opts{})
	tr := c.Transport.(*http.Transport)

	if tr.Proxy != nil {
		t.Error("a proxy is set")
	}

	// Set for the Kubernetes client only, and never through the environment.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	srv := clusterServer(t)
	if got := readBody(t, New(serverOpts(srv)), srv.URL); got != "from the cluster" {
		t.Errorf("body %q", got)
	}
}
