package ichorgo

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// cgroupsTimeout bounds the Copy of the whole hierarchy, a few MB on a busy node.
const cgroupsTimeout = 20 * time.Second

// memoryLimitAlert is the share of memory.max past which the kernel starts reclaiming
// hard and an OOM kill is near.
const memoryLimitAlert = 0.9

type cgroupReport struct {
	At       int64           `json:"at"`       // unix milliseconds, to turn CPU/IO deltas into rates
	Pressure cgroupPressure  `json:"pressure"` // whole node, from the root cgroup
	Hotspots []cgroupHotspot `json:"hotspots"`
	Alerts   []cgroupAlert   `json:"alerts"`
	Root     *cgroupNode     `json:"root"`
}

// cgroupHotspot is the workload whose tasks stall the most on a resource (PSI some avg10).
// Parent tells same-named services apart (system/runtime and podruntime/runtime).
type cgroupHotspot struct {
	Resource string  `json:"resource"` // cpu, memory, io
	Name     string  `json:"name"`
	Parent   string  `json:"parent"`
	Some10   float64 `json:"some10"`
}

// cgroupAlert is a workload that was OOM-killed since boot, or is near its memory limit.
type cgroupAlert struct {
	Kind    string  `json:"kind"` // oomKill, memoryLimit
	Name    string  `json:"name"`
	Parent  string  `json:"parent"`
	Count   uint64  `json:"count,omitempty"`   // oomKill
	Percent float64 `json:"percent,omitempty"` // memoryLimit: current / max
}

// NodeCgroups reads node's cgroup tree with pressure (PSI), memory, CPU and IO per system
// service, pod and container, like `talosctl cgroups`. Copying /sys/fs/cgroup needs os:admin.
func NodeCgroups(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeCgroups", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, cgroupsTimeout, func(ctx context.Context, s *session) (string, error) {
		r, err := s.client.Copy(ctx, constants.CgroupMountPath)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		defer r.Close() //nolint:errcheck

		root, err := cgroupTreeFromTarGz(r)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		// Without CRI the pods keep their pod<uid> and container id names.
		names := map[string]string{}

		if list, err := s.client.Containers(ctx, constants.K8sContainerdNamespace, common.ContainerDriver_CRI); err == nil {
			for _, c := range first(list.GetMessages()).GetContainers() {
				names = cgroupNames(names, c.GetUid(), c.GetPodId(), c.GetInternalId(), c.GetName())
			}
		}

		return toJSON(buildCgroupReport(root, names, time.Now().UnixMilli()))
	})
}

// cgroupNames maps kubelet's pod<uid> and container id directories to pod and container
// names, like talosctl: the pause container is "sandbox".
func cgroupNames(names map[string]string, uid, podID, internalID, name string) map[string]string {
	out := make(map[string]string, len(names)+2)

	for k, v := range names {
		out[k] = v
	}

	if uid != "" && podID != "" {
		out["pod"+uid] = podID
	}

	if internalID != "" {
		if podID == name {
			out[internalID] = "sandbox"
		} else {
			out[internalID] = name
		}
	}

	return out
}

func buildCgroupReport(root *cgroupNode, names map[string]string, at int64) cgroupReport {
	finishCgroup(root, names, 0, "")

	report := cgroupReport{At: at, Hotspots: []cgroupHotspot{}, Alerts: []cgroupAlert{}, Root: root}
	if root.Pressure != nil {
		report.Pressure = *root.Pressure
	}

	walkCgroups(root, "", func(n *cgroupNode, parent string) {
		// A pod's parent is its QoS class (burstable…): noise next to "namespace/pod".
		if n.Kind == "pod" {
			parent = ""
		}

		report.Alerts = append(report.Alerts, cgroupAlerts(n, parent)...)

		if n.Kind == "service" || n.Kind == "pod" {
			report.Hotspots = hotter(report.Hotspots, n, parent)
		}
	})

	// The tree stops at pods: their containers' CPU and memory are the Pods tab's, and
	// their alerts were taken above.
	walkCgroups(root, "", func(n *cgroupNode, _ string) {
		if n.Kind == "pod" {
			n.Children = nil
		}
	})

	sort.SliceStable(report.Hotspots, func(i, j int) bool {
		return resourceOrder(report.Hotspots[i].Resource) < resourceOrder(report.Hotspots[j].Resource)
	})
	sort.SliceStable(report.Alerts, func(i, j int) bool { return report.Alerts[i].Kind > report.Alerts[j].Kind })

	return report
}

