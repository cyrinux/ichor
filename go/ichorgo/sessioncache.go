package ichorgo

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// sessionCache shares one gRPC connection per (talosconfig, context) between calls and
// closes it after idle time with no call in flight. gRPC reconnects on its own after
// transient network errors, so a cached connection stays usable.
type sessionCache struct {
	mu      sync.Mutex
	idle    time.Duration
	open    func(configYAML, contextName string) (*session, error)
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	session *session
	inUse   int
	timer   *time.Timer
}

func newSessionCache(idle time.Duration, open func(configYAML, contextName string) (*session, error)) *sessionCache {
	return &sessionCache{idle: idle, open: open, entries: map[string]*cacheEntry{}}
}

// acquire returns a session and a release func that must be called when done with it.
func (c *sessionCache) acquire(configYAML, contextName string) (*session, func(), error) {
	key := cacheKey(configYAML, contextName)

	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		s, err := c.open(configYAML, contextName)
		if err != nil {
			return nil, nil, err
		}

		entry = &cacheEntry{session: s}
		c.entries[key] = entry
	}

	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}

	entry.inUse++

	var once sync.Once

	release := func() { once.Do(func() { c.release(key, entry) }) }

	return entry.session, release, nil
}

func (c *sessionCache) release(key string, entry *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.inUse--
	if entry.inUse > 0 {
		return
	}

	entry.timer = time.AfterFunc(c.idle, func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		// Re-check: it may have been acquired again, or replaced, since the timer started.
		if c.entries[key] != entry || entry.inUse > 0 {
			return
		}

		delete(c.entries, key)
		entry.session.Close()
	})
}

// cacheKey hashes the config so credentials are not kept as map keys in clear.
func cacheKey(configYAML, contextName string) string {
	sum := sha256.Sum256([]byte(configYAML + "\x00" + contextName))

	return hex.EncodeToString(sum[:])
}
