package ichorgo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The Talos hypervisor resources (Talos 1.15, pkg/machinery/resources/hypervisor at
// v1.15.0-beta.1). This client's machinery (1.14) has no Go types for them, so they are read
// untyped and their YAML spec decoded by key. Every upstream name this reader depends on is
// in this block, so the machinery bump can re-check them in one place:
//
//	VirtualMachineStatuses (ID: VM name): powerState (unknown|running|stopped), stage
//	  (unknown|pending|ready|error), error, addresses[] (CIDR), interfaces[{name,
//	  hardwareAddr, ips[]}], observedGeneration
//	VirtualMachineSpecs (ID: VM name): cpu.count, memory.size (bytes), powerState (wanted),
//	  profile ("" = the builtin host profile). Sensitive: os:reader and os:operator read it
//	  only in the redacted form, which blanks cloudInit.{metaData,userData,networkConfig} to
//	  REDACTED; none of those is read here, so a redacted spec reads like a full one.
//	VirtualMachineDiskStatuses (ID: "<vm>/<disk>@<digest>"): virtualMachine, name, format,
//	  readOnly, phase (unknown|notReady|ready|observationUnavailable), error, pool, volume,
//	  size (bytes), blank
//	ContentLibraryStatuses (ID: library name): volumeName, path, phase (unknown|notReady|
//	  ready), error
//
// The enums are written as text (enumer -text); their numeric values are mapped too, in case
// a node writes them as numbers.
const (
	hypervisorNamespace      = "hypervisor"
	vmStatusType             = "VirtualMachineStatuses.hypervisor.talos.dev"
	vmSpecType               = "VirtualMachineSpecs.hypervisor.talos.dev"
	vmDiskStatusType         = "VirtualMachineDiskStatuses.hypervisor.talos.dev"
	contentLibraryStatusType = "ContentLibraryStatuses.hypervisor.talos.dev"
)

// Upstream enum names, in their numeric order.
var (
	vmPowerStates   = []string{"unknown", "running", "stopped"}
	vmStages        = []string{"unknown", "pending", "ready", "error"}
	vmDiskPhases    = []string{"unknown", "notReady", "ready", "observationUnavailable"}
	vmLibraryPhases = []string{"unknown", "notReady", "ready"}
)

const (
	vmPowerRunning   = "running"
	reasonVMsVersion = "needs Talos v1.15 or newer"
)

// The sections of a node's VM report, as named in its errors.
const (
	vmSectionVMs       = "vms"
	vmSectionSpecs     = "specs"
	vmSectionDisks     = "disks"
	vmSectionLibraries = "libraries"
)

type nodeVMs struct {
	Supported bool              `json:"supported"`
	Reason    string            `json:"reason"`
	VMs       []talosVM         `json:"vms"`
	Libraries []contentLibrary  `json:"libraries"`
	Problems  []vmProblem       `json:"problems"` // VMs that should run and do not, see vmProblemOf
	Errors    map[string]string `json:"errors"`   // section -> error, for sections that failed
}

type talosVM struct {
	Name             string             `json:"name"`
	PowerState       string             `json:"powerState"`       // observed: unknown | running | stopped
	WantedPowerState string             `json:"wantedPowerState"` // the spec's intent, "" without a spec
	Stage            string             `json:"stage"`            // unknown | pending | ready | error
	Error            string             `json:"error"`
	Running          bool               `json:"running"`
	CPUs             int64              `json:"cpus"`
	MemoryBytes      int64              `json:"memoryBytes"`
	Profile          string             `json:"profile"`
	Addresses        []string           `json:"addresses"`
	Interfaces       []talosVMInterface `json:"interfaces"`
	Disks            []talosVMDisk      `json:"disks"`
}

type talosVMInterface struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac"`
	Addresses []string `json:"addresses"`
}

type talosVMDisk struct {
	Name      string `json:"name"`
	Pool      string `json:"pool"`
	Volume    string `json:"volume"`
	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	ReadOnly  bool   `json:"readOnly"`
	Phase     string `json:"phase"`
	Error     string `json:"error"`
}

type contentLibrary struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Phase string `json:"phase"`
	Error string `json:"error"`
}

// nodeVMsEntry is one node of ClusterVirtualMachines.
type nodeVMsEntry struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	nodeVMs
	Error string `json:"error,omitempty"` // the node could not be read
}

// vmSpecs are the decoded specs of one node, by resource ID, per section.
type vmSpecs struct {
	statuses  map[string]map[string]any
	specs     map[string]map[string]any
	disks     []map[string]any
	libraries map[string]map[string]any
}

