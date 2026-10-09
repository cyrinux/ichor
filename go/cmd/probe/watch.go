package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// watchProbeFor is how long the *-watch commands print events before they stop.
const watchProbeFor = 30 * time.Second

// watchProbe prints a Kubernetes watch's events as they come.
type watchProbe struct{ done chan string }

func (p *watchProbe) OnEvent(eventType, js string) { fmt.Fprintf(os.Stderr, "%s %s\n", eventType, js) }

func (p *watchProbe) OnDone(errMessage string) { p.done <- fmt.Sprintf("done err=%q", errMessage) }

// nodePodsWatchRun follows the pods of a Kubernetes node for watchProbeFor, then cancels.
func nodePodsWatchRun(cfg, contextName, kubeServer, node, phase string) string {
	return watchProbeRun(func(p *watchProbe) *ichorgo.KubeWatchRun {
		return ichorgo.StartKubeNodePodsWatch(cfg, contextName, kubeServer, node, phase, p)
	})
}

// resourceWatchRun follows one resource (Table rows but for pods) for watchProbeFor, then
// cancels; namespace "" is every one.
func resourceWatchRun(cfg, contextName, kubeServer, group, version, resource, namespace string) string {
	return watchProbeRun(func(p *watchProbe) *ichorgo.KubeWatchRun {
		return ichorgo.StartKubeWatch(cfg, contextName, kubeServer, group, version, resource, namespace, "", "", p)
	})
}

// watchProbeRun prints the events of the watch start makes until it ends or watchProbeFor
// passed, and how it ended.
func watchProbeRun(start func(*watchProbe) *ichorgo.KubeWatchRun) string {
	p := &watchProbe{done: make(chan string, 1)}
	run := start(p)

	select {
	case out := <-p.done:
		return out
	case <-time.After(watchProbeFor):
		run.Cancel()

		return <-p.done
	}
}

// changeProbe prints each change signal as it comes.
type changeProbe struct{ done chan string }

func (p *changeProbe) OnUpdate(js string) { fmt.Fprintln(os.Stderr, js) }

func (p *changeProbe) OnDone(errMessage string) { p.done <- fmt.Sprintf("done err=%q", errMessage) }

// changeWatchRun follows the change signal of kinds in namespace ("all" for every
// namespace) for watchProbeFor, then cancels.
func changeWatchRun(cfg, contextName, kubeServer, namespace, kinds string) string {
	if namespace == "all" {
		namespace = ""
	}

	p := &changeProbe{done: make(chan string, 1)}
	run := ichorgo.StartKubeChangeWatch(cfg, contextName, kubeServer, namespace, kinds, p)

	select {
	case out := <-p.done:
		return out
	case <-time.After(watchProbeFor):
		run.Cancel()

		return <-p.done
	}
}
