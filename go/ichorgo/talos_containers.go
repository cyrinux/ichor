package ichorgo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// Talos containers (Talos 1.15): OCI containers declared with ContainerConfig documents and
// run by Talos itself, without Kubernetes. This client's machinery (1.14) has no Go type for
// their status, so it is read untyped and its YAML spec decoded by key. Every upstream name
// this reader depends on is in this block, so the machinery bump can re-check them in one
// place (pkg/machinery/resources/containers and pkg/machinery/constants at v1.15.0-beta.1,
// internal/app/machined/internal/server/v1alpha1 for the Restart and Logs requests):
//
//	ContainerStatuses.containers.talos.dev, namespace "containers" (ID: the ContainerConfig
//	  name): state (pending|pulling|starting|running|exited|backoff|stopping|succeeded|
//	  failed), health (pending|pulling|healthy|degraded), image, pid, exitCode,
//	  restartCount, error, waitingFor[], generation, errorLastLog
//	containerd namespace "taloscontainers" (on the CRI containerd, driver CONTAINERD): one
//	  container per instance, ID "<name>-<generation>" (InstanceID)
//	Restart{namespace: taloscontainers, driver: CONTAINERD, id: the instance ID}
//	Logs{namespace: taloscontainers, id: the ContainerConfig name} (its log outlives
//	  instances: "taloscontainers-<name>" in the service log buffer)
//
// Only the status is read: ContainerSpecs and ContainerInstanceSpecs carry the environment
// variables (sensitive). The enums are written as text (enumer -text); their numeric values
// are mapped too, in case a node writes them as numbers.
const (
	containerNSTalos         = constants.TalosContainersContainerdNamespace
	talosContainersNamespace = "containers"
	containerStatusType      = "ContainerStatuses.containers.talos.dev"
)

// Upstream enum names, in their numeric order.
var (
	talosContainerStates  = []string{"pending", "pulling", "starting", "running", "exited", "backoff", "stopping", "succeeded", "failed"}
	talosContainerHealths = []string{"pending", "pulling", "healthy", "degraded"}
)

const (
	// talosLastLogMax bounds errorLastLog (workload output) in bytes.
	talosLastLogMax = 4096
	// talosContainerRole is the role a Talos container name is learned with: screenshot mode
	// shows it as ctr-N.
	talosContainerRole = "container"
	// talosContainerRestartsAlert is the restart count from which a Talos container is
	// reported as restarting repeatedly (the apps' background check uses the same number).
	talosContainerRestartsAlert = 5
)

// readTalosContainers lists node's Talos containers, joined with their containerd container
// and its stats; ctx targets node. Like the system containers they are a bonus: nil on a node
// without the status type (Talos older than 1.15) or that cannot list them.
func readTalosContainers(ctx context.Context, s *session, node string) []containerInfo {
	list, _ := listTalosContainers(ctx, s, node) //nolint:errcheck

	return list
}

