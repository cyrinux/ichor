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
	"sync"
	"testing"

	"github.com/cyrinux/ichor/go/kube"
	"golang.org/x/net/websocket"
)

// recordingProxy answers CONNECT by piping to the target, and remembers what it was asked for.
type recordingProxy struct {
	mu   sync.Mutex
	seen []string
}

func (p *recordingProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.seen = append(p.seen, r.Method+" "+r.Host)
	p.mu.Unlock()

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

	done := make(chan struct{}, 2)

	go func() { _, _ = io.Copy(conn, upstream); done <- struct{}{} }()
	go func() { _, _ = io.Copy(upstream, conn); done <- struct{}{} }()

	<-done
}

func (p *recordingProxy) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.seen)
}

// proxiedKubeconfig is a kubeconfig for srv through the proxy at proxyHost.
func proxiedKubeconfig(srv *httptest.Server, proxyHost string) string {
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})

	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: admin@test
clusters:
- name: test
  cluster: {server: %q, certificate-authority-data: %s, proxy-url: "http://%s"}
users:
- {name: admin, user: {token: abc}}
contexts:
- {name: admin@test, context: {cluster: test, user: admin}}
`, srv.URL, base64.StdEncoding.EncodeToString(ca), proxyHost)
}

// Every connection of a proxied cluster goes through its proxy: the requests of the client and,
// outside it, the WebSocket streams of exec, attach, port-forward and a VM console.
func TestProxiedClusterConnectsThroughTheProxy(t *testing.T) {
	echoed := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{}")) })
	mux.Handle("/stream", websocket.Server{
		// Of the protocols a client offers, the server has to choose one.
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			cfg.Protocol = cfg.Protocol[:1]

			return nil
		},
		Handler: func(ws *websocket.Conn) {
			var message string
			if err := websocket.Message.Receive(ws, &message); err == nil {
				echoed <- message
			}
		},
	})

	api := httptest.NewUnstartedServer(mux)
	api.StartTLS()
	t.Cleanup(api.Close)

	proxy := &recordingProxy{}

	front := httptest.NewServer(proxy)
	t.Cleanup(front.Close)

	creds, err := parseKubeconfig(proxiedKubeconfig(api, strings.TrimPrefix(front.URL, "http://")))
	if err != nil {
		t.Fatal(err)
	}

	k := newKubeClient(creds.server, creds)
	t.Cleanup(k.close)

	if status, _, _, err := k.getRaw(t.Context(), "/version", nil); err != nil || status != http.StatusOK {
		t.Fatalf("read through the proxy: status %d, err %v", status, err)
	}

	if proxy.count() != 1 {
		t.Fatalf("the proxy saw %v after the read", proxy.seen)
	}

	ws, err := k.dialStream(t.Context(), "/stream")
	if err != nil {
		t.Fatalf("stream through the proxy: %v", err)
	}
	defer ws.Close() //nolint:errcheck

	if err := websocket.Message.Send(ws, "hello"); err != nil {
		t.Fatal(err)
	}

	if got := <-echoed; got != "hello" {
		t.Errorf("the server got %q", got)
	}

	if proxy.count() != 2 {
		t.Errorf("the proxy saw %v after the stream", proxy.seen)
	}

	cfg, err := k.streamConfig(t.Context(), "/stream", nil)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := k.dialAPIServer(t.Context(), cfg)
	if err != nil {
		t.Fatalf("console dial through the proxy: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	if proxy.count() != 3 {
		t.Errorf("the proxy saw %v after the console dial", proxy.seen)
	}
}

func TestProxyRefusalReadsAsOneSentence(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	t.Cleanup(refusing.Close)

	api := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(api.Close)

	host := strings.TrimPrefix(refusing.URL, "http://")

	creds, err := parseKubeconfig(proxiedKubeconfig(api, host))
	if err != nil {
		t.Fatal(err)
	}

	k := newKubeClient(creds.server, creds)
	t.Cleanup(k.close)

	want := "The proxy " + host + " refused the connection"

	err = k.probe(t.Context())
	if err == nil || kubeTransportError(err) != want {
		t.Errorf("read: %v, want %q", err, want)
	}

	if !kube.IsTransportError(err) {
		t.Errorf("not a transport error: %v", err)
	}

	_, err = k.dialStream(t.Context(), "/stream")
	if err == nil || kubeTransportError(err) != want {
		t.Errorf("stream: %v, want %q", err, want)
	}
}

func TestParseKubeconfigRefusesABadProxy(t *testing.T) {
	api := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(api.Close)

	config := strings.ReplaceAll(proxiedKubeconfig(api, "x"), "http://x", "ftp://u:secret@x")

	_, err := parseKubeconfig(config)
	if err == nil {
		t.Fatal("no error")
	}

	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the error quotes the password: %v", err)
	}
}

func TestNoProxyURLKeepsTheClientDirect(t *testing.T) {
	creds, err := parseKubeconfig(testKubeconfigNoProxy)
	if err != nil {
		t.Fatal(err)
	}

	if creds.proxy != nil {
		t.Errorf("proxy %v", creds.proxy)
	}

	k := newKubeClient(creds.server, creds)
	if k.proxy != nil {
		t.Error("a stream dialer is set")
	}

	transport, ok := k.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Errorf("transport %+v", k.http.Transport)
	}
}

// Screenshot mode hides the proxy's host like the API server's.
func TestParseKubeconfigMasksTheProxy(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	config := strings.Replace(testKubeconfigNoProxy, `server: "https://k8s.example.org:6443"`,
		`server: "https://k8s.example.org:6443", proxy-url: "socks5://u:secret@bastion.example.org:1080"`, 1)

	out, err := ParseKubeconfig(config)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "bastion.example.org") || strings.Contains(out, "secret") {
		t.Errorf("the summary shows the proxy: %s", out)
	}

	if !strings.Contains(out, `"proxyURL":"socks5://`) {
		t.Errorf("no proxy in the summary: %s", out)
	}
}

const testKubeconfigNoProxy = `apiVersion: v1
kind: Config
current-context: admin@test
clusters:
- {name: test, cluster: {server: "https://k8s.example.org:6443"}}
users:
- {name: admin, user: {token: abc}}
contexts:
- {name: admin@test, context: {cluster: test, user: admin}}
`
