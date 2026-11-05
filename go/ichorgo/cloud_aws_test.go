package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func TestAWSSignsEveryCall(t *testing.T) {
	f := &fakeEC2{}
	startFakeEC2(t, f)

	if _, err := AWSTest(awsTestCred); err != nil {
		t.Fatal(err)
	}

	if _, err := AWSPrice(awsTestCred, "t3.small"); err != nil {
		t.Fatal(err)
	}

	// The fake refuses a call without the signature and the session token: each one got there.
	want := []string{"eu-west-1/sts", "eu-west-1/ec2", "us-east-1/pricing"}
	if !slices.Equal(f.scopes, want) {
		t.Errorf("credential scopes = %v, want %v", f.scopes, want)
	}
}

func TestAWSTest(t *testing.T) {
	cases := []struct {
		name string
		cred string
		fake *fakeEC2
		want awsTestResult
		fail string
	}{
		{name: "may run instances", cred: awsTestCred, fake: &fakeEC2{}, want: awsTestResult{CanCreate: true, Checked: true}},
		{name: "may not", cred: awsTestCred, fake: &fakeEC2{noRunRight: true}, want: awsTestResult{Checked: true}},
		{name: "no cloud-images.json: cannot tell", cred: awsTestCred, fake: &fakeEC2{noCloudImages: true}, want: awsTestResult{}},
		{
			name: "wrong keys", fake: &fakeEC2{}, fail: "check the access key",
			cred: `{"accessKeyId":"AKIAWRONGWRONGWRONG1","secretAccessKey":"wrong-secret-value","region":"eu-west-1"}`,
		},
		{name: "no keys", cred: "", fake: &fakeEC2{}, fail: "access keys first"},
		{name: "no region", cred: `{"accessKeyId":"a","secretAccessKey":"b"}`, fake: &fakeEC2{}, fail: "pick an AWS region"},
		{name: "a region that is not one", cred: `{"accessKeyId":"a","secretAccessKey":"b","region":"evil.example.com/x"}`, fake: &fakeEC2{}, fail: "not an AWS region"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeEC2(t, c.fake)

			out, err := AWSTest(c.cred)
			if c.fail != "" {
				if err == nil || !strings.Contains(err.Error(), c.fail) || strings.Contains(err.Error(), "wrong-secret-value") {
					t.Fatalf("err = %v, want %q in it", err, c.fail)
				}

				return
			}

			got := decodeJSON[awsTestResult](t, out, err)
			if got.Account != awsTestAccount || got.Region != "eu-west-1" || !strings.HasPrefix(got.Arn, "arn:aws:iam::") ||
				got.CanCreate != c.want.CanCreate || got.Checked != c.want.Checked {
				t.Errorf("got %+v, want %+v", got, c.want)
			}

			if len(c.fake.runs) != 0 {
				t.Errorf("a real RunInstances was sent: %v", c.fake.runs)
			}

			if c.fake.noCloudImages == (len(c.fake.dryRuns) == 1) {
				t.Errorf("dry runs = %v", c.fake.dryRuns)
			}
		})
	}
}

