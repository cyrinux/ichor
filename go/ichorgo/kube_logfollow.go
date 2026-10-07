package ichorgo

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// kubeLogLineMax bounds one log line; a longer one is cut (a JSON blob on one line).
const kubeLogLineMax = 256 << 10

// StartPodLogFollow follows a container's log through the Kubernetes API, like `kubectl logs
// -f --tail=N`: the last tailLines lines (capped like KubePodLogs), then each new line as it
// is written, until Cancel or the container stops. container may be "" for a pod with one.
func StartPodLogFollow(configYAML, contextName, kubeServer, namespace, pod, container string, tailLines int, listener LogListener) *LogRun {
	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	container = privacy.reveal(strings.TrimSpace(container))

	listener = maskedLogListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := followPodLog(ctx, kubeTarget{configYAML, contextName, kubeServer}, namespace, pod, container, tailLines, listener.OnLine)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			err = nil
		}

		msg := ""
		if err != nil {
			msg = err.Error()
		}

		listener.OnDone(msg)
	}()

	return &LogRun{cancel: cancel}
}

func followPodLog(ctx context.Context, target kubeTarget, namespace, pod, container string, tailLines int, onLine func(string)) error {
	if err := validateKubeName("pod", namespace, pod); err != nil {
		return err
	}

	if container != "" && !kubeNamePattern.MatchString(container) {
		return fmt.Errorf("invalid container name %q", container)
	}

	if tailLines <= 0 || tailLines > maxLogTail {
		tailLines = maxLogTail
	}

	if isDemoContext(target.config, target.context) {
		for _, line := range strings.Split(strings.TrimRight(demoPodLog(pod, false), "\n"), "\n") {
			onLine(line)
		}

		<-ctx.Done()

		return ctx.Err()
	}

	query := url.Values{}
	query.Set("follow", "true")
	query.Set("tailLines", strconv.Itoa(tailLines))

	if container != "" {
		query.Set("container", container)
	}

	k, _, err := kubeClients.get(target)
	if err != nil {
		return err
	}

	// Not through withKube: a follow outlives callTimeout, and ending it is not a failure.
	err = k.stream(ctx, podPath(namespace, pod)+"/log?"+query.Encode(), "text/plain, */*", func(r io.Reader) error {
		return readLogLines(r, onLine)
	})
	if err != nil && ctx.Err() == nil {
		return kubeError(err)
	}

	return err
}

// readLogLines hands each line of r to onLine until r ends.
func readLogLines(r io.Reader, onLine func(string)) error {
	reader := bufio.NewReaderSize(r, 64<<10)

	for {
		line, err := reader.ReadSlice('\n')

		// A line longer than the buffer comes in pieces: keep the first kubeLogLineMax bytes.
		if errors.Is(err, bufio.ErrBufferFull) {
			full := append([]byte(nil), line...)

			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = reader.ReadSlice('\n')
				if len(full) < kubeLogLineMax {
					full = append(full, line[:min(len(line), kubeLogLineMax-len(full))]...)
				}
			}

			line = full
		}

		if len(line) > 0 {
			onLine(strings.TrimRight(string(line), "\r\n"))
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}
	}
}
