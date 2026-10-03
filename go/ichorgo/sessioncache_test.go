package ichorgo

import (
	"errors"
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