func TestAWSOptions(t *testing.T) {
	cases := []struct {
		name       string
		fake       *fakeEC2
		wantImages []string
		wantWarn   string
	}{
		{name: "official AMIs of the version and region, both archs", fake: &fakeEC2{}, wantImages: []string{awsTestAMI, awsTestAMIARM, awsTestAMIOwn}},
		{name: "no cloud-images.json: a warning", fake: &fakeEC2{noCloudImages: true}, wantImages: []string{awsTestAMIOwn}, wantWarn: "cannot be listed"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeEC2(t, c.fake)

			out, err := AWSOptions(cfg, "fake", awsTestCred)
			o := decodeJSON[awsOptions](t, out, err)

			var types []string
			for _, typ := range o.Types {
				types = append(types, typ.Name+"/"+typ.Arch)
			}

			if !slices.Equal(types, []string{"t3.small/amd64", "t4g.small/arm64", "m6i.large/amd64"}) || o.Types[0].MemoryGB != 2 || !o.Types[0].Burstable {
				t.Errorf("types = %+v (both pages, no t2, no Mac)", o.Types)
			}

			var images []string
			for _, i := range o.Images {
				images = append(images, i.ID)
			}

			if !slices.Equal(images, c.wantImages) {
				t.Errorf("images = %+v, want %v", o.Images, c.wantImages)
			}

			own := o.Images[len(o.Images)-1]
			if own.Source != awsImageCustom || own.Arch != bootArchARM64 || own.TalosVersion != "v1.14.2" || own.Schematic != "sample-schematic" {
				t.Errorf("own AMI = %+v", own)
			}

			if c.wantWarn == "" && (o.Images[0].Source != awsImageOfficial || o.Images[0].TalosVersion != "v1.14.2" || o.Images[0].Arch != bootArchAMD64 ||
				o.Images[0].Schematic != defaultSchematic) {
				t.Errorf("official AMI = %+v", o.Images[0])
			}

			// The template installs the plain installer: the default schematic, the one of the official AMIs.
			if o.TemplateVersion != "v1.14.2" || o.TemplateSchematic != defaultSchematic {
				t.Errorf("template = %q %q", o.TemplateVersion, o.TemplateSchematic)
			}

			if got := strings.Join(o.Warnings, "\n"); (c.wantWarn == "") != (got == "") || !strings.Contains(got, c.wantWarn) {
				t.Errorf("warnings = %q, want %q", o.Warnings, c.wantWarn)
			}

			if o.Region != "eu-west-1" || !slices.Equal(o.Regions, []string{"eu-west-1", "us-east-1"}) {
				t.Errorf("regions = %s %v", o.Region, o.Regions)
			}

			if len(o.Subnets) != 2 || o.Subnets[0].ID != awsTestSubnet || !o.Subnets[0].VPCDefault || !o.Subnets[0].Public ||
				o.Subnets[0].Name != "nodes" || o.Subnets[1].VPCDefault || o.Subnets[1].Public {
				t.Errorf("subnets = %+v", o.Subnets)
			}

			if len(o.SecurityGroups) != 2 || o.SecurityGroups[0] != (awsSecurityGroup{ID: awsTestSG, VPCID: awsTestVPC, Name: "talos", Description: "Talos nodes"}) {
				t.Errorf("security groups = %+v", o.SecurityGroups)
			}

			for _, key := range []string{`"memoryGB":2,`, `"vpcDefault":true`, `"securityGroups":[{"id"`, `"source":"custom"`, `"templateVersion":"v1.14.2"`, `"templateSchematic":"` + defaultSchematic} {
				if !strings.Contains(out, key) {
					t.Errorf("%s not in %s", key, out)
				}
			}
		})
	}
}

func TestAWSTemplateImage(t *testing.T) {
	factory := factoryHost + "/metal-installer/" + sampleSchematic + ":v1.14.4"

	cases := []struct {
		name          string
		src           bootSource
		err           error
		wantVersion   string
		wantSchematic string
		wantWarning   string
	}{
		{name: "a factory image", src: bootSource{node: "cp-1", version: "v1.14.4", arch: "amd64", image: factory}, wantVersion: "v1.14.4", wantSchematic: sampleSchematic},
		{
			name: "the plain installer: the default schematic", src: bootSource{node: "cp-1", version: "v1.14.4", image: "ghcr.io/siderolabs/installer:v1.14.4"},
			wantVersion: "v1.14.4", wantSchematic: defaultSchematic,
		},
		{
			name: "an install image that cannot be read", src: bootSource{node: "cp-1", version: "v1.14.4", imageErr: "permission denied"},
			wantVersion: "v1.14.4", wantWarning: "install image of cp-1 cannot be read (permission denied)",
		},
		{name: "a member that cannot be read", err: errors.New("no member answers"), wantWarning: "cannot be read (no member answers)"},
		{name: "no version", src: bootSource{node: "cp-1", image: "registry.example.com/installer"}, wantWarning: "which Talos version"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img, warnings := awsTemplateImageOf(c.src, c.err)
			if img.version != c.wantVersion || img.schematic != c.wantSchematic {
				t.Errorf("image = %+v, want %q %q", img, c.wantVersion, c.wantSchematic)
			}

			if got := strings.Join(warnings, "\n"); (c.wantWarning == "") != (got == "") || !strings.Contains(got, c.wantWarning) {
				t.Errorf("warnings = %q, want %q", warnings, c.wantWarning)
			}
		})
	}
}

func TestAWSCloudImagesCachedPerVersion(t *testing.T) {
	f := &fakeEC2{}
	startFakeEC2(t, f)

	ctx := context.Background()

	for range 2 {
		if images, err := talosAWSImages(ctx, "v1.14.2", "eu-west-1"); err != nil || len(images) != 2 {
			t.Fatalf("images = %+v, %v", images, err)
		}
	}

	if _, err := talosAWSImages(ctx, "v1.14.3", "eu-west-1"); err != nil {
		t.Fatal(err)
	}

	if f.imageFetch != 2 {
		t.Errorf("fetched %d times, want once per version", f.imageFetch)
	}

	if _, err := talosAWSImages(ctx, "../../evil", "eu-west-1"); err == nil {
		t.Error("a version that is not one was fetched")
	}
}

