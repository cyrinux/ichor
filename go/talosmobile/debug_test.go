package talosmobile

import (
	"strings"
	"testing"
)

func TestDebugArgs(t *testing.T) {
	cases := map[string]string{
		"":                  "/bin/sh",
		"   ":               "/bin/sh",
		"/bin/bash":         "/bin/bash",
		"/bin/sh -c  'top'": "/bin/sh|-c|'top'",
	}

	for in, want := range cases {
		if got := strings.Join(debugArgs(in), "|"); got != want {
			t.Errorf("debugArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateDebugImage(t *testing.T) {
	for _, ok := range []string{"nicolaka/netshoot:latest", "docker.io/library/alpine:3", "ghcr.io/x/y@sha256:abc"} {
		if err := validateDebugImage(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}

	for _, bad := range []string{"", "  ", "has space:1", "-flag"} {
		if err := validateDebugImage(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

type recordingListener struct {
	exits []int
}

func (r *recordingListener) OnStatus(string)           {}
func (r *recordingListener) OnOutput([]byte)           {}
func (r *recordingListener) OnExit(code int, _ string) { r.exits = append(r.exits, code) }

func TestDebugExitReportedOnce(t *testing.T) {
	l := &recordingListener{}
	d := &DebugSession{listener: l}

	d.exit(0, "")
	d.exit(1, "again")

	if len(l.exits) != 1 || l.exits[0] != 0 {
		t.Errorf("exits = %v", l.exits)
	}
}
