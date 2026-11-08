package main

import (
	"strings"
	"testing"
)

func TestRunbookRunNeedsYesForDestructiveSteps(t *testing.T) {
	plan := `{"steps":[{"id":"trigger","destructive":false},{"id":"scale-down","destructive":true}]}`

	if err := refuseDestructive(plan, false); err == nil || !strings.Contains(err.Error(), "step scale-down") {
		t.Errorf("without -yes: %v", err)
	}

	if err := refuseDestructive(plan, true); err != nil {
		t.Errorf("with -yes: %v", err)
	}

	if err := refuseDestructive(`{"steps":[{"id":"trigger","destructive":false}]}`, false); err != nil {
		t.Errorf("nothing destructive: %v", err)
	}
}

func TestRunbookCommandsAreListed(t *testing.T) {
	for _, name := range []string{"runbook-plan", "runbook-run"} {
		if c, ok := lookup(name); !ok || c.noConfig {
			t.Errorf("%s: registered %v, noConfig %v", name, ok, c.noConfig)
		}
	}
}
