package ichorgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// The serial console of a KubeVirt virtual machine, the way `virtctl console` opens it: a
// WebSocket on the instance's console subresource carrying the guest's ttyS0 as raw bytes,
// with no channel byte, no resize and no exit status.

// vmConsoleProtocol is the WebSocket subprotocol KubeVirt's client dials the console with:
// PlainStreamProtocolName in kubevirt.io/client-go/subresources/constants.go, offered by
// roundTripperFromConfig in kubevirt.io/client-go/kubevirt/typed/core/v1/async.go.
const vmConsoleProtocol = "plain.kubevirt.io"

// How long the console is waited for while virt-handler readies it: KubeVirt answers 400
// until then, and its client asks again once a second.
var (
	vmConsoleRetry = time.Second
	vmConsoleWait  = 30 * time.Second
)

// vmConsoleDialTimeout bounds one attempt at the WebSocket handshake.
const vmConsoleDialTimeout = 15 * time.Second

var (
	errVMConsoleStopped = errors.New("the VM is stopped: start it first")
	errVMConsoleTimeout = errors.New("the console did not open within 30 s: the VM may still be booting, try again")
)

// vmConsoleDisconnected is how a console ends from the far side, with the causes virtctl
// gives for it (close code 1006).
const vmConsoleDisconnected = "disconnected: the VM was powered off, another user connected to its console, or the network dropped"

// vmConsoleRefusedError is a console the API server did not upgrade, with its HTTP status.
type vmConsoleRefusedError struct{ status int }

func (e *vmConsoleRefusedError) Error() string {
	switch e.status {
	case http.StatusForbidden:
		return "the console was refused (HTTP 403): the credentials may not get virtualmachineinstances/console"
	case http.StatusNotFound:
		return errVMConsoleStopped.Error()
	case 0:
		return "the console was refused by the Kubernetes API"
	default:
		return fmt.Sprintf("the console was refused (HTTP %d %s)", e.status, http.StatusText(e.status))
	}
}

// StartVMConsole opens the serial console of the KubeVirt virtual machine namespace/name, like
// `virtctl console -n NAMESPACE NAME` (os:admin): its running instance's ttyS0 in the app's
// terminal. Refused when the VM is not Running. Typed bytes go to the guest as they are and
// Resize does nothing (a serial console has no size); the session ends with code 0 and
// "disconnected…" when the VM powers off or another viewer takes the console. Each open is
// recorded in the audit log as vm-console. kubeServer: see KubePods.
func StartVMConsole(configYAML, contextName, kubeServer, namespace, name string, listener DebugListener) *DebugSession {
	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	d, ctx := newDebugSession(listener)

	go d.runVMConsole(ctx, kubeTarget{configYAML, contextName, kubeServer}, namespace, name)

	return d
}

func (d *DebugSession) runVMConsole(ctx context.Context, target kubeTarget, namespace, name string) {
	defer d.cancel()
	defer onPanic(func(msg string) { d.exit(-1, msg) })

	if err := validateKubeName("virtual machine", namespace, name); err != nil {
		d.exit(-1, err.Error())

		return
	}

	ws, err := d.openVMConsole(ctx, target, namespace, name)
	recordOutcome(target.config, target.context,
		auditAction{Server: target.server, Action: "vm-console", Namespace: namespace, Object: "VirtualMachine/" + name}, err)

	if err != nil {
		d.exitDialFailed(ctx, err)

		return
	}

	d.serveRaw(ctx, ws)
}

// openVMConsole checks the VM runs, then dials its console, waiting while KubeVirt readies it.
func (d *DebugSession) openVMConsole(ctx context.Context, target kubeTarget, namespace, name string) (*websocket.Conn, error) {
	if isDemoContext(target.config, target.context) {
		return nil, errDemoUnavailable
	}

	k, _, err := kubeClients.get(target)
	if err != nil {
		return nil, kubeError(err)
	}

	vmVersion, subVersion, err := virtVersions(ctx, k)
	if err != nil {
		return nil, kubeError(err)
	}

	var vmi vmiObject

	err = k.get(ctx, vmPath(groupKubeVirt, vmVersion, namespace, "virtualmachineinstances", name), &vmi)

	switch {
	case isNotFound(err):
		return nil, errVMConsoleStopped
	case err != nil:
		return nil, kubeError(err)
	case vmi.Status.Phase != "Running":
		return nil, fmt.Errorf("the VM is %s: its console opens once it is Running", vmPhaseOrUnknown(vmi.Status.Phase))
	}

	d.listener.OnStatus("Connecting to the serial console of " + namespace + "/" + name + "…")

	path := vmPath(groupKubeVirtSub, subVersion, namespace, "virtualmachineinstances", name) + "/console"

	return dialVMConsoleWaiting(ctx, k, path, func() { d.listener.OnStatus("Waiting for the console…") })
}

