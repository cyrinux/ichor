package ichorgo

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// duplicateConfig is a talosconfig naming lab twice: once as testConfig does and once with
// another endpoint (a file appended to, by hand or by a tool, rather than merged).
func duplicateConfig(t *testing.T, second string) string {
	t.Helper()

	ca, crt, key := testIdentity(t, time.Now().Add(24*time.Hour))

	return fmt.Sprintf(`context: lab
contexts:
  lab:
    endpoints:
      - 10.0.0.1
    ca: %s
    crt: %s
    key: %s
  lab:
    endpoints:
      - %s
    ca: %s
    crt: %s
    key: %s
`, ca, crt, key, second, ca, crt, key)
}

func parseSummary(t *testing.T, yaml string) configSummary {
	t.Helper()

	out, err := ParseConfig(yaml)
	if err != nil {
		t.Fatal(err)
	}

	var s configSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}

	return s
}

func TestDuplicateContextsCollapseWhenIdentical(t *testing.T) {
	s := parseSummary(t, duplicateConfig(t, "10.0.0.1"))

	if len(s.Contexts) != 1 || s.Contexts[0].Name != "lab" || s.Current != "lab" {
		t.Fatalf("got %+v", s)
	}
}

func TestDuplicateContextsKeptApartWhenDifferent(t *testing.T) {
	s := parseSummary(t, duplicateConfig(t, "10.0.0.9"))

	if len(s.Contexts) != 2 || s.Current != "lab" {
		t.Fatalf("got %+v", s)
	}

	// Sorted by name: the first definition keeps the name, the second is lab-1.
	if s.Contexts[0].Name != "lab" || s.Contexts[0].Endpoints[0] != "10.0.0.1" {
		t.Fatalf("first: %+v", s.Contexts[0])
	}

	if s.Contexts[1].Name != "lab-1" || s.Contexts[1].Endpoints[0] != "10.0.0.9" {
		t.Fatalf("second: %+v", s.Contexts[1])
	}
}

func TestDuplicateContextsSkipNamesTheFileUses(t *testing.T) {
	ca, crt, key := testIdentity(t, time.Now().Add(24*time.Hour))
	block := func(endpoint string) string {
		return fmt.Sprintf("    endpoints: [%s]\n    ca: %s\n    crt: %s\n    key: %s\n", endpoint, ca, crt, key)
	}

	cfg := "context: lab\ncontexts:\n  lab:\n" + block("10.0.0.1") + "  lab-1:\n" + block("10.0.0.2") + "  lab:\n" + block("10.0.0.3")

	s := parseSummary(t, cfg)

	names := make([]string, 0, len(s.Contexts))
	for _, c := range s.Contexts {
		names = append(names, c.Name)
	}

	if got := strings.Join(names, ","); got != "lab,lab-1,lab-2" {
		t.Fatalf("got %s", got)
	}

	if s.Contexts[2].Endpoints[0] != "10.0.0.3" {
		t.Fatalf("lab-2 is %+v", s.Contexts[2])
	}
}

// A duplicated context imports like two files would: the clash with the stored one is
// resolved by the user, the renamed one is new.
func TestDuplicateContextsMergeLikeTwoFiles(t *testing.T) {
	stored := testConfig(t, time.Now().Add(24*time.Hour))
	added := duplicateConfig(t, "10.0.0.9")

	out, err := ImportConflicts(stored, added)
	if err != nil {
		t.Fatal(err)
	}

	var conflicts []importConflict
	if err := json.Unmarshal([]byte(out), &conflicts); err != nil {
		t.Fatal(err)
	}

	if len(conflicts) != 1 || conflicts[0].Index != 0 || conflicts[0].Suggested != "lab-2" {
		t.Fatalf("got %+v", conflicts)
	}

	merged, err := MergeConfig(stored, added, "")
	if err != nil {
		t.Fatal(err)
	}

	s := parseSummary(t, merged)

	names := make([]string, 0, len(s.Contexts))
	for _, c := range s.Contexts {
		names = append(names, c.Name)
	}

	if got := strings.Join(names, ","); got != "lab,lab-1,lab-2,other" {
		t.Fatalf("got %s", got)
	}
}

func TestDedupeContextsLeavesOtherTextAlone(t *testing.T) {
	for name, in := range map[string]string{
		"clean":       testConfig(t, time.Now().Add(24*time.Hour)),
		"not yaml":    "{{{",
		"not mapping": "- a\n- b\n",
		"no contexts": "context: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got := dedupeContexts(in); got != in {
				t.Fatalf("changed:\n%s", got)
			}
		})
	}
}
