package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// capiMeta is the metadata the reader keeps of a Cluster API object.
type capiMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp string            `json:"creationTimestamp"`
	DeletionTimestamp string            `json:"deletionTimestamp"`
}

type capiRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// capiReplicaStatus is a v1beta2 Cluster's status.controlPlane or status.workers.
type capiReplicaStatus struct {
	DesiredReplicas   *int `json:"desiredReplicas"`
	ReadyReplicas     *int `json:"readyReplicas"`
	AvailableReplicas *int `json:"availableReplicas"`
	UpToDateReplicas  *int `json:"upToDateReplicas"`
	Versions          []struct {
		Version string `json:"version"`
	} `json:"versions"`
}

// capiDeprecated is a v1beta2 object's status.deprecated: the v1beta1 failure fields.
type capiDeprecated struct {
	V1Beta1 *struct {
		FailureReason  string `json:"failureReason"`
		FailureMessage string `json:"failureMessage"`
	} `json:"v1beta1"`
}

func (d *capiDeprecated) failure() (reason, message string) {
	if d == nil || d.V1Beta1 == nil {
		return "", ""
	}

	return d.V1Beta1.FailureReason, d.V1Beta1.FailureMessage
}

// capiClusterObject decodes a Cluster of either version: the v1beta1 fields (infrastructureReady,
// controlPlaneReady, failureReason) and the v1beta2 ones (initialization, controlPlane, workers,
// deprecated) have different names, so one struct reads both.
type capiClusterObject struct {
	Metadata capiMeta `json:"metadata"`
	Spec     struct {
		Paused               *bool `json:"paused"`
		ControlPlaneEndpoint struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		} `json:"controlPlaneEndpoint"`
		ControlPlaneRef   capiRef `json:"controlPlaneRef"`
		InfrastructureRef capiRef `json:"infrastructureRef"`
	} `json:"spec"`
	Status struct {
		Phase      string          `json:"phase"`
		Conditions []capiCondition `json:"conditions"`
		// v1beta1
		InfrastructureReady bool   `json:"infrastructureReady"`
		ControlPlaneReady   bool   `json:"controlPlaneReady"`
		FailureReason       string `json:"failureReason"`
		FailureMessage      string `json:"failureMessage"`
		// v1beta2
		Initialization struct {
			InfrastructureProvisioned *bool `json:"infrastructureProvisioned"`
			ControlPlaneInitialized   *bool `json:"controlPlaneInitialized"`
		} `json:"initialization"`
		ControlPlane *capiReplicaStatus `json:"controlPlane"`
		Workers      *capiReplicaStatus `json:"workers"`
		Deprecated   *capiDeprecated    `json:"deprecated"`
	} `json:"status"`
}

// capiMachineObject decodes a Machine of either version.
type capiMachineObject struct {
	Metadata capiMeta `json:"metadata"`
	Spec     struct {
		ClusterName string `json:"clusterName"`
		Version     string `json:"version"`
		ProviderID  string `json:"providerID"`
	} `json:"spec"`
	Status struct {
		Phase   string `json:"phase"`
		NodeRef *struct {
			Name string `json:"name"`
		} `json:"nodeRef"`
		Addresses []struct {
			Address string `json:"address"`
		} `json:"addresses"`
		LastUpdated string          `json:"lastUpdated"`
		Conditions  []capiCondition `json:"conditions"`
		// v1beta1
		BootstrapReady      bool   `json:"bootstrapReady"`
		InfrastructureReady bool   `json:"infrastructureReady"`
		FailureReason       string `json:"failureReason"`
		FailureMessage      string `json:"failureMessage"`
		// v1beta2
		Initialization struct {
			InfrastructureProvisioned  *bool `json:"infrastructureProvisioned"`
			BootstrapDataSecretCreated *bool `json:"bootstrapDataSecretCreated"`
		} `json:"initialization"`
		Deprecated *capiDeprecated `json:"deprecated"`
	} `json:"status"`
}

