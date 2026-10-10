package ichorgo

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"time"
)

// historyMinGap is the shortest stretch without a record counted as a gap; a ring whose
// runs are further apart (a long monitor interval) gets 3 times its median spacing.
const historyMinGap = 2 * time.Hour

type historySpan struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type historyQueryResult struct {
	From        int64                 `json:"from"`
	To          int64                 `json:"to"`
	Records     int                   `json:"records"`
	ResetAt     int64                 `json:"resetAt,omitempty"`
	ResetReason string                `json:"resetReason,omitempty"`
	Nodes       []historyNodeTimeline `json:"nodes"`
	Alerts      []historyAlertSpan    `json:"alerts"`
	Volumes     []historyVolumeSeries `json:"volumes"`
	Memory      []historyMemorySeries `json:"memory"`
	Gaps        []historySpan         `json:"gaps"`
}

type historyNodeTimeline struct {
	Node      string            `json:"node"`
	Hostname  string            `json:"hostname"`
	Intervals []historyInterval `json:"intervals"`
	// UptimePercent is the ready share of the time the node was observed; absent if never.
	UptimePercent *float64 `json:"uptimePercent,omitempty"`
}

type historyInterval struct {
	State string `json:"state"`
	From  int64  `json:"from"`
	To    int64  `json:"to"`
}

type historyAlertSpan struct {
	Key      string `json:"key"`
	Track    string `json:"track,omitempty"`
	Severity string `json:"severity,omitempty"`
	Title    string `json:"title,omitempty"`
	OpenedAt int64  `json:"openedAt"`
	ClosedAt *int64 `json:"closedAt,omitempty"`
}

type historyVolumeSeries struct {
	Key    string         `json:"key"`
	Name   string         `json:"name"`
	Node   string         `json:"node"`
	Series []historyPoint `json:"series"`
}

type historyMemorySeries struct {
	Node   string         `json:"node"`
	Series []historyPoint `json:"series"`
}

// historyPoint is a [time, percent] pair.
type historyPoint struct {
	T int64
	V float64
}

func (p historyPoint) MarshalJSON() ([]byte, error) {
	b := append([]byte{'['}, strconv.FormatInt(p.T, 10)...)
	b = append(b, ',')
	b = strconv.AppendFloat(b, p.V, 'f', -1, 64)

	return append(b, ']'), nil
}

// historySegment is the time a reachable record stands for: until the next record, unless
// a gap separates them.
type historySegment struct {
	rec      historyRecord
	from, to int64
}

func queryHistory(ring historyRing, since, now int64) historyQueryResult {
	segments, gaps := historyTimeline(ring.records, since, now)

	result := historyQueryResult{
		From: since, To: now,
		ResetAt: ring.meta.ResetAt, ResetReason: ring.meta.ResetReason,
		Nodes:  historyNodeTimelines(segments),
		Alerts: []historyAlertSpan{},
		Gaps:   gaps,
	}

	for _, a := range historyAlertSpans(ring.records) {
		if a.OpenedAt <= now && (a.ClosedAt == nil || *a.ClosedAt >= since) {
			result.Alerts = append(result.Alerts, a)
		}
	}

	var inRange []historyRecord

	for _, rec := range ring.records {
		if rec.At >= since && rec.At <= now {
			inRange = append(inRange, rec)
		}
	}

	result.Records = len(inRange)
	result.Volumes, result.Memory = historySeries(inRange, since, now)

	return result
}

// historyGapAfter is the spacing beyond which two records have a gap between them.
func historyGapAfter(records []historyRecord) int64 {
	if len(records) < 2 {
		return historyMinGap.Milliseconds()
	}

	deltas := make([]int64, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		deltas = append(deltas, records[i].At-records[i-1].At)
	}

	slices.Sort(deltas)

	return max(historyMinGap.Milliseconds(), 3*deltas[len(deltas)/2])
}

// historyTimeline splits [since, now] into the segments of reachable records and the gaps
// (no record, or the cluster unreachable from the phone).
func historyTimeline(records []historyRecord, since, now int64) ([]historySegment, []historySpan) {
	gapAfter := historyGapAfter(records)

	var (
		segments []historySegment
		gaps     []historySpan
	)

	addGap := func(from, to int64) {
		from, to = max(from, since), min(to, now)
		if from >= to {
			return
		}

		if n := len(gaps); n > 0 && gaps[n-1].To >= from {
			gaps[n-1].To = max(gaps[n-1].To, to)

			return
		}

		gaps = append(gaps, historySpan{from, to})
	}

	cursor := since

	for i, rec := range records {
		if rec.At > now {
			break
		}

		end := now
		if i+1 < len(records) && records[i+1].At <= now {
			end = records[i+1].At
		}

		addGap(cursor, rec.At)

		segEnd := end
		if end-rec.At > gapAfter {
			segEnd = rec.At
		}

		if !rec.reachable() {
			addGap(rec.At, segEnd)
		} else if from, to := max(rec.At, since), min(segEnd, now); from < to {
			segments = append(segments, historySegment{rec, from, to})
		}

		cursor = segEnd
	}

	addGap(cursor, now)

	if gaps == nil {
		gaps = []historySpan{}
	}

	return segments, gaps
}

