package talosmobile

import "time"

// demoKubeWorkloads are the Kubernetes workloads of the built-in demo cluster.
func demoKubeWorkloads() []kubeWorkload {
	created := demoBoot.UnixMilli()
	restarted := time.Now().Add(-26 * time.Hour).UnixMilli()

	return []kubeWorkload{
		{Kind: "Deployment", Namespace: "demo", Name: "hello-ichor", Desired: 3, Ready: 3, Updated: 3, Available: 3, State: workloadReady, RestartedAt: restarted, Created: created, Images: []string{"nginx:1.27"}},
		{Kind: "Deployment", Namespace: "demo", Name: "worker", Desired: 2, Ready: 1, Updated: 2, Available: 1, State: workloadDegraded, Created: created, Images: []string{"busybox:1.37"}},
		{Kind: "StatefulSet", Namespace: "demo", Name: "postgres", Desired: 1, Ready: 1, Updated: 1, Available: 1, State: workloadReady, Created: created, Images: []string{"postgres:17"}},
		{Kind: "Deployment", Namespace: "kube-system", Name: "coredns", Desired: 2, Ready: 2, Updated: 2, Available: 2, State: workloadReady, Created: created, Images: []string{"registry.k8s.io/coredns/coredns:v1.12.0"}},
		{Kind: "DaemonSet", Namespace: "kube-system", Name: "kube-proxy", Desired: 5, Ready: 5, Updated: 5, Available: 5, State: workloadReady, Created: created, Images: []string{"registry.k8s.io/kube-proxy:v1.34.0"}},
		{Kind: "DaemonSet", Namespace: "kube-system", Name: "flannel", Desired: 5, Ready: 5, Updated: 5, Available: 5, State: workloadReady, Created: created, Images: []string{"ghcr.io/flannel-io/flannel:v0.27.0"}},
	}
}

// demoPods are the Kubernetes pods of the built-in demo cluster.
func demoPods() []kubePod {
	created := demoBoot.UnixMilli()
	pod := func(ns, name, status, node, owner, image string, ready, restarts int) kubePod {
		return kubePod{
			Namespace: ns, Name: name, Status: status, Healthy: status == "Running" && ready == 1, Ready: ready, Containers: 1,
			Restarts: restarts, Node: node, Owner: owner, Created: created, Images: []string{image},
		}
	}

	return []kubePod{
		pod("demo", "hello-ichor-7d9c5-abcde", "Running", "demo-worker-1", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "hello-ichor-7d9c5-fghij", "Running", "demo-worker-2", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "hello-ichor-7d9c5-klmno", "Running", "demo-worker-1", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "worker-6f4b8-pqrst", "Running", "demo-worker-2", "ReplicaSet/worker-6f4b8", "busybox:1.37", 1, 0),
		pod("demo", "worker-6f4b8-uvwxy", "CrashLoopBackOff", "demo-worker-1", "ReplicaSet/worker-6f4b8", "busybox:1.37", 0, 14),
		pod("demo", "postgres-0", "Running", "demo-worker-2", "StatefulSet/postgres", "postgres:17", 1, 0),
		pod("kube-system", "coredns-5c6b7-aaaaa", "Running", "demo-cp-1", "ReplicaSet/coredns-5c6b7", "registry.k8s.io/coredns/coredns:v1.12.0", 1, 0),
		pod("kube-system", "coredns-5c6b7-bbbbb", "Running", "demo-cp-2", "ReplicaSet/coredns-5c6b7", "registry.k8s.io/coredns/coredns:v1.12.0", 1, 0),
	}
}
