package ichorgo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Creating an AWS EC2 instance for a new node: the provider's half of cloudCreateRun.

const (
	// awsUserDataLimit is EC2's: 16 KB of user data before base64.
	awsUserDataLimit = 16 << 10

	awsPollInterval = 5 * time.Second

	awsStateRunning      = "running"
	awsStatePending      = "pending"
	awsCodeNotFoundYet   = "InvalidInstanceID.NotFound"
	awsClientTokenPrefix = "ichor-"
)

// awsCreateSpec is StartAWSCreate's specJSON.
type awsCreateSpec struct {
	Region            string   `json:"region"`
	InstanceType      string   `json:"instanceType"`
	ImageID           string   `json:"imageId"`
	SubnetID          string   `json:"subnetId"`
	SecurityGroupIDs  []string `json:"securityGroupIds"`
	AssociatePublicIP bool     `json:"associatePublicIp"`
	KeyName           string   `json:"keyName"`
	Hostname          string   `json:"hostname"`
	Role              string   `json:"role"`
	TemplateNode      string   `json:"templateNode"`
	InstallDisk       string   `json:"installDisk"`
	ExtraEdits        string   `json:"extraEditsJSON"`
	LabelsJSON        string   `json:"labelsJson"`
}

// awsInstance is the part of an instance the run reads.
type awsInstance struct {
	ID          string `xml:"instanceId"`
	State       string `xml:"instanceState>name"`
	StateReason string `xml:"stateReason>message"`
	PublicIP    string `xml:"ipAddress"`
	PrivateIP   string `xml:"privateIpAddress"`
	Devices     []struct {
		Name     string `xml:"deviceName"`
		VolumeID string `xml:"ebs>volumeId"`
	} `xml:"blockDeviceMapping>item"`
}

// awsInstances is DescribeInstances' answer.
type awsInstances struct {
	Reservations []struct {
		Instances []awsInstance `xml:"instancesSet>item"`
	} `xml:"reservationSet>item"`
}

func (a awsInstances) all() []awsInstance {
	var out []awsInstance
	for _, r := range a.Reservations {
		out = append(out, r.Instances...)
	}

	return out
}

// StartAWSCreate creates an AWS EC2 instance from a Talos AMI with the new node's user data, and
// follows it: creating → starting → (plain Talos) rebooting → booted → joining → registering →
// done, or (Omni) omni → done. specJSON: {"region","instanceType","imageId","subnetId",
// "securityGroupIds","associatePublicIp","keyName","hostname","role","templateNode",
// "installDisk","extraEditsJSON","labelsJson"} (region "" for the keys'). credJSON: as AWSTest's. Refused
// before any write: keys that may not run the instance (a RunInstances dry run with every
// parameter), an AMI of another architecture than the type, security groups of another VPC than
// the subnet, user data over 16 KB. The instance carries the tags Name, ichor-run and
// ichor-cluster (a hash of the context name) and a client token, which finds it when the create
// gets no clear answer. OnDone gets the node's address to add to the context ("" on Omni). A
// failed or cancelled run terminates the one instance it created (see cloudCreateRun for the
// exception). Refused on the demo and in privacy mode; recorded in the activity log as
// aws-instance-create, never with the config or the keys. Billable: the app confirms the cost
// and the hostname first.
func StartAWSCreate(configYAML, contextName, credJSON, specJSON string, listener JoinListener) *JoinRun {
	var spec awsCreateSpec

	join, template, specErr := newCloudJoin(configYAML, contextName, specJSON, &spec, "instance", listener)
	spec.TemplateNode = template

	h := &awsCreate{
		join: join,
		spec: spec, specErr: specErr, credJSON: credJSON, poll: awsPollInterval,
	}

	return h.start(cloudCreateTimeout)
}

// awsCreate is one run of StartAWSCreate.
type awsCreate struct {
	join       *nodeJoin
	spec       awsCreateSpec
	specErr    error
	credJSON   string
	poll       time.Duration
	client     *awsClient
	omni       bool
	runID      string
	instanceID string // the instance this run created, "" before
	// omniHostnames lists the machines' host names (the Omni API; replaced in tests).
	omniHostnames func(context.Context, *session) ([]string, error)
}

func (h *awsCreate) start(timeout time.Duration) *JoinRun {
	run := &cloudCreateRun{provider: h, join: h.join, specErr: h.specErr, poll: h.poll, omni: &h.omni, omniHostnames: h.omniHostnames}

	return run.start(timeout)
}

func (h *awsCreate) label() string { return "AWS (region " + h.spec.Region + ")" }

func (h *awsCreate) machineWord() string { return "instance" }

func (h *awsCreate) machineID() string { return h.instanceID }

func (h *awsCreate) auditAction() auditAction {
	params := fmt.Sprintf("hostname=%s role=%s type=%s region=%s", strings.TrimSpace(h.spec.Hostname), strings.TrimSpace(h.spec.Role), h.spec.InstanceType, h.spec.Region)
	if h.instanceID != "" {
		params += " instance=" + h.instanceID
	}

	return auditAction{Action: "aws-instance-create", Node: h.join.node, Params: params}
}

