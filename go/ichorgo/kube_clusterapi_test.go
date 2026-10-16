package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// capiNow is the fixtures' clock: staging-cp-stuck was created two hours before it.
var capiNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func capiFixture(t *testing.T, name string) string {
	t.Helper()

	return string(readFixture(t, "clusterapi/"+name))
}

func capiGroups(groups ...string) string {
	var items []string
	for _, g := range groups {
		name, version, _ := strings.Cut(g, "/")
		items = append(items, `{"name":"`+name+`","preferredVersion":{"version":"`+version+`"}}`)
	}

	return `{"groups":[` + strings.Join(items, ",") + `]}`
}

// capiAnswers is a management cluster serving cluster.x-k8s.io at version, the Talos providers
// and Sidero Metal.
func capiAnswers(t *testing.T, version string) map[string]string {
	t.Helper()

	base := "GET /apis/cluster.x-k8s.io/" + version

	return map[string]string{
		"GET /apis": capiGroups("cluster.x-k8s.io/"+version, "controlplane.cluster.x-k8s.io/v1alpha3",
			"bootstrap.cluster.x-k8s.io/v1alpha3", "infrastructure.cluster.x-k8s.io/v1alpha3", "metal.sidero.dev/v1alpha2"),
		base + "/clusters":                                 capiFixture(t, "clusters-"+version+".json"),
		base + "/machines":                                 capiFixture(t, "machines.json"),
		base + "/machinedeployments":                       capiFixture(t, "machinedeployments.json"),
		"GET /apis/controlplane.cluster.x-k8s.io/v1alpha3": `{"resources":[{"name":"taloscontrolplanes","kind":"TalosControlPlane"}]}`,
		"GET /apis/controlplane.cluster.x-k8s.io/v1alpha3/taloscontrolplanes": capiFixture(t, "taloscontrolplanes.json"),
		"GET /apis/bootstrap.cluster.x-k8s.io/v1alpha3":                       `{"resources":[{"name":"talosconfigs","kind":"TalosConfig"}]}`,
		"GET /apis/metal.sidero.dev/v1alpha2/servers":                         capiFixture(t, "servers.json"),
		"GET /apis/metal.sidero.dev/v1alpha2/serverclasses":                   capiFixture(t, "serverclasses.json"),
	}
}

func capiReadFrom(t *testing.T, f *fakeKubeAPI) capiReport {
	t.Helper()

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readClusterAPI(context.Background(), k, capiNow)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func capiNames[T any](rows []T, name func(T) string) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, name(r))
	}

	return out
}

