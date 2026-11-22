package ichorgo

import (
	"fmt"
	"time"
)

// demoKubeWorkloads are the Kubernetes workloads of the built-in demo cluster.
func demoKubeWorkloads() []kubeWorkload {
	created := demoBoot.UnixMilli()
	restarted := time.Now().Add(-26 * time.Hour).UnixMilli()

	return []kubeWorkload{
		{Kind: "Deployment", Namespace: "demo", Name: "hello-ichor", Desired: 3, Ready: 3, Updated: 3, Available: 3, State: workloadReady, RestartedAt: restarted, Created: created, Images: []string{"nginx:1.27"},
			Autoscaler: &workloadAutoscaler{By: "hpa", Name: "hello-ichor", Min: 1, Max: 5, Current: 3, Desired: 3, State: "ok"}},
		{Kind: "Deployment", Namespace: "demo", Name: "worker", Desired: 2, Ready: 1, Updated: 2, Available: 1, State: workloadDegraded, Created: created, Images: []string{"busybox:1.37"},
			Autoscaler: &workloadAutoscaler{By: "keda", Name: "worker-so", Min: 0, Max: 10, Current: 2, Desired: 2, Paused: true, State: "paused"}},
		{Kind: "StatefulSet", Namespace: "demo", Name: "postgres", Desired: 1, Ready: 1, Updated: 1, Available: 1, State: workloadReady, Created: created, Images: []string{"postgres:17"}},
		{Kind: "Deployment", Namespace: "kube-system", Name: "coredns", Desired: 2, Ready: 2, Updated: 2, Available: 2, State: workloadReady, Created: created, Images: []string{"registry.k8s.io/coredns/coredns:v1.12.0"}},
		{Kind: "DaemonSet", Namespace: "kube-system", Name: "kube-proxy", Desired: 5, Ready: 5, Updated: 5, Available: 5, State: workloadReady, Created: created, Images: []string{"registry.k8s.io/kube-proxy:v1.34.0"}},
		{Kind: "DaemonSet", Namespace: "kube-system", Name: "flannel", Desired: 5, Ready: 5, Updated: 5, Available: 5, State: workloadReady, Created: created, Images: []string{"ghcr.io/flannel-io/flannel:v0.27.0"}},
		// The inventory's idle apps: one that cannot be scheduled, one scaled to zero.
		{Kind: "Deployment", Namespace: "home", Name: "mealie", Desired: 1, Updated: 1, State: workloadDegraded, Created: created, Images: []string{demoNotStartingImage}},
		{Kind: "Deployment", Namespace: "tools", Name: "stirling-pdf", State: workloadScaledDown, Created: created, Images: []string{"docker.io/stirlingtools/stirling-pdf:1.4.0"}},
	}
}

// The demo's app whose pod finds no node, and the scheduler's words for it.
const (
	demoNotStartingImage  = "ghcr.io/mealie-recipes/mealie:v3.3.2"
	demoNotStartingReason = "0/5 nodes are available: 5 node(s) didn't match Pod's node affinity/selector. preemption: 0/5 nodes are available: 5 Preemption is not helpful for scheduling."
)

// demoPods are the Kubernetes pods of the built-in demo cluster.
// The demo's pod that Docker Hub's pull limit stops, and the kubelet's words for it.
const (
	demoPullImage   = "nginx:1.28"
	demoPullMessage = `failed to pull and unpack image "docker.io/library/nginx:1.28": failed to copy: httpReadSeeker: failed open: unexpected status code https://registry-1.docker.io/v2/library/nginx/manifests/sha256:4f1c: 429 Too Many Requests - Server message: toomanyrequests: You have reached your unauthenticated pull rate limit.`
)

func demoPullRateLimited(p kubePod) kubePod {
	p.Waiting = demoPullMessage
	p.Pull = explainPull("ImagePullBackOff", demoPullMessage, demoPullImage, nil)

	return p
}

// demoWithSidecar adds a second, ready container to a running demo pod (a metrics exporter).
func demoWithSidecar(p kubePod, image string) kubePod {
	p.Containers, p.Ready, p.Images = 2, 2, append(p.Images, image)

	return p
}

