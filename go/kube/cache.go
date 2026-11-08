package kube

import (
	"errors"
	"sync"
	"time"
)

// ClientCache keeps one client C per target T for a while, opened on first use: fetching
// credentials and probing the API server are the slow part of a call. Concurrent callers of
// the same target wait for one open instead of each opening their own (a Talos admin
// kubeconfig is a new certificate every time).
type ClientCache[T any, C comparable] struct {
	mu      sync.Mutex
	entries map[string]cacheEntry[T, C]
	opening map[string]*cacheOpening[C]

	open  func(T) (C, error)
	key   func(T) string
	close func(C)
	ttl   time.Duration
}

type cacheEntry[T any, C comparable] struct {
	client  C
	target  T
	expires time.Time
}

// cacheOpening is an open in flight.
type cacheOpening[C comparable] struct {
	done   chan struct{}
	client C
	err    error
}

// NewClientCache keeps the clients open returns for ttl. key tells two targets apart; close
// releases a client the cache drops (its idle connections hold the client key).
func NewClientCache[T any, C comparable](open func(T) (C, error), key func(T) string, closeFn func(C), ttl time.Duration) *ClientCache[T, C] {
	return &ClientCache[T, C]{
		entries: map[string]cacheEntry[T, C]{},
		opening: map[string]*cacheOpening[C]{},
		open:    open,
		key:     key,
		close:   closeFn,
		ttl:     ttl,
	}
}

// Get returns the cached client, or opens one; fresh tells the client was opened for this call.
func (c *ClientCache[T, C]) Get(target T) (client C, fresh bool, err error) {
	key := c.key(target)

	c.mu.Lock()
	c.sweepLocked()

	if entry, ok := c.entries[key]; ok {
		c.mu.Unlock()

		return entry.client, false, nil
	}

	if op, ok := c.opening[key]; ok {
		c.mu.Unlock()
		<-op.done

		return op.client, false, op.err
	}

	op := &cacheOpening[C]{done: make(chan struct{})}
	c.opening[key] = op
	c.mu.Unlock()

	// Deferred so that a panic in open still releases the callers waiting on op.done.
	defer func() {
		var none C
		if op.client == none && op.err == nil {
			op.err = errors.New("kubernetes client not opened")
			err = op.err
		}

		c.mu.Lock()
		delete(c.opening, key)

		if op.err == nil {
			c.entries[key] = cacheEntry[T, C]{client: op.client, target: target, expires: time.Now().Add(c.ttl)}
		}
		c.mu.Unlock()
		close(op.done)
	}()

	op.client, op.err = c.open(target)

	return op.client, true, op.err
}

// sweepLocked drops expired clients: their keys should not stay in memory.
func (c *ClientCache[T, C]) sweepLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			c.close(entry.client)
		}
	}
}

// Forget drops client if it is still the cached one, so the next call opens and probes
// again; a client another call already replaced it with stays.
func (c *ClientCache[T, C]) Forget(target T, client C) {
	key := c.key(target)

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok && entry.client == client {
		delete(c.entries, key)
		c.close(client)
	}
}

// ForgetWhere drops every client whose target matches.
func (c *ClientCache[T, C]) ForgetWhere(match func(T) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.entries {
		if match(entry.target) {
			delete(c.entries, key)
			c.close(entry.client)
		}
	}
}
