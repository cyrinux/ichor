package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// probeImageReport is the images report as the apps make it: the inventory, then (Talos) each
// node's CRI images for the unused ones; "kube" reads a kubeconfig cluster's pods instead.
func probeImageReport(e env) (string, error) {
	if flag.Arg(1) == "kube" {
		inv, err := ichorgo.KubeInventory(e.cfg, e.context, e.kubeServer)
		if err != nil {
			return "", err
		}

		return ichorgo.ImageReport(inv)
	}

	inv, err := ichorgo.ClusterInventory(e.cfg, e.context, e.cfg, e.context, e.kubeServer)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Apps []struct {
			Nodes []string `json:"nodes"`
		} `json:"apps"`
	}
	if err := json.Unmarshal([]byte(inv), &parsed); err != nil {
		return "", err
	}

	nodeImages := map[string]json.RawMessage{}

	for _, app := range parsed.Apps {
		for _, node := range app.Nodes {
			if _, done := nodeImages[node]; done || node == "" {
				continue
			}

			images, err := ichorgo.NodeImages(e.cfg, e.context, node)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", node, err)

				images = "[]" // asked once; nothing listed is nothing unused
			}

			nodeImages[node] = json.RawMessage(images)
		}
	}

	byNode, err := json.Marshal(nodeImages)
	if err != nil {
		return "", err
	}

	return ichorgo.ImageReportWithNodeImages(inv, string(byNode))
}
