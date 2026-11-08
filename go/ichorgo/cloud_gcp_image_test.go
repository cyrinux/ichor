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
	gcpTestDebian    = "debian-12-bookworm-v20261020"
	gcpHelperConsole = "[  5.1] google_metadata_script_runner: starting\nichor: writing the image to /dev/disk/by-id/google-talos\n" +
		awsImageMarkerOK + "\n[ 61.0] reboot: Power down\n"
)

// fakeGCEImage is the image run's side of fakeGCE: the helper's states and console, its data
// disk, the images and the global operations. fakeGCE's lock covers it.
type fakeGCEImage struct {
	states         []string // the helper's statuses in turn, the last one repeated
	console        string
	helperStatus   int  // the helper insert's answer, creating nothing
	insertStatus   int  // images.insert's answer, after creating the image when >= 500
	imageOpFails   bool // the image's operation ends with an error, and no image
	diskBusy       int  // how many disk deletes are refused as in use
	slowDiskDelete bool // an accepted disk delete never finishes, and the disk stays
	existing       bool // an image of the default amd64 name is there
	noImageRight   bool
	noDebian       bool
	onRunning      func()

	calls         int
	families      []string
	disks         map[string]bool
	images        map[string]map[string]any
	imageInserts  []map[string]any
	imageRequests []string
	stopped       int
	deletedDisks  []string
	deletedImages []string
}

func newFakeGCEImage() *fakeGCEImage {
	return &fakeGCEImage{
		states: []string{"PROVISIONING", "STAGING", gcpStatusRunning, "STOPPING", gcpStatusStopped}, console: gcpHelperConsole,
		disks: map[string]bool{}, images: map[string]map[string]any{},
	}
}

// debian answers the Debian family image of the helper.
func (h *fakeGCEImage) debian(w http.ResponseWriter, family string) {
	h.families = append(h.families, family)

	if h.noDebian || (family != "debian-12" && family != "debian-12-arm64") {
		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "no family "+family)

		return
	}

	name := gcpTestDebian
	if family == "debian-12-arm64" {
		name = "debian-12-bookworm-arm64-v20261020"
	}

	gceJSON(w, http.StatusOK, map[string]string{
		"name": name, "selfLink": "https://www.googleapis.com/compute/v1/projects/debian-cloud/global/images/" + name, "status": gcpImageReady,
	})
}

// handle answers the calls of the image run; false leaves the call to fakeGCE.
func (h *fakeGCEImage) handle(f *fakeGCE, w http.ResponseWriter, r *http.Request, rest string) bool {
	zone := "/zones/" + gcpTestZone

	switch {
	case r.Method == http.MethodPost && rest == zone+"/instances":
		h.insertHelper(f, w, r)
	case strings.HasPrefix(rest, zone+"/instances/ichor-image-"):
		return h.helper(f, w, r, strings.TrimPrefix(rest, zone+"/instances/"))
	case r.Method == http.MethodDelete && strings.HasPrefix(rest, zone+"/disks/"):
		h.deleteDisk(f, w, strings.TrimPrefix(rest, zone+"/disks/"))
	case r.Method == http.MethodPost && rest == "/global/images":
		h.insertImage(f, w, r)
	case r.Method == http.MethodPost && strings.HasPrefix(rest, "/global/operations/") && strings.HasSuffix(rest, "/wait"):
		name := strings.TrimSuffix(strings.TrimPrefix(rest, "/global/operations/"), "/wait")
		if op, ok := f.ops[name]; ok {
			gceJSON(w, http.StatusOK, op)
		} else {
			gceFail(w, http.StatusNotFound, gcpReasonNotFound, "no operation "+name)
		}
	case strings.HasPrefix(rest, "/global/images/"):
		return h.image(f, w, r, strings.TrimPrefix(rest, "/global/images/"))
	default:
		return false
	}

	return true
}

func (h *fakeGCEImage) insertHelper(f *fakeGCE, w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	f.inserted = append(f.inserted, body)
	f.requestIDs = append(f.requestIDs, r.URL.Query().Get("requestId"))

	if h.helperStatus != 0 {
		gceFail(w, h.helperStatus, "invalid", "the helper was refused")

		return
	}

	name, _ := body["name"].(string)
	f.instances[name] = map[string]any{"labels": body["labels"]}

	if disk, _ := azureAt(body, "disks", 1, "initializeParams", "diskName").(string); disk != "" {
		h.disks[disk] = true
	}

	gceJSON(w, http.StatusOK, f.operation("insert", true))
}

