package ichorgo

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// fVol is one synthetic volume: its fill at hour h of the run.
type fVol struct {
	name string
	fill func(h int) float64
}

// fRing appends one record every step hours over hours hours, ending at now.
func fRing(t *testing.T, now int64, hours, step int, vols ...fVol) []byte {
	t.Helper()

	var ring []byte

	for h := 0; h <= hours; h += step {
		at := now - int64(hours-h)*hHour

		var vs []string

		for _, v := range vols {
			p := v.fill(h)
			if math.IsNaN(p) {
				continue // the volume did not exist then
			}

			vs = append(vs, fmt.Sprintf(`{"key":"10.0.0.1|%s","name":"%s","node":"10.0.0.1","usedPercent":%v}`, v.name, v.name, p))
		}

		ring = hAppend(t, ring, fmt.Sprintf(`{"at":%d,"volumes":[%s]}`, at, strings.Join(vs, ",")), now)
	}

	return ring
}

func fForecast(t *testing.T, ring []byte, now int64, crit float64) historyForecastResult {
	t.Helper()

	out, err := HistoryVolumeForecast(ring, now, crit)
	if err != nil {
		t.Fatalf("forecast: %v", err)
	}

	var res historyForecastResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return res
}

// noisy is a deterministic +-amp wobble.
func noisy(h int, amp float64) float64 {
	return amp * math.Sin(float64(h)*1.7) * math.Cos(float64(h)*0.3)
}

func TestHistoryVolumeForecast(t *testing.T) {
	now := hT0 + 40*hDay
	ptr := func(v float64) *float64 { return &v }

	tests := []struct {
		name     string
		hours    int
		step     int
		crit     float64
		fill     func(h int) float64
		conf     string
		slope    float64
		full     *float64
		critical *float64
		points   int
	}{
		{
			name: "steady growth", hours: 72, step: 2, fill: func(h int) float64 { return 50 + float64(h)/24*2 },
			conf: forecastHigh, slope: 2, full: ptr(22), critical: ptr(19.5), points: 37,
		},
		{
			name: "custom critical", hours: 72, step: 2, crit: 80, fill: func(h int) float64 { return 50 + float64(h)/24*2 },
			conf: forecastHigh, slope: 2, full: ptr(22), critical: ptr(12), points: 37,
		},
		{
			name: "flat", hours: 72, step: 2, fill: func(int) float64 { return 60 },
			conf: forecastLow, slope: 0, points: 37,
		},
		{
			name: "shrinking", hours: 72, step: 2, fill: func(h int) float64 { return 80 - float64(h)/24 },
			conf: forecastLow, slope: -1, points: 37,
		},
		{
			name: "too slow", hours: 168, step: 4, fill: func(h int) float64 { return 50 + float64(h)/24*0.03 },
			conf: forecastLow, slope: 0.03, points: 43,
		},
		{
			name: "noisy", hours: 72, step: 2, fill: func(h int) float64 { return 50 + float64(h)/24*0.2 + noisy(h, 4) },
			conf: forecastLow, points: 37,
		},
		{
			name: "under a day", hours: 20, step: 2, fill: func(h int) float64 { return 50 + float64(h) },
			conf: forecastLow, slope: 24, points: 11,
		},
		{
			name: "too few points", hours: 48, step: 12, fill: func(h int) float64 { return 50 + float64(h)/24 },
			conf: forecastLow, slope: 1, points: 5,
		},
		{
			name: "already critical", hours: 72, step: 2, fill: func(h int) float64 { return 94 + float64(h)/24 },
			conf: forecastHigh, slope: 1, full: ptr(3), points: 37,
		},
		{
			name: "too far out", hours: 72, step: 2, fill: func(h int) float64 { return 10 + float64(h)/24*0.1 },
			conf: forecastHigh, slope: 0.1, points: 37,
		},
		{
			// 2 points a day for 3 days, then a cleanup from 56 to 30 and 2 points a day
			// for 2 days: only the last 2 days count.
			name: "cleanup drop", hours: 120, step: 2, fill: func(h int) float64 {
				if h <= 72 {
					return 50 + float64(h)/24*2
				}

				return 30 + float64(h-74)/24*2
			},
			conf: forecastHigh, slope: 2, full: ptr(33.1), critical: ptr(30.6), points: 24,
		},
		{
			// the volume appeared 30 hours ago: only those records count.
			name: "new volume", hours: 120, step: 2, fill: func(h int) float64 {
				if h < 90 {
					return math.NaN()
				}

				return 20 + float64(h-90)/24*4
			},
			conf: forecastHigh, slope: 4, full: ptr(18.8), critical: ptr(17.5), points: 16,
		},
		{
			// 9 days of data: steep growth in the first 2, flat since. The fit only sees
			// the last 7, so it finds no growth.
			name: "window", hours: 216, step: 4, fill: func(h int) float64 {
				return 10 + float64(min(h, 48))
			},
			conf: forecastLow, slope: 0,
			points: 43,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := fRing(t, now, tt.hours, tt.step, fVol{"data", tt.fill})
			res := fForecast(t, ring, now, tt.crit)

			if len(res.Volumes) != 1 {
				t.Fatalf("volumes = %+v", res.Volumes)
			}

			v := res.Volumes[0]
			if v.Key != "10.0.0.1|data" || v.Name != "data" || v.Node != "10.0.0.1" || v.LatestAt != now {
				t.Errorf("row = %+v", v)
			}

			if (tt.conf != "" && v.Confidence != tt.conf) || v.Points != tt.points {
				t.Errorf("confidence %s points %d, want %s %d", v.Confidence, v.Points, tt.conf, tt.points)
			}

			if tt.name != "noisy" && math.Abs(v.SlopePerDay-tt.slope) > 0.011 {
				t.Errorf("slope = %v, want %v", v.SlopePerDay, tt.slope)
			}

			checkDays(t, "daysToFull", v.DaysToFull, tt.full)
			checkDays(t, "daysToCritical", v.DaysToCritical, tt.critical)
		})
	}
}

