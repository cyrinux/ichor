package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

// The fixtures follow the yaml tags of pkg/machinery/resources/hypervisor at Talos
// v1.15.0-beta.1 (virtual_machine_status.go, virtual_machine_spec.go,
// virtual_machine_disk_status.go, content_library_status.go). Names are invented.
const (
	vmStatusRunningYAML = `powerState: running
stage: ready
addresses:
    - 198.51.100.7/24
    - 2001:db8::7/64
interfaces:
    - name: eth0
      hardwareAddr: 52:54:00:aa:bb:01
      ips:
        - 198.51.100.7/24
        - 2001:db8::7/64
observedGeneration: abc/3
`
	// A node writing the enums as numbers: stopped, ready.
	vmStatusNumericYAML = `powerState: 2
stage: 2
`
	vmStatusErrorYAML = `powerState: stopped
stage: error
error: 'disk "root": volume pool "fast" is not ready'
`
	vmSpecWebYAML = `cpu:
    count: 4
    limit: 2000
memory:
    size: 8589934592
    ballooning:
        enabled: false
powerState: running
firmware:
    type: uefi
    secureBoot: true
profile: q35-virtio
`
	vmSpecFreshYAML = `cpu:
    count: 1
memory:
    size: 1073741824
powerState: stopped
`
	vmDiskWebRootYAML = `virtualMachine: web
name: root
sourcePath: /var/lib/hypervisor/pools/vm-pool/web-root.qcow2
format: qcow2
readOnly: false
phase: ready
pool: vm-pool
volume: web-root
size: 21474836480
`
	vmDiskWebSeedYAML = `virtualMachine: web
name: cidata
format: raw
readOnly: true
phase: ready
pool: vm-pool
volume: web-cidata
size: 1048576
`
	vmDiskBrokenYAML = `virtualMachine: broken
name: root
readOnly: false
phase: 1
error: volume pool "fast" is not ready
pool: fast
volume: broken-root
blank: true
`
	libraryImagesYAML = `volumeID: u-vm-images
path: /var/mnt/vm-images
phase: ready
fingerprint: 1f2e3d
volumeName: vm-images
`
	libraryPendingYAML = `volumeID: u-iso
phase: notReady
error: volume "iso" is not mounted
volumeName: iso
`
)

func hvSpec(t *testing.T, resourceType, id, yamlSpec string) map[string]any {
	t.Helper()

	spec, err := resourceSpecMap(untypedResource(t, resourceType, id, yamlSpec))
	if err != nil {
		t.Fatal(err)
	}

	return spec
}

