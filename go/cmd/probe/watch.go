package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// watchProbeFor is how long node-pods-watch prints events before it stops.
const watchProbeFor = 30 * time.Second

// watchProbe prints a Kubernetes watch's events as they come.
type watchProbe struct{ done chan string }

func (p *watchProbe) OnEvent(eventType, js string) { fmt.Fprintf(os.Stderr, "%s %s\n", eventType, js) }

func (p *watchProbe) OnDone(errMessage string) { p.done <- fmt.Sprintf("done err=%q", errMessage) }

// nodePodsWatchRun follows the pods of a Kubernetes node for watchProbeFor, then cancels.
func nodePodsWatchRun(cfg, contextName, kubeServer, node, phase string) string {
	p := &watchProbe{done: make(chan string, 1)}
	run := ichorgo.StartKubeNodePodsWatch(cfg, contextName, kubeServer, node, phase, p)

	select {
	case out := <-p.done:
		return out
	case <-time.After(watchProbeFor):
		run.Cancel()

		return <-p.done
	}
}