func historyNodeTimelines(segments []historySegment) []historyNodeTimeline {
	byNode := map[string]*historyNodeTimeline{}

	for _, seg := range segments {
		for _, n := range seg.rec.Nodes {
			tl := byNode[n.Node]
			if tl == nil {
				tl = &historyNodeTimeline{Node: n.Node}
				byNode[n.Node] = tl
			}

			if n.Hostname != "" {
				tl.Hostname = n.Hostname
			}

			if k := len(tl.Intervals); k > 0 && tl.Intervals[k-1].State == n.Health && tl.Intervals[k-1].To == seg.from {
				tl.Intervals[k-1].To = seg.to

				continue
			}

			tl.Intervals = append(tl.Intervals, historyInterval{n.Health, seg.from, seg.to})
		}
	}

	out := make([]historyNodeTimeline, 0, len(byNode))

	for _, tl := range byNode {
		var ready, total int64

		for _, iv := range tl.Intervals {
			total += iv.To - iv.From

			if iv.State == historyReady {
				ready += iv.To - iv.From
			}
		}

		if total > 0 {
			p := math.Round(float64(ready)*10000/float64(total)) / 100
			tl.UptimePercent = &p
		}

		out = append(out, *tl)
	}

	slices.SortFunc(out, func(a, b historyNodeTimeline) int { return cmp.Compare(a.Node, b.Node) })

	return out
}

// historyAlertSpans pairs each alert's opening (first record listing it) with its closing
// (first reachable record no longer listing it). Unreachable records say nothing.
func historyAlertSpans(records []historyRecord) []historyAlertSpan {
	open := map[string]int{}

	var out []historyAlertSpan

	for _, rec := range records {
		if !rec.reachable() {
			continue
		}

		present := map[string]bool{}

		for _, a := range rec.Alerts {
			present[a.Key] = true

			if i, ok := open[a.Key]; ok {
				out[i].Severity, out[i].Title = a.Severity, a.Title

				continue
			}

			open[a.Key] = len(out)
			out = append(out, historyAlertSpan{Key: a.Key, Track: a.Track, Severity: a.Severity, Title: a.Title, OpenedAt: rec.At})
		}

		for key, i := range open {
			if !present[key] {
				at := rec.At
				out[i].ClosedAt = &at

				delete(open, key)
			}
		}
	}

	return out
}

func historySeries(records []historyRecord, since, now int64) ([]historyVolumeSeries, []historyMemorySeries) {
	volumes := map[string]*historyVolumeSeries{}
	memory := map[string]*historyMemorySeries{}

	for _, rec := range records {
		if !rec.reachable() {
			continue
		}

		for _, v := range rec.Volumes {
			s := volumes[v.Key]
			if s == nil {
				s = &historyVolumeSeries{Key: v.Key}
				volumes[v.Key] = s
			}

			s.Name, s.Node = v.Name, v.Node
			s.Series = append(s.Series, historyPoint{rec.At, v.UsedPercent})
		}

		for _, n := range rec.Nodes {
			if n.MemUsedPercent == nil {
				continue
			}

			s := memory[n.Node]
			if s == nil {
				s = &historyMemorySeries{Node: n.Node}
				memory[n.Node] = s
			}

			s.Series = append(s.Series, historyPoint{rec.At, *n.MemUsedPercent})
		}
	}

	outV := make([]historyVolumeSeries, 0, len(volumes))
	for _, s := range volumes {
		s.Series = downsampleHistory(s.Series, since, now, historyMaxPoints)
		outV = append(outV, *s)
	}

	outM := make([]historyMemorySeries, 0, len(memory))
	for _, s := range memory {
		s.Series = downsampleHistory(s.Series, since, now, historyMaxPoints)
		outM = append(outM, *s)
	}

	slices.SortFunc(outV, func(a, b historyVolumeSeries) int { return cmp.Compare(a.Key, b.Key) })
	slices.SortFunc(outM, func(a, b historyMemorySeries) int { return cmp.Compare(a.Node, b.Node) })

	return outV, outM
}

// downsampleHistory keeps at most limit points: [from, to] is cut in limit buckets and
// each keeps its highest point, so a short peak still shows.
func downsampleHistory(points []historyPoint, from, to int64, limit int) []historyPoint {
	if len(points) <= limit {
		return points
	}

	span := max(to-from, 1)
	out := make([]historyPoint, 0, limit)
	last := -1

	for _, p := range points {
		b := int(min((p.T-from)*int64(limit)/span, int64(limit-1)))

		switch {
		case b != last:
			out = append(out, p)
			last = b
		case p.V > out[len(out)-1].V:
			out[len(out)-1] = p
		}
	}

	return out
}
