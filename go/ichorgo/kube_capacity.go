package ichorgo

import (
	"context"
	"sync"
)

// Cluster capacity: what is free per node and for the cluster, who gets killed first (QoS,
// priority, memory limits), how many more replicas of a workload fit, and whether the pods
// of any one node would fit on the others (N+1). One read of every node and pod, plus the
// optional PriorityClasses, PodDisruptionBudgets, ReplicaSets and metrics-server usage.

// The risk word of a workload, worst first.
const (
	riskOOM        = "oom"        // OOM-killed already, or a pod uses 90 % of its memory limit
	riskDiskNear   = "diskNear"   // a pod uses 90 % of its ephemeral-storage limit: evicted past it
	riskEvictFirst = "evictFirst" // BestEffort: the first evicted under node pressure
	riskUnbounded  = "unbounded"  // Burstable with no memory limit: may take a node's memory
	riskOK         = "ok"
)

// oomNearPercent is the share of its memory limit a pod may use before it counts as at risk,
// diskNearPercent the same for its ephemeral-storage limit.
const (
	oomNearPercent  = 90.0
	diskNearPercent = 90.0
)

// capacityAssumptions say what the fit estimates leave out, for the apps to word them as such.
var capacityAssumptions = []string{
	"Fit counts requests against allocatable only: affinity, topology spread, volume node affinity and extended resources are ignored.",
	"Memory limits add up the pods that set one: a pod without a limit is not bounded.",
	"Ephemeral storage counts ephemeral-storage requests against allocatable; disk free space is the kubelet's nodefs, evictions start under 10% free (15% for images).",
}

// kubeCapacity is KubeCapacity's answer. CPU is in cores, memory in bytes. MetricsAvailable
// false: no metrics-server (usage fields 0); MetricsForbidden: the credentials may not read
// it. PriorityClassesRead / PDBsRead false: those lists could not be read (priorities then
// come from the pods alone, no PDB is named). DiskMeasured: at least one kubelet gave its
// node's disk; DiskForbidden: the credentials may not read nodes/proxy.
type kubeCapacity struct {
	MetricsAvailable    bool                    `json:"metricsAvailable"`
	MetricsForbidden    bool                    `json:"metricsForbidden"`
	DiskMeasured        bool                    `json:"diskMeasured"`
	DiskForbidden       bool                    `json:"diskForbidden"`
	PriorityClassesRead bool                    `json:"priorityClassesRead"`
	PDBsRead            bool                    `json:"pdbsRead"`
	PriorityClasses     []capacityPriorityClass `json:"priorityClasses"`
	Totals              capacityTotals          `json:"totals"`
	Nodes               []capacityNode          `json:"nodes"`
	// Workloads, what goes first first: lowest priority, no class before a class of the same
	// value, then the most memory requested.
	Workloads   []capacityWorkload `json:"workloads"`
	NPlusOne    capacityNPlusOne   `json:"nPlusOne"`
	Assumptions []string           `json:"assumptions"`
}

type capacityPriorityClass struct {
	Name             string `json:"name"`
	Value            int32  `json:"value"`
	GlobalDefault    bool   `json:"globalDefault"`
	PreemptionPolicy string `json:"preemptionPolicy"`
}

type capacityTotals struct {
	CPUAllocatable    float64 `json:"cpuAllocatable"`
	CPURequests       float64 `json:"cpuRequests"`
	CPULimits         float64 `json:"cpuLimits"`
	CPUUsage          float64 `json:"cpuUsage"`
	MemoryAllocatable float64 `json:"memoryAllocatable"`
	MemoryRequests    float64 `json:"memoryRequests"`
	MemoryLimits      float64 `json:"memoryLimits"`
	MemoryUsage       float64 `json:"memoryUsage"`
	Pods              int     `json:"pods"`
	PodCapacity       int     `json:"podCapacity"`
	Nodes             int     `json:"nodes"`
	SchedulableNodes  int     `json:"schedulableNodes"`
	// Ephemeral storage in bytes; the disk sums the measured nodes only.
	EphemeralAllocatable float64 `json:"ephemeralAllocatable"`
	EphemeralRequests    float64 `json:"ephemeralRequests"`
	DiskCapacity         float64 `json:"diskCapacity"`
	DiskAvailable        float64 `json:"diskAvailable"`
	DiskMeasuredNodes    int     `json:"diskMeasuredNodes"`
}

