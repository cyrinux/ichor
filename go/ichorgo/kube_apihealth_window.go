package ichorgo

import "time"

// promWindow is two scrapes of the same API server's /metrics, taken seconds apart: the
// counters' growth between them is what the server is doing now. When the scrapes come
// from different servers (a load balancer in front of the control plane), seconds is 0 and
// the rates are averages since the second server started.
type promWindow struct {
	before, after promSamples
	seconds       float64
	uptime        float64 // seconds the second server has been running, 0 unknown
}

const metricProcessStart = "process_start_time_seconds"

func newPromWindow(before, after promSamples, elapsed time.Duration, now time.Time) promWindow {
	w := promWindow{before: before, after: after, seconds: elapsed.Seconds()}

	start := after.total(metricProcessStart, nil)
	if start > 0 {
		w.uptime = float64(now.Unix()) - start
	}

	if !before.has(metricProcessStart) || before.total(metricProcessStart, nil) != start {
		w.seconds = 0
	}

	return w
}

// deltaBy is how much a counter grew over the window, grouped by keys. A counter that went
// down (a restart) counts as not having grown.
func (w promWindow) deltaBy(name string, match func(map[string]string) bool, keys ...string) map[string]float64 {
	after := w.after.sumBy(name, match, keys...)
	if w.seconds == 0 {
		return after
	}

	before := w.before.sumBy(name, match, keys...)
	out := make(map[string]float64, len(after))

	for k, v := range after {
		out[k] = max(0, v-before[k])
	}

	return out
}

// span is what the deltas are divided by to make rates: the window, or the uptime.
func (w promWindow) span() float64 {
	if w.seconds > 0 {
		return w.seconds
	}

	return w.uptime
}

// rateBy is a counter's per-second rate, grouped by keys.
func (w promWindow) rateBy(name string, match func(map[string]string) bool, keys ...string) map[string]float64 {
	deltas := w.deltaBy(name, match, keys...)
	span := w.span()

	for k, v := range deltas {
		if span > 0 {
			deltas[k] = v / span
		} else {
			deltas[k] = 0
		}
	}

	return deltas
}

// rate is a counter's per-second rate over all its samples match accepts.
func (w promWindow) rate(name string, match func(map[string]string) bool) float64 {
	return w.rateBy(name, match)[""]
}

// meanMsBy is a histogram's mean over the window in milliseconds, grouped by keys; keys
// without observations are left out.
func (w promWindow) meanMsBy(histogram string, match func(map[string]string) bool, keys ...string) map[string]float64 {
	sums := w.deltaBy(histogram+"_sum", match, keys...)
	counts := w.deltaBy(histogram+"_count", match, keys...)
	out := make(map[string]float64, len(counts))

	for k, n := range counts {
		if n > 0 {
			out[k] = sums[k] / n * 1000
		}
	}

	return out
}
