package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/protobuf/types/known/emptypb"
)

const statsTimeout = 6 * time.Second

// nodeStats is one sample of cumulative counters; the app derives rates from two samples.
type nodeStats struct {
	At             int64             `json:"at"`      // unix milliseconds
	CPUBusy        float64           `json:"cpuBusy"` // cumulative busy CPU time (all cores)
	CPUTotal       float64           `json:"cpuTotal"`
	CPUCount       int               `json:"cpuCount"`
	MemTotal       uint64            `json:"memTotal"` // bytes
	MemAvailable   uint64            `json:"memAvailable"`
	Load1          float64           `json:"load1"`
	NetRx          uint64            `json:"netRx"` // cumulative bytes, physical-ish interfaces only
	NetTx          uint64            `json:"netTx"`
	DiskRead       uint64            `json:"diskRead"` // cumulative bytes, whole disks only
	DiskWrite      uint64            `json:"diskWrite"`
	CPUWait        float64           `json:"cpuWait"`
	CPUSteal       float64           `json:"cpuSteal"`
	BootTime       uint64            `json:"bootTime"`
	NetworkDevices []networkCounters `json:"networkDevices"`
	DiskDevices    []diskCounters    `json:"diskDevices"`
	Errors         map[string]string `json:"errors"`
}

// NodeStats samples CPU, memory, load, network and disk counters of node for live graphs.
func NodeStats(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeStats", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, statsTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		nodeCtx := client.WithNode(ctx, node)
		mc := s.client.MachineClient
		empty := &emptypb.Empty{}

		stat, err := mc.SystemStat(nodeCtx, empty)
		err = statsResponseError(len(stat.GetMessages()), first(stat.GetMessages()).GetMetadata().GetError(), err)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		// The rest is best effort; errors identify unavailable sections.
		mem, memErr := s.client.Memory(nodeCtx)              //nolint:errcheck
		load, loadErr := mc.LoadAvg(nodeCtx, empty)          //nolint:errcheck
		net, netErr := mc.NetworkDeviceStats(nodeCtx, empty) //nolint:errcheck
		disk, diskErr := mc.DiskStats(nodeCtx, empty)        //nolint:errcheck

		sys := first(stat.GetMessages())
		busy, total := cpuTimes(sys.GetCpuTotal())
		rx, tx := networkTotals(first(net.GetMessages()).GetDevices())
		read, write := diskTotals(first(disk.GetMessages()).GetDevices())
		meminfo := first(mem.GetMessages()).GetMeminfo()

		memErr = statsResponseError(len(mem.GetMessages()), first(mem.GetMessages()).GetMetadata().GetError(), memErr)
		loadErr = statsResponseError(len(load.GetMessages()), first(load.GetMessages()).GetMetadata().GetError(), loadErr)
		netErr = statsResponseError(len(net.GetMessages()), first(net.GetMessages()).GetMetadata().GetError(), netErr)
		diskErr = statsResponseError(len(disk.GetMessages()), first(disk.GetMessages()).GetMetadata().GetError(), diskErr)
		problems := map[string]string{}
		for section, e := range map[string]error{"memory": memErr, "load": loadErr, "network": netErr, "disk": diskErr} {
			if e != nil {
				problems[section] = s.friendly(node, e)
			}
		}
		return toJSON(nodeStats{
			At:      time.Now().UnixMilli(),
			CPUWait: sys.GetCpuTotal().GetIowait(), CPUSteal: sys.GetCpuTotal().GetSteal(), BootTime: sys.GetBootTime(),
			NetworkDevices: mapNetworkCounters(first(net.GetMessages()).GetDevices()),
			DiskDevices:    mapDiskCounters(first(disk.GetMessages()).GetDevices()), Errors: problems,
			CPUBusy:      busy,
			CPUTotal:     total,
			CPUCount:     len(sys.GetCpu()),
			MemTotal:     meminfo.GetMemtotal() * 1024,
			MemAvailable: meminfo.GetMemavailable() * 1024,
			Load1:        first(load.GetMessages()).GetLoad1(),
			NetRx:        rx,
			NetTx:        tx,
			DiskRead:     read,
			DiskWrite:    write,
		})
	})
}

// cpuTimes returns busy (everything but idle and iowait) and total CPU time.
func cpuTimes(s *machineapi.CPUStat) (busy, total float64) {
	if s == nil {
		return 0, 0
	}

	total = s.GetUser() + s.GetNice() + s.GetSystem() + s.GetIdle() + s.GetIowait() +
		s.GetIrq() + s.GetSoftIrq() + s.GetSteal()

	return total - s.GetIdle() - s.GetIowait(), total
}

// Virtual interfaces would double-count pod traffic (veth + bridge + host side).
var virtualNetPrefixes = []string{"lo", "veth", "cni", "cilium", "flannel", "lxc", "kube-", "docker", "br-", "vxlan", "genev", "tunl", "dummy"}

func networkTotals(devices []*machineapi.NetDev) (rx, tx uint64) {
	for _, d := range devices {
		if isVirtualNet(d.GetName()) {
			continue
		}

		rx += d.GetRxBytes()
		tx += d.GetTxBytes()
	}

	return rx, tx
}

func isVirtualNet(name string) bool {
	for _, p := range virtualNetPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}

// Whole disks only: partitions, device-mapper and loop devices would double-count I/O.
var wholeDisk = regexp.MustCompile(`^(sd[a-z]+|vd[a-z]+|xvd[a-z]+|hd[a-z]+|nvme\d+n\d+|mmcblk\d+)$`)

const sectorBytes = 512 // /proc/diskstats sectors are always 512 bytes

func diskTotals(devices []*machineapi.DiskStat) (read, write uint64) {
	for _, d := range devices {
		if !wholeDisk.MatchString(d.GetName()) {
			continue
		}

		read += d.GetReadSectors() * sectorBytes
		write += d.GetWriteSectors() * sectorBytes
	}

	return read, write
}

// Talos node-proxy failures can arrive in metadata rather than as a gRPC error.
func statsResponseError(count int, remote string, err error) error {
	if err != nil {
		return err
	}
	if remote != "" {
		return errors.New(remote)
	}
	if count == 0 {
		return fmt.Errorf("node returned no statistics")
	}
	return nil
}
