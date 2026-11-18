package httpx

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// fakeBastion is an in-process SSH server that forwards direct-tcpip channels, like sshd with
// AllowTcpForwarding.
type fakeBastion struct {
	t        *testing.T
	listener net.Listener
	hostKey  ssh.Signer
	clientPK ssh.PublicKey
	password string
	// refuseForward refuses every direct-tcpip channel (PermitOpen none).
	refuseForward atomic.Bool
	// handshakes counts SSH connections accepted.
	handshakes atomic.Int32

	mu    sync.Mutex
	conns []net.Conn
}

// newFakeBastion starts a bastion accepting the client key clientKey (may be nil) or password.
func newFakeBastion(t *testing.T, clientKey ssh.PublicKey, password string) *fakeBastion {
	t.Helper()

	b := &fakeBastion{t: t, hostKey: newSigner(t), clientPK: clientKey, password: password}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	b.listener = listener
	t.Cleanup(b.stop)

	go b.serve()

	return b
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}

	return signer
}

func (b *fakeBastion) config() *ssh.ServerConfig {
	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if b.clientPK != nil && string(key.Marshal()) == string(b.clientPK.Marshal()) {
				return &ssh.Permissions{}, nil
			}

			return nil, errors.New("unknown key")
		},
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if b.password != "" && string(password) == b.password {
				return &ssh.Permissions{}, nil
			}

			return nil, errors.New("wrong password")
		},
	}
	config.AddHostKey(b.hostKey)

	return config
}

func (b *fakeBastion) addr() string { return b.listener.Addr().String() }

func (b *fakeBastion) pin() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(b.hostKey.PublicKey())))
}

func (b *fakeBastion) serve() {
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}

		b.mu.Lock()
		b.conns = append(b.conns, conn)
		b.mu.Unlock()

		go b.serveConn(conn)
	}
}

func (b *fakeBastion) serveConn(conn net.Conn) {
	sshConn, channels, requests, err := ssh.NewServerConn(conn, b.config())
	if err != nil {
		return
	}
	defer sshConn.Close() //nolint:errcheck

	b.handshakes.Add(1)

	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "direct-tcpip" || b.refuseForward.Load() {
			_ = newChannel.Reject(ssh.Prohibited, "forwarding refused")

			continue
		}

		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(newChannel.ExtraData(), &target); err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, "bad request")

			continue
		}

		upstream, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.FormatUint(uint64(target.Port), 10)))
		if err != nil {
			_ = newChannel.Reject(ssh.ConnectionFailed, "cannot reach")

			continue
		}

		channel, reqs, err := newChannel.Accept()
		if err != nil {
			_ = upstream.Close()

			continue
		}

		go ssh.DiscardRequests(reqs)
		go pipeChannel(channel, upstream)
	}
}

func pipeChannel(channel ssh.Channel, upstream net.Conn) {
	done := make(chan struct{}, 2)

	go func() { _, _ = io.Copy(channel, upstream); _ = channel.CloseWrite(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(upstream, channel); done <- struct{}{} }()

	<-done
	_ = channel.Close()
	_ = upstream.Close()
}

// dropConnections closes every SSH connection the bastion accepted (a restart).
func (b *fakeBastion) dropConnections() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, c := range b.conns {
		_ = c.Close()
	}

	b.conns = nil
}

func (b *fakeBastion) stop() {
	_ = b.listener.Close()
	b.dropConnections()
}

// echoServer answers every line with the same line.
func echoServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() { _, _ = io.Copy(conn, conn); _ = conn.Close() }()
		}
	}()

	return listener.Addr().String()
}

// clientKey is a fresh private key in OpenSSH PEM form and its public key.
func clientKey(t *testing.T, passphrase string) (string, ssh.PublicKey) {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(private, "")
	}

	if err != nil {
		t.Fatal(err)
	}

	pk, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(block)), pk
}

func keyDialer(t *testing.T) (*fakeBastion, *BastionDialer) {
	t.Helper()

	key, pk := clientKey(t, "")
	b := newFakeBastion(t, pk, "")

	d, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: key, HostKey: b.pin()}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = d.Close() })

	return b, d
}

func roundTrip(t *testing.T, conn net.Conn, message string) {
	t.Helper()

	if _, err := conn.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}

	got := make([]byte, len(message))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}

	if string(got) != message {
		t.Fatalf("echo %q, want %q", got, message)
	}
}

func bastionCode(err error) string {
	var bastion *BastionError
	if errors.As(err, &bastion) {
		return bastion.Code
	}

	return ""
}

func TestBastionDialsThroughOneSSHConnection(t *testing.T) {
	b, d := keyDialer(t)
	target := echoServer(t)

	for i := range 3 {
		conn, err := d.DialContext(t.Context(), "tcp", target)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}

		roundTrip(t, conn, "hello")
		_ = conn.Close()
	}

	if n := b.handshakes.Load(); n != 1 {
		t.Errorf("%d SSH connections for three dials, want 1", n)
	}
}

