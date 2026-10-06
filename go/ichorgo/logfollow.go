package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// LogListener receives followed log lines (implemented in Kotlin/Swift).
type LogListener interface {
	OnLine(line string)
	// OnDone is called exactly once; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// LogRun is a handle on a followed log.
type LogRun struct {
	cancel context.CancelFunc
}

// Cancel stops following.
func (r *LogRun) Cancel() { r.cancel() }

// StartLogFollow streams a service log (or the kernel log when service is empty) from node,
// like `talosctl logs -f SERVICE --tail N` / `talosctl dmesg -f` (os:reader).
func StartLogFollow(configYAML, contextName, node, service string, tailLines int, listener LogListener) *LogRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedLogListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		listener.OnDone(followLog(ctx, configYAML, contextName, node, serviceLogOpener(strings.TrimSpace(service), clampTail(tailLines)), listener, service, tailLines))
	}()

	return &LogRun{cancel: cancel}
}

// logOpener opens a log stream on node (already set in ctx).
type logOpener func(ctx context.Context, s *session) (func() (*common.Data, error), error)

func serviceLogOpener(service string, tail int) logOpener {
	return func(ctx context.Context, s *session) (func() (*common.Data, error), error) {
		if service == "" {
			stream, err := s.client.Dmesg(ctx, true, true)
			if err != nil {
				return nil, err
			}

			return stream.Recv, nil
		}

		stream, err := s.client.Logs(ctx, constants.SystemContainerdNamespace, common.ContainerDriver_CONTAINERD, service, true, int32(tail))
		if err != nil {
			return nil, err
		}

		return stream.Recv, nil
	}
}

func followLog(ctx context.Context, configYAML, contextName, node string, open logOpener, listener LogListener, demoSource string, demoTail int) string {
	if isDemoContext(configYAML, contextName) {
		out, err := demoRead("ServiceLogs", configYAML, contextName, node, demoSource, fmt.Sprint(demoTail))
		if err != nil {
			return err.Error()
		}
		var tail logTail
		if err := json.Unmarshal([]byte(out), &tail); err != nil {
			return err.Error()
		}
		for _, line := range tail.Lines {
			if ctx.Err() != nil {
				return ""
			}
			listener.OnLine(line)
		}
		if demoSource == "" {
			demoSource = "kernel"
		}
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ""
			case at := <-ticker.C:
				listener.OnLine(demoLogLine(demoSource, at))
			}
		}
	}
	s, release, err := acquireNode(configYAML, contextName, node)
	if err != nil {
		return err.Error()
	}

	defer release()

	recv, err := open(client.WithNode(ctx, node), s)
	if err != nil {
		return s.friendly(node, err)
	}

	lines := newLineSplitter(listener.OnLine)

	for {
		msg, err := recv()

		switch {
		case errors.Is(err, io.EOF):
			lines.flush()

			return ""
		case err != nil:
			lines.flush()

			if ctx.Err() != nil {
				return ""
			}

			return friendlyError(err)
		}

		if e := msg.GetMetadata().GetError(); e != "" {
			return e
		}

		lines.write(msg.GetBytes())
	}
}

// maxLineBytes cuts a line that never ends (progress bars redrawn with \r, binary output):
// a follow can run for hours.
const maxLineBytes = 64 << 10

// lineSplitter emits complete lines from a byte stream that may split them anywhere.
type lineSplitter struct {
	emit    func(string)
	partial strings.Builder
}

func newLineSplitter(emit func(string)) *lineSplitter {
	return &lineSplitter{emit: emit}
}

func (l *lineSplitter) write(b []byte) {
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		if i < 0 {
			l.partial.Write(b)

			if l.partial.Len() >= maxLineBytes {
				l.flush()
			}

			return
		}

		l.partial.Write(b[:i])
		l.emit(strings.TrimSuffix(l.partial.String(), "\r"))
		l.partial.Reset()
		b = b[i+1:]
	}
}

func (l *lineSplitter) flush() {
	if l.partial.Len() > 0 {
		l.emit(l.partial.String())
		l.partial.Reset()
	}
}
