package ichorgo

import (
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
)

func TestMapProcesses(t *testing.T) {
	got := mapProcesses([]*machineapi.ProcessInfo{
		{Pid: 2, Ppid: 1, State: "S", Threads: 4, CpuTime: 1.5, ResidentMemory: 2048, VirtualMemory: 4096, Command: "kubelet", Args: "/usr/local/bin/kubelet --config x"},
		{Pid: 1, State: "S", Threads: 1, Command: "init", Executable: "/sbin/init"},
	})

	if len(got) != 2 || got[0].Pid != 1 || got[1].Pid != 2 {
		t.Fatalf("not sorted by pid: %+v", got)
	}

	if got[1].Command != "kubelet" || got[1].Args != "/usr/local/bin/kubelet --config x" || got[1].RSS != 2048 || got[1].CPUTime != 1.5 {
		t.Errorf("kubelet = %+v", got[1])
	}

	// No args: fall back to the executable, then to the command.
	if got[0].Args != "/sbin/init" {
		t.Errorf("init args = %q", got[0].Args)
	}
}