// NodeVirtualMachines returns the virtual machines the Talos hypervisor runs on node, their
// disks and the node's content libraries, like `talosctl get virtualmachinestatuses`
// (os:reader). supported is false with a reason on a node without the hypervisor resources
// (Talos older than 1.15); a section that cannot be read (refused, say) is named in errors
// and the rest still shows.
func NodeVirtualMachines(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeVirtualMachines", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		report, err := readNodeVMs(ctx, s, node)
		if err != nil {
			return "", err
		}

		return toJSON(report)
	})
}

// ClusterVirtualMachines is NodeVirtualMachines for every node of the context, in parallel:
// {"context","nodes":[{"node","hostname",...the node report,"error"}]}. A node that does not
// answer carries an error instead of failing the call.
func ClusterVirtualMachines(configYAML, contextName string) (string, error) {
	return readClusterNodes(configYAML, contextName, demoClusterVMs, readNodeVMsEntry)
}

func readNodeVMsEntry(ctx context.Context, s *session, node string) nodeVMsEntry {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	out := nodeVMsEntry{Node: node, Hostname: node}

	report, err := readNodeVMs(nodeCtx, s, node)
	if err != nil {
		out.nodeVMs = emptyNodeVMs()
		out.Error = err.Error()

		return out
	}

	out.nodeVMs = report

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		out.Hostname = hs.TypedSpec().Hostname
	}

	return out
}

func emptyNodeVMs() nodeVMs {
	return nodeVMs{VMs: []talosVM{}, Libraries: []contentLibrary{}, Problems: []vmProblem{}, Errors: map[string]string{}}
}

// readNodeVMs reads node's hypervisor resources; ctx targets node. It fails only when the node
// cannot be asked at all or when every section fails.
func readNodeVMs(ctx context.Context, s *session, node string) (nodeVMs, error) {
	known, err := s.hasResourceType(ctx, node, vmStatusType)
	if err != nil {
		return nodeVMs{}, s.friendlyErr(node, err)
	}

	if !known {
		return unsupportedNodeVMs(s.nodeVersion(ctx, node)), nil
	}

	errs := map[string]string{}
	list := func(section, resourceType string) []resource.Resource {
		listCtx := ctx
		if resourceType == vmSpecType {
			// Sensitive on 1.15: a role below os:admin reads the redacted form (see vmSpecType).
			listCtx = acceptRedacted(ctx)
		}

		items, err := s.client.COSI.List(listCtx, resource.NewMetadata(hypervisorNamespace, resourceType, "", resource.VersionUndefined))
		if err != nil {
			errs[section] = s.friendly(node, err)

			return nil
		}

		return items.Items
	}

	in := vmSpecs{
		statuses:  specsByID(vmSectionVMs, list(vmSectionVMs, vmStatusType), errs),
		specs:     specsByID(vmSectionSpecs, list(vmSectionSpecs, vmSpecType), errs),
		libraries: specsByID(vmSectionLibraries, list(vmSectionLibraries, contentLibraryStatusType), errs),
	}

	for _, spec := range specsByID(vmSectionDisks, list(vmSectionDisks, vmDiskStatusType), errs) {
		in.disks = append(in.disks, spec)
	}

	if len(errs) == 4 {
		return nodeVMs{}, fmt.Errorf("node %s: %s", node, errs[vmSectionVMs])
	}

	out := buildNodeVMs(in, errs)

	// Without the statuses nothing can be judged: no problem rather than every VM down.
	if errs[vmSectionVMs] == "" {
		out.Problems = vmProblemsOf(node, out.VMs)
	}

	return out, nil
}

// specsByID decodes the specs of items; one that does not decode is named in errs[section].
func specsByID(section string, items []resource.Resource, errs map[string]string) map[string]map[string]any {
	out := make(map[string]map[string]any, len(items))

	var failed []string

	for _, r := range items {
		spec, err := resourceSpecMap(r)
		if err != nil {
			failed = append(failed, r.Metadata().ID()+": "+err.Error())

			continue
		}

		out[r.Metadata().ID()] = spec
	}

	if len(failed) > 0 && errs[section] == "" {
		errs[section] = strings.Join(failed, "; ")
	}

	return out
}

func unsupportedNodeVMs(version string) nodeVMs {
	out := emptyNodeVMs()
	out.Reason = reasonVMsVersion

	if version != "" {
		out.Reason += " (this node runs " + version + ")"
	}

	return out
}

