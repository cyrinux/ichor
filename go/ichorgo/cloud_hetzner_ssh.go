package ichorgo

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// The SSH half of a Hetzner snapshot run: the commands on the rescue system. Its host key is new
// on every run and cannot be known in advance, so the first one seen is pinned for the run, and
// the host was created by this run seconds ago. One key pair per run: never stored.

const (
	hetznerSSHBootWait   = 2 * time.Minute
	hetznerSSHRetry      = 5 * time.Second
	hetznerSSHHandshake  = 20 * time.Second
	hetznerSSHPort       = "22"
	hetznerSSHUser       = "root"
	hetznerRescueDisk    = "/dev/sda"
	hetznerOutputEvery   = time.Second
	hetznerOutputLineMax = 200
	hetznerOutputBufMax  = 4 << 10
)

var errHetznerHostKeyChanged = errors.New("the rescue system's SSH host key changed during the run: stopped")

// hetznerSSHKey is a run's key pair.
type hetznerSSHKey struct {
	publicKey string // authorized_keys format
	signer    ssh.Signer
}

func newHetznerSSHKey() (hetznerSSHKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return hetznerSSHKey{}, err
	}

	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		return hetznerSSHKey{}, err
	}

	sshPublic, err := ssh.NewPublicKey(public)
	if err != nil {
		return hetznerSSHKey{}, err
	}

	return hetznerSSHKey{publicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic))), signer: signer}, nil
}

// hetznerSSH connects to the rescue system (replaced in tests).
type hetznerSSH struct {
	dial            func(ctx context.Context, network, address string) (net.Conn, error)
	bootWait, retry time.Duration
	every           time.Duration
}

func newHetznerSSH() hetznerSSH {
	return hetznerSSH{dial: (&net.Dialer{}).DialContext, bootWait: hetznerSSHBootWait, retry: hetznerSSHRetry, every: hetznerOutputEvery}
}

// hostKeyPin accepts the first host key it sees and refuses any other afterwards.
type hostKeyPin struct{ key []byte }

func (p *hostKeyPin) check(_ string, _ net.Addr, key ssh.PublicKey) error {
	if p.key == nil {
		p.key = key.Marshal()

		return nil
	}

	if !bytes.Equal(p.key, key.Marshal()) {
		return errHetznerHostKeyChanged
	}

	return nil
}

// hetznerRescue is a connection to the rescue system.
type hetznerRescue struct {
	client *ssh.Client
	every  time.Duration
}

func (c *hetznerRescue) Close() error { return c.client.Close() }

// connect retries while the rescue system boots.
func (s hetznerSSH) connect(ctx context.Context, address string, key hetznerSSHKey) (*hetznerRescue, error) {
	pin := &hostKeyPin{}
	config := &ssh.ClientConfig{
		User: hetznerSSHUser, Auth: []ssh.AuthMethod{ssh.PublicKeys(key.signer)},
		HostKeyCallback: pin.check, Timeout: hetznerSSHHandshake,
	}
	target := net.JoinHostPort(address, hetznerSSHPort)
	deadline := time.After(s.bootWait)

	for {
		client, err := s.dialOnce(ctx, target, config)
		if err == nil {
			return &hetznerRescue{client: client, every: s.every}, nil
		}

		if errors.Is(err, errHetznerHostKeyChanged) {
			return nil, errHetznerHostKeyChanged
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			// The dial's error names the address: not worth a line in the activity log.
			return nil, fmt.Errorf("the rescue system did not answer over SSH within %s", s.bootWait)
		case <-time.After(s.retry):
		}
	}
}

func (s hetznerSSH) dialOnce(ctx context.Context, target string, config *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := s.dial(ctx, "tcp", target)
	if err != nil {
		return nil, err
	}

	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := conn.SetDeadline(time.Now().Add(hetznerSSHHandshake)); err != nil {
		_ = conn.Close()

		return nil, err
	}

	sshConn, channels, requests, err := ssh.NewClientConn(conn, target, config)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = sshConn.Close()

		return nil, err
	}

	return ssh.NewClient(sshConn, channels, requests), nil
}

// hetznerWriteCommand writes the image to the disk. A pipeline's status is its last command's, so
// without pipefail a failed download would be a success that wrote nothing: bash is asked by name.
func hetznerWriteCommand(url string) string {
	pipeline := "wget --no-verbose -O - " + shellQuote(url) + " | xz -cd | dd of=" + hetznerRescueDisk + " bs=4M conv=sparse status=progress && sync"

	return "bash -o pipefail -c " + shellQuote(pipeline)
}

// writeImage runs the rescue route and asks the server to shut down. Stopping ctx closes the
// connection, which ends the command.
func (c *hetznerRescue) writeImage(ctx context.Context, url string, output func(string)) error {
	stop := context.AfterFunc(ctx, func() { _ = c.client.Close() })
	defer stop()

	// A disk that cannot discard is still written.
	if err := c.run("blkdiscard --force "+hetznerRescueDisk+" || true", io.Discard); err != nil {
		return fmt.Errorf("the rescue system refused a command: %w", err)
	}

	tail := &hetznerOutputTail{every: c.every, emit: output}

	err := c.run(hetznerWriteCommand(url), tail)
	tail.flush()

	if err != nil {
		if last := tail.lastLine(); last != "" {
			return fmt.Errorf("the image could not be written: %s", last)
		}

		return fmt.Errorf("the image could not be written: %w", err)
	}

	// The connection often goes before the answer does; waitOff tells what happened.
	_ = c.run("shutdown now", io.Discard)

	return nil
}

func (c *hetznerRescue) run(command string, output io.Writer) error {
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close() //nolint:errcheck

	session.Stdout, session.Stderr = output, output

	return session.Run(command)
}

// hetznerOutputTail follows a command's output: dd rewrites its progress line with \r, and one
// line a second is enough for a phone.
type hetznerOutputTail struct {
	mu      sync.Mutex
	every   time.Duration
	emit    func(string)
	buf     []byte
	last    string
	emitted string
	at      time.Time
}

func (t *hetznerOutputTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buf = append(t.buf, p...)

	for {
		i := bytes.IndexAny(t.buf, "\r\n")
		if i < 0 {
			break
		}

		t.line(string(t.buf[:i]))
		t.buf = t.buf[i+1:]
	}

	if len(t.buf) > hetznerOutputBufMax {
		t.buf = t.buf[:0]
	}

	return len(p), nil
}

func (t *hetznerOutputTail) line(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	if len(line) > hetznerOutputLineMax {
		line = line[:hetznerOutputLineMax]
	}

	t.last = line

	if time.Since(t.at) >= t.every {
		t.at = time.Now()
		t.emitted = line
		t.emit(line)
	}
}

// flush shows the last line, which the pace may have skipped.
func (t *hetznerOutputTail) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.line(string(t.buf))
	t.buf = nil

	if t.last != "" && t.last != t.emitted {
		t.emitted = t.last
		t.emit(t.last)
	}
}

func (t *hetznerOutputTail) lastLine() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.last
}
