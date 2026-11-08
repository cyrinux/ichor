package ichorgo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// sessionCache shares one gRPC connection per (talosconfig, context) between calls and
// closes it after idle time with no call in flight. gRPC reconnects on its own after
// transient network errors, so a cached connection stays usable. A session opens outside
// the lock: an Omni sign-in on one cluster does not hold up the calls of another.
type sessionCache struct {
	mu      sync.Mutex
	idle    time.Duration
	open    func(configYAML, contextName string) (*session, error)
	entries map[string]*cacheEntry
	opening map[string]*sessionOpening
	// epoch counts forgetAuth calls; forgotten is the epoch each sign-in was last forgotten
	// at, so a session opened meanwhile is not kept (see openShared).
	epoch     uint64
	forgotten map[string]uint64
}

type cacheEntry struct {
	session *session
	inUse   int
	timer   *time.Timer
}

// sessionOpening is an open in flight: concurrent acquires of its key wait for it.
type sessionOpening struct {
	done    chan struct{}
	epoch   uint64
	waiters int // acquires waiting, counted in entry.inUse once open
	entry   *cacheEntry
	err     error
}

func newSessionCache(idle time.Duration, open func(configYAML, contextName string) (*session, error)) *sessionCache {
	return &sessionCache{
		idle: idle, open: open,
		entries: map[string]*cacheEntry{}, opening: map[string]*sessionOpening{}, forgotten: map[string]uint64{},
	}
}

// acquire returns a session and a release func that must be called when done with it.
func (c *sessionCache) acquire(configYAML, contextName string) (*session, func(), error) {
	start := time.Now()
	key := cacheKey(configYAML, contextName)

	c.mu.Lock()

	if entry, ok := c.entries[key]; ok {
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}

		entry.inUse++
		c.mu.Unlock()
		trace("session.reused", start)

		return entry.session, c.releaser(key, entry), nil
	}

	op, waiting := c.opening[key]
	if waiting {
		op.waiters++
	} else {
		op = &sessionOpening{done: make(chan struct{}), epoch: c.epoch}
		c.opening[key] = op
	}

	c.mu.Unlock()

	if waiting {
		<-op.done
	} else {
		c.openShared(key, configYAML, contextName, op)
	}

	if op.err != nil {
		return nil, nil, op.err
	}

	// Includes the wait for an open of the same session by another call.
	trace("session.open", start)

	return op.entry.session, c.releaser(key, op.entry), nil
}

// openShared opens op's session, then hands it to the acquires waiting on op (counted in
// use) and caches it, unless its sign-in was forgotten during the open: then its users
// have it until they release it, and the next call opens a new one.
func (c *sessionCache) openShared(key, configYAML, contextName string, op *sessionOpening) {
	var (
		s   *session
		err error
	)

	// Deferred so that a panic in open still releases the acquires waiting on op.done.
	defer func() {
		if s == nil && err == nil {
			err = errors.New("session not opened")
		}

		c.mu.Lock()
		delete(c.opening, key)

		if err == nil {
			op.entry = &cacheEntry{session: s, inUse: 1 + op.waiters}
			if c.forgotten[s.authKey] <= op.epoch {
				c.entries[key] = op.entry
			}
		}

		op.err = err
		c.mu.Unlock()
		close(op.done)
	}()

	s, err = c.open(configYAML, contextName)
}

func (c *sessionCache) releaser(key string, entry *cacheEntry) func() {
	var once sync.Once

	return func() { once.Do(func() { c.release(key, entry) }) }
}

func (c *sessionCache) release(key string, entry *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.inUse--
	if entry.inUse > 0 {
		return
	}

	// Dropped while in use (see forgetAuth): closed by its last user.
	if c.entries[key] != entry {
		entry.session.Close()

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

// forgetAuth drops the sessions signed with the sign-in kept under authKey, which changed:
// the next call opens a new one. A session in use is closed when its last call ends.
func (c *sessionCache) forgetAuth(authKey string) {
	if authKey == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.epoch++
	c.forgotten[authKey] = c.epoch

	for key, entry := range c.entries {
		if entry.session.authKey != authKey {
			continue
		}

		delete(c.entries, key)

		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}

		if entry.inUse == 0 {
			entry.session.Close()
		}
	}
}

// cacheKey hashes the config so credentials are not kept as map keys in clear. The config's
// own hash comes from the config cache: no second pass over the text.
func cacheKey(configYAML, contextName string) string {
	sum := sha256.Sum256([]byte(configs.get(configYAML).hash + "\x00" + contextName))

	return hex.EncodeToString(sum[:])
}
