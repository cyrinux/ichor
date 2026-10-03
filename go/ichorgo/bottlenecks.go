package ichorgo

import (
	"encoding/json"
	"errors"
	"math"
	"sort"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

type networkCounters struct {
	Name   string `json:"name"`
	Rx     uint64 `json:"rx"`
	Tx     uint64 `json:"tx"`
	Errors uint64 `json:"errors"`
	Drops  uint64 `json:"drops"`
}
type diskCounters struct {
	Name       string `json:"name"`
	Read       uint64 `json:"read"`
	Write      uint64 `json:"write"`
	Operations uint64 `json:"operations"`
	TimeMs     uint64 `json:"timeMs"`
	BusyMs     uint64 `json:"busyMs"`
}
type deviceRate struct {
	Name    string  `json:"name"`
	Read    float64 `json:"read"`
	Write   float64 `json:"write"`
	Errors  float64 `json:"errors"`
	Drops   float64 `json:"drops"`
	Busy    float64 `json:"busy"`
	Latency float64 `json:"latency"`
}
type bottlenecks struct {
	Wait    float64           `json:"wait"`
	Steal   float64           `json:"steal"`
	Network []deviceRate      `json:"network"`
	Disks   []deviceRate      `json:"disks"`
	Errors  map[string]string `json:"errors"`
}

func mapNetworkCounters(devices []*machineapi.NetDev) []networkCounters {
	out := []networkCounters{}
	for _, d := range devices {
		if isVirtualNet(d.GetName()) {
			continue
		}
		out = append(out, networkCounters{Name: d.GetName(), Rx: d.GetRxBytes(), Tx: d.GetTxBytes(), Errors: d.GetRxErrors() + d.GetTxErrors(), Drops: d.GetRxDropped() + d.GetTxDropped()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func mapDiskCounters(devices []*machineapi.DiskStat) []diskCounters {
	out := []diskCounters{}
	for _, d := range devices {
		if !wholeDisk.MatchString(d.GetName()) {
			continue
		}
		out = append(out, diskCounters{Name: d.GetName(), Read: d.GetReadSectors() * sectorBytes, Write: d.GetWriteSectors() * sectorBytes, Operations: d.GetReadCompleted() + d.GetWriteCompleted(), TimeMs: d.GetReadTimeMs() + d.GetWriteTimeMs(), BusyMs: d.GetIoTimeMs()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CalculateBottlenecks uses counter deltas, never totals since boot. Disk latency is
// average completed read/write time; activity is not a saturation diagnosis for NVMe.
func CalculateBottlenecks(previousJSON, currentJSON string) (out string, err error) {
	defer maskResult(&out, &err)
	var a, b nodeStats
	if err := json.Unmarshal([]byte(previousJSON), &a); err != nil {
		return "", err
	}
	if err := json.Unmarshal([]byte(currentJSON), &b); err != nil {
		return "", err
	}
	if b.At <= a.At {
		return "", errors.New("samples must have increasing timestamps")
	}
	return toJSON(calculateBottlenecks(a, b))
}
func calculateBottlenecks(a, b nodeStats) bottlenecks {
	out := bottlenecks{Network: []deviceRate{}, Disks: []deviceRate{}, Errors: b.Errors}
	if out.Errors == nil {
		out.Errors = map[string]string{}
	}
	seconds := float64(b.At-a.At) / 1000
	if seconds <= 0 || (a.BootTime != 0 && b.BootTime != 0 && a.BootTime != b.BootTime) {
		return out
	}
	delta := func(a, b uint64) float64 {
		if b < a {
			return 0
		}
		return float64(b - a)
	}
	total := b.CPUTotal - a.CPUTotal
	if total > 0 {
		out.Wait = math.Min(100, math.Max(0, (b.CPUWait-a.CPUWait)/total*100))
		out.Steal = math.Min(100, math.Max(0, (b.CPUSteal-a.CPUSteal)/total*100))
	}
	for _, bdev := range b.NetworkDevices {
		if a.Errors["network"] != "" || b.Errors["network"] != "" {
			break
		}
		for _, adev := range a.NetworkDevices {
			if adev.Name != bdev.Name {
				continue
			}
			out.Network = append(out.Network, deviceRate{Name: bdev.Name, Read: delta(adev.Rx, bdev.Rx) / seconds, Write: delta(adev.Tx, bdev.Tx) / seconds, Errors: delta(adev.Errors, bdev.Errors) / seconds, Drops: delta(adev.Drops, bdev.Drops) / seconds})
			break
		}
	}
	for _, bdev := range b.DiskDevices {
		if a.Errors["disk"] != "" || b.Errors["disk"] != "" {
			break
		}
		for _, adev := range a.DiskDevices {
			if adev.Name != bdev.Name {
				continue
			}
			ops := delta(adev.Operations, bdev.Operations)
			latency := 0.0
			if ops > 0 {
				latency = delta(adev.TimeMs, bdev.TimeMs) / ops
			}
			out.Disks = append(out.Disks, deviceRate{Name: bdev.Name, Read: delta(adev.Read, bdev.Read) / seconds, Write: delta(adev.Write, bdev.Write) / seconds, Busy: math.Min(100, delta(adev.BusyMs, bdev.BusyMs)/seconds/10), Latency: latency})
			break
		}
	}
	return out
}
