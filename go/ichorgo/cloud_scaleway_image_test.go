package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const (
	scwTestHelperImage = "img-ubuntu-amd64"
	scwTestSnapshot    = "snap-1"
	scwTestNewImage    = "img-new"
	scwBlockZonePath   = "/block/v1/zones/fr-par-1"
	scwVerdictPath     = scwZonePath + "/servers/" + scwTestServer + "/user_data/" + scwImageVerdictKey
)

// fakeScwHelper is the image run's side of fakeScaleway: the marketplace, the helper's verdict,
// the Block Storage snapshot and the image. fakeScaleway's lock covers it.
type fakeScwHelper struct {
	f *fakeScaleway

	noHelper       bool
	nameTaken      string // an image of the project has this name
	verdict        string // what the helper writes after verdictAfter reads; "" never
	verdictAfter   int
	snapshotStatus int // the snapshot POST's answer, the snapshot still made on a 5xx
	imageStatus    int // the image POST's answer, the image still made on a 5xx

	market         url.Values
	verdictReads   int
	snapshotPolls  int
	imagePolls     int
	snapshotBody   map[string]any
	imageBody      map[string]any
	snapshotExists bool
	imageExists    bool
}

func newFakeScwHelper(f *fakeScaleway) *fakeScwHelper {
	h := &fakeScwHelper{f: f, verdict: scwImageVerdictOK, verdictAfter: 2}
	f.extra = h.handle

	return h
}

func (h *fakeScwHelper) handle(w http.ResponseWriter, r *http.Request) bool {
	path, query := r.URL.Path, r.URL.Query()

	switch {
	case r.Method == http.MethodGet && path == "/marketplace/v2/local-images":
		h.market = query
		h.localImages(w, query.Get("arch"))
	case r.Method == http.MethodGet && path == scwVerdictPath:
		h.verdictReads++

		if h.verdict == "" || h.verdictReads <= h.verdictAfter {
			scwFail(w, http.StatusNotFound, "key not found")

			return true
		}

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, h.verdict)
	case path == scwBlockZonePath+"/snapshots":
		h.snapshots(w, r)
	case path == scwBlockZonePath+"/snapshots/"+scwTestSnapshot:
		h.snapshot(w, r)
	case path == scwZonePath+"/images" && r.Method == http.MethodPost:
		h.createImage(w, r)
	case path == scwZonePath+"/images":
		h.images(w, query)
	case path == scwZonePath+"/images/"+scwTestNewImage:
		h.image(w, r)
	default:
		return false
	}

	return true
}

func (h *fakeScwHelper) localImages(w http.ResponseWriter, arch string) {
	if h.noHelper {
		fmt.Fprint(w, `{"local_images":[],"total_count":0}`)

		return
	}

	images := map[string]string{
		"x86_64": `{"id":"` + scwTestHelperImage + `","arch":"x86_64","zone":"fr-par-1","compatible_commercial_types":["DEV1-S","DEV1-M"],"label":"ubuntu_noble","type":"instance_sbs"}`,
		"arm64":  `{"id":"img-ubuntu-arm64","arch":"arm64","zone":"fr-par-1","compatible_commercial_types":["COPARM1-2C-8G"],"label":"ubuntu_noble","type":"instance_sbs"}`,
	}

	fmt.Fprintf(w, `{"local_images":[%s],"total_count":1}`, images[arch])
}

func readBody(r *http.Request) map[string]any {
	data, _ := io.ReadAll(r.Body)

	var body map[string]any
	_ = json.Unmarshal(data, &body)

	return body
}

func (h *fakeScwHelper) snapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("tags") != "" && h.snapshotExists {
			fmt.Fprintf(w, `{"snapshots":[{"id":%q,"status":"creating"}],"total_count":1}`, scwTestSnapshot)

			return
		}

		fmt.Fprint(w, `{"snapshots":[],"total_count":0}`)

		return
	}

	h.snapshotBody = readBody(r)
	h.f.writes = append(h.f.writes, "snapshot")

	if h.snapshotStatus != 0 {
		h.snapshotExists = h.snapshotStatus >= http.StatusInternalServerError
		scwFail(w, h.snapshotStatus, "snapshot broke")

		return
	}

	h.snapshotExists = true
	fmt.Fprintf(w, `{"id":%q,"name":"x","status":"creating"}`, scwTestSnapshot)
}

