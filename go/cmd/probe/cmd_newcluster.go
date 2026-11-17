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

// newClusterProbe prepares a new cluster (prepare) or prepares and bootstraps it (start
// with "yes"), storing the talosconfig first as the apps do.
func newClusterProbe(mode, name, address, disk, talosVersion, k8sVersion, confirm string) (string, error) {
	if mode != "prepare" && mode != "start" {
		return "", errors.New("new-cluster prepare|start NAME ADDRESS DISK [TALOS-VERSION|-] [K8S-VERSION|-] [yes]")
	}

	spec := map[string]string{"name": name, "endpoint": address, "installDisk": disk}
	if talosVersion != "-" {
		spec["talosVersion"] = talosVersion
	}

	if k8sVersion != "-" {
		spec["kubernetesVersion"] = k8sVersion
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	out, err := ichorgo.NewClusterPrepare(string(specJSON))
	if err != nil || mode == "prepare" {
		return out, err
	}

	if confirm != "yes" {
		return "", fmt.Errorf("add \"yes\": this erases %s on %s", disk, address)
	}

	var draft struct {
		DraftID     string `json:"draftId"`
		Talosconfig string `json:"talosconfig"`
	}

	if err := json.Unmarshal([]byte(out), &draft); err != nil {
		return "", err
	}

	path := argPath("talosconfig-" + name)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("store the talosconfig first: %w", err)
	}

	if _, err := f.WriteString(draft.Talosconfig); err != nil {
		f.Close() //nolint:errcheck,gosec

		return "", err
	}

	if err := f.Close(); err != nil {
		return "", err
	}

	fmt.Fprintln(os.Stderr, "stored", path)

	j := joinProbe{done: make(chan string, 1)}
	ichorgo.StartNewCluster(draft.DraftID, address, draft.Talosconfig, j)

	return "done: " + <-j.done, nil
}

// hetznerNewCluster makes a billable one-node cluster at Hetzner (a server and a Primary IP):
// scratch projects only, and only with -yes. The talosconfig is stored when the run hands it
// over, before the server exists.
func hetznerNewCluster(e env, tokenJSON string) (string, error) {
	if flag.NArg() != 7 || flag.Arg(6) != "-yes" {
		return "", errors.New("usage: hetzner new-cluster NAME TYPE LOCATION SNAPSHOT_ID -yes (creates a billable server and Primary IP)")
	}

	snapshot, err := strconv.ParseInt(flag.Arg(5), 10, 64)
	if err != nil {
		return "", errors.New("SNAPSHOT_ID is a number")
	}

	name := flag.Arg(2)

	spec, err := json.Marshal(map[string]any{"name": name, "serverType": flag.Arg(3), "location": flag.Arg(4), "snapshotId": snapshot})
	if err != nil {
		return "", err
	}

	j := newClusterListener{joinProbe: joinProbe{done: make(chan string, 1)}, path: argPath("talosconfig-" + name)}
	ichorgo.StartHetznerNewCluster(e.cfg, e.context, tokenJSON, string(spec), j)

	return "done: " + <-j.done, nil
}

// newClusterListener stores the talosconfig the secrets event carries, and prints the
// events without it.
type newClusterListener struct {
	joinProbe

	path string
}

func (l newClusterListener) OnProgress(event string) {
	var p map[string]any
	if err := json.Unmarshal([]byte(event), &p); err != nil {
		fmt.Println(event)

		return
	}

	if talosconfig, ok := p["talosconfig"].(string); ok {
		if err := os.WriteFile(l.path, []byte(talosconfig), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "could not store the talosconfig:", err)
		} else {
			fmt.Fprintln(os.Stderr, "stored", l.path)
		}

		p["talosconfig"] = "(stored)"
	}

	out, _ := json.Marshal(p)
	fmt.Println(string(out))
}
