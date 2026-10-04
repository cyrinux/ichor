package ichorgo

import (
	"strings"
	"testing"
	"time"
)

func TestParseConfigNullContext(t *testing.T) {
	for _, yaml := range []string{
		"context: a\ncontexts:\n  a: ~\n",
		"context: a\ncontexts:\n  a:\n",
	} {
		if _, err := ParseConfig(yaml); err == nil {
			t.Errorf("%q: accepted a null context", yaml)
		}

		if _, err := MergeConfig(yaml, yaml, "{}"); err == nil {
			t.Errorf("%q: merged a null context", yaml)
		}
	}
}

func panicking() (out string, err error) {
	defer maskResult(&out, &err)

	out = "partial"

	var m map[string]*session

	return m["x"].context.Cluster, nil
}

func panickingErr() (err error) {
	defer maskErr(&err)

	panic("boom")
}

func TestHooksRecoverPanics(t *testing.T) {
	out, err := panicking()
	if out != "" || err == nil || !strings.HasPrefix(err.Error(), "internal error:") {
		t.Errorf("maskResult: out=%q err=%v", out, err)
	}

	if err := panickingErr(); err == nil || err.Error() != "internal error: boom" {
		t.Errorf("maskErr: err=%v", err)
	}
}

func TestOnPanicReportsDone(t *testing.T) {
	done := make(chan string, 1)

	go func() {
		defer onPanic(func(msg string) { done <- msg })

		panic("boom")
	}()

	if msg := <-done; msg != "internal error: boom" {
		t.Errorf("done = %q", msg)
	}
}

func TestForEachNodeSurvivesPanic(t *testing.T) {
	got := make([]int, 3)

	forEachNode([]int{1, 2, 3}, func(i, n int) {
		if n == 2 {
			panic("boom")
		}

		got[i] = n
	})

	if got[0] != 1 || got[1] != 0 || got[2] != 3 {
		t.Errorf("got %v", got)
	}
}

func TestLineSplitterCapsEndlessLine(t *testing.T) {
	var lines []string

	l := newLineSplitter(func(s string) { lines = append(lines, s) })

	chunk := []byte(strings.Repeat("x", 1000))
	for range 200 {
		l.write(chunk)
	}

	if len(lines) == 0 {
		t.Fatal("an endless line was buffered whole")
	}

	for _, line := range lines {
		if len(line) > maxLineBytes+len(chunk) {
			t.Fatalf("line of %d bytes", len(line))
		}
	}
}

func TestOutputPathsMustBeAbsolute(t *testing.T) {
	if _, _, err := writeSnapshot(strings.NewReader("x"), "snapshot.db", nil, nil); err == nil {
		t.Error("snapshot: relative path accepted")
	}

	if _, err := writeBundle(t.Context(), "bundle.zip", nil, nil, time.Time{}); err == nil {
		t.Error("bundle: relative path accepted")
	}
}
