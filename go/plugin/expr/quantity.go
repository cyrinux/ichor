package expr

import (
	"math"
	"strconv"

	"github.com/cyrinux/ichor/go/internal/quantity"
)

// parseQuantity reads a Kubernetes quantity as a number of its base unit: cores for CPU
// ("250m" is 0.25), bytes for memory and storage ("2Gi"). ok is false when s is empty or
// not a quantity.
func parseQuantity(s string) (float64, bool) {
	return quantity.Parse(s)
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
