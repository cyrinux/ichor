package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const (
	azureTestImages   = azureTestGroup + "Microsoft.Compute/images"
	azureHelperOutput = "Enable succeeded: \n[stdout]\nichor: writing the image to /dev/sdc\n" + awsImageMarkerOK + "\n\n[stderr]\n"
)

// fakeAzureHelper is the image run's side of fakeARM: Run Command, deallocate, the helper's
// state, the image and the deletes that differ from a node VM's. fakeARM's lock covers it.
type fakeAzureHelper struct {
	output         string
	runConflicts   int  // Run Command answers 409 this many times first
	runNeverEnds   bool // its operation stays InProgress
	failDeallocate bool
	imageStatus    int    // the status the image PUT answers, after creating when >= 500
	failDeleteOf   string // a path suffix whose delete is refused
	existing       []any  // images already in the resource group
	onWriting      func()

	scripts      []string
	runPosts     int
	deallocated  bool
	imageVersion string // the api-version of the image PUT
}

func newFakeAzureHelper() *fakeAzureHelper { return &fakeAzureHelper{output: azureHelperOutput} }

func helperPath(path string) bool { return strings.Contains(path, "/ichor-image-") }

// handle answers what the image run asks that fakeARM does not, and records the ARM calls.
func (h *fakeAzureHelper) handle(f *fakeARM, w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path

	if path == "/operations/runcommand" {
		h.runOperation(w)

		return true
	}

	if strings.HasPrefix(path, "/operations/") {
		return false
	}

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/runCommand"):
		f.calls = append(f.calls, r.Method+" "+path)
		h.runCommand(f, w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/deallocate"):
		f.calls = append(f.calls, r.Method+" "+path)

		if h.failDeallocate {
			armFail(w, http.StatusConflict, "OperationNotAllowed", "The VM is busy")

			return true
		}

		h.deallocated = true
		w.Header().Set("Azure-AsyncOperation", f.url+"/operations/deallocate-1?api-version="+azureComputeVersion)
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && path == azureTestImages:
		f.calls = append(f.calls, r.Method+" "+path)
		armJSON(w, http.StatusOK, map[string]any{"value": h.existing})
	case r.Method == http.MethodGet && helperPath(path) && strings.Contains(path, "/virtualMachines/") && f.resources[path] != nil:
		f.calls = append(f.calls, r.Method+" "+path)
		h.vm(w, path)
	case r.Method == http.MethodPut && helperPath(path) && strings.Contains(path, "/virtualMachines/"):
		return h.putVM(f, path)
	case r.Method == http.MethodPut && strings.Contains(path, "/Microsoft.Compute/images/"):
		return h.putImage(f, w, r)
	case r.Method == http.MethodDelete && h.failDeleteOf != "" && strings.HasSuffix(path, h.failDeleteOf):
		f.calls = append(f.calls, r.Method+" "+path)
		armFail(w, http.StatusBadRequest, "OperationNotAllowed", "refused by a lock")
	case r.Method == http.MethodDelete && helperPath(path) && strings.Contains(path, "/virtualMachines/") && f.resources[path] != nil:
		// The VM takes its OS disk, NIC and public address (Delete), not its data disk (Detach).
		delete(f.resources, strings.Replace(path, "Microsoft.Compute/virtualMachines/", "Microsoft.Network/networkInterfaces/", 1)+"-nic")
		delete(f.resources, strings.Replace(path, "Microsoft.Compute/virtualMachines/", "Microsoft.Network/publicIPAddresses/", 1)+"-ip")

		return false
	default:
		return false
	}

	return true
}

func (h *fakeAzureHelper) runCommand(f *fakeARM, w http.ResponseWriter, r *http.Request) {
	h.runPosts++

	var body struct {
		CommandID string   `json:"commandId"`
		Script    []string `json:"script"`
	}

	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.CommandID != "RunShellScript" {
		armFail(w, http.StatusBadRequest, "InvalidParameter", "commandId")

		return
	}

	h.scripts = append(h.scripts, strings.Join(body.Script, "\n"))

	if h.runConflicts > 0 {
		h.runConflicts--
		armFail(w, http.StatusConflict, "Conflict", "Run command extension execution is in progress. Please wait for completion before invoking a run command.")

		return
	}

	w.Header().Set("Azure-AsyncOperation", f.url+"/operations/runcommand?api-version="+azureComputeVersion)
	w.WriteHeader(http.StatusAccepted)
}