func TestBastionPasswordSignsIn(t *testing.T) {
	b := newFakeBastion(t, nil, "s3cret")

	d, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", Password: "s3cret", HostKey: b.pin()}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = d.Close() })

	conn, err := d.DialContext(t.Context(), "tcp", echoServer(t))
	if err != nil {
		t.Fatal(err)
	}

	roundTrip(t, conn, "pw")
	_ = conn.Close()
}

func TestBastionRefusals(t *testing.T) {
	key, pk := clientKey(t, "")
	otherKey, _ := clientKey(t, "")
	lockedKey, _ := clientKey(t, "phrase")
	b := newFakeBastion(t, pk, "s3cret")
	stranger := newFakeBastion(t, pk, "")

	tests := []struct {
		name   string
		config BastionConfig
		want   string
	}{
		{"changed host key", BastionConfig{Host: b.addr(), User: "jump", PrivateKey: key, HostKey: stranger.pin()}, BastionCodeHostKey},
		{"unknown key", BastionConfig{Host: b.addr(), User: "jump", PrivateKey: otherKey, HostKey: b.pin()}, BastionCodeAuth},
		{"wrong password", BastionConfig{Host: b.addr(), User: "jump", Password: "nope", HostKey: b.pin()}, BastionCodeAuth},
		{"nobody listening", BastionConfig{Host: closedAddr(t), User: "jump", PrivateKey: key, HostKey: b.pin()}, BastionCodeUnreachable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewBastionDialer(tt.config, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() { _ = d.Close() })

			_, err = d.DialContext(t.Context(), "tcp", "127.0.0.1:1")
			if code := bastionCode(err); code != tt.want {
				t.Fatalf("code %q (%v), want %q", code, err, tt.want)
			}

			for _, secret := range []string{key, otherKey, "s3cret", "nope", "jump"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("the error quotes a secret: %v", err)
				}
			}
		})
	}

	t.Run("passphrase missing", func(t *testing.T) {
		_, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: lockedKey, HostKey: b.pin()}, 0)
		if code := bastionCode(err); code != BastionCodeKey {
			t.Fatalf("code %q (%v), want %q", code, err, BastionCodeKey)
		}
	})

	t.Run("wrong passphrase", func(t *testing.T) {
		_, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: lockedKey, Passphrase: "other", HostKey: b.pin()}, 0)
		if code := bastionCode(err); code != BastionCodeKey {
			t.Fatalf("code %q (%v), want %q", code, err, BastionCodeKey)
		}
	})

	t.Run("right passphrase", func(t *testing.T) {
		if _, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: lockedKey, Passphrase: "phrase", HostKey: b.pin()}, 0); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no pin", func(t *testing.T) {
		if _, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: key}, 0); err == nil {
			t.Fatal("a bastion without a pinned key was accepted")
		}
	})
}

func TestBastionChangedKeyNamesTheNewFingerprint(t *testing.T) {
	key, pk := clientKey(t, "")
	b := newFakeBastion(t, pk, "")
	other := newFakeBastion(t, pk, "")

	d, err := NewBastionDialer(BastionConfig{Host: b.addr(), User: "jump", PrivateKey: key, HostKey: other.pin()}, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = d.Close() })

	_, err = d.DialContext(t.Context(), "tcp", "127.0.0.1:1")
	if want := ssh.FingerprintSHA256(b.hostKey.PublicKey()); !strings.Contains(err.Error(), want) {
		t.Errorf("%v does not name %s", err, want)
	}

	if b.handshakes.Load() != 0 {
		t.Error("credentials were sent to a host that is not the pinned one")
	}
}

func closedAddr(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()
	_ = listener.Close()

	return addr
}

func TestBastionRefusedForward(t *testing.T) {
	b, d := keyDialer(t)
	b.refuseForward.Store(true)

	_, err := d.DialContext(t.Context(), "tcp", "api.example.net:6443")
	if code := bastionCode(err); code != BastionCodeForward {
		t.Fatalf("code %q (%v), want %q", code, err, BastionCodeForward)
	}

	if !strings.Contains(err.Error(), "api.example.net") {
		t.Errorf("%v does not name the API server", err)
	}
}

func TestBastionRedialsOnceAfterARestart(t *testing.T) {
	b, d := keyDialer(t)
	target := echoServer(t)

	conn, err := d.DialContext(t.Context(), "tcp", target)
	if err != nil {
		t.Fatal(err)
	}

	_ = conn.Close()

	b.dropConnections()

	conn, err = d.DialContext(t.Context(), "tcp", target)
	if err != nil {
		t.Fatalf("dial after the bastion dropped the connection: %v", err)
	}

	roundTrip(t, conn, "again")
	_ = conn.Close()

	if n := b.handshakes.Load(); n != 2 {
		t.Errorf("%d SSH connections, want 2", n)
	}
}

