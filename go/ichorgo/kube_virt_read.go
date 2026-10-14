package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// vmObject is a VirtualMachine (kubevirt.io/v1): how it should run, what its template asks,
// and what KubeVirt says of it.
type vmObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		RunStrategy         string `json:"runStrategy"`
		Running             *bool  `json:"running"` // deprecated for runStrategy, still common
		DataVolumeTemplates []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"dataVolumeTemplates"`
		Template struct {
			Spec struct {
				Domain struct {
					CPU struct {
						Cores   int `json:"cores"`
						Sockets int `json:"sockets"`
						Threads int `json:"threads"`
					} `json:"cpu"`
					Memory struct {
						Guest string `json:"guest"`
					} `json:"memory"`
					Resources struct {
						Requests map[string]string `json:"requests"`
					} `json:"resources"`
				} `json:"domain"`
				Volumes []struct {
					DataVolume *struct {
						Name string `json:"name"`
					} `json:"dataVolume"`
				} `json:"volumes"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		PrintableStatus     string `json:"printableStatus"`
		Ready               bool   `json:"ready"`
		Created             bool   `json:"created"`
		StateChangeRequests []struct {
			Action string `json:"action"`
		} `json:"stateChangeRequests"`
		Conditions []vmCondition `json:"conditions"`
	} `json:"status"`
}

// vmMigrationState is where a live migration goes, as the instance and the migration both say.
type vmMigrationState struct {
	SourceNode     string `json:"sourceNode"`
	TargetNode     string `json:"targetNode"`
	StartTimestamp string `json:"startTimestamp"`
	EndTimestamp   string `json:"endTimestamp"`
	Completed      bool   `json:"completed"`
	Failed         bool   `json:"failed"`
}

// vmiObject is a VirtualMachineInstance: the running VM, named like its VirtualMachine.
type vmiObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Phase      string `json:"phase"`
		NodeName   string `json:"nodeName"`
		Interfaces []struct {
			IPAddress   string   `json:"ipAddress"`
			IPAddresses []string `json:"ipAddresses"`
		} `json:"interfaces"`
		GuestOSInfo struct {
			PrettyName string `json:"prettyName"`
			Name       string `json:"name"`
			Version    string `json:"version"`
		} `json:"guestOSInfo"`
		LauncherContainerImageVersion string            `json:"launcherContainerImageVersion"`
		MigrationState                *vmMigrationState `json:"migrationState"`
	} `json:"status"`
}

// vmimObject is a VirtualMachineInstanceMigration: one live migration of an instance.
type vmimObject struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		VMIName string `json:"vmiName"`
	} `json:"spec"`
	Status struct {
		Phase          string            `json:"phase"`
		MigrationState *vmMigrationState `json:"migrationState"`
	} `json:"status"`
}

// dataVolumeObject is a CDI DataVolume: a disk being imported, cloned or uploaded.
type dataVolumeObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Status struct {
		Phase    string `json:"phase"`
		Progress string `json:"progress"`
	} `json:"status"`
}

// vmRaw is what the reads brought back; each goroutine writes its own fields only.
type vmRaw struct {
	vms         []vmObject
	vmis        []vmiObject
	migrations  []vmimObject
	dataVolumes []dataVolumeObject
	vmErr       error
	otherErrs   [3]error
}

// readVirtualMachines lists the VMs, their instances, migrations and DataVolumes of namespace
// ("" for every one) in parallel. Only the VM list failing leaves the list empty; the others
// fail soft into the report's error.
func readVirtualMachines(ctx context.Context, k *kubeClient, namespace string) (vmReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return vmReport{}, err
	}

	version := groups[groupKubeVirt]
	if version == "" {
		return vmReport{VMs: []vmRow{}, Namespaces: []string{}}, nil
	}

	base := "/apis/" + groupKubeVirt + "/" + version

	var (
		raw vmRaw
		wg  sync.WaitGroup
	)

	wg.Go(func() {
		var list kubeList[vmObject]
		raw.vmErr = getList(ctx, k, scopedPath(base, namespace, "virtualmachines"), &list)
		raw.vms = list.Items
	})
	wg.Go(func() {
		var list kubeList[vmiObject]
		raw.otherErrs[0] = getList(ctx, k, scopedPath(base, namespace, "virtualmachineinstances"), &list)
		raw.vmis = list.Items
	})
	wg.Go(func() {
		var list kubeList[vmimObject]
		raw.otherErrs[1] = getList(ctx, k, scopedPath(base, namespace, "virtualmachineinstancemigrations"), &list)
		raw.migrations = list.Items
	})
	if cdi := groups[groupCDI]; cdi != "" {
		wg.Go(func() {
			var list kubeList[dataVolumeObject]
			raw.otherErrs[2] = getList(ctx, k, scopedPath("/apis/"+groupCDI+"/"+cdi, namespace, "datavolumes"), &list)
			raw.dataVolumes = list.Items
		})
	}

	wg.Wait()

	r := buildVirtualMachines(raw, time.Now())
	r.Version = version
	r.Harvester = groups[groupHarvester] != ""

	return r, nil
}