// listTalosContainers is readTalosContainers with the error of a node that could not be read;
// nil, nil on a node without Talos containers or without the status type.
func listTalosContainers(ctx context.Context, s *session, node string) ([]containerInfo, error) {
	known, err := s.hasResourceType(ctx, node, containerStatusType)
	if err != nil || !known {
		return nil, err
	}

	items, err := s.client.COSI.List(ctx, resource.NewMetadata(talosContainersNamespace, containerStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}

	statuses := make(map[string]map[string]any, len(items.Items))

	for _, r := range items.Items {
		if spec, err := resourceSpecMap(r); err == nil {
			statuses[r.Metadata().ID()] = spec
		}
	}

	if len(statuses) == 0 {
		return nil, nil
	}

	// Pid, memory and CPU come from containerd; without them the status still shows.
	list, _ := s.client.Containers(ctx, containerNSTalos, common.ContainerDriver_CONTAINERD) //nolint:errcheck
	stats, _ := s.client.Stats(ctx, containerNSTalos, common.ContainerDriver_CONTAINERD)     //nolint:errcheck

	return buildTalosContainers(statuses, first(list.GetMessages()).GetContainers(), first(stats.GetMessages()).GetStats()), nil
}

// buildTalosContainers maps the statuses (by ContainerConfig name) with the containerd
// container of each one's current instance, sorted by name. A containerd container without a
// status (an instance being removed) is not listed.
func buildTalosContainers(statuses map[string]map[string]any, infos []*machineapi.ContainerInfo, stats []*machineapi.Stat) []containerInfo {
	byID := make(map[string]*machineapi.ContainerInfo, len(infos))
	for _, c := range infos {
		byID[c.GetId()] = c
	}

	statByID := make(map[string]*machineapi.Stat, len(stats))
	for _, st := range stats {
		statByID[st.GetId()] = st
	}

	out := make([]containerInfo, 0, len(statuses))

	for name, spec := range statuses {
		instance := talosInstanceID(name, spec, infos)
		out = append(out, mapTalosContainer(name, spec, byID[instance], statByID[instance]))
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return learnTalosContainerNames(out)
}

// mapTalosContainer maps the status of the container name; info and stat (its containerd
// container) may be nil. Its id is its name: logs and restarts are asked by name.
func mapTalosContainer(name string, spec map[string]any, info *machineapi.ContainerInfo, stat *machineapi.Stat) containerInfo {
	out := containerInfo{
		ID: name, Namespace: containerNSTalos, Name: name,
		Image:        specString(spec, "image"),
		Status:       specEnum(spec, "state", talosContainerStates),
		Health:       specEnum(spec, "health", talosContainerHealths),
		RestartCount: uint64(max(specIntOr0(spec, "restartCount"), 0)),
		ExitCode:     int32(specIntOr0(spec, "exitCode")),
		Error:        strings.TrimSpace(specString(spec, "error")),
		WaitingFor:   specStrings(spec, "waitingFor"),
		LastLog:      cleanLastLog(specString(spec, "errorLastLog")),
	}

	if pid := specIntOr0(spec, "pid"); pid > 0 && pid <= int64(^uint32(0)) {
		out.Pid = uint32(pid)
	}

	if info != nil {
		if out.Image == "" {
			out.Image = info.GetImage()
		}

		if out.Pid == 0 {
			out.Pid = info.GetPid()
		}
	}

	if stat != nil {
		out.Memory, out.CPUNanos = stat.GetMemoryUsage(), stat.GetCpuUsage()
	}

	return out
}

// talosInstanceID is the containerd id of name's current instance: "<name>-<generation>" from
// its status, else the newest "<name>-<n>" containerd lists, else "".
func talosInstanceID(name string, spec map[string]any, infos []*machineapi.ContainerInfo) string {
	if gen := specIntOr0(spec, "generation"); gen > 0 {
		return fmt.Sprintf("%s-%d", name, gen)
	}

	best, newest := "", int64(-1)

	for _, c := range infos {
		suffix, ok := strings.CutPrefix(c.GetId(), name+"-")
		if !ok {
			continue
		}

		if n, err := strconv.ParseInt(suffix, 10, 64); err == nil && n > newest {
			best, newest = c.GetId(), n
		}
	}

	return best
}

// cleanLastLog trims errorLastLog, drops carriage returns and clips it to talosLastLogMax.
func cleanLastLog(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", ""))

	if len(s) > talosLastLogMax {
		const ellipsis = "…"

		cut := talosLastLogMax - len(ellipsis)
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}

		s = s[:cut] + ellipsis
	}

	return s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// learnTalosContainerNames teaches the screenshot mask the containers' names (ctr-N), which
// operators choose and may name a customer or a site. Their last log line is workload output:
// it is masked like any text the app shows.
func learnTalosContainerNames(list []containerInfo) []containerInfo {
	for _, c := range list {
		privacy.learnHost(c.Name, talosContainerRole)
	}

	return list
}

// resolveTalosRestartID is the instance a restart of the Talos container id stops: its
// current instance when id is a ContainerConfig name with a status, else id as given (an
// instance id). ctx targets the node.
func resolveTalosRestartID(ctx context.Context, c *client.Client, id string) (string, error) {
	r, err := c.COSI.Get(ctx, resource.NewMetadata(talosContainersNamespace, containerStatusType, id, resource.VersionUndefined))
	if err != nil {
		if state.IsNotFoundError(err) {
			return id, nil
		}

		return "", err
	}

	spec, err := resourceSpecMap(r)
	if err != nil {
		return "", err
	}

	if instance := talosInstanceID(id, spec, nil); instance != "" {
		return instance, nil
	}

	return id, nil
}
