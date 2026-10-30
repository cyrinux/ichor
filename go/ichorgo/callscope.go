package ichorgo

import (
	"context"
	"errors"
	"sync"
)

// Kotlin coroutine and Swift task cancellation cannot reach Go across gomobile, so a cluster's
// synchronous reads run under one parent context, and the apps cancel it when they leave the
// cluster (CancelCalls). Mutations never derive from it: what the person tapped still lands.
// A followed run holds its cluster's scope while it runs, so the reads it makes are not cut:
// CancelCalls does nothing for that cluster until the run ends. Screen-bound streams do not
// hold (callscope_guard_test.go lists them): the overview stream is almost always live, and
// their screen is gone anyway.

// errCallsCancelled ends a read whose cluster the app left. Both apps match this exact text
// (GoCall.kt, TalosClient.swift): it means no error to show and no cluster to count as down.
var errCallsCancelled = errors.New("cancelled: the app left this cluster")

// callScope is one cluster's parent context, and how many runs hold it.
type callScope struct {
	ctx    context.Context
	cancel context.CancelFunc
	holds  int
}

var (
	callScopesMu sync.Mutex
	// callScopes holds one entry per cacheKey (a hash, no credentials in the key).
	callScopes = map[string]*callScope{}
)

// scopeLocked returns the cluster's live scope, creating it on first use.
func scopeLocked(configYAML, contextName string) *callScope {
	key := cacheKey(configYAML, contextName)

	if scope, ok := callScopes[key]; ok {
		return scope
	}

	ctx, cancel := context.WithCancel(context.Background())
	scope := &callScope{ctx: ctx, cancel: cancel}
	callScopes[key] = scope

	return scope
}

// callParent is the context the cluster's reads derive from.
func callParent(configYAML, contextName string) context.Context {
	callScopesMu.Lock()
	defer callScopesMu.Unlock()

	return scopeLocked(configYAML, contextName).ctx
}

// holdCalls keeps the cluster's scope from being cancelled until release is called (a second
// call does nothing). Deferred in a run's goroutine: `defer holdCalls(config, context)()`.
func holdCalls(configYAML, contextName string) (release func()) {
	callScopesMu.Lock()
	defer callScopesMu.Unlock()

	scope := scopeLocked(configYAML, contextName)
	scope.holds++

	var once sync.Once

	return func() {
		once.Do(func() {
			callScopesMu.Lock()
			defer callScopesMu.Unlock()

			scope.holds--
		})
	}
}

// cancelCalls cancels the cluster's in-flight reads, the next ones getting a fresh scope. It
// does nothing and returns false while a run holds the scope.
func cancelCalls(configYAML, contextName string) bool {
	callScopesMu.Lock()
	defer callScopesMu.Unlock()

	key := cacheKey(configYAML, contextName)

	scope, ok := callScopes[key]
	if !ok {
		return true
	}

	if scope.holds > 0 {
		return false
	}

	scope.cancel()
	delete(callScopes, key)

	return true
}

// scopedResult ends a read made under parent with errCallsCancelled once parent is cancelled,
// whatever fn made of the cancel: a gRPC status is no context.Canceled, and a read that tolerates
// per-node failures (an overview) answers with no error and every node down.
func scopedResult[T any](parent context.Context, out T, err error) (T, error) {
	if parent.Err() != nil {
		var zero T

		return zero, errCallsCancelled
	}

	return out, err
}

// CancelCalls ends the in-flight reads of the cluster the app just left, so they stop holding
// its threads and the network. Mutations and followed runs go on; while a run is under way on
// the cluster, this does nothing and its reads finish as usual. Never an error.
func CancelCalls(configYAML, contextName string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	cancelCalls(configYAML, contextName)

	return nil
}