func TestBastionCancelledContextReturnsPromptly(t *testing.T) {
	// A listener that accepts and never speaks: the SSH handshake hangs.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	key, _ := clientKey(t, "")

	d, err := NewBastionDialer(BastionConfig{Host: listener.Addr().String(), User: "jump", PrivateKey: key, HostKey: newFakeBastion(t, nil, "").pin()}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = d.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()

	_, err = d.DialContext(ctx, "tcp", "127.0.0.1:1")
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("returned after %s with %v", time.Since(start), err)
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%v is not the context's deadline", err)
	}
}

func TestBastionConnectionHonoursDeadlines(t *testing.T) {
	_, d := keyDialer(t)

	// A server that accepts and never answers.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	conn, err := d.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck

	if err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		_, err := conn.Read(make([]byte, 1))
		done <- err
	}()

	select {
	case err := <-done:
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() || !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("read ended with %v, want a timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read deadline did not end the read")
	}

	if _, err := conn.Write([]byte("x")); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("write after the deadline: %v", err)
	}
}

func TestBastionDeadlineClearedKeepsTheConnection(t *testing.T) {
	_, d := keyDialer(t)

	conn, err := d.DialContext(t.Context(), "tcp", echoServer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck

	_ = conn.SetDeadline(time.Now().Add(50 * time.Millisecond))
	_ = conn.SetDeadline(time.Time{})

	time.Sleep(100 * time.Millisecond)

	roundTrip(t, conn, "still here")
}

func TestBastionCloseAndIdleEndTheConnection(t *testing.T) {
	b, d := keyDialer(t)
	d.idle = 50 * time.Millisecond
	target := echoServer(t)

	conn, err := d.DialContext(t.Context(), "tcp", target)
	if err != nil {
		t.Fatal(err)
	}

	// An open forwarded connection keeps the SSH connection past the idle time.
	time.Sleep(150 * time.Millisecond)
	roundTrip(t, conn, "busy")
	_ = conn.Close()

	waitFor(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()

		return d.client == nil
	})

	conn, err = d.DialContext(t.Context(), "tcp", target)
	if err != nil {
		t.Fatal(err)
	}

	roundTrip(t, conn, "back")

	if n := b.handshakes.Load(); n != 2 {
		t.Errorf("%d SSH connections, want 2 (one closed for idleness)", n)
	}

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Error("a forwarded connection outlived Close")
	}

	if _, err := d.DialContext(t.Context(), "tcp", target); err == nil {
		t.Error("a closed dialer dialled")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in 5 s")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestBastionHostKeyReadsWithoutSigningIn(t *testing.T) {
	b := newFakeBastion(t, nil, "")

	key, err := BastionHostKey(t.Context(), b.addr(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if ssh.FingerprintSHA256(key) != ssh.FingerprintSHA256(b.hostKey.PublicKey()) {
		t.Error("another key than the server's")
	}

	if b.handshakes.Load() != 0 {
		t.Error("the key read completed a sign-in")
	}

	if _, err := BastionHostKey(t.Context(), closedAddr(t), time.Second); bastionCode(err) != BastionCodeUnreachable {
		t.Errorf("closed port: %v", err)
	}
}

func TestBastionAddressDefaultsToPort22(t *testing.T) {
	for in, want := range map[string]string{
		"bastion.example.net":      "bastion.example.net:22",
		"bastion.example.net:2222": "bastion.example.net:2222",
		"[2001:db8::1]":            "[2001:db8::1]:22",
		"2001:db8::1":              "[2001:db8::1]:22",
	} {
		if got := BastionAddress(in); got != want {
			t.Errorf("BastionAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewTakesDialAndProxyURLWins(t *testing.T) {
	var dialled atomic.Bool

	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialled.Store(true)

		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}

	target := echoServer(t)

	client := New(Opts{Dial: dial})
	if _, err := client.Get("http://" + target); err == nil || !dialled.Load() {
		// The echo server is no HTTP server: an error is expected, after the dial.
		t.Fatalf("dial used %v, err %v", dialled.Load(), err)
	}

	dialled.Store(false)

	proxy, err := ParseProxyURL("http://" + closedAddr(t))
	if err != nil {
		t.Fatal(err)
	}

	client = New(Opts{Dial: dial, ProxyURL: proxy})
	_, err = client.Get("http://" + target)

	var proxyErr *ProxyError
	if !errors.As(err, &proxyErr) || dialled.Load() {
		t.Errorf("with both set: dial used %v, err %v (want the proxy's)", dialled.Load(), err)
	}
}
