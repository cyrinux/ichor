// Package httpx builds every outbound HTTP client of the Go core: one TLS floor, one redirect
// policy and no proxy taken from the environment, which a phone has no use for. Every
// http.Client comes from New and every TLS setup starts from BaseTLS (httpx_test.go fails on
// any other in go/ichorgo and go/gitforge).
package httpx

import (
	"cmp"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"time"
)

const (
	dialTimeout      = 30 * time.Second
	handshakeTimeout = 10 * time.Second
	idleTimeout      = 90 * time.Second
	maxRedirects     = 10
)

// ErrInsecureRedirect is a redirect to plain http, refused by HTTPSRedirectsOnly.
var ErrInsecureRedirect = errors.New("refused a redirect to a non-https address")

// Opts sets up a client built by New.
type Opts struct {
	// TLS is cloned; nil is BaseTLS(nil, false).
	TLS *tls.Config
	// Timeout bounds a whole request, body included; 0 leaves it to the caller's context.
	Timeout time.Duration
	// DialTimeout bounds the connection and the TLS handshake; 0 is 30 s and 10 s.
	DialTimeout time.Duration
	// IdleTimeout closes kept-alive connections; 0 is 90 s.
	IdleTimeout time.Duration
	// FollowRedirects follows https redirects; otherwise a redirect is the final answer, so a
	// credential is never carried anywhere else.
	FollowRedirects bool
}

// BaseTLS is the TLS setup every client starts from: TLS 1.2 at least, the given roots (nil
// for the system's) and, only when the user asked for it, no verification.
func BaseTLS(roots *x509.CertPool, insecure bool) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, InsecureSkipVerify: insecure} //nolint:gosec // the user's explicit choice
}

// New is an HTTP client set up by o.
func New(o Opts) *http.Client {
	tlsConfig := o.TLS
	if tlsConfig == nil {
		tlsConfig = BaseTLS(nil, false)
	}

	dial, handshake := o.DialTimeout, o.DialTimeout
	if dial == 0 {
		dial, handshake = dialTimeout, handshakeTimeout
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig.Clone(),
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: dial, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: handshake,
		IdleConnTimeout:     cmp.Or(o.IdleTimeout, idleTimeout),
		ForceAttemptHTTP2:   true,
	}

	client := &http.Client{Transport: transport, Timeout: o.Timeout, CheckRedirect: RefuseRedirect}
	if o.FollowRedirects {
		client.CheckRedirect = HTTPSRedirectsOnly
	}

	return client
}

// RefuseRedirect makes a redirect the final answer (an error for the callers that want a 2xx).
func RefuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// HTTPSRedirectsOnly follows a few redirects, never to plain http.
func HTTPSRedirectsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return ErrInsecureRedirect
	}

	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}

	return nil
}
