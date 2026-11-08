package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	// A cluster serving KubeVirt's virtual machines and the subresources their actions live
	// under.
	apisKubeVirtActions = `{"groups":[` +
		`{"name":"kubevirt.io","preferredVersion":{"version":"v1"}},` +
		`{"name":"subresources.kubevirt.io","preferredVersion":{"version":"v1"}}]}`

	// A cluster that does not serve KubeVirt at all.
	apisWithoutKubeVirt = `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}}]}`

	vmActionName      = "k3s-sandbox"
	vmActionNamespace = "lab"

	vmActionVMPath      = "/apis/kubevirt.io/v1/namespaces/lab/virtualmachines/k3s-sandbox"
	vmActionMigPath     = "/apis/kubevirt.io/v1/namespaces/lab/virtualmachineinstancemigrations"
	vmActionSubPath     = "/apis/subresources.kubevirt.io/v1/namespaces/lab/"
	vmActionMigrationsQ = "GET " + vmActionMigPath
)

// vmActionVM is the VirtualMachine the fake API serves for the actions: spec as the test
// writes it, and a printable status.
func vmActionVM(spec, status string) string {
	return `{"metadata":{"name":"` + vmActionName + `","namespace":"` + vmActionNamespace + `"},` +
		`"spec":` + spec + `,"status":{"printableStatus":"` + status + `"}}`
}

// vmActionClient is a client over a fake API serving /apis with KubeVirt and the answers.
func vmActionClient(t *testing.T, answers map[string]string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	if _, ok := answers["GET /apis"]; !ok {
		answers["GET /apis"] = apisKubeVirtActions
	}

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

// methodRequest is the last request of method m, failing when there is none.
func methodRequest(t *testing.T, f *fakeKubeAPI, method string) fakeKubeRequest {
	t.Helper()

	for _, r := range slices.Backward(f.recorded()) {
		if r.method == method {
			return r
		}
	}

	t.Fatalf("no %s in %+v", method, f.recorded())

	return fakeKubeRequest{}
}

func TestVMActionRunsTheSubresource(t *testing.T) {
	for _, tc := range []struct {
		action, spec, status string
	}{
		{vmActionStart, `{"runStrategy":"Always"}`, "Stopped"},
		{vmActionStop, `{"runStrategy":"Always"}`, "Running"},
		{vmActionRestart, `{"runStrategy":"Always"}`, "Running"},
		{vmActionPause, `{"runStrategy":"Always"}`, "Running"},
		{vmActionPause, `{"runStrategy":"Always"}`, "Migrating"},
		{vmActionUnpause, `{"runStrategy":"Always"}`, "Paused"},
	} {
		// pause and unpause act on the instance, named like its virtual machine.
		resource := "virtualmachines"
		if tc.action == vmActionPause || tc.action == vmActionUnpause {
			resource = "virtualmachineinstances"
		}

		f, k := vmActionClient(t, map[string]string{
			"GET " + vmActionVMPath: vmActionVM(tc.spec, tc.status),
			"PUT " + vmActionSubPath + resource + "/" + vmActionName + "/" + tc.action: `{}`,
		})

		migration, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, tc.action, nil)
		if err != nil || migration != "" {
			t.Fatalf("%s: %q %v", tc.action, migration, err)
		}

		// The virtual machine first, then its subresource, bodyless.
		req := methodRequest(t, f, http.MethodPut)
		if want := vmActionSubPath + resource + "/" + vmActionName + "/" + tc.action; req.path != want {
			t.Errorf("%s put %s, want %s", tc.action, req.path, want)
		}

		if req.body != "{}" || req.contentType != "application/json" {
			t.Errorf("%s body %q (%s), want {} (application/json)", tc.action, req.body, req.contentType)
		}
	}
}

// TestVMActionReadsTheVMFirst: a VM that does not exist is the API's 404, nothing put.
func TestVMActionReadsTheVMFirst(t *testing.T) {
	f, k := vmActionClient(t, map[string]string{})

	var apiErr *kubeAPIError
	if _, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionStart, nil); !errors.As(err, &apiErr) || apiErr.Code != 404 {
		t.Fatalf("missing VM: %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPut {
			t.Errorf("put %s", r.path)
		}
	}
}

func TestVMActionRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, action, spec, status string
		want                       error
	}{
		{"start a running one", vmActionStart, `{"runStrategy":"Always"}`, "Running", errVMRunning},
		{"start a provisioning one", vmActionStart, `{"runStrategy":"Always"}`, "Provisioning", errVMStarting},
		{"stop a stopped one", vmActionStop, `{"running":false}`, "Stopped", errVMStopped},
		{"stop a stopping one", vmActionStop, `{"running":false}`, "Stopping", errVMStopped},
		{"restart a halted one", vmActionRestart, `{"runStrategy":"Halted"}`, "Stopped", errVMHalted},
		{"restart a stopped one", vmActionRestart, `{"runStrategy":"RerunOnFailure"}`, "ErrImagePull", errVMNotRunning},
		{"restart a crashlooping one", vmActionRestart, `{"runStrategy":"Always"}`, "CrashLoopBackOff", errVMNotRunning},
		{"migrate a stopped one", vmActionMigrate, `{"runStrategy":"Always"}`, "CrashLoopBackOff", errVMNotRunning},
		{"pause a provisioning one", vmActionPause, `{"runStrategy":"Always"}`, "Provisioning", errVMNotRunning},
		{"unpause a running one", vmActionUnpause, `{"runStrategy":"Always"}`, "Running", errVMNotPaused},
	} {
		f, k := vmActionClient(t, map[string]string{
			"GET " + vmActionVMPath: vmActionVM(tc.spec, tc.status),
			vmActionMigrationsQ:     `{"items":[]}`,
		})

		if _, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, tc.action, nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}

		for _, r := range f.recorded() {
			if r.method == http.MethodPut {
				t.Errorf("%s: put %s", tc.name, r.path)
			}
		}
	}
}

// TestVMActionMigrateFollowsTheMigration: the subresource answers with the migration it
// started, and a cluster answering nothing gives the one the list gained.
func TestVMActionMigrateFollowsTheMigration(t *testing.T) {
	put := "PUT " + vmActionSubPath + "virtualmachines/" + vmActionName + "/migrate"

	f, k := vmActionClient(t, map[string]string{
		"GET " + vmActionVMPath: vmActionVM(`{"runStrategy":"Always"}`, "Running"),
		vmActionMigrationsQ:     `{"items":[]}`,
		put:                     `{"metadata":{"name":"k3s-sandbox-mig-1"}}`,
	})

	migration, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionMigrate, nil)
	if err != nil || migration != "k3s-sandbox-mig-1" {
		t.Fatalf("%q %v", migration, err)
	}

	if req := methodRequest(t, f, http.MethodPut); req.body != "{}" {
		t.Errorf("migrate body %q", req.body)
	}

	// An older cluster that answers no body: the migration is the newest the list did not
	// hold before the call.
	f, k = vmActionClient(t, map[string]string{
		"GET " + vmActionVMPath: vmActionVM(`{"runStrategy":"Always"}`, "Running"),
		vmActionMigrationsQ:     `{"items":[]}`,
		put:                     `{}`,
	})
	f.beforeAnswer = func(*http.Request) {
		var reads int

		for _, r := range f.recorded() {
			if r.method == http.MethodGet && r.path == vmActionMigPath {
				reads++
			}
		}

		if reads == 1 {
			f.answerWith(vmActionMigrationsQ, http.StatusOK, `{"items":[`+
				`{"metadata":{"name":"k3s-sandbox-mig-old","namespace":"lab","creationTimestamp":"2026-10-15T09:00:00Z"},"spec":{"vmiName":"k3s-sandbox"},"status":{"phase":"Succeeded"}},`+
				`{"metadata":{"name":"k3s-sandbox-mig-2","namespace":"lab","creationTimestamp":"2026-10-15T10:00:00Z"},"spec":{"vmiName":"k3s-sandbox"},"status":{"phase":"Running"}}]}`)
		}
	}

	migration, err = runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionMigrate, nil)
	if err != nil || migration != "k3s-sandbox-mig-2" {
		t.Fatalf("fallback %q %v", migration, err)
	}
}

