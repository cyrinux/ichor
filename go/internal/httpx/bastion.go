package httpx

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// What a failed SSH hop says. The apps match on the code at the start of the message.
const (
	// BastionCodeUnreachable: the TCP connection or the SSH handshake to the bastion failed.
	BastionCodeUnreachable = "kube-bastion-unreachable"
	// BastionCodeHostKey: the bastion presented a host key other than the pinned one.
	BastionCodeHostKey = "kube-bastion-host-key"
	// BastionCodeAuth: the bastion refused the user, the key or the password.
	BastionCodeAuth = "kube-bastion-auth"
	// BastionCodeKey: the private key cannot be read (malformed, passphrase missing or wrong).
	BastionCodeKey = "kube-bastion-key"
	// BastionCodeForward: the bastion would not open a connection to the API server.
	BastionCodeForward = "kube-bastion-forward"
)

const (
	sshPort = "22"
	// bastionIdle closes the SSH connection after this long with no forwarded connection.
	bastionIdle = 2 * time.Minute
)

// BastionConfig is an SSH jump host. Host is host:port (port 22 when absent), HostKey the pinned
// key as one authorized_keys line. Exactly one of PrivateKey (PEM or OpenSSH, Passphrase if it
// is encrypted) and Password signs in.
type BastionConfig struct {
	Host, User, PrivateKey, Passphrase, Password, HostKey string
}

// BastionError is a connection the bastion could not give. It names the bastion's host (and,
// for a refused forward, the API server's), never the user, a key or a password.
type BastionError struct {
	Code string
	Host string
	// Detail is the presented key's SHA256 fingerprint (BastionCodeHostKey) or the address the
	// bastion could not reach (BastionCodeForward).
	Detail string
	Err    error
}

func (e *BastionError) Error() string {
	var sentence string

	switch e.Code {
	case BastionCodeHostKey:
		sentence = "the SSH bastion " + e.Host + " presented another host key (" + e.Detail + "): trust the new key only if you expected the change"
	case BastionCodeAuth:
		sentence = "the SSH bastion " + e.Host + " refused the user name, key or password"
	case BastionCodeKey:
		sentence = "the private key for the SSH bastion " + e.Host + " cannot be read (a passphrase missing or wrong?)"
	case BastionCodeForward:
		sentence = "the SSH bastion " + e.Host + " would not open a connection to " + e.Detail
	default:
		sentence = "the SSH bastion " + e.Host + " did not answer"
	}

	return e.Code + ": " + sentence
}

func (e *BastionError) Unwrap() error { return e.Err }

// BastionAddress is host with port 22 when it names none.
func BastionAddress(host string) string {
	host = strings.TrimSpace(host)
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}

	return net.JoinHostPort(strings.Trim(host, "[]"), sshPort)
}

// BastionDialer opens TCP connections from an SSH bastion (direct-tcpip channels), all on one
// SSH connection opened on first use and closed by Close, or after two idle minutes. TLS to the
// far end stays the caller's: the bastion only forwards encrypted bytes.
type BastionDialer struct {
	host    string
	config  *ssh.ClientConfig
	timeout time.Duration
	// idle closes the SSH connection after this long with no forwarded connection open.
	idle time.Duration
	// dial is the TCP connection to the bastion (replaced in tests).
	dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// connect serialises opening the SSH connection: N dials share one.
	connect sync.Mutex
	mu      sync.Mutex
	client  *ssh.Client
	open    int
	timer   *time.Timer
	closed  bool
}

// NewBastionDialer checks c (the key, the pin) without touching the network; timeout bounds
// reaching the bastion and each forwarded dial (0 is 30 s).
func NewBastionDialer(c BastionConfig, timeout time.Duration) (*BastionDialer, error) {
	host := BastionAddress(c.Host)
	if c.User == "" {
		return nil, errors.New("the SSH bastion needs a user name")
	}

	pin, err := ParseBastionHostKey(c.HostKey)
	if err != nil {
		return nil, err
	}

	auth, err := bastionAuth(c, host)
	if err != nil {
		return nil, err
	}

	timeout = cmp.Or(timeout, dialTimeout)
	pinned := pin.Marshal()
	config := &ssh.ClientConfig{
		User: c.User,
		Auth: auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !bytes.Equal(pinned, key.Marshal()) {
				return &hostKeyMismatch{fingerprint: ssh.FingerprintSHA256(key)}
			}

			return nil
		},
		// Only the pinned key's type: a server with several host keys would otherwise present
		// another one and look changed.
		HostKeyAlgorithms: hostKeyAlgorithms(pin),
		Timeout:           timeout,
	}

	return &BastionDialer{
		host: host, config: config, timeout: timeout, idle: bastionIdle,
		dial: (&net.Dialer{Timeout: timeout}).DialContext,
	}, nil
}