func (h *fakeGCEImage) helper(f *fakeGCE, w http.ResponseWriter, r *http.Request, rest string) bool {
	name, sub, _ := strings.Cut(rest, "/")

	switch {
	case sub == "serialPort":
		gceJSON(w, http.StatusOK, map[string]any{"contents": h.console, "start": "0"})
	case sub == "stop":
		h.stopped++
		h.states = []string{gcpStatusStopped}
		gceJSON(w, http.StatusOK, f.operation("stop", true))
	case r.Method == http.MethodGet && f.instances[name] != nil:
		state := h.states[min(h.calls, len(h.states)-1)]
		h.calls++

		if state == gcpStatusRunning && h.onRunning != nil {
			h.onRunning()
			h.onRunning = nil
		}

		gceJSON(w, http.StatusOK, map[string]any{"name": name, "status": state, "labels": f.instances[name]["labels"]})
	default:
		return false // a delete, or an instance that is not there
	}

	return true
}

func (h *fakeGCEImage) deleteDisk(f *fakeGCE, w http.ResponseWriter, name string) {
	switch {
	case !h.disks[name]:
		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "The resource 'projects/"+gcpTestProject+"/zones/"+gcpTestZone+"/disks/"+name+"' was not found")

		return
	case h.diskBusy > 0:
		h.diskBusy--
		gceFail(w, http.StatusBadRequest, "resourceInUseByAnotherResource", "The disk resource is already being used by another resource")

		return
	}

	h.deletedDisks = append(h.deletedDisks, name)

	if !h.slowDiskDelete {
		delete(h.disks, name)
	}

	gceJSON(w, http.StatusOK, f.operation("delete", !h.slowDiskDelete))
}

func (h *fakeGCEImage) insertImage(f *fakeGCE, w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	h.imageInserts = append(h.imageInserts, body)
	h.imageRequests = append(h.imageRequests, r.URL.Query().Get("requestId"))

	name, _ := body["name"].(string)
	made := map[string]any{
		"name": name, "selfLink": gcpTestLink + "/global/images/" + name, "status": gcpImageReady, "diskSizeGb": "12",
		"architecture": body["architecture"], "labels": body["labels"],
	}

	if h.insertStatus != 0 {
		if h.insertStatus >= http.StatusInternalServerError {
			h.images[name] = made
		}

		gceFail(w, h.insertStatus, "invalid", "the image was refused")

		return
	}

	op := f.operation("image", true)

	if h.imageOpFails {
		op["error"] = map[string]any{"errors": []any{map[string]string{"code": "INVALID_IMAGE", "message": "the disk holds no bootable image"}}}
	} else {
		h.images[name] = made
	}

	gceJSON(w, http.StatusOK, op)
}

func (h *fakeGCEImage) image(f *fakeGCE, w http.ResponseWriter, r *http.Request, name string) bool {
	made, ok := h.images[name]
	if !ok && h.existing && strings.HasPrefix(name, "talos-v1-14-2-gcp-amd64-") {
		made, ok = map[string]any{"name": name, "status": gcpImageReady}, true
	}

	switch {
	case r.Method == http.MethodGet && ok:
		gceJSON(w, http.StatusOK, made)
	case r.Method == http.MethodDelete && ok:
		h.deletedImages = append(h.deletedImages, name)
		delete(h.images, name)
		gceJSON(w, http.StatusOK, f.operation("delete", true))
	case r.Method == http.MethodDelete:
		gceFail(w, http.StatusNotFound, gcpReasonNotFound, "no image "+name)
	default:
		return false
	}

	return true
}

func gcpImageTestRun(t *testing.T, cfg string, rec *joinRecorder) *gcpImageRun {
	t.Helper()

	return &gcpImageRun{
		config: cfg, context: "fake", credJSON: gcpTestCred(t),
		spec:     gcpImageSpec{TemplateNode: joinTemplateNode, Subnetwork: gcpTestSubnet, AssociatePublicIP: true},
		listener: maskedJoinListener{rec}, poll: joinPollInterval, writeWait: 100 * joinPollInterval, imageWait: 100 * joinPollInterval,
	}
}

