package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type driftChange struct {
	Node, Reference, NodeName, ReferenceName, Key, Before, After string
	Added, Removed                                               []string
	Changed                                                      []struct{ Name, Before, After string }
}

// formatDrift prints one block per change of a CompareDrift result: the itemised
// differences of a list-valued setting, or before → after for a scalar.
func formatDrift(raw string) (string, error) {
	var changes []driftChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		return "", err
	}
	if len(changes) == 0 {
		return "no drift", nil
	}
	var b strings.Builder
	for _, c := range changes {
		fmt.Fprintf(&b, "%s (%s) · %s, reference %s (%s)\n", c.NodeName, c.Node, c.Key, c.ReferenceName, c.Reference)
		if len(c.Added)+len(c.Removed)+len(c.Changed) == 0 {
			fmt.Fprintf(&b, "  %s → %s\n", c.Before, c.After)
			continue
		}
		for _, item := range c.Added {
			fmt.Fprintf(&b, "  + %s\n", item)
		}
		for _, item := range c.Removed {
			fmt.Fprintf(&b, "  − %s\n", item)
		}
		for _, p := range c.Changed {
			fmt.Fprintf(&b, "  ~ %s %s → %s\n", p.Name, p.Before, p.After)
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
