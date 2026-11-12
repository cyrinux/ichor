package ichorgo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// S8 step 12: a Talos AMI for the cluster's schematic, made from the phone. The documented route
// (an S3 upload, import-snapshot and a vmimport role) needs a laptop and IAM pieces made by hand,
// so the run drives a throwaway helper instance instead: Amazon Linux with an empty data volume,
// whose user data downloads the Image Factory's aws image, writes it to that volume and powers
// off. The app then snapshots the volume, registers the AMI from the snapshot and terminates the
// helper (its volumes go with it). The phone moves no image bytes. Billable: one small instance
// for a few minutes, then the snapshot's storage. Every path ends in cleanup.

const (
	awsImageTimeout        = 45 * time.Minute
	awsImageStopWait       = 20 * time.Minute
	awsImageSnapshotWait   = 15 * time.Minute
	awsImageCleanupTimeout = 3 * time.Minute
	awsImageCleanupTries   = 10
	awsImageConsoleTries   = 3
	awsImageAuditAction    = "aws-ami-create"
	awsImageMinutes        = 10

	// awsImageVolumeGiB holds the unpacked aws-<arch>.raw image.
	awsImageVolumeGiB = 8
	awsImageDataDisk  = "/dev/sdf"
	awsImageRootDisk  = "/dev/xvda"

	awsImageTypeAMD64 = "t3.small"
	awsImageTypeARM64 = "t4g.small"

	awsStateStopped  = "stopped"
	awsStateStopping = "stopping"
	awsSnapCompleted = "completed"
	awsSnapError     = "error"

	awsSchematicShown = 12

	// Phases; creating, writing, snapshot and cleanup are the Hetzner snapshot's.
	awsPhaseCreating = cloudPhaseCreating
	awsPhaseWriting  = hetznerPhaseWriting
	awsPhaseSnapshot = hetznerPhaseSnapshot
	awsPhaseRegister = "register"
	awsPhaseCleanup  = cloudPhaseCleanup
)

var (
	// awsImageNamePattern is RegisterImage's: 3 to 128 of these characters.
	awsImageNamePattern = regexp.MustCompile(`^[A-Za-z0-9().\[\] /'@_-]{3,128}$`)

	errAWSNoImageRight = errors.New("these keys may not run the helper instance: add the actions of the printed policy to their IAM user or role")
)

// awsImageSpec is StartAWSImage's specJSON. The image URL is never in it: it comes from the
// template's schematic and version.
type awsImageSpec struct {
	TemplateNode    string `json:"templateNode"`
	Arch            string `json:"arch"`
	Region          string `json:"region"`
	SubnetID        string `json:"subnetId"`
	SecurityGroupID string `json:"securityGroupId"`
	InstanceType    string `json:"instanceType"`
	Name            string `json:"name"`
}

