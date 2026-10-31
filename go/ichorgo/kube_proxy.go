package ichorgo

import (
	"context"
	"crypto/tls"
	"net"
	"time"

	"golang.org/x/net/websocket"
)

// WebSocket streams (exec, attach, port-forward, a VM console) do not go through the client's
// http.Client, and websocket.Config can only take a *net.Dialer: a cluster behind a proxy-url
// gets its connection from here.

// dialAPIServer is a TLS connection to the API server cfg names, through the cluster's proxy if
// it has one.
func (k *kubeClient) dialAPIServer(ctx context.Context, cfg *websocket.Config) (net.Conn, error) {
	addr := hostWithPort(cfg.Location.Host)
	if k.proxy == nil {
		return (&tls.Dialer{NetDialer: cfg.Dialer, Config: cfg.TlsConfig}).DialContext(ctx, "tcp", addr)
	}

	raw, err := k.proxy.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	config := cfg.TlsConfig.Clone()
	if config.ServerName == "" {
		config.ServerName = cfg.Location.Hostname()
	}

	conn := tls.Client(raw, config)
	if err := conn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()

		return nil, err
	}

	return conn, nil
}

// openStream is cfg.DialContext for this cluster: its errors are the same *websocket.DialError.
func (k *kubeClient) openStream(ctx context.Context, cfg *websocket.Config) (*websocket.Conn, error) {
	if k.proxy == nil {
		return cfg.DialContext(ctx)
	}

	conn, err := k.dialAPIServer(ctx, cfg)
	if err != nil {
		return nil, &websocket.DialError{Config: cfg, Err: err}
	}

	// The handshake has no context of its own: an expired one unblocks it through the deadline.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })

	ws, err := websocket.NewClient(cfg, conn)
	if !stop() && err == nil {
		err = ctx.Err()
	}

	if err != nil {
		_ = conn.Close()

		return nil, &websocket.DialError{Config: cfg, Err: err}
	}

	return ws, nil
}