func startGCPImage(t *testing.T, h *fakeGCEImage, edit func(*gcpImageRun)) (*fakeGCE, string, string, []joinProgress) {
	t.Helper()
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeGCE{image: h}
	startFakeGCE(t, f)

	rec := newJoinRecorder()

	run := gcpImageTestRun(t, cfg, rec)
	if edit != nil {
		edit(run)
	}

	run.start(time.Minute)

	result, msg, events := rec.wait(t)

	return f, result, msg, events
}

func TestGCPImageHappyPath(t *testing.T) {
	h := newFakeGCEImage()
	f, result, msg, events := startGCPImage(t, h, nil)

	if msg != "" {
		t.Fatalf("error %q, events %+v", msg, events)
	}

	want := []string{cloudPhaseCreating, hetznerPhaseWriting, awsPhaseRegister, cloudPhaseCleanup, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	var r gcpImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}

	if len(r.Schematic) != 64 || r.Image != "talos-v1-14-2-gcp-amd64-"+r.Schematic[:12] || r.Arch != bootArchAMD64 || r.TalosVersion != "v1.14.2" ||
		r.SizeGB != gcpImageDiskGB || r.SelfLink != gcpTestLink+"/global/images/"+r.Image {
		t.Errorf("result = %+v", r)
	}

	helper := checkGCPHelperInsert(t, f, "e2-small", gcpTestDebian, "gcp-amd64.raw.tar.gz")
	checkGCPImageInsert(t, h, helper, gcpArchX86, r.Schematic)

	if !slices.Equal(f.deleted, []string{helper}) || !slices.Equal(h.deletedDisks, []string{helper + "-data"}) || len(h.deletedImages) != 0 ||
		len(h.disks) != 0 || len(h.images) != 1 || len(f.instances) != 0 || h.stopped != 0 {
		t.Errorf("deleted %v, disks %v, images deleted %v, left %v %v", f.deleted, h.deletedDisks, h.deletedImages, h.disks, f.instances)
	}

	// The printed role holds every Compute call the run made.
	for _, call := range f.calls {
		method, path, _ := strings.Cut(call, " ")
		if p := gcpPermissionOf(method, gcpComputeEndpoint+path); !slices.Contains(gcpRolePermissions(), p) {
			t.Errorf("%s needs %q, not in the role", call, p)
		}
	}

	for _, e := range events {
		if gcpLeaks(e.Message) {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", gcpImageAudit)
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "arch=amd64 zone=europe-west1-b version=v1.14.2 image="+r.Image) ||
		gcpLeaks(entries[0].Params) {
		t.Errorf("audit = %+v", entries)
	}
}

// checkGCPHelperInsert checks the helper's one insert and returns its name.
func checkGCPHelperInsert(t *testing.T, f *fakeGCE, machineType, debian, archive string) string {
	t.Helper()

	if len(f.inserted) != 1 || !gcpUUIDPattern.MatchString(f.requestIDs[0]) {
		t.Fatalf("inserts %d, request ids %v", len(f.inserted), f.requestIDs)
	}

	body := f.inserted[0]
	name, _ := body["name"].(string)
	labels, _ := body["labels"].(map[string]any)
	run, _ := labels[gcpLabelRun].(string)

	if len(run) != 16 || name != "ichor-image-"+run {
		t.Fatalf("name %q, labels %v", name, labels)
	}

	checks := map[string][2]any{
		"machine type":     {body["machineType"], "zones/europe-west1-b/machineTypes/" + machineType},
		"boot":             {azureAt(body, "disks", 0, "boot"), true},
		"boot auto delete": {azureAt(body, "disks", 0, "autoDelete"), true},
		"helper image":     {azureAt(body, "disks", 0, "initializeParams", "sourceImage"), "https://www.googleapis.com/compute/v1/projects/debian-cloud/global/images/" + debian},
		"data kept":        {azureAt(body, "disks", 1, "autoDelete"), false},
		"device name":      {azureAt(body, "disks", 1, "deviceName"), "talos"},
		"data disk":        {azureAt(body, "disks", 1, "initializeParams", "diskName"), name + "-data"},
		"data size":        {azureAt(body, "disks", 1, "initializeParams", "diskSizeGb"), "12"},
		"data type":        {azureAt(body, "disks", 1, "initializeParams", "diskType"), "zones/europe-west1-b/diskTypes/pd-balanced"},
		"data label":       {azureAt(body, "disks", 1, "initializeParams", "labels", gcpLabelRun), run},
		"subnet":           {azureAt(body, "networkInterfaces", 0, "subnetwork"), "regions/europe-west1/subnetworks/nodes"},
		"nat":              {azureAt(body, "networkInterfaces", 0, "accessConfigs", 0, "type"), "ONE_TO_ONE_NAT"},
		"script key":       {azureAt(body, "metadata", "items", 0, "key"), "startup-script"},
		"no restart":       {azureAt(body, "scheduling", "automaticRestart"), false},
	}

	for what, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %v, want %v", what, c[0], c[1])
		}
	}

	script, _ := azureAt(body, "metadata", "items", 0, "value").(string)
	for _, want := range []string{
		"#!/bin/bash", "set -euo pipefail", "/v1.14.2/" + archive, "tar -xzO disk.raw | dd of=\"$disk\" bs=4M conv=sparse",
		"disk=/dev/disk/by-id/google-talos", `say "$m-OK"`, "$m-FAIL", "shutdown -h now", "/dev/ttyS0",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q:\n%s", want, script)
		}
	}

	// The console's marker never shows in the script itself, else an echo of it would pass.
	if strings.Contains(script, awsImageMarkerOK) || strings.Contains(script, "/dev/sdb") {
		t.Errorf("the script names the marker or a guessed device:\n%s", script)
	}

	return name
}

