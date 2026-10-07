package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/websocket"
)

const (
	// kubeExecTimeout bounds one command run in a container.
	kubeExecTimeout = 10 * time.Second
	// kubeExecMaxOutput caps what the app reads from each of stdout and stderr.
	kubeExecMaxOutput = 1 << 20
)

// execLimits bound one command: how long it may run and how much it may write.
type execLimits struct {
	timeout   time.Duration
	maxOutput int
}

var defaultExecLimits = execLimits{kubeExecTimeout, kubeExecMaxOutput}

// kubeExecProtocols are the WebSocket subprotocols of `kubectl exec`, newest first: v5 adds
// a close signal the app does not need, v4 is what every supported Kubernetes accepts.
var kubeExecProtocols = []string{"v5.channel.k8s.io", "v4.channel.k8s.io"}

// The channels of the Kubernetes stream protocol: the first byte of every frame.
const (
	execStdout = 1
	execStderr = 2
	execStatus = 3 // a JSON metav1.Status once the command ended
)

// kubeExecError is a command that ran and failed (ExitCode >= 0), or a status the API
// server sent instead of running it (ExitCode -1).
type kubeExecError struct {
	ExitCode int
	Message  string
}

func (e *kubeExecError) Error() string {
	if e.ExitCode >= 0 {
		return fmt.Sprintf("command exited with code %d: %s", e.ExitCode, e.Message)
	}

	return "exec failed: " + e.Message
}

// errExecRefused is an exec the API server did not upgrade to a stream: forbidden, the pod
// or container is gone, or exec over WebSocket is not supported.
var errExecRefused = errors.New("exec refused by the Kubernetes API (forbidden, pod gone, or WebSocket exec unsupported)")

// exec runs argv in a container like `kubectl exec -n NAMESPACE POD -c CONTAINER -- ARGV`,
// without stdin or a terminal, and returns what it wrote. Callers pass fixed commands only:
// nothing a user types reaches argv. A non-zero exit returns the output with a *kubeExecError.
func (k *kubeClient) exec(ctx context.Context, namespace, pod, container string, argv []string) (stdout, stderr []byte, err error) {
	return k.execWith(ctx, defaultExecLimits, namespace, pod, container, argv)
}

// execWith is exec with its own limits, for the commands that answer more than a status.
func (k *kubeClient) execWith(ctx context.Context, limits execLimits, namespace, pod, container string, argv []string) (stdout, stderr []byte, err error) {
	if err := validateExecTarget(namespace, pod, container, argv); err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, limits.timeout)
	defer cancel()

	ws, err := k.dialExec(ctx, namespace, pod, container, argv, false)
	if err != nil {
		return nil, nil, err
	}
	defer ws.Close() //nolint:errcheck

	if deadline, ok := ctx.Deadline(); ok {
		_ = ws.SetDeadline(deadline)
	}

	ws.MaxPayloadBytes = limits.maxOutput

	return readExecStream(ws, limits.maxOutput)
}

// validateExecTarget checks the pod, the container ("" lets Kubernetes pick a pod's only one)
// and that there is a command.
func validateExecTarget(namespace, pod, container string, argv []string) error {
	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if (container != "" && !kubeNamePattern.MatchString(container)) || len(argv) == 0 {
		return fmt.Errorf("invalid exec target %q %v", container, argv)
	}

	return nil
}

// dialExec opens the exec stream of argv in the container: stdout and stderr only, or with
// tty, stdin and a terminal (stderr then comes merged into stdout, as Kubernetes requires).
func (k *kubeClient) dialExec(ctx context.Context, namespace, pod, container string, argv []string, tty bool) (*websocket.Conn, error) {
	query := url.Values{"command": argv, "stdout": {"true"}}
	if container != "" {
		query.Set("container", container)
	}

	if tty {
		query.Set("stdin", "true")
		query.Set("tty", "true")
	} else {
		query.Set("stderr", "true")
	}

	u, err := k.endpoint(podPath(namespace, pod) + "/exec?" + query.Encode())
	if err != nil {
		return nil, err
	}

	u.Scheme = "wss"

	cfg, err := websocket.NewConfig(u.String(), (&url.URL{Scheme: "https", Host: u.Host}).String())
	if err != nil {
		return nil, fmt.Errorf("exec URL: %w", err)
	}

	cfg.Protocol = kubeExecProtocols
	cfg.TlsConfig = k.tls.Clone()
	cfg.Dialer = &net.Dialer{Timeout: kubeProbeTimeout}
	cfg.Header.Set("User-Agent", "ichor")

	token, err := k.bearer(ctx)
	if err != nil {
		return nil, err
	}

	if token != "" {
		cfg.Header.Set("Authorization", "Bearer "+token)
	}

	ws, err := cfg.DialContext(ctx)
	if err != nil {
		// websocket.DialError does not unwrap: look at its Err by hand.
		var dialErr *websocket.DialError
		if errors.As(err, &dialErr) {
			if errors.Is(dialErr.Err, websocket.ErrBadStatus) {
				return nil, errExecRefused
			}

			return nil, dialErr.Err
		}

		return nil, err
	}

	return ws, nil
}

// readExecStream demultiplexes the frames until the server closes the stream.
func readExecStream(ws *websocket.Conn, maxOutput int) (stdout, stderr []byte, err error) {
	var out, errOut, status bytes.Buffer

	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, nil, fmt.Errorf("exec stream: %w", err)
		}

		// An empty frame (the channel byte alone) only announces the channel.
		if len(frame) < 2 {
			continue
		}

		var dst *bytes.Buffer

		switch frame[0] {
		case execStdout:
			dst = &out
		case execStderr:
			dst = &errOut
		case execStatus:
			dst = &status
		default:
			continue
		}

		if dst.Len()+len(frame)-1 > maxOutput {
			return nil, nil, fmt.Errorf("exec output is larger than the app reads (%d MiB)", maxOutput>>20)
		}

		dst.Write(frame[1:])
	}

	return out.Bytes(), errOut.Bytes(), execStatusError(status.Bytes())
}

// execStatusError reads the status channel: nil on success (or when the server sent none).
func execStatusError(raw []byte) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var st struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details struct {
			Causes []struct {
				Reason  string `json:"reason"`
				Message string `json:"message"`
			} `json:"causes"`
		} `json:"details"`
	}

	if err := json.Unmarshal(raw, &st); err != nil {
		return &kubeExecError{ExitCode: -1, Message: string(bytes.TrimSpace(raw))}
	}

	if st.Status == "Success" {
		return nil
	}

	e := &kubeExecError{ExitCode: -1, Message: st.Message}

	for _, cause := range st.Details.Causes {
		if cause.Reason == "ExitCode" {
			if code, err := strconv.Atoi(cause.Message); err == nil {
				e.ExitCode = code
			}
		}
	}

	return e
}
