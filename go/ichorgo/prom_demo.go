package ichorgo

import (
	"hash/fnv"
	"math"
	"strings"
)

func demoPromDiscovery() promDiscovery {
	return promDiscovery{Sources: []promSource{
		{Mode: promModeProxy, Kind: "prometheus", Namespace: "monitoring", Service: "prometheus-operated", Port: 9090},
	}}
}

// demoPromResult makes up gently moving series for any query: one per namespace or node
// when it groups by one, else a single series, scaled from what the query reads.
func demoPromResult(query string, grid promGrid) promResult {
	h := fnv.New32a()
	h.Write([]byte(query)) //nolint:errcheck
	seed := float64(h.Sum32()%1000) / 1000

	var labels []map[string]string

	switch {
	case strings.Contains(query, "namespace"):
		for _, ns := range []string{"kube-system", "monitoring", "argocd", "default"} {
			labels = append(labels, map[string]string{"namespace": ns})
		}
	case strings.Contains(query, "instance"):
		for _, n := range demoNodes() {
			labels = append(labels, map[string]string{"instance": n.Node + ":9100"})
		}
	default:
		labels = []map[string]string{{}}
	}

	base := 1.5
	switch {
	case strings.HasPrefix(query, "100"):
		base = 40
	case strings.Contains(query, "bytes"):
		base = 2 << 30
	}

	res := promResult{ResultType: "matrix", Times: grid.times(), Series: []promSeries{}, Warnings: []string{}, Total: len(labels)}
	if grid.n == 1 {
		res.ResultType = "vector"
	}

	for i, l := range labels {
		scale := base * (1 - 0.15*float64(i)) * (0.8 + 0.4*seed)
		values := make([]*float64, grid.n)

		for j, t := range res.Times {
			v := scale * (1 + 0.15*math.Sin(float64(t)/1000/900+seed*6+float64(i)))
			values[j] = &v
		}

		res.Series = append(res.Series, promSeries{Name: promSeriesName(l), Labels: l, Values: values})
	}

	return res
}