func (h *fakeAzureHelper) runOperation(w http.ResponseWriter) {
	if h.onWriting != nil {
		h.onWriting()
		h.onWriting = nil
	}

	if h.runNeverEnds {
		armJSON(w, http.StatusOK, map[string]string{"status": "InProgress"})

		return
	}

	armJSON(w, http.StatusOK, map[string]any{"status": azureOpSucceeded, "properties": map[string]any{"output": map[string]any{"value": []any{
		map[string]string{"code": "ProvisioningState/succeeded", "message": h.output},
	}}}})
}

func (h *fakeAzureHelper) vm(w http.ResponseWriter, path string) {
	power := "PowerState/running"
	if h.deallocated {
		power = "PowerState/deallocated"
	}

	armJSON(w, http.StatusOK, map[string]any{"id": path, "properties": map[string]any{
		"provisioningState": azureOpSucceeded, "instanceView": map[string]any{"statuses": []any{map[string]string{"code": power}}},
	}})
}

// putVM lets fakeARM store the VM, and makes its data disk as Azure does.
func (h *fakeAzureHelper) putVM(f *fakeARM, path string) bool {
	disk := strings.Replace(path, "/virtualMachines/", "/disks/", 1) + "-data"
	f.resources[disk] = map[string]any{}

	return false
}

func (h *fakeAzureHelper) putImage(f *fakeARM, w http.ResponseWriter, r *http.Request) bool {
	h.imageVersion = r.URL.Query().Get("api-version")

	if h.imageStatus == 0 {
		return false
	}

	f.calls = append(f.calls, r.Method+" "+r.URL.Path)

	var body map[string]any

	_ = json.NewDecoder(r.Body).Decode(&body)
	f.puts[r.URL.Path] = body
	f.putOrder = append(f.putOrder, r.URL.Path)

	if h.imageStatus >= http.StatusInternalServerError {
		f.resources[r.URL.Path] = body
	}

	armFail(w, h.imageStatus, "InvalidParameter", "image broke")

	return true
}

func azureImageTestRun(cfg string, rec *joinRecorder) *azureImageRun {
	return &azureImageRun{
		config: cfg, context: "fake", credJSON: azureTestCred,
		spec: azureImageSpec{
			TemplateNode: joinTemplateNode, ResourceGroup: "talos", Location: "westeurope", SubnetID: azureTestSubnet, SecurityGroupID: azureTestNSG,
		},
		listener: maskedJoinListener{rec}, poll: joinPollInterval, retryWait: joinPollInterval,
		writeWait: 100 * joinPollInterval, deallocateWait: 100 * joinPollInterval, imageWait: 100 * joinPollInterval,
	}
}

func startAzureImage(t *testing.T, f *fakeARM, edit func(*azureImageRun)) (string, string, []joinProgress) {
	t.Helper()
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	if f.helper == nil {
		f.helper = newFakeAzureHelper()
	}

	startFakeARM(t, f)

	rec := newJoinRecorder()

	run := azureImageTestRun(cfg, rec)
	if edit != nil {
		edit(run)
	}

	run.start(time.Minute)

	return rec.wait(t)
}

// azureLeft is what the run made that is still there, but the image.
func azureLeft(f *fakeARM) []string {
	var left []string

	for path := range f.resources {
		if helperPath(path) {
			left = append(left, path)
		}
	}

	return left
}

func azureMadeImage(f *fakeARM) string {
	for path := range f.resources {
		if strings.Contains(path, "/Microsoft.Compute/images/") {
			return path
		}
	}

	return ""
}

