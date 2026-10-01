package talosmobile

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
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

		listener.OnDone(followLog(ctx, configYAML, contextName, node, serviceLogOpener(strings.TrimSpace(service), clampTail(tailLines)), listener))
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

func followLog(ctx context.Context, configYAML, contextName, node string, open logOpener, listener LogListener) string {
	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return err.Error()
	}

	defer release()

	recv, err := open(withNode(ctx, node), s)
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
		i := strings.IndexByte(string(b), '\n')
		if i < 0 {
			l.partial.Write(b)

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