// ParseBastionHostKey reads a pinned host key: one authorized_keys line ("ssh-ed25519 AAAA…").
func ParseBastionHostKey(line string) (ssh.PublicKey, error) {
	if strings.TrimSpace(line) == "" {
		return nil, errors.New("the SSH bastion's host key is not pinned yet")
	}

	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, errors.New("the pinned host key is not an SSH public key")
	}

	return key, nil
}

func hostKeyAlgorithms(pin ssh.PublicKey) []string {
	if pin.Type() == ssh.KeyAlgoRSA {
		return []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA}
	}

	return []string{pin.Type()}
}

// bastionAuth is the key (preferred) or the password, also answered to a keyboard-interactive
// prompt (what many sshd setups ask a password through).
func bastionAuth(c BastionConfig, host string) ([]ssh.AuthMethod, error) {
	if c.PrivateKey != "" {
		var (
			signer ssh.Signer
			err    error
		)

		if c.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(c.PrivateKey), []byte(c.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(c.PrivateKey))
		}

		if err != nil {
			// The parser's own words can quote the key: never kept.
			return nil, &BastionError{Code: BastionCodeKey, Host: host}
		}

		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}

	if c.Password == "" {
		return nil, errors.New("the SSH bastion needs a private key or a password")
	}

	password := c.Password
	interactive := func(_, _ string, questions []string, _ []bool) ([]string, error) {
		answers := make([]string, len(questions))
		if len(questions) == 1 {
			answers[0] = password
		}

		return answers, nil
	}

	return []ssh.AuthMethod{ssh.Password(password), ssh.KeyboardInteractive(interactive)}, nil
}

// hostKeyMismatch is what the host key callback returns for a key that is not the pin.
type hostKeyMismatch struct{ fingerprint string }

func (e *hostKeyMismatch) Error() string { return "host key changed" }

// DialContext is a connection to addr opened by the bastion. It honours deadlines (the SSH
// channel itself does not). A failure is a *BastionError.
func (d *BastionDialer) DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	conn, err := d.forward(ctx, addr)
	if err != nil && retryable(ctx, err) {
		// The SSH connection was dead (the bastion restarted, the network changed): once more.
		conn, err = d.forward(ctx, addr)
	}

	if err != nil {
		return nil, d.classify(ctx, err, addr)
	}

	return conn, nil
}

// retryable is a failure a fresh SSH connection may fix.
func retryable(ctx context.Context, err error) bool {
	var (
		open     *ssh.OpenChannelError
		bastion  *BastionError
		mismatch *hostKeyMismatch
	)

	return ctx.Err() == nil && !errors.As(err, &open) && !errors.As(err, &bastion) && !errors.As(err, &mismatch) &&
		!isAuthFailure(err) && !errors.Is(err, errDialFailed)
}

// errDialFailed marks a failure to reach the bastion at all: a new attempt would not help.
var errDialFailed = errors.New("dial failed")

func (d *BastionDialer) forward(ctx context.Context, addr string) (net.Conn, error) {
	client, err := d.sshClient(ctx)
	if err != nil {
		return nil, err
	}

	ch, err := client.DialContext(ctx, "tcp", addr)
	if err != nil {
		var open *ssh.OpenChannelError
		if !errors.As(err, &open) && ctx.Err() == nil {
			d.drop(client)
		}

		d.scheduleIdle()

		return nil, err
	}

	d.acquire()

	return newDeadlineConn(ch, d.release), nil
}

// classify turns err into a *BastionError, cancellations aside (the caller's context is the
// answer then).
func (d *BastionDialer) classify(ctx context.Context, err error, addr string) error {
	var (
		bastion  *BastionError
		mismatch *hostKeyMismatch
		open     *ssh.OpenChannelError
	)

	switch {
	case errors.As(err, &bastion):
		return bastion
	case errors.As(err, &mismatch):
		return &BastionError{Code: BastionCodeHostKey, Host: d.host, Detail: mismatch.fingerprint}
	case isAuthFailure(err):
		return &BastionError{Code: BastionCodeAuth, Host: d.host, Err: err}
	case errors.As(err, &open):
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			host = addr
		}

		return &BastionError{Code: BastionCodeForward, Host: d.host, Detail: host, Err: err}
	default:
		return &BastionError{Code: BastionCodeUnreachable, Host: d.host, Err: cmp.Or(ctx.Err(), err)}
	}
}

