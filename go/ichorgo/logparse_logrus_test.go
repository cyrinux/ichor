package ichorgo

import (
	"strings"
	"testing"
	"time"
)

// logrus TextFormatter and zerolog ConsoleWriter lines, as they arrive once their colours are
// stripped: a level, the message and key=value fields.
func TestParseLogrusAndZerolog(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		want       logEntry
	}{
		{
			name: "logrus info, padded message",
			line: "INFO[0042] request done    client_ip=10.0.0.7 status=200",
			want: logEntry{Level: "info", Msg: "request done", Fields: kv("client_ip", "10.0.0.7", "status", "200")},
		},
		{
			name: "logrus error, one quoted field",
			line: `ERRO[0043] upstream timeout error="dial tcp: i/o timeout"`,
			want: logEntry{Level: "error", Msg: "upstream timeout", Fields: kv("error", "dial tcp: i/o timeout")},
		},
		{
			name: "logrus coloured sample",
			line: ansiSampleLines[1],
			want: logEntry{Level: "error", Msg: "upstream timeout", Fields: kv("error", "dial tcp: i/o timeout")},
		},
		{
			name: "logrus untruncated level, no fields",
			line: "WARNING[0007] disk almost full",
			want: logEntry{Level: "warn", Msg: "disk almost full", Fields: kv()},
		},
		{
			name: "logrus debug and trace",
			line: "DEBU[0001] cache miss key=pods",
			want: logEntry{Level: "debug", Msg: "cache miss", Fields: kv("key", "pods")},
		},
		{
			name: "logrus fatal",
			line: "FATA[0002] cannot start",
			want: logEntry{Level: "error", Msg: "cannot start", Fields: kv()},
		},
		{
			name: "logrus full timestamp",
			line: "INFO[2026-10-01T16:00:00Z] ready port=8080",
			want: logEntry{TS: ms("2026-10-01T16:00:00Z"), Level: "info", Msg: "ready", Fields: kv("port", "8080")},
		},
		{
			// An "=" in the message that is not a field: the fields start at the first tail
			// that is all key=value.
			name: "logrus message with an equals sign",
			line: "INFO[0042] set x=1 for the run attempt=2",
			want: logEntry{Level: "info", Msg: "set x=1 for the run", Fields: kv("attempt", "2")},
		},
		{
			name: "logrus empty message",
			line: "INFO[0000]",
			want: logEntry{Level: "info", Fields: kv()},
		},
		{
			name: "zerolog kitchen time",
			line: "3:04PM INF request done client_ip=10.0.0.7 status=200",
			want: logEntry{TS: ms("2026-10-01T15:04:00Z"), Level: "info", Msg: "request done", Fields: kv("client_ip", "10.0.0.7", "status", "200")},
		},
		{
			name: "zerolog error with caller",
			line: `11:58AM ERR main.go:42 > upstream timeout error="dial tcp: i/o timeout"`,
			want: logEntry{TS: ms("2026-10-01T11:58:00Z"), Level: "error", Source: "main.go:42", Msg: "upstream timeout", Fields: kv("error", "dial tcp: i/o timeout")},
		},
		{
			name: "zerolog rfc 3339",
			line: "2026-10-01T16:59:51Z WRN queue depth high depth=850",
			want: logEntry{TS: ms("2026-10-01T16:59:51Z"), Level: "warn", Msg: "queue depth high", Fields: kv("depth", "850")},
		},
		{
			name: "zerolog trace, no message",
			line: "9:00AM TRC",
			want: logEntry{TS: ms("2026-10-01T09:00:00Z"), Level: "debug", Fields: kv()},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogLine(tc.line, logNow)
			got.Raw, got.Spans = "", nil

			if !equalJSON(t, got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// Lines that only look like logrus or zerolog stay plain: never a wrong level.
func TestLogrusAndZerologLookAlikesStayPlain(t *testing.T) {
	for _, line := range []string{
		"INFO: something happened",
		"ERROR[x] not a counter",
		"ERROR[12 unclosed",
		"INFO[0042]glued",
		"NOTE[0042] not a logrus level",
		"Info[0042] wrong case",
		"3:04PM INFO not a zerolog level",
		"3:04PM inf lower case",
		"13:04PM INF hour out of range",
		"3:04 INF no meridiem",
		"2026-10-01 INF date only",
		"12 INF numbers",
	} {
		if e := parseLogLine(line, logNow); e.Level != "" || e.Msg != line || len(e.Fields) != 0 {
			t.Errorf("%q parsed as %+v", line, e)
		}
	}
}

// No prefix of a logrus or zerolog line panics or reads out of range.
func TestLogrusAndZerologPrefixes(t *testing.T) {
	for _, line := range []string{
		`ERRO[0043] upstream timeout error="dial tcp: i/o timeout"`,
		"INFO[2026-10-01T16:00:00Z] ready port=8080",
		`11:58AM ERR main.go:42 > upstream timeout error="dial tcp: i/o timeout"`,
		"2026-10-01T16:59:51Z WRN queue depth high depth=850",
	} {
		for i := range len(line) + 1 {
			parseLogLine(line[:i], logNow)
		}
	}
}

// The demo pod sheet: its coloured logrus lines get their level, so the level chips and the
// Errors and Warnings filters find them.
func TestDemoPodLogLogrusLines(t *testing.T) {
	tail := parseLogText(demoPodLog("web-0", false), logNow)
	levels := map[string]string{}

	for i, l := range tail.Lines {
		if e := tail.Entries[i]; strings.Contains(l, "[004") {
			levels[e.Msg] = e.Level
		}
	}

	if levels["request done"] != "info" || levels["slow query"] != "warn" {
		t.Errorf("demo logrus lines: %v", levels)
	}
}

// A time of day takes today's date in UTC, as klog's does.
func TestZerologTimeOfDayIsToday(t *testing.T) {
	now := time.Date(2026, 3, 9, 23, 30, 0, 0, time.UTC)
	if e := parseLogLine("12:15AM INF tick", now); e.TS != ms("2026-03-09T00:15:00Z") {
		t.Errorf("ts %d", e.TS)
	}
}
