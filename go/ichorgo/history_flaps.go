package ichorgo

import (
	"cmp"
	"fmt"
	"slices"
)

// historyFlapsDefaultHours is the window HistoryAlertFlaps counts over when asked for 0 hours.
const historyFlapsDefaultHours = 24

// historyFlapsMaxHours is the longest window: the ring keeps 30 days.
const historyFlapsMaxHours = 30 * 24

// historyFlap is one key's count. Track is the ring's (iOS keys the ring by track, without the
// "track:" its alert keys carry; Android's ring keys are the alert keys).
type historyFlap struct {
	Key          string `json:"key"`
	Track        string `json:"track,omitempty"`
	Opens        int    `json:"opens"`
	Closes       int    `json:"closes"`
	Open         bool   `json:"open"`
	LastOpenedAt int64  `json:"lastOpenedAt"`
}

type historyFlapsResult struct {
	WindowHours int           `json:"windowHours"`
	Alerts      []historyFlap `json:"alerts"`
}

// HistoryAlertFlaps counts, for each alert key of ringBytes, how many times it opened and
// closed in the windowHours before nowMillis (0 → 24 h; at most the ring's 30 days): the
// alerts that come and go, for the rules' flapping check (AlertRulesApply's flaps). An alert
// already open when the window starts counts as one open; unreachable runs say nothing, as in
// HistoryQuery. Keys that opened once are left out; the rest are sorted by opens, most first.
// The keys are the app's own (it matches them back to its alerts), so the result is not masked.
func HistoryAlertFlaps(ringBytes []byte, nowMillis int64, windowHours int) (out string, err error) {
	defer maskErr(&err)

	if windowHours < 0 || windowHours > historyFlapsMaxHours {
		return "", fmt.Errorf("history flaps: window %d h: want 0 to %d", windowHours, historyFlapsMaxHours)
	}

	if windowHours == 0 {
		windowHours = historyFlapsDefaultHours
	}

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	since := nowMillis - int64(windowHours)*hourMillis

	return toJSON(historyFlapsResult{WindowHours: windowHours, Alerts: historyFlaps(ring.records, since, nowMillis)})
}

const hourMillis = int64(60 * 60 * 1000)

func historyFlaps(records []historyRecord, since, now int64) []historyFlap {
	var window []historyRecord

	for _, rec := range records {
		if rec.At >= since && rec.At <= now {
			window = append(window, rec)
		}
	}

	byKey := map[string]*historyFlap{}

	for _, span := range historyAlertSpans(window) {
		f := byKey[span.Key]
		if f == nil {
			f = &historyFlap{Key: span.Key}
			byKey[span.Key] = f
		}

		f.Opens++
		f.Track = span.Track
		f.LastOpenedAt = max(f.LastOpenedAt, span.OpenedAt)

		if span.ClosedAt != nil {
			f.Closes++
		}

		f.Open = span.ClosedAt == nil
	}

	out := []historyFlap{}

	for _, f := range byKey {
		if f.Opens > 1 {
			out = append(out, *f)
		}
	}

	slices.SortFunc(out, func(a, b historyFlap) int {
		return cmp.Or(cmp.Compare(b.Opens, a.Opens), cmp.Compare(a.Key, b.Key))
	})

	return out
}
