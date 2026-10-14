package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	apisKubeVirtCDI = `{"groups":[` +
		`{"name":"kubevirt.io","preferredVersion":{"version":"v1"}},` +
		`{"name":"cdi.kubevirt.io","preferredVersion":{"version":"v1beta1"}}]}`
	apisKubeVirtHarvester = `{"groups":[` +
		`{"name":"kubevirt.io","preferredVersion":{"version":"v1"}},` +
		`{"name":"harvesterhci.io","preferredVersion":{"version":"v1beta1"}}]}`
)

func kubeVirtFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "kubevirt/"+name))
}

// kubeVirtAnswers is a cluster running KubeVirt and CDI, read across every namespace.
func kubeVirtAnswers(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"GET /apis": apisKubeVirtCDI,
		"GET /apis/kubevirt.io/v1/virtualmachines":                  kubeVirtFixture(t, "vms.json"),
		"GET /apis/kubevirt.io/v1/virtualmachineinstances":          kubeVirtFixture(t, "vmis.json"),
		"GET /apis/kubevirt.io/v1/virtualmachineinstancemigrations": kubeVirtFixture(t, "migrations.json"),
		"GET /apis/cdi.kubevirt.io/v1beta1/datavolumes":             kubeVirtFixture(t, "datavolumes.json"),
	}
}

