package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strconv"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The token comes from HETZNER_TOKEN, the OVHcloud service account from OVH_ENDPOINT, OVH_CLIENT_ID,
// OVH_CLIENT_SECRET and OVH_PROJECT (never a flag: it would show in the shell history).
var cloudCommands = []command{
	{name: "hetzner", args: "test|options|estimate ARCH LOCATION|snapshot ARCH LOCATION -yes|create TEMPLATE ROLE HOSTNAME DISK TYPE LOCATION SNAPSHOT_ID -yes", run: func(e env) (string, error) {
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
		case "estimate":
			return ichorgo.HetznerSnapshotEstimate(string(tokenJSON), flag.Arg(2), flag.Arg(3))
		case "snapshot":
			return hetznerSnapshot(e, string(tokenJSON))
		}

		return "", errors.New("usage: hetzner test|options|estimate|snapshot|create …")
	}},
	{name: "ovh", args: "test|options|policy|create TEMPLATE ROLE HOSTNAME DISK REGION FLAVOR_ID IMAGE_ID -yes", run: func(e env) (string, error) {
		credJSON, err := json.Marshal(map[string]string{
			"endpoint": os.Getenv("OVH_ENDPOINT"), "clientId": os.Getenv("OVH_CLIENT_ID"),
			"clientSecret": os.Getenv("OVH_CLIENT_SECRET"), "project": os.Getenv("OVH_PROJECT"),
		})
		if err != nil {
			return "", err
		}

		switch flag.Arg(1) {
		case "test":
			return ichorgo.OVHTest(string(credJSON))
		case "options":
			return ichorgo.OVHOptions(e.cfg, e.context, string(credJSON))
		case "policy":
			return ichorgo.OVHServiceAccountPolicy(os.Getenv("OVH_PROJECT")), nil
		case "create":
			return ovhCreate(e, string(credJSON))
		}

		return "", errors.New("usage: ovh test|options|policy|create …")
	}},
}

// ovhCreate creates a billable instance: scratch projects only, and only with -yes.
func ovhCreate(e env, credJSON string) (string, error) {
	if flag.NArg() != 10 || flag.Arg(9) != "-yes" {
		return "", errors.New("usage: ovh create TEMPLATE ROLE HOSTNAME DISK REGION FLAVOR_ID IMAGE_ID -yes (creates a billable instance)")
	}

	spec, err := json.Marshal(map[string]string{
		"templateNode": flag.Arg(2), "role": flag.Arg(3), "hostname": flag.Arg(4), "installDisk": flag.Arg(5),
		"region": flag.Arg(6), "flavorId": flag.Arg(7), "imageId": flag.Arg(8),
	})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartOVHCreate(e.cfg, e.context, credJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// hetznerSnapshot makes a billable snapshot with a throwaway server: scratch projects only, and
// only with -yes.
func hetznerSnapshot(e env, tokenJSON string) (string, error) {
	if flag.NArg() != 5 || flag.Arg(4) != "-yes" {
		return "", errors.New("usage: hetzner snapshot ARCH LOCATION -yes (creates a billable server and a snapshot)")
	}

	spec, err := json.Marshal(map[string]string{"arch": flag.Arg(2), "location": flag.Arg(3)})
	if err != nil {
		return "", err
	}

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartHetznerSnapshot(e.cfg, e.context, tokenJSON, string(spec), j)

	return "done: " + <-j.done, nil
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
