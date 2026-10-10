package main

import (
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// netToolProbe prints a network tool's lines on stderr and hands back its result.
type netToolProbe struct{ done chan string }

func (p netToolProbe) OnOutput(line string) { fmt.Fprintln(os.Stderr, line) }

func (p netToolProbe) OnDone(resultJSON string, errMessage string) {
	if errMessage != "" {
		p.done <- "error: " + errMessage

		return
	}

	p.done <- resultJSON
}

// netToolRun runs tool against target from node and waits for its result.
func netToolRun(cfg, contextName, node, tool, target, options string) string {
	p := netToolProbe{done: make(chan string, 1)}
	ichorgo.StartNodeNetTool(cfg, contextName, node, tool, target, options, p)

	return <-p.done
}
