package ichorgo

import (
	"math"
	"time"
)

// demoCgroups is a small Talos cgroup tree: CPU and IO grow with uptime so the apps show
// rates, and a gently waving IO pressure on etcd gives the pressure card something to say.
func demoCgroups(n nodeOverview, at int64) cgroupReport {
	seconds := time.Since(demoBoot).Seconds()
	wave := 0.5 + 0.5*math.Sin(seconds/40)
	cpu := func(share float64) uint64 { return uint64(seconds * share * 1e6) }
	io := func(perSecond float64) uint64 { return uint64(seconds * perSecond) }
	psi := func(cpu, memory, io float64) *cgroupPressure {
		return &cgroupPressure{CPU: cgroupPSI{Some10: cpu, Some60: cpu}, Memory: cgroupPSI{Some10: memory, Some60: memory}, IO: cgroupPSI{Some10: io, Some60: io, Full10: io / 2, Full60: io / 2}}
	}

	podruntime := []*cgroupNode{
		{Name: "kubelet", MemCurrent: 96 << 20, CPUUsec: cpu(0.08), IORead: io(2e3), Pressure: psi(0, 0, 0)},
		{Name: "runtime", MemCurrent: 80 << 20, CPUUsec: cpu(0.03), IOWrite: io(8e3), Pressure: psi(0, 0, 0)},
	}
	if n.Role == "controlplane" {
		podruntime = append([]*cgroupNode{{Name: "etcd", MemCurrent: 180 << 20, CPUUsec: cpu(0.12), IOWrite: io(4e4), Pressure: psi(0, 0, 3*wave)}}, podruntime...)
	}

	pod := func(name, container string, mem, limit uint64, share float64, ooms uint64) *cgroupNode {
		return &cgroupNode{Name: name, MemCurrent: mem + 256<<10, CPUUsec: cpu(share), OOMKills: ooms, Pressure: psi(0.2*wave, 0, 0), children: map[string]*cgroupNode{
			"c1": {Name: container, MemCurrent: mem, MemMax: limit, CPUUsec: cpu(share), Pressure: psi(0.2*wave, 0, 0)},
			"c0": {Name: "sandbox", MemCurrent: 256 << 10, Pressure: psi(0, 0, 0)},
		}}
	}

	root := &cgroupNode{Name: ".", Pressure: psi(0.4*wave, 0, 1.5*wave), children: map[string]*cgroupNode{
		"init": {Name: "init", MemCurrent: 60 << 20, CPUUsec: cpu(0.01), Pressure: psi(0, 0, 0)},
		"system": {Name: "system", MemCurrent: 120 << 20, CPUUsec: cpu(0.02), Pressure: psi(0, 0, 0), children: map[string]*cgroupNode{
			"apid":   {Name: "apid", MemCurrent: 24 << 20, MemMax: 40 << 20, CPUUsec: cpu(0.01), Pressure: psi(0, 0, 0)},
			"trustd": {Name: "trustd", MemCurrent: 12 << 20, MemMax: 24 << 20, CPUUsec: cpu(0.001), Pressure: psi(0, 0, 0)},
			"udevd":  {Name: "udevd", MemCurrent: 8 << 20, CPUUsec: cpu(0.001), Pressure: psi(0, 0, 0)},
		}},
		"podruntime": {Name: "podruntime", MemCurrent: 356 << 20, CPUUsec: cpu(0.23), Pressure: psi(0, 0, 2*wave), children: cgroupChildren(podruntime)},
		"kubepods": {Name: "kubepods", MemCurrent: 120 << 20, MemMax: n.MemTotal * 9 / 10, CPUUsec: cpu(0.3), Pressure: psi(0.3*wave, 0, 0), children: map[string]*cgroupNode{
			"burstable": {Name: "burstable", MemCurrent: 120 << 20, CPUUsec: cpu(0.3), Pressure: psi(0.3*wave, 0, 0), children: map[string]*cgroupNode{
				"poda": pod("kube-system/coredns-demo", "coredns", 24<<20, 170<<20, 0.02, 0),
				"podb": pod("demo/hello-ichor", "web", 61<<20, 64<<20, 0.05, 2),
			}},
		}},
	}}

	return buildCgroupReport(root, map[string]string{}, at)
}

func cgroupChildren(nodes []*cgroupNode) map[string]*cgroupNode {
	out := make(map[string]*cgroupNode, len(nodes))
	for _, node := range nodes {
		out[node.Name] = node
	}

	return out
}
