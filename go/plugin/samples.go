package plugin

import (
	"regexp"
	"strconv"
	"time"
)

// A sample's time may be written relative to the render's clock, so that the demo stays as
// true tomorrow as today: "now", "now-6h", "now+2d" (s, m, h, d) become RFC 3339 times.
var relativeSampleTime = regexp.MustCompile(`^now(?:([+-])(\d{1,6})([smhd]))?$`)

var sampleUnits = map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}

// sampleTimes copies the samples with their relative times resolved; the compiled plugin's
// samples stay as written.
func sampleTimes(list []map[string]any, now time.Time) []map[string]any {
	out := make([]map[string]any, len(list))
	for i, obj := range list {
		out[i], _ = resolveSampleTimes(obj, now).(map[string]any)
	}

	return out
}

func resolveSampleTimes(v any, now time.Time) any {
	switch x := v.(type) {
	case map[string]any:
		if x == nil {
			return x
		}

		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = resolveSampleTimes(e, now)
		}

		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = resolveSampleTimes(e, now)
		}

		return out
	case string:
		m := relativeSampleTime.FindStringSubmatch(x)
		if m == nil {
			return x
		}

		at := now
		if m[1] != "" {
			n, _ := strconv.Atoi(m[2])

			d := time.Duration(n) * sampleUnits[m[3]]
			if m[1] == "-" {
				d = -d
			}

			at = now.Add(d)
		}

		return at.UTC().Format(time.RFC3339)
	default:
		return v
	}
}
