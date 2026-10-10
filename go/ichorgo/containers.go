package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// Containerd namespaces of a node: "system" holds Talos' own containers (apid, trustd and the
// extension services; etcd and kubelet are services, see ServiceAction), run by containerd;
// "k8s.io" the Kubernetes ones, run through the CRI.
const (
	containerNSSystem = constants.SystemContainerdNamespace
	containerNSK8s    = constants.K8sContainerdNamespace
)

type containerList struct {
	At         int64           `json:"at"` // unix ms, to turn CPU nanosecond deltas into %
	Containers []containerInfo `json:"containers"`
}

type containerInfo struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace"` // containerNSSystem or containerNSK8s
	PodNamespace string `json:"podNamespace"`
	Pod          string `json:"pod"`
	Name         string `json:"name"`
	Image        string `json:"image"`
	Status       string `json:"status"`
	Pid          uint32 `json:"pid"`
	Memory       uint64 `json:"memory"` // bytes
	CPUNanos     uint64 `json:"cpuNanos"`
}

// NodeContainers lists the containers on node, Talos' system ones then the Kubernetes ones,
// with their memory and cumulative CPU time, like `talosctl containers [-k]` +
// `talosctl stats [-k]` (os:reader).
func NodeContainers(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeContainers", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		list, err := s.client.Containers(ctx, containerNSK8s, common.ContainerDriver_CRI)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		stats, _ := s.client.Stats(ctx, containerNSK8s, common.ContainerDriver_CRI) //nolint:errcheck

		// The system containers are a bonus: without them the Kubernetes list still answers.
		system, _ := s.client.Containers(ctx, containerNSSystem, common.ContainerDriver_CONTAINERD) //nolint:errcheck
		systemStats, _ := s.client.Stats(ctx, containerNSSystem, common.ContainerDriver_CONTAINERD) //nolint:errcheck

		return toJSON(containerList{
			At: time.Now().UnixMilli(),
			Containers: append(
				mergeContainers(containerNSSystem, first(system.GetMessages()).GetContainers(), first(systemStats.GetMessages()).GetStats()),
				mergeContainers(containerNSK8s, first(list.GetMessages()).GetContainers(), first(stats.GetMessages()).GetStats())...),
		})
	})
}

// ContainerRestart restarts one container on node without rebooting it, like
// `talosctl restart [-k] ID` (os:admin). namespace is "system" (a Talos container such as
// apid or an extension service) or "k8s.io" (a Kubernetes container, which the kubelet
// starts again). Talos stops the container; its supervisor starts it again.
func ContainerRestart(configYAML, contextName, node, namespace, id string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)
	namespace, id = strings.TrimSpace(namespace), strings.TrimSpace(id)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "container-restart", Node: node, Object: namespace + "/" + id})

	driver, err := containerDriver(namespace)
	if err != nil {
		return err
	}

	if id == "" {
		return errors.New("no container given")
	}

	return nodeAction(configYAML, contextName, node, callTimeout, func(ctx context.Context, c *client.Client) error {
		return c.Restart(ctx, namespace, driver, id)
	})
}

// containerDriver is the runtime that manages the containers of namespace.
func containerDriver(namespace string) (common.ContainerDriver, error) {
	switch namespace {
	case containerNSSystem:
		return common.ContainerDriver_CONTAINERD, nil
	case containerNSK8s:
		return common.ContainerDriver_CRI, nil
	}

	return 0, fmt.Errorf("unknown container namespace %q (system or k8s.io)", namespace)
}

// mergeContainers joins the container info of namespace with stats by id, dropping pause
// (sandbox) containers, and sorts by pod then container name.
func mergeContainers(namespace string, infos []*machineapi.ContainerInfo, stats []*machineapi.Stat) []containerInfo {
	byID := make(map[string]*machineapi.Stat, len(stats))
	for _, st := range stats {
		byID[st.GetId()] = st
	}

	out := make([]containerInfo, 0, len(infos))

	for _, c := range infos {
		// The pod sandbox ("pause") container has the pod's own id and no workload. A system
		// container has no pod: its pod id may be its own id, so only the image tells.
		sandbox := namespace == containerNSK8s && c.GetId() == c.GetPodId()
		if sandbox || strings.Contains(c.GetImage(), "/pause:") {
			continue
		}

		ns, pod := splitPodID(c.GetPodId())
		name := c.GetName()

		if namespace == containerNSSystem {
			ns, pod = "", ""

			if name == "" {
				name = c.GetId()
			}
		}

		info := containerInfo{
			ID:           c.GetId(),
			Namespace:    namespace,
			PodNamespace: ns,
			Pod:          pod,
			Name:         name,
			Image:        c.GetImage(),
			Status:       c.GetStatus(),
			Pid:          c.GetPid(),
		}

		if st := byID[c.GetId()]; st != nil {
			info.Memory, info.CPUNanos = st.GetMemoryUsage(), st.GetCpuUsage()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].PodNamespace+"/"+out[i].Pod != out[j].PodNamespace+"/"+out[j].Pod {
			return out[i].PodNamespace+"/"+out[i].Pod < out[j].PodNamespace+"/"+out[j].Pod
		}

		return out[i].Name < out[j].Name
	})

	return out
}

// splitPodID splits "namespace/pod" (Talos CRI pod ids); a bare id is a pod name.
func splitPodID(id string) (string, string) {
	if ns, pod, ok := strings.Cut(id, "/"); ok {
		return ns, pod
	}

	return "", id
}
