package httpx

import (
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// deadlineConn gives deadlines to a connection that refuses them (an SSH channel: "ssh: tcpChan:
// deadline not supported"). A deadline that passes closes the connection: the read or write it
// stops, and every later one, fails with os.ErrDeadlineExceeded (a net.Error whose Timeout is
// true), as on a TCP connection. Unlike TCP, the connection cannot be used again afterwards;
// every caller in the core closes it on a timeout anyway.
type deadlineConn struct {
	net.Conn

	mu          sync.Mutex
	read, write *time.Timer
	expired     atomic.Bool
	closeOnce   sync.Once
	onClose     func()
}

func newDeadlineConn(conn net.Conn, onClose func()) *deadlineConn {
	return &deadlineConn{Conn: conn, onClose: onClose}
}

func (c *deadlineConn) Read(p []byte) (int, error) {
	if c.expired.Load() {
		return 0, os.ErrDeadlineExceeded
	}

	n, err := c.Conn.Read(p)
	if err != nil && c.expired.Load() {
		return n, os.ErrDeadlineExceeded
	}

	return n, err
}

func (c *deadlineConn) Write(p []byte) (int, error) {
	if c.expired.Load() {
		return 0, os.ErrDeadlineExceeded
	}

	n, err := c.Conn.Write(p)
	if err != nil && c.expired.Load() {
		return n, os.ErrDeadlineExceeded
	}

	return n, err
}

func (c *deadlineConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.read = c.arm(c.read, t)
	c.write = c.arm(c.write, t)

	return nil
}

func (c *deadlineConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.read = c.arm(c.read, t)

	return nil
}

func (c *deadlineConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.write = c.arm(c.write, t)

	return nil
}

// arm replaces timer with one for t (none for the zero time). Called with c.mu held.
func (c *deadlineConn) arm(timer *time.Timer, t time.Time) *time.Timer {
	if timer != nil {
		timer.Stop()
	}

	if t.IsZero() {
		return nil
	}

	wait := time.Until(t)
	if wait <= 0 {
		c.expire()

		return nil
	}

	return time.AfterFunc(wait, c.expire)
}

func (c *deadlineConn) expire() {
	c.expired.Store(true)
	_ = c.Conn.Close()
}

func (c *deadlineConn) Close() error {
	var err error

	c.closeOnce.Do(func() {
		c.mu.Lock()
		for _, timer := range []*time.Timer{c.read, c.write} {
			if timer != nil {
				timer.Stop()
			}
		}
		c.mu.Unlock()

		err = c.Conn.Close()

		if c.onClose != nil {
			c.onClose()
		}
	})

	return err
}
