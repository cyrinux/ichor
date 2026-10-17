package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// overviewProbe prints each streamed node to stderr with its time since the start, so the
// complete overview on stdout stays clean JSON.
type overviewProbe struct {
	start time.Time
	done  chan [2]string
}

func (p *overviewProbe) OnNode(js string) {
	fmt.Fprintf(os.Stderr, "node +%d ms %s\n", time.Since(p.start).Milliseconds(), js)
}

func (p *overviewProbe) OnDone(overviewJSON, errMessage string) {
	p.done <- [2]string{overviewJSON, errMessage}
}

// overviewStream runs `overview -stream`: the nodes as they answer, then the overview;
// Ctrl-C stops the run.
func overviewStream(cfg, contextName string) (string, error) {
	p := &overviewProbe{start: time.Now(), done: make(chan [2]string, 1)}
	run := ichorgo.StartClusterOverview(cfg, contextName, p)

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	defer signal.Stop(interrupt)

	select {
	case d := <-p.done:
		fmt.Fprintf(os.Stderr, "done +%d ms\n", time.Since(p.start).Milliseconds())

		if d[1] != "" {
			return "", errors.New(d[1])
		}

		return d[0], nil
	case <-interrupt:
		run.Stop()

		return "", errors.New("stopped")
	}
}
