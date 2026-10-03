package main

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// netPerfProbe prints a network test's progress as it comes.
type netPerfProbe struct{ done chan string }

func (p *netPerfProbe) OnProgress(js string) { fmt.Fprintln(os.Stderr, js) }

func (p *netPerfProbe) OnDone(report, errMessage string) {
	p.done <- fmt.Sprintf("%s\nerr=%q", report, errMessage)
}

// netPerfRun runs a network test from client to server (host: also on the host network);
// Ctrl-C cancels it, the test namespace is still deleted.
func netPerfRun(cfg, contextName, kubeServer, server, client, host, seconds string) string {
	secs, _ := strconv.Atoi(seconds) //nolint:errcheck
	p := &netPerfProbe{done: make(chan string, 1)}
	run := ichorgo.StartNetPerf(cfg, contextName, kubeServer, server, client, host == "host", secs, p)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	defer signal.Stop(interrupt)

	select {
	case out := <-p.done:
		return out
	case <-interrupt:
		run.Cancel()

		return <-p.done
	}
}
