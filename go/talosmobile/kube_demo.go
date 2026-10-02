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
