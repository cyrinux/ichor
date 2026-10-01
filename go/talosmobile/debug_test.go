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
	exits    []int
	messages []string
}

func (r *recordingListener) OnStatus(message string) { r.messages = append(r.messages, message) }
func (r *recordingListener) OnOutput([]byte)         {}
func (r *recordingListener) OnExit(code int, message string) {
	r.exits = append(r.exits, code)
	r.messages = append(r.messages, message)
}

func TestDebugMessagesMasked(t *testing.T) {
	enableMask(t, "")

	l := &recordingListener{}
	d := &DebugSession{listener: maskedDebugListener{l}}

	d.listener.OnStatus("Starting on 192.168.1.11")
	d.exit(-1, "pull failed: dial 192.168.1.11:50000")

	want := "Starting on 10.0.0.1|pull failed: dial 10.0.0.1:50000"
	if got := strings.Join(l.messages, "|"); got != want {
		t.Errorf("messages = %q, want %q", got, want)
	}
}

func TestDebugExitReportedOnce(t *testing.T) {
	l := &recordingListener{}
	d := &DebugSession{listener: l}

	d.exit(0, "")
	d.exit(1, "again")

	if len(l.exits) != 1 || l.exits[0] != 0 {
		t.Errorf("exits = %v", l.exits)
	}
}
