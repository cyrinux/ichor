package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"
)

// Read progress: a screen whose one call makes many reads (the checkup) is slow on a large
// cluster, so the apps poll ReadProgress while the call runs and show its steps. A read is
// keyed by its op and context: the background monitor's checkup of another cluster does not
// take the screen's place. Reads of the same op and context may overlap (a refresh while the
// first still runs, the monitor checking the cluster on screen): ReadProgress shows the newest
// one still running, and a finished one leaves the list, so the screen never stays on a read
// that ended while its own goes on. The last one to finish stays, readable as finished.

// Lock order: readProgresses.mu, then a tracker's mu; never the other way round.

// The states of a step.
const (
	readStepPending = "pending"
	readStepRunning = "running"
	readStepDone    = "done"
	readStepFailed  = "failed"
)

// readProgresses holds, per key, the reads started there, oldest first.
var readProgresses = struct {
	mu sync.Mutex
	op map[string][]*readProgress
}{op: map[string][]*readProgress{}}

// readProgress tracks one read; its methods do nothing on a nil tracker, so code that is
// also called without one (tests, other screens) needs no check.
type readProgress struct {
	key string

	mu      sync.Mutex
	running bool
	steps   []readStep
}

func (p *readProgress) isRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.running
}

// readStep is one step as ReadProgress shows it. Its ID is a fixed word the apps translate,
// never an object or node name: the privacy mask has nothing to learn from it.
type readStep struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Count int    `json:"count,omitempty"`
	Total int    `json:"total,omitempty"`
	MS    int64  `json:"ms,omitempty"` // how long a finished step took

	started time.Time
}

type readProgressJSON struct {
	Running bool       `json:"running"`
	Done    int        `json:"done"`
	Total   int        `json:"total"`
	Steps   []readStep `json:"steps"`
}

// readProgressKey is where the read of op on contextName (unmasked) is kept.
func readProgressKey(op, contextName string) string { return op + "\x00" + contextName }

// startReadProgress starts the count of op's read on contextName (unmasked) with its steps,
// in the order they run.
func startReadProgress(op, contextName string, steps ...string) *readProgress {
	key := readProgressKey(op, contextName)
	p := &readProgress{key: key, running: true, steps: make([]readStep, len(steps))}

	for i, id := range steps {
		p.steps[i] = readStep{ID: id, State: readStepPending}
	}

	readProgresses.mu.Lock()
	defer readProgresses.mu.Unlock()

	// The finished one kept for reading is replaced by this read.
	running := slices.DeleteFunc(readProgresses.op[key], func(r *readProgress) bool { return !r.isRunning() })
	readProgresses.op[key] = append(running, p)

	return p
}

// update changes step id under the lock, if the tracker and the step exist.
func (p *readProgress) update(id string, change func(s *readStep)) {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.steps {
		if p.steps[i].ID == id {
			change(&p.steps[i])

			return
		}
	}
}

// begin marks a step running.
func (p *readProgress) begin(id string) {
	p.update(id, func(s *readStep) { s.State, s.started = readStepRunning, time.Now() })
}

// done marks a step finished.
func (p *readProgress) done(id string) { p.end(id, readStepDone) }

// fail marks a step that could not be read.
func (p *readProgress) fail(id string) { p.end(id, readStepFailed) }

func (p *readProgress) end(id, state string) {
	p.update(id, func(s *readStep) {
		s.State = state
		if !s.started.IsZero() {
			s.MS = time.Since(s.started).Milliseconds()
		}
	})
}

// count is how far a step's fan-out got: done of total (nodes asked, say).
func (p *readProgress) count(id string, done, total int) {
	p.update(id, func(s *readStep) {
		if s.State == readStepPending {
			s.State, s.started = readStepRunning, time.Now()
		}

		s.Count, s.Total = done, total
	})
}

// track begins a step and returns the func that ends it, failed or done.
func (p *readProgress) track(id string) func(failed bool) {
	p.begin(id)

	return func(failed bool) {
		if failed {
			p.fail(id)
		} else {
			p.done(id)
		}
	}
}

// finish marks the read over (call it with defer).
func (p *readProgress) finish() {
	if p == nil {
		return
	}

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()

	readProgresses.mu.Lock()
	defer readProgresses.mu.Unlock()

	// Out of the list, unless no other read is left there: then it stays, readable as
	// finished. One still running elsewhere in the list shows from now on.
	if reads := readProgresses.op[p.key]; len(reads) > 1 {
		readProgresses.op[p.key] = slices.DeleteFunc(reads, func(r *readProgress) bool { return r == p })
	}
}

// shownReadProgress is what ReadProgress shows for key: the newest read still running, else
// the one kept after the last finished. Called with readProgresses.mu held.
func shownReadProgress(key string) *readProgress {
	reads := readProgresses.op[key]

	for i := len(reads) - 1; i >= 0; i-- {
		if reads[i].isRunning() {
			return reads[i]
		}
	}

	if len(reads) > 0 {
		return reads[len(reads)-1]
	}

	return nil
}

func (p *readProgress) snapshot() readProgressJSON {
	out := readProgressJSON{Steps: []readStep{}}
	if p == nil {
		return out
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	out.Running, out.Total, out.Steps = p.running, len(p.steps), append(out.Steps, p.steps...)

	for _, s := range p.steps {
		if s.State == readStepDone || s.State == readStepFailed {
			out.Done++
		}
	}

	return out
}

type readProgressCtxKey struct{}

// withReadProgress carries p down to the readers of a call.
func withReadProgress(ctx context.Context, p *readProgress) context.Context {
	return context.WithValue(ctx, readProgressCtxKey{}, p)
}

// readProgressFrom is the tracker ctx carries, nil when none.
func readProgressFrom(ctx context.Context) *readProgress {
	p, _ := ctx.Value(readProgressCtxKey{}).(*readProgress)

	return p
}

// ReadProgress is, as JSON, how far the running read of op on contextName got (op "checkup":
// KubeCheckup): {"running":true,"done":5,"total":17,"steps":[{"id":"api","state":"done","ms":120},
// {"id":"storage","state":"running","count":12,"total":40},{"id":"helm","state":"pending"}]}.
// state is pending, running, done or failed; count/total follow a step's fan-out. An op that
// never ran there answers {"running":false,"done":0,"total":0,"steps":[]}.
func ReadProgress(configYAML, contextName, op string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	readProgresses.mu.Lock()
	p := shownReadProgress(readProgressKey(op, contextName))
	readProgresses.mu.Unlock()

	raw, err := json.Marshal(p.snapshot())

	return string(raw), err
}
