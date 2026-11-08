package expr

import (
	"testing"
	"time"
)

// prev walks back over the clock changes of a zone without looping and finds the run next
// finds from just before it.
func TestCronPrevAcrossClockChanges(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip(err)
	}

	for _, spec := range []string{"30 2 * * *", "0 * * * *", "*/7 1-3 * * 0", "0 0 * * 1"} {
		s, err := parseCronSchedule(spec)
		if err != nil {
			t.Fatal(err)
		}

		// Every quarter hour around the 2026 changes (29 March and 25 October).
		for _, day := range []time.Time{time.Date(2026, 3, 28, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)} {
			for m := 0; m < 8*60; m += 15 {
				from := day.Add(time.Duration(m) * time.Minute).In(paris)

				p := s.prev(from)
				if p.IsZero() || !p.Before(from) {
					t.Fatalf("%s from %s: prev %s", spec, from, p)
				}

				if n := s.next(p); n.Before(from) {
					t.Errorf("%s from %s: prev %s is not the last run, %s comes after it", spec, from, p, n)
				}
			}
		}
	}
}
