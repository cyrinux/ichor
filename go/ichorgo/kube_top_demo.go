package ichorgo

// Demo `kubectl top`: fixed, plausible usage on the demo nodes and pods, so screenshots show
// bars at a few levels (one pod over its request, one near its memory limit).

// demoNodeLoad is the share of allocatable CPU and memory each demo node uses, by position.
var demoNodeLoad = []struct{ cpu, memory float64 }{{0.22, 0.41}, {0.35, 0.58}, {0.64, 0.72}, {0.18, 0.33}, {0.83, 0.91}}

func demoTopNodesFor(target kubeTarget) func() kubeTopNodes {
	return func() kubeTopNodes {
		out := kubeTopNodes{Available: true, Nodes: []kubeTopNode{}}

		for i, n := range demoKubeNodesFor(target)().Nodes {
			load := demoNodeLoad[i%len(demoNodeLoad)]
			t := kubeTopNode{
				Name: n.Name, CPU: n.CPU * load.cpu, Memory: n.Memory * load.memory,
				CPUAllocatable: n.CPU, MemoryAllocatable: n.Memory,
			}
			t.CPUPercent, t.MemoryPercent = percentOf(t.CPU, t.CPUAllocatable), percentOf(t.Memory, t.MemoryAllocatable)
			out.Nodes = append(out.Nodes, t)
		}

		return out
	}
}

// demoPodUsage is each demo pod's usage and resources, by name; unlisted pods idle.
var demoPodUsage = map[string]kubeTopPod{
	"hello-ichor-7d9c5-abcde": {CPU: 0.012, Memory: 24 << 20, CPURequest: 0.05, CPULimit: 0.2, MemoryRequest: 32 << 20, MemoryLimit: 64 << 20},
	"hello-ichor-7d9c5-fghij": {CPU: 0.009, Memory: 22 << 20, CPURequest: 0.05, CPULimit: 0.2, MemoryRequest: 32 << 20, MemoryLimit: 64 << 20},
	"hello-ichor-7d9c5-klmno": {CPU: 0.071, Memory: 58 << 20, CPURequest: 0.05, CPULimit: 0.2, MemoryRequest: 32 << 20, MemoryLimit: 64 << 20},
	"worker-6f4b8-pqrst":      {CPU: 0.31, Memory: 96 << 20, CPURequest: 0.1, MemoryRequest: 128 << 20},
	"worker-6f4b8-uvwxy":      {CPU: 0.004, Memory: 3 << 20, CPURequest: 0.1, MemoryRequest: 128 << 20},
	"postgres-0":              {CPU: 0.18, Memory: 410 << 20, CPURequest: 0.25, CPULimit: 1, MemoryRequest: 512 << 20, MemoryLimit: 1 << 30},
	"coredns-5c6b7-aaaaa":     {CPU: 0.006, Memory: 18 << 20, CPURequest: 0.1, MemoryRequest: 70 << 20, MemoryLimit: 170 << 20},
	"coredns-5c6b7-bbbbb":     {CPU: 0.005, Memory: 17 << 20, CPURequest: 0.1, MemoryRequest: 70 << 20, MemoryLimit: 170 << 20},
}

func demoTopPods(namespace string) func() kubeTopPods {
	return func() kubeTopPods {
		out := kubeTopPods{Available: true, Pods: []kubeTopPod{}}

		for _, p := range inNamespace(demoPods(), namespace, func(p kubePod) string { return p.Namespace }) {
			t, ok := demoPodUsage[p.Name]
			if !ok {
				t = kubeTopPod{CPU: 0.001, Memory: 8 << 20}
			}

			t.Namespace, t.Name, t.Node = p.Namespace, p.Name, p.Node
			out.Pods = append(out.Pods, t)
		}

		return out
	}
}
