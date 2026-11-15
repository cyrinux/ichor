package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// ichor-relay pairing, through the first relay the cluster has.
var relayCommands = []command{
	{name: "relay-discover", args: "", run: func(e env) (string, error) {
		return ichorgo.RelayDiscover(e.cfg, e.context, e.kubeServer)
	}},
	{name: "relay-pair", args: "ENDPOINT [P256DH AUTH | ntfy]", run: runRelayPair},
	{name: "relay-unpair", args: "ID", run: func(e env) (string, error) {
		src, err := relaySource(e)
		if err != nil {
			return "", err
		}

		if err := ichorgo.RelayUnregister(e.cfg, e.context, e.kubeServer, src, flag.Arg(1)); err != nil {
			return "", err
		}

		return "removed", nil
	}},
}

// runRelayPair registers ENDPOINT: a ntfy topic, or a UnifiedPush endpoint with its keys. It
// asks the relay for its VAPID key first, as the app does before subscribing.
func runRelayPair(e env) (string, error) {
	src, err := relaySource(e)
	if err != nil {
		return "", err
	}

	device := map[string]any{"platform": "ntfy", "endpoint": flag.Arg(1), "label": "probe"}

	switch {
	case flag.Arg(1) == "":
		return "", errors.New("usage: relay-pair ENDPOINT [P256DH AUTH | ntfy]")
	case flag.Arg(2) != "" && flag.Arg(2) != "ntfy":
		pairing, err := ichorgo.RelayPairing(e.cfg, e.context, e.kubeServer, src)
		if err != nil {
			return "", err
		}

		fmt.Fprintln(os.Stderr, "pairing:", pairing)

		device["platform"], device["p256dh"], device["auth"] = "unifiedpush", flag.Arg(2), flag.Arg(3)
	}

	body, err := json.Marshal(device)
	if err != nil {
		return "", err
	}

	return ichorgo.RelayRegister(e.cfg, e.context, e.kubeServer, src, string(body))
}

// relaySource is the first relay RelayDiscover finds, as JSON.
func relaySource(e env) (string, error) {
	out, err := ichorgo.RelayDiscover(e.cfg, e.context, e.kubeServer)
	if err != nil {
		return "", err
	}

	var d struct {
		Sources []json.RawMessage `json:"sources"`
		Reason  string            `json:"reason"`
	}

	if err := json.Unmarshal([]byte(out), &d); err != nil {
		return "", err
	}

	if len(d.Sources) == 0 {
		return "", errors.New(d.Reason)
	}

	return string(d.Sources[0]), nil
}
