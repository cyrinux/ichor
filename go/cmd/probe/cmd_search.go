package main

import (
	"errors"
	"flag"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The search's screens, ranked like the apps do it from their catalog.
var searchCommands = []command{
	{name: "search-screens", args: "FILE QUERY", noConfig: true, run: probeSearchScreens},
}

// probeSearchScreens ranks the screens of FILE ({"screens":[…]}, as the apps send them) for
// QUERY: go/cmd/probe/testdata/screens.json is a sample catalog.
func probeSearchScreens(env) (string, error) {
	if flag.NArg() < 3 {
		return "", errors.New("usage: search-screens FILE QUERY")
	}

	raw, err := os.ReadFile(argPath(flag.Arg(1)))
	if err != nil {
		return "", err
	}

	return ichorgo.SearchScreens(string(raw), flag.Arg(2), 0)
}