func TestAWSPrice(t *testing.T) {
	f := &fakeEC2{}
	startFakeEC2(t, f)

	out, err := AWSPrice(awsTestCred, "t3.small")
	if err != nil || out != `{"hourly":"0.0208","currency":"USD"}` {
		t.Errorf("price = %s, %v", out, err)
	}

	for _, filter := range []string{`{"Field":"instanceType","Type":"TERM_MATCH","Value":"t3.small"}`, `{"Field":"regionCode","Type":"TERM_MATCH","Value":"eu-west-1"}`, `"ServiceCode":"AmazonEC2"`} {
		if !strings.Contains(f.priceFilter, filter) {
			t.Errorf("%s not in %s", filter, f.priceFilter)
		}
	}

	f.noPricing = true

	if out, err := AWSPrice(awsTestCred, "t3.small"); err != nil || out != `{"hourly":""}` {
		t.Errorf("unavailable price = %s, %v", out, err)
	}
}

func TestAWSIAMPolicy(t *testing.T) {
	var policy struct {
		Version   string               `json:"Version"`
		Statement []awsPolicyStatement `json:"Statement"`
	}

	if err := json.Unmarshal([]byte(AWSIAMPolicy()), &policy); err != nil {
		t.Fatal(err)
	}

	var actions []string

	for _, s := range policy.Statement {
		actions = append(actions, s.Action...)

		if slices.Contains(s.Action, "ec2:TerminateInstances") {
			like, _ := s.Condition["StringLike"].(map[string]any)
			if len(s.Action) != 1 || like["aws:ResourceTag/ichor-run"] != "*" {
				t.Errorf("terminate statement = %+v", s)
			}
		}
	}

	for _, want := range []string{"ec2:RunInstances", "ec2:CreateTags", "ec2:DescribeInstances", "ec2:TerminateInstances", "pricing:GetProducts", "sts:GetCallerIdentity"} {
		if !slices.Contains(actions, want) {
			t.Errorf("%s not in the policy", want)
		}
	}
}

func TestAWSErrorsCarryNoSecret(t *testing.T) {
	c, err := newAWSClient(awsTestCred)
	if err != nil {
		t.Fatal(err)
	}

	got := c.scrub("denied for " + awsTestKeyID + " of " + awsTestAccount + " with " + awsTestToken + ". Encoded authorization failure message: c2FtcGxl")
	if strings.Contains(got, awsTestKeyID) || strings.Contains(got, awsTestAccount) || strings.Contains(got, awsTestToken) || strings.Contains(got, "c2FtcGxl") {
		t.Errorf("scrubbed = %q", got)
	}

	if _, err := newAWSClient(`{"accessKeyId":"` + awsTestKeyID + `","secretAccessKey":"` + awsTestSecret); err == nil || strings.Contains(err.Error(), awsTestSecret) {
		t.Errorf("broken JSON: %v", err)
	}
}

func awsTestRun(cfg, contextName, role string, rec *joinRecorder) *awsCreate {
	return &awsCreate{
		join: &nodeJoin{
			config: cfg, context: contextName, listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/sda"},
		},
		spec: awsCreateSpec{
			Region: "eu-west-1", InstanceType: "t3.small", ImageID: awsTestAMI, SubnetID: awsTestSubnet, SecurityGroupIDs: []string{awsTestSG},
			Hostname: "node-c", Role: role, TemplateNode: joinTemplateNode, InstallDisk: "/dev/sda",
		},
		credJSON: awsTestCred, poll: joinPollInterval,
	}
}

