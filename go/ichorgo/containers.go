package ichorgo

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

type containerList struct {
	At         int64           `json:"at"` // unix ms, to turn CPU nanosecond deltas into %
	Containers []containerInfo `json:"containers"`
}

type containerInfo struct {
	ID           string `json:"id"`
	PodNamespace string `json:"podNamespace"`
	Pod          string `json:"pod"`
	Name         string `json:"name"`
	Image        string `json:"image"`
	Status       string `json:"status"`
	Pid          uint32 `json:"pid"`
	Memory       uint64 `json:"memory"` // bytes
	CPUNanos     uint64 `json:"cpuNanos"`
}

// NodeContainers lists the Kubernetes containers on node with their memory and cumulative
// CPU time, like `talosctl containers -k` + `talosctl stats -k` (os:reader).
func NodeContainers(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeContainers", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		nodeCtx := withNode(ctx, node)

		list, err := s.client.Containers(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		stats, _ := s.client.Stats(nodeCtx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI) //nolint:errcheck

		return toJSON(containerList{
			At:         time.Now().UnixMilli(),
			Containers: mergeContainers(first(list.GetMessages()).GetContainers(), first(stats.GetMessages()).GetStats()),
		})
	})
}

// mergeContainers joins container info with stats by id, dropping pause (sandbox)
// containers, and sorts by pod then container name.
func mergeContainers(infos []*machineapi.ContainerInfo, stats []*machineapi.Stat) []containerInfo {
	byID := make(map[string]*machineapi.Stat, len(stats))
	for _, st := range stats {
		byID[st.GetId()] = st
	}

	out := make([]containerInfo, 0, len(infos))

	for _, c := range infos {
		// The pod sandbox ("pause") container has the pod's own id and no workload.
		if c.GetId() == c.GetPodId() || strings.Contains(c.GetImage(), "/pause:") {
			continue
		}

		ns, pod := splitPodID(c.GetPodId())
		info := containerInfo{
			ID:           c.GetId(),
			PodNamespace: ns,
			Pod:          pod,
			Name:         c.GetName(),
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