func checkDays(t *testing.T, what string, got, want *float64) {
	t.Helper()

	switch {
	case got == nil && want == nil:
	case got == nil || want == nil:
		t.Errorf("%s = %v, want %v", what, fmtDays(got), fmtDays(want))
	case math.Abs(*got-*want) > 0.11:
		t.Errorf("%s = %v, want %v", what, *got, *want)
	}
}

func fmtDays(d *float64) string {
	if d == nil {
		return "absent"
	}

	return fmt.Sprint(*d)
}

func TestHistoryVolumeForecastMultipleVolumes(t *testing.T) {
	now := hT0 + 40*hDay
	ring := fRing(t, now, 72, 2,
		fVol{"b-grow", func(h int) float64 { return 40 + float64(h)/24*3 }},
		fVol{"a-flat", func(int) float64 { return 70 }},
		fVol{"c-gone", func(h int) float64 {
			if h > 60 {
				return math.NaN() // removed: no row
			}

			return 30 + float64(h)
		}},
	)

	res := fForecast(t, ring, now, 0)

	if len(res.Volumes) != 2 || res.Volumes[0].Name != "a-flat" || res.Volumes[1].Name != "b-grow" {
		t.Fatalf("volumes = %+v", res.Volumes)
	}

	if res.Volumes[0].Confidence != forecastLow || res.Volumes[0].DaysToFull != nil {
		t.Errorf("flat = %+v", res.Volumes[0])
	}

	grow := res.Volumes[1]
	if grow.Confidence != forecastHigh || grow.UsedPercent != 49 || grow.SpanHours != 72 {
		t.Errorf("grow = %+v", grow)
	}

	checkDays(t, "daysToFull", grow.DaysToFull, &[]float64{17}[0])
	checkDays(t, "daysToCritical", grow.DaysToCritical, &[]float64{15.3}[0])
}

func TestHistoryVolumeForecastEdges(t *testing.T) {
	now := hT0 + 40*hDay

	t.Run("empty ring", func(t *testing.T) {
		if out, err := HistoryVolumeForecast(nil, now, 0); err != nil || out != `{"volumes":[]}` {
			t.Errorf("got %s, %v", out, err)
		}
	})

	t.Run("corrupt ring", func(t *testing.T) {
		if _, err := HistoryVolumeForecast([]byte("nope"), now, 0); err == nil {
			t.Error("want an error")
		}
	})

	t.Run("unreachable and stale records", func(t *testing.T) {
		ring := fRing(t, now-10*hDay, 72, 2, fVol{"data", func(h int) float64 { return 50 + float64(h)/24 }})
		ring = hAppend(t, ring, fmt.Sprintf(`{"at":%d,"reachable":false}`, now), now)

		if res := fForecast(t, ring, now, 0); len(res.Volumes) != 0 {
			t.Errorf("volumes = %+v", res.Volumes)
		}
	})

	t.Run("full volume", func(t *testing.T) {
		ring := fRing(t, now, 72, 2, fVol{"data", func(h int) float64 { return min(97+float64(h)/24, 100) }})
		v := fForecast(t, ring, now, 0).Volumes[0]

		if v.Confidence != forecastHigh || v.DaysToFull == nil || *v.DaysToFull != 0 || v.DaysToCritical != nil {
			t.Errorf("row = %+v", v)
		}
	})

	t.Run("single point", func(t *testing.T) {
		if s, r2 := fitLine([]historyPoint{{T: 1, V: 5}}); s != 0 || r2 != 0 {
			t.Errorf("fit = %v %v", s, r2)
		}
	})
}
