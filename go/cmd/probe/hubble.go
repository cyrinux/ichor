package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// hubbleProbe prints every snapshot of a live flow view on stderr, the last one on stdout.
type hubbleProbe struct {
	last string
	done chan string
}

func (p *hubbleProbe) OnUpdate(js string) {
	p.last = js
	fmt.Fprintln(os.Stderr, js)
}

func (p *hubbleProbe) OnDone(errMessage string) { p.done <- fmt.Sprintf("err=%q", errMessage) }

// hubbleRun follows the flows for seconds (default 10; Ctrl-C stops sooner). mode "drops"
// keeps dropped flows only; namespace and pod narrow it.
func hubbleRun(cfg, contextName, kubeServer, seconds, mode, namespace, pod string) string {
	secs, err := strconv.Atoi(seconds)
	if err != nil || secs <= 0 {
		secs = 10
	}

	p := &hubbleProbe{done: make(chan string, 1)}
	run := ichorgo.StartHubbleFlows(cfg, contextName, kubeServer, namespace, pod, mode == "drops", p)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	defer signal.Stop(interrupt)

	select {
	case end := <-p.done:
		return p.last + "\n" + end
	case <-interrupt:
	case <-time.After(time.Duration(secs) * time.Second):
	}

	run.Cancel()
	end := <-p.done

	return p.last + "\n" + end
}
