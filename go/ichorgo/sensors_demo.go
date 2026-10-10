package ichorgo

// demoSensors are a demo node's sensors: every node has a CPU package, two cores, a NVMe
// drive, a fan and its rails; demo-worker-1 runs above its sensor's max with its cores
// held back under the performance governor, so the demo shows a throttled node.
func demoSensors(n nodeOverview) nodeSensors {
	hot := n.Hostname == "demo-worker-1"

	pkg, core, freq := 54.0, 51.0, int64(3100)
	if hot {
		pkg, core, freq = 88.0, 86.0, 1800
	}

	out := nodeSensors{
		Temperatures: []temperatureSensor{
			{Chip: "coretemp", Label: "Package id 0", Celsius: pkg, CritCelsius: 100, MaxCelsius: 85},
			{Chip: "coretemp", Label: "Core 0", Celsius: core, CritCelsius: 100, MaxCelsius: 85},
			{Chip: "coretemp", Label: "Core 1", Celsius: core - 2, CritCelsius: 100, MaxCelsius: 85},
			{Chip: "nvme", Label: "Composite", Celsius: 41.9, CritCelsius: 84.8, MaxCelsius: 81.8},
		},
		Fans: []fanSensor{{Chip: "nct6775", Label: "fan1", RPM: 1180}},
		Voltages: []voltageSensor{
			{Chip: "nct6775", Label: "Vcore", Volts: 0.912},
			{Chip: "nct6775", Label: "+12V", Volts: 12.096},
		},
		ThermalZones: []thermalZone{{Type: "x86_pkg_temp", Celsius: pkg}, {Type: "acpitz", Celsius: 27.8}},
		CPUFreq:      []cpuFrequency{},
		Throttle:     &throttleCounts{},
		PCI: []pciDevice{
			{ID: "0000:00:00.0", Class: "Bridge", Subclass: "Host bridge", Vendor: "Intel Corporation", Product: "Host Bridge/DRAM Registers"},
			{ID: "0000:00:1f.6", Class: "Network controller", Subclass: "Ethernet controller", Vendor: "Intel Corporation", Product: "Ethernet Connection I219-LM", Driver: "e1000e"},
			{ID: "0000:01:00.0", Class: "Mass storage controller", Subclass: "Non-Volatile memory controller", Vendor: "Samsung Electronics Co Ltd", Product: "NVMe SSD Controller", Driver: "nvme"},
		},
		Errors: map[string]string{},
	}

	for cpu := range n.CPUCount {
		out.CPUFreq = append(out.CPUFreq, cpuFrequency{CPU: cpu, CurrentMHz: freq, MinMHz: 800, MaxMHz: 3400, Governor: "performance"})
	}

	if hot {
		out.Throttle = &throttleCounts{CoreEvents: 1342, PackageEvents: 417}
	}

	out.Throttled = isThrottled(out.CPUFreq, out.Temperatures)

	return out
}
