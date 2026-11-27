package ichorgo

// demoVMHost is the demo node that runs the Talos hypervisor's virtual machines.
const demoVMHost = "demo-worker-1"

// demoNodeVMs is what a demo node answers to NodeVirtualMachines: the first worker runs three
// VMs (one running with two disks and an address, one stopped on purpose, one that should run
// and cannot) and has one content library; the other nodes have none. Everything invented.
func demoNodeVMs(n nodeOverview) nodeVMs {
	out := emptyNodeVMs()
	out.Supported = true

	if n.Hostname != demoVMHost {
		return out
	}

	out.VMs = []talosVM{
		{
			Name: "build-runner", PowerState: "running", WantedPowerState: "running", Stage: "ready", Running: true,
			CPUs: 4, MemoryBytes: 8 << 30, Profile: "",
			Addresses: []string{"198.51.100.21/24"},
			Interfaces: []talosVMInterface{
				{Name: "eth0", MAC: "52:54:00:12:34:01", Addresses: []string{"198.51.100.21/24"}},
			},
			Disks: []talosVMDisk{
				{Name: "cache", Pool: "vm-pool", Volume: "build-runner-cache", Format: "qcow2", SizeBytes: 100 << 30, Phase: "ready"},
				{Name: "root", Pool: "vm-pool", Volume: "build-runner-root", Format: "qcow2", SizeBytes: 40 << 30, Phase: "ready"},
			},
		},
		{
			Name: "legacy-app", PowerState: "stopped", WantedPowerState: "stopped", Stage: "ready",
			CPUs: 2, MemoryBytes: 4 << 30, Profile: "",
			Addresses: []string{}, Interfaces: []talosVMInterface{},
			Disks: []talosVMDisk{
				{Name: "root", Pool: "vm-pool", Volume: "legacy-app-root", Format: "raw", SizeBytes: 20 << 30, Phase: "ready"},
			},
		},
		{
			Name: "test-desktop", PowerState: "stopped", WantedPowerState: "running", Stage: "error",
			Error: `disk "root": image "desktop.qcow2" not found in content library "images"`,
			CPUs:  2, MemoryBytes: 4 << 30, Profile: "",
			Addresses: []string{}, Interfaces: []talosVMInterface{},
			Disks: []talosVMDisk{
				{
					Name: "root", Pool: "vm-pool", Volume: "test-desktop-root", Format: "qcow2", SizeBytes: 32 << 30, Phase: "notReady",
					Error: `image "desktop.qcow2" not found in content library "images"`,
				},
			},
		},
	}
	out.Libraries = []contentLibrary{{Name: "images", Path: "/var/mnt/vm-images", Phase: "ready"}}

	return learnVMNames(out)
}

// demoClusterVMs is ClusterVirtualMachines for the demo cluster.
func demoClusterVMs(configYAML, contextName string) (string, error) {
	name, _, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	nodes := demoNodes()
	result := nodeReadings[nodeVMsEntry]{Context: name, Nodes: make([]nodeVMsEntry, 0, len(nodes))}

	for _, n := range nodes {
		result.Nodes = append(result.Nodes, nodeVMsEntry{Node: n.Node, Hostname: n.Hostname, nodeVMs: demoNodeVMs(n)})
	}

	return toJSON(result)
}
