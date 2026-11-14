package ichorgo

import (
	"cmp"
	"slices"
)

// Memory trend: the storage forecast's least-squares line through each node's memory use, a
// leak detector from the history ring already on the phone.

// memoryCriticalPercent is the memory use a node's projection counts to by default: where
// kubelet's memory pressure and evictions usually start.
const memoryCriticalPercent = 90.0

type historyMemoryForecastResult struct {
	Nodes []historyMemoryForecast `json:"nodes"`
}

type historyMemoryForecast struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	historyTrend
}

// HistoryMemoryForecast projects when each node's memory use reaches criticalPercent (<= 0
// takes 90) and 100 %, as HistoryVolumeForecast does for volumes: the same 7-day window, the
// same restart after a fall of more than 10 points (a reboot, a large eviction) and the same
// gates, so a sawtooth node (caches, garbage collection) stays "low" and carries its slope
// only. One row per node with a memory sample in the window, sorted by node.
func HistoryMemoryForecast(ringBytes []byte, nowMillis int64, criticalPercent float64) (out string, err error) {
	defer maskResult(&out, &err)

	ring, err := decodeHistoryRing(ringBytes)
	if err != nil {
		return "", err
	}

	if criticalPercent <= 0 {
		criticalPercent = memoryCriticalPercent
	}

	return toJSON(forecastMemory(ring.records, nowMillis, criticalPercent))
}

func forecastMemory(records []historyRecord, now int64, crit float64) historyMemoryForecastResult {
	res := historyMemoryForecastResult{Nodes: []historyMemoryForecast{}}
	since := now - forecastWindow.Milliseconds()

	series := map[string][]historyPoint{}
	hostnames := map[string]string{}

	for _, rec := range records {
		if !rec.reachable() || rec.At < since || rec.At > now {
			continue
		}

		for _, n := range rec.Nodes {
			if n.MemUsedPercent == nil {
				continue
			}

			series[n.Node] = append(series[n.Node], historyPoint{rec.At, *n.MemUsedPercent})
			if n.Hostname != "" {
				hostnames[n.Node] = n.Hostname
			}
		}
	}

	for node, points := range series {
		res.Nodes = append(res.Nodes, historyMemoryForecast{
			Node: node, Hostname: hostnames[node], historyTrend: forecastSeries(afterCleanup(points), crit),
		})
	}

	slices.SortFunc(res.Nodes, func(a, b historyMemoryForecast) int { return cmp.Compare(a.Node, b.Node) })

	return res
}