// TestVMActionMigrateRefusedInFlight: a migration already running is refused before asking,
// as the API server would answer a conflict.
func TestVMActionMigrateRefusedInFlight(t *testing.T) {
	f, k := vmActionClient(t, map[string]string{
		"GET " + vmActionVMPath: vmActionVM(`{"runStrategy":"Always"}`, "Migrating"),
		vmActionMigrationsQ: `{"items":[` +
			`{"metadata":{"name":"k3s-sandbox-mig","namespace":"lab","creationTimestamp":"2026-10-15T09:00:00Z"},"spec":{"vmiName":"k3s-sandbox"},"status":{"phase":"Running"}}]}`,
	})

	if _, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionMigrate, nil); !errors.Is(err, errVMMigrating) {
		t.Errorf("in-flight migration: %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == http.MethodPut {
			t.Errorf("put %s", r.path)
		}
	}
}

// TestVMActionNeedsKubeVirt: a cluster that does not serve KubeVirt says so rather than
// asking for a path that does not exist.
func TestVMActionNeedsKubeVirt(t *testing.T) {
	f, k := vmActionClient(t, map[string]string{"GET /apis": apisWithoutKubeVirt})

	if _, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionStart, nil); !errors.Is(err, errKubeVirtAbsent) {
		t.Errorf("absent KubeVirt: %v", err)
	}

	reached := slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool {
		return strings.HasSuffix(r.path, "/virtualmachines/"+vmActionName)
	})
	if reached {
		t.Errorf("read the virtual machine anyway: %+v", f.recorded())
	}
}

// TestKubeVMActionRefusedBeforeTheCluster: the verb, the names and the demo context are
// checked without reaching a cluster.
func TestKubeVMActionRefusedBeforeTheCluster(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeVMAction(cfg, "", "", vmActionNamespace, vmActionName, vmActionStart); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo start: %v", err)
	}

	if _, err := KubeVMStop(cfg, "", "", vmActionNamespace, vmActionName, 0); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo forced stop: %v", err)
	}

	if _, err := KubeVMAction(cfg, "", "", vmActionNamespace, vmActionName, "reboot"); err == nil || !strings.Contains(err.Error(), "unknown virtual machine action") {
		t.Errorf("unknown verb: %v", err)
	}

	// A real cluster: a missing name and a negative grace period are refused first.
	f := newFakeKubeAPI(t, map[string]string{})
	real := f.kubeconfigFor(f.URL)

	if _, err := KubeVMAction(real, "admin@test", "", "", vmActionName, vmActionStart); err == nil || !strings.Contains(err.Error(), "no virtual machine namespace or name") {
		t.Errorf("no namespace: %v", err)
	}

	if _, err := KubeVMStop(real, "admin@test", "", vmActionNamespace, vmActionName, -1); err == nil || !strings.Contains(err.Error(), "grace period cannot be negative") {
		t.Errorf("negative grace: %v", err)
	}

	if len(f.recorded()) != 0 {
		t.Errorf("reached the cluster: %+v", f.recorded())
	}
}

// TestKubeVMStopForce: the forced stop asks the same subresource with a grace period, and
// both actions record themselves.
func TestKubeVMStopForce(t *testing.T) {
	withDataDir(t)
	SetAuditEventClusters("")

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":             apisKubeVirtActions,
		"GET " + vmActionVMPath: vmActionVM(`{"runStrategy":"Always"}`, "Running"),
		"PUT " + vmActionSubPath + "virtualmachines/" + vmActionName + "/stop": `{}`,
	})

	cfg := f.kubeconfigFor(f.URL)

	out, err := KubeVMStop(cfg, "admin@test", "", vmActionNamespace, vmActionName, 30)
	if err != nil {
		t.Fatal(err)
	}

	var result vmActionResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	if result.Action != "stop-force" || result.Namespace != vmActionNamespace || result.Name != vmActionName || result.Migration != "" {
		t.Errorf("result %+v", result)
	}

	if req := methodRequest(t, f, http.MethodPut); req.body != `{"gracePeriod":30}` {
		t.Errorf("forced stop body %q", req.body)
	}

	// The audit entry: the forced stop, its grace period, and the object.
	entries := readAudit(t, "", "vm-stop-force")
	if len(entries) != 1 || entries[0].Namespace != vmActionNamespace || entries[0].Object != "VirtualMachine/"+vmActionName ||
		entries[0].Params != "gracePeriod=30" || entries[0].Outcome != auditOK {
		t.Fatalf("audit %+v", entries)
	}

	// A plain action records itself too: force stops apart.
	f2 := newFakeKubeAPI(t, map[string]string{
		"GET /apis":             apisKubeVirtActions,
		"GET " + vmActionVMPath: vmActionVM(`{"runStrategy":"Always"}`, "Stopped"),
		"PUT " + vmActionSubPath + "virtualmachines/" + vmActionName + "/start": `{}`,
	})

	cfg2 := f2.kubeconfigFor(f2.URL)

	if _, err := KubeVMAction(cfg2, "admin@test", "", vmActionNamespace, vmActionName, vmActionStart); err != nil {
		t.Fatal(err)
	}

	entries = readAudit(t, "", "vm-start")
	if len(entries) != 1 || entries[0].Object != "VirtualMachine/"+vmActionName || entries[0].Outcome != auditOK {
		t.Errorf("start audit %+v", entries)
	}
}

