package ichorgo

import "time"

// demoTalosContainersHost is the demo node that runs Talos containers.
const demoTalosContainersHost = demoVMHost

// demoTalosContainers are the Talos containers of a demo node: on demoTalosContainersHost a
// healthy proxy and an agent that keeps crashing (with its last log line); none elsewhere.
// Everything invented.
func demoTalosContainers(n nodeOverview) []containerInfo {
	if n.Hostname != demoTalosContainersHost {
		return nil
	}

	up := time.Since(demoBoot).Seconds()

	return learnTalosContainerNames([]containerInfo{
		{
			ID: "edge-proxy", Namespace: containerNSTalos, Name: "edge-proxy",
			Image:  "registry.example.org/edge/proxy:3.1",
			Status: "running", Health: "healthy", Pid: 410, Memory: 48 << 20, CPUNanos: uint64(up * 8e6),
			WaitingFor: []string{},
		},
		{
			ID: "sensor-agent", Namespace: containerNSTalos, Name: "sensor-agent",
			Image:  "registry.example.org/edge/sensor-agent:1.4",
			Status: "backoff", Health: "degraded", RestartCount: 12, ExitCode: 1,
			Error:      "task exited with code 1",
			WaitingFor: []string{},
			LastLog:    "fatal: cannot open /var/lib/sensor-agent/state.db: no such file or directory",
		},
	})
}