// buildVirtualMachines joins the instances, migrations and disks to their VMs, groups the
// states, sorts (failing first) and counts.
func buildVirtualMachines(raw vmRaw, now time.Time) vmReport {
	r := vmReport{Installed: true, VMs: []vmRow{}, Namespaces: []string{}}
	r.Error = sectionError(append([]error{raw.vmErr}, raw.otherErrs[:]...)...)

	vmis := map[string]vmiObject{}
	for _, i := range raw.vmis {
		vmis[i.Metadata.Namespace+"/"+i.Metadata.Name] = i
	}

	dvs := map[string]dataVolumeObject{}
	for _, d := range raw.dataVolumes {
		dvs[d.Metadata.Namespace+"/"+d.Metadata.Name] = d
	}

	migrations := newestMigrations(raw.migrations)

	for _, vm := range raw.vms {
		key := vm.Metadata.Namespace + "/" + vm.Metadata.Name
		vmi, hasVMI := vmis[key]

		row := vmRowOf(vm)
		if hasVMI {
			addInstance(&row, vmi)
		}

		row.Migration = migrationOf(migrations[key], vmi, now)
		row.Disks = vmDisks(vm, dvs)
		r.VMs = append(r.VMs, row)
	}

	sortVMs(r.VMs)
	r.Namespaces = vmNamespaces(r.VMs)
	r.Counts = countVMs(r.VMs)

	return r
}

// vmRowOf maps what the VirtualMachine itself says.
func vmRowOf(vm vmObject) vmRow {
	s := vm.Status
	row := vmRow{
		Namespace:   vm.Metadata.Namespace,
		Name:        vm.Metadata.Name,
		RunStrategy: vmRunStrategy(vm),
		Status:      s.PrintableStatus,
		State:       vmState(s.PrintableStatus),
		Ready:       s.Ready,
		Created:     s.Created,
		Conditions:  vmConditions(s.Conditions),
		CPU:         vmCPU(vm),
		Memory:      cmp.Or(vm.Spec.Template.Spec.Domain.Memory.Guest, vm.Spec.Template.Spec.Domain.Resources.Requests["memory"]),
		IPs:         []string{},
	}

	if len(s.StateChangeRequests) > 0 {
		row.PendingChange = s.StateChangeRequests[0].Action
	}

	return row
}

// addInstance fills what the running instance says: its node, phase, addresses and guest.
func addInstance(row *vmRow, vmi vmiObject) {
	s := vmi.Status
	row.Node = s.NodeName
	row.Phase = s.Phase
	row.LauncherImage = s.LauncherContainerImageVersion
	row.GuestOS = cmp.Or(s.GuestOSInfo.PrettyName, strings.TrimSpace(s.GuestOSInfo.Name+" "+s.GuestOSInfo.Version))

	for _, iface := range s.Interfaces {
		ips := iface.IPAddresses
		if len(ips) == 0 {
			ips = []string{iface.IPAddress}
		}

		for _, ip := range ips {
			if ip != "" && !slices.Contains(row.IPs, ip) {
				row.IPs = append(row.IPs, ip)
			}
		}
	}
}

// vmRunStrategy is spec.runStrategy, else what the deprecated spec.running means.
func vmRunStrategy(vm vmObject) string {
	switch {
	case vm.Spec.RunStrategy != "":
		return vm.Spec.RunStrategy
	case vm.Spec.Running == nil:
		return ""
	case *vm.Spec.Running:
		return "Always"
	default:
		return "Halted"
	}
}

// vmCPU is the template's vCPUs (cores × sockets × threads, each 1 when unset), 0 when the
// template sets no CPU topology.
func vmCPU(vm vmObject) int {
	c := vm.Spec.Template.Spec.Domain.CPU
	if c.Cores == 0 && c.Sockets == 0 && c.Threads == 0 {
		return 0
	}

	return max(c.Cores, 1) * max(c.Sockets, 1) * max(c.Threads, 1)
}

// vmState groups a printable status. An unknown value is "changing": shown, never counted
// as failing.
func vmState(status string) string {
	switch status {
	case "Running":
		return vmStateRunning
	case "Stopped":
		return vmStateStopped
	case "CrashLoopBackOff", "Unknown", "Unschedulable", "ImagePullBackOff", "PvcNotFound", "DataVolumeError":
		return vmStateFailing
	}

	// ErrImagePull and whatever error status a later KubeVirt adds.
	if strings.HasPrefix(status, "Err") {
		return vmStateFailing
	}

	return vmStateChanging
}

