package expr

import (
	"math"
	"strconv"
	"strings"
)

// Copied from go/ichorgo/kube_quantity.go: this package must not import ichorgo.

// quantitySuffixes are the multipliers of a Kubernetes quantity ("500m", "2Gi", "1G").
var quantitySuffixes = []struct {
	suffix string
	factor float64
}{
	{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40}, {"Pi", 1 << 50}, {"Ei", 1 << 60},
	{"n", 1e-9}, {"u", 1e-6}, {"m", 1e-3}, {"k", 1e3}, {"M", 1e6}, {"G", 1e9}, {"T", 1e12}, {"P", 1e15}, {"E", 1e18},
}

// parseQuantity reads a Kubernetes quantity as a number of its base unit: cores for CPU
// ("250m" is 0.25), bytes for memory and storage ("2Gi"). ok is false when s is empty or
// not a quantity.
func parseQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
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
		return 0, false
	}

	return v * factor, true
}

var byteUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}

// formatBytes is a size in binary units with at most one decimal: "512 B", "1.5 GiB".
func formatBytes(n float64) string {
	sign := ""
	if n < 0 {
		sign, n = "-", -n
	}

	unit := 0
	for n >= 1024 && unit < len(byteUnits)-1 {
		n /= 1024
		unit++
	}

	if unit == 0 {
		return sign + strconv.FormatFloat(math.Round(n), 'f', -1, 64) + " B"
	}

	return sign + strconv.FormatFloat(math.Round(n*10)/10, 'f', -1, 64) + " " + byteUnits[unit]
}
