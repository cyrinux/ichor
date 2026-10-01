package talosmobile

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Log lines are parsed into entries so the apps can color them by level and show the
// fields apart. The parser never fails: a line it does not recognize becomes an entry with
// no level whose message is the whole line.

type logField struct {
	K string `json:"k"`
	V string `json:"v"`
}

type logEntry struct {
	TS     int64      `json:"ts"`    // unix ms, 0 when unknown
	Level  string     `json:"level"` // error, warn, info, debug or ""
	Source string     `json:"source"`
	Msg    string     `json:"msg"`
	Fields []logField `json:"fields"`
	Raw    string     `json:"raw"`
}

// ParseLogLine parses one log line (as received by LogListener.OnLine) into a JSON entry
// {"ts","level","source","msg","fields":[{"k","v"}],"raw"}.
func ParseLogLine(line string) (out string) {
	var err error

	defer maskResult(&out, &err)

	// The line was masked for display: map it back so masking the entry reuses its fakes.
	line = privacy.unmaskText(line)

	out, err = toJSON(parseLogLine(line, time.Now()))

	return out
}

func parseLogLines(lines []string, now time.Time) []logEntry {
	entries := make([]logEntry, len(lines))
	for i, l := range lines {
		entries[i] = parseLogLine(l, now)
	}

	return entries
}

// parseLogLine picks the format from the first bytes; each parser rejects lines that do
// not fully match it, falling through to the next one.
func parseLogLine(line string, now time.Time) logEntry {
	s := strings.TrimRight(line, " \t\r\n")

	var (
		e  logEntry
		ok bool
	)

	switch c := firstByte(s); {
	case c == '{':
		e, ok = parseJSONLog(s)
	case strings.IndexByte("IWEF", c) >= 0:
		e, ok = parseKlogText(s, now)
	case isDigit(c):
		e, ok = parseTalosService(s)
	case c >= 'a' && c <= 'z':
		e, ok = parseKernel(s)
	}

	if !ok && strings.IndexByte(s, '=') > 0 {
		e, ok = parseLogfmt(s)
	}

	if !ok {
		e = logEntry{Msg: s}
	}

	if e.Fields == nil {
		e.Fields = []logField{}
	}

	e.Raw = line

	return e
}

func firstByte(s string) byte {
	if s == "" {
		return 0
	}

	return s[0]
}

// --- structured (JSON, logfmt) ---

func parseJSONLog(s string) (logEntry, bool) {
	pairs, ok := jsonPairs(s)
	if !ok {
		return logEntry{}, false
	}

	return entryFromPairs(pairs, s), true
}

func parseLogfmt(s string) (logEntry, bool) {
	pairs, ok := logfmtPairs(s)
	if !ok || !slices.ContainsFunc(pairs, func(p logPair) bool {
		return keyRole(p.k) == roleLevel || keyRole(p.k) == roleMsg
	}) {
		return logEntry{}, false
	}

	return entryFromPairs(pairs, s), true
}

const (
	roleField = iota
	roleLevel
	roleTime
	roleMsg
	roleCaller
	roleLogger
	roleVerbosity
	roleError
)

func keyRole(k string) int {
	switch k {
	case "level", "lvl", "severity", "loglevel":
		return roleLevel
	case "ts", "time", "timestamp", "@timestamp":
		return roleTime
	case "msg", "message":
		return roleMsg
	case "caller":
		return roleCaller
	case "logger":
		return roleLogger
	case "v":
		return roleVerbosity
	case "err", "error":
		return roleError
	default:
		return roleField
	}
}

