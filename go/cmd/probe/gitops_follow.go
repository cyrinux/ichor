package main

import (
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// followProbe prints a followed sync's or reconcile's progress as it comes.
type followProbe struct{ done chan string }

func (p *followProbe) OnProgress(js string) { fmt.Fprintln(os.Stderr, js) }

func (p *followProbe) OnDone(result, errMessage string) {
	p.done <- fmt.Sprintf("result=%q err=%q", result, errMessage)
}

// argoFollowRun follows the sync of the Argo CD app namespace/name until it ends; Ctrl-C
// stops following (the sync goes on).
func argoFollowRun(cfg, contextName, kubeServer, namespace, name string) string {
	p := &followProbe{done: make(chan string, 1)}
	run := ichorgo.StartArgoSyncFollow(cfg, contextName, kubeServer, namespace, name, p)

	return (&netPerfProbe{done: p.done}).wait(run.Cancel)
}

// fluxFollowRun follows the reconcile of the Flux object kind namespace/name until the
// controller handled it, 10 minutes at most; Ctrl-C stops following.
func fluxFollowRun(cfg, contextName, kubeServer, kind, namespace, name string) string {
	p := &followProbe{done: make(chan string, 1)}
	run := ichorgo.StartFluxReconcileFollow(cfg, contextName, kubeServer, kind, namespace, name, p)

	return (&netPerfProbe{done: p.done}).wait(run.Cancel)
}