// awsRunUserData is the user data a RunInstances carried, decoded.
func awsRunUserData(t *testing.T, f *fakeEC2) string {
	t.Helper()

	data, err := base64.StdEncoding.DecodeString(f.runs[0].Get("UserData"))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestAWSCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeEC2{onRunning: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeEC2(t, f)

	rec := newJoinRecorder()
	awsTestRun(cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{cloudPhaseCreating, cloudPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	if len(f.dryRuns) != 1 || len(f.runs) != 1 {
		t.Fatalf("dry runs %d, runs %d", len(f.dryRuns), len(f.runs))
	}

	p := f.runs[0]
	tags := map[string]string{}

	for i := 1; p.Get(fmt.Sprintf("TagSpecification.1.Tag.%d.Key", i)) != ""; i++ {
		tags[p.Get(fmt.Sprintf("TagSpecification.1.Tag.%d.Key", i))] = p.Get(fmt.Sprintf("TagSpecification.1.Tag.%d.Value", i))
	}

	if p.Get("ImageId") != awsTestAMI || p.Get("InstanceType") != "t3.small" || p.Get("SubnetId") != awsTestSubnet || p.Get("SecurityGroupId.1") != awsTestSG ||
		p.Get("MinCount") != "1" || p.Get("MaxCount") != "1" || p.Get("TagSpecification.1.ResourceType") != "instance" ||
		tags["Name"] != "node-c" || tags[awsTagRun] == "" || tags[awsTagCluster] == "" || tags[awsTagCluster] == "fake" ||
		p.Get("ClientToken") != awsClientTokenPrefix+tags[awsTagRun] || len(p.Get("ClientToken")) > 64 {
		t.Errorf("RunInstances = %v", p)
	}

	if userData := awsRunUserData(t, f); !strings.Contains(userData, "node-c") || f.dryRuns[0].Get("UserData") != p.Get("UserData") {
		t.Errorf("user data = %q", userData)
	}

	if len(f.terminated) != 0 || len(talos.applies) != 0 {
		t.Errorf("terminated = %v, applies = %+v after a success", f.terminated, talos.applies)
	}

	for _, e := range events {
		if strings.Contains(e.Message, awsTestSecret) || strings.Contains(e.Message, awsTestToken) || strings.Contains(e.Message, awsTestAccount) {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", "aws-instance-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "instance="+awsTestInstance) ||
		!strings.Contains(entries[0].Params, "type=t3.small region=eu-west-1") ||
		strings.Contains(entries[0].Params, awsTestKeyID) || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}
}

// On Omni the run hands over to the Omni watch once the instance runs: the machine appears by its
// host name. (The whole Omni run, on a cluster with no machine yet, is
// TestCloudCreateOmniEmptyCluster.)
func TestAWSCreateWaitsForOmni(t *testing.T) {
	rec := newJoinRecorder()
	h := awsTestRun("", "", "", rec)

	calls := 0
	h.omniHostnames = func(context.Context, *session) ([]string, error) {
		calls++
		if calls < 3 {
			return []string{"other"}, nil
		}

		return []string{"other", "NODE-C"}, nil
	}

	run := &cloudCreateRun{provider: h, join: h.join, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := run.waitOmni(ctx, nil); err != nil || calls != 3 {
		t.Errorf("waitOmni = %v after %d calls", err, calls)
	}
}

// awsBigEdits makes the user data larger than EC2 takes.
var awsBigEdits = `[{"doc":0,"path":["machine","sysctls","net.ichor.padding"],"op":"put","type":"string","value":"` + strings.Repeat("x", awsUserDataLimit) + `"}]`

func TestAWSCreateFailures(t *testing.T) {
	cases := []struct {
		name           string
		fake           *fakeEC2
		mutate         func(*awsCreate)
		wantInError    string
		wantRun        bool
		wantTerminated bool
	}{
		{
			name: "no right to run: the dry run refuses before any billable call", fake: &fakeEC2{noRunRight: true},
			wantInError: "ec2:RunInstances",
		},
		{
			name: "an AMI of another architecture: refused before any write", fake: &fakeEC2{imageArch: awsArchARM},
			wantInError: "this AMI is arm64, the type is x86_64",
		},
		{
			name: "a security group of another VPC: refused before any write", fake: &fakeEC2{},
			mutate:      func(h *awsCreate) { h.spec.SecurityGroupIDs = []string{awsTestSG, awsTestSGOther} },
			wantInError: "another VPC",
		},
		{
			name: "user data over 16 KB: refused before any write", fake: &fakeEC2{},
			mutate:      func(h *awsCreate) { h.spec.ExtraEdits = awsBigEdits },
			wantInError: "over the 16 KB",
		},
		{
			name: "a refused RunInstances created nothing", fake: &fakeEC2{createStatus: http.StatusBadRequest},
			wantInError: "something broke", wantRun: true,
		},
		{
			name: "a RunInstances without a clear answer: found by its client token and terminated", fake: &fakeEC2{createStatus: http.StatusServiceUnavailable},
			wantInError: "Instance " + awsTestInstance + " was deleted", wantRun: true, wantTerminated: true,
		},
		{
			name: "the instance terminates on its own: an error, and it is terminated", fake: &fakeEC2{endState: "terminated"},
			wantInError: "is terminated: Server.InternalError", wantRun: true, wantTerminated: true,
		},
		{
			name: "the instance shuts down: an error, and it is terminated", fake: &fakeEC2{endState: "shutting-down"},
			wantInError: "is shutting-down", wantRun: true, wantTerminated: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeEC2(t, c.fake)

			rec := newJoinRecorder()

			h := awsTestRun(cfg, "fake", joinRoleWorker, rec)
			if c.mutate != nil {
				c.mutate(h)
			}

			h.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) || strings.Contains(msg, awsTestAccount) || strings.Contains(msg, awsTestKeyID) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if (len(c.fake.runs) == 1) != c.wantRun {
				t.Errorf("RunInstances sent = %d, want %v", len(c.fake.runs), c.wantRun)
			}

			if (len(c.fake.terminated) == 1) != c.wantTerminated {
				t.Errorf("terminated = %v, want %v", c.fake.terminated, c.wantTerminated)
			}

			entries := readAudit(t, "fake", "aws-instance-create")
			if len(entries) != 1 || entries[0].Outcome == auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestAWSCreateStopTerminates(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	started := make(chan struct{})
	f := &fakeEC2{onRunning: func() { close(started) }}
	startFakeEC2(t, f)

	rec := newJoinRecorder()
	run := awsTestRun(cfg, "fake", joinRoleWorker, rec).start(time.Minute)

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("the instance was never asked for")
	}

	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Instance "+awsTestInstance+" was deleted") || !slices.Equal(f.terminated, []string{awsTestInstance}) {
		t.Errorf("error = %q, terminated = %v", msg, f.terminated)
	}
}

func TestAWSRunParamsPublicAddress(t *testing.T) {
	h := awsTestRun("", "fake", joinRoleWorker, newJoinRecorder())
	h.spec.AssociatePublicIP, h.spec.KeyName = true, "ops"

	p := h.runParams("data")
	if p.Get("SubnetId") != "" || p.Get("SecurityGroupId.1") != "" || p.Get("NetworkInterface.1.SubnetId") != awsTestSubnet ||
		p.Get("NetworkInterface.1.SecurityGroupId.1") != awsTestSG || p.Get("NetworkInterface.1.DeviceIndex") != "0" ||
		p.Get("NetworkInterface.1.AssociatePublicIpAddress") != "true" || p.Get("KeyName") != "ops" {
		t.Errorf("params = %v", p)
	}
}

func TestAWSWaitRunningPrivateAddress(t *testing.T) {
	startFakeEC2(t, &fakeEC2{privateOnly: true})

	rec := newJoinRecorder()
	h := awsTestRun("", "fake", joinRoleWorker, rec)
	h.instanceID = awsTestInstance

	client, err := newAWSClient(awsTestCred)
	if err != nil {
		t.Fatal(err)
	}

	h.client = client

	if err := h.waitRunning(context.Background()); err != nil || h.join.node != "172.31.5.20" {
		t.Fatalf("waitRunning = %v, node %q", err, h.join.node)
	}

	rec.OnDone("", "")

	_, _, events := rec.wait(t)
	if last := events[len(events)-1].Message; !strings.Contains(last, "private address 172.31.5.20") || !strings.Contains(last, "route") {
		t.Errorf("progress = %q", last)
	}
}

func TestAWSDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := AWSOptions(cfg, "", "")

	o := decodeJSON[awsOptions](t, out, err)
	if len(o.Types) == 0 || len(o.Subnets) == 0 || len(o.Images) != 3 || o.Images[0].Source != awsImageOfficial {
		t.Errorf("demo options = %+v", o)
	}

	// One sample AMI of the account's own is the cluster's image.
	matching := slices.IndexFunc(o.Images, func(i awsImage) bool {
		return i.TalosVersion == o.TemplateVersion && i.Schematic == o.TemplateSchematic
	})
	if o.TemplateVersion != demoTalosVersion || o.TemplateSchematic != demoBootSchematic || matching < 0 || o.Images[matching].Source != awsImageCustom {
		t.Errorf("demo template = %q %q, images %+v", o.TemplateVersion, o.TemplateSchematic, o.Images)
	}

	rec := newJoinRecorder()
	StartAWSCreate(cfg, "", awsTestCred, `{"instanceType":"t3.small","imageId":"ami-0123456789abcdef0","subnetId":"subnet-0123456789abcdef0","hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}
