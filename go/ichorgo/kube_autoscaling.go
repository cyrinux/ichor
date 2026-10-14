package ichorgo

// The autoscalers' API groups. HorizontalPodAutoscalers are core (autoscaling/v2); the
// three others are optional CRD groups, each read in its preferred version.
const (
	groupKEDA      = "keda.sh"
	groupVPA       = "autoscaling.k8s.io"
	groupKarpenter = "karpenter.sh"
)

// What drives a workload's replica count, and the state an autoscaler is in. The states are
// listed in the order the screens show them: problems first.
const (
	autoscaledByHPA  = "hpa"
	autoscaledByKEDA = "keda"
	autoscaledByVPA  = "vpa"

	autoscaleNotReady = "notReady"
	autoscaleAtMax    = "atMax"
	autoscaleLimited  = "limited"
	autoscaleFallback = "fallback"
	autoscalePaused   = "paused"
	autoscaleOK       = "ok"
)

const (
	// autoscalingRowsMax is how many rows one list keeps (the problems first); beyond it the
	// group's truncated says so.
	autoscalingRowsMax = 2000
	// autoscalingTargetsMax is how many workloads one read GETs for the VPA's live requests.
	autoscalingTargetsMax = 200
	// kedaTriggerSummaryMax is how long a trigger's summary gets (clipUTF8 cuts at bytes).
	kedaTriggerSummaryMax = 120
)

// autoscalingReport is everything that scales the cluster: HPAs, KEDA, VPA and Karpenter,
// plus a per-workload index. A group the cluster does not serve answers installed false,
// an empty list and no error; a group whose read failed keeps its error, the others show.
type autoscalingReport struct {
	HPA       hpaGroup             `json:"hpa"`
	KEDA      kedaGroup            `json:"keda"`
	VPA       vpaGroup             `json:"vpa"`
	Karpenter karpenterGroup       `json:"karpenter"`
	Workloads []autoscaledWorkload `json:"workloads"`
	// WorkloadsTruncated says the index was cut at autoscalingRowsMax.
	WorkloadsTruncated bool `json:"workloadsTruncated"`
}

// autoscalingGroupState is what every group says about itself.
type autoscalingGroupState struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Error     string `json:"error"`
	Truncated bool   `json:"truncated"`
}

type hpaGroup struct {
	autoscalingGroupState
	Items []hpaRow `json:"items"`
}

type kedaGroup struct {
	autoscalingGroupState
	ScaledObjects []scaledObjectRow `json:"scaledObjects"`
	ScaledJobs    []scaledJobRow    `json:"scaledJobs"`
}

type vpaGroup struct {
	autoscalingGroupState
	Items []vpaRow `json:"items"`
}

type karpenterGroup struct {
	autoscalingGroupState
	NodePools  []nodePoolRow  `json:"nodePools"`
	NodeClaims []nodeClaimRow `json:"nodeClaims"`
}

// autoscaleTarget is the workload an autoscaler drives.
type autoscaleTarget struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// hpaRow is one HorizontalPodAutoscaler. One that KEDA created (managedBy "keda") stays in
// the list for its metrics, linked to its ScaledObject; the workload index names the
// ScaledObject only.
type hpaRow struct {
	Namespace    string          `json:"namespace"`
	Name         string          `json:"name"`
	Target       autoscaleTarget `json:"target"`
	Min          int             `json:"min"`
	Max          int             `json:"max"`
	Current      int             `json:"current"`
	Desired      int             `json:"desired"`
	Metrics      []hpaMetric     `json:"metrics"`
	Conditions   hpaConditions   `json:"conditions"`
	AtMax        bool            `json:"atMax"`
	LastScaleAt  int64           `json:"lastScaleAt"` // unix ms, 0 when it never scaled
	ManagedBy    string          `json:"managedBy,omitempty"`
	ScaledObject string          `json:"scaledObject,omitempty"`
	State        string          `json:"state"`
}

// hpaMetric is one metric the HPA scales on: current against target, as the user reads
// them ("63%", "500m", "2Gi"); current is "-" while the metrics pipeline answers nothing.
type hpaMetric struct {
	Type    string `json:"type"` // Resource, Pods, Object, External, ContainerResource
	Name    string `json:"name"`
	Current string `json:"current"`
	Target  string `json:"target"`
}

type hpaConditions struct {
	AbleToScale    bool   `json:"ableToScale"`
	ScalingActive  bool   `json:"scalingActive"`
	ScalingLimited bool   `json:"scalingLimited"`
	Message        string `json:"message,omitempty"`
}

// scaledObjectRow is one KEDA ScaledObject; current and desired come from the HPA it owns.
type scaledObjectRow struct {
	Namespace        string          `json:"namespace"`
	Name             string          `json:"name"`
	Target           autoscaleTarget `json:"target"`
	Min              int             `json:"min"`
	Max              int             `json:"max"`
	Current          int             `json:"current"`
	Desired          int             `json:"desired"`
	Triggers         []kedaTrigger   `json:"triggers"`
	FallbackReplicas int             `json:"fallbackReplicas"`
	Paused           bool            `json:"paused"`
	PausedReplicas   *int            `json:"pausedReplicas,omitempty"` // set when paused at a count
	Ready            bool            `json:"ready"`
	Active           bool            `json:"active"`
	Fallback         bool            `json:"fallback"`
	Message          string          `json:"message,omitempty"`
	HPAName          string          `json:"hpaName,omitempty"`
	State            string          `json:"state"`
}

