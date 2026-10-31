package httpx

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

const (
	schemeHTTP    = "http"
	schemeHTTPS   = "https"
	schemeSOCKS5  = "socks5"
	schemeSOCKS5H = "socks5h"
)

// ProxyError is a connection a proxy could not give. It names the proxy's host, never its
// credentials; the proxy's own answer is only in Err.
type ProxyError struct {
	Host string
	Err  error
}

func (e *ProxyError) Error() string { return "The proxy " + e.Host + " refused the connection" }

func (e *ProxyError) Unwrap() error { return e.Err }

// ParseProxyURL reads a kubeconfig's proxy-url: http, https, socks5 or socks5h, with a host. Its
// errors never quote the address, which can hold a password.
func ParseProxyURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, errors.New("the proxy address is not a URL")
	}

	switch u.Scheme {
	case schemeHTTP, schemeHTTPS, schemeSOCKS5, schemeSOCKS5H:
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}

	if u.Hostname() == "" {
		return nil, errors.New("the proxy address has no host")
	}

	return u, nil
}

// ProxyDisplay is what a proxy address may show: scheme://host:port, without its credentials.
func ProxyDisplay(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// ProxyDialer opens TCP connections through a proxy. New uses it for HTTP; connections outside
// an http.Client (WebSocket streams) dial it directly.
//
// It is a dialer rather than http.Transport.Proxy so that the transport's TLS settings only ever
// apply to the server at the end of the tunnel: a cluster's CA and client certificate never meet
// an https proxy, whose own certificate is checked against the system's roots.
type ProxyDialer struct {
	proxy   *url.URL
	timeout time.Duration
	// tls is for the hop to an https proxy; nil is BaseTLS(nil, false).
	tls *tls.Config
}

// NewProxyDialer dials through proxy; timeout bounds reaching it and its answer (0 is 30 s).
func NewProxyDialer(proxy *url.URL, timeout time.Duration) *ProxyDialer {
	return &ProxyDialer{proxy: proxy, timeout: cmp.Or(timeout, dialTimeout)}
}

// DialContext is a connection to addr through the proxy. A failure is a *ProxyError.
func (d *ProxyDialer) DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var (
		conn net.Conn
		err  error
	)

	switch d.proxy.Scheme {
	case schemeSOCKS5, schemeSOCKS5H:
		conn, err = d.dialSOCKS(ctx, addr)
	default:
		conn, err = d.dialCONNECT(ctx, addr)
	}

	if err != nil {
		return nil, &ProxyError{Host: d.proxy.Host, Err: cmp.Or(ctx.Err(), err)}
	}

	return conn, nil
}

// dialSOCKS sends the host name to the proxy, which resolves it: socks5 and socks5h alike.
func (d *ProxyDialer) dialSOCKS(ctx context.Context, addr string) (net.Conn, error) {
	dialer, err := proxy.FromURL(d.proxy, &net.Dialer{Timeout: d.timeout})
	if err != nil {
		return nil, err
	}

	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("the SOCKS5 dialer takes no context")
	}

	return contextDialer.DialContext(ctx, "tcp", addr)
}

// dialCONNECT asks an http or https proxy for a tunnel to addr.
func (d *ProxyDialer) dialCONNECT(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := d.dialProxyHop(ctx)
	if err != nil {
		return nil, err
	}

	// The exchange has no context of its own: an expired one unblocks it through the deadline.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })

	err = d.askTunnel(conn, addr)
	if !stop() && err == nil {
		err = ctx.Err()
	}

	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	return conn, nil
}

// dialProxyHop is the connection to the proxy itself, in TLS for an https one.
func (d *ProxyDialer) dialProxyHop(ctx context.Context) (net.Conn, error) {
	port := d.proxy.Port()
	if port == "" {
		port = "80"
		if d.proxy.Scheme == schemeHTTPS {
			port = "443"
		}
	}

	conn, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", net.JoinHostPort(d.proxy.Hostname(), port))
	if err != nil || d.proxy.Scheme != schemeHTTPS {
		return conn, err
	}

	base := d.tls
	if base == nil {
		base = BaseTLS(nil, false)
	}

	cfg := base.Clone()
	cfg.ServerName = d.proxy.Hostname()

	tlsConn := tls.Client(conn, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return tlsConn, nil
}

// askTunnel sends the CONNECT and reads the answer. The buffered reader can be dropped after
// it: the far end is a TLS server, which does not speak until spoken to.
func (d *ProxyDialer) askTunnel(conn net.Conn, addr string) error {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: http.Header{},
	}

	if user := d.proxy.User; user != nil {
		password, _ := user.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(user.Username() + ":" + password))
		req.Header.Set("Proxy-Authorization", "Basic "+credentials)
	}

	if err := req.Write(conn); err != nil {
		return err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CONNECT answered %s", resp.Status)
	}

	return nil
}