func TestClusterAPIRead(t *testing.T) {
	r := capiReadFrom(t, newFakeKubeAPI(t, capiAnswers(t, "v1beta2")))

	if !r.Installed || r.Version != "v1beta2" || !r.Sidero || !r.TalosControlPlane || !r.TalosBootstrap || r.Error != "" || r.Truncated {
		t.Fatalf("report header %+v", r)
	}

	// Clusters: the failed one first.
	if got := capiNames(r.Clusters, func(c capiCluster) string { return c.Name }); !slices.Equal(got, []string{"old-lab", "prod-edge"}) {
		t.Fatalf("clusters %v", got)
	}

	prod := r.Clusters[1]
	want := capiCluster{
		Namespace: "fleet", Name: "prod-edge", Phase: "Provisioned", Endpoint: "10.0.0.5:6443",
		InfraKind: "MetalCluster", ControlPlaneKind: "TalosControlPlane", InfrastructureReady: true, ControlPlaneReady: true,
		ControlPlane: &capiReplicas{Desired: 3, Ready: 3, Available: 3, UpToDate: 3},
		Workers:      capiReplicas{Desired: 4, Ready: 4, Available: 4, UpToDate: 4},
		Versions:     []string{"v1.34.1"}, Conditions: []capiCondition{}, Health: capiHealthOK, Reasons: []string{},
	}
	if !reflect.DeepEqual(prod, want) {
		t.Errorf("prod-edge\n got %+v\nwant %+v", prod, want)
	}

	lab := r.Clusters[0]
	if lab.Health != capiHealthCritical || !slices.Equal(lab.Reasons, []string{capiReasonClusterFailed}) ||
		lab.FailureReason != "CreateError" || lab.ControlPlane != nil || lab.ControlPlaneReady || len(lab.Conditions) != 2 {
		t.Errorf("old-lab %+v", lab)
	}

	// Machines: problems first, then by cluster with the control plane first.
	gotMachines := capiNames(r.Machines, func(m capiMachine) string { return m.Name })
	wantMachines := []string{"old-lab-cp", "prod-edge-workers-gone", "staging-cp-stuck", "prod-edge-cp-a", "prod-edge-workers-x", "staging-cp-fresh"}
	if !slices.Equal(gotMachines, wantMachines) {
		t.Fatalf("machines %v", gotMachines)
	}

	cp := r.Machines[3]
	if !cp.ControlPlane || cp.Cluster != "prod-edge" || cp.Node != "prod-edge-cp-1" || cp.ProviderID != "sidero://srv-a1" ||
		!slices.Equal(cp.Addresses, []string{"10.0.0.11", "prod-edge-cp-1"}) || !cp.BootstrapReady || !cp.InfrastructureReady ||
		cp.LastUpdated == 0 || cp.Stuck || cp.Health != capiHealthOK {
		t.Errorf("control-plane machine %+v", cp)
	}

	worker := r.Machines[4]
	if worker.Cluster != "prod-edge" || worker.ControlPlane || worker.Deployment != "prod-edge-workers" || !worker.BootstrapReady {
		t.Errorf("worker from labels %+v", worker)
	}

	stuck := r.Machines[2]
	if !stuck.Stuck || !slices.Equal(stuck.Reasons, []string{capiReasonMachineStuck}) || len(stuck.Conditions) != 1 ||
		stuck.Conditions[0].Reason != "WaitingForServer" || !stuck.BootstrapReady || stuck.InfrastructureReady {
		t.Errorf("stuck machine %+v", stuck)
	}

	if gone := r.Machines[1]; !gone.Stuck || !slices.Equal(gone.Reasons, []string{capiReasonMachineDeleting, capiReasonMachineStuck}) {
		t.Errorf("deleting machine %+v", gone)
	}

	if failed := r.Machines[0]; failed.Health != capiHealthCritical || failed.FailureReason != "CreateError" {
		t.Errorf("failed machine %+v", failed)
	}

	if fresh := r.Machines[5]; fresh.Stuck || fresh.Health != capiHealthOK {
		t.Errorf("fresh machine %+v", fresh)
	}

	// Machine deployments: the one short of workers first.
	wantMD := capiMachineDeployment{
		Namespace: "fleet", Name: "staging-workers", Cluster: "staging", Phase: "ScalingUp",
		Desired: 4, Ready: 2, Available: 2, UpToDate: 2, Unavailable: 2, Version: "v1.34.1",
		Health: capiHealthWarning, Reasons: []string{capiReasonWorkersShort},
	}
	if len(r.MachineDeployments) != 2 || !reflect.DeepEqual(r.MachineDeployments[0], wantMD) {
		t.Errorf("machine deployments %+v", r.MachineDeployments)
	}

	if want := (capiCounts{Clusters: 2, Machines: 6, Servers: 4, OK: 7, Warning: 4, Critical: 3}); r.Counts != want {
		t.Errorf("counts %+v", r.Counts)
	}
}

