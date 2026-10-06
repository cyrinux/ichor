package main

import (
	"strings"
	"testing"
)

func TestCommandsAreNamedOnceAndListed(t *testing.T) {
	seen := map[string]bool{}

	for _, c := range commands() {
		if c.name == "" || c.run == nil || strings.ContainsAny(c.name, " |") {
			t.Errorf("command %q: needs a plain name and a run function", c.name)
		}

		if seen[c.name] {
			t.Errorf("command %q is listed twice", c.name)
		}

		seen[c.name] = true

		if _, ok := lookup(c.name); !ok {
			t.Errorf("command %q is not found by lookup", c.name)
		}

		if !strings.Contains(usage(), c.name) {
			t.Errorf("command %q is missing from the usage", c.name)
		}
	}

	if len(seen) == 0 {
		t.Fatal("no command")
	}
}