type awsImageResult struct {
	ImageID      string `json:"imageId"`
	SnapshotID   string `json:"snapshotId"`
	Name         string `json:"name"`
	Arch         string `json:"arch"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
	SizeGB       int    `json:"sizeGB"`
}

// StartAWSImage makes a Talos AMI for the template's version and schematic in the keys' region:
// creating → writing → snapshot → register → cleanup → done. specJSON: {"templateNode","arch",
// "region","subnetId","securityGroupId","instanceType","name"}; templateNode "" takes the first
// member that answers, arch "" the template's, region "" the keys', instanceType "" t3.small
// (amd64) or t4g.small (arm64), name "" talos-<version>-aws-<arch>-<schematic12>. The subnet
// must reach the Image Factory (a public address is asked for). credJSON: as AWSTest's. OnDone's
// first argument is the result JSON, not an address: {"imageId","snapshotId","name","arch",
// "talosVersion","schematic","sizeGB"}. Refused before any write: keys whose RunInstances dry run
// fails, a template whose schematic cannot be read, an architecture with no Amazon Linux AMI, a
// security group of another VPC than the subnet, a name already taken. Cancel, or any failure,
// terminates the helper and deletes an unfinished snapshot or AMI, and the message names them.
// Nothing of the cluster is written. Refused on the demo; recorded in the activity log as
// aws-ami-create. Billable: the app shows AWSImageEstimate first.
func StartAWSImage(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec awsImageSpec

	specErr := json.Unmarshal([]byte(specJSON), &spec)
	if specErr != nil {
		specErr = errors.New("invalid AMI request")
	}

	contextName, spec.TemplateNode = unmaskTarget(configYAML, contextName, strings.TrimSpace(spec.TemplateNode))

	listener = maskedJoinListener{listener}

	r := &awsImageRun{
		config: configYAML, context: contextName, credJSON: credJSON, spec: spec, specErr: specErr,
		listener: listener, poll: awsPollInterval, stopWait: awsImageStopWait, snapshotWait: awsImageSnapshotWait,
	}

	return r.start(awsImageTimeout)
}

// awsImageRun is one run of StartAWSImage.
type awsImageRun struct {
	config, context string
	credJSON        string
	spec            awsImageSpec
	specErr         error
	listener        JoinListener
	poll            time.Duration
	stopWait        time.Duration
	snapshotWait    time.Duration

	client       *awsClient
	target       bootTarget
	instanceType string
	helperAMI    string
	bootMode     string
	runID        string
	name         string
	result       awsImageResult

	// What this run created ("" before), and the calls that may have created it without an answer.
	instanceID, volumeID, snapshotID, imageID string
	runPosted, snapshotPosted, registerPosted bool
}

func (r *awsImageRun) start(timeout time.Duration) *JoinRun {
	run := func(ctx context.Context) error { return r.run(ctx, timeout) }
	done := func() (any, string) { return r.result, "AMI " + r.result.ImageID + " is ready" }

	return startCloudImageRun(r.config, r.context, r.listener, r.auditAction, timeout, run, done, r.emit)
}

func (r *awsImageRun) auditAction() auditAction {
	params := fmt.Sprintf("arch=%s region=%s version=%s", cmp.Or(r.target.arch, r.spec.Arch), r.spec.Region, r.target.version)
	if r.imageID != "" {
		params += " image=" + r.imageID
	}

	return auditAction{Action: awsImageAuditAction, Node: r.spec.TemplateNode, Params: params}
}

func (r *awsImageRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli()}, r.listener.OnProgress)
}

func (r *awsImageRun) run(ctx context.Context, timeout time.Duration) error {
	if r.specErr != nil {
		return r.specErr
	}

	if isTalosDemoContext(r.config, r.context) {
		return errDemoUnavailable
	}

	if err := r.prepare(ctx); err != nil {
		return err
	}

	err := r.make(ctx)

	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("the AMI was not ready within %s", timeout)
	case ctx.Err() != nil:
		err = errors.New("stopped")
	}

	return r.cleanup(err)
}

// prepare checks everything that can be refused before a write, the keys' right to run the
// helper last (a dry run with every parameter).
func (r *awsImageRun) prepare(ctx context.Context) error {
	arch, err := normalizeBootArch(r.spec.Arch)
	if err != nil {
		return err
	}

	if r.spec.SubnetID = strings.TrimSpace(r.spec.SubnetID); r.spec.SubnetID == "" {
		return errors.New("pick a subnet that reaches the internet")
	}

	if r.client, err = newAWSClient(r.credJSON); err != nil {
		return err
	}

	if r.spec.Region != "" {
		if err := r.client.setRegion(r.spec.Region); err != nil {
			return err
		}
	}

	r.spec.Region = r.client.region

	if _, err := r.client.sts(ctx); err != nil {
		return err
	}

	media, err := cloudImageMedia(r.config, r.context, r.spec.TemplateNode)
	if err != nil {
		return err
	}

	for _, warning := range media.Warnings {
		r.emit(awsPhaseCreating, warning)
	}

	r.target = media.target()
	r.target.secureBoot = false
	r.target.arch = cmp.Or(arch, r.target.arch)
	r.spec.TemplateNode = media.TemplateNode
	r.instanceType = cmp.Or(strings.TrimSpace(r.spec.InstanceType), awsImageType(r.target.arch))

	if err := r.checkNetwork(ctx); err != nil {
		return err
	}

	if err := r.checkName(ctx); err != nil {
		return err
	}

	if r.helperAMI, err = r.client.helperImage(ctx, r.target.arch); err != nil {
		return err
	}

	r.bootMode = r.client.officialBootMode(ctx, r.target)

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return err
	}

	r.runID = hex.EncodeToString(id)

	can, _, err := r.client.canRun(ctx, r.runParams())
	if err != nil {
		return err
	}

	if !can {
		return errAWSNoImageRight
	}

	return nil
}

// awsImageType is the helper's default type: the smallest burstable of the architecture.
func awsImageType(arch string) string {
	if arch == bootArchARM64 {
		return awsImageTypeARM64
	}

	return awsImageTypeAMD64
}

// checkNetwork refuses a subnet of another region and a security group of another VPC.
func (r *awsImageRun) checkNetwork(ctx context.Context) error {
	var subnets struct {
		Items []awsAPISubnet `xml:"subnetSet>item"`
	}

	if err := r.client.ec2(ctx, "DescribeSubnets", url.Values{"SubnetId.1": {r.spec.SubnetID}}, &subnets); err != nil {
		return err
	}

	if len(subnets.Items) == 0 {
		return fmt.Errorf("subnet %s is not in %s: pick another", r.spec.SubnetID, r.spec.Region)
	}

	if r.spec.SecurityGroupID = strings.TrimSpace(r.spec.SecurityGroupID); r.spec.SecurityGroupID == "" {
		return nil
	}

	var groups struct {
		Items []awsAPISecurityGroup `xml:"securityGroupInfo>item"`
	}

	if err := r.client.ec2(ctx, "DescribeSecurityGroups", url.Values{"GroupId.1": {r.spec.SecurityGroupID}}, &groups); err != nil {
		return err
	}

	if vpc := subnets.Items[0].VPCID; len(groups.Items) == 0 || groups.Items[0].VPCID != vpc {
		return fmt.Errorf("security group %s is not in the VPC of subnet %s: pick a group of %s", r.spec.SecurityGroupID, r.spec.SubnetID, vpc)
	}

	return nil
}

// checkName refuses a name RegisterImage would: an invalid one, or one the account already has
// in the region. A second AMI of the same version and schematic needs a name of its own.
func (r *awsImageRun) checkName(ctx context.Context) error {
	schematic := r.target.schematic
	r.name = strings.TrimSpace(r.spec.Name)

	if r.name == "" {
		r.name = fmt.Sprintf("talos-%s-aws-%s-%s", r.target.version, r.target.arch, schematic[:min(awsSchematicShown, len(schematic))])
	}

	if !awsImageNamePattern.MatchString(r.name) {
		return fmt.Errorf("%q is not an AMI name (3 to 128 letters, digits, spaces and ( ) . [ ] / ' @ _ -)", r.name)
	}

	var images struct {
		Items []awsAPIImage `xml:"imagesSet>item"`
	}

	params := url.Values{"Owner.1": {"self"}, "Filter.1.Name": {"name"}, "Filter.1.Value.1": {r.name}}
	if err := r.client.ec2(ctx, "DescribeImages", params, &images); err != nil {
		return err
	}

	if len(images.Items) > 0 && strings.TrimSpace(r.spec.Name) == "" {
		return fmt.Errorf("an AMI for this version and schematic exists: %s. Pick it, or give the new one a name", images.Items[0].ID)
	}

	if len(images.Items) > 0 {
		return fmt.Errorf("AMI %s is already named %q: pick another name", images.Items[0].ID, r.name)
	}

	return nil
}

// helperImage is the newest Amazon Linux 2023 AMI of the architecture in the region (no SSM
// parameter: it would need ssm:GetParameter).
func (c *awsClient) helperImage(ctx context.Context, arch string) (string, error) {
	awsArch := awsArchX86
	if arch == bootArchARM64 {
		awsArch = awsArchARM
	}

	params := url.Values{
		"Owner.1":       {"amazon"},
		"Filter.1.Name": {"name"}, "Filter.1.Value.1": {"al2023-ami-2023.*-kernel-*-" + awsArch},
		"Filter.2.Name": {"state"}, "Filter.2.Value.1": {"available"},
	}

	var images struct {
		Items []struct {
			ID      string `xml:"imageId"`
			Created string `xml:"creationDate"`
		} `xml:"imagesSet>item"`
	}

	if err := c.ec2(ctx, "DescribeImages", params, &images); err != nil {
		return "", err
	}

	if len(images.Items) == 0 {
		return "", fmt.Errorf("no Amazon Linux 2023 AMI for %s in %s to write the image with: pick another region", arch, c.region)
	}

	newest := slices.MaxFunc(images.Items, func(a, b struct {
		ID      string `xml:"imageId"`
		Created string `xml:"creationDate"`
	},
	) int {
		return strings.Compare(a.Created, b.Created)
	})

	return newest.ID, nil
}

// officialBootMode is the boot mode of the official Talos AMI of the same version and
// architecture in the region, so that the new AMI boots the same way; when that AMI cannot be
// read, uefi-preferred on amd64 and none (always UEFI) on arm64.
func (c *awsClient) officialBootMode(ctx context.Context, t bootTarget) string {
	fallback := ""
	if t.arch == bootArchAMD64 {
		fallback = "uefi-preferred"
	}

	official, err := talosAWSImages(ctx, t.version, c.region)
	if err != nil {
		return fallback
	}

	i := slices.IndexFunc(official, func(i awsImage) bool { return i.Arch == t.arch })
	if i < 0 {
		return fallback
	}

	var images struct {
		Items []struct {
			BootMode string `xml:"bootMode"`
		} `xml:"imagesSet>item"`
	}

	if c.ec2(ctx, "DescribeImages", url.Values{"ImageId.1": {official[i].ID}}, &images) != nil || len(images.Items) == 0 {
		return fallback
	}

	return images.Items[0].BootMode
}

// runParams is the helper's RunInstances: a public address (it downloads from the Image Factory),
// an empty data volume deleted with it, a shutdown that stops it (the volume stays for the
// snapshot), the tags on the instance and its volumes.
func (r *awsImageRun) runParams() url.Values {
	p := url.Values{
		"ImageId": {r.helperAMI}, "InstanceType": {r.instanceType}, "MinCount": {"1"}, "MaxCount": {"1"},
		"InstanceInitiatedShutdownBehavior":            {"stop"},
		"UserData":                                     {base64.StdEncoding.EncodeToString([]byte(awsImageScript(cloudImageURL(r.target, cloudPlatformAWS))))},
		"NetworkInterface.1.DeviceIndex":               {"0"},
		"NetworkInterface.1.SubnetId":                  {r.spec.SubnetID},
		"NetworkInterface.1.AssociatePublicIpAddress":  {"true"},
		"BlockDeviceMapping.1.DeviceName":              {awsImageDataDisk},
		"BlockDeviceMapping.1.Ebs.VolumeSize":          {strconv.Itoa(awsImageVolumeGiB)},
		"BlockDeviceMapping.1.Ebs.VolumeType":          {"gp3"},
		"BlockDeviceMapping.1.Ebs.DeleteOnTermination": {"true"},
	}

	if r.spec.SecurityGroupID != "" {
		p.Set("NetworkInterface.1.SecurityGroupId.1", r.spec.SecurityGroupID)
	}

	for i, kind := range []string{"instance", "volume"} {
		awsSetTags(p, "TagSpecification."+strconv.Itoa(i+1), kind, [][2]string{{"Name", "ichor-ami-" + r.runID}, {awsTagRun, r.runID}})
	}

	return p
}

// awsSetTags writes one TagSpecification.N of a create call.
func awsSetTags(p url.Values, prefix, resourceType string, tags [][2]string) {
	p.Set(prefix+".ResourceType", resourceType)

	for i, t := range tags {
		n := strconv.Itoa(i + 1)
		p.Set(prefix+".Tag."+n+".Key", t[0])
		p.Set(prefix+".Tag."+n+".Value", t[1])
	}
}

// make runs the steps up to the registered AMI; cleanup follows, whatever it returns.
func (r *awsImageRun) make(ctx context.Context) error {
	r.emit(awsPhaseCreating, "creating the throwaway instance ichor-ami-"+r.runID+" ("+r.instanceType+" in "+r.spec.Region+")")

	if err := r.createHelper(ctx); err != nil {
		return err
	}

	r.emit(awsPhaseWriting, "instance "+r.instanceID+" writes "+cloudImageURL(r.target, cloudPlatformAWS))

	if err := r.waitStopped(ctx); err != nil {
		return err
	}

	if err := r.checkConsole(ctx); err != nil {
		return err
	}

	r.emit(awsPhaseSnapshot, "taking the snapshot of volume "+r.volumeID)

	if err := r.createSnapshot(ctx); err != nil {
		return err
	}

	r.emit(awsPhaseRegister, "registering the AMI "+r.name)

	return r.register(ctx)
}

func (r *awsImageRun) createHelper(ctx context.Context) error {
	params := r.runParams()
	params.Set("ClientToken", awsClientTokenPrefix+r.runID)

	var resp struct {
		Instances []awsInstance `xml:"instancesSet>item"`
	}

	r.runPosted = true

	if err := r.client.ec2(ctx, "RunInstances", params, &resp); err != nil {
		return err
	}

	if len(resp.Instances) == 0 || resp.Instances[0].ID == "" {
		return errors.New("EC2 answered without an instance id")
	}

	r.instanceID = resp.Instances[0].ID

	return nil
}