// buildNodeVMs joins the statuses, specs and disk statuses by VM name. A VM with a spec and no
// status yet (or the other way round) still shows.
func buildNodeVMs(in vmSpecs, errs map[string]string) nodeVMs {
	out := emptyNodeVMs()
	out.Supported, out.Errors = true, errs

	if out.Errors == nil {
		out.Errors = map[string]string{}
	}

	disks := map[string][]talosVMDisk{}

	for _, d := range in.disks {
		vm := specString(d, "virtualMachine")
		disks[vm] = append(disks[vm], mapTalosVMDisk(d))
	}

	names := map[string]bool{}
	for name := range in.statuses {
		names[name] = true
	}

	for name := range in.specs {
		names[name] = true
	}

	for name := range names {
		vm := mapTalosVM(name, in.statuses[name], in.specs[name])
		if d := disks[name]; d != nil {
			sort.Slice(d, func(i, j int) bool { return d[i].Name < d[j].Name })
			vm.Disks = d
		}

		out.VMs = append(out.VMs, vm)
	}

	sort.Slice(out.VMs, func(i, j int) bool { return out.VMs[i].Name < out.VMs[j].Name })

	for name, spec := range in.libraries {
		out.Libraries = append(out.Libraries, contentLibrary{
			Name: name, Path: specString(spec, "path"),
			Phase: specEnum(spec, "phase", vmLibraryPhases), Error: specString(spec, "error"),
		})
	}

	sort.Slice(out.Libraries, func(i, j int) bool { return out.Libraries[i].Name < out.Libraries[j].Name })

	return learnVMNames(out)
}

// vmHostRole is the role a VM name is learned with: screenshot mode shows it as vm-N.
const vmHostRole = "vm"

// learnVMNames teaches the screenshot mask the names of report's VMs, so they are shown (and
// map back) like node hostnames; their addresses are masked as every address is.
func learnVMNames(report nodeVMs) nodeVMs {
	for _, vm := range report.VMs {
		privacy.learnHost(vm.Name, vmHostRole)
	}

	return report
}

// mapTalosVM maps a VM's status and spec; either may be nil.
func mapTalosVM(name string, status, spec map[string]any) talosVM {
	out := talosVM{Name: name, Addresses: []string{}, Interfaces: []talosVMInterface{}, Disks: []talosVMDisk{}}

	if status != nil {
		out.PowerState = specEnum(status, "powerState", vmPowerStates)
		out.Stage = specEnum(status, "stage", vmStages)
		out.Error = specString(status, "error")
		out.Addresses = specStrings(status, "addresses")
		out.Running = out.PowerState == vmPowerRunning

		for _, raw := range specList(status, "interfaces") {
			out.Interfaces = append(out.Interfaces, talosVMInterface{
				Name: specString(raw, "name"), MAC: specString(raw, "hardwareAddr"), Addresses: specStrings(raw, "ips"),
			})
		}
	}

	if spec != nil {
		out.WantedPowerState = specString(spec, "powerState")
		out.Profile = specString(spec, "profile")
		out.CPUs = specIntOr0(specMap(spec, "cpu"), "count")
		out.MemoryBytes = specIntOr0(specMap(spec, "memory"), "size")
	}

	return out
}

func mapTalosVMDisk(spec map[string]any) talosVMDisk {
	readOnly, _ := spec["readOnly"].(bool) //nolint:errcheck

	return talosVMDisk{
		Name: specString(spec, "name"), Pool: specString(spec, "pool"), Volume: specString(spec, "volume"),
		Format: specString(spec, "format"), SizeBytes: specIntOr0(spec, "size"), ReadOnly: readOnly,
		Phase: specEnum(spec, "phase", vmDiskPhases), Error: specString(spec, "error"),
	}
}

// specEnum reads an enum written as text, or as its number (names in numeric order).
func specEnum(spec map[string]any, key string, names []string) string {
	if s := specString(spec, key); s != "" {
		return s
	}

	if n := specInt(spec, key); n != nil && *n >= 0 && *n < int64(len(names)) {
		return names[*n]
	}

	return ""
}

func specIntOr0(spec map[string]any, key string) int64 {
	if n := specInt(spec, key); n != nil {
		return *n
	}

	return 0
}

func specMap(spec map[string]any, key string) map[string]any {
	m, _ := spec[key].(map[string]any) //nolint:errcheck

	return m
}

// specList is the maps of a YAML list; other items are skipped.
func specList(spec map[string]any, key string) []map[string]any {
	raw, _ := spec[key].([]any) //nolint:errcheck
	out := make([]map[string]any, 0, len(raw))

	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}

	return out
}

// specStrings is the strings of a YAML list; never nil.
func specStrings(spec map[string]any, key string) []string {
	raw, _ := spec[key].([]any) //nolint:errcheck
	out := make([]string, 0, len(raw))

	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}
