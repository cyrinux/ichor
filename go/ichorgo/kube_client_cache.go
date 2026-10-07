package ichorgo

import (
	"context"
	"errors"
	"sync"
	"time"
)

// One Kubernetes client per cluster the app looks at, opened on first use and kept for a
// while: the kubeconfig fetch and the API server probe are the slow part of a call.

// kubeTarget is what a Kubernetes client is opened for: a talosconfig context and the API
// server address the user set for it, if any.
type kubeTarget struct {
	config, context, server string
}

func (t kubeTarget) key() string { return cacheKey(t.config, t.context+"\x00"+t.server) }

// kubeClients caches one client per kubeTarget for kubeClientTTL.
var kubeClients = newKubeClientCache(openKubeClientForContext)

type kubeClientEntry struct {
	client  *kubeClient
	target  kubeTarget
	expires time.Time
}

// kubeOpening is an open in flight: concurrent callers wait for it instead of each asking
// Talos for a kubeconfig (every Kubeconfig call signs a new certificate).
type kubeOpening struct {
	done   chan struct{}
	client *kubeClient
	err    error
}

type kubeClientCache struct {
	mu      sync.Mutex
	entries map[string]kubeClientEntry
	opening map[string]*kubeOpening
	open    func(kubeTarget) (*kubeClient, error)
}

func newKubeClientCache(open func(kubeTarget) (*kubeClient, error)) *kubeClientCache {
	return &kubeClientCache{entries: map[string]kubeClientEntry{}, opening: map[string]*kubeOpening{}, open: open}
}

// get returns the cached client, or opens one; fresh tells the client was opened for this call.
func (c *kubeClientCache) get(target kubeTarget) (k *kubeClient, fresh bool, err error) {
	key := target.key()

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

	op := &kubeOpening{done: make(chan struct{})}
	c.opening[key] = op
	c.mu.Unlock()

	// Deferred so that a panic in open still releases the callers waiting on op.done.
	defer func() {
		if op.client == nil && op.err == nil {
			op.err = errors.New("kubernetes client not opened")
			err = op.err
		}

		c.mu.Lock()
		delete(c.opening, key)

		if op.err == nil {
			c.entries[key] = kubeClientEntry{client: op.client, target: target, expires: time.Now().Add(kubeClientTTL)}
		}
		c.mu.Unlock()
		close(op.done)
	}()

	op.client, op.err = c.open(target)

	return op.client, true, op.err
}

// sweepLocked drops expired clients: their keys should not stay in memory.
func (c *kubeClientCache) sweepLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			entry.client.close()
		}
	}
}

// forget drops k if it is still the cached client, so the next call fetches a new
// kubeconfig and probes again; a client another call already replaced it with stays.
func (c *kubeClientCache) forget(target kubeTarget, k *kubeClient) {
	key := target.key()

	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[key]; ok && entry.client == k {
		delete(c.entries, key)
		k.close()
	}
}

// forgetConfig drops every client of the context (any API server address): its credentials
// changed (signed in or out).
func (c *kubeClientCache) forgetConfig(config, context string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.entries {
		if entry.target.config == config && entry.target.context == context {
			delete(c.entries, key)
			entry.client.close()
		}
	}
}

// openKubeClientForContext fetches an admin kubeconfig from Talos (bounded by callTimeout)
// and probes the API server addresses (bounded by kubeProbeTimeout). A cluster added from a
// kubeconfig uses its own context, at its own server only.
func openKubeClientForContext(target kubeTarget) (*kubeClient, error) {
	if isKubeconfig(target.config) {
		return openStoredKubeClient(target)
	}

	var endpoints []string

	kubeconfig, err := withSession(target.config, target.context, callTimeout, func(ctx context.Context, s *session) (string, error) {
		endpoints = s.context.Endpoints

		return fetchKubeconfig(ctx, s)
	})
	if err != nil {
		return nil, err
	}

	return openKubeClient(context.Background(), kubeconfig, endpoints, target.server)
}

// openStoredKubeClient opens a cluster added from a kubeconfig. A context that signs in
// through a method gets its token source; the first token is minted before probing, so a
// needed sign-in is reported as such rather than as an unreachable server.
func openStoredKubeClient(target kubeTarget) (*kubeClient, error) {
	sc, err := signInContext(target.config, target.context)
	if err != nil {
		return nil, err
	}

	if sc.method == nil {
		kubeconfig, err := kubeContextYAML(target.config, target.context)
		if err != nil {
			return nil, err
		}

		return openKubeClient(context.Background(), kubeconfig, nil, target.server)
	}

	tokens := kubeAuth.source(sc.key, sc.method)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if _, err := tokens.get(ctx); err != nil {
		return nil, err
	}

	kubeconfig, err := kubeContextYAMLWithoutUser(target.config, target.context)
	if err != nil {
		return nil, err
	}

	creds, err := parseKubeconfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	creds.token, creds.tokens = "", tokens

	return openKubeClientCreds(context.Background(), creds, nil, target.server)
}
