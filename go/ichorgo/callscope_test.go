package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

const (
	scopeContext  = "admin@test"
	scopeSlowPath = "/api/v1/slow"
	scopeBound    = 2 * time.Second
)

// slowKubeAPI answers scopeSlowPath only once released; started tells a request reached it.
type slowKubeAPI struct {
	*fakeKubeAPI

	config  string
	started chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func newSlowKubeAPI(t *testing.T) *slowKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{"GET " + scopeSlowPath: `{}`, "PATCH " + scopeSlowPath: `{}`})
	s := &slowKubeAPI{fakeKubeAPI: f, config: f.kubeconfigFor(f.URL), started: make(chan struct{}, 8), gate: make(chan struct{})}

	f.beforeAnswer = s.block
	t.Cleanup(s.release)

	return s
}

func (s *slowKubeAPI) block(r *http.Request) {
	if r.URL.Path == scopeSlowPath {
		s.started <- struct{}{}

		<-s.gate
	}
}

func (s *slowKubeAPI) release() { s.once.Do(func() { close(s.gate) }) }

func (s *slowKubeAPI) target() kubeTarget { return kubeTarget{config: s.config, context: scopeContext} }

func (s *slowKubeAPI) waitStarted(t *testing.T) {
	t.Helper()

	select {
	case <-s.started:
	case <-time.After(scopeBound):
		t.Fatal("the call did not reach the API server")
	}
}

// read starts a read of the slow path, its end on the channel.
func (s *slowKubeAPI) read() <-chan error {
	done := make(chan error, 1)

	go func() {
		_, err := withKube(s.target(), func(ctx context.Context, k *kubeClient) (struct{}, error) {
			return struct{}{}, k.get(ctx, scopeSlowPath, nil)
		})
		done <- err
	}()

	return done
}

func endsWith(t *testing.T, done <-chan error, want error, within time.Duration) {
	t.Helper()

	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("ended with %v, want %v", err, want)
		}
	case <-time.After(within):
		t.Fatal("still running")
	}
}

