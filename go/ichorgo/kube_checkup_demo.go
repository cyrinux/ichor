package ichorgo

import (
	"strings"
	"time"
)

// demoCheckup is the demo cluster's checkup: the crash-looping worker of demoPods, and one
// finding of about every kind, so each section shows what it can say.
func demoCheckup(now time.Time) checkupReport {
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

	const gi = float64(1 << 30)

	nodes := []checkupNode{
		demoCheckupNode("demo-cp-1", "control-plane", 4, 8*gi, 1.2, 3.1*gi, 14),
		demoCheckupNode("demo-cp-2", "control-plane", 4, 8*gi, 1.1, 2.9*gi, 13),
		demoCheckupNode("demo-cp-3", "control-plane", 4, 8*gi, 1.3, 3.4*gi, 15),
		demoCheckupNode("demo-worker-1", "", 8, 16*gi, 7.4, 11.8*gi, 61),
		demoCheckupNode("demo-worker-2", "", 8, 16*gi, 5.9, 14.6*gi, 48),
	}
	nodes[4].Cordoned = true
	nodes[4].Taints = []string{taintUnschedulable + ":NoSchedule"}

	volumes := []checkupVolume{
		{Namespace: "demo", Name: "data-postgres-0", Phase: "Bound", StorageClass: "longhorn", Capacity: 20 * gi, Used: 19.3 * gi, UsedPercent: 96.5, InodesPercent: 12, Measured: true, Pod: "postgres-0", Node: "demo-worker-2"},
		{Namespace: "monitoring", Name: "prometheus-db", Phase: "Bound", StorageClass: "longhorn", Capacity: 50 * gi, Used: 43.5 * gi, UsedPercent: 87, InodesPercent: 4, Measured: true, Pod: "prometheus-0", Node: "demo-worker-1"},
		{Namespace: "demo", Name: "uploads", Phase: "Bound", StorageClass: "longhorn", Capacity: 10 * gi, Used: 2.1 * gi, UsedPercent: 21, InodesPercent: 1, Measured: true, Pod: "hello-ichor-7d9c5-abcde", Node: "demo-worker-1"},
		{Namespace: "demo", Name: "cache", Phase: "Pending", StorageClass: "fast", Capacity: 5 * gi},
	}

	releases := []checkupRelease{
		{Namespace: "monitoring", Name: "kube-prometheus-stack", Status: "pending-upgrade", Revision: 12, Updated: ago(3 * time.Hour)},
		{Namespace: "demo", Name: "hello-ichor", Status: "failed", Revision: 4, Updated: ago(26 * time.Hour)},
		{Namespace: "ingress", Name: "traefik", Status: "deployed", Revision: 7, Updated: ago(9 * 24 * time.Hour)},
		{Namespace: "kube-system", Name: "cilium", Status: "deployed", Revision: 3, Updated: ago(31 * 24 * time.Hour)},
	}

	sections := []checkupSection{
		newSection(checkWorkloads, 8, []checkupFinding{
			{Kind: findPodCrashLoop, Severity: sevCritical, Namespace: "demo", Name: "worker-6f4b8-uvwxy", Node: "demo-worker-1", Count: 14, Reason: "Error (exit 1)", Since: ago(72 * time.Hour)},
			{Kind: findPodUnschedulable, Severity: sevCritical, Namespace: "demo", Name: "batch-7c9d4-zzzzz", Reason: "Unschedulable", Message: "0/5 nodes are available: 1 node(s) were unschedulable, 3 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 1 Insufficient memory.", Since: ago(40 * time.Minute)},
			{Kind: findPodOOMKilled, Severity: sevWarning, Namespace: "monitoring", Name: "prometheus-0", Node: "demo-worker-1", Count: 3, Reason: "OOMKilled (exit 137)", Since: ago(48 * time.Hour)},
			{Kind: findJobFailed, Severity: sevWarning, Namespace: "demo", Name: "backup-29340120", Extra: "backup", Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit", Since: ago(5 * time.Hour)},
		}),
		newSection(checkEvents, 42, []checkupFinding{
			{Kind: findEvent, Severity: sevInfo, Namespace: "demo", Name: "worker-6f4b8-uvwxy", Extra: "Pod", Reason: "BackOff", Message: "Back-off restarting failed container worker in pod worker-6f4b8-uvwxy", Count: 212, Since: ago(time.Minute)},
			{Kind: findEvent, Severity: sevInfo, Namespace: "demo", Name: "batch-7c9d4-zzzzz", Extra: "Pod", Reason: "FailedScheduling", Message: "0/5 nodes are available: 1 Insufficient memory.", Count: 9, Since: ago(4 * time.Minute)},
			{Kind: findEvent, Severity: sevInfo, Namespace: "demo", Name: "cache", Extra: "PersistentVolumeClaim", Reason: "ProvisioningFailed", Message: `storageclass.storage.k8s.io "fast" not found`, Count: 31, Since: ago(6 * time.Minute)},
		}),
		newSection(checkStorage, 4, []checkupFinding{
			{Kind: findVolumeFull, Severity: sevCritical, Namespace: "demo", Name: "data-postgres-0", Node: "demo-worker-2", Extra: "postgres-0", Value: 96.5, Limit: 20 * gi},
			{Kind: findVolumeFull, Severity: sevWarning, Namespace: "monitoring", Name: "prometheus-db", Node: "demo-worker-1", Extra: "prometheus-0", Value: 87, Limit: 50 * gi},
			{Kind: findPVCPending, Severity: sevWarning, Namespace: "demo", Name: "cache", Extra: "fast", Since: ago(2 * time.Hour)},
		}),
		newSection(checkUpgrade, 1, []checkupFinding{
			{Kind: findDeprecatedAPI, Severity: sevCritical, Name: "flowschemas.v1beta3.flowcontrol.apiserver.k8s.io", Extra: "1.35"},
		}),
		newSection(checkWebhooks, 3, []checkupFinding{
			{Kind: findWebhookDown, Severity: sevCritical, Name: "policy-validator", Extra: "policy/validator", Reason: "Validating", Count: 2},
		}),
		newSection(checkCapacity, 6, []checkupFinding{
			{Kind: findNodeRequestsHigh, Severity: sevWarning, Name: "demo-worker-1", Extra: "cpu", Value: 92.5},
			{Kind: findNodeRequestsHigh, Severity: sevWarning, Name: "demo-worker-2", Extra: "memory", Value: 91.3},
			{Kind: findQuotaNearLimit, Severity: sevWarning, Namespace: "demo", Name: "compute", Extra: "requests.memory", Value: 97},
		}),
		newSection(checkNodes, 5, []checkupFinding{
			{Kind: findNodeCordoned, Severity: sevWarning, Name: "demo-worker-2", Since: ago(3 * 24 * time.Hour)},
		}),
		newSection(checkLoadBalancers, 3, []checkupFinding{
			{Kind: findLBPending, Severity: sevCritical, Namespace: "demo", Name: "hello-ichor-public", Since: ago(50 * time.Minute)},
			{Kind: findLBPoolExhausted, Severity: sevCritical, Name: "default-pool", Value: 2},
		}),
		newSection(checkTerminating, 20, []checkupFinding{
			{Kind: findNamespaceTerminating, Severity: sevWarning, Name: "old-staging", Message: "Some resources are remaining: widgets.example.com has 2 resource instances. Some content in the namespace has finalizers remaining: example.com/cleanup in 2 resource instances.", Since: ago(6 * 24 * time.Hour)},
		}),
		newSection(checkCertificates, 14, []checkupFinding{
			{Kind: findCSRPending, Severity: sevWarning, Name: "system:node:demo-worker-1", Extra: "kubernetes.io/kubelet-serving", Count: 12, Since: ago(3 * time.Hour)},
		}),
		newSection(checkSecrets, 9, []checkupFinding{
			{Kind: findExternalSecretFailed, Severity: sevWarning, Namespace: "demo", Name: "database-credentials", Extra: "vault", Reason: "SecretSyncedError", Message: "could not get secret data from provider: permission denied"},
		}),
		newSection(checkHelm, len(releases), helmFindings(releases, now)),
	}

	return checkupReport{Status: checkupVerdict(sections), KubeVersion: "v1.34.1", Sections: sections, Nodes: nodes, Volumes: volumes, Releases: releases}
}

func demoCheckupNode(name, role string, cores, memory, cpuRequests, memoryRequests float64, pods int) checkupNode {
	n := checkupNode{
		Name: name, Roles: []string{}, Ready: true, Kubelet: "v1.34.1", Taints: []string{},
		Labels:         []string{"kubernetes.io/arch=amd64", "kubernetes.io/hostname=" + name, "kubernetes.io/os=linux"},
		CPURequests:    cpuRequests,
		CPUAllocatable: cores, CPUPercent: percentOf(cpuRequests, cores),
		MemoryRequests: memoryRequests, MemoryAllocatable: memory, MemoryPercent: percentOf(memoryRequests, memory),
		Pods: pods, PodCapacity: 110,
	}

	if role != "" {
		n.Roles = []string{role}
		n.Taints = []string{roleLabelPrefix + role + ":NoSchedule"}
		n.Labels = append(n.Labels, roleLabelPrefix+role)
	}

	return n
}

// demoEvents are the demo cluster's events for an object: the crash-looping worker tells
// its story, anything else is quiet.
func demoEvents(namespace, kind, name string, now time.Time) kubeEventList {
	ago := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }

	const pod = "worker-6f4b8-uvwxy"

	all := []kubeEvent{
		{Type: "Warning", Reason: "BackOff", Message: "Back-off restarting failed container worker in pod " + pod, Kind: "Pod", Namespace: "demo", Name: pod, Count: 212, First: ago(72 * time.Hour), Last: ago(time.Minute), Source: "kubelet"},
		{Type: "Normal", Reason: "Pulled", Message: `Container image "busybox:1.37" already present on machine`, Kind: "Pod", Namespace: "demo", Name: pod, Count: 15, First: ago(72 * time.Hour), Last: ago(6 * time.Minute), Source: "kubelet"},
		{Type: "Normal", Reason: "Created", Message: "Created container: worker", Kind: "Pod", Namespace: "demo", Name: pod, Count: 15, First: ago(72 * time.Hour), Last: ago(6 * time.Minute), Source: "kubelet"},
		{Type: "Normal", Reason: "ScalingReplicaSet", Message: "Scaled up replica set worker-6f4b8 from 0 to 2", Kind: "Deployment", Namespace: "demo", Name: "worker", Count: 1, First: ago(72 * time.Hour), Last: ago(72 * time.Hour), Source: "deployment-controller"},
	}

	events := []kubeEvent{}

	for _, e := range all {
		switch {
		case e.Namespace != namespace:
		case kind != "" && (e.Kind != kind || e.Name != name):
		case kind == "" && name != "" && e.Name != name && !strings.HasPrefix(e.Name, name+"-"):
		default:
			events = append(events, e)
		}
	}

	return kubeEventList{Events: events}
}
