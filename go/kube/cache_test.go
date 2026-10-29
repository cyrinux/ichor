package kube

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testClient struct{ closed atomic.Bool }

type testCache struct {
	*ClientCache[string, *testClient]
	opens atomic.Int32
}

func newTestCache(ttl time.Duration, open func(string) (*testClient, error)) *testCache {
	c := &testCache{}
	counted := func(target string) (*testClient, error) {
		c.opens.Add(1)

		return open(target)
	}
	c.ClientCache = NewClientCache(counted, func(t string) string { return t }, func(k *testClient) { k.closed.Store(true) }, ttl)

	return c
}

func TestClientCacheOpensOncePerTarget(t *testing.T) {
	release := make(chan struct{})
	c := newTestCache(time.Hour, func(string) (*testClient, error) {
		<-release

		return &testClient{}, nil
	})

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		got    []*testClient
		freshN int
	)

	for range 8 {
		wg.Go(func() {
			k, fresh, err := c.Get("a")
			if err != nil {
				t.Error(err)
			}

			mu.Lock()
			got = append(got, k)
			if fresh {
				freshN++
			}
			mu.Unlock()
		})
	}

	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if c.opens.Load() != 1 || freshN != 1 {
		t.Fatalf("opens = %d, fresh = %d, want 1 and 1", c.opens.Load(), freshN)
	}

	for _, k := range got {
		if k != got[0] {
			t.Fatal("callers got different clients")
		}
	}
}

func TestClientCacheForget(t *testing.T) {
	c := newTestCache(time.Hour, func(string) (*testClient, error) { return &testClient{}, nil })

	first, _, _ := c.Get("a")
	c.Forget("a", &testClient{}) // another client: the cached one stays

	if again, fresh, _ := c.Get("a"); again != first || fresh || first.closed.Load() {
		t.Fatal("a stale Forget dropped the cached client")
	}

	c.Forget("a", first)

	if !first.closed.Load() {
		t.Fatal("the forgotten client is not closed")
	}

	if second, fresh, _ := c.Get("a"); second == first || !fresh {
		t.Fatal("Forget did not drop the client")
	}

	b, _, _ := c.Get("b")
	c.ForgetWhere(func(target string) bool { return target == "b" })

	if !b.closed.Load() {
		t.Fatal("ForgetWhere did not close b")
	}

	if c.opens.Load() != 3 {
		t.Fatalf("opens = %d, want 3", c.opens.Load())
	}
}

func TestClientCacheExpires(t *testing.T) {
	c := newTestCache(time.Nanosecond, func(string) (*testClient, error) { return &testClient{}, nil })

	first, _, _ := c.Get("a")
	time.Sleep(time.Millisecond)

	if second, fresh, _ := c.Get("a"); second == first || !fresh || !first.closed.Load() {
		t.Fatal("an expired client was reused or not closed")
	}
}

func TestClientCacheErrors(t *testing.T) {
	boom := errors.New("no kubeconfig")
	c := newTestCache(time.Hour, func(target string) (*testClient, error) {
		switch target {
		case "panic":
			panic("open failed")
		case "nil":
			return nil, nil
		default:
			return nil, boom
		}
	})

	if _, _, err := c.Get("err"); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}

	if _, _, err := c.Get("nil"); err == nil {
		t.Fatal("a nil client without an error is accepted")
	}

	func() {
		defer func() { _ = recover() }()

		_, _, _ = c.Get("panic")
	}()

	// The panicked open released its waiters: the next Get opens again instead of hanging.
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() { _ = recover() }()

		_, _, _ = c.Get("panic")
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Get hangs after a panicked open")
	}

	if c.opens.Load() != 4 {
		t.Fatalf("opens = %d, want 4 (failures are not cached)", c.opens.Load())
	}
}
