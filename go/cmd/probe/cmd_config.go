package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// configTryProbe prints a config try's progress. It never keeps: the node reverts by itself.
type configTryProbe struct{ done chan string }

func (c configTryProbe) OnProgress(json string) { fmt.Println(json) }
func (c configTryProbe) OnDone(outcome, errMessage string) {
	c.done <- strings.TrimSpace(outcome + " " + errMessage)
}

// Editing the machine config.
var configCommands = []command{
	{name: "config-schema", args: "NODE", run: func(e env) (string, error) {
		return ichorgo.MachineConfigSchemaPrepare(e.cfg, e.context, flag.Arg(1))
	}},
	{name: "config-describe", args: "NODE", run: func(e env) (string, error) {
		// The tree of the redacted config, with the schema when it can be had.
		status, err := ichorgo.MachineConfigSchemaPrepare(e.cfg, e.context, flag.Arg(1))
		if err != nil {
			return "", err
		}

		var schema struct{ Version string }
		if err := json.Unmarshal([]byte(status), &schema); err != nil {
			return "", err
		}

		yaml, err := ichorgo.NodeMachineConfig(e.cfg, e.context, flag.Arg(1), false)
		if err != nil {
			return "", err
		}

		return ichorgo.MachineConfigDescribe(yaml, schema.Version)
	}},
	{name: "config-preview", args: "NODE FILE", run: func(e env) (string, error) {
		// FILE is the redacted config of `probe machineconfig NODE`, edited. Only a dry run
		// reaches the node.
		base, draft, err := configDraft(e)
		if err != nil {
			return "", err
		}

		return ichorgo.MachineConfigPreview(e.cfg, e.context, flag.Arg(1), base, draft)
	}},
	{name: "config-edit", args: "FILE EDITJSON", run: func(env) (string, error) {
		// FILE with one field edit applied (see MachineConfigEdit), printed. Local.
		file, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		return ichorgo.MachineConfigEdit(string(file), flag.Arg(2))
	}},
	{name: "config-try", args: "NODE FILE SECONDS [revert]", run: func(e env) (string, error) {
		// Applies FILE for SECONDS (60, 300 or 600): it changes the node for real. The node
		// reverts by itself at the end, or at once with "revert" (after 10 s). Never keeps.
		base, draft, err := configDraft(e)
		if err != nil {
			return "", err
		}

		seconds, _ := strconv.Atoi(flag.Arg(3)) //nolint:errcheck // 0 is refused
		c := configTryProbe{done: make(chan string, 1)}
		run := ichorgo.StartConfigTry(e.cfg, e.context, flag.Arg(1), base, draft, seconds, c)

		if flag.Arg(4) == "revert" {
			time.AfterFunc(10*time.Second, run.Revert)
		}

		return "done: " + <-c.done, nil
	}},
}

// configDraft reads the node's redacted config and the edited copy of it in FILE.
func configDraft(e env) (base, draft string, err error) {
	base, err = ichorgo.NodeMachineConfig(e.cfg, e.context, flag.Arg(1), false)
	if err != nil {
		return "", "", err
	}

	file, err := os.ReadFile(flag.Arg(2))
	if err != nil {
		return "", "", err
	}

	return base, string(file), nil
}