func TestAzureImageHappyPath(t *testing.T) {
	f := &fakeARM{}

	result, msg, events := startAzureImage(t, f, nil)
	if msg != "" {
		t.Fatalf("error %q, events %+v", msg, events)
	}

	want := []string{cloudPhaseCreating, azureImagePhaseWriting, azureImagePhaseSnapshot, cloudPhaseCleanup, joinPhaseDone}
	if got := slices.Compact(joinPhasesOf(events)); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	var r azureImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}

	name := "talos-v1.14.2-azure-amd64-" + r.Schematic[:12]
	if r.ImageID != azureTestImages+"/"+name || r.Name != name || r.Arch != bootArchAMD64 || r.TalosVersion != "v1.14.2" ||
		len(r.Schematic) != 64 || r.Generation != azureGenV1 || r.SizeGB != azureImageDiskGB {
		t.Errorf("result = %+v", r)
	}

	checkAzureHelper(t, f)
	checkAzureImage(t, f, r)

	if left := azureLeft(f); len(left) != 0 || azureMadeImage(f) != r.ImageID {
		t.Errorf("left behind %v, image %q", left, azureMadeImage(f))
	}

	checkAzureCallsInRole(t, f)

	for _, e := range events {
		if azureLeaks(e.Message) {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", azureImageAudit)
	if len(entries) != 1 || entries[0].Outcome != auditOK ||
		!strings.Contains(entries[0].Params, "arch=amd64 location=westeurope version=v1.14.2 image=/subscriptions/…/resourceGroups/talos/") ||
		azureLeaks(entries[0].Params) {
		t.Errorf("audit = %+v", entries)
	}
}

func checkAzureHelper(t *testing.T, f *fakeARM) {
	t.Helper()

	var vm map[string]any

	for _, path := range f.putOrder {
		if strings.Contains(path, "/virtualMachines/ichor-image-") {
			vm = f.puts[path]
		}
	}

	if vm == nil {
		t.Fatalf("no helper VM in %v", f.putOrder)
	}

	image, _ := azureAt(vm, "properties", "storageProfile", "imageReference").(map[string]any)
	data, _ := azureAt(vm, "properties", "storageProfile", "dataDisks", 0).(map[string]any)

	if image["publisher"] != "Canonical" || image["offer"] != "ubuntu-24_04-lts" || image["sku"] != "server" ||
		azureAt(vm, "properties", "hardwareProfile", "vmSize") != azureImageSizeAMD64 ||
		azureAt(vm, "properties", "storageProfile", "osDisk", "deleteOption") != "Delete" ||
		data["createOption"] != "Empty" || data["deleteOption"] != "Detach" || data["diskSizeGB"] != float64(azureImageDiskGB) || data["lun"] != float64(0) ||
		azureAt(vm, "properties", "networkProfile", "networkInterfaces", 0, "properties", "deleteOption") != "Delete" ||
		azureAt(vm, "properties", "osProfile", "customData") != nil {
		t.Errorf("helper VM = %v", vm)
	}

	h := f.helper
	if len(h.scripts) != 1 {
		t.Fatalf("scripts = %d", len(h.scripts))
	}

	script := h.scripts[0]
	for _, want := range []string{"#!/bin/bash", "set -euo pipefail", "/v1.14.2/azure-amd64.vhd.xz", "xz -dc | dd of=\"$disk\" bs=4M conv=sparse",
		"/dev/disk/azure/scsi1/lun0", "size=8589934592", `echo "$m-OK"`, "$m-FAIL"} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q:\n%s", want, script)
		}
	}

	if strings.Contains(script, awsImageMarkerOK) || strings.Contains(script, "/dev/sdc") {
		t.Errorf("the script names the marker or a device:\n%s", script)
	}

	if !h.deallocated {
		t.Error("the helper was not deallocated before the image")
	}
}

func checkAzureImage(t *testing.T, f *fakeARM, r azureImageResult) {
	t.Helper()

	body := f.puts[r.ImageID]
	disk, _ := azureAt(body, "properties", "storageProfile", "osDisk", "managedDisk", "id").(string)
	tags, _ := body["tags"].(map[string]any)

	if azureAt(body, "properties", "hyperVGeneration") != r.Generation || azureAt(body, "properties", "storageProfile", "osDisk", "osType") != "Linux" ||
		azureAt(body, "properties", "storageProfile", "osDisk", "osState") != "Generalized" ||
		!strings.HasPrefix(disk, azureTestGroup+"Microsoft.Compute/disks/ichor-image-") || !strings.HasSuffix(disk, "-data") ||
		tags["os"] != "talos" || tags[azureTagTalosVersion] != r.TalosVersion || tags[azureTagSchematic] != r.Schematic ||
		tags[azureTagArch] != r.Arch || tags[azureTagRun] == "" || f.helper.imageVersion != azureComputeVersion {
		t.Errorf("image PUT = %v", body)
	}

	// The data disk is deleted after the image, and the VM before the disk.
	vm, data := -1, -1

	for i, p := range f.deleted {
		switch {
		case strings.Contains(p, "/virtualMachines/ichor-image-"):
			vm = i
		case strings.HasSuffix(p, "-data") && p != disk:
			t.Errorf("deleted another disk: %s", p)
		case p == disk:
			data = i
		}
	}

	if vm < 0 || data < vm {
		t.Errorf("deleted = %v", f.deleted)
	}
}

