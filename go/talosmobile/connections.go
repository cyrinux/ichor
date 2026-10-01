package talosmobile

import (
	"context"
	"errors"
	"sort"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

type connectionInfo struct {
	Protocol    string `json:"protocol"` // tcp, tcp6, udp, udp6
	LocalIP     string `json:"localIp"`
	LocalPort   uint32 `json:"localPort"`
	RemoteIP    string `json:"remoteIp"`
	RemotePort  uint32 `json:"remotePort"`
	State       string `json:"state"` // LISTEN, ESTABLISHED, ...
	Listening   bool   `json:"listening"`
	Pid         uint32 `json:"pid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
}

// NodeConnections lists node's TCP/UDP sockets (IPv4 and IPv6, host network namespace),
// listening ones first by port, like `talosctl netstat -a -p` (os:reader).
func NodeConnections(configYAML, contextName, node string) (string, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		resp, err := s.client.Netstat(withNode(ctx, node), &machineapi.NetstatRequest{
			Filter:  machineapi.NetstatRequest_ALL,
			Feature: &machineapi.NetstatRequest_Feature{Pid: true},
			L4Proto: &machineapi.NetstatRequest_L4Proto{Tcp: true, Tcp6: true, Udp: true, Udp6: true},
			Netns:   &machineapi.NetstatRequest_NetNS{Hostnetwork: true},
		})
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		return toJSON(mapConnections(first(resp.GetMessages()).GetConnectrecord()))
	})
}

func mapConnections(in []*machineapi.ConnectRecord) []connectionInfo {
	out := make([]connectionInfo, 0, len(in))

	for _, r := range in {
		info := connectionInfo{
			Protocol:   r.GetL4Proto(),
			LocalIP:    r.GetLocalip(),
			LocalPort:  r.GetLocalport(),
			RemoteIP:   r.GetRemoteip(),
			RemotePort: r.GetRemoteport(),
			State:      r.GetState().String(),
		}

		// UDP has no LISTEN state: an unconnected socket (no remote port) is a listener.
		info.Listening = r.GetState() == machineapi.ConnectRecord_LISTEN ||
			(strings.HasPrefix(info.Protocol, "udp") && info.RemotePort == 0)

		if p := r.GetProcess(); p != nil {
			info.Pid, info.ProcessName = p.GetPid(), p.GetName()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]

		switch {
		case a.Listening != b.Listening:
			return a.Listening
		case a.LocalPort != b.LocalPort:
			return a.LocalPort < b.LocalPort
		case a.Protocol != b.Protocol:
			return a.Protocol < b.Protocol
		case a.RemoteIP != b.RemoteIP:
			return a.RemoteIP < b.RemoteIP
		default:
			return a.RemotePort < b.RemotePort
		}
	})

	return out
}
