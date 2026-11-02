package ichorgo

import (
	"math"

	"github.com/cyrinux/ichor/go/internal/quantity"
)

// parseQuantity reads a Kubernetes quantity as a number of its base unit: cores for CPU
// ("250m" is 0.25), bytes for memory and storage ("2Gi"). 0 when empty or not a quantity.
func parseQuantity(s string) float64 {
	v, _ := quantity.Parse(s)
	return v
}

// percentOf is used out of total in percent with one decimal, 0 when total is unknown.
func percentOf(used, total float64) float64 {
	if total <= 0 {
		return 0
	}

	return math.Round(used/total*1000) / 10
}
