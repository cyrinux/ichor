package ichorgo

import (
	"context"
	"slices"
	"time"
)

// Cluster API's API groups: the core objects (clusters, machines, machine deployments), the
// Talos control-plane provider a Cluster's spec.controlPlaneRef points at, and the bootstrap
// provider that writes the TalosConfigs.
const (
	groupClusterAPI       = "cluster.x-k8s.io"
	groupCAPIControlPlane = "controlplane.cluster.x-k8s.io"
	groupCAPIBootstrap    = "bootstrap.cluster.x-k8s.io"
)

// groupSidero is Sidero Metal's group: the bare-metal Servers and the ServerClasses grouping
// them (metal.sidero.dev, v1alpha2; both cluster-scoped).
const groupSidero = "metal.sidero.dev"

// sideroPowerOff is a Server's status.power when its BMC says it is off.
const sideroPowerOff = "off"

// What a Cluster API or Sidero row's state amounts to, problems first.
const (
	capiHealthCritical = "critical"
	capiHealthWarning  = "warning"
	capiHealthOK       = "ok"
)

const (
	// capiListMax is how many objects of one kind the report keeps (problems first).
	capiListMax = 200
	// capiConditionMax is how many not-True conditions a row keeps.
	capiConditionMax = 6
	// capiStuckAfter is how long a Machine may sit in Pending, Provisioning, Provisioned or
	// Deleting before the reader calls it stuck.
	capiStuckAfter = 30 * time.Minute
	// capiVersionV1Beta1 is the core group's version before Cluster API 1.11; the reader maps
	// it (and the ones before it) to the v1beta1 status shape. v1beta2 is the shape since.
	capiVersionV1Beta1 = "v1beta1"
	// The Talos providers' versions: TalosControlPlane is v1alpha3, TalosConfig v1alpha3.
	capiTalosControlPlaneVersion = "v1alpha3"
	capiTalosConfigVersion       = "v1alpha3"
)

// The Cluster and Machine phases the reader reasons about (status.phase).
const (
	capiPhasePending      = "Pending"
	capiPhaseProvisioning = "Provisioning"
	capiPhaseProvisioned  = "Provisioned"
	capiPhaseDeleting     = "Deleting"
	capiPhaseFailed       = "Failed"
)

// The labels Cluster API puts on the Machines it creates.
const (
	capiClusterNameLabel  = "cluster.x-k8s.io/cluster-name"
	capiControlPlaneLabel = "cluster.x-k8s.io/control-plane"
	capiDeploymentLabel   = "cluster.x-k8s.io/deployment-name"
)

// Why a row is not ok (capiReason*): the "reasons" the report carries beside the health.
const (
	capiReasonClusterFailed           = "clusterFailed"
	capiReasonClusterProvisioning     = "clusterProvisioning"
	capiReasonClusterPaused           = "clusterPaused"
	capiReasonControlPlaneDegraded    = "controlPlaneDegraded"
	capiReasonMachineFailed           = "machineFailed"
	capiReasonMachineStuck            = "machineStuck"
	capiReasonMachineDeleting         = "machineDeleting"
	capiReasonMachineDeploymentFailed = "machineDeploymentFailed"
	capiReasonWorkersShort            = "workersShort"
	capiReasonServerNotAccepted       = "serverNotAccepted"
	capiReasonServerPoweredOff        = "serverPoweredOff"
	capiReasonServerNotReady          = "serverNotReady"
	capiReasonServerCordoned          = "serverCordoned"
)

// capiReport is the Cluster API (and Sidero Metal) state of a management cluster: the workload
// clusters it manages, their machines and machine deployments, and the bare-metal servers.
// Installed is false (and every list empty) when the cluster serves neither cluster.x-k8s.io
// nor metal.sidero.dev.
type capiReport struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"` // the served cluster.x-k8s.io version, "" without Cluster API
	// Sidero is true when the cluster serves metal.sidero.dev: the Servers are read.
	Sidero bool `json:"sidero"`
	// TalosControlPlane is true when controlplane.cluster.x-k8s.io serves TalosControlPlane.
	TalosControlPlane bool `json:"talosControlPlane"`
	// TalosBootstrap is true when the bootstrap group serves TalosConfig.
	TalosBootstrap bool `json:"talosBootstrap"`
	// Error is a list that failed; what could be read still shows.
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated"` // a kind had more than capiListMax objects

	Clusters           []capiCluster           `json:"clusters"`
	Machines           []capiMachine           `json:"machines"`
	MachineDeployments []capiMachineDeployment `json:"machineDeployments"`
	Servers            []sideroServer          `json:"servers"`
	ServerClasses      []sideroServerClass     `json:"serverClasses"`
	Counts             capiCounts              `json:"counts"`
}

// capiReplicas is a replica count set (desired, ready, available, up to date), as the Cluster's
// control plane and workers report them.
type capiReplicas struct {
	Desired   int `json:"desired"`
	Ready     int `json:"ready"`
	Available int `json:"available"`
	UpToDate  int `json:"upToDate"`
}

