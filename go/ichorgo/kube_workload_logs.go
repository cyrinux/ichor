package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/kube"
)

const (
	// workloadLogMaxPods is how many pods of a workload one read covers.
	workloadLogMaxPods = 20
	// workloadLogMaxTail bounds the lines read per pod.
	workloadLogMaxTail = 2000
	// workloadLogMaxBytes bounds the lines' text in the answer: the newest are kept.
	workloadLogMaxBytes = 4 << 20
	// workloadLogColors is how many colours the apps have for the pods.
	workloadLogColors = 8
	// logFilterMaxPattern bounds a filter, plain or regular expression.
	logFilterMaxPattern = 1024
)

type workloadLogPod struct {
	Name      string `json:"name"`
	Container string `json:"container"`
	Color     int    `json:"color"` // 0..7, the pod's place in the list
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated"` // its tail or its size cap was reached
}

type workloadLogLine struct {
	Pod   string `json:"pod"`
	Time  int64  `json:"time"` // unix ms, the kubelet's; 0 when unknown
	Text  string `json:"text"`
	Level string `json:"level"`
}

type workloadLogs struct {
	Pods         []workloadLogPod  `json:"pods"`
	Lines        []workloadLogLine `json:"lines"`
	Matched      int               `json:"matched"`
	Scanned      int               `json:"scanned"`
	Truncated    bool              `json:"truncated"`
	SinceMinutes int               `json:"sinceMinutes"`
}

// podLogText is what one pod's read gave, before merging.
type podLogText struct {
	name      string
	container string
	log       string
	truncated bool
}

// KubeWorkloadLogs reads the logs of the pods of a Deployment, StatefulSet or DaemonSet (kind)
// through the Kubernetes API (`get pods/log`), merged by time, oldest first:
// {"pods":[{"name","container","color","lines","truncated"}],"lines":[{"pod","time","text","level"}],
// "matched","scanned","truncated","sinceMinutes"}. The first workloadLogMaxPods pods are read,
// their first container unless container names one; a pod that cannot answer (still starting,
// its kubelet down) stays listed with no lines, while a refusal is returned as the error.
// filter keeps the lines containing it, whatever the case, or matching it when regex (Go
// syntax); matched and scanned count the whole read, and lines holds the newest
// workloadLogMaxBytes of the matches. sinceMinutes bounds the read in time (0: none),
// tailLines the lines per pod (0 or over workloadLogMaxTail: workloadLogMaxTail).
func KubeWorkloadLogs(configYAML, contextName, kubeServer, kind, namespace, name, container, filter string, regex bool, sinceMinutes, tailLines int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))
	container = privacy.reveal(strings.TrimSpace(container))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	if container != "" && !kubeNamePattern.MatchString(container) {
		return "", fmt.Errorf("invalid container name %q", container)
	}

	match, err := newLogFilter(filter, regex)
	if err != nil {
		return "", err
	}

	sinceMinutes = max(sinceMinutes, 0)
	if tailLines <= 0 || tailLines > workloadLogMaxTail {
		tailLines = workloadLogMaxTail
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() workloadLogs { return demoWorkloadLogs(wk, namespace, name, match, sinceMinutes) },
		func(ctx context.Context, k *kubeClient) (workloadLogs, error) {
			q := pageQuery{limit: workloadLogMaxPods}

			page, err := listWorkloadPodsPage(ctx, k, wk, namespace, name, q)
			if err != nil {
				return workloadLogs{}, err
			}

			learnPodNames(page.Pods)

			reads, err := readPodLogs(ctx, k, page.Pods, container, sinceMinutes, tailLines)
			if err != nil {
				return workloadLogs{}, err
			}

			logs := mergeWorkloadLogs(reads, match, sinceMinutes)
			logs.Truncated = logs.Truncated || !page.Complete

			return logs, nil
		})
}

// KubePodLogsFiltered is KubePodLogs keeping the lines filter takes (see KubeWorkloadLogs) and
// bounded to the last sinceMinutes (0: no bound).
func KubePodLogsFiltered(configYAML, contextName, kubeServer, namespace, pod, container string, previous bool, tailLines int, filter string, regex bool, sinceMinutes int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	match, err := newLogFilter(filter, regex)
	if err != nil {
		return "", err
	}

	log, err := readPodLog(configYAML, contextName, kubeServer, namespace, pod, container, previous, tailLines, max(sinceMinutes, 0))
	if err != nil || strings.TrimSpace(filter) == "" {
		return log, err
	}

	var b strings.Builder

	for line := range strings.SplitSeq(log, "\n") {
		if line != "" && match(line) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	return b.String(), nil
}

// newLogFilter is the test a log line must pass: filter as a regular expression, else as text
// in any case; everything when it is empty. The filter may hold a name masked on screen.
func newLogFilter(filter string, regex bool) (func(string) bool, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return func(string) bool { return true }, nil
	}

	if len(filter) > logFilterMaxPattern {
		return nil, fmt.Errorf("the filter is too long: at most %d bytes", logFilterMaxPattern)
	}

	if regex {
		re, err := regexp.Compile(filter)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}

		return re.MatchString, nil
	}

	want := strings.ToLower(privacy.unmaskText(filter))

	return func(line string) bool { return strings.Contains(strings.ToLower(line), want) }, nil
}