// capiMachineDeploymentObject decodes a MachineDeployment of either version: readyReplicas and
// availableReplicas keep their names, up-to-date is updatedReplicas in v1beta1.
type capiMachineDeploymentObject struct {
	Metadata capiMeta `json:"metadata"`
	Spec     struct {
		ClusterName string `json:"clusterName"`
		Replicas    *int   `json:"replicas"`
		Template    struct {
			Spec struct {
				Version string `json:"version"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		Replicas          int    `json:"replicas"`
		ReadyReplicas     int    `json:"readyReplicas"`
		AvailableReplicas int    `json:"availableReplicas"`
		// v1beta2
		UpToDateReplicas *int `json:"upToDateReplicas"`
		// v1beta1
		UpdatedReplicas     int `json:"updatedReplicas"`
		UnavailableReplicas int `json:"unavailableReplicas"`
	} `json:"status"`
}

// capiTalosControlPlaneObject is a TalosControlPlane (controlplane.cluster.x-k8s.io/v1alpha3):
// the control-plane counts of a v1beta1 Cluster.
type capiTalosControlPlaneObject struct {
	Metadata capiMeta `json:"metadata"`
	Spec     struct {
		Replicas *int   `json:"replicas"`
		Version  string `json:"version"`
	} `json:"spec"`
	Status struct {
		Replicas            int    `json:"replicas"`
		ReadyReplicas       int    `json:"readyReplicas"`
		UnavailableReplicas int    `json:"unavailableReplicas"`
		Version             string `json:"version"`
	} `json:"status"`
}

// capiRaw is what the reads brought back; each goroutine writes its own fields only.
type capiRaw struct {
	version           string // cluster.x-k8s.io's preferred version, "" without Cluster API
	sidero            bool
	talosControlPlane bool
	talosBootstrap    bool
	clusters          []capiClusterObject
	machines          []capiMachineObject
	deployments       []capiMachineDeploymentObject
	controlPlanes     []capiTalosControlPlaneObject
	servers           []sideroServerObject
	serverClasses     []sideroServerClassObject
	errs              [6]error
}

// readClusterAPI lists the Cluster API objects (all namespaces) and the Sidero Servers and
// ServerClasses (cluster-scoped), in parallel. now dates the stuck machines. A kind the cluster
// does not serve is "none"; any other failure lands in the report's error and the rest shows.
func readClusterAPI(ctx context.Context, k *kubeClient, now time.Time) (capiReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return capiReport{}, err
	}

	raw := capiRaw{version: groups[groupClusterAPI], sidero: groups[groupSidero] != ""}
	if raw.version == "" && !raw.sidero {
		return emptyClusterAPI(), nil
	}

	var wg sync.WaitGroup

	if raw.version != "" {
		base := "/apis/" + groupClusterAPI + "/" + raw.version

		wg.Go(func() {
			var list kubeList[capiClusterObject]
			raw.errs[0] = getList(ctx, k, base+"/clusters", &list)
			raw.clusters = list.Items
		})
		wg.Go(func() {
			var list kubeList[capiMachineObject]
			raw.errs[1] = getList(ctx, k, base+"/machines", &list)
			raw.machines = list.Items
		})
		wg.Go(func() {
			var list kubeList[capiMachineDeploymentObject]
			raw.errs[2] = getList(ctx, k, base+"/machinedeployments", &list)
			raw.deployments = list.Items
		})
	}

	if groups[groupCAPIControlPlane] != "" {
		wg.Go(func() {
			if !servesResource(ctx, k, groupCAPIControlPlane, capiTalosControlPlaneVersion, "taloscontrolplanes") {
				return
			}

			var list kubeList[capiTalosControlPlaneObject]
			raw.errs[3] = getList(ctx, k, "/apis/"+groupCAPIControlPlane+"/"+capiTalosControlPlaneVersion+"/taloscontrolplanes", &list)
			raw.controlPlanes, raw.talosControlPlane = list.Items, raw.errs[3] == nil
		})
	}

	if groups[groupCAPIBootstrap] != "" {
		wg.Go(func() {
			raw.talosBootstrap = servesResource(ctx, k, groupCAPIBootstrap, capiTalosConfigVersion, "talosconfigs")
		})
	}

	if raw.sidero {
		base := "/apis/" + groupSidero + "/" + groups[groupSidero]

		wg.Go(func() {
			var list kubeList[sideroServerObject]
			raw.errs[4] = ignoreNotFound(getList(ctx, k, base+"/servers", &list))
			raw.servers = list.Items
		})
		wg.Go(func() {
			var list kubeList[sideroServerClassObject]
			raw.errs[5] = ignoreNotFound(getList(ctx, k, base+"/serverclasses", &list))
			raw.serverClasses = list.Items
		})
	}

	wg.Wait()

	return buildClusterAPI(raw, now), nil
}

func emptyClusterAPI() capiReport {
	return capiReport{
		Clusters: []capiCluster{}, Machines: []capiMachine{}, MachineDeployments: []capiMachineDeployment{},
		Servers: []sideroServer{}, ServerClasses: []sideroServerClass{},
	}
}

// buildClusterAPI maps the objects to rows, joins the control planes, workers and server
// classes, then finishes the report.
func buildClusterAPI(raw capiRaw, now time.Time) capiReport {
	r := emptyClusterAPI()
	r.Installed = true
	r.Version = raw.version
	r.Sidero = raw.sidero
	r.TalosControlPlane = raw.talosControlPlane
	r.TalosBootstrap = raw.talosBootstrap
	r.Error = sectionError(raw.errs[:]...)

	legacy := capiLegacy(raw.version)

	for _, md := range raw.deployments {
		r.MachineDeployments = append(r.MachineDeployments, capiMachineDeploymentRow(md, legacy))
	}

	controlPlanes := map[string]capiTalosControlPlaneObject{}
	for _, cp := range raw.controlPlanes {
		controlPlanes[cp.Metadata.Namespace+"/"+cp.Metadata.Name] = cp
	}

	for _, c := range raw.clusters {
		r.Clusters = append(r.Clusters, capiClusterRow(c, controlPlanes, r.MachineDeployments))
	}

	for _, m := range raw.machines {
		r.Machines = append(r.Machines, capiMachineRow(m, now))
	}

	r.Servers, r.ServerClasses = sideroRows(raw.servers, raw.serverClasses)

	finishClusterAPI(&r)

	return r
}

// capiLegacy reports whether version is v1beta1 or older: the status shape before Cluster API
// 1.11.
func capiLegacy(version string) bool {
	return version == capiVersionV1Beta1 || strings.HasPrefix(version, "v1alpha")
}

func capiClusterRow(c capiClusterObject, controlPlanes map[string]capiTalosControlPlaneObject, deployments []capiMachineDeployment) capiCluster {
	s := c.Status
	failureReason, failureMessage := s.Deprecated.failure()

	row := capiCluster{
		Namespace:           c.Metadata.Namespace,
		Name:                c.Metadata.Name,
		Phase:               s.Phase,
		Paused:              c.Spec.Paused != nil && *c.Spec.Paused,
		InfraKind:           c.Spec.InfrastructureRef.Kind,
		ControlPlaneKind:    c.Spec.ControlPlaneRef.Kind,
		InfrastructureReady: s.InfrastructureReady || capiTrue(s.Initialization.InfrastructureProvisioned),
		ControlPlaneReady:   s.ControlPlaneReady || capiTrue(s.Initialization.ControlPlaneInitialized),
		Versions:            []string{},
		Conditions:          capiProblemConditions(s.Conditions, true),
		FailureReason:       cmp.Or(s.FailureReason, failureReason),
		FailureMessage:      cmp.Or(s.FailureMessage, failureMessage),
	}

	if ep := c.Spec.ControlPlaneEndpoint; ep.Host != "" {
		row.Endpoint = ep.Host
		if ep.Port != 0 {
			row.Endpoint += ":" + strconv.Itoa(ep.Port)
		}
	}

	var talos *capiReplicas

	ref := c.Spec.ControlPlaneRef
	if cp, ok := controlPlanes[cmp.Or(ref.Namespace, c.Metadata.Namespace)+"/"+ref.Name]; ok && ref.Kind == "TalosControlPlane" {
		talos = capiTalosControlPlaneReplicas(cp)
		if v := cmp.Or(cp.Status.Version, cp.Spec.Version); v != "" {
			row.Versions = []string{v}
		}
	}

	row.ControlPlane = talos
	if s.ControlPlane != nil {
		row.ControlPlane = capiControlPlaneOf(s.ControlPlane, talos)
		if len(s.ControlPlane.Versions) > 0 {
			row.Versions = []string{}
			for _, v := range s.ControlPlane.Versions {
				row.Versions = append(row.Versions, v.Version)
			}
		}
	}

	if s.Workers != nil {
		row.Workers = capiReplicasOf(s.Workers, capiReplicas{})
	} else {
		row.Workers = capiWorkersOf(row.Namespace, row.Name, deployments)
	}

	return row
}

// capiControlPlaneOf is a v1beta2 status.controlPlane, each count it leaves unset taken from the
// provider's own object (talos, nil without one). A provider on the v1beta1 contract (the Talos
// one) does not feed readyReplicas and availableReplicas: without talos to fill them the counts
// are unknown (nil), never "0 ready".
func capiControlPlaneOf(s *capiReplicaStatus, talos *capiReplicas) *capiReplicas {
	if talos == nil && (s.ReadyReplicas == nil || s.AvailableReplicas == nil) {
		return nil
	}

	r := capiReplicasOf(s, *cmp.Or(talos, &capiReplicas{}))

	return &r
}

// capiReplicasOf is a v1beta2 replica status, fallback filling the counts it leaves unset.
func capiReplicasOf(s *capiReplicaStatus, fallback capiReplicas) capiReplicas {
	return capiReplicas{
		Desired:   derefOr(s.DesiredReplicas, fallback.Desired),
		Ready:     derefOr(s.ReadyReplicas, fallback.Ready),
		Available: derefOr(s.AvailableReplicas, fallback.Available),
		UpToDate:  derefOr(s.UpToDateReplicas, fallback.UpToDate),
	}
}

// capiTalosControlPlaneReplicas is a TalosControlPlane's counts. It reports no up-to-date
// count: the ready replicas stand in for it.
func capiTalosControlPlaneReplicas(cp capiTalosControlPlaneObject) *capiReplicas {
	desired := cp.Status.Replicas
	if cp.Spec.Replicas != nil {
		desired = *cp.Spec.Replicas
	}

	return &capiReplicas{
		Desired:   desired,
		Ready:     cp.Status.ReadyReplicas,
		Available: max(0, cp.Status.Replicas-cp.Status.UnavailableReplicas),
		UpToDate:  cp.Status.ReadyReplicas,
	}
}

// capiWorkersOf sums the MachineDeployments of the cluster namespace/name.
func capiWorkersOf(namespace, name string, deployments []capiMachineDeployment) capiReplicas {
	var w capiReplicas

	for _, md := range deployments {
		if md.Namespace == namespace && md.Cluster == name {
			w.Desired += md.Desired
			w.Ready += md.Ready
			w.Available += md.Available
			w.UpToDate += md.UpToDate
		}
	}

	return w
}

func capiMachineRow(m capiMachineObject, now time.Time) capiMachine {
	s := m.Status
	failureReason, failureMessage := s.Deprecated.failure()

	row := capiMachine{
		Namespace:           m.Metadata.Namespace,
		Name:                m.Metadata.Name,
		Cluster:             cmp.Or(m.Spec.ClusterName, m.Metadata.Labels[capiClusterNameLabel]),
		Deployment:          m.Metadata.Labels[capiDeploymentLabel],
		Phase:               s.Phase,
		Version:             m.Spec.Version,
		ProviderID:          m.Spec.ProviderID,
		Addresses:           []string{},
		BootstrapReady:      s.BootstrapReady || capiTrue(s.Initialization.BootstrapDataSecretCreated),
		InfrastructureReady: s.InfrastructureReady || capiTrue(s.Initialization.InfrastructureProvisioned),
		LastUpdated:         unixMilli(s.LastUpdated),
		CreatedAt:           unixMilli(m.Metadata.CreationTimestamp),
		FailureReason:       cmp.Or(s.FailureReason, failureReason),
		FailureMessage:      cmp.Or(s.FailureMessage, failureMessage),
		Conditions:          capiProblemConditions(s.Conditions, false),
	}

	_, row.ControlPlane = m.Metadata.Labels[capiControlPlaneLabel]

	if s.NodeRef != nil {
		row.Node = s.NodeRef.Name
	}

	for _, a := range s.Addresses {
		if a.Address != "" && !slices.Contains(row.Addresses, a.Address) {
			row.Addresses = append(row.Addresses, a.Address)
		}
	}

	row.Stuck = capiMachineStuck(row.Phase, row.CreatedAt, unixMilli(m.Metadata.DeletionTimestamp), now)

	return row
}

// capiMachineStuck reports whether a Machine has sat in a provisioning phase since it was
// created, or in Deleting since it was deleted, for over capiStuckAfter.
func capiMachineStuck(phase string, createdAt, deletedAt int64, now time.Time) bool {
	since := int64(0)

	switch phase {
	case capiPhasePending, capiPhaseProvisioning, capiPhaseProvisioned:
		since = createdAt
	case capiPhaseDeleting:
		since = deletedAt
	}

	return since > 0 && now.UnixMilli()-since > capiStuckAfter.Milliseconds()
}

// capiMachineDeploymentRow maps a MachineDeployment; legacy (v1beta1) reports its unavailable
// replicas, v1beta2 leaves them to desired minus available.
func capiMachineDeploymentRow(md capiMachineDeploymentObject, legacy bool) capiMachineDeployment {
	s := md.Status

	row := capiMachineDeployment{
		Namespace: md.Metadata.Namespace,
		Name:      md.Metadata.Name,
		Cluster:   cmp.Or(md.Spec.ClusterName, md.Metadata.Labels[capiClusterNameLabel]),
		Phase:     s.Phase,
		Desired:   s.Replicas,
		Ready:     s.ReadyReplicas,
		Available: s.AvailableReplicas,
		UpToDate:  s.UpdatedReplicas,
		Version:   md.Spec.Template.Spec.Version,
	}

	if md.Spec.Replicas != nil {
		row.Desired = *md.Spec.Replicas
	}
	if s.UpToDateReplicas != nil {
		row.UpToDate = *s.UpToDateReplicas
	}

	if legacy {
		row.Unavailable = s.UnavailableReplicas
	} else {
		row.Unavailable = max(0, row.Desired-row.Available)
	}

	return row
}

// finishClusterAPI gives every row its health, sorts every list problems first, caps it at
// capiListMax and counts the rows. The demo report goes through it too.
func finishClusterAPI(r *capiReport) {
	for i := range r.Clusters {
		r.Clusters[i].Health, r.Clusters[i].Reasons = capiClusterHealth(r.Clusters[i])
	}
	for i := range r.Machines {
		r.Machines[i].Health, r.Machines[i].Reasons = capiMachineHealth(r.Machines[i])
	}
	for i := range r.MachineDeployments {
		md := &r.MachineDeployments[i]
		md.Health, md.Reasons = capiMachineDeploymentHealth(*md)
	}
	for i := range r.Servers {
		r.Servers[i].Health, r.Servers[i].Reasons = sideroServerHealth(r.Servers[i])
	}

	slices.SortFunc(r.Clusters, func(a, b capiCluster) int {
		return cmp.Or(cmp.Compare(capiRank(a.Health), capiRank(b.Health)),
			strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})
	slices.SortFunc(r.Machines, func(a, b capiMachine) int {
		return cmp.Or(cmp.Compare(capiRank(a.Health), capiRank(b.Health)),
			strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Cluster, b.Cluster),
			capiControlPlaneFirst(a.ControlPlane, b.ControlPlane), strings.Compare(a.Name, b.Name))
	})
	slices.SortFunc(r.MachineDeployments, func(a, b capiMachineDeployment) int {
		return cmp.Or(cmp.Compare(capiRank(a.Health), capiRank(b.Health)),
			strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})
	slices.SortFunc(r.Servers, func(a, b sideroServer) int {
		return cmp.Or(cmp.Compare(capiRank(a.Health), capiRank(b.Health)), strings.Compare(a.Name, b.Name))
	})
	slices.SortFunc(r.ServerClasses, func(a, b sideroServerClass) int { return strings.Compare(a.Name, b.Name) })

	r.Truncated = r.Truncated || len(r.Clusters) > capiListMax || len(r.Machines) > capiListMax ||
		len(r.MachineDeployments) > capiListMax || len(r.Servers) > capiListMax || len(r.ServerClasses) > capiListMax
	r.Clusters = r.Clusters[:min(len(r.Clusters), capiListMax)]
	r.Machines = r.Machines[:min(len(r.Machines), capiListMax)]
	r.MachineDeployments = r.MachineDeployments[:min(len(r.MachineDeployments), capiListMax)]
	r.Servers = r.Servers[:min(len(r.Servers), capiListMax)]
	r.ServerClasses = r.ServerClasses[:min(len(r.ServerClasses), capiListMax)]

	r.Counts = capiCounts{Clusters: len(r.Clusters), Machines: len(r.Machines), Servers: len(r.Servers)}

	var healths []string
	for _, c := range r.Clusters {
		healths = append(healths, c.Health)
	}
	for _, m := range r.Machines {
		healths = append(healths, m.Health)
	}
	for _, md := range r.MachineDeployments {
		healths = append(healths, md.Health)
	}
	for _, s := range r.Servers {
		healths = append(healths, s.Health)
	}

	for _, h := range healths {
		switch h {
		case capiHealthCritical:
			r.Counts.Critical++
		case capiHealthWarning:
			r.Counts.Warning++
		default:
			r.Counts.OK++
		}
	}
}

// capiTrue is a v1beta2 *bool status field, false when unset.
func capiTrue(b *bool) bool { return b != nil && *b }

// capiControlPlaneFirst orders a control-plane machine before a worker.
func capiControlPlaneFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	default:
		return 1
	}
}
