package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// A log's most repeated messages, on lines saved from pod-logs or workload-logs.
var logTemplateCommands = []command{
	{name: "log-templates", args: "FILE [LIMIT]", noConfig: true, run: probeLogTemplates},
}

// probeLogTemplates groups the log lines of FILE (a JSON array of lines, or the "lines" of
// workload-logs) into their LIMIT (20 by default) most repeated messages.
func probeLogTemplates(env) (string, error) {
	if flag.Arg(1) == "" {
		return "", errors.New("usage: log-templates FILE [LIMIT]")
	}

	raw, err := os.ReadFile(argPath(flag.Arg(1)))
	if err != nil {
		return "", err
	}

	limit := 0
	if arg := flag.Arg(2); arg != "" {
		if limit, err = strconv.Atoi(arg); err != nil {
			return "", fmt.Errorf("LIMIT %q: %w", arg, err)
		}
	}

	// A workload-logs answer: its lines.
	var workload struct {
		Lines json.RawMessage `json:"lines"`
	}
	if json.Unmarshal(raw, &workload) == nil && len(workload.Lines) > 0 {
		raw = workload.Lines
	}

	return ichorgo.LogTemplates(string(raw), limit)
}
