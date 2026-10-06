package main

import (
	"slices"
	"strings"
)

// noOutput is returned by a command that printed its result itself.
const noOutput = "\x00"

// env is what a command runs with: the talosconfig and the flags.
type env struct {
	cmd, cfg, context, kubeServer, ageRecipient, configPath, home, maskWords string
	mask                                                                     bool
}

// command is one subcommand of the probe: its name, its arguments as the usage shows them
// and what it prints.
type command struct {
	name string
	args string
	run  func(e env) (string, error)
}

// commands is every subcommand, by area.
func commands() []command {
	return slices.Concat(nodeCommands, kubeCommands, dataCommands, etcdCommands)
}

func lookup(name string) (command, bool) {
	for _, c := range commands() {
		if c.name == name {
			return c, true
		}
	}

	return command{}, false
}

// usage lists every command with its arguments.
func usage() string {
	var parts []string
	for _, c := range commands() {
		parts = append(parts, strings.TrimSpace(c.name+" "+c.args))
	}

	return "usage: probe [flags] " + strings.Join(parts, "|")
}
