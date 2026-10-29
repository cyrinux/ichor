package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The diagnostics ring (see ichorgo/diag.go). With -data-dir the probe keeps it there
// (SetDiagPersist, main.go), so diag-tail and diag-export also show the previous runs.
var diagCommands = []command{
	{name: "diag-tail", args: "[N]", noConfig: true, run: probeDiagTail},
	{name: "diag-export", args: "DEST.zip", noConfig: true, run: probeDiagExport},
}

// probeDiagTail prints the newest N entries (20 by default, 0 = all), oldest first, one
// JSON entry per line like the zip's log.jsonl.
func probeDiagTail(env) (string, error) {
	limit := 20

	if arg := flag.Arg(1); arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 {
			return "", fmt.Errorf("diag-tail %q: want a number of entries", arg)
		}

		limit = n
	}

	out, err := ichorgo.DiagEntries(limit)
	if err != nil {
		return "", err
	}

	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return "", err
	}

	slices.Reverse(entries)

	lines := make([]string, 0, len(entries))
	for _, e := range entries {
		lines = append(lines, string(e))
	}

	return strings.Join(lines, "\n"), nil
}

// probeDiagExport writes the diagnostics zip at DEST (under the temporary directory or next
// to -data-dir) and prints its summary.
func probeDiagExport(env) (string, error) {
	if flag.Arg(1) == "" {
		return "", errors.New("usage: diag-export DEST.zip")
	}

	status, err := ichorgo.DataStoreStatus()
	if err != nil {
		return "", err
	}

	app, err := json.Marshal(map[string]any{
		"app": "probe", "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(),
		"dataStore": json.RawMessage(status),
	})
	if err != nil {
		return "", err
	}

	return ichorgo.DiagExport(argPath(flag.Arg(1)), string(app), "{}")
}
