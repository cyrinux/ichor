package upstream

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	cacheTTL     = 6 * time.Hour
	cacheRetry   = 5 * time.Minute
	cacheEntries = 500
)

// fetched is what one upstream request chain returns: the tags of a repository, or the
// chart versions of an index.yaml.
type fetched struct {
	tags   []string
	charts map[string][]string
	source string
}

type cacheEntry struct {
	at  time.Time
	val fetched
	err error
}

// cache keeps answers 6 h and failures 5 min (the releaseCache policy of TalosUpdateCheck),
// at most 500 entries, and lets one fetch per key run at a time.
type cache struct {
	mu       sync.Mutex
	now      func() time.Time
	entries  map[string]cacheEntry
	inflight map[string]chan struct{}
}

func newCache() *cache {
	return &cache{now: time.Now, entries: map[string]cacheEntry{}, inflight: map[string]chan struct{}{}}
}

// get answers from the cache, waits for a fetch of the same key already running, or runs
// fetch. A fetch cut short by ctx is not remembered.
func (c *cache) get(ctx context.Context, key string, fetch func() (fetched, error)) (fetched, error) {
	for {
		c.mu.Lock()

		if e, ok := c.entries[key]; ok && c.fresh(e) {
			c.mu.Unlock()

			return e.val, e.err
		}

		if wait, ok := c.inflight[key]; ok {
			c.mu.Unlock()

			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return fetched{}, ctx.Err()
			}
		}

		done := make(chan struct{})
		c.inflight[key] = done
		c.mu.Unlock()

		val, err := fetch()

		c.mu.Lock()
		if ctx.Err() == nil || !errors.Is(err, ctx.Err()) {
			c.store(key, cacheEntry{at: c.now(), val: val, err: err})
		}

		delete(c.inflight, key)
		close(done)
		c.mu.Unlock()

		return val, err
	}
}

func (c *cache) fresh(e cacheEntry) bool {
	age := c.now().Sub(e.at)
	if e.err != nil {
		return age < cacheRetry
	}

	return age < cacheTTL
}

// store adds e, dropping the oldest entry past the bound. Called with mu held.
func (c *cache) store(key string, e cacheEntry) {
	if _, ok := c.entries[key]; !ok && len(c.entries) >= cacheEntries {
		oldest, first := "", true

		for k, v := range c.entries {
			if first || v.at.Before(c.entries[oldest].at) {
				oldest, first = k, false
			}
		}

		delete(c.entries, oldest)
	}

	c.entries[key] = e
}