func TestBuildNodeVirtualMachines(t *testing.T) {
	in := vmSpecs{
		statuses: map[string]map[string]any{
			"web":    hvSpec(t, vmStatusType, "web", vmStatusRunningYAML),
			"idle":   hvSpec(t, vmStatusType, "idle", vmStatusNumericYAML),
			"broken": hvSpec(t, vmStatusType, "broken", vmStatusErrorYAML),
		},
		specs: map[string]map[string]any{
			"web":   hvSpec(t, vmSpecType, "web", vmSpecWebYAML),
			"fresh": hvSpec(t, vmSpecType, "fresh", vmSpecFreshYAML),
		},
		disks: []map[string]any{
			hvSpec(t, vmDiskStatusType, "web/root@0123456789ab", vmDiskWebRootYAML),
			hvSpec(t, vmDiskStatusType, "broken/root@0123456789ab", vmDiskBrokenYAML),
			hvSpec(t, vmDiskStatusType, "web/cidata@0123456789ab", vmDiskWebSeedYAML),
		},
		libraries: map[string]map[string]any{
			"iso":    hvSpec(t, contentLibraryStatusType, "iso", libraryPendingYAML),
			"images": hvSpec(t, contentLibraryStatusType, "images", libraryImagesYAML),
		},
	}

	got := buildNodeVMs(in, nil)

	none := func() ([]string, []talosVMInterface, []talosVMDisk) {
		return []string{}, []talosVMInterface{}, []talosVMDisk{}
	}

	brokenAddr, brokenIfaces, _ := none()
	freshAddr, freshIfaces, freshDisks := none()
	idleAddr, idleIfaces, idleDisks := none()

	want := nodeVMs{
		Supported: true,
		VMs: []talosVM{
			{
				Name: "broken", PowerState: "stopped", Stage: "error", Error: `disk "root": volume pool "fast" is not ready`,
				Addresses: brokenAddr, Interfaces: brokenIfaces,
				Disks: []talosVMDisk{{Name: "root", Pool: "fast", Volume: "broken-root", Phase: "notReady", Error: `volume pool "fast" is not ready`}},
			},
			// A spec without a status yet: the wanted state and the size show, the rest is empty.
			{
				Name: "fresh", WantedPowerState: "stopped", CPUs: 1, MemoryBytes: 1 << 30,
				Addresses: freshAddr, Interfaces: freshIfaces, Disks: freshDisks,
			},
			// A status without a spec, enums written as numbers.
			{Name: "idle", PowerState: "stopped", Stage: "ready", Addresses: idleAddr, Interfaces: idleIfaces, Disks: idleDisks},
			{
				Name: "web", PowerState: "running", WantedPowerState: "running", Stage: "ready", Running: true,
				CPUs: 4, MemoryBytes: 8 << 30, Profile: "q35-virtio",
				Addresses:  []string{"198.51.100.7/24", "2001:db8::7/64"},
				Interfaces: []talosVMInterface{{Name: "eth0", MAC: "52:54:00:aa:bb:01", Addresses: []string{"198.51.100.7/24", "2001:db8::7/64"}}},
				Disks: []talosVMDisk{
					{Name: "cidata", Pool: "vm-pool", Volume: "web-cidata", Format: "raw", SizeBytes: 1 << 20, ReadOnly: true, Phase: "ready"},
					{Name: "root", Pool: "vm-pool", Volume: "web-root", Format: "qcow2", SizeBytes: 20 << 30, Phase: "ready"},
				},
			},
		},
		Libraries: []contentLibrary{
			{Name: "images", Path: "/var/mnt/vm-images", Phase: "ready"},
			{Name: "iso", Phase: "notReady", Error: `volume "iso" is not mounted`},
		},
		// buildNodeVMs leaves them to readNodeVMs, which knows the node (vmProblemsOf).
		Problems: []vmProblem{},
		Errors:   map[string]string{},
	}

	if !equalJSON(t, got, want) {
		t.Error("node VMs differ")
	}
}

func TestNodeVirtualMachinesShapes(t *testing.T) {
	tests := []struct {
		name  string
		got   nodeVMs
		parts []string
	}{
		{
			name: "unsupported (Talos 1.14)",
			got:  unsupportedNodeVMs("v1.14.7"),
			parts: []string{
				`"supported":false`, `"reason":"needs Talos v1.15 or newer (this node runs v1.14.7)"`,
				`"vms":[]`, `"libraries":[]`, `"errors":{}`,
			},
		},
		{
			name:  "unsupported, version unknown",
			got:   unsupportedNodeVMs(""),
			parts: []string{`"supported":false`, `"reason":"needs Talos v1.15 or newer"`},
		},
		{
			name:  "supported without VMs",
			got:   buildNodeVMs(vmSpecs{}, nil),
			parts: []string{`"supported":true`, `"reason":""`, `"vms":[]`, `"libraries":[]`, `"errors":{}`},
		},
		{
			// A refused section is named, the others still show.
			name: "a section refused",
			got: buildNodeVMs(vmSpecs{statuses: map[string]map[string]any{"web": hvSpec(t, vmStatusType, "web", vmStatusRunningYAML)}},
				map[string]string{vmSectionDisks: "permission denied"}),
			parts: []string{`"supported":true`, `"errors":{"disks":"permission denied"}`, `"name":"web"`, `"disks":[]`, `"running":true`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := toJSON(tt.got)
			if err != nil {
				t.Fatal(err)
			}

			for _, part := range tt.parts {
				if !strings.Contains(out, part) {
					t.Errorf("JSON lacks %s: %s", part, out)
				}
			}
		})
	}
}