func (h *awsCreate) clientToken() string { return awsClientTokenPrefix + h.runID }

// prepare checks everything that can be refused before a write and makes the user data.
func (h *awsCreate) prepare(ctx context.Context, omni bool) (string, error) {
	h.spec.Hostname = strings.TrimSpace(h.spec.Hostname)
	h.join.req.hostname = h.spec.Hostname

	if !validHostname(h.spec.Hostname) || strings.Contains(h.spec.Hostname, ".") {
		return "", fmt.Errorf("%q is not a host name (letters, digits and dashes)", h.spec.Hostname)
	}

	if h.spec.InstanceType == "" || h.spec.ImageID == "" || h.spec.SubnetID == "" {
		return "", errors.New("pick an instance type, an AMI and a subnet")
	}

	client, err := newAWSClient(h.credJSON)
	if err != nil {
		return "", err
	}

	if h.spec.Region != "" {
		if err := client.setRegion(h.spec.Region); err != nil {
			return "", err
		}
	}

	h.client, h.spec.Region = client, client.region

	if _, err := client.sts(ctx); err != nil {
		return "", err
	}

	if err := h.checkChoice(ctx); err != nil {
		return "", err
	}

	data, err := cloudNodeUserData(h.join, omni, h.join.req, h.spec.ExtraEdits)
	if err != nil {
		return "", err
	}

	if data.Size > awsUserDataLimit {
		return "", fmt.Errorf("the user data is %d bytes, over the 16 KB EC2 takes: remove extra edits, or the config's docs and examples on the template", data.Size)
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}

	h.runID = hex.EncodeToString(id)

	can, _, err := client.canRun(ctx, h.runParams(data.UserData))
	if err != nil {
		return "", err
	}

	if !can {
		return "", errAWSNoCreate
	}

	return data.UserData, nil
}

// checkChoice refuses an AMI of another architecture than the instance type, and security
// groups of another VPC than the subnet.
func (h *awsCreate) checkChoice(ctx context.Context) error {
	var images struct {
		Items []awsAPIImage `xml:"imagesSet>item"`
	}

	if err := h.client.ec2(ctx, "DescribeImages", url.Values{"ImageId.1": {h.spec.ImageID}}, &images); err != nil {
		return err
	}

	if len(images.Items) == 0 {
		return fmt.Errorf("AMI %s is not in %s: pick another", h.spec.ImageID, h.spec.Region)
	}

	var types struct {
		Items []awsAPIType `xml:"instanceTypeSet>item"`
	}

	if err := h.client.ec2(ctx, "DescribeInstanceTypes", url.Values{"InstanceType.1": {h.spec.InstanceType}}, &types); err != nil {
		return err
	}

	if len(types.Items) == 0 {
		return fmt.Errorf("no instance type %s in %s", h.spec.InstanceType, h.spec.Region)
	}

	if arch := images.Items[0].Arch; !slices.Contains(types.Items[0].Archs, arch) {
		return fmt.Errorf("this AMI is %s, the type is %s: pick another", arch, strings.Join(types.Items[0].Archs, ", "))
	}

	var subnets struct {
		Items []awsAPISubnet `xml:"subnetSet>item"`
	}

	if err := h.client.ec2(ctx, "DescribeSubnets", url.Values{"SubnetId.1": {h.spec.SubnetID}}, &subnets); err != nil {
		return err
	}

	if len(subnets.Items) == 0 {
		return fmt.Errorf("subnet %s is not in %s: pick another", h.spec.SubnetID, h.spec.Region)
	}

	if len(h.spec.SecurityGroupIDs) == 0 {
		return nil
	}

	params := url.Values{}
	for i, id := range h.spec.SecurityGroupIDs {
		params.Set("GroupId."+strconv.Itoa(i+1), id)
	}

	var groups struct {
		Items []awsAPISecurityGroup `xml:"securityGroupInfo>item"`
	}

	if err := h.client.ec2(ctx, "DescribeSecurityGroups", params, &groups); err != nil {
		return err
	}

	vpc := subnets.Items[0].VPCID

	for _, g := range groups.Items {
		if g.VPCID != vpc {
			return fmt.Errorf("security group %s is in another VPC than subnet %s: pick groups of %s", g.ID, h.spec.SubnetID, vpc)
		}
	}

	return nil
}

