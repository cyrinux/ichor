package ichorgo

import (
	"context"
	"time"
)

// KubeInventory lists the applications running in a cluster added from a kubeconfig, as
// ClusterInventory does for a Talos cluster: every pod's containers, identified from their
// image (and pod, and namespace) against the bundled catalog, read from the Kubernetes API
// instead of each node's containerd. Memory is not known there: the apps carry none.
// kubeServer is the API server address the user set for the cluster, "" for the kubeconfig's.
func KubeInventory(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	if isDemoContext(target.config, target.context) {
		return inventoryJSON(demoInventory(time.Now().UnixMilli()))
	}

	inv, err := withKube(target, func(ctx context.Context, k *kubeClient) (inventory, error) {
		// Best effort, up to idleGrace past the pods: without it, the apps that run nothing are
		// only left out.
		idle := inBackground(func() []idleWorkload {
			idle, _ := readIdleWorkloads(ctx, k) //nolint:errcheck

			return idle
		})

		list, err := listPods(ctx, k)
		if err != nil {
			return inventory{}, err
		}

		nodes := podContainersByNode(list.Pods)

		return withIdleWorkloads(buildInventory(time.Now().UnixMilli(), len(nodes), nodes), idle()), nil
	})
	if err != nil {
		return "", err
	}

	learnInventoryNames(inv)

	return inventoryJSON(inv)
}

// podContainersByNode turns pods into the per-node container lists the inventory reads, in
// the order the nodes first appear. A pod not scheduled yet sits under "".
func podContainersByNode(pods []kubePod) []nodeContainers {
	index := map[string]int{}

	var nodes []nodeContainers

	for _, p := range pods {
		i, ok := index[p.Node]
		if !ok {
			i = len(nodes)
			index[p.Node] = i
			nodes = append(nodes, nodeContainers{Node: p.Node})
		}

		status := podContainerStatus(p.Status)

		for c, image := range p.Images {
			name := ""
			if c < len(p.ContainerNames) {
				name = p.ContainerNames[c]
			}

			nodes[i].Containers = append(nodes[i].Containers, containerInfo{
				Namespace:    containerNSK8s,
				PodNamespace: p.Namespace,
				Pod:          p.Name,
				Name:         name,
				Image:        image,
				Status:       status,
			})
		}
	}

	return nodes
}

// podContainerStatus is the containerd state the inventory counts, from the pod's status as
// `kubectl get pods` shows it: running, finished, or neither (pending, crash-looping).
func podContainerStatus(status string) string {
	switch status {
	case "Running":
		return containerRunning
	case "Completed", "Succeeded", "Error", "Failed":
		return containerExited
	default:
		return "CONTAINER_UNKNOWN"
	}
}
