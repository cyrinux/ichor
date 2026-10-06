package ichorgo

import "sync"

// Every per-node call goes through one endpoint's apid: a large cluster must not have the phone
// open hundreds of streams at once. Small clusters still query all their nodes in one go.
const maxNodeFanout = 32

// forEachLimit calls fn for every item in parallel, at most limit at a time, and waits for
// them all. A panic in fn must not abort the app (through gomobile it would): the item keeps
// its zero result.
func forEachLimit[T any](items []T, limit int, fn func(i int, item T)) {
	slots := make(chan struct{}, limit)

	var wg sync.WaitGroup

	for i, item := range items {
		slots <- struct{}{}

		wg.Go(func() {
			defer func() { <-slots }()
			defer func() { _ = recover() }()

			fn(i, item)
		})
	}

	wg.Wait()
}

// forEachNode is forEachLimit for per-node calls, maxNodeFanout at a time. A per-node timeout
// should start inside fn, so a node waiting its turn keeps its full time.
func forEachNode[T any](items []T, fn func(i int, item T)) {
	forEachLimit(items, maxNodeFanout, fn)
}