func checkGCPImageInsert(t *testing.T, h *fakeGCEImage, helper, arch, schematic string) {
	t.Helper()

	if len(h.imageInserts) != 1 || !gcpUUIDPattern.MatchString(h.imageRequests[0]) {
		t.Fatalf("image inserts %d, request ids %v", len(h.imageInserts), h.imageRequests)
	}

	body := h.imageInserts[0]
	labels, _ := body["labels"].(map[string]any)
	features, _ := json.Marshal(body["guestOsFeatures"])
	wantArch := map[string]string{gcpArchX86: bootArchAMD64, gcpArchARM: bootArchARM64}[arch]

	if body["sourceDisk"] != "zones/europe-west1-b/disks/"+helper+"-data" || body["architecture"] != arch || body["family"] != "talos-"+wantArch ||
		!strings.Contains(string(features), gcpFeatureMultiqueue) || strings.Contains(string(features), gcpFeatureUEFI) != (arch == gcpArchARM) ||
		!strings.HasSuffix(body["description"].(string), " gcp "+wantArch+" schematic "+schematic) {
		t.Errorf("image insert = %v", body)
	}

	if labels["os"] != "talos" || labels[gcpLabelTalosVersion] != "v1-14-2" || labels[gcpLabelArch] != wantArch ||
		labels[gcpLabelSchematic] != schematic[:16] || "ichor-image-"+labels[gcpLabelRun].(string) != helper {
		t.Errorf("image labels = %v", labels)
	}
}

func TestGCPImageArm64(t *testing.T) {
	h := newFakeGCEImage()
	f, result, msg, _ := startGCPImage(t, h, func(r *gcpImageRun) { r.spec.Arch = "aarch64" })

	var r gcpImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil || msg != "" {
		t.Fatalf("result %q, error %q", result, msg)
	}

	if !slices.Equal(h.families, []string{"debian-12-arm64"}) || r.Arch != bootArchARM64 || !strings.Contains(r.Image, "-gcp-arm64-") {
		t.Errorf("families %v, result %+v", h.families, r)
	}

	helper := checkGCPHelperInsert(t, f, gcpImageTypeARM64, "debian-12-bookworm-arm64-v20261020", "gcp-arm64.raw.tar.gz")
	checkGCPImageInsert(t, h, helper, gcpArchARM, r.Schematic)
}

