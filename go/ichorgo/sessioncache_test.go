package ichorgo

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache(idle time.Duration) (*sessionCache, *atomic.Int32, *atomic.Int32) {
	var opened, closed atomic.Int32

	cache := newSessionCache(idle, func(configYAML, contextName string) (*session, error) {
		if contextName == "broken" {
			return nil, errors.New("boom")
		}

		opened.Add(1)

		return &session{onClose: func() { closed.Add(1) }}, nil
	})

	return cache, &opened, &closed
}

func TestSessionCacheReusesPerConfigAndContext(t *testing.T) {
	cache, opened, _ := newTestCache(time.Minute)

	a, releaseA, err := cache.acquire("cfg", "lab")
	if err != nil {
		t.Fatal(err)
	}
	releaseA()

	b, releaseB, _ := cache.acquire("cfg", "lab")
	releaseB()

	if a != b || opened.Load() != 1 {
		t.Fatalf("expected reuse, opened=%d", opened.Load())
	}

	_, releaseC, _ := cache.acquire("cfg", "other")
	releaseC()

	_, releaseD, _ := cache.acquire("cfg2", "lab")
	releaseD()

	if opened.Load() != 3 {
		t.Fatalf("different context/config must not share sessions, opened=%d", opened.Load())
	}
}

func TestSessionCacheClosesIdleButNotInUse(t *testing.T) {
	cache, opened, closed := newTestCache(20 * time.Millisecond)

	_, release, _ := cache.acquire("cfg", "lab")
	time.Sleep(60 * time.Millisecond)

	if closed.Load() != 0 {
		t.Fatal("closed a session still in use")
	}

	release()
	time.Sleep(60 * time.Millisecond)

	if closed.Load() != 1 {
		t.Fatalf("idle session not closed, closed=%d", closed.Load())
	}

	_, release2, _ := cache.acquire("cfg", "lab")
	release2()

	if opened.Load() != 2 {
		t.Fatalf("expected a fresh session after idle close, opened=%d", opened.Load())
	}
}

func TestSessionCacheOpenError(t *testing.T) {
	cache, _, _ := newTestCache(time.Minute)

	if _, _, err := cache.acquire("cfg", "broken"); err == nil {
		t.Fatal("expected error")
	}
}

// newBlockingCache opens "slow" only once gate is closed (failing if fail is set), and
// counts the opens per context.
func newBlockingCache(gate <-chan struct{}, fail bool) (*sessionCache, *sync.Map) {
	var opens sync.Map

	cache := newSessionCache(time.Minute, func(configYAML, contextName string) (*session, error) {
		n, _ := opens.LoadOrStore(contextName, new(atomic.Int32))
		n.(*atomic.Int32).Add(1)

		if contextName == "slow" {
			<-gate

			if fail {
				return nil, errors.New("sign-in refused")
			}
		}

		return &session{}, nil
	})

	return cache, &opens
}

func opensOf(opens *sync.Map, contextName string) int32 {
	n, ok := opens.Load(contextName)
	if !ok {
		return 0
	}

	return n.(*atomic.Int32).Load()
}

// waitersOf is the number of acquires waiting on the open of (cfg, contextName), -1 for none.
func waitersOf(cache *sessionCache, contextName string) int {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	op := cache.opening[cacheKey("cfg", contextName)]
	if op == nil {
		return -1
	}

	return op.waiters
}

func TestSessionCacheBlockedOpenDoesNotBlockOtherKeys(t *testing.T) {
	gate := make(chan struct{})
	defer close(gate)

	cache, _ := newBlockingCache(gate, false)

	go func() {
		if _, release, err := cache.acquire("cfg", "slow"); err == nil {
			release()
		}
	}()

	waitFor(t, "the slow open", func() bool { return waitersOf(cache, "slow") == 0 })

	done := make(chan error, 1)

	go func() {
		_, release, err := cache.acquire("cfg", "fast")
		if err == nil {
			release()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire of another context waited for a blocked open")
	}
}

func TestSessionCacheSameKeyWaitersShareOneOpen(t *testing.T) {
	gate := make(chan struct{})
	cache, opens := newBlockingCache(gate, false)

	const callers = 5

	type acquired struct {
		s       *session
		release func()
	}

	results := make(chan acquired, callers)

	for range callers {
		go func() {
			s, release, err := cache.acquire("cfg", "slow")
			if err != nil {
				t.Error(err)
			}
			results <- acquired{s, release}
		}()
	}

	waitFor(t, "the waiters", func() bool { return waitersOf(cache, "slow") == callers-1 })
	close(gate)

	got := make([]acquired, 0, callers)
	for range callers {
		got = append(got, <-results)
	}

	for _, a := range got {
		if a.s == nil || a.s != got[0].s {
			t.Fatal("waiters got different sessions")
		}
	}

	if n := opensOf(opens, "slow"); n != 1 {
		t.Fatalf("opened %d times, want 1", n)
	}

	cache.mu.Lock()
	inUse := cache.entries[cacheKey("cfg", "slow")].inUse
	cache.mu.Unlock()

	if inUse != callers {
		t.Fatalf("in use by %d, want %d", inUse, callers)
	}

	for _, a := range got {
		a.release()
	}
}

func TestSessionCacheFailedOpenReleasesWaiters(t *testing.T) {
	gate := make(chan struct{})
	cache, opens := newBlockingCache(gate, true)

	const callers = 3

	errs := make(chan error, callers)

	for range callers {
		go func() {
			_, _, err := cache.acquire("cfg", "slow")
			errs <- err
		}()
	}

	waitFor(t, "the waiters", func() bool { return waitersOf(cache, "slow") == callers-1 })
	close(gate)

	for range callers {
		if err := <-errs; err == nil || err.Error() != "sign-in refused" {
			t.Fatalf("err = %v, want the open's error", err)
		}
	}

	if n := opensOf(opens, "slow"); n != 1 {
		t.Fatalf("opened %d times, want 1", n)
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()

	if len(cache.entries) != 0 || len(cache.opening) != 0 {
		t.Fatal("a failed open left an entry")
	}
}

func TestSessionCacheForgetDuringOpenIsNotKept(t *testing.T) {
	gate := make(chan struct{})

	var closed atomic.Int32

	cache := newSessionCache(time.Minute, func(configYAML, contextName string) (*session, error) {
		<-gate

		return &session{authKey: "acme", onClose: func() { closed.Add(1) }}, nil
	})

	got := make(chan func(), 1)

	go func() {
		_, release, err := cache.acquire("cfg", "omni")
		if err != nil {
			t.Error(err)
		}
		got <- release
	}()

	waitFor(t, "the open", func() bool { return waitersOf(cache, "omni") == 0 })
	cache.forgetAuth("acme")
	close(gate)

	if release := <-got; release != nil {
		release()
	}

	cache.mu.Lock()
	kept := len(cache.entries)
	cache.mu.Unlock()

	if kept != 0 || closed.Load() != 1 {
		t.Fatalf("kept=%d closed=%d: a session of a forgotten sign-in must close on release", kept, closed.Load())
	}
}
