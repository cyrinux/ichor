package ichorgo

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestForEachNodeVisitsEveryItemWithItsIndex(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	got := make([]string, len(items))

	forEachNode(items, func(i int, item string) { got[i] = item })

	for i := range items {
		if got[i] != items[i] {
			t.Fatalf("index %d: got %q, want %q", i, got[i], items[i])
		}
	}
}

func TestForEachNodeCapsConcurrency(t *testing.T) {
	items := make([]int, maxNodeFanout*3)

	var running, peak atomic.Int32

	forEachNode(items, func(int, int) {
		now := running.Add(1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}

		time.Sleep(5 * time.Millisecond)
		running.Add(-1)
	})

	if p := peak.Load(); p > maxNodeFanout {
		t.Fatalf("peak concurrency %d exceeds the cap of %d", p, maxNodeFanout)
	}

	if p := peak.Load(); p < 2 {
		t.Fatalf("peak concurrency %d: nodes were not queried in parallel", p)
	}
}

func TestForEachNodeWaitsForAll(t *testing.T) {
	var mu sync.Mutex

	done := 0

	forEachNode(make([]struct{}, 50), func(int, struct{}) {
		time.Sleep(time.Millisecond)
		mu.Lock()
		done++
		mu.Unlock()
	})

	if done != 50 {
		t.Fatalf("returned after %d of 50 items", done)
	}
}

func TestForEachNodeEmpty(t *testing.T) {
	forEachNode([]string(nil), func(int, string) { t.Fatal("called for no items") })
}
