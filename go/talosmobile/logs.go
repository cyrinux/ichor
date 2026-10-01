package talosmobile

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

const (
	defaultLogLines = 500
	maxLogLines     = 5000
)

type logTail struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"` // older lines were dropped
}

// ServiceLogs returns the last tailLines lines of a Talos service log (e.g. "kubelet", "etcd")
// as JSON logTail, like `talosctl logs SERVICE --tail N`.
func ServiceLogs(configYAML, contextName, node, service string, tailLines int) (string, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if strings.TrimSpace(service) == "" {
			return "", errors.New("no service given")
		}

		n := clampTail(tailLines)

		stream, err := s.client.Logs(client.WithNode(ctx, node), constants.SystemContainerdNamespace,
			common.ContainerDriver_CONTAINERD, service, false, int32(n))
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		tail, err := drainStream(stream.Recv, n)
		if err != nil {
			return "", err
		}

		return toJSON(tail)
	})
}

// KernelLogs returns the last tailLines lines of the kernel ring buffer (`talosctl dmesg`).
func KernelLogs(configYAML, contextName, node string, tailLines int) (string, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		stream, err := s.client.Dmesg(client.WithNode(ctx, node), false, false)
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		tail, err := drainStream(stream.Recv, clampTail(tailLines))
		if err != nil {
			return "", err
		}

		return toJSON(tail)
	})
}

func clampTail(n int) int {
	switch {
	case n <= 0:
		return defaultLogLines
	case n > maxLogLines:
		return maxLogLines
	default:
		return n
	}
}

// drainStream reads a Talos byte stream to EOF, keeping only the last n lines.
func drainStream(recv func() (*common.Data, error), n int) (logTail, error) {
	tl := newTailLines(n)

	for {
		msg, err := recv()

		switch {
		case errors.Is(err, io.EOF):
			return tl.result(), nil
		case err != nil:
			return logTail{}, errors.New(friendlyError(err))
		}

		if e := msg.GetMetadata().GetError(); e != "" {
			return logTail{}, errors.New(e)
		}

		tl.write(msg.GetBytes())
	}
}

// tailLines is a ring of the last max complete lines plus the pending partial line.
type tailLines struct {
	max       int
	lines     []string
	partial   strings.Builder
	truncated bool
}

func newTailLines(maxLines int) *tailLines {
	return &tailLines{max: maxLines}
}

func (t *tailLines) write(b []byte) {
	for len(b) > 0 {
		i := strings.IndexByte(string(b), '\n')
		if i < 0 {
			t.partial.Write(b)

			return
		}

		t.partial.Write(b[:i])
		t.push(strings.TrimSuffix(t.partial.String(), "\r"))
		t.partial.Reset()
		b = b[i+1:]
	}
}

func (t *tailLines) push(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
		t.truncated = true
	}
}

func (t *tailLines) result() logTail {
	if t.partial.Len() > 0 {
		t.push(t.partial.String())
		t.partial.Reset()
	}

	return logTail{Lines: append([]string{}, t.lines...), Truncated: t.truncated}
}
