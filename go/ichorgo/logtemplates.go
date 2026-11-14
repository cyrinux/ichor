package ichorgo

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Log templates: a pod in trouble writes the same few messages thousands of times with another
// request id, duration or address each time. Masking the variable tokens (the preprocessing step
// of Drain-style log parsing, no model) folds them into templates the phone can list on one screen.

const (
	// logTemplatesMaxLines is how many lines are read, the newest.
	logTemplatesMaxLines = 4000
	// logTemplatesMaxRunes cuts a template.
	logTemplatesMaxRunes = 200
	// logTemplatesDefault is how many groups are kept when the app asks for 0.
	logTemplatesDefault = 20
	// logTemplateHole stands for a masked token.
	logTemplateHole = "<*>"
)

type logTemplateGroup struct {
	Template string  `json:"template"`
	Level    string  `json:"level"`
	Source   string  `json:"source"`
	Count    int     `json:"count"`
	Share    float64 `json:"share"`
	FirstTs  int64   `json:"firstTs"`
	LastTs   int64   `json:"lastTs"`
	Sample   string  `json:"sample"`
	Pods     int     `json:"pods"`
}

type logTemplatesResult struct {
	Groups   []logTemplateGroup `json:"groups"`
	Lines    int                `json:"lines"`
	Distinct int                `json:"distinct"`
}