type scaledJobRow struct {
	Namespace       string        `json:"namespace"`
	Name            string        `json:"name"`
	Triggers        []kedaTrigger `json:"triggers"`
	MaxReplicaCount int           `json:"maxReplicaCount"`
	Paused          bool          `json:"paused"`
	Ready           bool          `json:"ready"`
	Message         string        `json:"message,omitempty"`
	State           string        `json:"state"`
}

// kedaTrigger is one trigger: its type and the one setting the user recognises (a query, a
// topic, a cron window). Never a secret: the summary only takes allow-listed keys.
type kedaTrigger struct {
	Type    string `json:"type"`
	Summary string `json:"summary,omitempty"`
}

// vpaRow is one VerticalPodAutoscaler with its recommendation per container.
type vpaRow struct {
	Namespace              string          `json:"namespace"`
	Name                   string          `json:"name"`
	Target                 autoscaleTarget `json:"target"`
	UpdateMode             string          `json:"updateMode"`
	Containers             []vpaContainer  `json:"containers"`
	RecommendationProvided bool            `json:"recommendationProvided"`
	Message                string          `json:"message,omitempty"`
	State                  string          `json:"state"`
}

// vpaContainer is the recommender's answer for one container next to its live requests
// ("" when the target could not be read).
type vpaContainer struct {
	Name          string `json:"name"`
	CPUTarget     string `json:"cpuTarget"`
	MemoryTarget  string `json:"memoryTarget"`
	CPULower      string `json:"cpuLower"`
	CPUUpper      string `json:"cpuUpper"`
	MemoryLower   string `json:"memoryLower"`
	MemoryUpper   string `json:"memoryUpper"`
	CPURequest    string `json:"cpuRequest"`
	MemoryRequest string `json:"memoryRequest"`
}

// nodePoolRow is one Karpenter NodePool: its limits against what it runs, and how it may
// disrupt its nodes.
type nodePoolRow struct {
	Name          string             `json:"name"`
	Limits        nodePoolLimits     `json:"limits"`
	Resources     nodePoolResources  `json:"resources"`
	AtLimit       bool               `json:"atLimit"`
	Disruption    nodePoolDisruption `json:"disruption"`
	Weight        int                `json:"weight"`
	Ready         bool               `json:"ready"`
	Message       string             `json:"message,omitempty"`
	CapacityTypes []string           `json:"capacityTypes"`
}

type nodePoolLimits struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

type nodePoolResources struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Nodes  int    `json:"nodes"`
}

type nodePoolDisruption struct {
	ConsolidationPolicy string           `json:"consolidationPolicy"`
	ConsolidateAfter    string           `json:"consolidateAfter"`
	Budgets             []nodePoolBudget `json:"budgets"`
}

type nodePoolBudget struct {
	Nodes    string   `json:"nodes"`
	Schedule string   `json:"schedule,omitempty"`
	Duration string   `json:"duration,omitempty"`
	Reasons  []string `json:"reasons"`
}

// nodeClaimRow is one Karpenter NodeClaim: a node being launched, running, or on its way out.
type nodeClaimRow struct {
	Name         string `json:"name"`
	Pool         string `json:"pool"`
	NodeName     string `json:"nodeName"`
	InstanceType string `json:"instanceType"`
	CapacityType string `json:"capacityType"`
	Launched     bool   `json:"launched"`
	Registered   bool   `json:"registered"`
	Initialized  bool   `json:"initialized"`
	Ready        bool   `json:"ready"`
	Message      string `json:"message,omitempty"`
	Created      int64  `json:"created"` // unix ms
	Pending      bool   `json:"pending"`
	Drifted      bool   `json:"drifted"`
	Disrupting   bool   `json:"disrupting"`
}

// autoscaledWorkload is one (workload, autoscaler kind) pair: what the workload sheet names.
type autoscaledWorkload struct {
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	By         string `json:"by"` // hpa, keda or vpa
	Autoscaler string `json:"autoscaler"`
	Min        int    `json:"min"`
	Max        int    `json:"max"`
	Current    int    `json:"current"`
	Desired    int    `json:"desired"`
	Paused     bool   `json:"paused"`
	State      string `json:"state"`
}

// KubeAutoscaling reads what scales the cluster (os:admin): HorizontalPodAutoscalers, KEDA
// ScaledObjects and ScaledJobs, VPA recommendations against the live requests, Karpenter
// NodePools and NodeClaims, and the per-workload index {"hpa","keda","vpa","karpenter",
// "workloads"} (see autoscalingReport). Whole cluster; the apps filter by namespace.
// kubeServer: see KubePods.
func KubeAutoscaling(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoAutoscaling, readAutoscaling)
}
