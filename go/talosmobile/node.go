package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

type serviceInfo struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Health     string `json:"health"` // healthy | unhealthy | unknown
	Message    string `json:"message,omitempty"`
	LastEvent  string `json:"lastEvent,omitempty"`
	LastChange int64  `json:"lastChange,omitempty"`
}

type nodeResources struct {
	MemTotal     uint64       `json:"memTotal"`     // bytes
	MemAvailable uint64       `json:"memAvailable"` // bytes
	Load1        float64      `json:"load1"`
	Load5        float64      `json:"load5"`
	Load15       float64      `json:"load15"`
	BootTime     uint64       `json:"bootTime"` // unix seconds
	CPUCount     int          `json:"cpuCount"`
	CPUModel     string       `json:"cpuModel"`
	Mounts       []mountUsage `json:"mounts"`
}

type mountUsage struct {
	Filesystem string `json:"filesystem"`
	MountedOn  string `json:"mountedOn"`
	Size       uint64 `json:"size"`      // bytes
	Available  uint64 `json:"available"` // bytes
}

// NodeServices returns the JSON list of Talos services on node.
func NodeServices(configYAML, contextName, node string) (string, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		resp, err := s.client.ServiceList(client.WithNode(ctx, node))
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		msgs := resp.GetMessages()
		if len(msgs) == 0 {
			return toJSON([]serviceInfo{})
		}

		return toJSON(mapServices(msgs[0].GetServices()))
	})
}

func mapServices(in []*machineapi.ServiceInfo) []serviceInfo {
	out := make([]serviceInfo, 0, len(in))

	for _, svc := range in {
		info := serviceInfo{ID: svc.GetId(), State: svc.GetState()}
		health := svc.GetHealth()

		switch {
		case health == nil || health.GetUnknown():
			info.Health = "unknown"
		case health.GetHealthy():
			info.Health = "healthy"
		default:
			info.Health = "unhealthy"
		}

		info.Message = health.GetLastMessage()

		if ts := health.GetLastChange(); ts != nil {
			info.LastChange = ts.AsTime().Unix()
		}

		if events := svc.GetEvents().GetEvents(); len(events) > 0 {
			info.LastEvent = events[len(events)-1].GetMsg()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

// NodeResources returns JSON memory, load, CPU, uptime and disk usage for node.
// Individual RPC failures leave their section empty; only a total failure is an error.
func NodeResources(configYAML, contextName, node string) (string, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		nodeCtx := client.WithNode(ctx, node)
		mc := s.client.MachineClient
		empty := &emptypb.Empty{}

		mem, memErr := s.client.Memory(nodeCtx)
		load, loadErr := mc.LoadAvg(nodeCtx, empty)
		stat, statErr := mc.SystemStat(nodeCtx, empty)
		cpu, cpuErr := mc.CPUInfo(nodeCtx, empty)
		mounts, mountsErr := s.client.Mounts(nodeCtx)

		if memErr != nil && loadErr != nil && statErr != nil && cpuErr != nil && mountsErr != nil {
			return "", fmt.Errorf("node %s: %s", node, friendlyError(memErr))
		}

		return toJSON(buildNodeResources(
			first(mem.GetMessages()), first(load.GetMessages()), first(stat.GetMessages()),
			first(cpu.GetMessages()), first(mounts.GetMessages()),
		))
	})
}

func buildNodeResources(
	mem *machineapi.Memory, load *machineapi.LoadAvg, stat *machineapi.SystemStat,
	cpu *machineapi.CPUsInfo, mounts *machineapi.Mounts,
) nodeResources {
	const kib = 1024

	r := nodeResources{
		MemTotal:     mem.GetMeminfo().GetMemtotal() * kib,
		MemAvailable: mem.GetMeminfo().GetMemavailable() * kib,
		Load1:        load.GetLoad1(),
		Load5:        load.GetLoad5(),
		Load15:       load.GetLoad15(),
		BootTime:     stat.GetBootTime(),
		CPUCount:     len(cpu.GetCpuInfo()),
	}

	if infos := cpu.GetCpuInfo(); len(infos) > 0 {
		r.CPUModel = infos[0].GetModelName()
	}

	r.Mounts = diskMounts(mounts.GetStats())

	return r
}

// diskMounts keeps block-device filesystems only (one entry per device, shortest mount
// point), dropping pseudo filesystems and per-pod kubelet/CSI volume mounts.
func diskMounts(stats []*machineapi.MountStat) []mountUsage {
	byDevice := map[string]mountUsage{}

	for _, m := range stats {
		fs, at := m.GetFilesystem(), m.GetMountedOn()

		if m.GetSize() == 0 || !strings.HasPrefix(fs, "/dev/") || strings.HasPrefix(at, "/var/lib/kubelet/") {
			continue
		}

		if prev, ok := byDevice[fs]; ok && len(prev.MountedOn) <= len(at) {
			continue
		}

		byDevice[fs] = mountUsage{Filesystem: fs, MountedOn: at, Size: m.GetSize(), Available: m.GetAvailable()}
	}

	out := make([]mountUsage, 0, len(byDevice))
	for _, m := range byDevice {
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].MountedOn < out[j].MountedOn })

	return out
}

func first[T any](items []*T) *T {
	if len(items) == 0 {
		return nil
	}

	return items[0]
}