// The masks, in order: the longer and more specific shapes first, so an IP's digits are not
// masked one number at a time.
var logTemplateMasks = []struct {
	re   *regexp.Regexp
	with string
	// needs: the characters one of which a match must contain; the regexp is skipped without them.
	needs string
}{
	{regexp.MustCompile(`"[^"]*"`), `"` + logTemplateHole + `"`, `"`},
	{regexp.MustCompile(`'[^']*'`), `'` + logTemplateHole + `'`, `'`},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?`), logTemplateHole, ":"},
	{regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`), logTemplateHole, "-"},
	// A Kubernetes pod name's replica-set and pod suffixes (web-7d9f8c6b5-x2k9q): the name stays.
	{regexp.MustCompile(`\b([a-z0-9]+(?:-[a-z0-9]+)*?)-[a-z0-9]{8,10}-[a-z0-9]{5}\b`), "$1-" + logTemplateHole, "-"},
	{regexp.MustCompile(`\[[0-9a-fA-F:.]*:[0-9a-fA-F:.]*\](?::\d+)?`), logTemplateHole, "["},
	{regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`), logTemplateHole, "."},
	{regexp.MustCompile(`\b(?:[0-9a-fA-F]{1,4}:){2,7}[0-9a-fA-F]{1,4}\b`), logTemplateHole, ":"},
	// A number, or a duration ("12.3ms", "2h1m3s"): one pass for both.
	{regexp.MustCompile(`\b(?:\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h|d)?)+\b`), logTemplateHole, "0123456789"},
}

// logTemplateHex is a hex run of 6 or more: an id only when it has a digit ("facade" is a word).
var logTemplateHex = regexp.MustCompile(`\b(?:0x)?[0-9a-fA-F]{6,}\b`)

// LogTemplates groups linesJSON into templates: the lines as a pod's tail (["line", …]) or as
// KubeWorkloadLogs returns them ([{"pod","time" (unix ms),"text","level"}]). Each line is parsed as the log
// views parse it, and its message's variable tokens (numbers, durations, hex ids, UUIDs, IPv4 and
// IPv6 addresses with their port, timestamps, quoted strings, a pod name's generated suffix)
// become <*>; field keys stay, their values are masked. Lines with the same level, source and
// masked message are one group. Pure: no cluster call.
//
// Returns {"groups":[{template, level, source, count, share, firstTs, lastTs, sample, pods}],
// "lines", "distinct"}: the limit (0 → 20) largest groups, most lines first. sample is the newest
// message of the group as written, for the log filter; pods counts a workload's pods in it (0
// for a single pod's tail). At most the newest 4 000 lines are read; a template is cut at 200
// runes, and one that would be nothing but placeholders keeps its message as written.
func LogTemplates(linesJSON string, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	lines, err := decodeTemplateLines(linesJSON)
	if err != nil {
		return "", err
	}

	if limit <= 0 {
		limit = logTemplatesDefault
	}

	return toJSON(logTemplates(lines, limit, time.Now()))
}

// templateLine is one line to group: its text, and for a workload its pod, time (unix ms) and level.
type templateLine struct {
	text, pod, level string
	time             int64
}

func decodeTemplateLines(linesJSON string) ([]templateLine, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(linesJSON), &raw); err != nil {
		return nil, fmt.Errorf("log lines: %w", err)
	}

	out := make([]templateLine, 0, len(raw))

	for _, r := range raw {
		var l templateLine

		switch {
		case len(r) > 0 && r[0] == '"':
			if err := json.Unmarshal(r, &l.text); err != nil {
				return nil, fmt.Errorf("log line: %w", err)
			}
		case len(r) > 0 && r[0] == '{':
			var w workloadLogLine
			if err := json.Unmarshal(r, &w); err != nil {
				return nil, fmt.Errorf("log line: %w", err)
			}

			l = templateLine{text: w.Text, pod: w.Pod, time: w.Time, level: w.Level}
		default:
			return nil, errors.New("log lines: each one a string or a {pod, time, text, level} object")
		}

		// The line was masked for display: map it back so the sample filters what is on screen.
		l.text = privacy.unmaskText(l.text)
		out = append(out, l)
	}

	return out, nil
}

type templateAcc struct {
	group  logTemplateGroup
	pods   map[string]bool
	order  int
	newest int64
}

func logTemplates(lines []templateLine, limit int, now time.Time) logTemplatesResult {
	if len(lines) > logTemplatesMaxLines {
		lines = lines[len(lines)-logTemplatesMaxLines:]
	}

	groups := map[string]*templateAcc{}
	// A tail repeats messages word for word: each is masked once.
	masked := map[string]string{}

	for i, l := range lines {
		e := parseLogLine(l.text, now)

		level := cmp.Or(e.Level, strings.ToLower(l.level))
		msg := strings.TrimSpace(cmp.Or(e.Msg, e.Raw))
		ts := cmp.Or(e.TS, l.time)

		m, ok := masked[msg]
		if !ok {
			m = maskLogMessage(msg)
			masked[msg] = m
		}

		template := logTemplateOf(msg, m, e.Fields)
		key := level + "|" + e.Source + "|" + template

		acc := groups[key]
		if acc == nil {
			acc = &templateAcc{group: logTemplateGroup{Template: template, Level: level, Source: e.Source, FirstTs: ts}, pods: map[string]bool{}, order: i}
			groups[key] = acc
		}

		acc.group.Count++
		if l.pod != "" {
			acc.pods[l.pod] = true
		}

		if ts != 0 && (acc.group.FirstTs == 0 || ts < acc.group.FirstTs) {
			acc.group.FirstTs = ts
		}

		acc.group.LastTs = max(acc.group.LastTs, ts)

		// The newest message: the latest time, else the latest line.
		if ts >= acc.newest {
			acc.newest, acc.group.Sample = ts, msg
		}
	}

	res := logTemplatesResult{Groups: []logTemplateGroup{}, Lines: len(lines), Distinct: len(groups)}

	accs := make([]*templateAcc, 0, len(groups))
	for _, a := range groups {
		accs = append(accs, a)
	}

	slices.SortFunc(accs, func(a, b *templateAcc) int {
		return cmp.Or(cmp.Compare(b.group.Count, a.group.Count), cmp.Compare(a.order, b.order))
	})

	for _, a := range accs[:min(limit, len(accs))] {
		g := a.group
		g.Share = roundTo(float64(g.Count)/float64(len(lines)), 1000)
		// 0 for a single pod's tail, whose lines name no pod.
		g.Pods = len(a.pods)
		res.Groups = append(res.Groups, g)
	}

	return res
}

// logTemplateOf is msg masked (masked), then each field as key=<*>, cut at logTemplatesMaxRunes;
// msg as written when masking leaves nothing but placeholders.
func logTemplateOf(msg, masked string, fields []logField) string {
	if !hasLogWord(masked) {
		masked = msg
	}

	var b strings.Builder

	b.WriteString(masked)

	for _, f := range fields {
		b.WriteString(" " + f.K + "=" + logTemplateHole)
	}

	t := strings.TrimSpace(b.String())
	if r := []rune(t); len(r) > logTemplatesMaxRunes {
		t = string(r[:logTemplatesMaxRunes])
	}

	return t
}

// logTemplateHexAfter is how many masks run before the hex ids: the quoted strings, timestamps,
// UUIDs and pod suffixes, whose hex parts are not ids of their own.
const logTemplateHexAfter = 5

// maskLogMessage replaces the variable tokens of msg with <*>.
func maskLogMessage(msg string) string {
	s := msg
	digits := strings.ContainsAny(s, "0123456789")

	for i, m := range logTemplateMasks {
		if i == logTemplateHexAfter && digits {
			s = logTemplateHex.ReplaceAllStringFunc(s, func(h string) string {
				if strings.IndexFunc(strings.TrimPrefix(h, "0x"), unicode.IsDigit) < 0 {
					return h
				}

				return logTemplateHole
			})
		}

		// A mask of digits (all but the quoted strings) cannot match a message without any.
		if (i > 1 && !digits) || !strings.ContainsAny(s, m.needs) {
			continue
		}

		s = m.re.ReplaceAllString(s, m.with)
	}

	return s
}

// hasLogWord is whether s keeps a letter once its placeholders are left out.
func hasLogWord(s string) bool {
	return strings.IndexFunc(strings.ReplaceAll(s, logTemplateHole, ""), unicode.IsLetter) >= 0
}
