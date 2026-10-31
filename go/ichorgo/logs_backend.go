package ichorgo

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The log backend (Loki, VictoriaLogs): found and reached like a metrics source (the service
// proxy or a URL, the same source JSON), searched with its own query language.

const (
	logsDefaultLimit = 500
	logsMaxLimit     = 2000
	logsMaxValues    = 500
	logsMaxLabel     = 256
	// logsLabelWindow is how far back the label picker looks.
	logsLabelWindow = 24 * time.Hour
)

var logsNow = time.Now

type logsLine struct {
	Time   int64             `json:"time"` // unix ms
	Labels map[string]string `json:"labels"`
	Text   string            `json:"text"`
}

type logsStats struct {
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`
}

type logsResult struct {
	Lines []logsLine `json:"lines"`
	Stats logsStats  `json:"stats"`
}

type logsValues struct {
	Values []string `json:"values"`
}

// LogsPodQuery is the query selecting one pod in a log backend of kind ("loki" or
// "victorialogs"): {namespace="ns",pod="name"} or namespace:"ns" AND pod:"name". Built from
// the names on screen, so it is not masked again: LogsQuery reveals them.
func LogsPodQuery(kind, namespace, pod string) string {
	if kind == logsKindVictoriaLogs {
		return "namespace:" + strconv.Quote(namespace) + " AND pod:" + strconv.Quote(pod)
	}

	return "{namespace=" + strconv.Quote(namespace) + ",pod=" + strconv.Quote(pod) + "}"
}

// LogsWorkloadQuery is the query selecting the pods of one workload by their name prefix, for
// the same backends and with the same masking as LogsPodQuery.
func LogsWorkloadQuery(kind, namespace, workload string) string {
	pods := regexp.QuoteMeta(workload) + "-.*"
	if kind == logsKindVictoriaLogs {
		return "namespace:" + strconv.Quote(namespace) + " AND pod:~" + strconv.Quote(pods)
	}

	return "{namespace=" + strconv.Quote(namespace) + ",pod=~" + strconv.Quote(pods) + "}"
}

// LogsProbe checks the log backend at sourceJSON answers, and returns the source cleaned up
// with its kind, without its secret (the app keeps that apart). A source of no known kind (a
// URL, a Service typed in) is asked as Loki, then as VictoriaLogs.
func LogsProbe(configYAML, contextName, kubeServer, sourceJSON string) (out string, err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	target := kubeTarget{configYAML, contextName, kubeServer}

	src, err = logsResolveKind(target, src, isDemoContext(configYAML, contextName))
	if err != nil {
		return "", err
	}

	src.Secret = ""

	return toJSON(src)
}

func logsResolveKind(target kubeTarget, src promSource, demo bool) (promSource, error) {
	if demo {
		if !logsKnownKind(src.Kind) {
			src.Kind = logsKindLoki
		}

		return src, nil
	}

	kinds := []string{src.Kind}
	if !logsKnownKind(src.Kind) {
		kinds = []string{logsKindLoki, logsKindVictoriaLogs}
	}

	var firstErr error

	for _, kind := range kinds {
		src.Kind = kind
		if _, err := logsReadLabels(target, src, ""); err == nil {
			return src, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}

	return promSource{}, firstErr
}

func logsKnownKind(kind string) bool {
	return kind == logsKindLoki || kind == logsKindVictoriaLogs
}

// LogsLabels lists what the picker offers from the log backend at sourceJSON (see
// LogsDiscover), seen over the last day: with label "", the label names (VictoriaLogs: the
// field names), else the values of label: {"values":[...]}, sorted, at most 500.
func LogsLabels(configYAML, contextName, kubeServer, sourceJSON, label string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	label = strings.TrimSpace(label)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	if len(label) > logsMaxLabel || strings.ContainsAny(label, "\x00\r\n") {
		return "", errors.New("not a label name")
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(logsValues{Values: demoLogsLabels(label)})
	}

	values, err := logsReadLabels(kubeTarget{configYAML, contextName, kubeServer}, src, label)
	if err != nil {
		return "", err
	}

	return toJSON(logsValues{Values: values})
}

// LogsQuery runs query (LogQL or LogsQL, by the source's kind) on the log backend at
// sourceJSON from startMillis to endMillis (unix ms, at most 31 days):
// {"lines":[{"time":ms,"labels":{...},"text"}],"stats":{"returned","truncated"}}, newest first.
// limit bounds the lines: 0 for 500, at most 2000; truncated when it was reached.
func LogsQuery(configYAML, contextName, kubeServer, sourceJSON, query string, startMillis, endMillis int64, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	query = logsRevealQuery(strings.TrimSpace(query))

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	switch {
	case query == "":
		return "", errors.New("the query is empty")
	case len(query) > promMaxQuery:
		return "", errors.New("the query is longer than 4 KiB")
	case strings.ContainsRune(query, 0):
		return "", errors.New("the query has a NUL character")
	case startMillis <= 0 || endMillis <= startMillis:
		return "", errors.New("the time range ends before it starts")
	case endMillis-startMillis > promMaxRange.Milliseconds():
		return "", errors.New("the time range is longer than 31 days")
	}

	if limit <= 0 {
		limit = logsDefaultLimit
	}

	limit = min(limit, logsMaxLimit)

	if isDemoContext(configYAML, contextName) {
		return toJSON(logsResultOf(demoLogsQuery(query, logsNow()), limit))
	}

	start, end := time.UnixMilli(startMillis), time.UnixMilli(endMillis)
	target := kubeTarget{configYAML, contextName, kubeServer}

	var lines []logsLine

	if src.Kind == logsKindVictoriaLogs {
		params := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}, "start": {logsRFC3339(start)}, "end": {logsRFC3339(end)}}

		var body []byte
		if body, err = logsGet(target, src, "/select/logsql/query", params); err == nil {
			lines, err = parseVictoriaLogs(body)
		}
	} else {
		params := url.Values{"query": {query}, "limit": {strconv.Itoa(limit)}, "direction": {"backward"}, "start": {logsNanos(start)}, "end": {logsNanos(end)}}

		var body []byte
		if body, err = logsGet(target, src, "/loki/api/v1/query_range", params); err == nil {
			lines, err = parseLokiStreams(body)
		}
	}

	if err != nil {
		return "", err
	}

	return toJSON(logsResultOf(lines, limit))
}

// logsResultOf sorts the lines newest first and keeps limit of them; the streams of a backend
// come one after the other, not merged by time.
func logsResultOf(lines []logsLine, limit int) logsResult {
	sorted := slices.SortedStableFunc(slices.Values(lines), func(a, b logsLine) int { return cmp.Compare(b.Time, a.Time) })
	truncated := len(sorted) >= limit
	sorted = sorted[:min(len(sorted), limit)]

	if sorted == nil {
		sorted = []logsLine{}
	}

	return logsResult{Lines: sorted, Stats: logsStats{Returned: len(sorted), Truncated: truncated}}
}

var logsQuoted = regexp.MustCompile(`"[^"\\]*"`)

// logsRevealQuery maps the masked names of a query back to the real ones: a quoted value may
// be a namespace or a pod name the app was shown, the rest may hold hostnames and addresses.
func logsRevealQuery(query string) string {
	revealed := logsQuoted.ReplaceAllStringFunc(query, func(quoted string) string {
		value := quoted[1 : len(quoted)-1]

		real := privacy.revealNamespace(value)
		if real == value {
			real = privacy.revealName(value)
		}

		return `"` + real + `"`
	})

	return privacy.reveal(revealed)
}

func logsRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func logsNanos(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }

// logsReadLabels asks the backend for the label names, or the values of label.
func logsReadLabels(target kubeTarget, src promSource, label string) ([]string, error) {
	end := logsNow()
	start := end.Add(-logsLabelWindow)

	if src.Kind == logsKindVictoriaLogs {
		params := url.Values{"query": {"*"}, "limit": {strconv.Itoa(logsMaxValues)}, "start": {logsRFC3339(start)}, "end": {logsRFC3339(end)}}
		path := "/select/logsql/field_names"

		if label != "" {
			params.Set("field", label)
			path = "/select/logsql/field_values"
		}

		body, err := logsGet(target, src, path, params)
		if err != nil {
			return nil, err
		}

		return parseVictoriaValues(body, label == "")
	}

	path := "/loki/api/v1/labels"
	if label != "" {
		path = "/loki/api/v1/label/" + url.PathEscape(label) + "/values"
	}

	body, err := logsGet(target, src, path, url.Values{"start": {logsNanos(start)}, "end": {logsNanos(end)}})
	if err != nil {
		return nil, err
	}

	return parseLokiValues(body)
}

// logsGet reads one API path of the backend; the reason it gives for any failed answer.
func logsGet(target kubeTarget, src promSource, path string, params url.Values) ([]byte, error) {
	status, body, err := promSend(target, src, http.MethodGet, path+"?"+params.Encode(), nil)

	switch {
	case errors.Is(err, errPromNoAnswer):
		return nil, fmt.Errorf("%s: no answer within %s: narrow the query or shorten the range", src.label(), callTimeout)
	case errors.Is(err, errPromTooLarge):
		return nil, errLogsTooLarge
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		return nil, fmt.Errorf("%s: no %s API here (check the port and the path)", src.label(), logsKindName(src.Kind))
	case status/100 != 2:
		return nil, fmt.Errorf("%s: %s refused: %s", src.label(), logsKindName(src.Kind), amErrorMessage(status, body))
	}

	return body, nil
}

var errLogsTooLarge = errors.New("the answer is larger than 16 MiB: narrow the query or lower the limit")

func logsKindName(kind string) string {
	if kind == logsKindVictoriaLogs {
		return "VictoriaLogs"
	}

	return "Loki"
}

// parseLokiValues reads the answer to /labels and /label/<name>/values.
func parseLokiValues(body []byte) ([]string, error) {
	var answer struct {
		Data []string `json:"data"`
	}

	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, errors.New("the answer from Loki cannot be read")
	}

	return logsSortedValues(answer.Data), nil
}

