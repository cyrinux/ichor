package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// traceRecorder is a TraceListener that keeps every step it receives.
type traceRecorder struct {
	mu    sync.Mutex
	steps []traceStep
	raw   []string
}

func (r *traceRecorder) OnTrace(s string) {
	var step traceStep
	_ = json.Unmarshal([]byte(s), &step) //nolint:errcheck // a broken line fails the test below

	r.mu.Lock()
	defer r.mu.Unlock()

	r.steps = append(r.steps, step)
	r.raw = append(r.raw, s)
}

func (r *traceRecorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.steps))
	for _, s := range r.steps {
		out = append(out, s.Step)
	}

	return out
}

func (r *traceRecorder) step(name string) (traceStep, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range r.steps {
		if s.Step == name {
			return s, true
		}
	}

	return traceStep{}, false
}

// recordTrace registers a recorder for the test and removes it afterwards.
func recordTrace(t *testing.T) *traceRecorder {
	t.Helper()

	r := &traceRecorder{}
	SetTraceListener(r)
	t.Cleanup(func() { SetTraceListener(nil) })

	return r
}

func TestTracedFanoutReportsFirstAndAllNodes(t *testing.T) {
	cases := []struct {
		name            string
		reachable       []bool
		wantSteps       []string
		wantUnreachable int
	}{
		{"all answer", []bool{true, true, true}, []string{"overview.first_node", "overview.all_nodes"}, 0},
		{"one down", []bool{true, false, true}, []string{"overview.first_node", "overview.all_nodes"}, 1},
		{"none answers", []bool{false, false}, []string{"overview.all_nodes"}, 2},
		{"no nodes", nil, []string{"overview.all_nodes"}, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := recordTrace(t)
			nodes := make([]string, len(tc.reachable))

			for i := range nodes {
				nodes[i] = "10.5.0." + string(rune('1'+i))
			}

			tracedFanout(time.Now(), nodes, func(i int, _ string) bool {
				if !tc.reachable[i] {
					time.Sleep(5 * time.Millisecond) // a node that times out answers last
				}

				return tc.reachable[i]
			})

			if got := rec.names(); !slices.Equal(got, tc.wantSteps) {
				t.Fatalf("steps = %v, want %v", got, tc.wantSteps)
			}

			all, _ := rec.step("overview.all_nodes")
			if all.Count != len(nodes) || all.Unreachable != tc.wantUnreachable {
				t.Errorf("all_nodes count=%d unreachable=%d, want %d/%d", all.Count, all.Unreachable, len(nodes), tc.wantUnreachable)
			}

			if all.At == 0 || all.Ms < 0 {
				t.Errorf("all_nodes at=%d ms=%d", all.At, all.Ms)
			}
		})
	}
}

func TestClusterOverviewDemoTracesWithoutSession(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := recordTrace(t)

	if _, err := ClusterOverview(demo, ""); err != nil {
		t.Fatal(err)
	}

	want := []string{"overview.resolve", "overview.all_nodes", "overview.total"}
	if got := rec.names(); !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v (no session step for the demo)", got, want)
	}

	if all, _ := rec.step("overview.all_nodes"); all.Count != len(demoNodes()) {
		t.Errorf("all_nodes count = %d, want %d", all.Count, len(demoNodes()))
	}
}

func TestParseConfigTracesContextCountOnly(t *testing.T) {
	rec := recordTrace(t)

	if _, err := ParseConfig(testConfig(t, time.Now().Add(time.Hour))); err != nil {
		t.Fatal(err)
	}

	step, ok := rec.step("config.parse")
	if !ok || step.Count != 2 {
		t.Fatalf("config.parse = %+v (found %v), want count 2", step, ok)
	}

	for _, raw := range rec.raw {
		if strings.Contains(raw, "lab") || strings.Contains(raw, "other") || strings.Contains(raw, "10.") {
			t.Errorf("trace leaks the context name: %s", raw)
		}
	}
}

func TestParseKubeconfigTracesContextCount(t *testing.T) {
	rec := recordTrace(t)

	if _, err := ParseKubeconfig(singleTokenKubeconfig("https://api.example.test:6443", "t")); err != nil {
		t.Fatal(err)
	}

	if step, ok := rec.step("kubeconfig.parse"); !ok || step.Count != 1 {
		t.Fatalf("kubeconfig.parse = %+v (found %v), want count 1", step, ok)
	}
}

func TestSessionCacheTracesOpenThenReused(t *testing.T) {
	cache, _, _ := newTestCache(time.Minute)
	rec := recordTrace(t)

	for range 2 {
		_, release, err := cache.acquire("cfg", "lab")
		if err != nil {
			t.Fatal(err)
		}

		release()
	}

	if got, want := rec.names(), []string{"session.open", "session.reused"}; !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
}

func TestTraceWithoutListenerDoesNotAllocate(t *testing.T) {
	SetTraceListener(nil)

	start := time.Now()

	if allocs := testing.AllocsPerRun(100, func() {
		trace("overview.total", start)
		traceCounts("overview.all_nodes", start, 8, 1)
	}); allocs != 0 {
		t.Errorf("trace without a listener allocates %v times", allocs)
	}
}

type panickingListener struct{}

func (panickingListener) OnTrace(string) { panic("listener bug") }

func TestTraceSurvivesAPanickingListener(t *testing.T) {
	SetTraceListener(panickingListener{})
	t.Cleanup(func() { SetTraceListener(nil) })

	trace("config.parse", time.Now()) // must not panic
}
