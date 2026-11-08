package ichorgo

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const (
	awsTestSnapshot = "snap-0123456789abcdef0"
	awsTestNewAMI   = "ami-0123456789abcdef5"
	awsTestHelper   = "ami-0123456789abcdef7"
)

// fakeAWSHelper is the AMI run's side of fakeEC2: the helper's states, its console, the
// snapshot and the AMI. fakeEC2's lock covers it.
type fakeAWSHelper struct {
	states        []string // DescribeInstances' answers in turn, the last one repeated
	console       string
	failSnapshot  int // the status CreateSnapshot answers, after creating when >= 500
	snapshotFails bool
	failRegister  int // the status RegisterImage answers, after registering when >= 500
	failTerminate bool
	nameTaken     bool
	noHelperAMI   bool
	onRunning     func()

	calls          int
	snapshotPolls  int
	snapshots      []url.Values
	registers      []url.Values
	deregistered   []string
	deletedSnaps   []string
	stopped        []string
	snapshotExists bool
	imageExists    bool
}

const awsHelperConsole = "[  5.1] cloud-init[812]: starting\nichor: writing the image to /dev/nvme1n1\n" + awsImageMarkerOK + "\n[ 61.0] reboot: Power down\n"

func newFakeAWSHelper() *fakeAWSHelper {
	return &fakeAWSHelper{states: []string{awsStatePending, awsStateRunning, awsStateStopping, awsStateStopped}, console: awsHelperConsole}
}

func (h *fakeAWSHelper) handle(w http.ResponseWriter, action string, form url.Values) bool {
	const ns = ` xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"`

	switch action {
	case "GetConsoleOutput":
		fmt.Fprint(w, `<GetConsoleOutputResponse`+ns+`><instanceId>`+awsTestInstance+`</instanceId><output>`+
			base64.StdEncoding.EncodeToString([]byte(h.console))+`</output></GetConsoleOutputResponse>`)
	case "CreateSnapshot":
		h.snapshots = append(h.snapshots, form)

		if h.failSnapshot != 0 {
			h.snapshotExists = h.failSnapshot >= 500
			awsFail(w, h.failSnapshot, "InternalError", "snapshot broke")

			return true
		}

		h.snapshotExists = true
		fmt.Fprint(w, `<CreateSnapshotResponse`+ns+`><snapshotId>`+awsTestSnapshot+`</snapshotId><status>pending</status></CreateSnapshotResponse>`)
	case "DescribeSnapshots":
		fmt.Fprint(w, `<DescribeSnapshotsResponse`+ns+`><snapshotSet>`+h.snapshotItems(form)+`</snapshotSet></DescribeSnapshotsResponse>`)
	case "RegisterImage":
		h.registers = append(h.registers, form)

		if h.failRegister != 0 {
			h.imageExists = h.failRegister >= 500
			awsFail(w, h.failRegister, "InvalidParameterValue", "register broke")

			return true
		}

		h.imageExists = true
		fmt.Fprint(w, `<RegisterImageResponse`+ns+`><imageId>`+awsTestNewAMI+`</imageId></RegisterImageResponse>`)
	case "DeregisterImage":
		h.deregistered, h.imageExists = append(h.deregistered, form.Get("ImageId")), false
		fmt.Fprint(w, `<DeregisterImageResponse`+ns+`><return>true</return></DeregisterImageResponse>`)
	case "DeleteSnapshot":
		h.deletedSnaps, h.snapshotExists = append(h.deletedSnaps, form.Get("SnapshotId")), false
		fmt.Fprint(w, `<DeleteSnapshotResponse`+ns+`><return>true</return></DeleteSnapshotResponse>`)
	case "StopInstances":
		h.stopped = append(h.stopped, form.Get("InstanceId.1"))
		fmt.Fprint(w, `<StopInstancesResponse`+ns+`/>`)
	default:
		return false
	}

	return true
}

func (h *fakeAWSHelper) snapshotItems(form url.Values) string {
	if strings.HasPrefix(form.Get("Filter.1.Name"), "tag:") {
		if !h.snapshotExists {
			return ""
		}

		return `<item><snapshotId>` + awsTestSnapshot + `</snapshotId><status>pending</status></item>`
	}

	h.snapshotPolls++

	switch {
	case h.snapshotFails:
		return `<item><snapshotId>` + awsTestSnapshot + `</snapshotId><status>error</status><statusMessage>volume read error</statusMessage></item>`
	case h.snapshotPolls < 2:
		return `<item><snapshotId>` + awsTestSnapshot + `</snapshotId><status>pending</status><progress>40%</progress></item>`
	}

	return `<item><snapshotId>` + awsTestSnapshot + `</snapshotId><status>completed</status><progress>100%</progress></item>`
}

func (h *fakeAWSHelper) images(form url.Values) string {
	switch {
	case form.Get("Owner.1") == "amazon":
		if h.noHelperAMI || !strings.HasSuffix(form.Get("Filter.1.Value.1"), "-x86_64") || form.Get("Filter.2.Value.1") != "available" {
			return ""
		}

		return `<item><imageId>ami-0123456789abcdef6</imageId><creationDate>2026-09-01T10:00:00.000Z</creationDate></item>` +
			`<item><imageId>` + awsTestHelper + `</imageId><creationDate>2026-10-20T10:00:00.000Z</creationDate></item>`
	case form.Get("Filter.1.Name") == "name" && h.nameTaken:
		return `<item><imageId>ami-0123456789abcdef8</imageId><name>` + form.Get("Filter.1.Value.1") + `</name></item>`
	case strings.HasPrefix(form.Get("Filter.1.Name"), "tag:") && h.imageExists:
		return `<item><imageId>` + awsTestNewAMI + `</imageId></item>`
	}

	return ""
}

func (h *fakeAWSHelper) instance() string {
	state := h.states[min(h.calls, len(h.states)-1)]
	h.calls++

	if state == awsStateRunning && h.onRunning != nil {
		h.onRunning()
		h.onRunning = nil
	}

	return `<item><instancesSet><item><instanceId>` + awsTestInstance + `</instanceId><instanceState><name>` + state + `</name></instanceState>` +
		`<blockDeviceMapping><item><deviceName>/dev/xvda</deviceName><ebs><volumeId>vol-0123456789abcdef0</volumeId></ebs></item>` +
		`<item><deviceName>/dev/sdf</deviceName><ebs><volumeId>vol-0123456789abcdef1</volumeId></ebs></item></blockDeviceMapping></item></instancesSet></item>`
}

func awsImageTestRun(cfg string, rec *joinRecorder) *awsImageRun {
	return &awsImageRun{
		config: cfg, context: "fake", credJSON: awsTestCred,
		spec:     awsImageSpec{TemplateNode: joinTemplateNode, SubnetID: awsTestSubnet, SecurityGroupID: awsTestSG},
		listener: maskedJoinListener{rec}, poll: joinPollInterval, stopWait: 100 * joinPollInterval, snapshotWait: 100 * joinPollInterval,
	}
}

// awsTags reads the tags of one TagSpecification.N of a create call.
func awsTags(p url.Values, prefix string) map[string]string {
	tags := map[string]string{}

	for i := 1; p.Get(fmt.Sprintf("%s.Tag.%d.Key", prefix, i)) != ""; i++ {
		tags[p.Get(fmt.Sprintf("%s.Tag.%d.Key", prefix, i))] = p.Get(fmt.Sprintf("%s.Tag.%d.Value", prefix, i))
	}

	return tags
}

func startAWSImage(t *testing.T, h *fakeAWSHelper, edit func(*awsImageRun)) (*fakeEC2, string, string, []joinProgress) {
	t.Helper()
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeEC2{helper: h}
	startFakeEC2(t, f)

	rec := newJoinRecorder()

	run := awsImageTestRun(cfg, rec)
	if edit != nil {
		edit(run)
	}

	run.start(time.Minute)

	result, msg, events := rec.wait(t)

	return f, result, msg, events
}

func TestAWSImageHappyPath(t *testing.T) {
	h := newFakeAWSHelper()
	f, result, msg, events := startAWSImage(t, h, nil)

	if msg != "" {
		t.Fatalf("error %q, events %+v", msg, events)
	}

	want := []string{awsPhaseCreating, awsPhaseWriting, awsPhaseSnapshot, awsPhaseRegister, awsPhaseCleanup, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	var r awsImageResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}

	if r.ImageID != awsTestNewAMI || r.SnapshotID != awsTestSnapshot || r.Arch != bootArchAMD64 || r.TalosVersion != "v1.14.2" ||
		len(r.Schematic) != 64 || r.SizeGB != awsImageVolumeGiB || r.Name != "talos-v1.14.2-aws-amd64-"+r.Schematic[:12] {
		t.Errorf("result = %+v", r)
	}

	checkAWSHelperRun(t, f)
	checkAWSSnapshotAndImage(t, h, r.Schematic)

	if !slices.Equal(f.terminated, []string{awsTestInstance}) || len(h.deregistered) != 0 || len(h.deletedSnaps) != 0 || !h.imageExists {
		t.Errorf("terminated %v, deregistered %v, deleted snapshots %v", f.terminated, h.deregistered, h.deletedSnaps)
	}

	for _, e := range events {
		if strings.Contains(e.Message, awsTestSecret) || strings.Contains(e.Message, awsTestToken) || strings.Contains(e.Message, awsTestAccount) {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", awsImageAuditAction)
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "arch=amd64 region=eu-west-1 version=v1.14.2 image="+awsTestNewAMI) ||
		strings.Contains(entries[0].Params, awsTestKeyID) {
		t.Errorf("audit = %+v", entries)
	}
}

func checkAWSHelperRun(t *testing.T, f *fakeEC2) {
	t.Helper()

	if len(f.dryRuns) != 1 || len(f.runs) != 1 {
		t.Fatalf("dry runs %d, runs %d", len(f.dryRuns), len(f.runs))
	}

	p := f.runs[0]
	if p.Get("ImageId") != awsTestHelper || p.Get("InstanceType") != awsImageTypeAMD64 || p.Get("InstanceInitiatedShutdownBehavior") != "stop" ||
		p.Get("NetworkInterface.1.SubnetId") != awsTestSubnet || p.Get("NetworkInterface.1.SecurityGroupId.1") != awsTestSG ||
		p.Get("NetworkInterface.1.AssociatePublicIpAddress") != "true" || p.Get("SubnetId") != "" ||
		p.Get("BlockDeviceMapping.1.DeviceName") != "/dev/sdf" || p.Get("BlockDeviceMapping.1.Ebs.VolumeSize") != "8" ||
		p.Get("BlockDeviceMapping.1.Ebs.VolumeType") != "gp3" || p.Get("BlockDeviceMapping.1.Ebs.DeleteOnTermination") != "true" ||
		p.Get("TagSpecification.1.ResourceType") != "instance" || p.Get("TagSpecification.2.ResourceType") != "volume" {
		t.Errorf("RunInstances = %v", p)
	}

	run := awsTags(p, "TagSpecification.1")[awsTagRun]
	if run == "" || awsTags(p, "TagSpecification.2")[awsTagRun] != run || p.Get("ClientToken") != awsClientTokenPrefix+run ||
		f.dryRuns[0].Get("UserData") != p.Get("UserData") {
		t.Errorf("tags and token = %v", p)
	}

	data, err := base64.StdEncoding.DecodeString(p.Get("UserData"))
	if err != nil {
		t.Fatal(err)
	}

	script := string(data)
	for _, want := range []string{"#!/bin/bash", "set -euo pipefail", "/v1.14.2/aws-amd64.raw.xz", "xz -dc | dd of=\"$disk\" bs=4M conv=sparse",
		"findmnt -no SOURCE /", "size=8589934592", `say "$m-OK"`, "$m-FAIL", "shutdown -h now", "/dev/ttyS0"} {
		if !strings.Contains(script, want) {
			t.Errorf("the script lacks %q:\n%s", want, script)
		}
	}

	// The console's marker never shows in the script itself, else cloud-init echoing it would pass.
	if strings.Contains(script, awsImageMarkerOK) || strings.Contains(script, "nvme1n1") {
		t.Errorf("the script names the marker or a device:\n%s", script)
	}
}

func checkAWSSnapshotAndImage(t *testing.T, h *fakeAWSHelper, schematic string) {
	t.Helper()

	if len(h.snapshots) != 1 || len(h.registers) != 1 {
		t.Fatalf("snapshots %d, registers %d", len(h.snapshots), len(h.registers))
	}

	s := h.snapshots[0]
	tags := awsTags(s, "TagSpecification.1")

	if s.Get("VolumeId") != "vol-0123456789abcdef1" || s.Get("TagSpecification.1.ResourceType") != "snapshot" ||
		!strings.HasPrefix(s.Get("Description"), "Talos v1.14.2 aws amd64 ") || tags["os"] != "talos" ||
		tags[awsTagTalosVersion] != "v1.14.2" || tags[awsTagArch] != bootArchAMD64 || tags[awsTagSchematic] != schematic || tags[awsTagRun] == "" {
		t.Errorf("CreateSnapshot = %v", s)
	}

	i := h.registers[0]
	if i.Get("Architecture") != awsArchX86 || i.Get("RootDeviceName") != "/dev/xvda" || i.Get("VirtualizationType") != "hvm" ||
		i.Get("EnaSupport") != "true" || i.Get("BootMode") != "uefi-preferred" || i.Get("BlockDeviceMapping.1.DeviceName") != "/dev/xvda" ||
		i.Get("BlockDeviceMapping.1.Ebs.SnapshotId") != awsTestSnapshot || i.Get("TagSpecification.1.ResourceType") != "image" ||
		awsTags(i, "TagSpecification.1")[awsTagSchematic] != schematic || !strings.Contains(i.Get("Description"), schematic) {
		t.Errorf("RegisterImage = %v", i)
	}
}

func TestAWSImageFailuresCleanUp(t *testing.T) {
	cases := []struct {
		name          string
		helper        func(*fakeAWSHelper)
		wantInError   []string
		wantSnapshots int
		wantStopped   bool
		wantDeleted   []string // snapshots, then AMIs
	}{
		{
			name:        "the write fails: no snapshot, the helper terminated",
			helper:      func(h *fakeAWSHelper) { h.console = "ichor: writing\n" + awsImageMarkerFail + ": line 9 exited 22\n" },
			wantInError: []string{"could not write the image: line 9 exited 22", "Deleted: instance " + awsTestInstance},
		},
		{
			name:        "no marker on the console: no snapshot of a volume of zeros",
			helper:      func(h *fakeAWSHelper) { h.console = "[  5.1] cloud-init[812]: starting\n" },
			wantInError: []string{"shows no result", "cloud-init[812]: starting", "Deleted: instance"},
		},
		{
			name:        "the helper never powers off: stopped, then terminated",
			helper:      func(h *fakeAWSHelper) { h.states = []string{awsStateRunning} },
			wantInError: []string{"did not finish writing", "Deleted: instance"}, wantStopped: true,
		},
		{
			name:        "a refused snapshot left nothing",
			helper:      func(h *fakeAWSHelper) { h.failSnapshot = http.StatusBadRequest },
			wantInError: []string{"snapshot broke", "Deleted: instance"}, wantSnapshots: 1,
		},
		{
			name:        "a snapshot without a clear answer: found by its tag and deleted",
			helper:      func(h *fakeAWSHelper) { h.failSnapshot = http.StatusServiceUnavailable },
			wantInError: []string{"snapshot broke", "unfinished snapshot " + awsTestSnapshot}, wantSnapshots: 1, wantDeleted: []string{awsTestSnapshot},
		},
		{
			name:        "a failed snapshot is deleted",
			helper:      func(h *fakeAWSHelper) { h.snapshotFails = true },
			wantInError: []string{"volume read error", "unfinished snapshot"}, wantSnapshots: 1, wantDeleted: []string{awsTestSnapshot},
		},
		{
			name:        "a refused register: the snapshot is deleted",
			helper:      func(h *fakeAWSHelper) { h.failRegister = http.StatusBadRequest },
			wantInError: []string{"register broke", "unfinished snapshot"}, wantSnapshots: 1, wantDeleted: []string{awsTestSnapshot},
		},
		{
			name:        "a register without a clear answer: the AMI is found by its tag and deregistered",
			helper:      func(h *fakeAWSHelper) { h.failRegister = http.StatusInternalServerError },
			wantInError: []string{"register broke", "unfinished AMI " + awsTestNewAMI}, wantSnapshots: 1, wantDeleted: []string{awsTestSnapshot, awsTestNewAMI},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFakeAWSHelper()
			c.helper(h)

			f, result, msg, _ := startAWSImage(t, h, nil)
			if result != "" {
				t.Errorf("result = %q", result)
			}

			for _, want := range c.wantInError {
				if !strings.Contains(msg, want) {
					t.Errorf("error = %q, want %q in it", msg, want)
				}
			}

			if strings.Contains(msg, awsTestAccount) || strings.Contains(msg, awsTestKeyID) {
				t.Errorf("error carries a secret: %q", msg)
			}

			deleted := append(slices.Clone(h.deletedSnaps), h.deregistered...)
			if len(h.snapshots) != c.wantSnapshots || !slices.Equal(deleted, c.wantDeleted) || (len(h.stopped) == 1) != c.wantStopped {
				t.Errorf("snapshots %d, deleted %v, stopped %v", len(h.snapshots), deleted, h.stopped)
			}

			if !slices.Equal(f.terminated, []string{awsTestInstance}) || h.snapshotExists || h.imageExists {
				t.Errorf("left behind: terminated %v, snapshot %v, image %v", f.terminated, h.snapshotExists, h.imageExists)
			}

			if entries := readAudit(t, "fake", awsImageAuditAction); len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestAWSImageRefusedBeforeAnyWrite(t *testing.T) {
	cases := []struct {
		name   string
		fake   func(*fakeEC2)
		helper func(*fakeAWSHelper)
		edit   func(*awsImageRun)
		want   string
	}{
		{name: "keys that may not run the helper", fake: func(f *fakeEC2) { f.noRunRight = true }, want: "may not run the helper"},
		{name: "unknown architecture", edit: func(r *awsImageRun) { r.spec.Arch = "riscv64" }, want: "unknown architecture"},
		{name: "no subnet", edit: func(r *awsImageRun) { r.spec.SubnetID = " " }, want: "pick a subnet"},
		{name: "a security group of another VPC", edit: func(r *awsImageRun) { r.spec.SecurityGroupID = awsTestSGOther }, want: "not in the VPC of subnet"},
		{name: "an AMI of this version and schematic exists", helper: func(h *fakeAWSHelper) { h.nameTaken = true }, want: "exists: ami-0123456789abcdef8"},
		{
			name: "a name taken", helper: func(h *fakeAWSHelper) { h.nameTaken = true }, edit: func(r *awsImageRun) { r.spec.Name = "talos-own" },
			want: `already named "talos-own"`,
		},
		{name: "an invalid name", edit: func(r *awsImageRun) { r.spec.Name = "a|b" }, want: "not an AMI name"},
		{name: "no Amazon Linux for the architecture", helper: func(h *fakeAWSHelper) { h.noHelperAMI = true }, want: "no Amazon Linux 2023 AMI for amd64"},
		{name: "a member that cannot be read", edit: func(r *awsImageRun) { r.spec.TemplateNode = "192.0.2.99" }, want: "192.0.2.99"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			h := newFakeAWSHelper()

			if c.helper != nil {
				c.helper(h)
			}

			f := &fakeEC2{helper: h}
			if c.fake != nil {
				c.fake(f)
			}

			startFakeEC2(t, f)

			rec := newJoinRecorder()

			run := awsImageTestRun(cfg, rec)
			if c.edit != nil {
				c.edit(run)
			}

			run.start(time.Minute)

			if _, msg, _ := rec.wait(t); !strings.Contains(msg, c.want) || strings.Contains(msg, awsTestAccount) {
				t.Errorf("error = %q, want %q in it", msg, c.want)
			}

			if len(f.runs) != 0 || len(h.snapshots) != 0 || len(h.registers) != 0 || len(f.terminated) != 0 {
				t.Errorf("writes: runs %d, snapshots %d, registers %d", len(f.runs), len(h.snapshots), len(h.registers))
			}
		})
	}
}

// Without the template's config the schematic is unknown: no AMI of the wrong image.
func TestAWSImageRefusesAnUnreadableSchematic(t *testing.T) {
	withDataDir(t)

	talos := newFakeTalos()
	talos.addNode(t, joinTemplateNode, "v1.14.2", machine.TypeWorker)
	cfg := talos.start(t, joinTemplateNode)

	f := &fakeEC2{helper: newFakeAWSHelper()}
	startFakeEC2(t, f)

	rec := newJoinRecorder()
	awsImageTestRun(cfg, rec).start(time.Minute)

	if _, msg, _ := rec.wait(t); !strings.Contains(msg, "schematic") || len(f.runs) != 0 {
		t.Errorf("error = %q, runs = %d", msg, len(f.runs))
	}
}

func TestAWSImageFailedTerminateNamesTheInstance(t *testing.T) {
	h := newFakeAWSHelper()
	h.failTerminate = true

	_, _, msg, _ := startAWSImage(t, h, nil)

	for _, want := range []string{"AMI " + awsTestNewAMI + " is ready", "instance " + awsTestInstance + " (ichor-ami-", "still billed", "in eu-west-1", "AWS console"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want %q in it", msg, want)
		}
	}
}

func TestAWSImageStopCleansUp(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	running := make(chan struct{})
	h := newFakeAWSHelper()
	h.states = []string{awsStateRunning}
	h.onRunning = func() { close(running) }

	f := &fakeEC2{helper: h}
	startFakeEC2(t, f)

	rec := newJoinRecorder()
	run := awsImageTestRun(cfg, rec).start(time.Minute)

	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatal("the helper was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Deleted: instance "+awsTestInstance) ||
		!slices.Equal(f.terminated, []string{awsTestInstance}) || len(h.snapshots) != 0 {
		t.Errorf("error = %q, terminated = %v, snapshots = %d", msg, f.terminated, len(h.snapshots))
	}
}

func TestAWSImageDemoRefused(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := newJoinRecorder()
	StartAWSImage(cfg, "", awsTestCred, `{"subnetId":"subnet-0123456789abcdef0"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo AMI = %q", msg)
	}
}

func TestAWSImageEstimate(t *testing.T) {
	f := &fakeEC2{}
	startFakeEC2(t, f)

	out, err := AWSImageEstimate(awsTestCred, bootArchAMD64, "")
	if c := decodeJSON[awsImageCost](t, out, err); c.InstanceType != awsImageTypeAMD64 || c.Hourly != "0.0208" || c.Currency != "USD" ||
		c.SnapshotMonthlyPerGB != "0.0500" || c.Minutes != awsImageMinutes {
		t.Errorf("estimate = %+v", c)
	}

	out, err = AWSImageEstimate(awsTestCred, "aarch64", "us-east-1")
	if c := decodeJSON[awsImageCost](t, out, err); c.InstanceType != awsImageTypeARM64 || !strings.Contains(f.priceFilter, "us-east-1") {
		t.Errorf("arm64 estimate = %+v, filter %s", c, f.priceFilter)
	}

	f.noPricing = true

	out, err = AWSImageEstimate(awsTestCred, bootArchAMD64, "")
	if c := decodeJSON[awsImageCost](t, out, err); c.Hourly != "" || c.SnapshotMonthlyPerGB != "" || c.Currency != "" || c.InstanceType == "" {
		t.Errorf("estimate without prices = %+v", c)
	}

	if _, err := AWSImageEstimate(awsTestCred, "", ""); err == nil || !strings.Contains(err.Error(), "pick an architecture") {
		t.Errorf("no architecture: %v", err)
	}

	if len(f.runs) != 0 || len(f.dryRuns) != 0 {
		t.Errorf("an estimate ran something: %d runs", len(f.runs))
	}
}

func TestAWSConsoleVerdict(t *testing.T) {
	cases := []struct {
		console string
		ok      bool
		failure string
		tail    int
	}{
		{console: "a\r\n" + awsImageMarkerOK + "\r\nreboot: Power down\r\n", ok: true},
		{console: awsImageMarkerOK + "\n" + awsImageMarkerFail + ": no empty 8 GiB disk\n", failure: "no empty 8 GiB disk"},
		{console: awsImageMarkerFail + "\n", failure: "no reason given"},
		{console: "1\n2\n\n3\n4\n5\n6\n7\n", tail: awsImageTailLines},
		{console: "", tail: 0},
	}

	for _, c := range cases {
		ok, failure, tail := awsConsoleVerdict(c.console)
		if ok != c.ok || failure != c.failure || len(tail) != c.tail {
			t.Errorf("verdict(%q) = %v, %q, %q", c.console, ok, failure, tail)
		}
	}
}

func TestAWSIAMPolicyCoversTheImageRun(t *testing.T) {
	policy := AWSIAMPolicy()

	for _, want := range []string{
		"ec2:CreateSnapshot", "ec2:DescribeSnapshots", "ec2:RegisterImage", "ec2:GetConsoleOutput",
		"ec2:StopInstances", "ec2:DeleteSnapshot", "ec2:DeregisterImage", `"CreateSnapshot"`, `"RegisterImage"`,
	} {
		if !strings.Contains(policy, want) {
			t.Errorf("%s not in the policy", want)
		}
	}

	var parsed struct {
		Statement []awsPolicyStatement `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(policy), &parsed); err != nil {
		t.Fatal(err)
	}

	for _, s := range parsed.Statement {
		if slices.Contains(s.Action, "ec2:DeleteSnapshot") {
			like, _ := s.Condition["StringLike"].(map[string]any)
			if like["aws:ResourceTag/"+awsTagRun] != "*" {
				t.Errorf("the delete statement is not limited to the run's tag: %+v", s)
			}
		}
	}
}
