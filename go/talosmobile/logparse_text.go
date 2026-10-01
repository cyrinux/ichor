package talosmobile

import (
	"strings"
	"time"
)

// --- kernel ring buffer: "kern:    info: [2026-10-01T16:47:32.186Z]: message" ---

// parseKernel reads talosctl's dmesg format (padded facility and level). Talos' own
// messages, "[talos] message {json}", get source "talos" and their JSON as fields.
func parseKernel(s string) (logEntry, bool) {
	facility, rest, ok := strings.Cut(s, ": ")
	if !ok || !isFacility(facility) {
		return logEntry{}, false
	}

	sev, rest, ok := strings.Cut(strings.TrimLeft(rest, " "), ": ")

	level, known := kernelLevels[sev]
	if !ok || !known || !strings.HasPrefix(rest, "[") {
		return logEntry{}, false
	}

	stamp, msg, ok := strings.Cut(rest[1:], "]: ")
	if !ok {
		stamp, ok = strings.CutSuffix(rest[1:], "]")
		if !ok {
			return logEntry{}, false
		}
	}

	e := logEntry{Level: level, Source: facility, Msg: msg}

	if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		e.TS = t.UnixMilli()
	}

	if talos, ok := strings.CutPrefix(msg, "[talos] "); ok {
		e.Source = "talos"
		e.Msg, e.Fields = splitTrailingJSON(talos)
	}

	return e, true
}

var kernelLevels = map[string]string{
	"emerg": "error", "alert": "error", "crit": "error", "err": "error",
	"warning": "warn", "notice": "info", "info": "info", "debug": "debug",
}

func isFacility(s string) bool {
	if s == "" || len(s) > 10 {
		return false
	}

	for i := range len(s) {
		if c := s[i]; (c < 'a' || c > 'z') && !isDigit(c) {
			return false
		}
	}

	return true
}

// splitTrailingJSON splits "message {json object}" into the message and the object's
// fields; anything else is all message.
func splitTrailingJSON(s string) (string, []logField) {
	if !strings.HasSuffix(s, "}") {
		return s, nil
	}

	for off := 0; ; {
		i := strings.Index(s[off:], " {")
		if i < 0 {
			return s, nil
		}

		at := off + i
		if pairs, ok := jsonPairs(s[at+1:]); ok {
			return s[:at], pairsToFields(pairs)
		}

		off = at + 2
	}
}

// --- klog text: "I1001 16:00:00.123456    1234 file.go:12] message" ---

func parseKlogText(s string, now time.Time) (logEntry, bool) {
	const header = len("I0102 15:04:05.000000")

	if len(s) < header+2 || s[5] != ' ' || s[8] != ':' || s[11] != ':' {
		return logEntry{}, false
	}

	t, err := time.Parse("0102 15:04:05.999999", s[1:header])
	if err != nil {
		return logEntry{}, false
	}

	// klog has no year: take the current one, or last year for a date ahead of now.
	ts := time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
	if ts.After(now.Add(24 * time.Hour)) {
		ts = ts.AddDate(-1, 0, 0)
	}

	head, msg, ok := strings.Cut(s[header:], "] ")
	if !ok {
		head, ok = strings.CutSuffix(s[header:], "]")
		if !ok {
			return logEntry{}, false
		}
	}

	// head is "   <pid> file.go:12".
	fields := strings.Fields(head)
	if len(fields) != 2 {
		return logEntry{}, false
	}

	return logEntry{
		TS:     ts.UnixMilli(),
		Level:  levelName(s[:1]),
		Source: fields[1],
		Msg:    msg,
	}, true
}

// --- Talos services (Go log): "2026/10/01 16:58:33.985707 [file.go:12: ] message" ---

// parseTalosService reads the Go log timestamp and an optional file:line source. Only the
// gRPC access lines ("machined OK [/machine.MachineService/Version] ...") get a level, from
// their status code; other messages are left without one.
func parseTalosService(s string) (logEntry, bool) {
	const date = len("2006/01/02 15:04:05")

	if len(s) < date+1 || s[4] != '/' || s[7] != '/' || s[10] != ' ' || s[13] != ':' {
		return logEntry{}, false
	}

	end := len(s)
	if i := strings.IndexByte(s[date:], ' '); i >= 0 {
		end = date + i
	}

	rest := strings.TrimPrefix(s[end:], " ")

	t, err := time.Parse("2006/01/02 15:04:05.999999999", s[:end])
	if err != nil {
		return logEntry{}, false
	}

	e := logEntry{TS: t.UnixMilli(), Msg: rest}

	// machined echoes its kernel log messages, without their level.
	if talos, ok := strings.CutPrefix(rest, "[talos] "); ok {
		e.Source = "talos"
		e.Msg, e.Fields = splitTrailingJSON(talos)

		return e, true
	}

	first, after, _ := strings.Cut(rest, " ")
	if strings.HasSuffix(first, ":") && strings.Contains(first, ".go:") {
		e.Source, e.Msg = strings.TrimSuffix(first, ":"), after
		first, after, _ = strings.Cut(after, " ")
	}

	if lvl, ok := grpcLevels[first]; ok && strings.HasPrefix(after, "[/") {
		e.Level = lvl
	} else if code, tail, _ := strings.Cut(after, " "); grpcLevels[code] != "" &&
		strings.HasPrefix(tail, "[/") && e.Source == "" && isServiceName(first) {
		e.Level, e.Source, e.Msg = grpcLevels[code], first, after
	}

	return e, true
}

// grpcLevels rates gRPC status codes: client mistakes are warnings, server faults errors.
var grpcLevels = map[string]string{
	"OK": "info", "Canceled": "info",
	"InvalidArgument": "warn", "NotFound": "warn", "AlreadyExists": "warn",
	"PermissionDenied": "warn", "ResourceExhausted": "warn", "FailedPrecondition": "warn",
	"Aborted": "warn", "OutOfRange": "warn", "Unauthenticated": "warn",
	"DeadlineExceeded": "warn", "Unavailable": "warn",
	"Unknown": "error", "Unimplemented": "error", "Internal": "error", "DataLoss": "error",
}

func isServiceName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}

	for i := range len(s) {
		if c := s[i]; !isWordByte(c) && c != '-' && c != '_' {
			return false
		}
	}

	return true
}
