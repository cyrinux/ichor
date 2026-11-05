package ichorgo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const (
	scwTestProject = "11111111-1111-1111-1111-111111111111"
	scwTestSecret  = "sample-scw-secret"
	scwTestCred    = `{"accessKey":"SCWSAMPLEACCESSKEY00","secretKey":"` + scwTestSecret + `","project":"` + scwTestProject + `"}`
	scwTestServer  = "server-1"
	scwTestIP      = "ip-1"
	scwZonePath    = "/instance/v1/zones/fr-par-1"
)

// fakeScaleway is the part of Scaleway's API the app calls. It records every write in order; the
// server moves stopped → starting → running as the run polls it.
type fakeScaleway struct {
	mu sync.Mutex

	noCreateRight bool
	noTalosImage  bool
	createStatus  int  // the server POST's answer, the server still made on a 5xx
	userDataFails bool // the user data PATCH answers 500
	locked        bool
	onRunning     func()

	writes      []string
	serverBody  map[string]any
	userData    string
	contentType string
	state       string // "" before the create, "gone" after a delete
	dataType    string
}

func startFakeScaleway(t *testing.T, f *fakeScaleway) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client, endpoint := cloudHTTP, scwAPIEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	scwAPIEndpoint = srv.URL

	t.Cleanup(func() { cloudHTTP, scwAPIEndpoint = client, endpoint })
}

