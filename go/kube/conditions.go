package kube

import "time"

// Health of one item a domain lists (a Longhorn volume, a Flux app), worst first.
const (
	HealthCritical = "critical"
	HealthWarning  = "warning"
	HealthOK       = "ok"
	HealthIdle     = "idle" // not in use: a detached volume, a suspended app
)

// HealthRank orders health values for sorting, worst first.
func HealthRank(h string) int {
	switch h {
	case HealthCritical:
		return 0
	case HealthWarning:
		return 1
	case HealthOK:
		return 2
	default:
		return 3
	}
}

// Condition is a status condition as Kubernetes objects and operators report it.
type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

// Conditions is an object's status.conditions.
type Conditions []Condition

// Get is the condition of type typ, zero (Status "") when absent.
func (c Conditions) Get(typ string) Condition {
	for _, x := range c {
		if x.Type == typ {
			return x
		}
	}

	return Condition{}
}

// Is tells whether the condition typ is True.
func (c Conditions) Is(typ string) bool { return c.Get(typ).Status == "True" }

// UnixMilli parses an RFC 3339 time into unix ms, 0 when empty or invalid.
func UnixMilli(s string) int64 {
	if s == "" {
		return 0
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}

	return t.UnixMilli()
}