func vmPhaseOrUnknown(phase string) string {
	if phase == "" {
		return "not running yet"
	}

	return phase
}

// dialVMConsoleWaiting dials the console at path, again once every vmConsoleRetry while the
// API answers 400 (the console is not ready yet), for up to vmConsoleWait; waiting is called
// the first time it waits.
func dialVMConsoleWaiting(ctx context.Context, k *kubeClient, path string, waiting func()) (*websocket.Conn, error) {
	deadline := time.Now().Add(vmConsoleWait)

	for attempt := 0; ; attempt++ {
		ws, err := k.dialVMConsole(ctx, path)

		var refused *vmConsoleRefusedError
		if err == nil || !errors.As(err, &refused) || refused.status != http.StatusBadRequest {
			return ws, err
		}

		if time.Now().Add(vmConsoleRetry).After(deadline) {
			return nil, errVMConsoleTimeout
		}

		if attempt == 0 {
			waiting()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(vmConsoleRetry):
		}
	}
}

// dialVMConsole opens the console WebSocket at path. It dials the connection itself rather
// than through websocket.Config.DialContext, whose refusal (ErrBadStatus) drops the HTTP
// status: the console's 400 (not ready, ask again) has to be told from a 403 or a 404.
func (k *kubeClient) dialVMConsole(ctx context.Context, path string) (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, vmConsoleDialTimeout)
	defer cancel()

	cfg, err := k.streamConfig(ctx, path, []string{vmConsoleProtocol})
	if err != nil {
		return nil, err
	}

	conn, err := k.dialAPIServer(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// The handshake has no context of its own: an expired one unblocks it through the deadline.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	peek := &statusPeekConn{Conn: conn}

	ws, err := websocket.NewClient(cfg, peek)
	if !stop() || err != nil {
		_ = conn.Close()

		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, websocket.ErrBadStatus):
			return nil, &vmConsoleRefusedError{status: peek.status()}
		case err != nil:
			return nil, fmt.Errorf("console handshake: %w", err)
		}

		return nil, ctx.Err()
	}

	return ws, nil
}

// hostWithPort is host with the HTTPS port when it names none.
func hostWithPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}

	return net.JoinHostPort(strings.Trim(host, "[]"), "443")
}

// statusPeekConn keeps the first bytes read from the connection: the status line of the
// handshake's answer.
type statusPeekConn struct {
	net.Conn

	mu   sync.Mutex
	head []byte
}

const statusPeekMax = 32 // "HTTP/1.1 400 Bad Request" and a little more

func (c *statusPeekConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)

	c.mu.Lock()
	if room := statusPeekMax - len(c.head); room > 0 {
		c.head = append(c.head, p[:min(n, room)]...)
	}
	c.mu.Unlock()

	return n, err
}

// status is the HTTP status the server answered, 0 when it could not be read.
func (c *statusPeekConn) status() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	fields := bytes.Fields(c.head)
	if len(fields) < 2 || !bytes.HasPrefix(fields[0], []byte("HTTP/")) {
		return 0
	}

	code, err := strconv.Atoi(string(fields[1]))
	if err != nil {
		return 0
	}

	return code
}

// serveRaw carries a raw byte stream over ws until it ends: typed bytes one way as they are,
// output the other. Resizes are dropped. The far side closing is "disconnected", not an error.
func (d *DebugSession) serveRaw(ctx context.Context, ws *websocket.Conn) {
	// Closing the connection is what ends a blocked Receive when the session is closed.
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	defer ws.Close() //nolint:errcheck

	ws.MaxPayloadBytes = kubeExecMaxOutput
	ws.PayloadType = websocket.BinaryFrame

	go d.forwardRawInput(ctx, ws)

	for {
		var frame []byte

		err := websocket.Message.Receive(ws, &frame)

		switch {
		case ctx.Err() != nil:
			d.exit(-1, "closed")

			return
		case err != nil:
			d.exit(0, vmConsoleDisconnected)

			return
		case len(frame) > 0:
			d.listener.OnOutput(frame)
		}
	}
}

func (d *DebugSession) forwardRawInput(ctx context.Context, ws *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-d.send:
			if in.data == nil {
				continue // a serial console has no size
			}

			if err := websocket.Message.Send(ws, in.data); err != nil {
				return
			}
		}
	}
}