func TestClusterAPIServers(t *testing.T) {
	r := capiReadFrom(t, newFakeKubeAPI(t, capiAnswers(t, "v1beta2")))

	got := capiNames(r.Servers, func(s sideroServer) string { return s.Name })
	if !slices.Equal(got, []string{"srv-b2", "srv-b1", "srv-a1", "srv-c1"}) {
		t.Fatalf("servers %v", got)
	}

	a1 := r.Servers[2]
	want := sideroServer{
		Name: "srv-a1", Hostname: "rack1-u01", Accepted: true, Ready: true, InUse: true, Power: "on",
		Addresses: []string{"10.0.0.11"}, ServerClasses: []string{"any", "large"},
		CPU: "AMD EPYC 7313P 16-Core Processor, 16 cores", MemoryBytes: 128 << 30, Health: capiHealthOK, Reasons: []string{},
	}
	if !reflect.DeepEqual(a1, want) {
		t.Errorf("srv-a1\n got %+v\nwant %+v", a1, want)
	}

	if b2 := r.Servers[0]; b2.Health != capiHealthCritical ||
		!slices.Equal(b2.Reasons, []string{capiReasonServerPoweredOff, capiReasonServerNotReady}) || b2.CPU != "2 × Intel Xeon Gold 6338, 64 cores" {
		t.Errorf("srv-b2 %+v", b2)
	}

	if b1 := r.Servers[1]; b1.Health != capiHealthWarning || !slices.Equal(b1.Reasons, []string{capiReasonServerNotAccepted}) ||
		b1.CPU != "" || b1.MemoryBytes != 0 || len(b1.ServerClasses) != 0 {
		t.Errorf("srv-b1 %+v", b1)
	}

	if c1 := r.Servers[3]; c1.Health != capiHealthOK || !c1.IsClean || c1.MemoryBytes != 32<<30 || !slices.Equal(c1.ServerClasses, []string{"any"}) {
		t.Errorf("srv-c1 %+v", c1)
	}

	wantClasses := []sideroServerClass{{Name: "any", Available: 1, InUse: 2}, {Name: "large", Available: 0, InUse: 1}}
	if !reflect.DeepEqual(r.ServerClasses, wantClasses) {
		t.Errorf("server classes %+v", r.ServerClasses)
	}

	// The BMC credentials and address never reach the report.
	out, _ := json.Marshal(r)
	for _, secret := range []string{"10.9.9.1", "bmc-pass-never-shown", "bmc-srv-a1"} {
		if strings.Contains(string(out), secret) {
			t.Errorf("report leaks %q", secret)
		}
	}
}

// A v1beta1 management cluster (Cluster API before 1.11) gives the same rows: the control plane
// from the TalosControlPlane, the workers summed from the MachineDeployments.
func TestClusterAPIV1Beta1(t *testing.T) {
	v2 := capiReadFrom(t, newFakeKubeAPI(t, capiAnswers(t, "v1beta2")))
	v1 := capiReadFrom(t, newFakeKubeAPI(t, capiAnswers(t, "v1beta1")))

	if v1.Version != "v1beta1" || v1.Error != "" {
		t.Fatalf("v1beta1 header %+v", v1)
	}
	if !reflect.DeepEqual(v1.Clusters, v2.Clusters) {
		t.Errorf("clusters differ\nv1beta1 %+v\nv1beta2 %+v", v1.Clusters, v2.Clusters)
	}
	if !reflect.DeepEqual(v1.Machines, v2.Machines) || !reflect.DeepEqual(v1.MachineDeployments, v2.MachineDeployments) {
		t.Errorf("machines or deployments differ\nv1beta1 %+v\nv1beta2 %+v", v1.MachineDeployments, v2.MachineDeployments)
	}
}

func TestClusterAPIWithoutSidero(t *testing.T) {
	answers := capiAnswers(t, "v1beta2")
	answers["GET /apis"] = capiGroups("cluster.x-k8s.io/v1beta2")

	r := capiReadFrom(t, newFakeKubeAPI(t, answers))
	if !r.Installed || r.Sidero || r.TalosControlPlane || r.TalosBootstrap || r.Servers == nil || len(r.Servers) != 0 || len(r.Clusters) != 2 {
		t.Errorf("without sidero %+v", r)
	}
	// Without the TalosControlPlane, a v1beta2 cluster still has its status counts.
	if r.Clusters[1].ControlPlane == nil || r.Clusters[1].ControlPlane.Ready != 3 {
		t.Errorf("control plane %+v", r.Clusters[1].ControlPlane)
	}
}

