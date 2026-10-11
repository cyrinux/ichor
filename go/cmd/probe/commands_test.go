package main

import (
	"flag"
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

func TestHoistFlagsMovesFlagsAfterTheCommand(t *testing.T) {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.Bool("timing", false, "")
	fs.String("context", "", "")

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"overview"}, "overview"},
		{[]string{"-context", "lab", "overview"}, "-context lab overview"},
		{[]string{"overview", "-timing", "-context", "lab"}, "-timing -context lab overview"},
		{[]string{"overview", "-context=lab", "-timing"}, "-context=lab -timing overview"},
		{[]string{"logs", "10.5.0.2", "-n", "kubelet"}, "logs 10.5.0.2 -n kubelet"},
	}

	for _, tc := range cases {
		if got := strings.Join(hoistFlags(fs, tc.args), " "); got != tc.want {
			t.Errorf("hoistFlags(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestFormatTrace(t *testing.T) {
	cases := map[string]string{
		`{"step":"config.parse","ms":12,"at":1,"count":23}`:                        "trace config.parse 12 ms count=23",
		`{"step":"overview.all_nodes","ms":8012,"at":1,"count":8,"unreachable":1}`: "trace overview.all_nodes 8012 ms count=8 unreachable=1",
		`{"step":"session.reused","ms":0,"at":1}`:                                  "trace session.reused 0 ms",
		`not json`: "trace: not json",
	}

	for raw, want := range cases {
		if got := formatTrace(raw); got != want {
			t.Errorf("formatTrace(%s) = %q, want %q", raw, got, want)
		}
	}
}
