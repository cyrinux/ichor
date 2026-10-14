package ichorgo

import "time"

// demoVirtualMachines is what the demo cluster answers for namespace ("" for every one): one
// VM per state worth a look (running with addresses, halted, starting while its disk imports,
// crash-looping, migrating). Everything invented; nothing readable as a real cluster.
func demoVirtualMachines(namespace string) vmReport {
	now := time.Now().UnixMilli()

	all := []vmRow{
		{
			Namespace: "vms", Name: "web-01", RunStrategy: "Always", Status: "Running", Ready: true, Created: true,
			Conditions: []vmCondition{{Type: "Ready", Status: "True"}, {Type: "LiveMigratable", Status: "True"}},
			CPU:        2, Memory: "4Gi", Node: "worker-1", Phase: "Running", IPs: []string{"10.244.1.23"},
			GuestOS: "Debian GNU/Linux 13 (trixie)", LauncherImage: "quay.io/kubevirt/virt-launcher:v1.6.0",
			Disks: []vmDisk{{Name: "web-01-root", Phase: "Succeeded", Progress: "100.0%"}},
		},
		{
			Namespace: "vms", Name: "db-01", RunStrategy: "Always", Status: "Migrating", Ready: true, Created: true,
			Conditions: []vmCondition{{Type: "Ready", Status: "True"}, {Type: "LiveMigratable", Status: "True"}},
			CPU:        4, Memory: "16Gi", Node: "worker-2", Phase: "Running", IPs: []string{"10.244.2.41"},
			GuestOS: "Rocky Linux 10.0", LauncherImage: "quay.io/kubevirt/virt-launcher:v1.6.0",
			Migration: &vmMigration{
				Name: "db-01-migration-x7k2p", Phase: "Running", SourceNode: "worker-2", TargetNode: "worker-3",
				StartedAt: now - 95_000,
			},
			Disks: []vmDisk{{Name: "db-01-root", Phase: "Succeeded", Progress: "100.0%"}},
		},
		{
			Namespace: "vms", Name: "build-runner", RunStrategy: "Always", Status: "CrashLoopBackOff",
			Conditions: []vmCondition{
				{Type: "Ready", Status: "False", Reason: "GuestNotRunning", Message: "Guest VM is not reported as running"},
				{Type: "Failure", Status: "True", Reason: "CrashLoopBackOff", Message: "virt-launcher exited: not enough memory on the node"},
			},
			CPU: 8, Memory: "32Gi", IPs: []string{}, Disks: []vmDisk{},
		},
		{
			Namespace: "lab", Name: "win11-test", RunStrategy: "Always", Status: "Provisioning",
			Conditions: []vmCondition{{Type: "Ready", Status: "False", Reason: "Provisioning"}},
			CPU:        4, Memory: "8Gi", IPs: []string{},
			Disks: []vmDisk{{Name: "win11-test-root", Phase: "ImportInProgress", Progress: "62.00%", Importing: true}},
		},
		{
			Namespace: "lab", Name: "k3s-sandbox", RunStrategy: "Halted", Status: "Stopped",
			Conditions: []vmCondition{}, CPU: 2, Memory: "2Gi", IPs: []string{},
			Disks: []vmDisk{{Name: "k3s-sandbox-root", Phase: "Succeeded", Progress: "100.0%"}},
		},
	}

	r := vmReport{Installed: true, Version: "v1", VMs: []vmRow{}}
	for _, vm := range all {
		if namespace == "" || vm.Namespace == namespace {
			vm.State = vmState(vm.Status)
			r.VMs = append(r.VMs, vm)
		}
	}

	sortVMs(r.VMs)
	r.Namespaces = vmNamespaces(r.VMs)
	r.Counts = countVMs(r.VMs)

	return r
}