func TestGCPImageFailuresCleanUp(t *testing.T) {
	cases := []struct {
		name         string
		helper       func(*fakeGCEImage)
		wantInError  []string
		wantInserts  int
		wantStopped  bool
		wantDeleted  []string // images
		helperFailed bool
	}{
		{
			name:        "the write fails: no image, both deleted",
			helper:      func(h *fakeGCEImage) { h.console = "ichor: writing\n" + awsImageMarkerFail + ": line 9 exited 1\n" },
			wantInError: []string{"could not write the image: line 9 exited 1", "Deleted: VM ichor-image-", "data disk ichor-image-"},
		},
		{
			name:        "no marker on the console: no image of a disk of zeros",
			helper:      func(h *fakeGCEImage) { h.console = "[  5.1] google_metadata_script_runner: starting\n" },
			wantInError: []string{"shows no result", "google_metadata_script_runner: starting", "Deleted: VM"},
		},
		{
			name:        "the helper never powers off: stopped, then deleted",
			helper:      func(h *fakeGCEImage) { h.states = []string{gcpStatusRunning} },
			wantInError: []string{"did not finish writing", "Deleted: VM", "data disk"}, wantStopped: true,
		},
		{
			name:        "a refused image insert left nothing",
			helper:      func(h *fakeGCEImage) { h.insertStatus = http.StatusBadRequest },
			wantInError: []string{"the image was refused", "Deleted: VM", "data disk"}, wantInserts: 1,
		},
		{
			name:        "an image insert without a clear answer: the image is deleted",
			helper:      func(h *fakeGCEImage) { h.insertStatus = http.StatusServiceUnavailable },
			wantInError: []string{"the image was refused", "unfinished image talos-v1-14-2-gcp-amd64-"}, wantInserts: gcpInsertTries, wantDeleted: []string{"talos"},
		},
		{
			name:        "an image whose operation fails",
			helper:      func(h *fakeGCEImage) { h.imageOpFails = true },
			wantInError: []string{"the disk holds no bootable image", "Deleted: VM"}, wantInserts: 1,
		},
		{
			name:         "a refused helper: nothing was made",
			helper:       func(h *fakeGCEImage) { h.helperStatus = http.StatusBadRequest },
			wantInError:  []string{"the helper was refused"},
			helperFailed: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFakeGCEImage()
			c.helper(h)

			f, result, msg, _ := startGCPImage(t, h, func(r *gcpImageRun) { r.writeWait = 20 * joinPollInterval })
			if result != "" {
				t.Errorf("result = %q", result)
			}

			for _, want := range c.wantInError {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want %q in it", msg, want)
				}
			}

			if gcpLeaks(msg) || strings.Contains(msg, "Deleted: unfinished") != (len(c.wantDeleted) > 0) {
				t.Errorf("error = %q", msg)
			}

			if len(h.imageInserts) != c.wantInserts || len(h.deletedImages) != len(c.wantDeleted) || (h.stopped == 1) != c.wantStopped {
				t.Errorf("image inserts %d, deleted images %v, stopped %d", len(h.imageInserts), h.deletedImages, h.stopped)
			}

			if len(h.disks) != 0 || len(h.images) != 0 || len(f.instances) != 0 || (len(f.deleted) == 1) == c.helperFailed {
				t.Errorf("left behind: disks %v, images %v, instances %v, deleted %v", h.disks, h.images, f.instances, f.deleted)
			}

			if entries := readAudit(t, "fake", gcpImageAudit); len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestGCPImageDiskInUseIsRetried(t *testing.T) {
	h := newFakeGCEImage()
	h.diskBusy = 2

	_, result, msg, _ := startGCPImage(t, h, nil)
	if msg != "" || result == "" || len(h.deletedDisks) != 1 || len(h.disks) != 0 || h.diskBusy != 0 {
		t.Errorf("error %q, deleted disks %v, left %v", msg, h.deletedDisks, h.disks)
	}
}

func TestGCPImageUnfinishedDeleteNamesTheDisk(t *testing.T) {
	saved := gcpDeleteWait
	gcpDeleteWait = 20 * joinPollInterval

	t.Cleanup(func() { gcpDeleteWait = saved })

	h := newFakeGCEImage()
	h.slowDiskDelete = true

	_, result, msg, _ := startGCPImage(t, h, nil)

	for _, want := range []string{
		"image talos-v1-14-2-gcp-amd64-", " is ready", "still billed: data disk ichor-image-", "-data (", "had not finished", "in zone europe-west1-b",
		"Google Cloud console",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want %q in it", msg, want)
		}
	}

	if result != "" || len(h.images) != 1 {
		t.Errorf("result %q, images %v", result, h.images)
	}
}

func TestGCPImageStopCleansUp(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	running := make(chan struct{})
	h := newFakeGCEImage()
	h.states = []string{gcpStatusRunning}
	h.onRunning = func() { close(running) }

	f := &fakeGCE{image: h}
	startFakeGCE(t, f)

	rec := newJoinRecorder()
	run := gcpImageTestRun(t, cfg, rec).start(time.Minute)

	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("the helper was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Deleted: VM ichor-image-") || len(f.deleted) != 1 ||
		len(h.deletedDisks) != 1 || len(h.imageInserts) != 0 {
		t.Errorf("error = %q, deleted = %v %v, image inserts = %d", msg, f.deleted, h.deletedDisks, len(h.imageInserts))
	}
}

func TestGCPImageRefusedBeforeAnyWrite(t *testing.T) {
	cases := []struct {
		name   string
		fake   func(*fakeGCE)
		helper func(*fakeGCEImage)
		edit   func(*gcpImageRun)
		want   string
	}{
		{name: "a key that may not make images", helper: func(h *fakeGCEImage) { h.noImageRight = true }, want: "may not use compute.images.create in project example-project"},
		{name: "a check that cannot be made", fake: func(f *fakeGCE) { f.crmDisabled = true }, want: "cannot be checked, so nothing is created"},
		{name: "unknown architecture", edit: func(r *gcpImageRun) { r.spec.Arch = "riscv64" }, want: "unknown architecture"},
		{name: "no subnet", edit: func(r *gcpImageRun) { r.spec.Subnetwork = " " }, want: "pick a zone and a subnet"},
		{name: "a bad zone", edit: func(r *gcpImageRun) { r.spec.Zone = "europe" }, want: "not a Compute Engine zone"},
		{name: "a subnet of another region", edit: func(r *gcpImageRun) { r.spec.Subnetwork = "regions/us-central1/subnetworks/far" }, want: "is in us-central1"},
		{name: "no route to the Image Factory", edit: func(r *gcpImageRun) { r.spec.AssociatePublicIP = false }, want: "no Cloud NAT"},
		{name: "no machine type in the zone", edit: func(r *gcpImageRun) { r.spec.MachineType = "e2-huge" }, want: "no machine type e2-huge in europe-west1-b"},
		{name: "a machine type of another architecture", edit: func(r *gcpImageRun) { r.spec.MachineType = "t2a-standard-1" }, want: "is arm64, the image amd64"},
		{name: "no Debian image", helper: func(h *fakeGCEImage) { h.noDebian = true }, want: "Debian image of the helper (debian-12)"},
		{name: "the image of this version and schematic exists", helper: func(h *fakeGCEImage) { h.existing = true }, want: "this schematic exists: talos-v1-14-2-gcp-amd64-"},
		{name: "a name taken", edit: func(r *gcpImageRun) { r.spec.Name = "talos-by-hand" }, want: "an image named talos-by-hand exists"},
		{name: "an invalid name", edit: func(r *gcpImageRun) { r.spec.Name = "Talos_1" }, want: "not an image name"},
		{name: "a member that cannot be read", edit: func(r *gcpImageRun) { r.spec.TemplateNode = "192.0.2.99" }, want: "192.0.2.99"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			h := newFakeGCEImage()

			if c.helper != nil {
				c.helper(h)
			}

			f := &fakeGCE{image: h}
			if c.fake != nil {
				c.fake(f)
			}

			startFakeGCE(t, f)

			rec := newJoinRecorder()

			run := gcpImageTestRun(t, cfg, rec)
			if c.edit != nil {
				c.edit(run)
			}

			run.start(time.Minute)

			if _, msg, _ := rec.wait(t); !strings.Contains(msg, c.want) || gcpLeaks(msg) {
				t.Errorf("error = %q, want %q in it", msg, c.want)
			}

			if len(f.inserted) != 0 || len(h.imageInserts) != 0 || len(f.deleted) != 0 || len(h.deletedDisks) != 0 {
				t.Errorf("writes: inserts %d, image inserts %d, deletes %v", len(f.inserted), len(h.imageInserts), f.deleted)
			}
		})
	}
}

