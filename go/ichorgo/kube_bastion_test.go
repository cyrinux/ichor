package ichorgo

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/websocket"
)

// testBastion is an in-process SSH server that forwards direct-tcpip channels. It resolves
// every *.test name to 127.0.0.1: an API server named api.internal.test is only reachable
// through it, as behind a real jump host.
type testBastion struct {
	listener net.Listener
	hostKey  atomic.Pointer[ssh.Signer]
	clientPK ssh.PublicKey
	// handshakes counts SSH connections; forwards the channels opened.
	handshakes, forwards atomic.Int32

	mu    sync.Mutex
	conns []net.Conn
}

func newTestBastion(t *testing.T, clientPK ssh.PublicKey) *testBastion {
	t.Helper()

	b := &testBastion{clientPK: clientPK}
	b.rotateHostKey(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	b.listener = listener
	t.Cleanup(func() {
		_ = listener.Close()
		b.mu.Lock()
		for _, c := range b.conns {
			_ = c.Close()
		}
		b.mu.Unlock()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			b.mu.Lock()
			b.conns = append(b.conns, conn)
			b.mu.Unlock()

			go b.serve(conn)
		}
	}()

	return b
}

func testSSHSigner(t *testing.T) (ssh.Signer, ed25519.PrivateKey) {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}

	return signer, private
}

// rotateHostKey gives the bastion a new host key (a reinstall, or a machine in the middle).
func (b *testBastion) rotateHostKey(t *testing.T) {
	t.Helper()

	signer, _ := testSSHSigner(t)
	b.hostKey.Store(&signer)
}

func (b *testBastion) pin() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey((*b.hostKey.Load()).PublicKey())))
}

func (b *testBastion) addr() string { return b.listener.Addr().String() }

func (b *testBastion) serve(conn net.Conn) {
	config := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		if string(key.Marshal()) == string(b.clientPK.Marshal()) {
			return &ssh.Permissions{}, nil
		}

		return nil, errors.New("unknown key")
	}}
	config.AddHostKey(*b.hostKey.Load())

	sshConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close() //nolint:errcheck

	b.handshakes.Add(1)

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}

		if newChannel.ChannelType() != "direct-tcpip" || ssh.Unmarshal(newChannel.ExtraData(), &target) != nil {
			_ = newChannel.Reject(ssh.Prohibited, "no")

			continue
		}

		host := target.Host
		if strings.HasSuffix(host, ".test") {
			host = "127.0.0.1"
		}

		upstream, err := net.Dial("tcp", net.JoinHostPort(host, strconv.FormatUint(uint64(target.Port), 10)))
		if err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, "cannot reach")

			continue
		}

		channel, reqs, err := newChannel.Accept()
		if err != nil {
			_ = upstream.Close()

			continue
		}

		b.forwards.Add(1)

		go ssh.DiscardRequests(reqs)
		go func() {
			done := make(chan struct{}, 2)

			go func() { _, _ = io.Copy(channel, upstream); _ = channel.CloseWrite(); done <- struct{}{} }()
			go func() { _, _ = io.Copy(upstream, channel); done <- struct{}{} }()

			<-done
			_ = channel.Close()
			_ = upstream.Close()
		}()
	}
}

// testClientKey is a fresh private key (OpenSSH PEM) and its public key.
func testClientKey(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()

	signer, private := testSSHSigner(t)

	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(block)), signer.PublicKey()
}

// bastionAPI is an API server whose kubeconfig names it api.internal.test (unresolvable: only
// the bastion reaches it), with a WebSocket echo for the streams.
func bastionAPI(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()

	echoed := make(chan string, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"gitVersion":"v1.33.0"}`)) })
	mux.HandleFunc("/api/v1/namespaces", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"kube-system"}}]}`))
	})
	mux.Handle("/stream", websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			cfg.Protocol = cfg.Protocol[:1]

			return nil
		},
		Handler: func(ws *websocket.Conn) {
			var message string
			for websocket.Message.Receive(ws, &message) == nil {
				echoed <- message
				_ = websocket.Message.Send(ws, message)
			}
		},
	})

	api := httptest.NewUnstartedServer(mux)
	api.StartTLS()
	t.Cleanup(api.Close)

	return api, echoed
}

// bastionKubeconfig names api at api.internal.test with ca (its own CA by default), verified
// as example.com (a name of httptest's certificate).
func bastionKubeconfig(api *httptest.Server, ca []byte, extra string) string {
	if ca == nil {
		ca = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: api.Certificate().Raw})
	}

	_, port, _ := net.SplitHostPort(api.Listener.Addr().String())

	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: admin@internal
clusters:
- name: internal
  cluster: {server: "https://api.internal.test:%s", certificate-authority-data: %s, tls-server-name: example.com%s}
