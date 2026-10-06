package ichorgo

import (
	"errors"
	"slices"
	"strings"
)

// planChecks are the findings of a plan that can refuse its run (an upgrade, a node
// maintenance): the blockers always, the acknowledgments until the user confirmed them.
type planChecks struct {
	blockers    []string
	acknowledge []string
}

// blocked refuses verb for the blockers, minus skip (the ones a force bypasses).
func (c planChecks) blocked(verb string, skip []string) error {
	blockers := slices.DeleteFunc(slices.Clone(c.blockers), func(b string) bool { return slices.Contains(skip, b) })
	if len(blockers) == 0 {
		return nil
	}

	return errors.New(verb + " refused: " + strings.Join(blockers, "; "))
}

// unconfirmed refuses verb for the acknowledgments and the extra risks, unless acknowledged.
func (c planChecks) unconfirmed(verb string, acknowledged bool, extra ...string) error {
	risks := append(slices.Clone(c.acknowledge), extra...)
	if len(risks) == 0 || acknowledged {
		return nil
	}

	return errors.New(verb + " refused until confirmed: " + strings.Join(risks, "; "))
}

// refusal applies both: no blocker, and the acknowledgments confirmed.
func (c planChecks) refusal(verb string, acknowledged bool) error {
	if err := c.blocked(verb, nil); err != nil {
		return err
	}

	return c.unconfirmed(verb, acknowledged)
}

func (p upgradePlan) checks() planChecks { return planChecks{p.Blockers, p.Acknowledge} }

func (p maintenancePlan) checks() planChecks { return planChecks{p.Blockers, p.Acknowledge} }