func (h *fakeScwHelper) snapshot(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodDelete:
		h.f.writes = append(h.f.writes, "delete "+scwTestSnapshot)
		h.snapshotExists = false
		w.WriteHeader(http.StatusNoContent)
	case !h.snapshotExists:
		scwFail(w, http.StatusNotFound, "snapshot not found")
	default:
		h.snapshotPolls++

		status := "available"
		if h.snapshotPolls < 2 {
			status = "creating"
		}

		fmt.Fprintf(w, `{"id":%q,"status":%q}`, scwTestSnapshot, status)
	}
}

func (h *fakeScwHelper) createImage(w http.ResponseWriter, r *http.Request) {
	h.imageBody = readBody(r)
	h.f.writes = append(h.f.writes, "image")

	if h.imageStatus != 0 {
		h.imageExists = h.imageStatus >= http.StatusInternalServerError
		scwFail(w, h.imageStatus, "image broke")

		return
	}

	h.imageExists = true
	fmt.Fprintf(w, `{"image":{"id":%q,"state":"creating"}}`, scwTestNewImage)
}

func (h *fakeScwHelper) newImageJSON(state string) string {
	name, _ := h.imageBody["name"].(string)
	tags, _ := json.Marshal(h.imageBody["tags"])

	return fmt.Sprintf(`{"id":%q,"name":%q,"arch":"x86_64","state":%q,"project":%q,"tags":%s,"root_volume":{"size":10000000000,"volume_type":"sbs_snapshot"}}`,
		scwTestNewImage, name, state, scwTestProject, tags)
}

func (h *fakeScwHelper) images(w http.ResponseWriter, query url.Values) {
	switch {
	case query.Get("name") != "" && query.Get("name") == h.nameTaken:
		fmt.Fprintf(w, `{"images":[{"id":"img-taken","name":%q,"arch":"x86_64","state":"available"}]}`, h.nameTaken)
	case query.Get("name") != "":
		// The API's name filter is a substring match: a longer name is not the same one.
		fmt.Fprintf(w, `{"images":[{"id":"img-longer","name":%q,"arch":"x86_64","state":"available"}]}`, query.Get("name")+"-old")
	case h.imageExists:
		fmt.Fprintf(w, `{"images":[%s]}`, h.newImageJSON("available"))
	default:
		fmt.Fprint(w, `{"images":[]}`)
	}
}

func (h *fakeScwHelper) image(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodDelete:
		h.f.writes = append(h.f.writes, "delete "+scwTestNewImage)
		h.imageExists = false
		w.WriteHeader(http.StatusNoContent)
	case !h.imageExists:
		scwFail(w, http.StatusNotFound, "image not found")
	default:
		h.imagePolls++

		state := "available"
		if h.imagePolls < 2 {
			state = "creating"
		}

		fmt.Fprintf(w, `{"image":%s}`, h.newImageJSON(state))
	}
}

func scwImageTestRun(cfg string, rec *joinRecorder) *scwImageRun {
	return &scwImageRun{
		config: cfg, context: "fake", credJSON: scwTestCred, spec: scwImageSpec{TemplateNode: joinTemplateNode},
		listener: maskedJoinListener{rec}, poll: joinPollInterval, stopWait: 100 * joinPollInterval, snapshotWait: 100 * joinPollInterval,
	}
}

func startScwImage(t *testing.T, f *fakeScaleway, edit func(*scwImageRun)) (string, string, []joinProgress) {
	t.Helper()
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	startFakeScaleway(t, f)

	rec := newJoinRecorder()

	run := scwImageTestRun(cfg, rec)
	if edit != nil {
		edit(run)
	}

	run.start(time.Minute)

	result, msg, events := rec.wait(t)

	return result, msg, events
}