// Without the template's config the schematic is unknown: no image of the wrong Talos.
func TestGCPImageRefusesAnUnreadableSchematic(t *testing.T) {
	withDataDir(t)

	talos := newFakeTalos()
	talos.addNode(t, joinTemplateNode, "v1.14.2", machine.TypeWorker)
	cfg := talos.start(t, joinTemplateNode)

	f := &fakeGCE{image: newFakeGCEImage()}
	startFakeGCE(t, f)

	rec := newJoinRecorder()
	gcpImageTestRun(t, cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "schematic") || len(f.inserted) != 0 {
		t.Errorf("error = %q, inserts = %d", msg, len(f.inserted))
	}
}

func TestGCPImageDemoRefused(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := newJoinRecorder()
	StartGCPImage(cfg, "", gcpTestCred(t), `{"zone":"europe-west1-b","subnetwork":"default"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo image = %q", msg)
	}
}

func TestGCPImageEstimate(t *testing.T) {
	f := &fakeGCE{}
	startFakeGCE(t, f)

	out, err := GCPImageEstimate(gcpTestCred(t), bootArchAMD64, "")
	if c := decodeJSON[gcpImageCost](t, out, err); c.MachineType != gcpImageTypeAMD64 || c.Hourly != "0.0170" || c.Currency != "USD" ||
		c.DiskMonthlyPerGB != "0.1100" || c.ImageMonthlyPerGB != "0.0500" || c.Minutes != gcpImageMinutes {
		t.Errorf("estimate = %+v", c)
	}

	out, err = GCPImageEstimate(gcpTestCred(t), "aarch64", "us-central1-a")
	if c := decodeJSON[gcpImageCost](t, out, err); c.MachineType != gcpImageTypeARM64 || c.Hourly != "" || c.DiskMonthlyPerGB != "0.1000" {
		t.Errorf("arm64 estimate = %+v", c)
	}

	f.billingDisabled = true

	out, err = GCPImageEstimate(gcpTestCred(t), bootArchAMD64, "")
	if c := decodeJSON[gcpImageCost](t, out, err); c.Hourly != "" || c.DiskMonthlyPerGB != "" || c.ImageMonthlyPerGB != "" || c.Currency != "" ||
		c.MachineType == "" {
		t.Errorf("estimate without prices = %+v", c)
	}

	if _, err := GCPImageEstimate(gcpTestCred(t), "", ""); err == nil || !strings.Contains(err.Error(), "pick an architecture") {
		t.Errorf("no architecture: %v", err)
	}

	if len(f.inserted) != 0 {
		t.Errorf("an estimate made something: %d inserts", len(f.inserted))
	}
}

func TestGCPImagePermissionsOfItsCalls(t *testing.T) {
	cases := map[string]string{
		"POST " + gcpTestBase + "/zones/" + gcpTestZone + "/instances/ichor-image-1/stop":      "compute.instances.stop",
		"POST " + gcpTestBase + "/global/operations/op-1/wait":                                 "compute.globalOperations.get",
		"POST " + gcpTestBase + "/zones/" + gcpTestZone + "/operations/op-1/wait":              "compute.zoneOperations.get",
		"POST " + gcpTestBase + "/global/images":                                               "compute.images.create",
		"DELETE " + gcpTestBase + "/zones/" + gcpTestZone + "/disks/ichor-image-1-data":        "compute.disks.delete",
		"GET " + gcpTestBase + "/zones/" + gcpTestZone + "/instances/ichor-image-1/serialPort": "compute.instances.getSerialPortOutput",
	}

	for call, want := range cases {
		method, path, _ := strings.Cut(call, " ")
		if got := gcpPermissionOf(method, gcpComputeEndpoint+path); got != want || !slices.Contains(gcpRolePermissions(), got) {
			t.Errorf("%s = %q, want %q in the role", call, got, want)
		}
	}
}