func TestClusterAPISideroOnly(t *testing.T) {
	answers := capiAnswers(t, "v1beta2")
	answers["GET /apis"] = capiGroups("metal.sidero.dev/v1alpha2")

	r := capiReadFrom(t, newFakeKubeAPI(t, answers))
	if !r.Installed || r.Version != "" || !r.Sidero || len(r.Clusters) != 0 || len(r.Machines) != 0 || len(r.Servers) != 4 {
		t.Errorf("sidero only %+v", r)
	}
}

func TestClusterAPINotInstalled(t *testing.T) {
	r := capiReadFrom(t, newFakeKubeAPI(t, map[string]string{"GET /apis": capiGroups("apps/v1", "traefik.io/v1alpha1")}))

	out, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Installed || !strings.Contains(string(out), `"installed":false`) || !strings.Contains(string(out), `"clusters":[]`) {
		t.Errorf("not installed %s", out)
	}
}

func TestClusterAPIRefusedList(t *testing.T) {
	f := newFakeKubeAPI(t, capiAnswers(t, "v1beta2"))
	f.answerWith("GET /apis/cluster.x-k8s.io/v1beta2/machines", http.StatusForbidden, forbiddenStatus)

	r := capiReadFrom(t, f)
	if r.Error == "" || len(r.Machines) != 0 || len(r.Clusters) != 2 || len(r.Servers) != 4 {
		t.Errorf("refused machines %+v", r)
	}
}

func TestClusterAPITruncated(t *testing.T) {
	raw := capiRaw{version: "v1beta2"}
	for i := range capiListMax + 1 {
		var m capiMachineObject
		m.Metadata.Name, m.Status.Phase = "m-"+strconv.Itoa(i), capiPhaseFailed
		raw.machines = append(raw.machines, m)
	}

	if r := buildClusterAPI(raw, capiNow); !r.Truncated || len(r.Machines) != capiListMax || r.Counts.Critical != capiListMax {
		t.Errorf("truncated %v, %d machines, counts %+v", r.Truncated, len(r.Machines), r.Counts)
	}
}

func TestCAPIMachineStuck(t *testing.T) {
	ms := func(d time.Duration) int64 { return capiNow.Add(-d).UnixMilli() }

	for _, tc := range []struct {
		phase            string
		created, deleted int64
		want             bool
	}{
		{capiPhaseProvisioning, ms(31 * time.Minute), 0, true},
		{capiPhaseProvisioned, ms(2 * time.Hour), 0, true},
		{capiPhasePending, ms(29 * time.Minute), 0, false},
		{"Running", ms(48 * time.Hour), 0, false},
		{capiPhaseDeleting, ms(48 * time.Hour), ms(10 * time.Minute), false},
		{capiPhaseDeleting, ms(48 * time.Hour), ms(40 * time.Minute), true},
		{capiPhaseProvisioning, 0, 0, false},
	} {
		if got := capiMachineStuck(tc.phase, tc.created, tc.deleted, capiNow); got != tc.want {
			t.Errorf("%s created %d deleted %d: stuck %v", tc.phase, tc.created, tc.deleted, got)
		}
	}
}

func TestClusterAPIDemo(t *testing.T) {
	demo, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeClusterAPI(demo, demoKubeContext, "")
	if err != nil {
		t.Fatal(err)
	}

	var r capiReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	if !r.Installed || r.Version != "v1beta2" || !r.Sidero || len(r.Clusters) != 3 || r.Clusters[0].Name != "old-lab" ||
		r.Clusters[1].Name != "staging" || len(r.MachineDeployments) != 2 || len(r.Servers) != 5 || len(r.ServerClasses) != 2 {
		t.Fatalf("demo %+v", r)
	}
	if r.Machines[0].Name != "old-lab-cp-q9d4m" || !r.Machines[1].Stuck || r.Servers[0].Name != "srv-b2" {
		t.Errorf("demo order: machines %v, servers %v",
			capiNames(r.Machines, func(m capiMachine) string { return m.Name }), capiNames(r.Servers, func(s sideroServer) string { return s.Name }))
	}
	if want := (capiCounts{Clusters: 3, Machines: 9, Servers: 5, OK: 12, Warning: 4, Critical: 3}); r.Counts != want {
		t.Errorf("demo counts %+v", r.Counts)
	}
}