func stillRunning(t *testing.T, done <-chan error) {
	t.Helper()

	select {
	case err := <-done:
		t.Fatalf("ended with %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestCallScopeErrorTextIsPinned(t *testing.T) {
	// GoCall.kt and TalosClient.swift match this exact text.
	if got := errCallsCancelled.Error(); got != "cancelled: the app left this cluster" {
		t.Fatalf("text %q", got)
	}
}

func TestCallScopeCancelEndsAReadAtOnce(t *testing.T) {
	s := newSlowKubeAPI(t)
	done := s.read()

	s.waitStarted(t)

	if err := CancelCalls(s.config, scopeContext); err != nil {
		t.Fatal(err)
	}

	endsWith(t, done, errCallsCancelled, 500*time.Millisecond)

	// A fresh scope: the next read goes through.
	s.release()
	endsWith(t, s.read(), nil, scopeBound)
}

func TestCallScopeCancelEndsAColdOpenWait(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	gate := make(chan struct{})

	t.Cleanup(func() { close(gate) })

	// The probe of a new client does not answer: an address that went away.
	f.beforeAnswer = func(*http.Request) { <-gate }
	config := f.kubeconfigFor(f.URL)

	done := make(chan error, 1)

	go func() {
		_, err := withKube(kubeTarget{config: config, context: scopeContext}, func(context.Context, *kubeClient) (struct{}, error) {
			return struct{}{}, nil
		})
		done <- err
	}()

	// callParent is taken on the way in: wait for the read to be under way.
	time.Sleep(50 * time.Millisecond)
	cancelCalls(config, scopeContext)

	endsWith(t, done, errCallsCancelled, 500*time.Millisecond)
}

func TestCallScopeReadThatSwallowedTheCancelStillEnds(t *testing.T) {
	s := newSlowKubeAPI(t)

	// An overview answers with its nodes down and no error, whatever failed.
	out, err := withKube(s.target(), func(context.Context, *kubeClient) (string, error) {
		cancelCalls(s.config, scopeContext)

		return "every node down", nil
	})
	if !errors.Is(err, errCallsCancelled) || out != "" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestCallScopeMutationIsNotCut(t *testing.T) {
	s := newSlowKubeAPI(t)
	done := make(chan error, 1)

	go func() {
		done <- kubeMutate(s.target(), func(ctx context.Context, k *kubeClient) error {
			return k.get(ctx, scopeSlowPath, nil)
		})
	}()

	s.waitStarted(t)
	cancelCalls(s.config, scopeContext)
	stillRunning(t, done)

	s.release()
	endsWith(t, done, nil, scopeBound)
}

func TestCallScopeRunHoldsTheScope(t *testing.T) {
	fastFluxFollow(t, time.Minute)

	s := newSlowKubeAPI(t)
	s.answerWith("GET /apis", http.StatusOK, fluxFollowGroups)
	s.answerWith("GET "+fluxFollowPath, http.StatusOK,
		fluxFollowKustomization("2026-01-01T00:00:00Z", "Unknown", "Reconciliation in progress"))

	rec := newFollowRecorder()
	run := StartFluxReconcileFollow(s.config, scopeContext, "", "Kustomization", "flux-system", "shop", rec)

	waitHeld(t, s.config, true)

	// While the run goes on, the cluster's reads are left alone.
	done := s.read()

	s.waitStarted(t)

	if err := CancelCalls(s.config, scopeContext); err != nil {
		t.Fatal(err)
	}

	stillRunning(t, done)

	if len(rec.done) != 0 {
		t.Fatal("the run ended on the cancel")
	}

	s.release()
	endsWith(t, done, nil, scopeBound)

	run.Cancel()
	rec.wait(t)
	waitHeld(t, s.config, false)

	// The run over, the next cancel cuts what is under the scope.
	parent := callParent(s.config, scopeContext)

	if !cancelCalls(s.config, scopeContext) || parent.Err() == nil {
		t.Fatal("still held after the run")
	}
}

// waitHeld waits for the scope's hold, taken and dropped in the run's goroutine.
func waitHeld(t *testing.T, config string, held bool) {
	t.Helper()

	for deadline := time.Now().Add(scopeBound); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		callScopesMu.Lock()
		holds := 0
		if scope, ok := callScopes[cacheKey(config, scopeContext)]; ok {
			holds = scope.holds
		}
		callScopesMu.Unlock()

		if (holds > 0) == held {
			return
		}
	}

	t.Fatalf("held never became %t", held)
}

func TestCallScopeReleaseIsIdempotent(t *testing.T) {
	const config = "config-release"

	first, second := holdCalls(config, "c"), holdCalls(config, "c")

	first()
	first()

	if cancelCalls(config, "c") {
		t.Fatal("a second release of one hold dropped the other")
	}

	second()

	if !cancelCalls(config, "c") {
		t.Fatal("still held")
	}
}

func TestCallScopeIsPerCluster(t *testing.T) {
	const config = "config-two-clusters"

	one, other := callParent(config, "one"), callParent(config, "other")

	cancelCalls(config, "one")

	if one.Err() == nil || other.Err() != nil {
		t.Fatalf("one %v, other %v", one.Err(), other.Err())
	}

	if callParent(config, "one").Err() != nil {
		t.Fatal("no fresh scope after the cancel")
	}

	cancelCalls(config, "other")
}

func TestCallScopeConcurrent(t *testing.T) {
	const config = "config-concurrent"

	var wg sync.WaitGroup

	for i := range 16 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 200 {
				switch i % 3 {
				case 0:
					_ = callParent(config, "c").Err()
				case 1:
					holdCalls(config, "c")()
				default:
					cancelCalls(config, "c")
				}
			}
		}()
	}

	wg.Wait()

	if !cancelCalls(config, "c") {
		t.Fatal("a hold was left behind")
	}
}

func TestCallScopeDemoContext(t *testing.T) {
	config := demoConfigForTest(t)

	if err := CancelCalls(config, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := ClusterOverview(config, ""); err != nil {
		t.Fatalf("demo read after a cancel: %v", err)
	}
}

func TestCallScopeErrorIsNotMasked(t *testing.T) {
	err := errCallsCancelled

	maskErr(&err)

	if !errors.Is(err, errCallsCancelled) {
		t.Fatalf("got %v", err)
	}
}
