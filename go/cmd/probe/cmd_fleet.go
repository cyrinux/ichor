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

// The fleet view's ranking, from the snapshots the apps would send.
var fleetCommands = []command{
	{name: "fleet", args: "FILE [NOW_MS]", noConfig: true, run: probeFleet},
}

// probeFleet ranks the clusters of FILE ({"clusters":[…]}, as the apps send them) worst first,
// as of NOW_MS (unix ms; by default the newest check in FILE, so a sample keeps its ages):
// go/cmd/probe/testdata/fleet.json is the demo fleet.
func probeFleet(env) (string, error) {
	if flag.NArg() < 2 {
		return "", errors.New("usage: fleet FILE [NOW_MS]")
	}

	raw, err := os.ReadFile(argPath(flag.Arg(1)))
	if err != nil {
		return "", err
	}

	now, err := fleetNow(raw, flag.Arg(2))
	if err != nil {
		return "", err
	}

	return ichorgo.FleetSummary(string(raw), now)
}

func fleetNow(raw []byte, arg string) (int64, error) {
	if arg != "" {
		now, err := strconv.ParseInt(arg, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("NOW_MS %q: %w", arg, err)
		}

		return now, nil
	}

	var in struct {
		Clusters []struct {
			TakenAt int64 `json:"takenAt"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return 0, fmt.Errorf("invalid fleet snapshots: %w", err)
	}

	newest := int64(0)
	for _, c := range in.Clusters {
		newest = max(newest, c.TakenAt)
	}

	return newest, nil
}
