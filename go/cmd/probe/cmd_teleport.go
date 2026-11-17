package main

import (
	"flag"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// Teleport: the Kubernetes clusters of a context's proxy, and the kubeconfig with one more
// (printed, never written).
var teleportCommands = []command{
	{name: "teleport-clusters", args: "CONTEXT", run: func(e env) (string, error) {
		return ichorgo.KubeTeleportClusters(e.cfg, flag.Arg(1))
	}},
	{name: "teleport-add", args: "CONTEXT KUBE_CLUSTER", run: func(e env) (string, error) {
		return ichorgo.KubeTeleportAddCluster(e.cfg, flag.Arg(1), flag.Arg(2))
	}},
}
