package relay

import (
	"errors"
	"fmt"
	"time"

	// The IANA zones, embedded (about 450 KB): the relay's image has no zoneinfo, and without
	// them no device's quiet hours could be read.
	_ "time/tzdata"
)

// QuietHours is a daily window, wall-clock time ("HH:MM", 24 h) in an IANA zone, during which
// a phone wants no notification; ExceptCritical lets the critical alerts through. Start == End
// (both empty included) is no window. The alert rules of the apps and a paired Device share it.
type QuietHours struct {
	Start          string `json:"start"`
	End            string `json:"end"`
	TZ             string `json:"tz"`
	ExceptCritical bool   `json:"exceptCritical"`
}

// QuietWindow is a parsed QuietHours: minutes since midnight in its zone.
type QuietWindow struct {
	start, end int
	loc        *time.Location
	// ExceptCritical: the window does not hold the critical alerts back.
	ExceptCritical bool
}

// Window parses the quiet hours; nil when Start == End (no window). The errors quote the bad
// value: a time that is not HH:MM, a missing zone, one that is not IANA.
func (q QuietHours) Window() (*QuietWindow, error) {
	if q.Start == q.End {
		return nil, nil
	}

	start, err := parseClockMinutes(q.Start)
	if err != nil {
		return nil, fmt.Errorf("start %w", err)
	}

	end, err := parseClockMinutes(q.End)
	if err != nil {
		return nil, fmt.Errorf("end %w", err)
	}

	if q.TZ == "" {
		return nil, errors.New("no time zone (pass the phone's IANA zone)")
	}

	loc, err := time.LoadLocation(q.TZ)
	if err != nil || q.TZ == "Local" {
		return nil, fmt.Errorf("time zone %q is not an IANA zone", q.TZ)
	}

	return &QuietWindow{start: start, end: end, loc: loc, ExceptCritical: q.ExceptCritical}, nil
}

// parseClockMinutes reads "HH:MM" (24 h) as minutes since midnight.
func parseClockMinutes(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != len("15:04") {
		return 0, fmt.Errorf("%q: want HH:MM", s)
	}

	return t.Hour()*60 + t.Minute(), nil
}

// Until is the end of the window now falls in, or the zero time when it is outside.
// It compares wall-clock minutes, so a DST day keeps the window at the same clock times.
func (w QuietWindow) Until(now time.Time) time.Time {
	local := now.In(w.loc)
	mins := local.Hour()*60 + local.Minute()
	y, m, d := local.Date()
	endOn := func(day int) time.Time {
		return time.Date(y, m, day, w.end/60, w.end%60, 0, 0, w.loc)
	}

	switch {
	case w.start < w.end && mins >= w.start && mins < w.end:
		return endOn(d)
	case w.start > w.end && mins >= w.start:
		return endOn(d + 1)
	case w.start > w.end && mins < w.end:
		return endOn(d)
	default:
		return time.Time{}
	}
}

// quietAlerts keeps the alerts a device's quiet hours let through at now: all of them outside
// the window (or without one, or with one that cannot be read: a bad window never drops an
// alert), none inside it, or only the critical ones (their resolution too) with ExceptCritical.
func quietAlerts(q *QuietHours, alerts []PushAlert, now time.Time) []PushAlert {
	if q == nil {
		return alerts
	}

	w, err := q.Window()
	if err != nil || w == nil || w.Until(now).IsZero() {
		return alerts
	}

	if !w.ExceptCritical {
		return nil
	}

	kept := make([]PushAlert, 0, len(alerts))
	for _, a := range alerts {
		if a.Severity == SeverityCritical {
			kept = append(kept, a)
		}
	}

	return kept
}
