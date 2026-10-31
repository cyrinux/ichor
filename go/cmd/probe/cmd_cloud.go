package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The token comes from HETZNER_TOKEN (never a flag: it would show in the shell history).
var cloudCommands = []command{
	{name: "hetzner", args: "test|options|create TEMPLATE ROLE HOSTNAME DISK TYPE LOCATION SNAPSHOT_ID -yes", run: func(e env) (string, error) {
		token := os.Getenv("HETZNER_TOKEN")
		if token == "" && flag.Arg(1) != "options" {
			return "", errors.New("set HETZNER_TOKEN to a scratch project's token")
		}

		tokenJSON, err := json.Marshal(map[string]string{"token": token})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.HetznerTest(string(tokenJSON))
		case "options":
			return ichorgo.HetznerOptions(e.cfg, e.context, string(tokenJSON))
		case "create":
			return hetznerCreate(e, string(tokenJSON))
		}

		return "", errors.New("usage: hetzner test|options|create …")
	}},
}

// hetznerCreate creates a billable server: scratch projects only, and only with -yes.
func hetznerCreate(e env, tokenJSON string) (string, error) {
	if flag.NArg() != 10 || flag.Arg(9) != "-yes" {
		return "", errors.New("usage: hetzner create TEMPLATE ROLE HOSTNAME DISK TYPE LOCATION SNAPSHOT_ID -yes (creates a billable server)")
	}

	snapshot, err := strconv.ParseInt(flag.Arg(8), 10, 64)
	if err != nil {
		return "", errors.New("SNAPSHOT_ID is a number")
	}

	spec, err := json.Marshal(map[string]any{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5),
		"serverType": flag.Arg(6), "location": flag.Arg(7), "snapshotId": snapshot,
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartHetznerCreate(e.cfg, e.context, tokenJSON, string(spec), j)

	return "done: " + <-j.done, nil
}