// vmConditions keeps Ready and Failure first, then the others as written, at most
// vmConditionsMax.
func vmConditions(conds []vmCondition) []vmCondition {
	rank := func(c vmCondition) int {
		switch c.Type {
		case "Ready":
			return 0
		case "Failure":
			return 1
		default:
			return 2
		}
	}

	out := slices.Clone(conds)
	slices.SortStableFunc(out, func(a, b vmCondition) int { return cmp.Compare(rank(a), rank(b)) })

	if out == nil {
		return []vmCondition{}
	}

	return out[:min(len(out), vmConditionsMax)]
}

// newestMigrations keeps, per "namespace/vmiName", the newest migration.
func newestMigrations(ms []vmimObject) map[string]vmimObject {
	out := map[string]vmimObject{}

	for _, m := range ms {
		key := m.Metadata.Namespace + "/" + m.Spec.VMIName
		if old, ok := out[key]; !ok || unixMilli(m.Metadata.CreationTimestamp) > unixMilli(old.Metadata.CreationTimestamp) {
			out[key] = m
		}
	}

	return out
}

// migrationOf is a VM's newest migration, nil when it has none or when it finished more than
// vmMigrationRecent ago. Its nodes and times come from the migration, else from the instance.
func migrationOf(m vmimObject, vmi vmiObject, now time.Time) *vmMigration {
	if m.Metadata.Name == "" {
		return nil
	}

	state := m.Status.MigrationState
	if state == nil {
		state = vmi.Status.MigrationState
	}
	if state == nil {
		state = &vmMigrationState{}
	}

	out := &vmMigration{
		Name:       m.Metadata.Name,
		Phase:      m.Status.Phase,
		SourceNode: state.SourceNode,
		TargetNode: state.TargetNode,
		StartedAt:  cmp.Or(unixMilli(state.StartTimestamp), unixMilli(m.Metadata.CreationTimestamp)),
		Completed:  state.Completed || m.Status.Phase == "Succeeded",
		Failed:     state.Failed || m.Status.Phase == "Failed",
	}

	if out.Completed || out.Failed {
		ended := cmp.Or(unixMilli(state.EndTimestamp), out.StartedAt)
		if now.UnixMilli()-ended > vmMigrationRecent {
			return nil
		}
	}

	return out
}

// vmDisks is the VM's DataVolumes (its templates, then the volumes that name one), those the
// cluster still has.
func vmDisks(vm vmObject, dvs map[string]dataVolumeObject) []vmDisk {
	var names []string
	for _, t := range vm.Spec.DataVolumeTemplates {
		names = append(names, t.Metadata.Name)
	}
	for _, v := range vm.Spec.Template.Spec.Volumes {
		if v.DataVolume != nil {
			names = append(names, v.DataVolume.Name)
		}
	}

	out := []vmDisk{}
	seen := map[string]bool{}

	for _, name := range names {
		dv, ok := dvs[vm.Metadata.Namespace+"/"+name]
		if !ok || seen[name] {
			continue
		}

		seen[name] = true
		out = append(out, vmDisk{
			Name:      name,
			Phase:     dv.Status.Phase,
			Progress:  dv.Status.Progress,
			Importing: strings.HasSuffix(dv.Status.Phase, "Scheduled") || strings.HasSuffix(dv.Status.Phase, "InProgress"),
		})
	}

	return out
}

// vmRank orders the states: failing first, then changing, running, stopped.
func vmRank(state string) int {
	switch state {
	case vmStateFailing:
		return 0
	case vmStateChanging:
		return 1
	case vmStateRunning:
		return 2
	default:
		return 3
	}
}

func sortVMs(vms []vmRow) {
	slices.SortFunc(vms, func(a, b vmRow) int {
		return cmp.Or(
			cmp.Compare(vmRank(a.State), vmRank(b.State)),
			strings.Compare(a.Namespace, b.Namespace),
			strings.Compare(a.Name, b.Name),
		)
	})
}

func vmNamespaces(vms []vmRow) []string {
	out := []string{}
	for _, vm := range vms {
		out = append(out, vm.Namespace)
	}

	slices.Sort(out)

	return slices.Compact(out)
}

func countVMs(vms []vmRow) vmCounts {
	var c vmCounts

	for _, vm := range vms {
		switch vm.State {
		case vmStateRunning:
			c.Running++
		case vmStateStopped:
			c.Stopped++
		case vmStateFailing:
			c.Failing++
		}

		if vm.Migration != nil && !vm.Migration.Completed && !vm.Migration.Failed {
			c.Migrating++
		}

		if slices.ContainsFunc(vm.Disks, func(d vmDisk) bool { return d.Importing }) {
			c.Importing++
		}
	}

	return c
}