// capacityNode is a checkup node with its limits, usage and free room. Free is allocatable
// minus requests, never negative. Overcommitted: its pods' memory limits exceed what it has,
// so they cannot all reach them at once without an OOM kill. Ephemeral storage is in bytes,
// 0 allocatable when the node does not say (unknown, not full). The disk is the kubelet's
// nodefs and imagefs (often one filesystem); DiskMeasured is false when it did not answer.
type capacityNode struct {
	checkupNode

	CPULimits     float64 `json:"cpuLimits"`
	MemoryLimits  float64 `json:"memoryLimits"`
	CPUUsage      float64 `json:"cpuUsage"`
	MemoryUsage   float64 `json:"memoryUsage"`
	CPUFree       float64 `json:"cpuFree"`
	MemoryFree    float64 `json:"memoryFree"`
	PodsFree      int     `json:"podsFree"`
	Overcommitted bool    `json:"overcommitted"`

	EphemeralAllocatable float64 `json:"ephemeralAllocatable"`
	EphemeralRequests    float64 `json:"ephemeralRequests"`
	EphemeralFree        float64 `json:"ephemeralFree"`
	DiskPressure         bool    `json:"diskPressure"`
	DiskMeasured         bool    `json:"diskMeasured"`
	DiskCapacity         float64 `json:"diskCapacity"`
	DiskAvailable        float64 `json:"diskAvailable"`
	ImageFsCapacity      float64 `json:"imageFsCapacity"`
	ImageFsAvailable     float64 `json:"imageFsAvailable"`
}

// capacityWorkload is the running pods of one controller. Requests and limits are one
// replica's (the largest of its pods; a limit is 0 when a pod has none), usage the sum.
// PDBAllowed is -1 when no PDB covers it. MoreFit: replicas of this size that fit today;
// MoreFitNA for a DaemonSet or a pod no controller scales.
type capacityWorkload struct {
	Kind              string  `json:"kind"`
	Namespace         string  `json:"namespace"`
	Name              string  `json:"name"`
	Replicas          int     `json:"replicas"`
	CPURequest        float64 `json:"cpuRequest"`
	CPULimit          float64 `json:"cpuLimit"`
	MemoryRequest     float64 `json:"memoryRequest"`
	MemoryLimit       float64 `json:"memoryLimit"`
	CPUUsage          float64 `json:"cpuUsage"`
	MemoryUsage       float64 `json:"memoryUsage"`
	PriorityClassName string  `json:"priorityClassName"`
	Priority          int32   `json:"priority"`
	NoPriorityClass   bool    `json:"noPriorityClass"`
	QoS               string  `json:"qos"`
	PDB               string  `json:"pdb"`
	PDBAllowed        int     `json:"pdbAllowed"`
	OOMKills          int     `json:"oomKills"`
	Risk              string  `json:"risk"`
	MoreFit           int     `json:"moreFit"`
	MoreFitNA         bool    `json:"moreFitNA"`
	// Ephemeral storage in bytes: one replica's request and limit (0 when unbounded), the
	// pods' summed use (0 when no kubelet measured it).
	EphemeralRequest float64 `json:"ephemeralRequest"`
	EphemeralLimit   float64 `json:"ephemeralLimit"`
	EphemeralUsage   float64 `json:"ephemeralUsage"`
}

// capacityNPlusOne: OK when the pods of every schedulable node would fit on the others (by
// requests, first fit, largest first). Otherwise Node is the first that fails, with how many
// of its pods would stay Pending and the requests they add up to.
type capacityNPlusOne struct {
	OK          bool    `json:"ok"`
	Node        string  `json:"node"`
	PendingPods int     `json:"pendingPods"`
	ShortCPU    float64 `json:"shortCPU"`
	ShortMemory float64 `json:"shortMemory"`
	// ShortEphemeral is the ephemeral storage those pods request, in bytes.
	ShortEphemeral float64 `json:"shortEphemeral"`
}

type priorityClassObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Value            int32  `json:"value"`
	GlobalDefault    bool   `json:"globalDefault"`
	PreemptionPolicy string `json:"preemptionPolicy"`
}

type capacityReplicaSet struct {
	Metadata struct {
		Name            string `json:"name"`
		Namespace       string `json:"namespace"`
		OwnerReferences []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller *bool  `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
}

// KubeCapacity is the cluster's capacity as JSON (see kubeCapacity): free CPU, memory and pod
// slots per node and in total, each workload's priority, QoS class, OOM risk and how many
// more replicas fit, and the N+1 verdict. It only reads. RBAC: list nodes and pods
// (required); list replicasets, priorityclasses, poddisruptionbudgets and metrics.k8s.io
// nodes and pods (optional: without them the answer says what is missing).
// kubeServer: see KubePods.
func KubeCapacity(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoKubeCapacity, readCapacity)
}

// capacityInput is what readCapacity reads, each list with its error.
type capacityInput struct {
	nodes      []checkNodeObject
	nodesErr   error
	pods       []checkPod
	podsErr    error
	classes    []priorityClassObject
	classesErr error
	pdbs       []pdbObject
	pdbsErr    error
	sets       []capacityReplicaSet
	topNodes   kubeTopNodes
	topPods    kubeTopPods
	kubelets   []kubeletRead // the ready nodes' stats/summary, when asked for
}

func readCapacity(ctx context.Context, k *kubeClient) (kubeCapacity, error) {
	in, err := readCapacityInput(ctx, k, true)
	if err != nil {
		return kubeCapacity{}, err
	}

	return buildCapacity(in), nil
}

// readCapacityInput reads every list the capacity and the node-loss simulation need, in
// parallel; only the nodes and the pods are required. withDisk also asks each ready node's
// kubelet for its disk, once the nodes are known.
func readCapacityInput(ctx context.Context, k *kubeClient, withDisk bool) (capacityInput, error) {
	var (
		in capacityInput
		wg sync.WaitGroup
	)

	nodesRead := make(chan struct{})

	wg.Go(func() {
		defer close(nodesRead)

		in.nodes, in.nodesErr = listObjects[checkNodeObject](ctx, k, "/api/v1/nodes")
	})
	wg.Go(func() {
		<-nodesRead

		if withDisk && in.nodesErr == nil {
			in.kubelets = readKubeletSummaries(ctx, k, in.nodes, nil)
		}
	})
	wg.Go(func() { in.pods, in.podsErr = listCheckPods(ctx, k) })
	wg.Go(func() {
		in.classes, in.classesErr = listObjects[priorityClassObject](ctx, k, "/apis/scheduling.k8s.io/v1/priorityclasses")
	})
	wg.Go(func() { in.pdbs, in.pdbsErr = listObjects[pdbObject](ctx, k, "/apis/policy/v1/poddisruptionbudgets") })
	// Without the ReplicaSets a Deployment's pods group under their ReplicaSet.
	wg.Go(func() { in.sets, _ = listObjects[capacityReplicaSet](ctx, k, "/apis/apps/v1/replicasets") }) //nolint:errcheck
	wg.Go(func() { in.topNodes = readCapacityNodeMetrics(ctx, k) })
	wg.Go(func() {
		// Every namespace: the usage only, one list.
		top, err := readTopPods(ctx, k, "", "")
		if err != nil {
			top = kubeTopPods{}
		}

		in.topPods = top
	})
	wg.Wait()

	if in.nodesErr != nil {
		return in, in.nodesErr
	}

	if in.podsErr != nil {
		return in, in.podsErr
	}

	return in, nil
}

// readCapacityNodeMetrics is the nodes' usage alone (the nodes themselves are read once, by
// readCapacity); a refusal or any failure leaves it unavailable.
func readCapacityNodeMetrics(ctx context.Context, k *kubeClient) kubeTopNodes {
	var metrics kubeList[nodeMetrics]
	if err := getList(ctx, k, metricsGroup+"/nodes", &metrics); err != nil {
		top, err := metricsUnavailable[kubeTopNodes](err, func(forbidden bool) kubeTopNodes {
			return kubeTopNodes{Forbidden: forbidden}
		})
		if err != nil {
			return kubeTopNodes{}
		}

		return top
	}

	out := kubeTopNodes{Available: true}
	for _, m := range metrics.Items {
		out.Nodes = append(out.Nodes, kubeTopNode{Name: m.Metadata.Name, CPU: parseQuantity(m.Usage.CPU), Memory: parseQuantity(m.Usage.Memory)})
	}

	return out
}

// buildCapacity turns what was read into the answer; pure, for the tests.
func buildCapacity(in capacityInput) kubeCapacity {
	out := kubeCapacity{
		MetricsAvailable: in.topNodes.Available, MetricsForbidden: in.topNodes.Forbidden || in.topPods.Forbidden,
		PriorityClassesRead: in.classesErr == nil, PDBsRead: in.pdbsErr == nil,
		PriorityClasses: []capacityPriorityClass{}, Assumptions: capacityAssumptions,
	}

	for _, c := range in.classes {
		out.PriorityClasses = append(out.PriorityClasses, capacityPriorityClass{
			Name: c.Metadata.Name, Value: c.Value, GlobalDefault: c.GlobalDefault, PreemptionPolicy: c.PreemptionPolicy,
		})
	}

	pods := resolvePriorities(in.pods, in.classes)
	usage := map[string]kubeTopPod{}

	for _, p := range in.topPods.Pods {
		usage[p.Namespace+"/"+p.Name] = p
	}

	disk := measureNodeDisk(in.kubelets)
	out.DiskMeasured, out.DiskForbidden = disk.measured, disk.forbidden
	out.Nodes = withNodeDisk(capacityNodes(in.nodes, pods, in.topNodes), disk)
	out.Totals = capacityTotalsOf(out.Nodes)
	out.Workloads = capacityWorkloads(pods, in.sets, in.pdbs, usage, disk.ephemeralUse, out.Nodes)
	out.NPlusOne = nPlusOne(out.Nodes, pods)

	return out
}

// resolvePriorities sets each pod's priority the API server left unset (an odd cluster
// without the Priority admission plugin): its class's value, else the global default's, else 0.
func resolvePriorities(pods []checkPod, classes []priorityClassObject) []checkPod {
	values := map[string]int32{}

	var fallback int32

	for _, c := range classes {
		values[c.Metadata.Name] = c.Value
		if c.GlobalDefault {
			fallback = c.Value
		}
	}

	out := make([]checkPod, len(pods))

	for i, p := range pods {
		if !p.prioritySet {
			if v, ok := values[p.PriorityClassName]; ok && p.PriorityClassName != "" {
				p.Priority = v
			} else {
				p.Priority = fallback
			}
		}

		out[i] = p
	}

	return out
}

// podQoS is the QoS class Kubernetes gives a pod of these containers (status.qosClass is
// only set once the pod is admitted): Guaranteed when every container's CPU and memory
// limits are set and its requests equal them (a missing request takes the limit),
// BestEffort when none sets any, Burstable otherwise.
func podQoS(containers []podContainerResources) string {
	bounded, guaranteed := false, len(containers) > 0

	for _, c := range containers {
		for _, resource := range []string{"cpu", "memory"} {
			request, limit := c.Resources.Requests[resource], c.Resources.Limits[resource]
			if request != "" || limit != "" {
				bounded = true
			}

			if limit == "" || request != "" && parseQuantity(request) != parseQuantity(limit) {
				guaranteed = false
			}
		}
	}

	switch {
	case !bounded:
		return qosBestEffort
	case guaranteed:
		return qosGuaranteed
	default:
		return qosBurstable
	}
}
