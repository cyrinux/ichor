package ichorgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/net/websocket"
)

// The channels a terminal adds to the Kubernetes stream protocol.
const (
	execStdin  = 0
	execResize = 4 // a JSON {"Width","Height"}
)

// podShellAuto runs bash when the image has it, sh otherwise, like most `kubectl exec -it` habits.
var podShellAuto = []string{"sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}

// StartPodShell opens a terminal in a running container, like `kubectl exec -it -n NAMESPACE
// POD -c CONTAINER -- COMMAND` (container "" for a pod with one). An empty command runs bash,
// or sh when the image lacks it. Output and exit go to listener, as for StartDebugShell.
func StartPodShell(configYAML, contextName, kubeServer, namespace, pod, container, command string, cols, rows int, listener DebugListener) *DebugSession {
	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	container = privacy.reveal(strings.TrimSpace(container))

	d, ctx := newDebugSession(listener)

	go d.runExec(ctx, kubeTarget{configYAML, contextName, kubeServer}, namespace, pod, container, podShellArgs(command), cols, rows)

	return d
}

func podShellArgs(command string) []string {
	if fields := strings.Fields(command); len(fields) > 0 {
		return fields
	}

	return podShellAuto
}

func (d *DebugSession) runExec(ctx context.Context, target kubeTarget, namespace, pod, container string, argv []string, cols, rows int) {
	defer d.cancel()
	defer onPanic(func(msg string) { d.exit(-1, msg) })

	if err := validateExecTarget(namespace, pod, container, argv); err != nil {
		d.exit(-1, err.Error())

		return
	}

	if isDemoContext(target.config, target.context) {
		d.exit(-1, errDemoUnavailable.Error())

		return
	}

	k, _, err := kubeClients.get(target)
	if err != nil {
		d.exit(-1, kubeError(err).Error())

		return
	}

	ws, err := k.dialExec(ctx, namespace, pod, container, argv, true)
	if err != nil {
		d.exitDialFailed(ctx, err)

		return
	}

	d.serveTTY(ctx, ws, cols, rows, false)
}

func (d *DebugSession) exitDialFailed(ctx context.Context, err error) {
	if ctx.Err() != nil {
		d.exit(-1, "closed")
	} else {
		d.exit(-1, err.Error())
	}
}

// serveTTY carries the terminal over ws until it ends: input and resizes one way, output the
// other. attached: a stream from attach, whose end without a status is the process exiting.
func (d *DebugSession) serveTTY(ctx context.Context, ws *websocket.Conn, cols, rows int, attached bool) {
	// Closing the connection is what ends a blocked Receive when the session is closed.
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	defer ws.Close() //nolint:errcheck

	ws.MaxPayloadBytes = kubeExecMaxOutput

	if cols > 0 && rows > 0 {
		d.Resize(cols, rows)
	}

	go d.forwardExecInput(ctx, ws)

	d.receiveExec(ctx, ws, attached)
}

func (d *DebugSession) forwardExecInput(ctx context.Context, ws *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case in := <-d.send:
			if err := websocket.Message.Send(ws, in.execFrame()); err != nil {
				return
			}
		}
	}
}

func (in shellInput) execFrame() []byte {
	if in.data == nil {
		return fmt.Appendf([]byte{execResize}, `{"Width":%d,"Height":%d}`, in.cols, in.rows)
	}

	return append([]byte{execStdin}, in.data...)
}

// receiveExec hands the terminal output to the listener until the command ends: its exit
// code comes on the status channel, just before the server closes the stream. attached: an
// attach stream, which may end without a status when the process exits.
func (d *DebugSession) receiveExec(ctx context.Context, ws *websocket.Conn, attached bool) {
	var status bytes.Buffer

	for {
		var frame []byte

		err := websocket.Message.Receive(ws, &frame)

		switch {
		case ctx.Err() != nil:
			d.exit(-1, "closed")

			return
		case errors.Is(err, io.EOF) && status.Len() == 0 && attached:
			d.exit(0, "")

			return
		case errors.Is(err, io.EOF) && status.Len() == 0:
			// v4 and v5 always send a status once the command ends: none is a dropped connection.
			d.exit(-1, "connection closed")

			return
		case errors.Is(err, io.EOF):
			d.exit(execExit(status.Bytes()))

			return
		case err != nil:
			d.exit(-1, "exec stream: "+err.Error())

			return
		}

		if len(frame) < 2 {
			continue
		}

		switch frame[0] {
		case execStdout, execStderr:
			d.listener.OnOutput(frame[1:])
		case execStatus:
			if status.Len() < kubeExecMaxLine {
				status.Write(frame[1:])
			}
		}
	}
}

// execExit is the exit code and message of a finished command, from its status.
func execExit(status []byte) (int, string) {
	err := execStatusError(status)
	if err == nil {
		return 0, ""
	}

	var execErr *kubeExecError
	if errors.As(err, &execErr) && execErr.ExitCode >= 0 {
		return execErr.ExitCode, ""
	}

	return -1, err.Error()
}