// TestVMStopForceOnStopping: a forced stop is what kills a VM stuck in Stopping, so it goes
// through there; a VM already stopped is still refused.
func TestVMStopForceOnStopping(t *testing.T) {
	stopPath := vmActionSubPath + "virtualmachines/" + vmActionName + "/stop"

	for _, tc := range []struct {
		status string
		want   error
	}{
		{"Stopping", nil},
		{"Stopped", errVMStopped},
	} {
		f, k := vmActionClient(t, map[string]string{
			"GET " + vmActionVMPath: vmActionVM(`{"running":false}`, tc.status),
			"PUT " + stopPath:       `{}`,
		})

		grace := 0

		_, err := runVMAction(context.Background(), k, vmActionNamespace, vmActionName, vmActionStop, &grace)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.status, err, tc.want)
		}

		put := slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return r.method == http.MethodPut })
		if put != (tc.want == nil) {
			t.Errorf("%s: put %v", tc.status, put)
		}

		if tc.want == nil {
			if req := methodRequest(t, f, http.MethodPut); req.path != stopPath || req.body != `{"gracePeriod":0}` {
				t.Errorf("%s: put %s %q", tc.status, req.path, req.body)
			}
		}
	}
}

// TestVMActionAccessChecks: every verb asks for its own subresource, on the group that
// serves it.
func TestVMActionAccessChecks(t *testing.T) {
	want := map[string]string{
		"vmStart":   "update virtualmachines.subresources.kubevirt.io/start",
		"vmStop":    "update virtualmachines.subresources.kubevirt.io/stop",
		"vmRestart": "update virtualmachines.subresources.kubevirt.io/restart",
		"vmMigrate": "update virtualmachines.subresources.kubevirt.io/migrate",
		"vmPause":   "update virtualmachineinstances.subresources.kubevirt.io/pause",
		"vmUnpause": "update virtualmachineinstances.subresources.kubevirt.io/unpause",
	}

	for name, w := range want {
		needs := kubeActionChecks[name]
		if len(needs) == 0 {
			t.Fatalf("no check for %s", name)
		}

		a := needs[0].attrs
		if got := a.Verb + " " + a.Resource + "." + a.Group + "/" + a.Subresource; got != w || !needs[0].namespaced {
			t.Errorf("%s asks %q, want %q namespaced", name, got, w)
		}
	}

	// A migration is created after the ones in flight are listed, and every verb reads its
	// virtual machine.
	migrations := map[string]bool{}

	for _, n := range kubeActionChecks["vmMigrate"] {
		if n.attrs.Group == groupKubeVirt && n.attrs.Resource == "virtualmachineinstancemigrations" && n.namespaced {
			migrations[n.attrs.Verb] = true
		}
	}

	if !migrations["create"] || !migrations["list"] {
		t.Errorf("vmMigrate needs %+v", kubeActionChecks["vmMigrate"])
	}

	for name := range want {
		needs := kubeActionChecks[name]

		reads := slices.ContainsFunc(needs, func(n accessNeed) bool {
			return n.attrs.Verb == "get" && n.attrs.Group == groupKubeVirt && n.attrs.Resource == "virtualmachines"
		})
		if !reads {
			t.Errorf("%s does not read its virtual machine: %+v", name, needs)
		}
	}
}
