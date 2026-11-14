package ichorgo

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

// mNode is one synthetic node: its memory use at hour h of the run (NaN: not reported).
type mNode struct {
	addr string
	mem  func(h int) float64
}

// mRing appends one record every step hours over hours hours, ending at now.
func mRing(t *testing.T, now int64, hours, step int, nodes ...mNode) []byte {
	t.Helper()

	var ring []byte

	for h := 0; h <= hours; h += step {
		at := now - int64(hours-h)*hHour

		var ns []string

		for _, n := range nodes {
			m := n.mem(h)
			if math.IsNaN(m) {
				ns = append(ns, fmt.Sprintf(`{"node":"%s","hostname":"host-%s","health":"ready"}`, n.addr, n.addr))
				continue
			}

			ns = append(ns, fmt.Sprintf(`{"node":"%s","hostname":"host-%s","health":"ready","memUsedPercent":%v}`, n.addr, n.addr, m))
		}

		ring = hAppend(t, ring, fmt.Sprintf(`{"at":%d,"nodes":[%s]}`, at, strings.Join(ns, ",")), now)
	}

	return ring
}

func mForecast(t *testing.T, ring []byte, now int64, crit float64) historyMemoryForecastResult {
	t.Helper()

	out, err := HistoryMemoryForecast(ring, now, crit)
	if err != nil {
		t.Fatalf("memory forecast: %v", err)
	}

	var res historyMemoryForecastResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}

	return res
}

func TestHistoryMemoryForecast(t *testing.T) {
	now := hT0 + 10*hDay

	ring := mRing(t, now, 48, 1,
		// Leaks 1.8 points a day from 50 %, with a little noise: critical (90) in days.
		mNode{"10.0.0.2", func(h int) float64 { return 50 + 1.8*float64(h)/24 + noisy(h, 0.2) }},
		// Flat with cache noise: a row, but low and without a projection.
		mNode{"10.0.0.3", func(h int) float64 { return 40 + noisy(h, 3) }},
		// Rebooted at hour 30 (from 85 down to 30): the fit restarts and the 18 h after is too short.
		mNode{"10.0.0.4", func(h int) float64 {
			if h < 30 {
				return 60 + float64(h)
			}

			return 30 + 0.5*float64(h-30)
		}},
		// Never reports memory: no row.
		mNode{"10.0.0.5", func(int) float64 { return math.NaN() }},
	)

	res := mForecast(t, ring, now, 90)
	if len(res.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want 3 rows (the one without memory left out)", res.Nodes)
	}

	byNode := map[string]historyMemoryForecast{}
	for _, n := range res.Nodes {
		byNode[n.Node] = n
	}

	leak := byNode["10.0.0.2"]
	if leak.Hostname != "host-10.0.0.2" || leak.Confidence != forecastHigh || leak.DaysToCritical == nil || leak.DaysToFull == nil {
		t.Fatalf("leaking node = %+v, want a high-confidence projection", leak)
	}

	if s := leak.SlopePerDay; s < 1.6 || s > 2.0 {
		t.Errorf("slope = %v, want about 1.8", s)
	}

	// From about 53.6 % at hour 48, (90 - 53.6) / 1.8 ≈ 20 days.
	if d := *leak.DaysToCritical; d < 18 || d > 22 {
		t.Errorf("days to critical = %v, want about 20", d)
	}

	if flat := byNode["10.0.0.3"]; flat.Confidence != forecastLow || flat.DaysToCritical != nil {
		t.Errorf("flat node = %+v, want low and no projection", flat)
	}

	if reboot := byNode["10.0.0.4"]; reboot.Confidence != forecastLow || reboot.Points != 19 || reboot.UsedPercent != 39 {
		t.Errorf("rebooted node = %+v, want the fit restarted after the drop (19 points from 30 %%)", reboot)
	}
}

func TestHistoryMemoryForecastEdges(t *testing.T) {
	now := hT0 + 10*hDay

	if res := mForecast(t, nil, now, 0); res.Nodes == nil || len(res.Nodes) != 0 {
		t.Errorf("empty ring = %+v, want an empty list", res)
	}

	// Critical <= 0 takes 90; already past it, only the days to full are given.
	ring := mRing(t, now, 30, 1, mNode{"10.0.0.2", func(h int) float64 { return 91 + 0.2*float64(h) }})

	n := mForecast(t, ring, now, 0).Nodes[0]
	if n.Confidence != forecastHigh || n.DaysToCritical != nil || n.DaysToFull == nil {
		t.Errorf("past critical = %+v, want days to full only", n)
	}
}
