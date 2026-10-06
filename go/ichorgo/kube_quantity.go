package ichorgo

import (
	"math"
	"strconv"
	"strings"
)

// quantitySuffixes are the multipliers of a Kubernetes quantity ("500m", "2Gi", "1G").
var quantitySuffixes = []struct {
	suffix string
	factor float64
}{
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"Pi", 1 << 50}, {"Ei", 1 << 60},
	{"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3}, {"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}, {"P", 1e15}, {"E", 1e18},
}

// parseQuantity reads a Kubernetes quantity as a number of its base unit: cores for CPU
// ("250m" is 0.25), bytes for memory and storage ("2Gi"). 0 when empty or not a quantity.
func parseQuantity(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	factor := 1.0

	for _, q := range quantitySuffixes {
		if strings.HasSuffix(s, q.suffix) {
			s, factor = strings.TrimSuffix(s, q.suffix), q.factor

			break
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}

	return v * factor
}

// percentOf is used out of total in percent with one decimal, 0 when total is unknown.
func percentOf(used, total float64) float64 {
	if total <= 0 {
		return 0
	}

	return math.Round(used/total*1000) / 10
}