func TestTalosVMMissingFields(t *testing.T) {
	// Fields of the wrong type or missing never fail the mapping.
	vm := mapTalosVM("odd", map[string]any{"powerState": 9, "addresses": "not a list", "interfaces": []any{"x", map[string]any{}}},
		map[string]any{"cpu": "four", "memory": map[string]any{"size": "big"}})
	if vm.PowerState != "" || vm.Running || vm.CPUs != 0 || vm.MemoryBytes != 0 || len(vm.Addresses) != 0 || len(vm.Interfaces) != 1 ||
		vm.Interfaces[0].Addresses == nil {
		t.Errorf("vm = %+v", vm)
	}

	if d := mapTalosVMDisk(map[string]any{}); d != (talosVMDisk{}) {
		t.Errorf("disk = %+v", d)
	}
}

func TestDemoVirtualMachines(t *testing.T) {
	yaml := demoConfigForTest(t)

	var host nodeOverview

	for _, n := range demoNodes() {
		if n.Hostname == demoVMHost {
			host = n
		}
	}

	out, err := NodeVirtualMachines(yaml, "", host.Node)
	if err != nil {
		t.Fatal(err)
	}

	var report nodeVMs
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}

	if !report.Supported || len(report.VMs) != 3 || len(report.Libraries) != 1 {
		t.Fatalf("demo VMs: %s", out)
	}

	running, stopped, failing := report.VMs[0], report.VMs[1], report.VMs[2]
	if !running.Running || len(running.Disks) != 2 || len(running.Addresses) == 0 {
		t.Errorf("running VM: %+v", running)
	}

	if stopped.Running || stopped.WantedPowerState != "stopped" || stopped.Error != "" {
		t.Errorf("stopped VM: %+v", stopped)
	}

	if failing.Running || failing.WantedPowerState != "running" || failing.Error == "" {
		t.Errorf("failing VM: %+v", failing)
	}

	out, err = ClusterVirtualMachines(yaml, "")
	if err != nil {
		t.Fatal(err)
	}

	var cluster nodeReadings[nodeVMsEntry]
	if err := json.Unmarshal([]byte(out), &cluster); err != nil {
		t.Fatal(err)
	}

	vms := 0

	for _, n := range cluster.Nodes {
		if n.Node == "" || n.Hostname == "" || !n.Supported || n.VMs == nil {
			t.Errorf("node %+v", n)
		}

		vms += len(n.VMs)
	}

	if len(cluster.Nodes) != len(demoNodes()) || vms != 3 || !strings.Contains(out, `"hostname":"`+demoVMHost+`"`) {
		t.Errorf("demo cluster VMs: %s", out)
	}

	features, err := NodeFeatures(yaml, "", host.Node)
	if err != nil || !strings.Contains(features, `"hypervisor":{"supported":true,"minVersion":"v1.15"`) {
		t.Errorf("demo features: %s %v", features, err)
	}
}

// Screenshot mode shows VM names like node hostnames (vm-N) and masks their addresses.
func TestVirtualMachinesMasked(t *testing.T) {
	yaml := demoConfigForTest(t)
	enableMask(t, "")

	out, err := ClusterVirtualMachines(yaml, "")
	if err != nil {
		t.Fatal(err)
	}

	for _, real := range []string{`"build-runner"`, `"legacy-app"`, `"test-desktop"`, "198.51.100.21"} {
		if strings.Contains(out, real) {
			t.Errorf("%s shows in screenshot mode: %s", real, out)
		}
	}

	if !strings.Contains(out, `"name":"vm-1"`) {
		t.Errorf("no masked VM name: %s", out)
	}
}