// The printed role holds every call the run made.
func checkAzureCallsInRole(t *testing.T, f *fakeARM) {
	t.Helper()

	var role azureRole
	if err := json.Unmarshal([]byte(AzureRoleDefinition(azureTestSub, "talos")), &role); err != nil {
		t.Fatal(err)
	}

	for _, call := range f.calls {
		method, path, _ := strings.Cut(call, " ")
		if action := azureActionOf(method, path); !slices.Contains(role.Actions, action) {
			t.Errorf("%s needs %s, not in the role", call, action)
		}
	}
}

func TestAzureImageArm64IsGenerationV2(t *testing.T) {
	f := &fakeARM{}

	result, msg, _ := startAzureImage(t, f, func(r *azureImageRun) { r.spec.Arch = "arm64"; r.spec.Size = "Standard_D2ps_v5" })
	if msg != "" {
		t.Fatal(msg)
	}

	var r azureImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatal(err)
	}

	if r.Arch != bootArchARM64 || r.Generation != azureGenV2 || azureAt(f.puts[r.ImageID], "properties", "hyperVGeneration") != azureGenV2 ||
		!strings.Contains(f.helper.scripts[0], "/azure-arm64.vhd.xz") {
		t.Errorf("result = %+v", r)
	}

	for path, body := range f.puts {
		if strings.Contains(path, "/virtualMachines/") && azureAt(body, "properties", "storageProfile", "imageReference", "sku") != "server-arm64" {
			t.Errorf("helper image = %v", azureAt(body, "properties", "storageProfile", "imageReference"))
		}
	}
}

func TestAzureImageRunCommandRetriesWhileTheAgentIsBusy(t *testing.T) {
	f := &fakeARM{helper: newFakeAzureHelper()}
	f.helper.runConflicts = 2

	if _, msg, _ := startAzureImage(t, f, nil); msg != "" || f.helper.runPosts != 3 {
		t.Errorf("error %q, Run Command posts %d", msg, f.helper.runPosts)
	}
}

