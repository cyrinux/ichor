package ichorgo

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// The startup trace says where a cold start's time goes inside the Go core: how long the
// config parse took, whether a session was opened or reused, when the first node of the
// overview answered and when the last did. It stays on the device (each step lands in the
// diagnostics ring, see diag.go; the apps forward it to logcat or the unified log) and carries step names, counts and durations only: never a
// context name, an endpoint or an address, which the privacy mask would not catch there.

// TraceListener receives the startup trace steps as JSON
// {"step":"…","ms":n,"at":unixMs[,"count":n][,"unreachable":n]} (implemented in Kotlin/Swift).
// It is called on Go threads.
type TraceListener interface {
	OnTrace(json string)
}

// traceSink holds the registered listener; nil (the default) makes trace a no-op.
var traceSink atomic.Pointer[traceHolder]

type traceHolder struct{ listener TraceListener }

// SetTraceListener registers the app's listener, or removes it with nil. The apps set it only
// while the "startup timing" setting is on.
func SetTraceListener(l TraceListener) {
	if l == nil {
		traceSink.Store(nil)

		return
	}

	traceSink.Store(&traceHolder{listener: l})
}

type traceStep struct {
	Step        string `json:"step"`
	Ms          int64  `json:"ms"`
	At          int64  `json:"at"`
	Count       int    `json:"count,omitempty"`
	Unreachable int    `json:"unreachable,omitempty"`
}

// traceEnabled says whether a listener is registered, for a count that costs work to compute.
func traceEnabled() bool { return traceSink.Load() != nil }

// tracedFanout is forEachNode for the overview: probe returns whether the node answered, and
// the trace gets the first answer ("overview.first_node") and the last one
// ("overview.all_nodes", with the node and unreachable counts), both measured from start.
func tracedFanout(start time.Time, nodes []string, probe func(i int, node string) bool) {
	var first sync.Once

	var unreachable atomic.Int32

	forEachNode(nodes, func(i int, node string) {
		if probe(i, node) {
			first.Do(func() { trace("overview.first_node", start) })
		} else {
			unreachable.Add(1)
		}
	})

	traceCounts("overview.all_nodes", start, len(nodes), int(unreachable.Load()))
}

// trace reports step, which took the time since since, to the diagnostics ring and to the
// listener, if any.
func trace(step string, since time.Time) {
	traceCounts(step, since, 0, 0)
}

// traceCounts is trace with a count (contexts, nodes) and how many of them did not answer.
func traceCounts(step string, since time.Time, count, unreachable int) {
	now := time.Now()

	diag(slog.LevelInfo, "trace", "startup step", nil, "step", step, "ms", now.Sub(since).Milliseconds(),
		"count", count, "unreachable", unreachable)

	h := traceSink.Load()
	if h == nil {
		return
	}

	b, err := json.Marshal(traceStep{
		Step: step, Ms: now.Sub(since).Milliseconds(), At: now.UnixMilli(),
		Count: count, Unreachable: unreachable,
	})
	if err != nil {
		return
	}

	// A listener that panics must not take the call it traces down with it.
	defer func() { _ = recover() }()

	h.listener.OnTrace(string(b))
}
