package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// Runbooks (docs/runbooks.md).
var runbookCommands = []command{
	{name: "runbook-plan", args: "FILE", run: func(e env) (string, error) {
		// Read-only: each step with destructive, role and allowed.
		src, err := readRunbookFile()
		if err != nil {
			return "", err
		}

		return ichorgo.RunbookPlan(e.cfg, e.context, e.kubeServer, src)
	}},
	{name: "runbook-run", args: "FILE [-yes]", run: probeRunbookRun},
}

func readRunbookFile() (string, error) {
	if flag.Arg(1) == "" {
		return "", errors.New("usage: runbook-plan|runbook-run FILE")
	}

	b, err := os.ReadFile(flag.Arg(1))

	return string(b), err
}

// probeRunbookRun runs the runbook for real, printing each step's state; a runbook with a
// destructive step needs -yes (the apps confirm each such step instead).
func probeRunbookRun(e env) (string, error) {
	src, err := readRunbookFile()
	if err != nil {
		return "", err
	}

	planJSON, err := ichorgo.RunbookPlan(e.cfg, e.context, e.kubeServer, src)
	if err != nil {
		return "", err
	}

	if err := refuseDestructive(planJSON, flag.Arg(2) == "-yes"); err != nil {
		return "", err
	}

	r := runbookProbe{done: make(chan string, 1), failed: make(chan string, 1)}
	ichorgo.StartRunbook(e.cfg, e.context, e.kubeServer, src, r)

	select {
	case out := <-r.done:
		var res struct {
			OK bool `json:"ok"`
		}

		if err := json.Unmarshal([]byte(out), &res); err != nil || !res.OK {
			return "", fmt.Errorf("the runbook did not complete: %s", out)
		}

		return out, nil
	case msg := <-r.failed:
		return "", errors.New(msg)
	}
}

// refuseDestructive names the first destructive step of planJSON unless yes.
func refuseDestructive(planJSON string, yes bool) error {
	var plan struct {
		Steps []struct {
			ID          string `json:"id"`
			Destructive bool   `json:"destructive"`
		} `json:"steps"`
	}

	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return err
	}

	for _, s := range plan.Steps {
		if s.Destructive && !yes {
			return fmt.Errorf("step %s is destructive: run again with -yes after reading runbook-plan", s.ID)
		}
	}

	return nil
}

type runbookProbe struct{ done, failed chan string }

func (r runbookProbe) OnStep(_ int, stateJSON string) { fmt.Println(stateJSON) }
func (r runbookProbe) OnDone(resultJSON string)       { r.done <- resultJSON }
func (r runbookProbe) OnError(message string)         { r.failed <- message }