func demoPods() []kubePod {
	created := demoBoot.UnixMilli()
	pod := func(ns, name, status, node, owner, image string, ready, restarts int) kubePod {
		return kubePod{
			Namespace: ns, Name: name, Status: status, Healthy: status == "Running" && ready == 1, Ready: ready, Containers: 1,
			Restarts: restarts, Node: node, Owner: owner, Created: created, Images: []string{image},
		}
	}

	pods := []kubePod{
		pod("demo", "hello-ichor-7d9c5-abcde", "Running", "demo-worker-1", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "hello-ichor-7d9c5-fghij", "Running", "demo-worker-2", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "hello-ichor-7d9c5-klmno", "Running", "demo-worker-1", "ReplicaSet/hello-ichor-7d9c5", "nginx:1.27", 1, 0),
		pod("demo", "worker-6f4b8-pqrst", "Running", "demo-worker-2", "ReplicaSet/worker-6f4b8", "busybox:1.37", 1, 0),
		pod("demo", "worker-6f4b8-uvwxy", "CrashLoopBackOff", "demo-worker-1", "ReplicaSet/worker-6f4b8", "busybox:1.37", 0, 14),
		demoPullRateLimited(pod("demo", "reports-5b7c9-xkq2p", "ImagePullBackOff", "demo-worker-2", "ReplicaSet/reports-5b7c9", demoPullImage, 0, 0)),
		demoWithSidecar(pod("demo", "postgres-0", "Running", "demo-worker-2", "StatefulSet/postgres", "postgres:17", 1, 0), "quay.io/prometheuscommunity/postgres-exporter:v0.17.1"),
		pod("kube-system", "coredns-5c6b7-aaaaa", "Running", "demo-cp-1", "ReplicaSet/coredns-5c6b7", "registry.k8s.io/coredns/coredns:v1.12.0", 1, 0),
		pod("kube-system", "coredns-5c6b7-bbbbb", "Running", "demo-cp-2", "ReplicaSet/coredns-5c6b7", "registry.k8s.io/coredns/coredns:v1.12.0", 1, 0),
		pod("home", "home-assistant-0", "Running", "demo-worker-2", "StatefulSet/home-assistant", "ghcr.io/home-assistant/home-assistant:2025.10.1", 1, 0),
		pod("garage", "garage-4kq7z", "Running", "demo-worker-1", "StatefulSet/garage", "dxflrs/garage:v2.3.0", 1, 0),
		pod("garage-archive", "garage-archive-0", "Running", "demo-worker-2", "StatefulSet/garage-archive", "dxflrs/garage:v2.3.0", 1, 0),
	}

	return append(pods, demoCnpgPods(pod)...)
}

// demoCnpgInstances are the demo CloudNativePG clusters' instances, ready or not, in the order
// demoDataServices lists them: the pods the PostgreSQL list links to.
var demoCnpgInstances = []struct {
	cluster string
	ready   []bool
}{
	{"orders-db", []bool{false, false}},
	{"app-db", []bool{true, true, false}},
	{"wiki-db", []bool{true, true}},
	{"dns-db", []bool{true, true, true}},
	{"auth-db", []bool{true, true, true}},
}

// demoCnpgPods are the CloudNativePG instance pods: a ready one runs on demo-worker-<n>, one
// that is not ready waits unscheduled (Pending), as the PostgreSQL list shows them.
func demoCnpgPods(pod func(ns, name, status, node, owner, image string, ready, restarts int) kubePod) []kubePod {
	var out []kubePod

	for _, c := range demoCnpgInstances {
		for i, ready := range c.ready {
			name, status, node, r := fmt.Sprintf("%s-%d", c.cluster, i+1), "Pending", "", 0
			if ready {
				status, node, r = "Running", fmt.Sprintf("demo-worker-%d", i+1), 1
			}

			out = append(out, pod("demo", name, status, node, "Cluster/"+c.cluster, "ghcr.io/cloudnative-pg/postgresql:17.6", r, 0))
		}
	}

	return out
}
