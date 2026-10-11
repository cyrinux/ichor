package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type readProgressOut struct {
	Running     bool
	Done, Total int
	Steps       []struct {
		ID, State    string
		Count, Total int
		MS           int64
	}
}

func readProgressFor(t *testing.T, op string) readProgressOut {
	t.Helper()

	out, err := ReadProgress("", "", op)
	if err != nil {
		t.Fatal(err)
	}

	var p readProgressOut
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("%s: %v", out, err)
	}

	return p
}

func TestReadProgressSteps(t *testing.T) {
	p := startReadProgress("test-steps", "", "a", "b", "c", "d")

	p.begin("a")
	p.done("a")
	p.begin("b")
	p.count("b", 3, 8)
	p.fail("c")

	got := readProgressFor(t, "test-steps")
	if !got.Running || got.Done != 2 || got.Total != 4 || len(got.Steps) != 4 {
		t.Fatalf("progress = %+v", got)
	}

	want := []struct {
		id, state    string
		count, total int
	}{{"a", readStepDone, 0, 0}, {"b", readStepRunning, 3, 8}, {"c", readStepFailed, 0, 0}, {"d", readStepPending, 0, 0}}
	for i, w := range want {
		s := got.Steps[i]
		if s.ID != w.id || s.State != w.state || s.Count != w.count || s.Total != w.total {
			t.Errorf("step %d = %+v, want %+v", i, s, w)
		}
	}

	// Unknown steps are ignored, a finished read is no longer running.
	p.done("nope")
	p.finish()

	if got := readProgressFor(t, "test-steps"); got.Running || got.Done != 2 {
		t.Errorf("after finish: %+v", got)
	}
}

func TestReadProgressRestartAndUnknown(t *testing.T) {
	old := startReadProgress("test-restart", "", "a", "b")
	old.done("a")

	fresh := startReadProgress("test-restart", "", "a", "b")

	// The first read, still running somewhere, no longer moves the counts.
	old.done("b")
	old.finish()

	if got := readProgressFor(t, "test-restart"); !got.Running || got.Done != 0 {
		t.Errorf("restart: %+v", got)
	}

	fresh.finish()

	got := readProgressFor(t, "never-started")
	if got.Running || got.Done != 0 || got.Total != 0 || got.Steps == nil || len(got.Steps) != 0 {
		t.Errorf("unknown op: %+v", got)
	}

	out, _ := ReadProgress("", "", "never-started")
	if out != `{"running":false,"done":0,"total":0,"steps":[]}` {
		t.Errorf("unknown op JSON = %s", out)
	}
}

func TestReadProgressPerContext(t *testing.T) {
	// The screen reads cluster a while the background monitor checks cluster b.
	screen := startReadProgress("test-per-ctx", "cluster-a", "x", "y")
	monitor := startReadProgress("test-per-ctx", "cluster-b", "x", "y")

	screen.done("x")
	monitor.finish()

	a, _ := ReadProgress("", "cluster-a", "test-per-ctx")
	b, _ := ReadProgress("", "cluster-b", "test-per-ctx")

	if !strings.Contains(a, `"running":true`) || !strings.Contains(a, `"done":1`) {
		t.Errorf("cluster a = %s", a)
	}

	if !strings.Contains(b, `"running":false`) || !strings.Contains(b, `"done":0`) {
		t.Errorf("cluster b = %s", b)
	}
}

func TestReadProgressInContext(t *testing.T) {
	// No tracker in the context: every call is a no-op.
	readProgressFrom(context.Background()).begin("x")
	readProgressFrom(context.Background()).finish()

	p := startReadProgress("test-ctx", "", "x")
	ctx := withReadProgress(context.Background(), p)

	end := readProgressFrom(ctx).track("x")
	end(false)
	p.finish()

	if got := readProgressFor(t, "test-ctx"); got.Steps[0].State != readStepDone {
		t.Errorf("tracked step = %+v", got.Steps[0])
	}
}
