package ichorgo

import (
	"cmp"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"time"
)

// Every outbound HTTP client of the package comes from newHTTPClient, and every TLS setup
// starts from baseTLS (httpclient_test.go fails on any other): one TLS floor, one redirect
// policy and no proxy taken from the environment, which a phone has no use for.

const (
	httpDialTimeout      = 30 * time.Second
	httpHandshakeTimeout = 10 * time.Second
	httpIdleTimeout      = 90 * time.Second
	httpMaxRedirects     = 10
)

var errInsecureRedirect = errors.New("refused a redirect to a non-https address")

type httpClientOpts struct {
	// tls is cloned; nil is baseTLS(nil, false).
	tls *tls.Config
	// timeout bounds a whole request, body included; 0 leaves it to the caller's context.
	timeout time.Duration
	// dialTimeout bounds the connection and the TLS handshake; 0 is httpDialTimeout and
	// httpHandshakeTimeout.
	dialTimeout time.Duration
	// idleTimeout closes kept-alive connections; 0 is httpIdleTimeout.
	idleTimeout time.Duration
	// followRedirects follows https redirects; otherwise a redirect is the final answer, so a
	// credential is never carried anywhere else.
	followRedirects bool
}

// baseTLS is the TLS setup every client starts from: TLS 1.2 at least, the given roots (nil
// for the system's) and, only when the user asked for it, no verification.
func baseTLS(roots *x509.CertPool, insecure bool) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, InsecureSkipVerify: insecure} //nolint:gosec // the user's explicit choice
}

func newHTTPClient(o httpClientOpts) *http.Client {
	tlsConfig := o.tls
	if tlsConfig == nil {
		tlsConfig = baseTLS(nil, false)
	}

	dial, handshake := o.dialTimeout, o.dialTimeout
	if dial == 0 {
		dial, handshake = httpDialTimeout, httpHandshakeTimeout
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig.Clone(),
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: dial, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: handshake,
		IdleConnTimeout:     cmp.Or(o.idleTimeout, httpIdleTimeout),
		ForceAttemptHTTP2:   true,
	}

	client := &http.Client{Transport: transport, Timeout: o.timeout, CheckRedirect: refuseRedirect}
	if o.followRedirects {
		client.CheckRedirect = httpsRedirectsOnly
	}

	return client
}

// refuseRedirect makes a redirect the final answer (an error for the callers that want a 2xx).
func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// httpsRedirectsOnly follows a few redirects, never to plain http.
func httpsRedirectsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return errInsecureRedirect
	}

	if len(via) >= httpMaxRedirects {
		return errors.New("too many redirects")
	}

	return nil
}