func TestAzureImageFailuresCleanUp(t *testing.T) {
	cases := []struct {
		name        string
		helper      func(*fakeAzureHelper)
		wantInError []string
		imagePut    bool
	}{
		{
			name: "the write fails: no image, the helper and its disk deleted",
			helper: func(h *fakeAzureHelper) {
				h.output = "[stdout]\nichor: writing\n" + awsImageMarkerFail + ": line 9 exited 22\n"
			},
			wantInError: []string{"could not write the image: line 9 exited 22", "Deleted: VM ichor-image-", "data disk ichor-image-"},
		},
		{
			name:        "no marker in the output: no image of a disk of zeros",
			helper:      func(h *fakeAzureHelper) { h.output = "[stdout]\n\n[stderr]\ncurl: (6) Could not resolve host\n" },
			wantInError: []string{"shows no result", "Could not resolve host", "Deleted: VM"},
		},
		{
			name:        "Run Command never ends",
			helper:      func(h *fakeAzureHelper) { h.runNeverEnds = true },
			wantInError: []string{"did not finish writing the image", "Deleted: VM"},
		},
		{
			name:        "the agent is never ready",
			helper:      func(h *fakeAzureHelper) { h.runConflicts = azureImageRunTries },
			wantInError: []string{"Run command extension execution is in progress", "Deleted: VM"},
		},
		{
			name:        "deallocate refused",
			helper:      func(h *fakeAzureHelper) { h.failDeallocate = true },
			wantInError: []string{"The VM is busy", "Deleted: VM", "data disk"},
		},
		{
			name:        "a refused image PUT made nothing",
			helper:      func(h *fakeAzureHelper) { h.imageStatus = http.StatusBadRequest },
			wantInError: []string{"image broke", "Deleted: VM"}, imagePut: true,
		},
		{
			name:        "an image PUT without a clear answer: the image is deleted",
			helper:      func(h *fakeAzureHelper) { h.imageStatus = http.StatusInternalServerError },
			wantInError: []string{"image broke", "unfinished image talos-v1.14.2-azure-amd64-"}, imagePut: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeARM{helper: newFakeAzureHelper()}
			c.helper(f.helper)

			result, msg, _ := startAzureImage(t, f, nil)
			if result != "" {
				t.Errorf("result = %q", result)
			}

			for _, want := range c.wantInError {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want %q in it", msg, want)
				}
			}

			if azureLeaks(msg) {
				t.Errorf("error carries a secret: %q", msg)
			}

			imagePut := slices.ContainsFunc(f.putOrder, func(p string) bool { return strings.Contains(p, "/Microsoft.Compute/images/") })
			if left := azureLeft(f); len(left) != 0 || azureMadeImage(f) != "" || imagePut != c.imagePut {
				t.Errorf("left behind %v, image %q, image put %v", left, azureMadeImage(f), imagePut)
			}

			if entries := readAudit(t, "fake", azureImageAudit); len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestAzureImageFailedDeleteNamesTheResource(t *testing.T) {
	f := &fakeARM{helper: newFakeAzureHelper()}
	f.helper.failDeleteOf = "-data"

	_, msg, _ := startAzureImage(t, f, nil)

	for _, want := range []string{
		"image talos-v1.14.2-azure-amd64-", "is ready", "data disk ichor-image-", "/subscriptions/…/resourceGroups/talos/providers/Microsoft.Compute/disks/ichor-image-",
		"refused by a lock", "still billed", "resource group talos", "Azure portal",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want %q in it", msg, want)
		}
	}

	if azureLeaks(msg) {
		t.Errorf("error carries a secret: %q", msg)
	}
}

func TestAzureImageRefusedBeforeAnyWrite(t *testing.T) {
	existing := map[string]any{"id": azureTestImages + "/talos-old", "name": "talos-old", "location": "westeurope",
		"tags": map[string]string{azureTagTalosVersion: "v1.14.2", azureTagSchematic: "SCHEMATIC", azureTagArch: bootArchAMD64}}

	cases := []struct {
		name   string
		fake   func(*fakeARM)
		edit   func(*azureImageRun)
		want   string
		withID bool // the existing image carries the template's schematic
	}{
		{name: "a principal that may not run the helper", fake: func(f *fakeARM) { f.noCreateRight = true }, want: "may not Microsoft.Network/networkInterfaces/write"},
		{name: "permissions that cannot be read", fake: func(f *fakeARM) { f.noPermissionsRead = true }, want: "may not read its own permissions"},
		{name: "unknown architecture", edit: func(r *azureImageRun) { r.spec.Arch = "riscv64" }, want: "unknown architecture"},
		{name: "no subnet", edit: func(r *azureImageRun) { r.spec.SubnetID = " " }, want: "pick a resource group, a location and a subnet"},
		{name: "a bad location", edit: func(r *azureImageRun) { r.spec.Location = "West Europe" }, want: "not an Azure location"},
		{name: "a security group of another location", edit: func(r *azureImageRun) { r.spec.SecurityGroupID = azureTestNSGOther }, want: "is in northeurope"},
		{name: "a size of another architecture", edit: func(r *azureImageRun) { r.spec.Size = "Standard_D2ps_v5" }, want: "is arm64, the image amd64"},
		{name: "an unknown size", edit: func(r *azureImageRun) { r.spec.Size = "Standard_Nope" }, want: "no VM size Standard_Nope"},
		{name: "an invalid name", edit: func(r *azureImageRun) { r.spec.Name = "a|b" }, want: "not an image name"},
		{name: "an image of this version and schematic exists", withID: true, want: "exists: talos-old. Pick it, or give the new one a name"},
		{name: "a name taken", edit: func(r *azureImageRun) { r.spec.Name = "talos-old" }, want: "an image named talos-old exists in talos"},
		{name: "a member that cannot be read", edit: func(r *azureImageRun) { r.spec.TemplateNode = "192.0.2.99" }, want: "192.0.2.99"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)

			f := &fakeARM{helper: newFakeAzureHelper()}
			if c.fake != nil {
				c.fake(f)
			}

			f.helper.existing = []any{existing}
			startFakeARM(t, f)

			rec := newJoinRecorder()

			run := azureImageTestRun(cfg, rec)
			if c.edit != nil {
				c.edit(run)
			}

			if c.withID {
				media, err := cloudImageMedia(cfg, "fake", joinTemplateNode)
				if err != nil {
					t.Fatal(err)
				}

				existing["tags"].(map[string]string)[azureTagSchematic] = media.target().schematic //nolint:forcetypeassert // built above

				t.Cleanup(func() { existing["tags"].(map[string]string)[azureTagSchematic] = "SCHEMATIC" }) //nolint:forcetypeassert // built above
			}

			run.start(time.Minute)

			if _, msg, _ := rec.wait(t); !strings.Contains(msg, c.want) || azureLeaks(msg) {
				t.Errorf("error = %q, want %q in it", msg, c.want)
			}

			if len(f.putOrder) != 0 || len(f.helper.scripts) != 0 || len(f.deleted) != 0 {
				t.Errorf("writes: puts %v, scripts %d, deletes %v", f.putOrder, len(f.helper.scripts), f.deleted)
			}
		})
	}
}

