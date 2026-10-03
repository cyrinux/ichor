package talosmobile

import (
	"strings"
	"testing"
)

func TestDebugSnippets(t *testing.T) {
	out, err := DebugSnippets()
	if err != nil || !strings.HasPrefix(out, `[{"group":`) {
		t.Fatalf("DebugSnippets() = %.40q, %v", out, err)
	}

	seen := map[string]bool{}
	groups := []string{}

	for _, s := range debugSnippets {
		if s.Group == "" || s.Label == "" || strings.TrimSpace(s.Command) == "" {
			t.Errorf("incomplete snippet %+v", s)
		}

		if key := s.Group + "/" + s.Label; seen[key] {
			t.Errorf("duplicate snippet %s", key)
		} else {
			seen[key] = true
		}

		// Typed snippets end where the argument goes, so the user only types it.
		if !s.Run && !strings.HasSuffix(s.Command, " ") && !strings.HasSuffix(s.Command, "://") {
			t.Errorf("%s: typed snippet %q does not end at its argument", s.Label, s.Command)
		}

		if s.Run && strings.HasSuffix(s.Command, " ") {
			t.Errorf("%s: run snippet %q has a trailing space", s.Label, s.Command)
		}

		if len(groups) == 0 || groups[len(groups)-1] != s.Group {
			groups = append(groups, s.Group)
		}
	}

	// Each group is contiguous, so the apps can section the list in order.
	if want := 11; len(groups) != want {
		t.Errorf("groups %v: want %d contiguous groups", groups, want)
	}
}
