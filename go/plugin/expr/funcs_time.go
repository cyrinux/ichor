package expr

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Times are unix milliseconds, as everywhere in the Go core's JSON; 0 is "none".
var timeBuiltins = map[string]Func{
	"time":         {Min: 1, Max: 1, Returns: KindNumber, Call: func(_ Ctx, a []Value) (Value, error) { return unixMilli(a[0]), nil }},
	"age":          {Min: 1, Max: 1, Returns: KindNumber, Call: fnAge},
	"relativeTime": {Min: 1, Max: 1, Returns: KindString, Call: fnRelativeTime},
	"cronNext":     {Min: 1, Max: 3, Returns: KindNumber, Call: fnCronNext},
	"cronPrev":     {Min: 1, Max: 3, Returns: KindNumber, Call: fnCronPrev},
}

// unixMilli reads an RFC 3339 timestamp; a number is already unix ms; 0 otherwise.
func unixMilli(v Value) int64 {
	switch x := normalize(v).(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		t, err := time.Parse(time.RFC3339Nano, x)
		if err != nil {
			return 0
		}

		return t.UnixMilli()
	default:
		return 0
	}
}

// fnAge is the milliseconds since a time, null when there is none.
func fnAge(c Ctx, a []Value) (Value, error) {
	ms := unixMilli(a[0])
	if ms == 0 {
		return nil, nil
	}

	return c.Now.UnixMilli() - ms, nil
}

// fnRelativeTime words a time against now: "5m ago", "in 3h", "now"; "never" when there is
// none. English for now; the render tree carries the timestamp for the apps to localize.
func fnRelativeTime(c Ctx, a []Value) (Value, error) {
	ms := unixMilli(a[0])
	if ms == 0 {
		return "never", nil
	}

	// In float milliseconds: a time.Duration saturates past 292 years.
	d := float64(ms) - float64(c.Now.UnixMilli())

	future := d > 0
	if !future {
		d = -d
	}

	const second, minute, hour, day = 1e3, 60e3, 3600e3, 86400e3

	var text string

	switch {
	case d < 45*second:
		return "now", nil
	case d < hour:
		text = strconv.FormatFloat(math.Round(d/minute), 'f', 0, 64) + "m"
	case d < 48*hour:
		text = strconv.FormatFloat(math.Round(d/hour), 'f', 0, 64) + "h"
	default:
		text = strconv.FormatFloat(math.Round(d/day), 'f', 0, 64) + "d"
	}

	if future {
		return "in " + text, nil
	}

	return text + " ago", nil
}

// fnCronNext is the next run after now, in unix ms, of a daily time "HH:mm" (each part cron
// syntax) on cron weekdays ("1-5", "" every day) in an IANA time zone ("" is UTC), as
// kube-green schedules; at may also be a whole 5-field cron or a macro (@daily), then
// weekdays is ignored. 0 when the schedule or the zone does not parse.
func fnCronNext(c Ctx, a []Value) (Value, error) {
	cron, loc, ok := dailyCron(a)
	if !ok {
		return int64(0), nil
	}

	next := cron.next(c.Now.In(loc))
	if next.IsZero() {
		return int64(0), nil // never runs (Feb 31)
	}

	return next.UnixMilli(), nil
}

// fnCronPrev mirrors cronNext: the last run strictly before now, in unix ms; 0 when the
// schedule or the zone does not parse, or when it did not run in the last 8 years.
func fnCronPrev(c Ctx, a []Value) (Value, error) {
	cron, loc, ok := dailyCron(a)
	if !ok {
		return int64(0), nil
	}

	prev := cron.prev(c.Now.In(loc))
	if prev.IsZero() {
		return int64(0), nil
	}

	return prev.UnixMilli(), nil
}

// dailyCron reads the arguments of cronNext and cronPrev: (at, weekdays, tz).
func dailyCron(a []Value) (cronSchedule, *time.Location, bool) {
	at := strings.TrimSpace(toString(a[0]))

	weekdays, zone := "*", "UTC"
	if len(a) > 1 && toString(a[1]) != "" {
		weekdays = toString(a[1])
	}

	if len(a) > 2 && toString(a[2]) != "" {
		zone = toString(a[2])
	}

	spec := at
	if !strings.ContainsAny(at, " @") {
		hh, mm, ok := strings.Cut(at, ":")
		if !ok {
			return cronSchedule{}, nil, false
		}

		spec = mm + " " + hh + " * * " + weekdays
	}

	cron, err := parseCronSchedule(spec)
	if err != nil {
		return cronSchedule{}, nil, false
	}

	loc, err := time.LoadLocation(zone)
	if err != nil {
		return cronSchedule{}, nil, false
	}

	return cron, loc, true
}