func scwFail(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"message":%q,"type":"invalid_arguments"}`, message)
}

func (f *fakeScaleway) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("X-Auth-Token") != scwTestSecret {
		scwFail(w, http.StatusUnauthorized, "bad token "+r.Header.Get("X-Auth-Token"))

		return
	}

	path, server := r.URL.Path, scwZonePath+"/servers/"+scwTestServer

	switch {
	case r.Method == http.MethodGet && path == scwZonePath+"/products/servers":
		fmt.Fprint(w, `{"servers":{
			"DEV1-M":{"hourly_price":0.0198,"ncpus":3,"ram":4294967296,"arch":"x86_64","volumes_constraint":{"min_size":0,"max_size":40000000000}},
			"DEV1-S":{"hourly_price":0.0099,"ncpus":2,"ram":2147483648,"arch":"x86_64","volumes_constraint":{"min_size":0,"max_size":20000000000}},
			"COPARM1-2C-8G":{"hourly_price":0.0426,"ncpus":2,"ram":8589934592,"arch":"arm64","volumes_constraint":{"min_size":0,"max_size":0}},
			"VC1S":{"hourly_price":0.001,"ncpus":2,"ram":2147483648,"arch":"x86_64","end_of_service":true}
		}}`)
	case r.Method == http.MethodGet && path == scwZonePath+"/images":
		f.images(w)
	case r.Method == http.MethodGet && strings.HasPrefix(path, scwZonePath+"/images/"):
		f.image(w, strings.TrimPrefix(path, scwZonePath+"/images/"))
	case r.Method == http.MethodGet && path == scwZonePath+"/security_groups":
		fmt.Fprint(w, `{"security_groups":[{"id":"sg-1","name":"Default security group","project_default":true}]}`)
	case path == scwZonePath+"/servers":
		f.servers(w, r)
	case r.Method == http.MethodPost && path == scwZonePath+"/ips":
		f.writes = append(f.writes, "ip")
		fmt.Fprintf(w, `{"ip":{"id":%q,"address":%q}}`, scwTestIP, joinNewNode)
	case r.Method == http.MethodGet && path == scwZonePath+"/ips":
		fmt.Fprintf(w, `{"ips":[{"id":%q,"address":%q}]}`, scwTestIP, joinNewNode)
	case r.Method == http.MethodDelete && path == scwZonePath+"/ips/"+scwTestIP:
		f.writes = append(f.writes, "delete ip")
	case r.Method == http.MethodDelete && (strings.HasPrefix(path, scwZonePath+"/volumes/") || strings.HasPrefix(path, "/block/v1alpha1/zones/fr-par-1/volumes/")):
		f.writes = append(f.writes, "delete "+path[strings.LastIndexByte(path, '/')+1:])
	case strings.HasPrefix(path, server) && f.state != "" && f.state != "gone":
		f.server(w, r, strings.TrimPrefix(path, server))
	default:
		scwFail(w, http.StatusNotFound, path)
	}
}

func (f *fakeScaleway) images(w http.ResponseWriter) {
	talos, tag := `{"id":"img-talos","name":"talos v1.12 scaleway","arch":"x86_64","state":"available","root_volume":{"size":10000000000,"volume_type":"l_ssd"}},`, "talos-1.12"
	if f.noTalosImage {
		talos, tag = "", "base"
	}

	fmt.Fprint(w, `{"images":[`+talos+`
		{"id":"img-own","name":"node base","arch":"x86_64","state":"available","tags":["`+tag+`"],"root_volume":{"size":10000000000,"volume_type":"l_ssd"}},
		{"id":"img-plain","name":"Debian","arch":"x86_64","state":"available","root_volume":{"size":10000000000,"volume_type":"l_ssd"}},
		{"id":"img-gone","name":"talos old","arch":"x86_64","state":"error"}
	]}`)
}

func (f *fakeScaleway) image(w http.ResponseWriter, id string) {
	project, arch := scwTestProject, "x86_64"

	switch id {
	case "img-talos":
	case "img-arm":
		arch = "arm64"
	case "img-other":
		project = "22222222-2222-2222-2222-222222222222"
	default:
		scwFail(w, http.StatusNotFound, "image not found")

		return
	}

	fmt.Fprintf(w, `{"image":{"id":%q,"name":"talos","arch":%q,"project":%q,"state":"available","root_volume":{"size":10000000000,"volume_type":"l_ssd"}}}`, id, arch, project)
}

func (f *fakeScaleway) servers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if r.URL.Query().Get("tags") != "" && f.state != "" && f.state != "gone" {
			fmt.Fprintf(w, `{"servers":[%s]}`, f.serverJSON())

			return
		}

		fmt.Fprint(w, `{"servers":[]}`)

		return
	}

	if f.noCreateRight {
		scwFail(w, http.StatusForbidden, "insufficient permissions")

		return
	}

	data, _ := io.ReadAll(r.Body)

	var body map[string]any
	_ = json.Unmarshal(data, &body)

	if body["commercial_type"] == nil {
		scwFail(w, http.StatusBadRequest, "commercial_type is required")

		return
	}

	f.serverBody = body
	f.writes = append(f.writes, "server")

	if f.createStatus != 0 && f.createStatus < http.StatusInternalServerError {
		scwFail(w, f.createStatus, "something broke")

		return
	}

	volume, _ := body["volumes"].(map[string]any)["1"].(map[string]any)
	f.dataType, _ = volume["volume_type"].(string)
	f.state = scwStateStopped

	if f.createStatus != 0 {
		scwFail(w, f.createStatus, "bad gateway")

		return
	}

	fmt.Fprintf(w, `{"server":%s}`, f.serverJSON())
}

func (f *fakeScaleway) serverJSON() string {
	return fmt.Sprintf(`{"id":%q,"state":%q,"public_ips":[{"address":%q,"family":"inet"}],"volumes":{"0":{"id":"vol-root","volume_type":"l_ssd"},"1":{"id":"vol-data","volume_type":%q}}}`,
		scwTestServer, f.state, joinNewNode, f.dataType)
}

func (f *fakeScaleway) server(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case r.Method == http.MethodPatch && rest == "/user_data/cloud-init":
		data, _ := io.ReadAll(r.Body)
		f.userData, f.contentType = string(data), r.Header.Get("Content-Type")
		f.writes = append(f.writes, "user data")

		if f.userDataFails {
			scwFail(w, http.StatusBadRequest, "user data refused")
		}
	case r.Method == http.MethodPost && rest == "/action":
		var body struct {
			Action string `json:"action"`
		}

		_ = json.NewDecoder(r.Body).Decode(&body)
		f.writes = append(f.writes, body.Action)

		f.state = map[string]string{"poweron": "starting", "terminate": "gone"}[body.Action]
		fmt.Fprint(w, `{"task":{"id":"task-1","status":"pending"}}`)
	case r.Method == http.MethodDelete && rest == "":
		f.writes = append(f.writes, "delete server")
		f.state = "gone"
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && rest == "":
		f.poll()
		fmt.Fprintf(w, `{"server":%s}`, f.serverJSON())
	default:
		scwFail(w, http.StatusNotFound, rest)
	}
}

// poll moves the server on: starting answers once, then running (or locked).
func (f *fakeScaleway) poll() {
	switch {
	case f.state == "starting" && f.locked:
		f.state = scwStateLocked
	case f.state == "starting":
		f.state = "booting"
	case f.state == "booting":
		f.state = scwStateRunning

		if f.onRunning != nil {
			f.onRunning()
			f.onRunning = nil
		}
	}
}

func TestScalewayTest(t *testing.T) {
	cases := []struct {
		name string
		cred string
		fake *fakeScaleway
		want scwTestResult
		fail string
	}{
		{name: "may create", cred: scwTestCred, fake: &fakeScaleway{}, want: scwTestResult{CanCreate: true, Checked: true}},
		{name: "may not create", cred: scwTestCred, fake: &fakeScaleway{noCreateRight: true}, want: scwTestResult{Checked: true}},
		{name: "a wrong secret", cred: `{"secretKey":"wrong","project":"` + scwTestProject + `"}`, fake: &fakeScaleway{}, fail: "check its secret key"},
		{name: "no key", cred: "", fake: &fakeScaleway{}, fail: "API key first"},
		{name: "no project", cred: `{"secretKey":"` + scwTestSecret + `"}`, fake: &fakeScaleway{}, fail: "project id"},
		{name: "a zone that is not one", cred: `{"secretKey":"x","project":"` + scwTestProject + `","zone":"../x"}`, fake: &fakeScaleway{}, fail: "unknown Scaleway zone"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeScaleway(t, c.fake)

			out, err := ScalewayTest(c.cred)
			if c.fail != "" {
				if err == nil || !strings.Contains(err.Error(), c.fail) || strings.Contains(err.Error(), "wrong") {
					t.Fatalf("err = %v, want %q in it", err, c.fail)
				}

				return
			}

			got := decodeJSON[scwTestResult](t, out, err)
			if got.Project != scwTestProject || got.Zone != "fr-par-1" || got.Account != "SCWSAMPLEACCESSKEY00" ||
				got.CanCreate != c.want.CanCreate || got.Checked != c.want.Checked || strings.Contains(out, scwTestSecret) {
				t.Errorf("got %s, want %+v", out, c.want)
			}
		})
	}
}

func TestScalewayOptions(t *testing.T) {
	cases := []struct {
		name        string
		noTalos     bool
		wantImages  []string
		wantWarning bool
	}{
		{name: "Talos images first", wantImages: []string{"img-own", "img-talos", "img-plain"}},
		{name: "no Talos image: a warning", noTalos: true, wantImages: []string{"img-plain", "img-own"}, wantWarning: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeScaleway(t, &fakeScaleway{noTalosImage: c.noTalos})

			out, err := ScalewayOptions("", "", scwTestCred)
			got := decodeJSON[scwOptions](t, out, err)

			var types []string
			for _, ty := range got.Types {
				types = append(types, ty.Name)
			}

			if !slices.Equal(types, []string{"DEV1-S", "DEV1-M", "COPARM1-2C-8G"}) {
				t.Errorf("types = %v", types)
			}

			if s := got.Types[0]; s.Hourly != "0.0099" || s.Currency != "EUR" || s.LocalDiskGB != 20 || s.RAMGB != 2 || s.BlockOnly || !got.Types[2].BlockOnly {
				t.Errorf("types = %+v", got.Types)
			}

			var images []string
			for _, i := range got.Images {
				images = append(images, i.ID)
			}

			if !slices.Equal(images, c.wantImages) || got.Images[0].Talos == c.noTalos || got.Images[0].SizeGB != 10 {
				t.Errorf("images = %+v", got.Images)
			}

			warned := slices.ContainsFunc(got.Warnings, func(w string) bool { return strings.Contains(w, "no Talos image") })
			if warned != c.wantWarning || len(got.SecurityGroups) != 1 || !got.SecurityGroups[0].Default || len(got.Zones) < 9 {
				t.Errorf("warnings = %v, groups = %+v, zones = %v", got.Warnings, got.SecurityGroups, got.Zones)
			}
		})
	}
}

func TestScalewayPolicyNamesTheProject(t *testing.T) {
	out := ScalewayPolicy(scwTestProject)
	if !strings.Contains(out, scwTestProject) || !strings.Contains(out, "InstancesFullAccess") || !strings.Contains(out, "InstancesReadOnly") {
		t.Errorf("policy = %s", out)
	}

	if !strings.Contains(ScalewayPolicy(""), "PROJECT_ID") {
		t.Error("an empty project prints no placeholder")
	}
}

func TestScalewayDataVolume(t *testing.T) {
	root := scwAPIImage{}
	root.RootVolume.Size, root.RootVolume.VolumeType = 10*scwGB, scwVolumeLocal

	typ := func(minGB, maxGB int64) scwAPIType {
		var t scwAPIType
		t.VolumesConstraint.MinSize, t.VolumesConstraint.MaxSize = minGB*scwGB, maxGB*scwGB

		return t
	}

	cases := []struct {
		name      string
		typ       scwAPIType
		image     scwAPIImage
		wantType  string
		wantGB    int64
		wantMatch string
	}{
		{name: "room beside a local root: the guide's /dev/vdb", typ: typ(0, 40), image: root, wantType: scwVolumeLocal, wantGB: 25, wantMatch: "/dev/vdb"},
		{name: "a local minimum is filled", typ: typ(50, 80), image: root, wantType: scwVolumeLocal, wantGB: 40, wantMatch: "/dev/vdb"},
		{name: "no room: Block Storage", typ: typ(0, 20), image: root, wantType: scwVolumeBlock, wantGB: 25, wantMatch: "!system_disk"},
		{name: "a block-only type", typ: typ(0, 0), image: root, wantType: scwVolumeBlock, wantGB: 25, wantMatch: "!system_disk"},
		{name: "a block root: not the boot disk", typ: typ(0, 40), image: scwAPIImage{}, wantType: scwVolumeLocal, wantGB: 25, wantMatch: "!system_disk"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := &scwCreate{spec: scwCreateSpec{EphemeralGB: 25}}

			match := h.pickDataVolume(c.typ, c.image)
			if h.dataType != c.wantType || h.dataSize != c.wantGB*scwGB || !strings.Contains(match, c.wantMatch) {
				t.Errorf("got %s %d %q", h.dataType, h.dataSize, match)
			}
		})
	}
}

func scwTestRun(cfg, role string, rec *joinRecorder) *scwCreate {
	return &scwCreate{
		join: &nodeJoin{
			config: cfg, context: "fake", listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/vda"},
		},
		spec: scwCreateSpec{
			Zone: "fr-par-1", Type: "DEV1-M", ImageID: "img-talos", Hostname: "node-c", Role: role,
			TemplateNode: joinTemplateNode, InstallDisk: "/dev/vda",
		},
		credJSON: scwTestCred, poll: joinPollInterval,
	}
}

func TestScalewayCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeScaleway{onRunning: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeScaleway(t, f)

	rec := newJoinRecorder()
	scwTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{cloudPhaseCreating, cloudPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if !slices.Equal(f.writes, []string{"ip", "server", "user data", "poweron"}) {
		t.Errorf("writes = %v", f.writes)
	}

	b := f.serverBody
	volume, _ := b["volumes"].(map[string]any)["1"].(map[string]any)

	if b["name"] != "node-c" || b["commercial_type"] != "DEV1-M" || b["image"] != "img-talos" || b["project"] != scwTestProject ||
		b["routed_ip_enabled"] != true || fmt.Sprint(b["public_ips"]) != "["+scwTestIP+"]" || b["security_group"] != nil ||
		volume["volume_type"] != scwVolumeLocal || volume["size"] != float64(25*scwGB) || !strings.Contains(fmt.Sprint(b["tags"]), "ichor-run=") {
		t.Errorf("POST /servers = %+v", b)
	}

	if f.contentType != "text/plain" || !strings.Contains(f.userData, "node-c") || !strings.Contains(f.userData, "kind: VolumeConfig") ||
		!strings.Contains(f.userData, "name: EPHEMERAL") || !strings.Contains(f.userData, "disk.dev_path == '/dev/vdb'") {
		t.Errorf("user data (%s) = %s", f.contentType, f.userData)
	}

	for _, e := range events {
		if strings.Contains(e.Message, scwTestSecret) {
			t.Errorf("a progress line carries the secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", "scaleway-instance-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "instance="+scwTestServer) ||
		strings.Contains(entries[0].Params, scwTestSecret) || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestScalewayCreateFailures(t *testing.T) {
	cases := []struct {
		name        string
		fake        *fakeScaleway
		mutate      func(*scwCreate)
		wantInError string
		wantWrites  []string
	}{
		{
			name: "a type of another architecture: refused before any write", fake: &fakeScaleway{},
			mutate:      func(h *scwCreate) { h.spec.Type = "COPARM1-2C-8G" },
			wantInError: "pick a matching type",
		},
		{
			name: "an image of another project: refused before any write", fake: &fakeScaleway{},
			mutate:      func(h *scwCreate) { h.spec.ImageID = "img-other" },
			wantInError: "another project",
		},
		{
			name: "a type the zone does not sell: refused before any write", fake: &fakeScaleway{},
			mutate:      func(h *scwCreate) { h.spec.Type = "VC1S" },
			wantInError: "cannot be ordered",
		},
		{
			name: "an EPHEMERAL under 2 GB: refused before any write", fake: &fakeScaleway{},
			mutate:      func(h *scwCreate) { h.spec.EphemeralGB = 1 },
			wantInError: "EPHEMERAL volume takes",
		},
		{
			name: "no right to create: refused before any write", fake: &fakeScaleway{noCreateRight: true},
			wantInError: "printed policy",
		},
		{
			name: "a refused server: its IP is deleted", fake: &fakeScaleway{createStatus: http.StatusBadRequest},
			wantInError: "Flexible IP " + scwTestIP + " was deleted", wantWrites: []string{"ip", "server", "delete ip"},
		},
		{
			name: "a server made without an answer: found by tag, deleted with its volumes and IP", fake: &fakeScaleway{createStatus: http.StatusBadGateway},
			wantInError: "Instance " + scwTestServer + " was deleted",
			wantWrites:  []string{"ip", "server", "delete server", "delete vol-root", "delete vol-data", "delete ip"},
		},
		{
			name: "refused user data: the stopped server is deleted with its volumes", fake: &fakeScaleway{userDataFails: true},
			wantInError: "user data refused",
			wantWrites:  []string{"ip", "server", "user data", "delete server", "delete vol-root", "delete vol-data", "delete ip"},
		},
		{
			name: "a locked server: the IP goes, the server is named", fake: &fakeScaleway{locked: true},
			wantInError: "still there in fr-par-1: Instance " + scwTestServer,
			wantWrites:  []string{"ip", "server", "user data", "poweron", "delete ip"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeScaleway(t, c.fake)

			rec := newJoinRecorder()

			h := scwTestRun(cfg, joinRoleWorker, rec)
			if c.mutate != nil {
				c.mutate(h)
			}

			h.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) || strings.Contains(msg, scwTestSecret) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if !slices.Equal(c.fake.writes, c.wantWrites) {
				t.Errorf("writes = %v, want %v", c.fake.writes, c.wantWrites)
			}
		})
	}
}

func TestScalewayCreateStopDeletes(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	started := make(chan struct{})
	f := &fakeScaleway{onRunning: func() { close(started) }}
	startFakeScaleway(t, f)

	rec := newJoinRecorder()
	run := scwTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	<-started
	run.Cancel()

	// terminate takes the local volumes: only the IP is left to delete.
	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Instance "+scwTestServer+" was deleted") ||
		!slices.Equal(f.writes, []string{"ip", "server", "user data", "poweron", "terminate", "delete ip"}) {
		t.Errorf("error = %q, writes = %v", msg, f.writes)
	}
}

func TestScalewayUserDataTooLarge(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeScaleway{}
	startFakeScaleway(t, f)

	edit, _ := json.Marshal([]configEdit{{Doc: 0, Path: []string{"machine", "sysctls", "net.core.big"}, Op: configOpPut, Type: "string", Value: strings.Repeat("x", scwUserDataLimit)}})

	rec := newJoinRecorder()
	h := scwTestRun(cfg, joinRoleWorker, rec)
	h.spec.ExtraEdits = string(edit)
	h.join.editsJSON = string(edit)
	h.start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "over the 32 KiB") || len(f.writes) != 0 {
		t.Errorf("error = %q, writes = %v", msg, f.writes)
	}
}

func TestScalewayDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := ScalewayOptions(cfg, "", "")
	if o := decodeJSON[scwOptions](t, out, err); len(o.Types) == 0 || len(o.Images) == 0 || !o.Images[0].Talos {
		t.Errorf("demo options = %+v", o)
	}

	rec := newJoinRecorder()
	StartScalewayCreate(cfg, "", scwTestCred, `{"zone":"fr-par-1","type":"DEV1-S","imageId":"demo-talos","hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}
