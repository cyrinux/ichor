package ichorgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// The diagnostics ring is the one place the core's own events land: failed exported calls,
// recovered panics, the startup trace, anything logged through log/slog, and what the apps
// add with DiagLog. It stays on the phone, in memory (and, opt-in, in the app's private
// data directory, see diag_persist.go) until the user exports it as a zip and shares it
// themself (diag_export.go). Nothing is ever sent by the app.
//
// What may be logged: a constant message, and attributes that are numbers, bools, durations
// (ms), counts, HTTP status codes, kinds and verbs. NEVER a context name, a node, a
// namespace, an object name or a URL: the masking below would not reliably catch them in a
// log. Every entry is still redacted on write, whatever the screenshot-mode setting:
// secrets go through redactSecrets, then names and addresses through diagMask, and the
// message and error lose their quoted values and URLs (Kubernetes and Go errors quote the
// names they are about).

const (
	diagMaxEntries = 2000
	diagMaxBytes   = 512 << 10

	diagMaxMsg   = 512
	diagMaxErr   = 2048
	diagMaxValue = 256
	diagMaxAttrs = 32
)

var (
	diagLevels     = []string{"debug", "info", "warn", "error"}
	diagComponents = []string{"kube", "talos", "gitops", "net", "store", "trace", "api", "android", "ios"}
)

// diagEntry is one line of the ring. At is unix ms.
type diagEntry struct {
	At        int64          `json:"at"`
	Level     string         `json:"level"`
	Component string         `json:"component"`
	Msg       string         `json:"msg"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Err       string         `json:"err,omitempty"`
}

// diagRing holds the encoded entries, oldest first, within diagMaxEntries and diagMaxBytes.
type diagRing struct {
	mu    sync.Mutex
	lines [][]byte
	bytes int
	persistState
}

var (
	diagLog   = &diagRing{}
	diagClock = time.Now
)

// diagMask is a privacy mask that is always on, for the ring alone: it learns the contexts,
// endpoints and hostnames the core sees (the screenshot mask only learns while it is on),
// and its fakes are never mapped back.
var diagMask = newAlwaysOnMask()

func newAlwaysOnMask() *privacyMask {
	m := &privacyMask{}
	m.set(true, nil)

	return m
}

// diagRedact is a string attribute or a document value as the ring keeps it.
func diagRedact(s string) string {
	return diagMask.maskPlain(redactSecrets(s))
}

var (
	diagQuoted = regexp.MustCompile(`"[^"\n]{1,512}"`)
	diagURL    = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s"]+`)
)

// diagScrub is a message or an error as the ring keeps it: redacted, without URLs or
// quoted values.
func diagScrub(s string, limit int) string {
	s = diagURL.ReplaceAllString(diagRedact(s), "<url>")

	return diagTruncate(diagQuoted.ReplaceAllString(s, `"<redacted>"`), limit)
}

func diagTruncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	return s[:limit] + "…"
}

// add records an entry, redacted, and persists it when persistence is on.
func (r *diagRing) add(e diagEntry) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.pushLocked(line)
	r.persistLocked(line)
}

// pushLocked appends line and drops the oldest entries past the bounds.
func (r *diagRing) pushLocked(line []byte) {
	r.lines = append(r.lines, line)
	r.bytes += len(line) + 1

	drop := 0
	for len(r.lines)-drop > diagMaxEntries || (r.bytes > diagMaxBytes && len(r.lines)-drop > 1) {
		r.bytes -= len(r.lines[drop]) + 1
		drop++
	}

	if drop > 0 {
		r.lines = slices.Clone(r.lines[drop:])
	}
}

// snapshot is the ring, oldest first.
func (r *diagRing) snapshot() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.lines)
}

func (r *diagRing) clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines, r.bytes = nil, 0
	r.clearFilesLocked()
}

// diagCallFailed records the failure of the exported function maskErr runs for. It must not
// go through maskErr itself (it would recurse): only unexported helpers here.
func diagCallFailed(err error) {
	if err == nil {
		return
	}

	diag(slog.LevelWarn, "api", "call failed", err, "fn", exportedCaller())
}

const ichorgoPackage = "github.com/cyrinux/ichor/go/ichorgo."

// exportedCaller is the innermost exported function of the package on the stack (the one
// whose deferred maskErr is running), or "" when there is none.
func exportedCaller() string {
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])

	for {
		f, more := frames.Next()

		if rest, ok := strings.CutPrefix(f.Function, ichorgoPackage); ok {
			name, _, _ := strings.Cut(rest, ".")
			if name != "" && name[0] >= 'A' && name[0] <= 'Z' {
				return name
			}
		}

		if !more {
			return ""
		}
	}
}

// DiagLog adds an entry to the diagnostics ring for the apps: level is debug, info, warn or
// error, component one of kube, talos, gitops, net, store, trace, api, android, ios, msg a
// constant text and attrsJSON a flat JSON object of numbers, bools and short words ("" or
// "{}" for none; one that is not is recorded as {"bad_attrs": true}). Never pass a cluster
// name, a node, a namespace, an object name or a URL: they are masked, but a log is not
// where they belong.
func DiagLog(level, component, msg, attrsJSON string) (err error) {
	defer maskErr(&err)

	if !slices.Contains(diagLevels, level) {
		return fmt.Errorf("unknown level %q (%s)", level, strings.Join(diagLevels, ", "))
	}

	if !slices.Contains(diagComponents, component) {
		return fmt.Errorf("unknown component %q (%s)", component, strings.Join(diagComponents, ", "))
	}

	e := diagEntry{
		At: diagClock().UnixMilli(), Level: level, Component: component, Msg: diagScrub(msg, diagMaxMsg),
	}
	e.Attrs = diagAppAttrs(attrsJSON)

	diagLog.add(e)

	return nil
}

var errBadAttrs = errors.New("bad attrs")

// diagAppAttrs is DiagLog's attrsJSON as entry attributes.
func diagAppAttrs(attrsJSON string) map[string]any {
	if strings.TrimSpace(attrsJSON) == "" {
		return nil
	}

	attrs, err := flatAttrs(attrsJSON)
	if err != nil {
		return map[string]any{"bad_attrs": true}
	}

	if len(attrs) == 0 {
		return nil
	}

	return attrs
}

func flatAttrs(attrsJSON string) (map[string]any, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(attrsJSON), &raw); err != nil || len(raw) > diagMaxAttrs {
		return nil, errBadAttrs
	}

	out := make(map[string]any, len(raw))

	for k, v := range raw {
		switch v := v.(type) {
		case float64, bool:
			out[k] = v
		case string:
			out[k] = diagTruncate(diagRedact(v), diagMaxValue)
		default:
			return nil, errBadAttrs
		}
	}

	return out, nil
}

// DiagEntries is the newest limit entries of the diagnostics ring (0 = all), newest first,
// as a JSON array of {at (unix ms), level, component, msg, attrs, err}.
func DiagEntries(limit int) (out string, err error) {
	defer maskResult(&out, &err)

	lines := diagLog.snapshot()
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}

	slices.Reverse(lines)

	return "[" + string(bytes.Join(lines, []byte(","))) + "]", nil
}

// DiagClear forgets every entry of the diagnostics ring, and its files when it is kept.
func DiagClear() (err error) {
	defer maskErr(&err)

	diagLog.clear()

	return nil
}