// entryFromPairs maps the well-known keys of zap, klog, logrus and slog: the first usable
// level, time and message, the caller (else logger) as source; the rest stay fields in
// order. Without a level, an error value means error and klog's verbosity info or debug;
// a klog line (numeric ts, caller) with neither is an error too.
func entryFromPairs(pairs []logPair, s string) logEntry {
	e := logEntry{Msg: s}
	used := make([]bool, len(pairs))
	levelAt, msgAt, sourceAt, sourceRole, verbosity := -1, -1, -1, 0, -1
	hasErr, numericTS := false, false

	for i, p := range pairs {
		switch r := keyRole(p.k); r {
		case roleLevel:
			if levelAt < 0 {
				if lvl := pairLevel(p); lvl != "" {
					e.Level, levelAt, used[i] = lvl, i, true
				}
			}
		case roleTime:
			if e.TS == 0 {
				if ts := pairMillis(p); ts != 0 {
					e.TS, used[i], numericTS = ts, true, p.num
				}
			}
		case roleMsg:
			if msgAt < 0 {
				e.Msg, msgAt, used[i] = p.v, i, true
			}
		case roleCaller, roleLogger:
			if sourceAt < 0 || r < sourceRole {
				sourceAt, sourceRole = i, r
			}
		case roleVerbosity:
			if n, err := strconv.Atoi(p.v); err == nil && p.num {
				verbosity, used[i] = n, true
			}
		case roleError:
			hasErr = hasErr || (p.v != "" && p.v != "null")
		}
	}

	if sourceAt >= 0 {
		e.Source, used[sourceAt] = shortCaller(pairs[sourceAt].v), true
	}

	if levelAt < 0 {
		switch {
		case hasErr:
			e.Level = "error"
		case verbosity > 0:
			e.Level = "debug"
		case verbosity == 0:
			e.Level = "info"
		case numericTS && sourceRole == roleCaller:
			// klog's JSON logger gives every info line a "v"; ErrorS without an error and
			// Errorf lines have neither "v" nor "err".
			e.Level = "error"
		}
	}

	e.Fields = make([]logField, 0, len(pairs))

	for i, p := range pairs {
		if !used[i] {
			e.Fields = append(e.Fields, logField{K: p.k, V: p.v})
		}
	}

	return e
}

func pairsToFields(pairs []logPair) []logField {
	fields := make([]logField, len(pairs))
	for i, p := range pairs {
		fields[i] = logField{K: p.k, V: p.v}
	}

	return fields
}

// shortCaller keeps the file:line of a caller path ("prober/prober.go:272" -> "prober.go:272").
func shortCaller(c string) string {
	if i := strings.LastIndexByte(c, '/'); i >= 0 && i < len(c)-1 {
		return c[i+1:]
	}

	return c
}

func pairLevel(p logPair) string {
	if !p.num {
		return levelName(p.v)
	}

	// pino/bunyan numeric levels.
	n, err := strconv.Atoi(p.v)

	switch {
	case err != nil || n < 10:
		return ""
	case n >= 50:
		return "error"
	case n >= 40:
		return "warn"
	case n >= 30:
		return "info"
	default:
		return "debug"
	}
}

// levelName maps level names (any case) of syslog, zap, logrus, klog and slog.
func levelName(name string) string {
	switch strings.ToLower(name) {
	case "emerg", "emergency", "alert", "crit", "critical", "err", "error", "fatal", "panic",
		"dpanic", "e", "f", "severe":
		return "error"
	case "warn", "warning", "w":
		return "warn"
	case "info", "notice", "information", "informational", "i":
		return "info"
	case "debug", "trace", "d", "t", "verbose":
		return "debug"
	default:
		return ""
	}
}

// pairMillis reads a timestamp: RFC 3339 text, or a number whose magnitude gives its unit
// (seconds as zap's float, milliseconds as klog's, micro- or nanoseconds).
func pairMillis(p logPair) int64 {
	if t, err := time.Parse(time.RFC3339Nano, p.v); err == nil {
		return t.UnixMilli()
	}

	f, err := strconv.ParseFloat(p.v, 64)
	if err != nil || f <= 0 {
		return 0
	}

	switch {
	case f >= 1e17:
		return int64(f / 1e6)
	case f >= 1e14:
		return int64(f / 1e3)
	case f >= 1e11:
		return int64(f)
	default:
		return int64(f * 1e3)
	}
}

// --- privacy ---

// unmaskText maps the fake addresses and hostnames of a masked text back to the real ones,
// so that masking it again yields the same fakes instead of minting new ones. Only for text
// the app got from a masked result, unmasked once on entry like the other arguments.
func (m *privacyMask) unmaskText(s string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || s == "" {
		return s
	}

	terms := make([]maskTerm, 0, len(m.hostsBack))
	for fake, real := range m.hostsBack {
		terms = append(terms, maskTerm{match: fake, repl: real})
	}

	slices.SortFunc(terms, func(a, b maskTerm) int {
		return cmp.Or(cmp.Compare(len(b.match), len(a.match)), cmp.Compare(a.match, b.match))
	})

	back := func(fake string) (string, bool) {
		real, ok := m.ipsBack[fake]

		return real, ok
	}

	return replaceIPv6(replaceIPv4(replaceTerms(s, terms), back), back)
}