// Without the template's config the schematic is unknown: no image of the wrong Talos.
func TestAzureImageRefusesAnUnreadableSchematic(t *testing.T) {
	withDataDir(t)

	talos := newFakeTalos()
	talos.addNode(t, joinTemplateNode, "v1.14.2", machine.TypeWorker)
	cfg := talos.start(t, joinTemplateNode)

	f := &fakeARM{helper: newFakeAzureHelper()}
	startFakeARM(t, f)

	rec := newJoinRecorder()
	azureImageTestRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "schematic") || len(f.putOrder) != 0 {
		t.Errorf("error = %q, puts = %v", msg, f.putOrder)
	}
}

func TestAzureImageStopCleansUp(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	writing := make(chan struct{})
	f := &fakeARM{helper: newFakeAzureHelper()}
	f.helper.runNeverEnds = true
	f.helper.onWriting = func() { close(writing) }

	startFakeARM(t, f)

	rec := newJoinRecorder()
	run := azureImageTestRun(cfg, rec)
	run.writeWait = time.Hour
	joinRun := run.start(time.Minute)

	select {
	case <-writing:
	case <-time.After(10 * time.Second):
		t.Fatal("Run Command was never followed")
	}

	joinRun.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Deleted: VM ichor-image-") || len(azureLeft(f)) != 0 || azureMadeImage(f) != "" {
		t.Errorf("error = %q, left %v", msg, azureLeft(f))
	}
}

func TestAzureImageDemoRefused(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := newJoinRecorder()
	StartAzureImage(cfg, "", azureTestCred, `{"resourceGroup":"talos","location":"westeurope","subnetId":"`+azureTestSubnet+`"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo image = %q", msg)
	}
}

func TestAzureImageEstimate(t *testing.T) {
	f := &fakeARM{}
	startFakeARM(t, f)

	out, err := AzureImageEstimate(azureTestCred, bootArchAMD64, "westeurope")
	if c := decodeJSON[azureImageCost](t, out, err); c.Size != azureImageSizeAMD64 || c.Hourly != "0.0480" || c.Currency != "USD" ||
		!strings.Contains(c.ImageNote, "per month") || c.Minutes != azureImageMinutes {
		t.Errorf("estimate = %+v", c)
	}

	out, err = AzureImageEstimate(azureTestCred, "aarch64", "northeurope")
	if c := decodeJSON[azureImageCost](t, out, err); c.Size != azureImageSizeARM64 || !strings.Contains(f.priceFilter, "'northeurope'") ||
		!strings.Contains(f.priceFilter, "'"+azureImageSizeARM64+"'") {
		t.Errorf("arm64 estimate = %+v, filter %s", c, f.priceFilter)
	}

	f.noPrices = true

	out, err = AzureImageEstimate(azureTestCred, bootArchAMD64, "westeurope")
	if c := decodeJSON[azureImageCost](t, out, err); c.Hourly != "" || c.Currency != "" || c.Size == "" || c.ImageNote == "" {
		t.Errorf("estimate without prices = %+v", c)
	}

	if _, err := AzureImageEstimate(azureTestCred, "", "westeurope"); err == nil || !strings.Contains(err.Error(), "pick an architecture") {
		t.Errorf("no architecture: %v", err)
	}

	if _, err := AzureImageEstimate(azureTestCred, bootArchAMD64, "West Europe"); err == nil {
		t.Error("a location that is not one went into the filter")
	}

	if len(f.calls) != 0 {
		t.Errorf("an estimate called ARM: %v", f.calls)
	}
}

func TestAzureRoleCoversTheImageRun(t *testing.T) {
	var role azureRole
	if err := json.Unmarshal([]byte(AzureRoleDefinition(azureTestSub, "talos")), &role); err != nil {
		t.Fatal(err)
	}

	for _, want := range azureImageActions {
		if !slices.Contains(role.Actions, want) {
			t.Errorf("%s not in the role", want)
		}
	}
}

// An image the app made says its architecture in a tag: Options and the create read it.
func TestAzureManagedImageArchFromTag(t *testing.T) {
	var i azureAPIImage
	if i.managedArch() != bootArchAMD64 {
		t.Error("an untagged image is not amd64")
	}

	i.Tags = map[string]string{azureTagArch: bootArchARM64}
	if i.managedArch() != bootArchARM64 {
		t.Error("the tag is not read")
	}
}
