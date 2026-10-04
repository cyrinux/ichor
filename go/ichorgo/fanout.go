package ichorgo

import "sync"

// Every per-node call goes through one endpoint's apid: a large cluster must not have the phone
// open hundreds of streams at once. Small clusters still query all their nodes in one go.
const maxNodeFanout = 32

// forEachNode calls fn for every item in parallel, at most maxNodeFanout at a time, and waits for
// them all. A per-node timeout should start inside fn, so a node waiting its turn keeps its full time.
func forEachNode[T any](items []T, fn func(i int, item T)) {
	slots := make(chan struct{}, maxNodeFanout)

	var wg sync.WaitGroup

	for i, item := range items {
		slots <- struct{}{}

		wg.Go(func() {
			defer func() { <-slots }()

			fn(i, item)
		})
	}

	wg.Wait()
}
