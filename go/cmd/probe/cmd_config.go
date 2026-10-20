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
	{name: "join-config", args: "TEMPLATE ROLE HOSTNAME DISK [ADDRESS|-] [sealed]", run: func(e env) (string, error) {
		// A new node's config from TEMPLATE's, redacted. With "sealed", the full config with
		// the cluster's secrets, for `talosctl apply-config --insecure -n IP -f -`: keep it off
		// disk.
		address := flag.Arg(5)
		if address == "-" || address == "sealed" {
			address = ""
		}

		generate := ichorgo.JoinNodeConfigPreview
		if flag.Arg(5) == "sealed" || flag.Arg(6) == "sealed" {
			generate = ichorgo.JoinNodeConfigSealed
		}

		return generate(e.cfg, e.context, flag.Arg(1), flag.Arg(2), flag.Arg(3), flag.Arg(4), address, "")
	}},
	{name: "meta", args: "NODE", run: func(e env) (string, error) {
		return ichorgo.NodeMetaKeys(e.cfg, e.context, flag.Arg(1))
	}},
	{name: "meta-write", args: "NODE KEY VALUE", run: func(e env) (string, error) {
		// Writes the key for real (KEY: 10 or 0x0a); Talos reads it at the next boot.
		key, err := strconv.ParseInt(flag.Arg(2), 0, 64)
		if err != nil {
			return "", err
		}

		return "written", ichorgo.NodeMetaWrite(e.cfg, e.context, flag.Arg(1), int(key), flag.Arg(3))
	}},
	{name: "meta-delete", args: "NODE KEY", run: func(e env) (string, error) {
		key, err := strconv.ParseInt(flag.Arg(2), 0, 64)
		if err != nil {
			return "", err
		}

		return "deleted", ichorgo.NodeMetaDelete(e.cfg, e.context, flag.Arg(1), int(key))
	}},
	{name: "config-snippets", run: func(env) (string, error) {
		return ichorgo.ConfigSnippets()
	}},
	{name: "snippet-edits", args: "FILE ID PARAMSJSON", run: func(env) (string, error) {
		// The edits of FILE (a redacted config) that snippet ID makes. Local.
		file, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		return ichorgo.ConfigSnippetEdits(string(file), flag.Arg(2), flag.Arg(3))
	}},
	{name: "drift-fix", args: "FILE KEY BEFORE AFTER", run: func(env) (string, error) {
		// The edits of FILE that give it the reference's value of a drift key. Local.
		file, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		return ichorgo.DriftFixEdits(string(file), flag.Arg(2), flag.Arg(3), flag.Arg(4))
	}},
	{name: "talos-patch-detect", args: "FILE", run: func(env) (string, error) {
		// Whether FILE is a Talos patch that can be tried on a node. Local.
		file, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosPatchDetect(string(file))
	}},
	{name: "talos-patch-draft", args: "NODE FILE", run: func(e env) (string, error) {
		// NODE's config with the patch FILE applied, redacted. Reads the config with its
		// secrets (os:admin); only a dry run reaches the node.
		file, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosPatchDraft(e.cfg, e.context, flag.Arg(1), string(file))
	}},
	{name: "talos-patch-edits", args: "DRAFT FILE", run: func(env) (string, error) {
		// The field edits of DRAFT (a redacted config) that make the patch FILE. Local.
		draft, err := os.ReadFile(flag.Arg(1))
		if err != nil {
			return "", err
		}

		file, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.TalosPatchEdits(string(draft), string(file))
	}},
	{name: "drift", args: "[BASELINE]", run: func(e env) (string, error) {
		// The drift of each node against BASELINE (a saved snapshot) or its role's first node.
		baseline := ""
		if flag.Arg(1) != "" {
			file, err := os.ReadFile(flag.Arg(1))
			if err != nil {
				return "", err
			}
			baseline = string(file)
		}
		snapshot, err := ichorgo.ClusterDriftSnapshot(e.cfg, e.context)
		if err != nil {
			return "", err
		}
		changes, err := ichorgo.CompareDrift(baseline, snapshot)
		if err != nil {
			return "", err
		}
		return formatDrift(changes)
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
	{name: "k8s-upgrade-plan", args: "VERSION", run: func(e env) (string, error) {
		// Read-only: the images per node, the support range, deprecated APIs, blockers.
		return ichorgo.K8sUpgradePlan(e.cfg, e.context, e.kubeServer, flag.Arg(1))
	}},
	{name: "k8s-upgrade", args: "VERSION [--dry-run]", run: func(e env) (string, error) {
		// Without --dry-run it upgrades Kubernetes for real: use a disposable cluster.
		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartK8sUpgrade(e.cfg, e.context, e.kubeServer, flag.Arg(1), flag.Arg(2) == "--dry-run", m)

		return "done: " + <-m.done, nil
	}},
	{name: "config-apply", args: "NODE FILE MODE", run: func(e env) (string, error) {
		// Applies FILE for good (MODE: auto, staged or reboot): it changes the node for real,
		// and "reboot" reboots it.
		base, draft, err := configDraft(e)
		if err != nil {
			return "", err
		}

		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartConfigApply(e.cfg, e.context, flag.Arg(1), base, draft, flag.Arg(3), m)

		return "done: " + <-m.done, nil
	}},
	{name: "config-multi-preview", args: "NODES EDITSFILE", run: func(e env) (string, error) {
		// EDITSFILE is a JSON array of config-edit edits, replayed on each node (a,b,c). Only
		// dry runs reach the nodes.
		edits, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		return ichorgo.MachineConfigMultiPreview(e.cfg, e.context, flag.Arg(1), string(edits))
	}},
	{name: "config-multi-apply", args: "NODES EDITSFILE MODE", run: func(e env) (string, error) {
		// Applies the edits to each node for good, one after the other (MODE: auto, staged or
		// reboot): it changes the nodes for real, and "reboot" reboots them.
		edits, err := os.ReadFile(flag.Arg(2))
		if err != nil {
			return "", err
		}

		m := maintenanceProbe{done: make(chan string, 1)}
		ichorgo.StartConfigApplyMulti(e.cfg, e.context, flag.Arg(1), string(edits), flag.Arg(3), m)

		return "done: " + <-m.done, nil
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
