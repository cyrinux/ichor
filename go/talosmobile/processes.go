package talosmobile

import (
	"context"
	"errors"
	"sort"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type processList struct {
	At        int64         `json:"at"` // unix milliseconds, to turn CPU time deltas into %
	Processes []processInfo `json:"processes"`
}

type processInfo struct {
	Pid     int32   `json:"pid"`
	Ppid    int32   `json:"ppid"`
	State   string  `json:"state"`
	Threads int32   `json:"threads"`
	CPUTime float64 `json:"cpuTime"` // cumulative seconds
	RSS     uint64  `json:"rss"`     // bytes
	VMS     uint64  `json:"vms"`     // bytes
	Command string  `json:"command"`
	Args    string  `json:"args"`
}

// NodeProcesses lists node's processes, like `talosctl processes` (os:reader).
func NodeProcesses(configYAML, contextName, node string) (string, error) {
	return withSession(configYAML, contextName, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		resp, err := s.client.Processes(client.WithNode(ctx, node))
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		return toJSON(processList{
			At:        time.Now().UnixMilli(),
			Processes: mapProcesses(first(resp.GetMessages()).GetProcesses()),
		})
	})
}

func mapProcesses(in []*machineapi.ProcessInfo) []processInfo {
	out := make([]processInfo, 0, len(in))

	for _, p := range in {
		args := p.GetArgs()
		if args == "" {
			args = p.GetExecutable()
		}

		if args == "" {
			args = p.GetCommand()
		}

		out = append(out, processInfo{
			Pid:     p.GetPid(),
			Ppid:    p.GetPpid(),
			State:   p.GetState(),
			Threads: p.GetThreads(),
			CPUTime: p.GetCpuTime(),
			RSS:     p.GetResidentMemory(),
			VMS:     p.GetVirtualMemory(),
			Command: p.GetCommand(),
			Args:    args,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })

	return out
}