func TestScalewayImageHappyPath(t *testing.T) {
	f := &fakeScaleway{}
	h := newFakeScwHelper(f)

	result, msg, events := startScwImage(t, f, nil)
	if msg != "" {
		t.Fatalf("error %q, events %+v", msg, events)
	}

	want := []string{cloudPhaseCreating, hetznerPhaseWriting, hetznerPhaseSnapshot, scwPhaseRegister, cloudPhaseCleanup, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	wantWrites := []string{
		"ip", "server", "user data", "poweron", "poweroff", "snapshot", "image",
		"delete server", "delete vol-root", "delete vol-data", "delete ip",
	}
	if !slices.Equal(f.writes, wantWrites) {
		t.Errorf("writes = %v, want %v", f.writes, wantWrites)
	}

	var r scwImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}

	if r.ImageID != scwTestNewImage || r.SnapshotID != scwTestSnapshot || r.Arch != bootArchAMD64 || r.TalosVersion != "v1.14.2" ||
		len(r.Schematic) != 64 || r.SizeGB != scwImageVolumeGB || r.Name != "talos-v1.14.2-scaleway-amd64-"+r.Schematic[:12] {
		t.Errorf("result = %+v", r)
	}

	run := checkScwHelper(t, f, h, r.Schematic)
	checkScwSnapshotAndImage(t, h, r, run)

	if !h.imageExists || !h.snapshotExists {
		t.Errorf("the image (%v) or its snapshot (%v) is gone", h.imageExists, h.snapshotExists)
	}

	for _, e := range events {
		if strings.Contains(e.Message, scwTestSecret) {
			t.Errorf("a progress line carries the secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", scwImageAuditAction)
	if len(entries) != 1 || entries[0].Outcome != auditOK ||
		!strings.Contains(entries[0].Params, "arch=amd64 zone=fr-par-1 version=v1.14.2 image="+scwTestNewImage) ||
		strings.Contains(entries[0].Params, scwTestSecret) {
		t.Errorf("audit = %+v", entries)
	}

	// The create card then lists the new image as a Talos one.
	out, err := ScalewayOptions("", "", scwTestCred)
	if o := decodeJSON[scwOptions](t, out, err); len(o.Images) != 1 || o.Images[0].ID != scwTestNewImage || !o.Images[0].Talos ||
		o.Images[0].TalosVersion != "v1.14.2" || o.Images[0].Schematic != r.Schematic[:scwSchematicShown] {
		t.Errorf("options images = %+v", o.Images)
	}
}

// checkScwHelper checks the helper's marketplace lookup, its create and its script; it returns
// the run's id.
func checkScwHelper(t *testing.T, f *fakeScaleway, h *fakeScwHelper, schematic string) string {
	t.Helper()

	if m := h.market; m.Get("image_label") != scwImageHelperLabel || m.Get("type") != scwImageHelperKind || m.Get("zone") != "fr-par-1" || m.Get("arch") != "x86_64" {
		t.Errorf("marketplace query = %v", m)
	}

	b := f.serverBody
	volume, _ := b["volumes"].(map[string]any)["1"].(map[string]any)
	tags := fmt.Sprint(b["tags"])

	_, run, _ := strings.Cut(tags, scwTagRun)
	run, _, _ = strings.Cut(run, " ")

	if b["name"] != "ichor-image-"+run || b["commercial_type"] != "DEV1-S" || b["image"] != scwTestHelperImage || b["project"] != scwTestProject ||
		b["routed_ip_enabled"] != true || fmt.Sprint(b["public_ips"]) != "["+scwTestIP+"]" || len(run) != 16 ||
		volume["volume_type"] != scwVolumeBlock || volume["size"] != float64(scwImageVolumeGB*scwGB) || volume["name"] != "ichor-image-"+run+"-data" {
		t.Errorf("POST /servers = %+v", b)
	}

	script := f.userData
	for _, want := range []string{
		"#!/bin/bash", "set -euo pipefail", "/image/" + schematic + "/v1.14.2/scaleway-amd64.raw.zst",
		`zstd -dc | dd of="$disk" bs=4M conv=fsync`, "apt-get install -y -qq zstd", "findmnt -no SOURCE /", "size=10000000000",
		"--local-port 1-1023", "http://169.254.42.42/user_data/" + scwImageVerdictKey, "verdict OK", `verdict "FAIL: line $LINENO exited $rc"`,
		"shutdown -h now",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q:\n%s", want, script)
		}
	}

	if f.contentType != "text/plain" || len(script) > scwUserDataLimit {
		t.Errorf("user data: %s, %d bytes", f.contentType, len(script))
	}

	return run
}

func checkScwSnapshotAndImage(t *testing.T, h *fakeScwHelper, r scwImageResult, run string) {
	t.Helper()

	wantTags := []string{"talos", "ichor-managed", scwTagRun + run, "schematic=" + r.Schematic[:12], "version=v1.14.2", "arch=amd64"}

	s := h.snapshotBody
	if s["volume_id"] != "vol-data" || s["project_id"] != scwTestProject || s["name"] != r.Name || fmt.Sprint(s["tags"]) != fmt.Sprint(wantTags) {
		t.Errorf("POST snapshot = %+v", s)
	}

	i := h.imageBody
	if i["root_volume"] != scwTestSnapshot || i["arch"] != "x86_64" || i["project"] != scwTestProject || i["name"] != r.Name ||
		i["public"] != false || fmt.Sprint(i["tags"]) != fmt.Sprint(wantTags) {
		t.Errorf("POST image = %+v", i)
	}
}

func TestScalewayImageFailuresCleanUp(t *testing.T) {
	afterPowerOff := []string{"ip", "server", "user data", "poweron", "poweroff", "snapshot"}
	helperStopped := []string{"delete server", "delete vol-root", "delete vol-data", "delete ip"}

	cases := []struct {
		name        string
		fake        func(*fakeScaleway, *fakeScwHelper)
		edit        func(*scwImageRun)
		wantInError []string
		wantWrites  []string
	}{
		{
			name:        "the write fails: no snapshot, the running helper terminated",
			fake:        func(_ *fakeScaleway, h *fakeScwHelper) { h.verdict = "FAIL: line 9 exited 22" },
			wantInError: []string{"could not write the image: line 9 exited 22", "Deleted: Instance " + scwTestServer},
			wantWrites:  []string{"ip", "server", "user data", "poweron", "terminate", "delete vol-data", "delete ip"},
		},
		{
			name:        "no verdict in time: no snapshot of a volume of zeros",
			fake:        func(_ *fakeScaleway, h *fakeScwHelper) { h.verdict = "" },
			edit:        func(r *scwImageRun) { r.stopWait = 5 * joinPollInterval },
			wantInError: []string{"did not finish writing the image", "Deleted: Instance"},
			wantWrites:  []string{"ip", "server", "user data", "poweron", "terminate", "delete vol-data", "delete ip"},
		},
		{
			name:        "a refused snapshot left nothing",
			fake:        func(_ *fakeScaleway, h *fakeScwHelper) { h.snapshotStatus = http.StatusBadRequest },
			wantInError: []string{"snapshot broke", "Deleted: Instance " + scwTestServer},
			wantWrites:  append(slices.Clone(afterPowerOff), helperStopped...),
		},
		{
			name:        "a snapshot without a clear answer: found by its tag and deleted",
			fake:        func(_ *fakeScaleway, h *fakeScwHelper) { h.snapshotStatus = http.StatusBadGateway },
			wantInError: []string{"snapshot broke", "unfinished snapshot " + scwTestSnapshot},
			wantWrites:  append(append(slices.Clone(afterPowerOff), "delete "+scwTestSnapshot), helperStopped...),
		},
		{
			name:        "an image without a clear answer: found by its tag, deleted before its snapshot",
			fake:        func(_ *fakeScaleway, h *fakeScwHelper) { h.imageStatus = http.StatusInternalServerError },
			wantInError: []string{"image broke", "unfinished image " + scwTestNewImage, "unfinished snapshot " + scwTestSnapshot},
			wantWrites:  append(append(slices.Clone(afterPowerOff), "image", "delete "+scwTestNewImage, "delete "+scwTestSnapshot), helperStopped...),
		},
		{
			name:        "a helper made without an answer: found by its tag and deleted",
			fake:        func(f *fakeScaleway, _ *fakeScwHelper) { f.createStatus = http.StatusBadGateway },
			wantInError: []string{"may exist without an answer", "Deleted: Instance " + scwTestServer},
			wantWrites:  []string{"ip", "server", "delete server", "delete vol-root", "delete vol-data", "delete ip"},
		},
		{
			name:        "a refused helper: its IP is deleted",
			fake:        func(f *fakeScaleway, _ *fakeScwHelper) { f.createStatus = http.StatusBadRequest },
			wantInError: []string{"something broke", "Deleted: flexible IP " + scwTestIP},
			wantWrites:  []string{"ip", "server", "delete ip"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeScaleway{}
			h := newFakeScwHelper(f)
			c.fake(f, h)

			result, msg, _ := startScwImage(t, f, c.edit)
			if result != "" {
				t.Errorf("result = %q", result)
			}

			for _, want := range c.wantInError {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want %q in it", msg, want)
				}
			}

			if strings.Contains(msg, scwTestSecret) || strings.Contains(msg, "still billed") {
				t.Errorf("error = %q", msg)
			}

			if !slices.Equal(f.writes, c.wantWrites) || h.snapshotExists || h.imageExists {
				t.Errorf("writes = %v, want %v; left: snapshot %v, image %v", f.writes, c.wantWrites, h.snapshotExists, h.imageExists)
			}

			if entries := readAudit(t, "fake", scwImageAuditAction); len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestScalewayImageRefusedBeforeAnyWrite(t *testing.T) {
	cases := []struct {
		name   string
		fake   func(*fakeScaleway, *fakeScwHelper)
		edit   func(*scwImageRun)
		want   string
		noCall bool
	}{
		{name: "a key that may not create", fake: func(f *fakeScaleway, _ *fakeScwHelper) { f.noCreateRight = true }, want: "may not run the helper"},
		{name: "unknown architecture", edit: func(r *scwImageRun) { r.spec.Arch = "riscv64" }, want: "unknown architecture"},
		{name: "a zone that is not one", edit: func(r *scwImageRun) { r.spec.Zone = "../x" }, want: "unknown Scaleway zone"},
		{name: "no helper image for the architecture", fake: func(_ *fakeScaleway, h *fakeScwHelper) { h.noHelper = true }, want: "no Ubuntu image for amd64 in fr-par-1"},
		{name: "a type of another architecture", edit: func(r *scwImageRun) { r.spec.Type = "COPARM1-2C-8G" }, want: "cannot run the x86_64 helper"},
		{name: "a type the zone does not sell", edit: func(r *scwImageRun) { r.spec.Type = "VC1S" }, want: "cannot be ordered"},
		{
			name: "an image of this version and schematic exists",
			fake: func(_ *fakeScaleway, h *fakeScwHelper) { h.nameTaken = "talos-v1.14.2-scaleway-amd64-" },
			edit: func(r *scwImageRun) { r.spec.Name = "talos-v1.14.2-scaleway-amd64-" },
			want: `already named "talos-v1.14.2-scaleway-amd64-"`,
		},
		{name: "a name that is not one", edit: func(r *scwImageRun) { r.spec.Name = "a\nb" }, want: "not an image name"},
		{name: "a member that cannot be read", edit: func(r *scwImageRun) { r.spec.TemplateNode = "192.0.2.99" }, want: "192.0.2.99"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeScaleway{}
			h := newFakeScwHelper(f)

			if c.fake != nil {
				c.fake(f, h)
			}

			_, msg, _ := startScwImage(t, f, c.edit)
			if !strings.Contains(msg, c.want) || strings.Contains(msg, scwTestSecret) {
				t.Errorf("error = %q, want %q in it", msg, c.want)
			}

			if len(f.writes) != 0 {
				t.Errorf("writes = %v", f.writes)
			}
		})
	}
}

// The default name of an image that exists: pick it rather than make a second one.
func TestScalewayImageDefaultNameTaken(t *testing.T) {
	f := &fakeScaleway{}
	h := newFakeScwHelper(f)

	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	media, err := cloudImageMedia(cfg, "fake", joinTemplateNode)
	if err != nil {
		t.Fatal(err)
	}

	h.nameTaken = "talos-v1.14.2-scaleway-amd64-" + media.Schematic[:12]

	startFakeScaleway(t, f)

	rec := newJoinRecorder()
	scwImageTestRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "exists: img-taken. Pick it") || len(f.writes) != 0 {
		t.Errorf("error = %q, writes = %v", msg, f.writes)
	}
}

// Without the template's config the schematic is unknown: no image of the wrong Talos.
func TestScalewayImageRefusesAnUnreadableSchematic(t *testing.T) {
	withDataDir(t)

	talos := newFakeTalos()
	talos.addNode(t, joinTemplateNode, "v1.14.2", machine.TypeWorker)
	cfg := talos.start(t, joinTemplateNode)

	f := &fakeScaleway{}
	newFakeScwHelper(f)
	startFakeScaleway(t, f)

	rec := newJoinRecorder()
	scwImageTestRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "schematic") || len(f.writes) != 0 {
		t.Errorf("error = %q, writes = %v", msg, f.writes)
	}
}

func TestScalewayImageStopDuringTheWrite(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	running := make(chan struct{})
	f := &fakeScaleway{onRunning: func() { close(running) }}
	h := newFakeScwHelper(f)
	h.verdict = ""

	startFakeScaleway(t, f)

	rec := newJoinRecorder()
	run := scwImageTestRun(cfg, rec).start(time.Minute)

	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("the helper never ran")
	}

	run.Cancel()

	// terminate takes the local root: the Block Storage data volume and the IP are deleted after.
	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Deleted: Instance "+scwTestServer) ||
		!slices.Equal(f.writes, []string{"ip", "server", "user data", "poweron", "terminate", "delete vol-data", "delete ip"}) {
		t.Errorf("error = %q, writes = %v", msg, f.writes)
	}
}

