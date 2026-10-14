package ichorgo

import (
	"context"
	"strings"
)

// KubeVirt's API groups: the virtual machines themselves, the Containerized Data Importer
// that fills their disks, and Harvester (which runs plain KubeVirt VMs and adds its own CRDs).
const (
	groupKubeVirt  = "kubevirt.io"
	groupCDI       = "cdi.kubevirt.io"
	groupHarvester = "harvesterhci.io"
)

// What the counts and the order of the list group a VM's printable status into.
const (
	vmStateFailing  = "failing"
	vmStateChanging = "changing"
	vmStateRunning  = "running"
	vmStateStopped  = "stopped"
)

const (
	// vmConditionsMax is how many conditions one VM row keeps (Ready and Failure first).
	vmConditionsMax = 6
	// vmMigrationRecent is how long (ms) a finished migration stays on its VM's row.
	vmMigrationRecent = 3600_000
)

// vmReport is the KubeVirt virtual machines of the cluster (or one namespace). VMs is empty
// (not nil, Installed false) when the cluster does not serve kubevirt.io.
type vmReport struct {
	Installed bool   `json:"installed"`
	Harvester bool   `json:"harvester"`
	Version   string `json:"version"` // the served kubevirt.io version, "" when not installed
	// Error is a list that failed; what could be read still shows.
	Error      string   `json:"error,omitempty"`
	VMs        []vmRow  `json:"vms"`
	Namespaces []string `json:"namespaces"`
	Counts     vmCounts `json:"counts"`
}

// vmRow is one VirtualMachine, with what its running instance, its newest migration and its
// disk imports say.
type vmRow struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	RunStrategy string `json:"runStrategy"`
	// Status is KubeVirt's printableStatus (Running, Stopped, CrashLoopBackOff, …), passed
	// through as written; State is our grouping of it.
	Status        string        `json:"status"`
	State         string        `json:"state"`
	Ready         bool          `json:"ready"`
	Created       bool          `json:"created"`
	PendingChange string        `json:"pendingChange"` // Start or Stop, "" when none
	Conditions    []vmCondition `json:"conditions"`
	CPU           int           `json:"cpu"`    // vCPUs, 0 when the template sets none
	Memory        string        `json:"memory"` // as written ("4Gi"), "" when unset
	Node          string        `json:"node"`
	Phase         string        `json:"phase"` // the instance's phase, "" without one
	IPs           []string      `json:"ips"`
	GuestOS       string        `json:"guestOS"`
	LauncherImage string        `json:"launcherImage"`
	Migration     *vmMigration  `json:"migration"`
	Disks         []vmDisk      `json:"disks"`
}

type vmCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// vmMigration is the newest VirtualMachineInstanceMigration of a VM: one in flight, or one
// that finished less than an hour ago.
type vmMigration struct {
	Name       string `json:"name"`
	Phase      string `json:"phase"`
	SourceNode string `json:"sourceNode"`
	TargetNode string `json:"targetNode"`
	StartedAt  int64  `json:"startedAt"` // unix ms
	Completed  bool   `json:"completed"`
	Failed     bool   `json:"failed"`
}

// vmDisk is one DataVolume a VM boots from: its import, clone or upload progress.
type vmDisk struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Progress  string `json:"progress"` // "45.00%", "N/A" or ""
	Importing bool   `json:"importing"`
}

type vmCounts struct {
	Running   int `json:"running"`
	Stopped   int `json:"stopped"`
	Failing   int `json:"failing"`
	Migrating int `json:"migrating"`
	Importing int `json:"importing"`
}

// KubeVirtualMachines reads the KubeVirt (and Harvester) virtual machines of namespace, every
// namespace when "" (os:admin): {"installed","harvester","version","error","vms","namespaces",
// "counts"} (see vmReport), failing VMs first. A cluster without KubeVirt answers
// {"installed":false} and no error. kubeServer: see KubePods.
func KubeVirtualMachines(configYAML, contextName, kubeServer, namespace string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace = privacy.reveal(strings.TrimSpace(namespace))

	if err := validateNamespace(namespace); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() vmReport { return demoVirtualMachines(namespace) },
		func(ctx context.Context, k *kubeClient) (vmReport, error) {
			return readVirtualMachines(ctx, k, namespace)
		})
}