// parseLokiStreams reads the answer to query_range: a metric query has no log lines to show.
func parseLokiStreams(body []byte) ([]logsLine, error) {
	var answer struct {
		Data struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, errors.New("the answer from Loki cannot be read")
	}

	if answer.Data.ResultType != "streams" {
		return nil, errors.New("this is a metric query, with no log lines to show: query the logs, or use the metrics screen")
	}

	var streams []struct {
		Stream map[string]string `json:"stream"`
		Values [][]string        `json:"values"`
	}

	if err := json.Unmarshal(answer.Data.Result, &streams); err != nil {
		return nil, errors.New("the answer from Loki cannot be read")
	}

	lines := []logsLine{}

	for _, s := range streams {
		for _, v := range s.Values {
			if len(v) < 2 {
				continue
			}

			nanos, _ := strconv.ParseInt(v[0], 10, 64)
			lines = append(lines, logsLine{Time: nanos / int64(time.Millisecond), Labels: s.Stream, Text: v[1]})
		}
	}

	return lines, nil
}

// VictoriaLogs' own fields, not labels of a line.
var victoriaOwnFields = []string{"_msg", "_time", "_stream", "_stream_id"}

// parseVictoriaValues reads the answer to field_names and field_values.
func parseVictoriaValues(body []byte, names bool) ([]string, error) {
	var answer struct {
		Values []struct {
			Value string `json:"value"`
		} `json:"values"`
	}

	if err := json.Unmarshal(body, &answer); err != nil {
		return nil, errors.New("the answer from VictoriaLogs cannot be read")
	}

	values := make([]string, 0, len(answer.Values))

	for _, v := range answer.Values {
		if !names || !slices.Contains(victoriaOwnFields, v.Value) {
			values = append(values, v.Value)
		}
	}

	return logsSortedValues(values), nil
}

// parseVictoriaLogs reads the answer to query: one JSON object per line.
func parseVictoriaLogs(body []byte) ([]logsLine, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64<<10), promMaxBody+1)

	lines := []logsLine{}

	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}

		var fields map[string]string
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, errors.New("the answer from VictoriaLogs cannot be read")
		}

		line := logsLine{Text: fields["_msg"], Labels: map[string]string{}}
		if at, err := time.Parse(time.RFC3339Nano, fields["_time"]); err == nil {
			line.Time = at.UnixMilli()
		}

		for name, value := range fields {
			if !slices.Contains(victoriaOwnFields, name) {
				line.Labels[name] = value
			}
		}

		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.New("the answer from VictoriaLogs cannot be read")
	}

	return lines, nil
}

func logsSortedValues(values []string) []string {
	sorted := slices.Sorted(slices.Values(values))
	sorted = sorted[:min(len(sorted), logsMaxValues)]

	if sorted == nil {
		return []string{}
	}

	return sorted
}