func TestScalewayImageDemoRefused(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := newJoinRecorder()
	StartScalewayImage(cfg, "", scwTestCred, `{"zone":"fr-par-1"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo image = %q", msg)
	}
}

func TestScalewayImageEstimate(t *testing.T) {
	f := &fakeScaleway{}
	newFakeScwHelper(f)
	startFakeScaleway(t, f)

	out, err := ScalewayImageEstimate(scwTestCred, bootArchAMD64, "")
	if c := decodeJSON[scwImageCost](t, out, err); c.Type != "DEV1-S" || c.Hourly != "0.0099" || c.Currency != "EUR" ||
		c.Minutes != scwImageMinutes || c.SnapshotGB != scwImageVolumeGB || !strings.Contains(c.Note, "no snapshot price") {
		t.Errorf("estimate = %+v", c)
	}

	out, err = ScalewayImageEstimate(scwTestCred, "aarch64", "fr-par-1")
	if c := decodeJSON[scwImageCost](t, out, err); c.Type != "COPARM1-2C-8G" || c.Hourly != "0.0426" {
		t.Errorf("arm64 estimate = %+v", c)
	}

	if _, err := ScalewayImageEstimate(scwTestCred, "", ""); err == nil || !strings.Contains(err.Error(), "pick an architecture") {
		t.Errorf("no architecture: %v", err)
	}

	if _, err := ScalewayImageEstimate("", bootArchAMD64, ""); err == nil || !strings.Contains(err.Error(), "API key first") {
		t.Errorf("no key: %v", err)
	}

	if len(f.writes) != 0 {
		t.Errorf("an estimate wrote: %v", f.writes)
	}
}

func TestScalewayImageVerdict(t *testing.T) {
	cases := []struct {
		text    string
		ok      bool
		failure string
	}{
		{text: "OK\n", ok: true},
		{text: "FAIL: line 9 exited 22", failure: "line 9 exited 22"},
		{text: "FAIL", failure: "no reason given"},
		{text: "maybe", failure: "an unknown answer: maybe"},
		{text: ""},
	}

	for _, c := range cases {
		if ok, failure := scwImageVerdict(c.text); ok != c.ok || failure != c.failure {
			t.Errorf("%q: %v %q, want %v %q", c.text, ok, failure, c.ok, c.failure)
		}
	}
}

// The printed policy covers the run: images are Instance resources, the snapshot Block Storage.
func TestScalewayPolicyCoversTheImageRun(t *testing.T) {
	out := ScalewayPolicy(scwTestProject)
	if !strings.Contains(out, "InstancesFullAccess") || !strings.Contains(out, "BlockStorageFullAccess") {
		t.Errorf("policy = %s", out)
	}
}
