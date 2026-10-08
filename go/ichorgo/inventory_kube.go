package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"
)

// The app inventory of a cluster added from a kubeconfig: no Talos API to list each node's
// CRI containers, so the pods of the whole cluster, read from the Kubernetes API, stand in:
// every container of every pod, grouped by the node it runs on, through the same catalog
// identification as the Talos inventory. Memory and CPU stay 0: the API server does not
// know them.

// inventoryPodPages caps how many pages of kubePageLimit pods are read: a cluster with more
// gets a truncated inventory rather than a read that never ends. A var so tests lower it.
var inventoryPodPages = 20

const inventoryKubeTimeout = 30 * time.Second

var errInventoryTruncated = errors.New("inventory: the pod list was cut")

// KubeInventory lists the applications running in the cluster from its pods (os:admin), as
// ClusterInventory does from the Talos nodes' containers: for a cluster added from a
// kubeconfig, whose inventory the app asks here with the API address set for it.
// kubeServer: see KubePods.
func KubeInventory(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterInventory", configYAML, contextName, "")
	}

	return kubeInventory(kubeTarget{configYAML, contextName, kubeServer})
}

func kubeInventory(target kubeTarget) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), inventoryKubeTimeout)
	defer cancel()

	inv, err := withKubeContext(ctx, target, readKubeInventory)
	if err != nil {
		return "", kubeError(err)
	}

	learnInventoryNames(inv)

	return inventoryJSON(inv)
}

// readKubeInventory reads the nodes (403 tolerated: then the nodes are those the pods name)
// and every page of the pods, up to inventoryPodPages, and builds the inventory as the Talos
// one is built. Pods not scheduled yet have no node, as no CRI ever saw them: left out.
func readKubeInventory(ctx context.Context, k *kubeClient) (inventory, error) {
	at := time.Now().UnixMilli()

	nodes, err := listKubeNodeObjects(ctx, k)
	if err != nil && !isForbidden(err) {
		return inventory{}, err
	}

	byNode := map[string][]containerInfo{}
	pages := 0

	reset := func() {
		clear(byNode)
		pages = 0

		for _, n := range nodes {
			byNode[n.Metadata.Name] = []containerInfo{}
		}
	}

	reset()

	err = k.listAll(ctx, "/api/v1/pods", pageQuery{}, reset, func(page kubePage) error {
		objs, err := decodeItems[podObject](page)
		if err != nil {
			return err
		}

		for _, obj := range objs {
			if obj.Spec.NodeName == "" {
				continue
			}

			byNode[obj.Spec.NodeName] = append(byNode[obj.Spec.NodeName], podContainers(obj)...)
		}

		if pages++; pages == inventoryPodPages && page.continueToken != "" {
			return errInventoryTruncated
		}

		return nil
	})

	truncated := errors.Is(err, errInventoryTruncated)

	switch {
	case truncated:
	case isForbidden(err):
		return inventory{At: at, Nodes: len(nodes), Apps: []inventoryApp{}, Forbidden: true}, nil
	case err != nil:
		return inventory{}, err
	}

	lists := make([]nodeContainers, 0, len(byNode))
	for _, name := range slices.Sorted(maps.Keys(byNode)) {
		lists = append(lists, nodeContainers{Node: name, Containers: byNode[name]})
	}

	// Without a node list, the nodes are those the pods run on.
	inv := buildInventory(at, max(len(nodes), len(lists)), lists)
	inv.Truncated = truncated

	return inv, nil
}

// podContainers are a pod's containers (init ones included, as the CRI lists them), in the
// shape the Talos inventory reads from a node.
func podContainers(obj podObject) []containerInfo {
	statuses := map[string]containerStatus{}

	for _, cs := range obj.Status.InitContainerStatuses {
		statuses[cs.Name] = cs
	}

	for _, cs := range obj.Status.ContainerStatuses {
		statuses[cs.Name] = cs
	}

	out := make([]containerInfo, 0, len(obj.Spec.InitContainers)+len(obj.Spec.Containers))

	add := func(name, image string) {
		cs, known := statuses[name]
		out = append(out, containerInfo{
			ID:           cmp.Or(containerIDOf(cs.ContainerID), obj.Metadata.Namespace+"/"+obj.Metadata.Name+"/"+name),
			PodNamespace: obj.Metadata.Namespace,
			Pod:          obj.Metadata.Name,
			Name:         name,
			Image:        image,
			Status:       criState(cs, known),
		})
	}

	for _, c := range obj.Spec.InitContainers {
		add(c.Name, c.Image)
	}

	for _, c := range obj.Spec.Containers {
		add(c.Name, c.Image)
	}

	return out
}

// criState is the CRI state of a container as its Kubernetes status tells it: exited once
// terminated, created while waiting (or before any status), running otherwise.
func criState(cs containerStatus, known bool) string {
	switch {
	case !known || cs.State.Waiting != nil:
		return "CONTAINER_CREATED"
	case cs.State.Terminated != nil:
		return containerExited
	default:
		return containerRunning
	}
}

// containerIDOf strips the runtime scheme ("containerd://") off a status containerID.
func containerIDOf(id string) string {
	if _, after, ok := strings.Cut(id, "://"); ok {
		return after
	}

	return id
}
