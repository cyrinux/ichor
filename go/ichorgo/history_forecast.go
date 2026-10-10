package ichorgo

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// Storage trend: a least-squares line through a volume's fill over the last week tells the
// apps when it will be full, so they can warn days before the fill alert opens.

const (
	// forecastWindow is how far back the fit looks (raw records: the ring keeps every
	// record that recent).
	forecastWindow = 7 * 24 * time.Hour
	// forecastMinSpan and forecastMinPoints are the least data a projection needs.
	forecastMinSpan   = 24 * time.Hour
	forecastMinPoints = 6
	// forecastMinSlope (percentage points per day) is the growth below which a volume
	// counts as flat.
	forecastMinSlope = 0.05
	// forecastMinR2 is the fit quality below which the fill is too noisy to project.
	forecastMinR2 = 0.5
	// forecastDropPoints is the fall (percentage points between two samples) taken as a
	// cleanup: the fit restarts after it.
	forecastDropPoints = 10.0
	// forecastMaxDays caps a projection: further out says nothing useful.
	forecastMaxDays = 365.0
	// forecastDay is a day in milliseconds, the slope's time unit.
	forecastDay = float64(24 * time.Hour / time.Millisecond)
)

// Confidence of a forecast row.
const (
	forecastHigh = "high"
	forecastLow  = "low"
)

type historyForecastResult struct {
	Volumes []historyVolumeForecast `json:"volumes"`
}

type historyVolumeForecast struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Node           string   `json:"node"`
	UsedPercent    float64  `json:"usedPercent"`
	LatestAt       int64    `json:"latestAt"`
	SlopePerDay    float64  `json:"slopePerDay"`
	DaysToFull     *float64 `json:"daysToFull,omitempty"`
	DaysToCritical *float64 `json:"daysToCritical,omitempty"`
	Confidence     string   `json:"confidence"`
	Points         int      `json:"points"`
	SpanHours      float64  `json:"spanHours"`
}

// HistoryVolumeForecast projects when each volume of the latest reachable record fills up:
// a least-squares line through its fill over the 7 days before nowMillis (restarted after
// a cleanup, a fall of more than 10 points). criticalPercent <= 0 takes 95.
//
// A row always carries the latest fill and the slope (percentage points per day);
// "daysToFull" and "daysToCritical" (counted from "latestAt") only come with confidence
// "high": at least 24 h and 6 samples, growth of at least 0.05 points a day and a fit
// with R² >= 0.5. "daysToCritical" is left out when the volume is already critical, and
// either is left out beyond 365 days.
func HistoryVolumeForecast(ringBytes []byte, nowMillis int64, criticalPercent float64) (out string, err error) {
	defer maskResult(&out, &err)

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	if criticalPercent <= 0 {
		criticalPercent = volumeCriticalPercent
	}

	return toJSON(forecastVolumes(ring.records, nowMillis, criticalPercent))
}

func forecastVolumes(records []historyRecord, now int64, crit float64) historyForecastResult {
	res := historyForecastResult{Volumes: []historyVolumeForecast{}}
	since := now - forecastWindow.Milliseconds()

	var latest *historyRecord

	series := map[string][]historyPoint{}

	for i := range records {
		rec := records[i]
		if !rec.reachable() || rec.At < since || rec.At > now {
			continue
		}

		latest = &records[i]

		for _, v := range rec.Volumes {
			series[v.Key] = append(series[v.Key], historyPoint{rec.At, v.UsedPercent})
		}
	}

	if latest == nil {
		return res
	}

	for _, v := range latest.Volumes {
		row := forecastVolume(afterCleanup(series[v.Key]), crit)
		row.Key, row.Name, row.Node = v.Key, v.Name, v.Node
		res.Volumes = append(res.Volumes, row)
	}

	slices.SortFunc(res.Volumes, func(a, b historyVolumeForecast) int { return cmp.Compare(a.Key, b.Key) })

	return res
}

// afterCleanup keeps the points after the last fall of more than forecastDropPoints.
func afterCleanup(points []historyPoint) []historyPoint {
	start := 0

	for i := 1; i < len(points); i++ {
		if points[i-1].V-points[i].V > forecastDropPoints {
			start = i
		}
	}

	return points[start:]
}

// forecastVolume fits points (in time order, at least one) and projects the fill.
func forecastVolume(points []historyPoint, crit float64) historyVolumeForecast {
	last := points[len(points)-1]
	span := last.T - points[0].T
	slope, r2 := fitLine(points)

	row := historyVolumeForecast{
		UsedPercent: last.V,
		LatestAt:    last.T,
		SlopePerDay: roundTo(slope, 100),
		Confidence:  forecastLow,
		Points:      len(points),
		SpanHours:   roundTo(float64(span)/float64(time.Hour.Milliseconds()), 10),
	}

	if span < forecastMinSpan.Milliseconds() || len(points) < forecastMinPoints ||
		slope < forecastMinSlope || r2 < forecastMinR2 {
		return row
	}

	row.Confidence = forecastHigh
	row.DaysToFull = daysUntil(100, last.V, slope)

	if last.V < crit {
		row.DaysToCritical = daysUntil(crit, last.V, slope)
	}

	return row
}

// fitLine returns the least-squares slope (points per day) and R² of points. A single
// point, or points all at one time, give 0 and 0; a flat line has R² 0 (nothing explained).
func fitLine(points []historyPoint) (slope, r2 float64) {
	n := float64(len(points))
	t0 := points[0].T

	var sx, sy float64

	for _, p := range points {
		sx += float64(p.T-t0) / forecastDay
		sy += p.V
	}

	mx, my := sx/n, sy/n

	var sxx, sxy, syy float64

	for _, p := range points {
		dx := float64(p.T-t0)/forecastDay - mx
		dy := p.V - my
		sxx += dx * dx
		sxy += dx * dy
		syy += dy * dy
	}

	if sxx == 0 || syy == 0 {
		return 0, 0
	}

	return sxy / sxx, sxy * sxy / (sxx * syy)
}

// daysUntil is how long growing by slope points a day takes from used to target, nil
// beyond forecastMaxDays.
func daysUntil(target, used, slope float64) *float64 {
	days := roundTo(max(target-used, 0)/slope, 10)
	if days > forecastMaxDays {
		return nil
	}

	return &days
}

// roundTo rounds v to 1/scale.
func roundTo(v, scale float64) float64 {
	return math.Round(v*scale) / scale
}
