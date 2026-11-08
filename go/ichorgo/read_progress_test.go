package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
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

func readProgressFor(t *testing.T, op string, contextName ...string) readProgressOut {
	t.Helper()

	out, err := ReadProgress("", strings.Join(contextName, ""), op)
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

func TestReadProgressSameContextOverlap(t *testing.T) {
	// The screen reads a cluster; the background monitor checks the same one meanwhile.
	screen := startReadProgress("test-overlap", "cluster-a", "x", "y")
	screen.done("x")

	monitor := startReadProgress("test-overlap", "cluster-a", "x", "y")
	if got := readProgressFor(t, "test-overlap", "cluster-a"); got.Done != 0 || !got.Running {
		t.Errorf("while both run, the newest shows: %+v", got)
	}

	// The monitor ends first: the screen's own read shows again, not a frozen finished one.
	monitor.finish()

	if got := readProgressFor(t, "test-overlap", "cluster-a"); !got.Running || got.Done != 1 {
		t.Errorf("after the monitor ended: %+v", got)
	}

	screen.finish()

	if got := readProgressFor(t, "test-overlap", "cluster-a"); got.Running || got.Done != 1 {
		t.Errorf("after both ended, the last one stays readable: %+v", got)
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

// checkFinishedProgress checks that op's read is over with the steps want, in order, each
// ended (done, or failed when listed in failed).
func checkFinishedProgress(t *testing.T, op string, want []string, failed ...string) {
	t.Helper()

	p := readProgressFor(t, op)
	if p.Running || p.Total != len(want) || p.Done != len(want) {
		t.Fatalf("progress = %+v, want %d steps ended", p, len(want))
	}

	for i, s := range p.Steps {
		wantState := readStepDone
		if slices.Contains(failed, s.ID) {
			wantState = readStepFailed
		}

		if s.ID != want[i] || s.State != wantState {
			t.Errorf("step %d = %s %s, want %s %s", i, s.ID, s.State, want[i], wantState)
		}
	}
}

func TestReadProgressWaitCountsFromTheClock(t *testing.T) {
	p := startReadProgress("test-wait", "", "wait")
	defer p.finish()

	p.wait("wait", time.Now().Add(-2500*time.Millisecond), 5*time.Second)

	got := readProgressFor(t, "test-wait")
	if s := got.Steps[0]; s.State != readStepRunning || s.Count != 2 || s.Total != 5 {
		t.Fatalf("wait step = %+v, want running 2/5", s)
	}

	p.done("wait")

	if s := readProgressFor(t, "test-wait").Steps[0]; s.State != readStepDone || s.Count != 0 {
		t.Fatalf("finished wait = %+v", s)
	}
}
