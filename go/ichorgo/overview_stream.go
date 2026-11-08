package ichorgo

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// demoNodeGap spaces the demo's streamed nodes, so the apps' partial overview shows offline.
const demoNodeGap = 100 * time.Millisecond

// OverviewListener follows a streamed overview (implemented in Kotlin/Swift). Calls come one
// at a time, from Go threads.
type OverviewListener interface {
	// OnNode gets one nodeOverview as soon as that node answered or failed, in completion
	// order (reachable nodes first in practice). It lacks the public IPs only its peers see,
	// which the complete overview adds.
	OnNode(json string)
	// OnDone is called once with the complete clusterOverview, exactly as ClusterOverview
	// returns it, or errMessage; never after Stop.
	OnDone(overviewJSON string, errMessage string)
}

// OverviewRun is a handle on a streamed overview.
type OverviewRun struct {
	cancel  context.CancelFunc
	stopped atomic.Bool
}

// Stop ends the run: no OnDone follows (nor any further OnNode).
func (r *OverviewRun) Stop() {
	r.stopped.Store(true)
	r.cancel()
}

// StartClusterOverview is ClusterOverview node by node: listener gets each node as it
// answers (OnNode), then the complete overview (OnDone). The demo context sends its five
// nodes 100 ms apart.
func StartClusterOverview(configYAML, contextName string, listener OverviewListener) *OverviewRun {
	start := time.Now()
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedOverviewListener{listener}

	ctx, cancel := context.WithCancel(context.Background())
	run := &OverviewRun{cancel: cancel}

	var mu sync.Mutex // one callback at a time

	deliver := func(call func()) {
		mu.Lock()
		defer mu.Unlock()

		if !run.stopped.Load() {
			call()
		}
	}

	onNode := func(n nodeOverview) {
		js, err := toJSON(n)
		if err != nil {
			return
		}

		deliver(func() { listener.OnNode(js) })
	}

	go func() {
		defer cancel()

		out, err := streamOverview(ctx, start, configYAML, contextName, onNode)
		msg := ""

		if err != nil {
			out, msg = "", err.Error()
		}

		deliver(func() { listener.OnDone(out, msg) })
	}()

	return run
}

// streamOverview is clusterOverviewJSON with a panic turned into an error (gomobile would
// abort the app on one in this goroutine).
func streamOverview(ctx context.Context, start time.Time, configYAML, contextName string, onNode func(nodeOverview)) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", panicError(r)
		}
	}()

	return clusterOverviewJSON(ctx, start, configYAML, contextName, overviewOptions{onNode: onNode, versionTimeout: nodeDialTimeout})
}

// streamDemoNodes sends the demo nodes to onNode demoNodeGap apart; nil onNode (ClusterOverview)
// sends nothing and waits for nothing.
func streamDemoNodes(ctx context.Context, onNode func(nodeOverview)) error {
	if onNode == nil {
		return nil
	}

	for i, n := range demoNodes() {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(demoNodeGap):
			}
		}

		onNode(n)
	}

	return ctx.Err()
}
