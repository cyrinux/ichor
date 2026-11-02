// Package quantity parses Kubernetes resource quantities used by the core and plugins.
package quantity

import (
	"math"
	"strconv"
	"strings"
)

var suffixes = []struct {
	suffix string
	factor float64
}{
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"Pi", 1 << 50}, {"Ei", 1 << 60},
	{"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3}, {"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}, {"P", 1e15}, {"E", 1e18},
}

// Parse returns a quantity in base units, or false for an empty or invalid input.
func Parse(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	factor := 1.0
	for _, q := range suffixes {
		if strings.HasSuffix(s, q.suffix) {
			s, factor = strings.TrimSuffix(s, q.suffix), q.factor
			break
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v * factor, true
}
