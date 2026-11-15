package ichorgo

import (
	"strings"
	"time"
)

// --- logrus TextFormatter: "INFO[0042] request done    client_ip=10.0.0.7 status=200" ---

// logrusLevels are TextFormatter's level words: truncated to four letters by default, whole
// with DisableLevelTruncation.
var logrusLevels = map[string]string{
	"INFO": "info", "WARN": "warn", "WARNING": "warn",
	"ERRO": "error", "ERROR": "error", "FATA": "error", "FATAL": "error", "PANI": "error", "PANIC": "error",
	"DEBU": "debug", "DEBUG": "debug", "TRAC": "debug", "TRACE": "debug",
}

// parseLogrusText reads logrus' coloured text line: a level word, then in brackets the seconds
// since the process started (no wall time: TS stays 0) or, with FullTimestamp, an RFC 3339
// time; then the message, padded, and its key=value fields.
func parseLogrusText(s string) (logEntry, bool) {
	open := strings.IndexByte(s, '[')
	if open < 4 {
		return logEntry{}, false
	}

	level, ok := logrusLevels[s[:open]]
	if !ok {
		return logEntry{}, false
	}

	stamp, rest, ok := strings.Cut(s[open+1:], "]")
	if !ok || stamp == "" || (rest != "" && rest[0] != ' ') {
		return logEntry{}, false
	}

	var ts int64

	if isDigits(stamp) {
		// Seconds since start: not a time the app can place.
	} else if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		ts = t.UnixMilli()
	} else {
		return logEntry{}, false
	}

	msg, fields := splitMessageFields(rest)

	return logEntry{TS: ts, Level: level, Msg: msg, Fields: fields}, true
}

// --- zerolog ConsoleWriter: "3:04PM INF main.go:42 > request done client_ip=10.0.0.7" ---

var zerologLevels = map[string]string{
	"TRC": "debug", "DBG": "debug", "INF": "info", "WRN": "warn", "ERR": "error", "FTL": "error", "PNC": "error",
}

// parseZerologConsole reads zerolog's pretty console line: its time (time.Kitchen by default,
// or RFC 3339), a three-letter level, an optional "file:line >" caller, then the message and
// its fields. A time of day takes the date of now, in UTC.
func parseZerologConsole(s string, now time.Time) (logEntry, bool) {
	stamp, rest, _ := strings.Cut(s, " ")

	ts, ok := zerologTime(stamp, now)
	if !ok {
		return logEntry{}, false
	}

	lvl, rest, _ := strings.Cut(rest, " ")

	level, ok := zerologLevels[lvl]
	if !ok {
		return logEntry{}, false
	}

	var source string

	if caller, after, found := strings.Cut(rest, " > "); found && isCaller(caller) {
		source, rest = caller, after
	}

	msg, fields := splitMessageFields(rest)

	return logEntry{TS: ts, Level: level, Source: source, Msg: msg, Fields: fields}, true
}

func zerologTime(stamp string, now time.Time) (int64, bool) {
	if t, err := time.Parse(time.Kitchen, stamp); err == nil {
		day := now.UTC()

		return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC).UnixMilli(), true
	}

	if len(stamp) < len("2006-01-02T15:04:05Z") || stamp[10] != 'T' {
		return 0, false
	}

	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return 0, false
	}

	return t.UnixMilli(), true
}

// isCaller is a "file.go:42" source: a name, a colon and a line number.
func isCaller(s string) bool {
	name, line, ok := strings.Cut(s, ":")

	return ok && name != "" && !strings.ContainsAny(name, " =") && isDigits(line)
}

// splitMessageFields splits "message   key=value key=\"quoted\"" at the first token from which
// the rest is all key=value; an "=" inside the message before it stays text.
func splitMessageFields(s string) (string, []logField) {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' || i+1 == len(s) || s[i+1] == ' ' || !isFieldStart(s[i+1:]) {
			continue
		}

		if pairs, ok := logfmtTail(s[i+1:]); ok {
			return strings.TrimSpace(s[:i]), pairsToFields(pairs)
		}
	}

	if isFieldStart(strings.TrimLeft(s, " ")) {
		if pairs, ok := logfmtTail(s); ok {
			return "", pairsToFields(pairs)
		}
	}

	return strings.TrimSpace(s), []logField{}
}

// isFieldStart is a token beginning "key=" with a key that starts with a letter or "_".
func isFieldStart(s string) bool {
	if s == "" || !(isLetter(s[0]) || s[0] == '_') {
		return false
	}

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '=':
			return true
		case !isLogfmtKeyByte(c):
			return false
		}
	}

	return false
}

func isLetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }
