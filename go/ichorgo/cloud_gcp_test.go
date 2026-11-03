package ichorgo

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// gcpLeaks is what no message may carry: the private key, the token, the key's email and id,
// the project number.
func gcpLeaks(s string) bool {
	for _, secret := range []string{gcpTestToken, gcpTestEmail, gcpTestKeyID, gcpTestNumber, "PRIVATE KEY", "ya29."} {
		if strings.Contains(s, secret) {
			return true
		}
	}

	return gcpTestKeyPEM != "" && strings.Contains(s, strings.Split(gcpTestKeyPEM, "\n")[1])
}

func TestGCPTokenAndHeaders(t *testing.T) {
	f := &fakeGCE{}
	startFakeGCE(t, f)

	if _, err := GCPTest(gcpTestCred(t)); err != nil {
		t.Fatal(err)
	}

	if _, err := GCPOptions("", "", gcpTestCred(t)); err != nil {
		t.Fatal(err)
	}

	// One token per facade call, from a JWT with the cloud-platform scope; every call carries
	// it and the key's project as the quota project.
	if f.tokens != 2 || f.badScope != 0 || f.unauthorized != 0 || f.noQuotaHeader != 0 || len(f.calls) < 6 {
		t.Errorf("tokens = %d, bad scope = %d, unauthorized = %d, no quota header = %d, calls = %v", f.tokens, f.badScope, f.unauthorized, f.noQuotaHeader, f.calls)
	}
}

func TestGCPTest(t *testing.T) {
	cases := []struct {
		name        string
		cred        func(t *testing.T) string
		fake        *fakeGCE
		want        gcpTestResult
		wantMissing []string
		fail        string
	}{
		{name: "may create", cred: gcpTestCred, fake: &fakeGCE{}, want: gcpTestResult{CanCreate: true, Checked: true}},
		{
			name: "no create right", cred: gcpTestCred, fake: &fakeGCE{noCreateRight: true}, want: gcpTestResult{Checked: true},
			wantMissing: []string{"compute.instances.create", "compute.disks.create"},
		},
		{name: "Resource Manager disabled: cannot tell", cred: gcpTestCred, fake: &fakeGCE{crmDisabled: true}, want: gcpTestResult{}},
		{
			name: "Compute Engine disabled", cred: gcpTestCred, fake: &fakeGCE{apiDisabled: true},
			fail: "the Compute Engine API is not enabled in project example-project: enable it at " +
				"https://console.developers.google.com/apis/api/compute.googleapis.com/overview?project=example-project",
		},
		{name: "no credential", cred: func(*testing.T) string { return "" }, fake: &fakeGCE{}, fail: "add a service account key first"},
		{
			name: "a user credential", fake: &fakeGCE{},
			cred: func(*testing.T) string {
				return `{"serviceAccountJson":"{\"type\":\"authorized_user\",\"refresh_token\":\"x\"}"}`
			},
			fail: "not a service account key",
		},
		{
			name: "a zone that is not one", fake: &fakeGCE{},
			cred: func(t *testing.T) string { return strings.Replace(gcpTestCred(t), gcpTestZone, "europe/../x", 1) },
			fail: "is not a Compute Engine zone",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeGCE(t, c.fake)

			out, err := GCPTest(c.cred(t))
			if c.fail != "" {
				if err == nil || !strings.Contains(err.Error(), c.fail) || gcpLeaks(err.Error()) {
					t.Fatalf("err = %v, want %q in it", err, c.fail)
				}

				return
			}

			got := decodeJSON[gcpTestResult](t, out, err)
			if got.Account != gcpTestEmail || got.Project != gcpTestProject || got.CanCreate != c.want.CanCreate || got.Checked != c.want.Checked ||
				!slices.Equal(got.Missing, append([]string{}, c.wantMissing...)) {
				t.Errorf("got %+v, want %+v missing %v", got, c.want, c.wantMissing)
			}

			if !strings.Contains(out, `"missing":[`) {
				t.Errorf("missing is not a list: %s", out)
			}

			if len(c.fake.inserted) != 0 || len(c.fake.deleted) != 0 {
				t.Errorf("a test wrote: %v %v", c.fake.inserted, c.fake.deleted)
			}
		})
	}
}

func TestGCPPermissionOf(t *testing.T) {
	base := gcpComputeEndpoint + "/compute/v1/projects/p"
	cases := []struct{ method, url, want string }{
		{http.MethodGet, base, "compute.projects.get"},
		{http.MethodGet, base + "/zones", "compute.zones.list"},
		{http.MethodGet, base + "/zones/z/machineTypes?maxResults=500", "compute.machineTypes.list"},
		{http.MethodGet, base + "/zones/z/machineTypes/e2-medium", "compute.machineTypes.get"},
		{http.MethodPost, base + "/zones/z/instances?requestId=x", "compute.instances.create"},
		{http.MethodGet, base + "/zones/z/instances/node-c", "compute.instances.get"},
		{http.MethodDelete, base + "/zones/z/instances/node-c", "compute.instances.delete"},
		{http.MethodGet, base + "/zones/z/instances/node-c/serialPort?port=1", "compute.instances.getSerialPortOutput"},
		{http.MethodPost, base + "/zones/z/operations/op-1/wait", "compute.zoneOperations.get"},
		{http.MethodGet, base + "/global/networks", "compute.networks.list"},
		{http.MethodGet, base + "/global/images/talos", "compute.images.get"},
		{http.MethodGet, base + "/aggregated/subnetworks", "compute.subnetworks.list"},
		{http.MethodGet, base + "/regions/r/subnetworks/s", "compute.subnetworks.get"},
		{http.MethodGet, base + "/regions/r/routers", "compute.routers.list"},
		{http.MethodPost, gcpCRMEndpoint + "/v1/projects/p:testIamPermissions", ""},
	}

	for _, c := range cases {
		if got := gcpPermissionOf(c.method, c.url); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.method, c.url, got, c.want)
		}
	}
}

func TestGCPOptions(t *testing.T) {
	cases := []struct {
		name         string
		cred         func(t *testing.T) string
		fake         *fakeGCE
		wantWarnings []string
	}{
		{
			name: "the credential's zone, a network without Cloud NAT", cred: gcpTestCred, fake: &fakeGCE{},
			wantWarnings: []string{"network default has no Cloud NAT in europe-west1: without a public address, the node needs a route out to reach the Image Factory or Omni"},
		},
		{
			name: "no zone: the first that is up", fake: &fakeGCE{nat: true},
			cred: func(t *testing.T) string {
				return strings.Replace(gcpTestCred(t), `,"zone":"`+gcpTestZone+`"`, "", 1)
			},
			wantWarnings: []string{"no zone given: listing europe-west1-b"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeGCE(t, c.fake)

			out, err := GCPOptions(cfg, "fake", c.cred(t))
			o := decodeJSON[gcpOptions](t, out, err)

			var types []string
			for _, m := range o.MachineTypes {
				types = append(types, m.Name+"/"+m.Arch)
			}

			// Both pages, the deprecated type dropped, by vCPUs then memory, x86 without an architecture.
			if want := []string{"e2-micro/amd64", "e2-medium/amd64", "t2a-standard-2/arm64"}; !slices.Equal(types, want) || o.MachineTypes[1].MemoryGB != 4 {
				t.Errorf("machine types = %v, want %v", types, want)
			}

			if o.Project != gcpTestProject || o.Zone != gcpTestZone || len(o.Projects) != 2 || o.Projects[0] != (gcpProject{ID: gcpTestProject, Name: "Example project"}) {
				t.Errorf("project/zone/projects = %+v", o)
			}

			if want := []gcpZone{{"europe-west1-b", "europe-west1"}, {"europe-west1-c", "europe-west1"}, {"us-central1-a", "us-central1"}}; !slices.Equal(o.Zones, want) {
				t.Errorf("zones = %+v (up only)", o.Zones)
			}

			if len(o.Networks) != 2 || o.Networks[0].Name != "default" || !o.Networks[0].AutoSubnets || len(o.Networks[0].Subnets) != 1 ||
				o.Networks[0].Subnets[0] != (gcpSubnet{Name: "nodes", SelfLink: gcpTestSubnet, Region: gcpTestRegion, CIDR: "10.132.0.0/20"}) ||
				len(o.Networks[1].Subnets) != 0 {
				t.Errorf("networks = %+v (the region's subnets only)", o.Networks)
			}

			want := []gcpImage{
				{Name: "talos-v1-14-2-arm64", SelfLink: gcpTestLink + "/global/images/talos-v1-14-2-arm64", Arch: bootArchARM64, SizeGB: 10,
					TalosVersion: "v1.14.2", Schematic: "0123456789abcdef", UEFI: true},
				{Name: "talos-by-hand", SelfLink: gcpTestLink + "/global/images/talos-by-hand", Arch: bootArchAMD64, SizeGB: 10},
			}
			if !slices.Equal(o.Images, want) {
				t.Errorf("images = %+v, want %+v (labelled first, ready only)", o.Images, want)
			}

			if !slices.Equal(o.Warnings, c.wantWarnings) {
				t.Errorf("warnings = %q, want %q", o.Warnings, c.wantWarnings)
			}

			for _, key := range []string{`"memoryGB":4,`, `"autoSubnets":true`, `"uefi":true`, `"sizeGB":10`, `"region":"europe-west1"`} {
				if !strings.Contains(out, key) {
					t.Errorf("%s not in %s", key, out)
				}
			}
		})
	}
}

func TestGCPOptionsWarnsWithoutTheVersion(t *testing.T) {
	startFakeGCE(t, &fakeGCE{nat: true, crmDisabled: true})

	c, err := newGCPClient(gcpTestCred(t))
	if err != nil {
		t.Fatal(err)
	}

	o, err := c.options(context.Background(), "v1.15.0")
	if err != nil {
		t.Fatal(err)
	}

	// The project list refused: a warning, and the key's own project.
	if !slices.Contains(o.Warnings, "no image labelled with Talos v1.15.0: pick one of your own or make one") || len(o.Projects) != 1 ||
		!slices.ContainsFunc(o.Warnings, func(w string) bool { return strings.Contains(w, "Cloud Resource Manager API is not enabled") }) {
		t.Errorf("warnings = %q, projects = %+v", o.Warnings, o.Projects)
	}

	if o, err := c.options(context.Background(), "v1.14.2"); err != nil || slices.ContainsFunc(o.Warnings, func(w string) bool { return strings.Contains(w, "labelled") }) {
		t.Errorf("a labelled image of the version still warns: %q, %v", o.Warnings, err)
	}
}

func TestGCPPrice(t *testing.T) {
	f := &fakeGCE{}
	startFakeGCE(t, f)

	// e2-medium is billed one shared vCPU and 4 GiB: 0.022 + 4 × 0.003, from the region's
	// on-demand E2 SKUs over two pages (not Spot, not another region, not N2D).
	out, err := GCPPrice(gcpTestCred(t), gcpTestZone, "e2-medium")
	if err != nil || out != `{"hourly":"0.0340","currency":"USD"}` {
		t.Errorf("price = %s, %v", out, err)
	}

	if out, err := GCPPrice(gcpTestCred(t), gcpTestZone, "t2a-standard-2"); err != nil || out != `{"hourly":""}` {
		t.Errorf("a series without SKUs = %s, %v", out, err)
	}

	f.billingDisabled = true

	if out, err := GCPPrice(gcpTestCred(t), gcpTestZone, "e2-medium"); err != nil || out != `{"hourly":""}` {
		t.Errorf("Billing API disabled = %s, %v", out, err)
	}

	for _, bad := range [][2]string{{"europe-west1", "e2-medium"}, {gcpTestZone, "e2-medium/../x"}} {
		if _, err := GCPPrice(gcpTestCred(t), bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestGCPRoleDefinition(t *testing.T) {
	role := GCPRoleDefinition(gcpTestProject)

	for _, want := range []string{
		"title: Ichor node creator\n", "stage: GA\n", "includedPermissions:\n",
		"# gcloud iam roles create ichorNodeCreator --project example-project --file",
		"--role projects/example-project/roles/ichorNodeCreator", "roles/compute.instanceAdmin.v1",
	} {
		if !strings.Contains(role, want) {
			t.Errorf("%q not in the role:\n%s", want, role)
		}
	}

	for _, p := range append(gcpCreatePermissions(true), gcpOptionsPermissions()...) {
		if !strings.Contains(role, "\n- "+p+"\n") {
			t.Errorf("%s not in the role", p)
		}
	}

	if strings.Contains(role, "*") || strings.Count(role, "\n- ") != len(gcpRolePermissions()) {
		t.Errorf("the role is not exactly the list:\n%s", role)
	}

	if !strings.Contains(GCPRoleDefinition(""), "--project PROJECT_ID") {
		t.Error("no placeholder for an empty project")
	}
}

func TestGCPErrorsCarryNoSecret(t *testing.T) {
	c, err := newGCPClient(gcpTestCred(t))
	if err != nil {
		t.Fatal(err)
	}

	c.token, c.number = gcpTestToken, gcpTestNumber

	got := c.scrub("key " + gcpTestKeyPEM + " token " + gcpTestToken + " other ya29.another-token, " + gcpTestEmail + " id " + gcpTestKeyID +
		", projects/" + gcpTestNumber + " and project 987654321098, other@sample.iam.gserviceaccount.com")
	if gcpLeaks(got) || strings.Contains(got, "987654321098") || strings.Contains(got, "other@") {
		t.Errorf("scrubbed = %q", got)
	}

	if _, err := newGCPClient(`{"serviceAccountJson":"` + strings.ReplaceAll(gcpTestKeyPEM, "\n", `\n`)); err == nil || gcpLeaks(err.Error()) {
		t.Errorf("broken JSON: %v", err)
	}

	refusal := &gcpError{action: "compute.instances.create", reason: gcpReasonForbidden, status: http.StatusForbidden}
	if refusal.Error() != "Google Cloud refused compute.instances.create: grant the printed role to the service account" {
		t.Errorf("refusal = %q", refusal.Error())
	}

	quota := &gcpError{reason: "QUOTA_EXCEEDED", message: "CPUS", zone: gcpTestZone}
	if !strings.HasPrefix(quota.Error(), "quota exceeded in europe-west1-b: pick another zone or machine type") {
		t.Errorf("quota = %q", quota.Error())
	}
}

func gcpTestRun(t *testing.T, cfg, contextName, role string, rec *joinRecorder) *gcpCreate {
	t.Helper()

	return &gcpCreate{
		join: &nodeJoin{
			config: cfg, context: contextName, listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/sda"},
		},
		spec: gcpCreateSpec{
			Zone: gcpTestZone, MachineType: "e2-medium", Image: "talos-by-hand", Subnetwork: gcpTestSubnet, AssociatePublicIP: true,
			Hostname: "node-c", Role: role, TemplateNode: joinTemplateNode, InstallDisk: "/dev/sda",
		},
		credJSON: gcpTestCred(t), poll: joinPollInterval,
	}
}

var gcpUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestGCPCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeGCE{onRunning: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeGCE(t, f)

	rec := newJoinRecorder()
	gcpTestRun(t, cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{cloudPhaseCreating, cloudPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if len(f.inserted) != 1 || len(f.requestIDs) != 1 || !gcpUUIDPattern.MatchString(f.requestIDs[0]) {
		t.Fatalf("inserts = %d, request ids = %v", len(f.inserted), f.requestIDs)
	}

	body := f.inserted[0]
	labels, _ := body["labels"].(map[string]any)

	if run, _ := labels[gcpLabelRun].(string); len(run) != 16 || labels[gcpLabelCluster] == "" || labels[gcpLabelCluster] == "fake" {
		t.Errorf("labels = %v", labels)
	}

	checks := map[string][2]any{
		"name":         {body["name"], "node-c"},
		"machine type": {body["machineType"], "zones/europe-west1-b/machineTypes/e2-medium"},
		"boot":         {azureAt(body, "disks", 0, "boot"), true},
		"auto delete":  {azureAt(body, "disks", 0, "autoDelete"), true},
		"image":        {azureAt(body, "disks", 0, "initializeParams", "sourceImage"), "projects/example-project/global/images/talos-by-hand"},
		"disk size":    {azureAt(body, "disks", 0, "initializeParams", "diskSizeGb"), "20"},
		"disk type":    {azureAt(body, "disks", 0, "initializeParams", "diskType"), "zones/europe-west1-b/diskTypes/pd-balanced"},
		"subnet":       {azureAt(body, "networkInterfaces", 0, "subnetwork"), "regions/europe-west1/subnetworks/nodes"},
		"nat":          {azureAt(body, "networkInterfaces", 0, "accessConfigs", 0, "type"), "ONE_TO_ONE_NAT"},
		"user-data":    {azureAt(body, "metadata", "items", 0, "key"), "user-data"},
		"tag":          {azureAt(body, "tags", "items", 0), gcpNetworkTag},
	}

	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %v, want %v", name, c[0], c[1])
		}
	}

	// Plain text, no base64: Talos's gcp platform reads it as is.
	if data, _ := azureAt(body, "metadata", "items", 0, "value").(string); !strings.HasPrefix(data, "version: v1alpha1") || !strings.Contains(data, "node-c") {
		t.Errorf("user data = %.80q", data)
	}

	if len(f.deleted) != 0 || len(talos.applies) != 0 {
		t.Errorf("deleted = %v, applies = %+v after a success", f.deleted, talos.applies)
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

	entries := readAudit(t, "fake", "gcp-instance-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "instance=projects/…/zones/europe-west1-b/instances/node-c") ||
		!strings.Contains(entries[0].Params, "machineType=e2-medium zone=europe-west1-b") || gcpLeaks(entries[0].Params) ||
		strings.Contains(entries[0].Params, "v1alpha1") || strings.Contains(entries[0].Params, gcpTestProject) {
		t.Errorf("audit = %+v", entries)
	}
}

// On Omni the run hands over to the Omni watch once the VM runs (see TestAWSCreateWaitsForOmni).
func TestGCPCreateWaitsForOmni(t *testing.T) {
	rec := newJoinRecorder()
	h := gcpTestRun(t, "", "", "", rec)

	calls := 0
	h.omniHostnames = func(context.Context, *session) ([]string, error) {
		calls++
		if calls < 3 {
			return []string{"other"}, nil
		}

		return []string{"other", "node-c"}, nil
	}

	run := &cloudCreateRun{provider: h, join: h.join, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := run.waitOmni(ctx, nil); err != nil || calls != 3 {
		t.Errorf("waitOmni = %v after %d calls", err, calls)
	}
}

// gcpBigEdits makes the user data larger than Compute Engine takes.
var gcpBigEdits = `[{"doc":0,"path":["machine","sysctls","net.ichor.padding"],"op":"put","type":"string","value":"` + strings.Repeat("x", gcpUserDataLimit) + `"}]`

func TestGCPCreateFailures(t *testing.T) {
	const deleted = "Instance projects/…/zones/europe-west1-b/instances/node-c was deleted"

	cases := []struct {
		name        string
		fake        *fakeGCE
		mutate      func(*gcpCreate)
		wantInError string
		wantInserts int
		wantDeleted []string
	}{
		{
			name: "no create right: refused before any write", fake: &fakeGCE{noCreateRight: true},
			wantInError: "may not use compute.instances.create, compute.disks.create in project example-project: grant the printed role",
		},
		{name: "permissions cannot be checked: refused before any write", fake: &fakeGCE{crmDisabled: true}, wantInError: "cannot be checked, so nothing is created"},
		{name: "Compute Engine disabled: refused before any write", fake: &fakeGCE{apiDisabled: true}, wantInError: "the Compute Engine API is not enabled in project example-project"},
		{name: "an arm64 image on an amd64 type: refused before any write", fake: &fakeGCE{archMismatch: true}, wantInError: "this image is arm64, the machine type is amd64"},
		{
			name: "an Arm type and an image without UEFI: refused before any write", fake: &fakeGCE{archMismatch: true},
			mutate: func(h *gcpCreate) { h.spec.MachineType = "t2a-standard-2" }, wantInError: "UEFI_COMPATIBLE",
		},
		{
			name: "a subnet of another region: refused before any write", fake: &fakeGCE{},
			mutate: func(h *gcpCreate) { h.spec.Subnetwork = "regions/us-central1/subnetworks/far" }, wantInError: "subnet far is in us-central1, the zone europe-west1-b in europe-west1",
		},
		{
			name: "a boot disk under 10 GB: refused before any write", fake: &fakeGCE{},
			mutate: func(h *gcpCreate) { h.spec.DiskGB = 8 }, wantInError: "Compute Engine takes 10 to 65536",
		},
		{name: "an instance of that name: refused before any write", fake: &fakeGCE{existing: true}, wantInError: "an instance named node-c exists in europe-west1-b"},
		{
			name: "user data over 256 KiB: refused before any write", fake: &fakeGCE{},
			mutate: func(h *gcpCreate) { h.spec.ExtraEdits = gcpBigEdits }, wantInError: "over the 256 KiB Compute Engine takes",
		},
		{
			name: "an upper-case host name: refused before any write", fake: &fakeGCE{},
			mutate: func(h *gcpCreate) { h.spec.Hostname = "Node-C" }, wantInError: "is not a host name Compute Engine takes",
		},
		{
			name: "a refused insert: nothing to delete", fake: &fakeGCE{createStatus: http.StatusBadRequest},
			wantInError: "Google Cloud compute.instances.create: something broke", wantInserts: 1,
		},
		{
			name: "an insert without a clear answer: sent again with its requestId, found by name and label, and deleted",
			fake: &fakeGCE{createStatus: http.StatusInternalServerError}, wantInError: deleted, wantInserts: gcpInsertTries, wantDeleted: []string{"node-c"},
		},
		{
			name: "the instance stops before it runs: an error with the serial console's end, and it is deleted", fake: &fakeGCE{endState: "TERMINATED"},
			wantInError: "instance node-c is terminated: instance node-c stopped by the guest; its serial console ends with: ", wantInserts: 1, wantDeleted: []string{"node-c"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeGCE(t, c.fake)

			rec := newJoinRecorder()

			h := gcpTestRun(t, cfg, "fake", joinRoleWorker, rec)
			if c.mutate != nil {
				c.mutate(h)
			}

			h.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) || gcpLeaks(msg) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if c.fake.endState != "" && !strings.Contains(msg, "boot failed") {
				t.Errorf("no serial console tail in %q", msg)
			}

			if len(c.fake.inserted) != c.wantInserts {
				t.Errorf("inserts = %d, want %d", len(c.fake.inserted), c.wantInserts)
			}

			if len(c.fake.requestIDs) > 0 && len(slices.Compact(slices.Clone(c.fake.requestIDs))) != 1 {
				t.Errorf("request ids = %v, want one for every try", c.fake.requestIDs)
			}

			if !slices.Equal(c.fake.deleted, c.wantDeleted) {
				t.Errorf("deleted = %v, want %v", c.fake.deleted, c.wantDeleted)
			}

			if _, left := c.fake.instances["node-c"]; left && c.wantInserts > 0 && c.fake.createStatus != http.StatusBadRequest {
				t.Error("the instance is left behind")
			}

			entries := readAudit(t, "fake", "gcp-instance-create")
			if len(entries) != 1 || entries[0].Outcome == auditOK || gcpLeaks(entries[0].Params) {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

// An insert whose answer is lost is sent again with the same requestId: Google makes it a no-op,
// and the run goes on with the one instance.
func TestGCPCreateRetriesWithTheSameRequestID(t *testing.T) {
	f := &fakeGCE{firstInsertLost: true}
	startFakeGCE(t, f)

	h := gcpTestRun(t, "", "fake", joinRoleWorker, newJoinRecorder())

	client, err := newGCPClient(h.credJSON)
	if err != nil {
		t.Fatal(err)
	}

	h.client = client
	if err := h.checkSpec(); err != nil {
		t.Fatal(err)
	}

	h.runID, h.requestID = "0123456789abcdef", "00000000-0000-4000-8000-000000000001"

	if err := h.create(context.Background(), "version: v1alpha1"); err != nil || !h.created || h.unclear || h.operation == "" {
		t.Fatalf("create = %v, created %v, unclear %v", err, h.created, h.unclear)
	}

	if len(f.requestIDs) != 2 || f.requestIDs[0] != h.requestID || f.requestIDs[1] != h.requestID || len(f.instances) != 1 {
		t.Errorf("request ids = %v, instances = %v", f.requestIDs, f.instances)
	}
}

func TestGCPCreateStopDeletes(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	started := make(chan struct{})
	f := &fakeGCE{onRunning: func() { close(started) }}
	startFakeGCE(t, f)

	rec := newJoinRecorder()
	run := gcpTestRun(t, cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the instance was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Instance projects/…/zones/europe-west1-b/instances/node-c was deleted") ||
		!slices.Equal(f.deleted, []string{"node-c"}) || len(f.instances) != 0 {
		t.Errorf("error = %q, deleted = %v, left = %v", msg, f.deleted, f.instances)
	}
}

// A delete Google accepted but did not finish is reported as unconfirmed, never as done: the
// instance may still be billed.
func TestGCPCreateStopUnfinishedDelete(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	before := gcpDeleteWait
	gcpDeleteWait = 300 * time.Millisecond

	t.Cleanup(func() { gcpDeleteWait = before })

	started := make(chan struct{})
	f := &fakeGCE{slowDelete: true, onRunning: func() { close(started) }}
	startFakeGCE(t, f)

	rec := newJoinRecorder()
	run := gcpTestRun(t, cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the instance was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "instances/node-c could not be deleted (the delete of instance node-c in europe-west1-b was accepted but had not finished") ||
		!strings.Contains(msg, "still billed") || strings.Contains(msg, "was deleted") || strings.Contains(msg, "timed out") || len(f.deleted) != 1 {
		t.Errorf("error = %q, deleted = %v", msg, f.deleted)
	}
}

func TestGCPWaitRunningPrivateAddress(t *testing.T) {
	f := &fakeGCE{privateOnly: true}
	startFakeGCE(t, f)

	f.instances["node-c"] = map[string]any{}

	rec := newJoinRecorder()
	h := gcpTestRun(t, "", "fake", joinRoleWorker, rec)
	h.spec.AssociatePublicIP, h.created, h.spec.Project = false, true, gcpTestProject

	client, err := newGCPClient(h.credJSON)
	if err != nil {
		t.Fatal(err)
	}

	h.client = client

	if err := h.waitRunning(context.Background()); err != nil || h.join.node != "10.132.0.20" {
		t.Fatalf("waitRunning = %v, node %q", err, h.join.node)
	}

	rec.OnDone("", "")

	_, _, events := rec.wait(t)
	if last := events[len(events)-1].Message; !strings.Contains(last, "private address 10.132.0.20") || !strings.Contains(last, "route") {
		t.Errorf("progress = %q", last)
	}
}

func TestGCPDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := GCPOptions(cfg, "", "")
	if o := decodeJSON[gcpOptions](t, out, err); o.Project != "example-project" || o.Zone != "europe-west1-b" || len(o.MachineTypes) == 0 ||
		len(o.Networks) == 0 || len(o.Images) != 1 || o.Images[0].TalosVersion != demoTalosVersion {
		t.Errorf("demo options = %+v", o)
	}

	rec := newJoinRecorder()
	StartGCPCreate(cfg, "", gcpTestCred(t), `{"zone":"europe-west1-b","machineType":"e2-medium","image":"talos-by-hand","subnetwork":"nodes","hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}
