package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

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