users:
- {name: admin, user: {token: abc}}
contexts:
- {name: admin@internal, context: {cluster: internal, user: admin}}
`, port, base64.StdEncoding.EncodeToString(ca), extra)
}

const bastionContextName = "admin@internal"

func setBastionJSON(t *testing.T, fields map[string]string) string {
	t.Helper()

	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// bastionFixture is an API server behind a bastion, its stored kubeconfig, and the bastion set.
type bastionFixture struct {
	api     *httptest.Server
	echoed  chan string
	bastion *testBastion
	store   *memAuthStore
	config  string
	key     string
}

func newBastionFixture(t *testing.T) *bastionFixture {
	t.Helper()

	store := withAuthStore(t)
	api, echoed := bastionAPI(t)
	key, pk := testClientKey(t)
	b := newTestBastion(t, pk)
	config := bastionKubeconfig(api, nil, "")

	t.Cleanup(func() { kubeClients.forgetConfig(config, bastionContextName) })

	out, err := KubeSetBastion(config, bastionContextName, setBastionJSON(t, map[string]string{
		"host": b.addr(), "user": "jump", "privateKey": key, "hostKey": b.pin(),
	}))
	if err != nil {
		t.Fatalf("KubeSetBastion: %v", err)
	}

	if !strings.Contains(out, `"configured":true`) {
		t.Fatalf("KubeSetBastion answered %s", out)
	}

	return &bastionFixture{api: api, echoed: echoed, bastion: b, store: store, config: config, key: key}
}

func (f *bastionFixture) client(t *testing.T) *kubeClient {
	t.Helper()

	k, _, err := kubeClients.get(kubeTarget{config: f.config, context: bastionContextName})
	if err != nil {
		t.Fatalf("open through the bastion: %v", err)
	}

	return k
}

// The cluster is only reachable through the bastion: reads, a stream (what exec and attach
// use) and a raw API server connection (port-forward, a VM console) all go through it, on one
// SSH connection, with the kubeconfig's CA and server name still verified.
func TestBastionCarriesRESTAndStreams(t *testing.T) {
	f := newBastionFixture(t)

	out, err := KubeNamespaces(f.config, bastionContextName, "")
	if err != nil || !strings.Contains(out, "kube-system") {
		t.Fatalf("namespaces through the bastion: %s, %v", out, err)
	}

	k := f.client(t)

	ws, err := k.dialStream(t.Context(), "/stream")
	if err != nil {
		t.Fatalf("stream through the bastion: %v", err)
	}
	defer ws.Close() //nolint:errcheck

	if err := websocket.Message.Send(ws, "hello"); err != nil {
		t.Fatal(err)
	}

	if got := <-f.echoed; got != "hello" {
		t.Errorf("the server got %q", got)
	}

	var echo string
	if err := websocket.Message.Receive(ws, &echo); err != nil || echo != "hello" {
		t.Fatalf("echo %q, %v", echo, err)
	}

	cfg, err := k.streamConfig(t.Context(), "/stream", nil)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := k.dialAPIServer(t.Context(), cfg)
	if err != nil {
		t.Fatalf("raw connection through the bastion: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	// A deadline on a stream through the hop ends a read that gets nothing (kube_exec.go sets
	// one on every exec).
	_ = ws.SetDeadline(time.Now().Add(100 * time.Millisecond))

	var message string

	err = websocket.Message.Receive(ws, &message)

	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("read past the deadline: %v, want a timeout", err)
	}

	// The SSH connection of KubeSetBastion's check, then the client's: two, whatever the dials.
	if n := f.bastion.handshakes.Load(); n != 2 {
		t.Errorf("%d SSH connections, want 2", n)
	}

	if f.bastion.forwards.Load() < 4 {
		t.Errorf("%d forwarded connections, want at least 4", f.bastion.forwards.Load())
	}
}

func TestBastionKeepsTLSVerification(t *testing.T) {
	f := newBastionFixture(t)

	// Every httptest server has the same certificate: another CA is made here.
	wrongCA := selfSignedCA(t)

	// The same cluster (same fingerprint inputs aside from the CA) is a new context: give it
	// the bastion, then expect TLS to refuse the API server.
	for name, config := range map[string]string{
		"wrong CA":          bastionKubeconfig(f.api, wrongCA, ""),
		"wrong server name": strings.Replace(f.config, "tls-server-name: example.com", "tls-server-name: other.example.net", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() { kubeClients.forgetConfig(config, bastionContextName) })

			bastion, _ := loadBastion(mustBastionKey(t, f.config))
			if err := saveBastionFor(t, config, bastion); err != nil {
				t.Fatal(err)
			}

			_, err := KubeNamespaces(config, bastionContextName, "")
			if err == nil || !strings.Contains(err.Error(), "certificate") {
				t.Fatalf("read with %s: %v, want a certificate error", name, err)
			}
		})
	}
}

func selfSignedCA(t *testing.T) []byte {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "other-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func mustBastionKey(t *testing.T, config string) string {
	t.Helper()

	target, err := bastionContext(config, bastionContextName)
	if err != nil {
		t.Fatal(err)
	}

	return target.key
}

// saveBastionFor stores c for the context of config, without KubeSetBastion's check (which a
// wrong CA would not fail: it does not speak TLS).
func saveBastionFor(t *testing.T, config string, c httpx.BastionConfig) error {
	t.Helper()

	kubeAuth.save(mustBastionKey(t, config), bastionState(c))

	return nil
}

func TestBastionChangedHostKeyStopsEveryCall(t *testing.T) {
	f := newBastionFixture(t)

	if _, err := KubeNamespaces(f.config, bastionContextName, ""); err != nil {
		t.Fatal(err)
	}

	kubeClients.forgetConfig(f.config, bastionContextName)
	f.bastion.rotateHostKey(t)

	_, err := KubeNamespaces(f.config, bastionContextName, "")
	if err == nil || !strings.HasPrefix(err.Error(), httpx.BastionCodeHostKey+":") {
		t.Fatalf("read after the host key changed: %v", err)
	}

	// A wrong new pin is refused and changes nothing.
	stranger := newTestBastion(t, nil)
	if _, err := KubeSetBastion(f.config, bastionContextName, setBastionJSON(t, map[string]string{"hostKey": stranger.pin()})); err == nil {
		t.Fatal("a pin of another host was accepted")
	}

	// The new pin alone (the key kept from the store) restores the calls.
	if _, err := KubeSetBastion(f.config, bastionContextName, setBastionJSON(t, map[string]string{"hostKey": f.bastion.pin()})); err != nil {
		t.Fatalf("replace the pin: %v", err)
	}

	if _, err := KubeNamespaces(f.config, bastionContextName, ""); err != nil {
		t.Fatalf("read after replacing the pin: %v", err)
	}
}

func TestBastionSetKeepsWhatIsLeftEmpty(t *testing.T) {
	f := newBastionFixture(t)

	out, err := KubeSetBastion(f.config, bastionContextName, `{}`)
	if err != nil {
		t.Fatalf("edit without changes: %v", err)
	}

	stored, ok := loadBastion(mustBastionKey(t, f.config))
	if !ok || stored.PrivateKey != strings.TrimSpace(f.key) || stored.User != "jump" || stored.HostKey != f.bastion.pin() {
		t.Fatalf("stored after an empty edit: %+v", stored)
	}

	var info kubeBastionInfo
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatal(err)
	}

	if info.Auth != "key" || info.HostKeyType != ssh.KeyAlgoED25519 || !strings.HasPrefix(info.HostKeyFingerprint, "SHA256:") {
		t.Errorf("info %+v", info)
	}
}

func TestBastionSecretsStayOut(t *testing.T) {
	f := newBastionFixture(t)

	info, err := KubeBastionInfo(f.config, bastionContextName)
	if err != nil {
		t.Fatal(err)
	}

	summary, err := ParseKubeconfig(f.config)
	if err != nil {
		t.Fatal(err)
	}

	keyBody := strings.Split(f.key, "\n")[1]

	for what, text := range map[string]string{"info": info, "summary": summary} {
		if strings.Contains(text, keyBody) || strings.Contains(text, "PRIVATE KEY") {
			t.Errorf("%s carries the private key: %s", what, text)
		}
	}

	if !strings.Contains(summary, `"bastionKey":"`+mustBastionKey(t, f.config)+`"`) {
		t.Errorf("summary without the bastion key: %s", summary)
	}

	// An error names the host only.
	f.bastion.rotateHostKey(t)
	kubeClients.forgetConfig(f.config, bastionContextName)

	_, err = KubeNamespaces(f.config, bastionContextName, "")
	if err == nil || strings.Contains(err.Error(), keyBody) || strings.Contains(err.Error(), "jump") {
		t.Errorf("error %v", err)
	}
}

func TestBastionBackupKeepsTheEntry(t *testing.T) {
	f := newBastionFixture(t)

	raw := f.store.Load(mustBastionKey(t, f.config))

	kept, err := KubeAuthForBackup(raw)
	if err != nil {
		t.Fatal(err)
	}

	var state kubeAuthState
	if err := json.Unmarshal([]byte(kept), &state); err != nil {
		t.Fatal(err)
	}

	if state.Method != bastionMethod || state.secret(bastionPrivateKey) != strings.TrimSpace(f.key) || state.secret(bastionHostKey) != f.bastion.pin() {
		t.Errorf("backup kept %+v", state)
	}
}

func TestBastionForgetConfigClosesTheHop(t *testing.T) {
	f := newBastionFixture(t)

	k := f.client(t)
	if _, _, _, err := k.getRaw(t.Context(), "/version", nil); err != nil {
		t.Fatal(err)
	}

	kubeClients.forgetConfig(f.config, bastionContextName)

	if _, err := k.proxy.DialContext(t.Context(), "tcp", "api.internal.test:443"); err == nil {
		t.Error("the bastion still dials after forgetConfig")
	}

	// The next call reconnects.
	if _, err := KubeNamespaces(f.config, bastionContextName, ""); err != nil {
		t.Fatalf("read after forgetConfig: %v", err)
	}
}

func TestBastionRemove(t *testing.T) {
	f := newBastionFixture(t)

	if err := KubeRemoveBastion(f.config, bastionContextName); err != nil {
		t.Fatal(err)
	}

	info, err := KubeBastionInfo(f.config, bastionContextName)
	if err != nil || !strings.Contains(info, `"configured":false`) {
		t.Fatalf("info after removal: %s, %v", info, err)
	}

	// Direct again: api.internal.test does not resolve.
	if _, err := KubeNamespaces(f.config, bastionContextName, ""); err == nil {
		t.Error("a cluster only the bastion reaches answered without it")
	}
}

func TestBastionRefusedWhereItCannotApply(t *testing.T) {
	withAuthStore(t)

	api, _ := bastionAPI(t)
	proxied := bastionKubeconfig(api, nil, `, proxy-url: "http://proxy.example.net:3128"`)

	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	demoContext := firstContextName(t, demo)

	for name, tc := range map[string]struct{ config, context, want string }{
		"proxy-url": {proxied, bastionContextName, bastionUnavailableProxy},
		"demo":      {demo, demoContext, bastionUnavailableDemo},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := KubeSetBastion(tc.config, tc.context, `{"host":"bastion.example.net","user":"jump","password":"pw","hostKey":"x"}`)
			if err == nil || !strings.HasPrefix(err.Error(), KubeBastionUnavailable+":") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("KubeSetBastion: %v", err)
			}

			info, err := KubeBastionInfo(tc.config, tc.context)
			if err != nil || !strings.Contains(info, `"unavailable":"`+tc.want+`"`) {
				t.Fatalf("info %s, %v", info, err)
			}
		})
	}

	if _, err := KubeNamespaces(demo, demoContext, ""); err != nil {
		t.Errorf("demo read: %v", err)
	}
}

func firstContextName(t *testing.T, config string) string {
	t.Helper()

	doc, err := loadKubeconfigDoc(config)
	if err != nil {
		t.Fatal(err)
	}

	names := doc.sortedNames()
	if len(names) == 0 {
		t.Fatal("no context")
	}

	return names[0]
}

// A stored bastion that meets a proxy-url (the kubeconfig replaced later) is ignored: the
// proxy-url wins.
func TestProxyURLWinsOverAStoredBastion(t *testing.T) {
	f := newBastionFixture(t)

	creds, err := parseKubeconfig(bastionKubeconfig(f.api, nil, `, proxy-url: "http://proxy.example.net:3128"`))
	if err != nil {
		t.Fatal(err)
	}

	stored, _ := loadBastion(mustBastionKey(t, f.config))
	kubeAuth.save(kubeBastionKey("other"), bastionState(stored))

	if err := attachBastion(creds, "other"); err != nil {
		t.Fatal(err)
	}

	if creds.bastion != nil {
		t.Error("a bastion was attached to a proxied cluster")
	}

	k := newKubeClient(creds.server, creds)
	t.Cleanup(k.close)

	if _, ok := k.proxy.(*httpx.ProxyDialer); !ok {
		t.Errorf("the client dials through %T", k.proxy)
	}
}

func TestBastionHostKeyAndInfoForTheApps(t *testing.T) {
	b := newTestBastion(t, nil)

	out, err := KubeBastionHostKey(b.addr())
	if err != nil {
		t.Fatal(err)
	}

	var key kubeBastionHostKey
	if err := json.Unmarshal([]byte(out), &key); err != nil {
		t.Fatal(err)
	}

	if key.HostKey != b.pin() || !strings.HasPrefix(key.HostKeyFingerprint, "SHA256:") || key.HostKeyType != ssh.KeyAlgoED25519 {
		t.Errorf("host key %+v", key)
	}

	if b.handshakes.Load() != 0 {
		t.Error("reading the host key signed in")
	}
}
