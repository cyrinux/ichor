package ichorgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/net/websocket"
)

// kubeExecMaxLine caps one stdout line of a streamed command: longer ones are dropped.
const kubeExecMaxLine = 256 << 10

// execLines runs argv in a container like exec, but for a command that keeps writing
// (`hubble observe --follow`): every complete stdout line goes to onLine as it arrives,
// until the command ends or ctx is cancelled (then the error is ctx's). Stderr is kept, up
// to kubeExecMaxOutput, for the error of a command that failed. onStart, when set, is called
// once the command runs.
func (k *kubeClient) execLines(ctx context.Context, namespace, pod, container string, argv []string, onStart func(), onLine func([]byte)) error {
	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if !kubeNamePattern.MatchString(container) || len(argv) == 0 {
		return fmt.Errorf("invalid exec target %q %v", container, argv)
	}

	cfg, err := k.execConfig(namespace, pod, container, argv)
	if err != nil {
		return err
	}

	ws, err := cfg.DialContext(ctx)
	if err != nil {
		var dialErr *websocket.DialError
		if errors.As(err, &dialErr) {
			if errors.Is(dialErr.Err, websocket.ErrBadStatus) {
				return errExecRefused
			}

			return dialErr.Err
		}

		return err
	}

	// Closing the connection is what ends a blocked Receive when ctx is cancelled.
	stop := context.AfterFunc(ctx, func() { _ = ws.Close() })
	defer stop()
	defer ws.Close() //nolint:errcheck

	ws.MaxPayloadBytes = kubeExecMaxLine

	if onStart != nil {
		onStart()
	}

	err = readExecLines(ws, onLine)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	return err
}

// readExecLines demultiplexes the frames like readExecStream, splitting stdout into lines.
func readExecLines(ws *websocket.Conn, onLine func([]byte)) error {
	var (
		pending        []byte
		errOut, status bytes.Buffer
		skipping       bool // inside a line longer than kubeExecMaxLine
	)

	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return fmt.Errorf("exec stream: %w", err)
		}

		if len(frame) < 2 {
			continue
		}

		switch frame[0] {
		case execStdout:
			pending, skipping = splitLines(append(pending, frame[1:]...), skipping, onLine)
		case execStderr:
			if errOut.Len()+len(frame)-1 <= kubeExecMaxOutput {
				errOut.Write(frame[1:])
			}
		case execStatus:
			status.Write(frame[1:])
		}
	}

	if len(pending) > 0 && !skipping {
		onLine(pending)
	}

	if err := execStatusError(status.Bytes()); err != nil {
		var execErr *kubeExecError
		if errors.As(err, &execErr) && len(bytes.TrimSpace(errOut.Bytes())) > 0 {
			execErr.Message = string(bytes.TrimSpace(errOut.Bytes()))
		}

		return err
	}

	return nil
}

// splitLines hands every complete line of buf to onLine and returns the incomplete rest.
// A line over kubeExecMaxLine is dropped up to its end (skipping carries that over).
func splitLines(buf []byte, skipping bool, onLine func([]byte)) ([]byte, bool) {
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}

		if !skipping && i > 0 {
			onLine(buf[:i])
		}

		skipping = false
		buf = buf[i+1:]
	}

	if len(buf) > kubeExecMaxLine {
		return nil, true
	}

	// A copy: the caller appends the next frame to it.
	return append([]byte(nil), buf...), skipping
}