// finishCgroup names and classifies node's children and turns them into a sorted list.
// Kinds: init and the children of system and podruntime are Talos services; under kubepods
// (directly or in a QoS class) pod<uid> directories are pods, and their children containers.
func finishCgroup(n *cgroupNode, names map[string]string, depth int, parentKind string) {
	for raw, child := range n.children {
		switch {
		case depth == 0 && raw == "init":
			child.Kind = "service"
		case depth == 1 && (n.Name == "system" || n.Name == "podruntime"):
			child.Kind = "service"
		case strings.HasPrefix(raw, "pod") && parentKind == "group" && underKubepods(n, depth):
			child.Kind = "pod"
		case parentKind == "pod":
			child.Kind = "container"
		}

		if name, ok := names[raw]; ok {
			child.Name = name
		}

		finishCgroup(child, names, depth+1, child.Kind)
		n.Children = append(n.Children, child)
	}

	sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Name < n.Children[j].Name })
	n.children = nil
}

// underKubepods tells whether n is kubepods or one of its QoS classes.
func underKubepods(n *cgroupNode, depth int) bool {
	return (depth == 1 && n.Name == "kubepods") || (depth == 2 && (n.Name == "burstable" || n.Name == "besteffort"))
}

func walkCgroups(n *cgroupNode, parent string, fn func(*cgroupNode, string)) {
	fn(n, parent)

	for _, child := range n.Children {
		walkCgroups(child, n.Name, fn)
	}
}

// cgroupAlerts reports OOM kills once per workload (memory.events counts the subtree, so a
// pod's count already holds its containers'), and the cgroups near their memory.max: Talos
// services, containers (where Kubernetes sets limits) and kubepods (what kubelet leaves pods).
func cgroupAlerts(n *cgroupNode, parent string) []cgroupAlert {
	alerts := []cgroupAlert{}

	if n.OOMKills > 0 && (n.Kind == "service" || n.Kind == "pod") {
		alerts = append(alerts, cgroupAlert{Kind: "oomKill", Name: n.Name, Parent: parent, Count: n.OOMKills})
	}

	limited := n.Kind == "service" || n.Kind == "container" || n.Name == "kubepods"

	if limited && n.MemMax > 0 {
		if share := float64(n.MemCurrent) / float64(n.MemMax); share >= memoryLimitAlert {
			alerts = append(alerts, cgroupAlert{Kind: "memoryLimit", Name: n.Name, Parent: parent, Percent: share * 100})
		}
	}

	return alerts
}

// hotter keeps, per resource, the workload with the highest PSI some avg10 above zero.
func hotter(spots []cgroupHotspot, n *cgroupNode, parent string) []cgroupHotspot {
	if n.Pressure == nil {
		return spots
	}

	out := spots

	for _, c := range []struct {
		resource string
		some10   float64
	}{{"cpu", n.Pressure.CPU.Some10}, {"memory", n.Pressure.Memory.Some10}, {"io", n.Pressure.IO.Some10}} {
		if c.some10 <= 0 {
			continue
		}

		spot := cgroupHotspot{Resource: c.resource, Name: n.Name, Parent: parent, Some10: c.some10}
		i := indexOfResource(out, c.resource)

		switch {
		case i < 0:
			out = append(append([]cgroupHotspot{}, out...), spot)
		case out[i].Some10 < c.some10:
			out = append(append(append([]cgroupHotspot{}, out[:i]...), spot), out[i+1:]...)
		}
	}

	return out
}

func indexOfResource(spots []cgroupHotspot, resource string) int {
	for i, s := range spots {
		if s.Resource == resource {
			return i
		}
	}

	return -1
}

func resourceOrder(resource string) int {
	return strings.Index("cpu memory io", resource)
}