// capiCondition is a Cluster API condition, the four fields the app shows (the providers write
// more, the reader keeps these).
type capiCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// capiCluster is one workload cluster of the management cluster.
type capiCluster struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Phase is status.phase: Pending, Provisioning, Provisioned, Deleting, Failed, Unknown.
	Phase    string `json:"phase"`
	Paused   bool   `json:"paused"`
	Endpoint string `json:"endpoint"` // spec.controlPlaneEndpoint host:port, "" when unset
	// InfraKind and ControlPlaneKind name the providers (MetalCluster, TalosControlPlane…).
	InfraKind        string `json:"infraKind"`
	ControlPlaneKind string `json:"controlPlaneKind"`
	// InfrastructureReady and ControlPlaneReady come from the v1beta1 fields or, on v1beta2,
	// from status.initialization.
	InfrastructureReady bool `json:"infrastructureReady"`
	ControlPlaneReady   bool `json:"controlPlaneReady"`
	// ControlPlane is nil without a TalosControlPlane (v1beta1) or a readable status (v1beta2).
	ControlPlane *capiReplicas `json:"controlPlane"`
	Workers      capiReplicas  `json:"workers"`
	// Versions are the control-plane versions (status.controlPlane, else the provider's).
	Versions []string `json:"versions"`
	// Conditions are the not-True ones, at most capiConditionMax.
	Conditions     []capiCondition `json:"conditions"`
	FailureReason  string          `json:"failureReason"`
	FailureMessage string          `json:"failureMessage"`
	Health         string          `json:"health"`
	Reasons        []string        `json:"reasons"`
}

// capiMachine is one Machine of a workload cluster.
type capiMachine struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	// ControlPlane is true on a control-plane Machine (label cluster.x-k8s.io/control-plane).
	ControlPlane bool `json:"controlPlane"`
	// Deployment is the MachineDeployment's name (label cluster.x-k8s.io/deployment-name), ""
	// for a control-plane or hand-made Machine.
	Deployment string `json:"deployment"`
	// Phase is status.phase: Pending, Provisioning, Provisioned, Running, Updating, Deleting,
	// Deleted, Failed, Unknown.
	Phase      string   `json:"phase"`
	Node       string   `json:"node"` // status.nodeRef.name, "" while not joined
	Version    string   `json:"version"`
	ProviderID string   `json:"providerId"`
	Addresses  []string `json:"addresses"`
	// BootstrapReady and InfrastructureReady come from the v1beta1 fields or, on v1beta2, from
	// status.initialization.
	BootstrapReady      bool   `json:"bootstrapReady"`
	InfrastructureReady bool   `json:"infrastructureReady"`
	LastUpdated         int64  `json:"lastUpdated"` // unix ms, 0 when never
	CreatedAt           int64  `json:"createdAt"`   // unix ms
	Stuck               bool   `json:"stuck"`       // not moving for over capiStuckAfter
	FailureReason       string `json:"failureReason"`
	FailureMessage      string `json:"failureMessage"`
	// Conditions are the False ones, at most capiConditionMax.
	Conditions []capiCondition `json:"conditions"`
	Health     string          `json:"health"`
	Reasons    []string        `json:"reasons"`
}

// capiMachineDeployment is one MachineDeployment of a workload cluster.
type capiMachineDeployment struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	// Phase is status.phase: ScalingUp, ScalingDown, Running, Failed, Unknown.
	Phase       string   `json:"phase"`
	Desired     int      `json:"desired"`
	Ready       int      `json:"ready"`
	Available   int      `json:"available"`
	UpToDate    int      `json:"upToDate"`
	Unavailable int      `json:"unavailable"`
	Version     string   `json:"version"`
	Health      string   `json:"health"`
	Reasons     []string `json:"reasons"`
}

// sideroServer is one bare-metal Server Sidero Metal manages.
type sideroServer struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	// Accepted is false until a human accepts the Server: it is never used before that.
	Accepted      bool     `json:"accepted"`
	Cordoned      bool     `json:"cordoned"`
	Ready         bool     `json:"ready"`
	InUse         bool     `json:"inUse"`
	IsClean       bool     `json:"isClean"`
	Power         string   `json:"power"` // on, off, unknown
	Addresses     []string `json:"addresses"`
	ServerClasses []string `json:"serverClasses"`
	// CPU is a processor summary, "" when the hardware is unknown; MemoryBytes its memory.
	CPU         string   `json:"cpu"`
	MemoryBytes int64    `json:"memoryBytes"`
	Health      string   `json:"health"`
	Reasons     []string `json:"reasons"`
}

// sideroServerClass is one ServerClass and how many Servers it has available and in use.
type sideroServerClass struct {
	Name      string `json:"name"`
	Available int    `json:"available"`
	InUse     int    `json:"inUse"`
}

