package ichorgo

import (
	"testing"
	"time"
)

func TestCronNextRun(t *testing.T) {
	from := time.Date(2026, 10, 3, 12, 34, 56, 0, time.UTC) // a Saturday

	cases := []struct {
		spec string
		want time.Time
	}{
		{"* * * * *", time.Date(2026, 10, 3, 12, 35, 0, 0, time.UTC)},
		{"*/15 * * * *", time.Date(2026, 10, 3, 12, 45, 0, 0, time.UTC)},
		{"0 3 * * *", time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)},
		{"@hourly", time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)},
		{"@weekly", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)},
		{"@monthly", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{"@yearly", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"30 2 * * MON-FRI", time.Date(2026, 10, 5, 2, 30, 0, 0, time.UTC)},
		{"0 0 * * 7", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}, // 7 is Sunday too
		{"0 9 1,15 * *", time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)},
		{"0 0 29 2 *", time.Date(2028, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"0 12 1-7 JAN-MAR ?", time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC)},
		// Day of month and day of week both restricted: either matches, like cron.
		{"0 0 13 * FRI", time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)},
		{"5-10/2 * * * *", time.Date(2026, 10, 3, 13, 5, 0, 0, time.UTC)},
		// A stepped wildcard restricts the day: odd days or Mondays (Sunday 4th is even, 5th is both).
		{"0 0 */2 * 1", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
		{"0 0 */2 * 0", time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)},
	}

	for _, c := range cases {
		sched, err := parseCronSchedule(c.spec)
		if err != nil {
			t.Fatalf("%q: %v", c.spec, err)
		}

		if got := sched.next(from); !got.Equal(c.want) {
			t.Errorf("%q: next %v, want %v", c.spec, got, c.want)
		}
	}
}

func TestCronNextRunInTimeZone(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata")
	}

	sched, err := parseCronSchedule("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if got, want := sched.next(from.In(paris)), time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("next %v, want %v", got, want)
	}
}

func TestCronRejects(t *testing.T) {
	for _, spec := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "a b c d e", "@every 5m", "5-1 * * * *"} {
		if _, err := parseCronSchedule(spec); err == nil {
			t.Errorf("%q: expected an error", spec)
		}
	}
}

func TestCronImpossibleDateHasNoNextRun(t *testing.T) {
	sched, err := parseCronSchedule("0 0 31 2 *")
	if err != nil {
		t.Fatal(err)
	}

	if got := sched.next(time.Now()); !got.IsZero() {
		t.Fatalf("got %v", got)
	}
}
