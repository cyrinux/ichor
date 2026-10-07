package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/websocket"
)

// Port-forward, like `kubectl port-forward pod/POD LOCAL:REMOTE`: the app listens on the
// loopback address only (a random port), and each connection made to it gets its own
// WebSocket to the pod's port through the API server. Nothing listens on the network the
// phone is on.

// kubePortForwardProtocol is the API server's WebSocket port-forward protocol: per port,
// channel 0 carries the data and channel 1 the errors, every frame starts with its channel,
// and the first frame of each channel is the port number (2 bytes, little endian).
const kubePortForwardProtocol = "portforward.k8s.io"

// PortForwardListener follows a forward (implemented in Kotlin/Swift).
type PortForwardListener interface {
	// OnReady gives the local address to open, "127.0.0.1:PORT".
	OnReady(address string)
	// OnConnectionError reports a connection that failed (the pod refused the port); the
	// forward keeps listening.
	OnConnectionError(errMessage string)
	// OnDone is called exactly once; errMessage is empty when stopped.
	OnDone(errMessage string)
}

// PortForwardRun is a handle on a forward.
type PortForwardRun struct {
	cancel context.CancelFunc
}

// Stop closes the local port and every forwarded connection; OnDone follows.
func (r *PortForwardRun) Stop() { r.cancel() }

type maskedPortForwardListener struct{ PortForwardListener }

func (l maskedPortForwardListener) OnReady(address string) { l.PortForwardListener.OnReady(address) }
func (l maskedPortForwardListener) OnConnectionError(errMessage string) {
	l.PortForwardListener.OnConnectionError(privacy.maskPlain(errMessage))
}

func (l maskedPortForwardListener) OnDone(errMessage string) {
	l.PortForwardListener.OnDone(privacy.maskPlain(errMessage))
}

// StartPortForward forwards a local loopback port to remotePort of the pod until Stop.
func StartPortForward(configYAML, contextName, kubeServer, namespace, pod string, remotePort int, listener PortForwardListener) *PortForwardRun {
	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	listener = maskedPortForwardListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := runPortForward(ctx, kubeTarget{configYAML, contextName, kubeServer}, namespace, pod, remotePort, listener)
		if ctx.Err() != nil {
			err = nil
		}

		msg := ""
		if err != nil {
			msg = err.Error()
		}

		listener.OnDone(msg)
	}()

	return &PortForwardRun{cancel: cancel}
}

func runPortForward(ctx context.Context, target kubeTarget, namespace, pod string, remotePort int, listener PortForwardListener) error {
	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if remotePort < 1 || remotePort > 65535 {
		return fmt.Errorf("invalid port %d", remotePort)
	}

	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	k, _, err := kubeClients.get(target)
	if err != nil {
		return err
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen on the phone: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close() //nolint:errcheck
	}()

	listener.OnReady(ln.Addr().String())

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := forwardConn(ctx, k, namespace, pod, remotePort, conn); err != nil {
				listener.OnConnectionError(err.Error())
			}
		}()
	}
}

// forwardConn carries one local connection to the pod's port until either side closes.
func forwardConn(ctx context.Context, k *kubeClient, namespace, pod string, port int, conn net.Conn) error {
	defer conn.Close() //nolint:errcheck

	cfg, err := k.portForwardConfig(namespace, pod, port)
	if err != nil {
		return err
	}

	ws, err := cfg.DialContext(ctx)
	if err != nil {
		var dialErr *websocket.DialError
		if errors.As(err, &dialErr) && errors.Is(dialErr.Err, websocket.ErrBadStatus) {
			return errors.New("port-forward refused by the Kubernetes API (forbidden, or the pod is gone)")
		}

		return err
	}
	defer ws.Close() //nolint:errcheck

	ws.PayloadType = websocket.BinaryFrame

	stop := context.AfterFunc(ctx, func() { _ = ws.Close() }) //nolint:errcheck
	defer stop()

	errs := make(chan error, 2)

	go func() { errs <- pumpToPod(conn, ws) }()
	go func() { errs <- pumpFromPod(ws, conn) }()

	err = <-errs
	if ctx.Err() != nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}

	return err
}

func (k *kubeClient) portForwardConfig(namespace, pod string, port int) (*websocket.Config, error) {
	u, err := k.endpoint(podPath(namespace, pod) + "/portforward?" + url.Values{"ports": {strconv.Itoa(port)}}.Encode())
	if err != nil {
		return nil, err
	}

	u.Scheme = "wss"

	cfg, err := websocket.NewConfig(u.String(), (&url.URL{Scheme: "https", Host: u.Host}).String())
	if err != nil {
		return nil, fmt.Errorf("port-forward URL: %w", err)
	}

	cfg.Protocol = []string{kubePortForwardProtocol}
	cfg.TlsConfig = k.tls.Clone()
	cfg.Dialer = &net.Dialer{Timeout: kubeProbeTimeout}
	cfg.Header.Set("User-Agent", "ichor")

	if k.token != "" {
		cfg.Header.Set("Authorization", "Bearer "+k.token)
	}

	return cfg, nil
}

// pumpToPod sends what the local client writes on the data channel.
func pumpToPod(conn net.Conn, ws *websocket.Conn) error {
	buf := make([]byte, 32<<10)

	for {
		n, err := conn.Read(buf)
		if n > 0 {
			frame := append([]byte{0}, buf[:n]...)
			if werr := websocket.Message.Send(ws, frame); werr != nil {
				return werr
			}
		}

		if err != nil {
			return err
		}
	}
}

// pumpFromPod writes the data channel to the local client; an error frame ends the
// connection with the pod's message. The first frame of each channel (the port) is skipped.
func pumpFromPod(ws *websocket.Conn, conn net.Conn) error {
	seen := map[byte]bool{}

	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			return err
		}

		if len(frame) == 0 {
			continue
		}

		channel, payload := frame[0], frame[1:]
		if !seen[channel] {
			seen[channel] = true

			continue
		}

		switch channel {
		case 0:
			if _, err := conn.Write(payload); err != nil {
				return err
			}
		case 1:
			if msg := strings.TrimSpace(string(payload)); msg != "" {
				return errors.New(msg)
			}
		}
	}
}