// capiCounts is how many of each kind the report holds, and how their healths split.
type capiCounts struct {
	Clusters int `json:"clusters"`
	Machines int `json:"machines"`
	Servers  int `json:"servers"`
	OK       int `json:"ok"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
}

// KubeClusterAPI reads the management cluster's Cluster API state (os:admin): the workload
// clusters with their readiness and replica counts, the machines with their phase, role, node
// and whether they are stuck, the machine deployments, and (with Sidero Metal) the bare-metal
// servers and their server classes. Twice a management cluster: {"installed","version",
// "sidero","talosControlPlane","talosBootstrap","error","truncated","clusters","machines",
// "machineDeployments","servers","serverClasses","counts"} (see capiReport), problems first in
// every list. A cluster without Cluster API or Sidero answers {"installed":false} and no error.
// kubeServer: see KubePods.
func KubeClusterAPI(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoClusterAPI,
		func(ctx context.Context, k *kubeClient) (capiReport, error) {
			return readClusterAPI(ctx, k, time.Now())
		})
}

// capiIssues collects a row's health and reasons as the checks add up.
type capiIssues struct {
	health  string
	reasons []string
}

func newCAPIssues() capiIssues {
	return capiIssues{health: capiHealthOK, reasons: []string{}}
}

// add records a reason and keeps the most serious health; the same reason twice is a no-op.
func (i *capiIssues) add(health, reason string) {
	i.health = capiWorst(i.health, health)

	if !slices.Contains(i.reasons, reason) {
		i.reasons = append(i.reasons, reason)
	}
}

// capiRank orders the healths: critical, warning, then ok.
func capiRank(health string) int {
	switch health {
	case capiHealthCritical:
		return 0
	case capiHealthWarning:
		return 1
	default:
		return 2
	}
}

// capiWorst is the more serious of two healths.
func capiWorst(a, b string) string {
	if capiRank(b) < capiRank(a) {
		return b
	}

	return a
}

// capiClusterHealth is a cluster's health: a failed or unprovisioned cluster first, then a
// paused one, then a control plane missing replicas.
func capiClusterHealth(c capiCluster) (string, []string) {
	i := newCAPIssues()

	if c.Phase == capiPhaseFailed || c.FailureReason != "" {
		i.add(capiHealthCritical, capiReasonClusterFailed)
	}

	switch c.Phase {
	case capiPhasePending, capiPhaseProvisioning:
		i.add(capiHealthWarning, capiReasonClusterProvisioning)
	}

	if c.Paused {
		i.add(capiHealthWarning, capiReasonClusterPaused)
	}

	if c.ControlPlane != nil && c.ControlPlane.Ready < c.ControlPlane.Desired {
		i.add(capiHealthWarning, capiReasonControlPlaneDegraded)
	}

	return i.health, i.reasons
}

// capiMachineHealth is a machine's health: failed first, then deleting, then stuck.
func capiMachineHealth(m capiMachine) (string, []string) {
	i := newCAPIssues()

	if m.Phase == capiPhaseFailed {
		i.add(capiHealthCritical, capiReasonMachineFailed)
	}

	if m.Phase == capiPhaseDeleting {
		i.add(capiHealthWarning, capiReasonMachineDeleting)
	}

	if m.Stuck {
		i.add(capiHealthWarning, capiReasonMachineStuck)
	}

	return i.health, i.reasons
}

// capiMachineDeploymentHealth is a machine deployment's health: failed first, then short of
// replicas.
func capiMachineDeploymentHealth(md capiMachineDeployment) (string, []string) {
	i := newCAPIssues()

	if md.Phase == capiPhaseFailed {
		i.add(capiHealthCritical, capiReasonMachineDeploymentFailed)
	}

	if md.Available < md.Desired {
		i.add(capiHealthWarning, capiReasonWorkersShort)
	}

	return i.health, i.reasons
}

// sideroServerHealth is a server's health: an in-use server powered off first, then one never
// accepted, not ready while in use, or cordoned.
func sideroServerHealth(s sideroServer) (string, []string) {
	i := newCAPIssues()

	if s.InUse && s.Power == sideroPowerOff {
		i.add(capiHealthCritical, capiReasonServerPoweredOff)
	}

	if !s.Accepted {
		i.add(capiHealthWarning, capiReasonServerNotAccepted)
	}

	if s.InUse && !s.Ready {
		i.add(capiHealthWarning, capiReasonServerNotReady)
	}

	if s.Cordoned {
		i.add(capiHealthWarning, capiReasonServerCordoned)
	}

	return i.health, i.reasons
}

// capiNegativeConditions are the v1beta2 conditions whose normal state is False (Paused,
// Deleting, a rollout or scale in progress): False there is not a problem, so they are left out.
var capiNegativeConditions = map[string]bool{
	"Paused": true, "Deleting": true, "RollingOut": true, "ScalingUp": true, "ScalingDown": true, "Remediating": true,
	"Updating": true,
}

// capiProblemConditions keeps the conditions the app shows, at most capiConditionMax: the False
// ones, and the Unknown ones too when withUnknown (clusters).
func capiProblemConditions(conds []capiCondition, withUnknown bool) []capiCondition {
	out := []capiCondition{}

	for _, c := range conds {
		if c.Status == "True" || (!withUnknown && c.Status != "False") || capiNegativeConditions[c.Type] {
			continue
		}

		out = append(out, c)
		if len(out) == capiConditionMax {
			break
		}
	}

	return out
}
