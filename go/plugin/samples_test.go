package plugin

import (
	"reflect"
	"testing"
	"time"
)

func TestSampleTimes(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	samples := []map[string]any{{
		"metadata": map[string]any{"name": "now-ish", "creationTimestamp": "now-2d"},
		"status": map[string]any{
			"lastScheduleTime": "now-6h", "next": "now+30m", "at": "now", "seconds": "now-45s",
			"conditions": []any{map[string]any{"lastTransitionTime": "now-1h", "reason": "now"}},
			"kept":       []any{"nowhere", "now-", "now-5w", "now - 1h", "2026-10-15T06:00:00Z", int64(3), nil},
			// Past a century: kept as written, never an overflowed Duration.
			"far": []any{"now+999999d", "now-999999d", "now+36501d", "now+999999h", "now+999999s"},
		},
	}}

	got := sampleTimes(samples, now)
	want := []map[string]any{{
		"metadata": map[string]any{"name": "now-ish", "creationTimestamp": "2026-10-14T10:00:00Z"},
		"status": map[string]any{
			"lastScheduleTime": "2026-10-16T04:00:00Z", "next": "2026-10-16T10:30:00Z", "at": "2026-10-16T10:00:00Z", "seconds": "2026-10-16T09:59:15Z",
			"conditions": []any{map[string]any{"lastTransitionTime": "2026-10-16T09:00:00Z", "reason": "2026-10-16T10:00:00Z"}},
			"kept":       []any{"nowhere", "now-", "now-5w", "now - 1h", "2026-10-15T06:00:00Z", int64(3), nil},
			"far":        []any{"now+999999d", "now-999999d", "now+36501d", "now+999999h", "2026-10-27T23:46:39Z"},
		},
	}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}

	// The plugin's own samples are left as written.
	if samples[0]["status"].(map[string]any)["lastScheduleTime"] != "now-6h" {
		t.Fatalf("samples changed: %v", samples)
	}
}