func isAuthFailure(err error) bool {
	return strings.Contains(err.Error(), "unable to authenticate")
}

// sshClient is the open SSH connection, opened now if there is none.
func (d *BastionDialer) sshClient(ctx context.Context) (*ssh.Client, error) {
	d.connect.Lock()
	defer d.connect.Unlock()

	d.mu.Lock()
	client, closed := d.client, d.closed
	d.mu.Unlock()

	if closed {
		return nil, net.ErrClosed
	}

	if client != nil {
		return client, nil
	}

	client, err := d.handshake(ctx)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()

		_ = client.Close()

		return nil, net.ErrClosed
	}

	d.client = client
	d.mu.Unlock()

	// A connection the bastion drops is forgotten at once: the next dial opens another.
	go func() {
		_ = client.Wait()
		d.drop(client)
	}()

	return client, nil
}

// handshake dials the bastion under ctx and runs the SSH handshake under a deadline.
func (d *BastionDialer) handshake(ctx context.Context) (*ssh.Client, error) {
	conn, err := d.dial(ctx, "tcp", d.host)
	if err != nil {
		return nil, errors.Join(errDialFailed, err)
	}

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := conn.SetDeadline(time.Now().Add(d.timeout)); err != nil {
		_ = conn.Close()

		return nil, errors.Join(errDialFailed, err)
	}

	sshConn, channels, requests, err := ssh.NewClientConn(conn, d.host, d.config)
	if err != nil {
		_ = conn.Close()

		var mismatch *hostKeyMismatch
		if errors.As(err, &mismatch) || isAuthFailure(err) {
			return nil, err
		}

		return nil, errors.Join(errDialFailed, err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = sshConn.Close()

		return nil, errors.Join(errDialFailed, err)
	}

	return ssh.NewClient(sshConn, channels, requests), nil
}

// drop forgets client if it is still the current one, and closes it.
func (d *BastionDialer) drop(client *ssh.Client) {
	d.mu.Lock()
	if d.client == client {
		d.client = nil
	}
	d.mu.Unlock()

	_ = client.Close()
}

func (d *BastionDialer) acquire() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.open++

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

func (d *BastionDialer) release() {
	d.mu.Lock()
	d.open--
	d.mu.Unlock()

	d.scheduleIdle()
}

// scheduleIdle closes the SSH connection after d.idle if nothing is forwarded by then.
func (d *BastionDialer) scheduleIdle() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.open > 0 || d.client == nil || d.closed {
		return
	}

	if d.timer != nil {
		d.timer.Stop()
	}

	d.timer = time.AfterFunc(d.idle, func() {
		d.mu.Lock()
		client := d.client
		idle := d.open == 0

		if idle {
			d.client = nil
		}
		d.mu.Unlock()

		if idle && client != nil {
			_ = client.Close()
		}
	})
}

// Close ends the SSH connection and every connection forwarded through it; later dials fail.
func (d *BastionDialer) Close() error {
	d.mu.Lock()
	client := d.client
	d.client, d.closed = nil, true

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()

	if client != nil {
		return client.Close()
	}

	return nil
}

// Connect opens the SSH connection (the pin, the sign-in) without forwarding anything: the check
// of a bastion whose far end is not known yet. A failure is a *BastionError.
func (d *BastionDialer) Connect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	if _, err := d.sshClient(ctx); err != nil {
		return d.classify(ctx, err, d.host)
	}

	d.scheduleIdle()

	return nil
}

// Host is the bastion's address (host:port), for messages.
func (d *BastionDialer) Host() string { return d.host }

// BastionHostKey reads the key host presents, for the user to trust (first use or a changed
// key). It stops there: no credential is ever sent to a host that is not pinned yet.
func BastionHostKey(ctx context.Context, host string, timeout time.Duration) (ssh.PublicKey, error) {
	host = BastionAddress(host)
	timeout = cmp.Or(timeout, dialTimeout)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, &BastionError{Code: BastionCodeUnreachable, Host: host, Err: err}
	}
	defer conn.Close() //nolint:errcheck

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	var presented ssh.PublicKey

	config := &ssh.ClientConfig{
		User: "ichor",
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = key

			return errHostKeyRead
		},
		Timeout: timeout,
	}

	_, _, _, err = ssh.NewClientConn(conn, host, config)
	if presented != nil {
		return presented, nil
	}

	return nil, &BastionError{Code: BastionCodeUnreachable, Host: host, Err: cmp.Or(ctx.Err(), err)}
}

// errHostKeyRead ends a handshake once the host key is known.
var errHostKeyRead = errors.New("host key read")
