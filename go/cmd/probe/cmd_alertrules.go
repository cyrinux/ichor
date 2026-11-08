package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// The alert rules' decisions, from the rules and alerts the apps would send.
var alertRulesCommands = []command{
	{name: "alert-rules", args: "RULES_FILE ALERTS_FILE [NOW_MS]", noConfig: true, run: probeAlertRules},
}

// probeAlertRules decides what to do with the alerts of ALERTS_FILE ([{cluster, key, …}]) under
// the rules of RULES_FILE ({"default":{…},"clusters":{…}}) as of NOW_MS (unix ms; now by
// default): go/cmd/probe/testdata/alert-rules.json and alert-rules-alerts.json are samples.
func probeAlertRules(env) (string, error) {
	if flag.NArg() < 3 {
		return "", errors.New("usage: alert-rules RULES_FILE ALERTS_FILE [NOW_MS]")
	}

	rules, err := os.ReadFile(argPath(flag.Arg(1)))
	if err != nil {
		return "", err
	}

	alerts, err := os.ReadFile(argPath(flag.Arg(2)))
	if err != nil {
		return "", err
	}

	now := time.Now().UnixMilli()
	if arg := flag.Arg(3); arg != "" {
		if now, err = strconv.ParseInt(arg, 10, 64); err != nil {
			return "", fmt.Errorf("NOW_MS %q: %w", arg, err)
		}
	}

	return ichorgo.AlertRulesApply(string(rules), string(alerts), now)
}