// readPodLogs reads the log of each pod, appWorkloadsParallel at a time, in the pods' order. A
// pod that cannot answer has no log; every pod failing, or a refusal, is an error.
func readPodLogs(ctx context.Context, k *kubeClient, pods []kubePod, container string, sinceMinutes, tailLines int) ([]podLogText, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	reads := make([]podLogText, len(pods))

	var (
		mu       sync.Mutex
		failed   int
		firstErr error
	)

	forEachLimit(pods, appWorkloadsParallel, func(i int, pod kubePod) {
		if ctx.Err() != nil {
			return
		}

		read, err := readWorkloadPodLog(ctx, k, pod, container, sinceMinutes, tailLines)
		reads[i] = read

		if err == nil {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		failed++
		firstErr = cmp.Or(firstErr, err)

		if code := kube.Code(err); code == http.StatusForbidden || code == http.StatusUnauthorized {
			cancel(err)
		}
	})

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}

	if len(pods) > 0 && failed == len(pods) {
		return nil, firstErr
	}

	return reads, nil
}

// readWorkloadPodLog reads one pod's log with the kubelet's timestamps.
func readWorkloadPodLog(ctx context.Context, k *kubeClient, pod kubePod, container string, sinceMinutes, tailLines int) (podLogText, error) {
	if container == "" && len(pod.ContainerNames) > 0 {
		container = pod.ContainerNames[0]
	}

	read := podLogText{name: pod.Name, container: container}

	query := podLogQuery(container, false, tailLines, sinceMinutes)
	query.Set("timestamps", "true")

	log, err := k.getText(ctx, podPath(pod.Namespace, pod.Name)+"/log?"+query.Encode())
	if err != nil {
		return read, err
	}

	read.log = newestBytes(log, maxLogBytes)
	read.truncated = len(read.log) < len(log) || strings.Count(log, "\n") >= tailLines

	return read, nil
}

// podLogQuery is the query of a read of a container's log. No limitBytes: Kubernetes applies
// it to the start of the tail, which would drop the newest lines (the crash the log was opened
// for). The caller bounds the size.
func podLogQuery(container string, previous bool, tailLines, sinceMinutes int) url.Values {
	query := url.Values{}
	query.Set("tailLines", strconv.Itoa(tailLines))

	if container != "" {
		query.Set("container", container)
	}

	if previous {
		query.Set("previous", "true")
	}

	if sinceMinutes > 0 {
		query.Set("sinceSeconds", strconv.Itoa(sinceMinutes*60))
	}

	return query
}

// mergeWorkloadLogs merges the pods' logs by time, a pod's own lines staying in their order,
// keeps those match takes, and cuts the oldest past workloadLogMaxBytes.
func mergeWorkloadLogs(reads []podLogText, match func(string) bool, sinceMinutes int) workloadLogs {
	logs := workloadLogs{Pods: make([]workloadLogPod, 0, len(reads)), Lines: []workloadLogLine{}, SinceMinutes: sinceMinutes}

	var all []workloadLogLine

	now := time.Now()

	for i, read := range reads {
		lines := timedLogLines(read.name, read.log, now)
		all = append(all, lines...)
		logs.Pods = append(logs.Pods, workloadLogPod{read.name, read.container, i % workloadLogColors, len(lines), read.truncated})
		logs.Truncated = logs.Truncated || read.truncated
	}

	slices.SortStableFunc(all, func(a, b workloadLogLine) int { return cmp.Compare(a.Time, b.Time) })

	logs.Scanned = len(all)

	for _, line := range all {
		if match(line.Text) {
			logs.Lines = append(logs.Lines, line)
		}
	}

	logs.Matched = len(logs.Lines)

	size := 0

	for i, line := range slices.Backward(logs.Lines) {
		if size += len(line.Text); size > workloadLogMaxBytes {
			logs.Lines = logs.Lines[i+1:]
			logs.Truncated = true

			break
		}
	}

	return logs
}

// timedLogLines splits a log read with timestamps=true: each line starts with its RFC 3339
// time. A line without one (the kubelet always writes it, a cut line does not) takes the time
// of the line before, so it stays next to it.
func timedLogLines(pod, log string, now time.Time) []workloadLogLine {
	var (
		lines []workloadLogLine
		last  int64
	)

	for raw := range strings.SplitSeq(log, "\n") {
		if raw == "" {
			continue
		}

		text := raw

		if stamp, rest, ok := strings.Cut(raw, " "); ok {
			if at, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				last, text = at.UnixMilli(), rest
			}
		}

		lines = append(lines, workloadLogLine{Pod: pod, Time: last, Text: text, Level: parseLogLine(text, now).Level})
	}

	return lines
}