// kubeVirtReadFrom is readVirtualMachines of namespace over the fake API f.
func kubeVirtReadFrom(t *testing.T, f *fakeKubeAPI, namespace string) vmReport {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readVirtualMachines(context.Background(), k, namespace)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func vmByName(t *testing.T, r vmReport, name string) vmRow {
	t.Helper()

	for _, vm := range r.VMs {
		if vm.Name == name {
			return vm
		}
	}

	t.Fatalf("no VM %s in %+v", name, r.VMs)

	return vmRow{}
}

func TestVirtualMachinesRead(t *testing.T) {
	r := kubeVirtReadFrom(t, newFakeKubeAPI(t, kubeVirtAnswers(t)), "")

	if !r.Installed || r.Harvester || r.Version != "v1" || r.Error != "" {
		t.Fatalf("report header %+v", r)
	}

	// Failing first, then changing, running, stopped; by namespace and name within each. The
	// instance without a VirtualMachine is not a row.
	var names []string
	for _, vm := range r.VMs {
		names = append(names, vm.Namespace+"/"+vm.Name)
	}
	if want := []string{
		"lab/gpu-01", "vms/build-runner", "lab/win11-test", "vms/db-01", "vms/old-migrated", "vms/web-01", "lab/k3s-sandbox",
	}; !slices.Equal(names, want) {
		t.Fatalf("order %v", names)
	}

	if !slices.Equal(r.Namespaces, []string{"lab", "vms"}) {
		t.Errorf("namespaces %v", r.Namespaces)
	}
	if want := (vmCounts{Running: 2, Stopped: 1, Failing: 2, Migrating: 1, Importing: 1}); r.Counts != want {
		t.Errorf("counts %+v", r.Counts)
	}
}

func TestVirtualMachineRows(t *testing.T) {
	r := kubeVirtReadFrom(t, newFakeKubeAPI(t, kubeVirtAnswers(t)), "")

	// Its instance: node, addresses of every interface once, guest, launcher; Ready first.
	web := vmByName(t, r, "web-01")
	if web.State != vmStateRunning || web.RunStrategy != "Always" || !web.Ready || !web.Created || web.CPU != 2 ||
		web.Memory != "4Gi" || web.Node != "worker-1" || web.Phase != "Running" ||
		web.GuestOS != "Debian GNU/Linux 13 (trixie)" || web.LauncherImage != "quay.io/kubevirt/virt-launcher:v1.6.0" ||
		web.Migration != nil || web.PendingChange != "" {
		t.Errorf("web-01 %+v", web)
	}
	if want := []string{"10.244.1.23", "fd10:244::17", "192.168.50.20"}; !slices.Equal(web.IPs, want) {
		t.Errorf("web-01 ips %v", web.IPs)
	}
	if web.Conditions[0].Type != "Ready" {
		t.Errorf("web-01 conditions %+v", web.Conditions)
	}
	// The template and the volume name the same DataVolume: one disk.
	if want := []vmDisk{{Name: "web-01-root", Phase: "Succeeded", Progress: "100.0%"}}; !slices.Equal(web.Disks, want) {
		t.Errorf("web-01 disks %+v", web.Disks)
	}

	// The deprecated running field, memory from the requests, the guest's name and version,
	// and the newest migration with the instance's nodes and start.
	db := vmByName(t, r, "db-01")
	if db.RunStrategy != "Always" || db.CPU != 4 || db.Memory != "16Gi" || db.GuestOS != "Rocky Linux 10.0" ||
		db.State != vmStateChanging {
		t.Errorf("db-01 %+v", db)
	}
	if want := (vmMigration{
		Name: "db-01-mig-new", Phase: "Running", SourceNode: "worker-2", TargetNode: "worker-3",
		StartedAt: time.Date(2026, 10, 14, 9, 58, 0, 0, time.UTC).UnixMilli(),
	}); db.Migration == nil || *db.Migration != want {
		t.Errorf("db-01 migration %+v", db.Migration)
	}

	// Failure and Ready first; no instance, so no node, phase or address.
	runner := vmByName(t, r, "build-runner")
	if runner.State != vmStateFailing || runner.Node != "" || runner.Phase != "" || len(runner.IPs) != 0 || runner.CPU != 0 {
		t.Errorf("build-runner %+v", runner)
	}
	var types []string
	for _, c := range runner.Conditions {
		types = append(types, c.Type)
	}
	if !slices.Equal(types, []string{"Ready", "Failure", "Paused"}) || runner.Conditions[1].Message != "virt-launcher exited" {
		t.Errorf("build-runner conditions %+v", runner.Conditions)
	}

	// A start requested while its disk imports.
	win := vmByName(t, r, "win11-test")
	if win.PendingChange != "Start" || win.CPU != 4 || win.State != vmStateChanging {
		t.Errorf("win11-test %+v", win)
	}
	if want := []vmDisk{{Name: "win11-test-root", Phase: "ImportInProgress", Progress: "40.00%", Importing: true}}; !slices.Equal(win.Disks, want) {
		t.Errorf("win11-test disks %+v", win.Disks)
	}

	if sandbox := vmByName(t, r, "k3s-sandbox"); sandbox.RunStrategy != "Halted" || sandbox.State != vmStateStopped {
		t.Errorf("k3s-sandbox %+v", sandbox)
	}
	if gpu := vmByName(t, r, "gpu-01"); gpu.RunStrategy != "RerunOnFailure" || gpu.State != vmStateFailing {
		t.Errorf("gpu-01 %+v", gpu)
	}
	// Its migration finished hours ago: off the row.
	if old := vmByName(t, r, "old-migrated"); old.Migration != nil {
		t.Errorf("old-migrated migration %+v", old.Migration)
	}
}

func TestVirtualMachineStates(t *testing.T) {
	for status, want := range map[string]string{
		"Running": vmStateRunning, "Stopped": vmStateStopped,
		"CrashLoopBackOff": vmStateFailing, "Unknown": vmStateFailing, "Unschedulable": vmStateFailing,
		"ErrImagePull": vmStateFailing, "ImagePullBackOff": vmStateFailing, "PvcNotFound": vmStateFailing,
		"DataVolumeError": vmStateFailing, "ErrorSomethingNew": vmStateFailing,
		"Provisioning": vmStateChanging, "Starting": vmStateChanging, "Stopping": vmStateChanging,
		"Terminating": vmStateChanging, "Migrating": vmStateChanging, "Paused": vmStateChanging,
		"WaitingForVolumeBinding": vmStateChanging, "SomethingNew": vmStateChanging, "": vmStateChanging,
	} {
		if got := vmState(status); got != want {
			t.Errorf("vmState(%q) = %s, want %s", status, got, want)
		}
	}
}

func TestVirtualMachineMigrationWindow(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

	finished := func(end time.Time, failed bool) vmimObject {
		var m vmimObject
		m.Metadata.Name = "m"
		m.Status.Phase = "Succeeded"
		m.Status.MigrationState = &vmMigrationState{
			StartTimestamp: end.Add(-time.Minute).Format(time.RFC3339), EndTimestamp: end.Format(time.RFC3339),
			Completed: !failed, Failed: failed,
		}
		if failed {
			m.Status.Phase = "Failed"
		}

		return m
	}

	if got := migrationOf(finished(now.Add(-10*time.Minute), true), vmiObject{}, now); got == nil || !got.Failed {
		t.Errorf("recent failure %+v", got)
	}
	if got := migrationOf(finished(now.Add(-2*time.Hour), false), vmiObject{}, now); got != nil {
		t.Errorf("old success kept: %+v", got)
	}
	if got := migrationOf(vmimObject{}, vmiObject{}, now); got != nil {
		t.Errorf("no migration: %+v", got)
	}
}

func TestVirtualMachinesNamespace(t *testing.T) {
	answers := map[string]string{
		"GET /apis": apisKubeVirtHarvester,
		"GET /apis/kubevirt.io/v1/namespaces/lab/virtualmachines":                  kubeVirtFixture(t, "vms.json"),
		"GET /apis/kubevirt.io/v1/namespaces/lab/virtualmachineinstances":          `{"items":[]}`,
		"GET /apis/kubevirt.io/v1/namespaces/lab/virtualmachineinstancemigrations": `{"items":[]}`,
	}
	f := newFakeKubeAPI(t, answers)

	// No CDI: no DataVolume read, so no disks; Harvester is said.
	r := kubeVirtReadFrom(t, f, "lab")
	if !r.Harvester || r.Error != "" || len(r.VMs) != 7 {
		t.Fatalf("report %+v", r)
	}
	for _, vm := range r.VMs {
		if len(vm.Disks) != 0 {
			t.Errorf("%s disks %+v", vm.Name, vm.Disks)
		}
	}
	for _, req := range f.recorded() {
		if strings.Contains(req.path, "datavolumes") || (strings.HasPrefix(req.path, "/apis/") && !strings.Contains(req.path, "/namespaces/lab/")) {
			t.Errorf("unexpected request %s", req.path)
		}
	}
}

func TestVirtualMachinesNotInstalled(t *testing.T) {
	r := kubeVirtReadFrom(t, newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`}), "")

	out, err := toJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Installed || !strings.Contains(out, `"installed":false`) || !strings.Contains(out, `"vms":[]`) {
		t.Errorf("not installed: %s", out)
	}
}

func TestVirtualMachinesPartialRead(t *testing.T) {
	f := newFakeKubeAPI(t, kubeVirtAnswers(t))
	f.answerWith("GET /apis/kubevirt.io/v1/virtualmachineinstances", http.StatusForbidden, forbiddenStatus)

	// The instances are forbidden: the VMs still show, without nodes, and the error says why.
	r := kubeVirtReadFrom(t, f, "")
	if len(r.VMs) != 7 || r.Error == "" || vmByName(t, r, "web-01").Node != "" {
		t.Errorf("partial read %+v", r)
	}

	f.answerWith("GET /apis/kubevirt.io/v1/virtualmachines", http.StatusForbidden, forbiddenStatus)
	if r := kubeVirtReadFrom(t, f, ""); len(r.VMs) != 0 || !r.Installed || r.Error == "" {
		t.Errorf("vms forbidden %+v", r)
	}
}

func TestVirtualMachinesDemo(t *testing.T) {
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeVirtualMachines(demo, demoKubeContext, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var r vmReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Installed || len(r.VMs) != 5 || r.VMs[0].Name != "build-runner" || r.Counts.Migrating != 1 || r.Counts.Importing != 1 {
		t.Errorf("demo %+v", r)
	}

	lab := demoVirtualMachines("lab")
	if len(lab.VMs) != 2 || !slices.Equal(lab.Namespaces, []string{"lab"}) {
		t.Errorf("demo lab %+v", lab)
	}

	if _, err := KubeVirtualMachines(demo, demoKubeContext, "", "Bad/NS"); err == nil {
		t.Error("invalid namespace accepted")
	}
}
