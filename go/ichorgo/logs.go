package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

const (
	defaultLogLines = 500
	maxLogLines     = 5000
)

type logTail struct {
	Lines     []string   `json:"lines"`
	Entries   []logEntry `json:"entries"`   // lines parsed, same order and count
	Truncated bool       `json:"truncated"` // older lines were dropped
}

// ServiceLogs returns the last tailLines lines of a Talos service log (e.g. "kubelet", "etcd")
// as JSON logTail, like `talosctl logs SERVICE --tail N`.
func ServiceLogs(configYAML, contextName, node, service string, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ServiceLogs", configYAML, contextName, node, service, fmt.Sprint(tailLines))
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		if strings.TrimSpace(service) == "" {
			return "", errors.New("no service given")
		}

		n := clampTail(tailLines)

		stream, err := s.client.Logs(client.WithNode(ctx, node), constants.SystemContainerdNamespace,
			common.ContainerDriver_CONTAINERD, service, false, int32(n))
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		tail, err := drainStream(stream.Recv, n)
		if err != nil {
			return "", err
		}

		return toJSON(tail)
	})
}

// KernelLogs returns the last tailLines lines of the kernel ring buffer (`talosctl dmesg`).
func KernelLogs(configYAML, contextName, node string, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("KernelLogs", configYAML, contextName, node, "", fmt.Sprint(tailLines))
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		stream, err := s.client.Dmesg(client.WithNode(ctx, node), false, false)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		tail, err := drainStream(stream.Recv, clampTail(tailLines))
		if err != nil {
			return "", err
		}

		return toJSON(tail)
	})
}

func clampTail(n int) int {
	return clampOr(n, defaultLogLines, maxLogLines)
}

// clampOr is v capped at maxV, or def when v is not positive (unset).
func clampOr[T int | int64](v, def, maxV T) T {
	if v <= 0 {
		return def
	}

	return min(v, maxV)
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
			return logTail{}, friendlyErr(err)
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
	split     *lineSplitter
	truncated bool
}

func newTailLines(maxLines int) *tailLines {
	t := &tailLines{max: maxLines}
	t.split = newLineSplitter(t.push)

	return t
}

func (t *tailLines) write(b []byte) {
	t.split.write(b)
}

func (t *tailLines) push(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
		t.truncated = true
	}
}

func (t *tailLines) result() logTail {
	t.split.flush()

	lines := append([]string{}, t.lines...)

	return logTail{Lines: lines, Entries: parseLogLines(lines, time.Now()), Truncated: t.truncated}
}
