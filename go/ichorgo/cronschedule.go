package ichorgo

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSchedule is a parsed CronJob schedule: the standard five fields (minute hour
// day-of-month month day-of-week) with lists, ranges, steps, month and day names, and the
// @hourly/@daily/@weekly/@monthly/@yearly macros, as the Kubernetes CronJob controller reads it.
type cronSchedule struct {
	minute, hour, dom, month, dow uint64
	// Day of month and day of week both restricted: a day matching either runs, as in cron.
	eitherDay bool
}

var cronMacros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

type cronField struct {
	min, max int
	names    []string // names[i] is value min+i
}

var (
	cronMinute = cronField{min: 0, max: 59}
	cronHour   = cronField{min: 0, max: 23}
	cronDom    = cronField{min: 1, max: 31}
	cronMonth  = cronField{min: 1, max: 12, names: []string{"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"}}
	// 7 is Sunday as well, folded into 0 once parsed.
	cronDow = cronField{min: 0, max: 7, names: []string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}}
)

func parseCronSchedule(spec string) (cronSchedule, error) {
	spec = strings.TrimSpace(spec)
	if expanded, ok := cronMacros[strings.ToLower(spec)]; ok {
		spec = expanded
	}

	fields := strings.Fields(spec)
	if len(fields) != 5 {
		return cronSchedule{}, fmt.Errorf("schedule %q: expected 5 fields", spec)
	}

	var (
		s   cronSchedule
		err error
	)

	targets := []struct {
		out   *uint64
		field cronField
	}{{&s.minute, cronMinute}, {&s.hour, cronHour}, {&s.dom, cronDom}, {&s.month, cronMonth}, {&s.dow, cronDow}}

	for i, t := range targets {
		if *t.out, err = parseCronField(fields[i], t.field); err != nil {
			return cronSchedule{}, fmt.Errorf("schedule %q: %w", spec, err)
		}
	}

	if s.dow&(1<<7) != 0 {
		s.dow = s.dow&^(1<<7) | 1
	}

	s.eitherDay = !isCronWildcard(fields[2]) && !isCronWildcard(fields[4])

	return s, nil
}

// isCronWildcard tells an unrestricted field as the controller's parser (robfig/cron) does:
// "*" or "?" without a step; "*/2" restricts the day.
func isCronWildcard(field string) bool {
	for part := range strings.SplitSeq(field, ",") {
		r, step, _ := strings.Cut(part, "/")
		if (r == "*" || r == "?") && (step == "" || step == "1") {
			return true
		}
	}

	return false
}

// parseCronField reads one field into a bit set of its allowed values.
func parseCronField(field string, f cronField) (uint64, error) {
	var bits uint64

	for part := range strings.SplitSeq(field, ",") {
		rangePart, stepPart, hasStep := strings.Cut(part, "/")

		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepPart)
			if err != nil || n <= 0 {
				return 0, fmt.Errorf("bad step in %q", part)
			}

			step = n
		}

		lo, hi, err := parseCronRange(rangePart, f, hasStep)
		if err != nil {
			return 0, err
		}

		for v := lo; v <= hi; v += step {
			bits |= 1 << v
		}
	}

	return bits, nil
}

func parseCronRange(s string, f cronField, stepped bool) (lo, hi int, err error) {
	if s == "*" || s == "?" {
		return f.min, f.max, nil
	}

	first, last, isRange := strings.Cut(s, "-")
	if lo, err = parseCronValue(first, f); err != nil {
		return 0, 0, err
	}

	switch {
	case isRange:
		if hi, err = parseCronValue(last, f); err != nil {
			return 0, 0, err
		}
	case stepped:
		hi = f.max // "5/10" is "5-max/10"
	default:
		hi = lo
	}

	if lo > hi {
		return 0, 0, fmt.Errorf("bad range %q", s)
	}

	return lo, hi, nil
}

func parseCronValue(s string, f cronField) (int, error) {
	for i, name := range f.names {
		if strings.EqualFold(s, name) {
			return f.min + i, nil
		}
	}

	n, err := strconv.Atoi(s)
	if err != nil || n < f.min || n > f.max {
		return 0, fmt.Errorf("value %q out of range %d-%d", s, f.min, f.max)
	}

	return n, nil
}

// cronSearchYears bounds the search for the next run: Feb 29 comes back within 8 years.
const cronSearchYears = 8

// next is the first run strictly after from, in from's location; zero when the schedule
// never runs (Feb 31).
func (s cronSchedule) next(from time.Time) time.Time {
	loc := from.Location()
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(cronSearchYears, 0, 0)

	for t.Before(limit) {
		switch {
		case s.month&(1<<uint(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		case s.hour&(1<<uint(t.Hour())) == 0:
			// Not Truncate: it rounds in UTC, which is off by 30 minutes in India.
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case s.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}

	return time.Time{}
}

func (s cronSchedule) dayMatches(t time.Time) bool {
	dom := s.dom&(1<<uint(t.Day())) != 0
	dow := s.dow&(1<<uint(t.Weekday())) != 0

	if s.eitherDay {
		return dom || dow
	}

	return dom && dow
}
