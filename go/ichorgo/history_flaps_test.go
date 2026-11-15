package ichorgo

import (
	"encoding/json"
	"fmt"
	"testing"
)

func hFlaps(t *testing.T, ring []byte, now int64, hours int) historyFlapsResult {
	t.Helper()

	out, err := HistoryAlertFlaps(ring, now, hours)
	if err != nil {
		t.Fatalf("flaps: %v", err)
	}

	var r historyFlapsResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return r
}

func TestHistoryAlertFlaps(t *testing.T) {
	var ring []byte

	// Every 15 min over 26 h: "flap" fires and recovers on alternate runs from hour 20 on,
	// "steady" fires the whole time, "old" flapped only before the window, and an
	// unreachable run in the middle says nothing.
	end := 26 * 60
	for m := 0; m <= end; m += 15 {
		at := hT0 + int64(m)*hMin

		var alerts []string
		if m >= 20*60 && (m/15)%2 == 0 {
			alerts = append(alerts, "flap")
		}

		alerts = append(alerts, "steady")
		if m < 60 && (m/15)%2 == 0 {
			alerts = append(alerts, "old")
		}

		rec := hRec(at, []string{"a=ready"}, alerts...)
		if m == 23*60 {
			rec = fmt.Sprintf(`{"at":%d,"reachable":false}`, at)
		}

		ring = hAppend(t, ring, rec, at)
	}

	now := hT0 + int64(end)*hMin
	r := hFlaps(t, ring, now, 0) // 0 → 24 h

	if r.WindowHours != 24 || len(r.Alerts) != 1 {
		t.Fatalf("flaps = %+v, want only flap over 24 h", r)
	}

	f := r.Alerts[0]
	// Fired every 30 min from 20:00 to 26:00 (13 runs), less the unreachable 23:00 run that
	// keeps it as it was (closed): 12 opens, still open at the last run.
	if f.Key != "flap" || f.Track != "data" || f.Opens != 12 || f.Closes != 11 || !f.Open || f.LastOpenedAt != now {
		t.Errorf("flap = %+v", f)
	}

	// A 1 h window: 25:00, 25:30 and 26:00. "steady", open since before the window, counts
	// once and so is no flap.
	short := hFlaps(t, ring, now, 1)
	if len(short.Alerts) != 1 || short.Alerts[0].Key != "flap" || short.Alerts[0].Opens != 3 {
		t.Errorf("1 h flaps = %+v", short)
	}
}

func TestHistoryAlertFlapsEdges(t *testing.T) {
	r := hFlaps(t, nil, hT0, 24)
	if r.Alerts == nil || len(r.Alerts) != 0 {
		t.Errorf("empty ring: %+v, want an empty list", r)
	}

	if _, err := HistoryAlertFlaps(nil, hT0, -1); err == nil {
		t.Error("negative window accepted")
	}

	if _, err := HistoryAlertFlaps(nil, hT0, 24*31); err == nil {
		t.Error("window past the ring's 30 days accepted")
	}

	// Sorted by opens, then key; one open is not a flap.
	var ring []byte

	pattern := []struct {
		m      int
		alerts []string
	}{
		{0, []string{"b", "a", "c"}}, {15, nil}, {30, []string{"b", "a"}}, {45, nil}, {60, []string{"a"}}, {75, nil},
	}
	for _, p := range pattern {
		at := hT0 + int64(p.m)*hMin
		ring = hAppend(t, ring, hRec(at, []string{"a=ready"}, p.alerts...), at)
	}

	got := hFlaps(t, ring, hT0+75*hMin, 24)

	var keys []string
	for _, a := range got.Alerts {
		keys = append(keys, fmt.Sprintf("%s=%d", a.Key, a.Opens))
	}

	if fmt.Sprint(keys) != "[a=3 b=2]" {
		t.Errorf("order = %v, want [a=3 b=2]", keys)
	}
}