// runParams is RunInstances' request. A public address needs the network interface form, which
// cannot be combined with a top-level subnet.
func (h *awsCreate) runParams(userData string) url.Values {
	p := url.Values{
		"ImageId": {h.spec.ImageID}, "InstanceType": {h.spec.InstanceType}, "MinCount": {"1"}, "MaxCount": {"1"},
		"UserData":                        {base64.StdEncoding.EncodeToString([]byte(userData))},
		"TagSpecification.1.ResourceType": {"instance"},
	}

	tags := [][2]string{{"Name", h.spec.Hostname}, {awsTagRun, h.runID}, {awsTagCluster, hetznerClusterLabel(h.join.context)}}
	for i, t := range tags {
		n := strconv.Itoa(i + 1)
		p.Set("TagSpecification.1.Tag."+n+".Key", t[0])
		p.Set("TagSpecification.1.Tag."+n+".Value", t[1])
	}

	groups, subnet := "SecurityGroupId.", "SubnetId"
	if h.spec.AssociatePublicIP {
		groups, subnet = "NetworkInterface.1.SecurityGroupId.", "NetworkInterface.1.SubnetId"
		p.Set("NetworkInterface.1.DeviceIndex", "0")
		p.Set("NetworkInterface.1.AssociatePublicIpAddress", "true")
	}

	p.Set(subnet, h.spec.SubnetID)

	for i, id := range h.spec.SecurityGroupIDs {
		p.Set(groups+strconv.Itoa(i+1), id)
	}

	if h.spec.KeyName != "" {
		p.Set("KeyName", h.spec.KeyName)
	}

	return p
}

// create sends RunInstances, once, with the run's client token: an instance made without a
// clear answer is found by it afterwards (cleanup).
func (h *awsCreate) create(ctx context.Context, userData string) error {
	params := h.runParams(userData)
	params.Set("ClientToken", h.clientToken())

	h.join.emit(cloudPhaseCreating, "creating "+h.spec.Hostname+" ("+h.spec.InstanceType+" in "+h.spec.Region+")")

	var resp struct {
		Instances []awsInstance `xml:"instancesSet>item"`
	}

	if err := h.client.ec2(ctx, "RunInstances", params, &resp); err != nil {
		return err
	}

	if len(resp.Instances) == 0 || resp.Instances[0].ID == "" {
		return errors.New("EC2 answered without an instance id")
	}

	h.instanceID = resp.Instances[0].ID
	h.join.applied = true

	return nil
}

// waitRunning follows the instance until it runs with an address: its public IPv4, else its
// private one. A provider hiccup is polled through, as is an instance EC2 does not list yet:
// only an instance going away or the run's end stops it.
func (h *awsCreate) waitRunning(ctx context.Context) error {
	h.join.emit(cloudPhaseStarting, "waiting for instance "+h.instanceID+" to start")

	last := ""

	for {
		var answer awsInstances

		err := h.client.ec2(ctx, "DescribeInstances", url.Values{"InstanceId.1": {h.instanceID}}, &answer)

		var e *awsError

		all := answer.all()

		switch {
		case err != nil && !h.transient(err) && (!errors.As(err, &e) || e.code != awsCodeNotFoundYet):
			return err
		case err == nil && len(all) == 1 && all[0].State != awsStatePending && all[0].State != awsStateRunning:
			return fmt.Errorf("instance %s is %s: %s", h.instanceID, all[0].State, cmpOr(all[0].StateReason, "EC2 gave no reason"))
		case err == nil && len(all) == 1 && all[0].State == awsStateRunning && (all[0].PublicIP != "" || all[0].PrivateIP != ""):
			h.reachedRunning(all[0])

			return nil
		case err == nil && len(all) == 1 && all[0].State != last:
			last = all[0].State
			h.join.emit(cloudPhaseStarting, "instance "+h.instanceID+": "+last)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.poll):
		}
	}
}

func (h *awsCreate) reachedRunning(i awsInstance) {
	if i.PublicIP != "" {
		h.join.node = normalizeEndpoint(i.PublicIP)
		h.join.emit(cloudPhaseStarting, "instance "+h.instanceID+" is running at "+h.join.node)

		return
	}

	h.join.node = normalizeEndpoint(i.PrivateIP)
	h.join.emit(cloudPhaseStarting, "instance "+h.instanceID+" is running with no public address: following it at its private address "+
		h.join.node+", which needs a route from this device (a VPN into the VPC)")
}

// transient: worth trying again (rate limit, a server error, no answer).
func (h *awsCreate) transient(err error) bool { return awsTransient(err) }

func awsTransient(err error) bool {
	var e *awsError
	if errors.As(err, &e) {
		switch e.code {
		case "RequestLimitExceeded", "Throttling", "InternalError", "Unavailable":
			return true
		}

		return e.status >= http.StatusInternalServerError
	}

	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// findUnanswered looks for the instance of a RunInstances with no clear answer, by the run's
// client token. A refusal (4xx) created nothing, and nothing is looked up.
func (h *awsCreate) findUnanswered(ctx context.Context, why error) {
	var e *awsError
	if h.runID == "" || h.client == nil || (errors.As(why, &e) && e.status < http.StatusInternalServerError && !h.transient(why)) {
		return
	}

	var answer awsInstances

	params := url.Values{"Filter.1.Name": {"client-token"}, "Filter.1.Value.1": {h.clientToken()}}
	if err := h.client.ec2(ctx, "DescribeInstances", params, &answer); err != nil {
		return
	}

	live := slices.DeleteFunc(answer.all(), func(i awsInstance) bool { return i.State == "terminated" })
	if len(live) == 1 {
		h.instanceID = live[0].ID
	}
}

func (h *awsCreate) deleteMachine(ctx context.Context) error {
	return h.client.ec2(ctx, "TerminateInstances", url.Values{"InstanceId.1": {h.instanceID}}, nil)
}
