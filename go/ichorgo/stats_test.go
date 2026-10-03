package ichorgo

import (
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

func TestNetworkTotalsSkipLoopbackAndVirtual(t *testing.T) {
	devices := []*machineapi.NetDev{
		{Name: "lo", RxBytes: 1000, TxBytes: 1000},
		{Name: "eth0", RxBytes: 10, TxBytes: 20},
		{Name: "enp1s0", RxBytes: 1, TxBytes: 2},
		{Name: "veth1234", RxBytes: 500, TxBytes: 500},
		{Name: "cilium_host", RxBytes: 500, TxBytes: 500},
		{Name: "flannel.1", RxBytes: 500, TxBytes: 500},
		{Name: "cni0", RxBytes: 500, TxBytes: 500},
		{Name: "lxc5678", RxBytes: 500, TxBytes: 500},
		{Name: "kube-ipvs0", RxBytes: 500, TxBytes: 500},
		{Name: "wg0", RxBytes: 3, TxBytes: 4},
	}

	rx, tx := networkTotals(devices)
	if rx != 14 || tx != 26 {
		t.Errorf("rx=%d tx=%d, want 14/26", rx, tx)
	}
}

func TestDiskTotalsWholeDisksOnly(t *testing.T) {
	devices := []*machineapi.DiskStat{
		{Name: "nvme0n1", ReadSectors: 10, WriteSectors: 20},
		{Name: "nvme0n1p1", ReadSectors: 5, WriteSectors: 5},
		{Name: "sda", ReadSectors: 1, WriteSectors: 2},
		{Name: "sda1", ReadSectors: 1, WriteSectors: 1},
		{Name: "vdb", ReadSectors: 3, WriteSectors: 4},
		{Name: "mmcblk0", ReadSectors: 1, WriteSectors: 1},
		{Name: "mmcblk0p2", ReadSectors: 9, WriteSectors: 9},
		{Name: "dm-1", ReadSectors: 100, WriteSectors: 100},
		{Name: "loop0", ReadSectors: 100, WriteSectors: 100},
	}

	read, write := diskTotals(devices)
	if read != 15*512 || write != 27*512 {
		t.Errorf("read=%d write=%d, want %d/%d", read, write, 15*512, 27*512)
	}
}

func TestCPUTimes(t *testing.T) {
	busy, total := cpuTimes(&machineapi.CPUStat{User: 3, Nice: 1, System: 2, Idle: 10, Iowait: 4, Irq: 1, SoftIrq: 1, Steal: 0})
	if busy != 8 || total != 22 {
		t.Errorf("busy=%v total=%v, want 8/22", busy, total)
	}

	if b, tt := cpuTimes(nil); b != 0 || tt != 0 {
		t.Error("nil stat must be zero")
	}
}
