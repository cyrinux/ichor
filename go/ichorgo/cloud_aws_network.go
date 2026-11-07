package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// S8 step 15: a network for the new node without a trip to the AWS console. In the region's
// default VPC: its first subnet that assigns a public IPv4, and a security group ichor-nodes the
// app makes once (tagged ichor-managed) and reuses, which lets the nodes reach each other and,
// for a plain Talos cluster, lets this phone reach the node's Talos API (TCP 50000) from its
// public address of the day. Nothing is ever opened to the world, no VPC or subnet is made, and
// the group is kept: it is not a run's resource, and no cleanup deletes it.

const (
	awsNetworkGroup       = "ichor-nodes"
	awsNetworkGroupDesc   = "Ichor: nodes of a Talos cluster"
	awsTagManaged         = "ichor-managed"
	awsTalosAPIPort       = 50000
	awsCheckIPTimeout     = 5 * time.Second
	awsCodeDuplicateRule  = "InvalidPermission.Duplicate"
	awsProtocolAll        = "-1"
	awsProtocolTCP        = "tcp"
	awsMaxPort            = 65535
	awsCheckIPLimit       = 64
	awsNetworkAuditAction = "aws-network-prepare"
)

var (
	// awsCheckIPEndpoint answers the caller's public IPv4 as plain text: AWS's own. Replaced in
	// tests.
	awsCheckIPEndpoint = func() string { return "https://checkip.amazonaws.com" }
	// awsNetworkNow dates the phone's rule. Replaced in tests.
	awsNetworkNow = time.Now

	errAWSNoDefaultVPC = errors.New("this account has no default VPC")
)

// awsNetworkSpec is what AWSNetworkPrepare is asked. Region "" is the keys' region. Both roles
// get the same rules: a control plane joined from the phone is reached through the cluster's
// existing endpoint, so 6443 is never opened.
type awsNetworkSpec struct {
	Region string `json:"region"`
	Role   string `json:"role"`
	Omni   bool   `json:"omni"`
}

// awsNetworkRule is one inbound rule of the group: protocol "all" or "tcp", port 0 for all,
// source a CIDR or a group id (self true when it is the group itself).
type awsNetworkRule struct {
	Protocol    string `json:"protocol"`
	Port        int    `json:"port"`
	Source      string `json:"source"`
	Self        bool   `json:"self"`
	Description string `json:"description,omitempty"`
}

type awsNetworkResult struct {
	SubnetID        string `json:"subnetId"`
	SecurityGroupID string `json:"securityGroupId"`
	VPCID           string `json:"vpcId"`
	Created         bool   `json:"created"`
	// PhoneIP is the address port 50000 was opened to, "" on Omni or when it cannot be learned:
	// the apps tell this phone's rule from older ones by it.
	PhoneIP  string           `json:"phoneIp,omitempty"`
	Rules    []awsNetworkRule `json:"rules"`
	Warnings []string         `json:"warnings"`
	// Credential is the credential JSON to seal again when the call renewed it, else "".
	Credential string `json:"credential,omitempty"`
}

// The inbound rules of a security group, as DescribeSecurityGroups answers them.
type awsAPIPermission struct {
	Protocol string          `xml:"ipProtocol"`
	FromPort int             `xml:"fromPort"`
	ToPort   int             `xml:"toPort"`
	Groups   []awsAPIGroupID `xml:"groups>item"`
	Ranges   []awsAPIRange   `xml:"ipRanges>item"`
	Ranges6  []awsAPIRange6  `xml:"ipv6Ranges>item"`
}

type awsAPIGroupID struct {
	ID string `xml:"groupId"`
}

type awsAPIRange struct {
	CIDR        string `xml:"cidrIp"`
	Description string `xml:"description"`
}

type awsAPIRange6 struct {
	CIDR string `xml:"cidrIpv6"`
}

// allPorts says the rule covers every port: protocol -1, or 0-65535.
func (p awsAPIPermission) allPorts() bool {
	return p.Protocol == awsProtocolAll || (p.FromPort <= 0 && p.ToPort >= awsMaxPort)
}

func (p awsAPIPermission) fromCIDR(cidr string) bool {
	return slices.ContainsFunc(p.Ranges, func(r awsAPIRange) bool { return r.CIDR == cidr })
}

func (p awsAPIPermission) fromGroup(id string) bool {
	return slices.ContainsFunc(p.Groups, func(g awsAPIGroupID) bool { return g.ID == id })
}

// awsWideOpen says a group lets the whole internet in on every port.
func awsWideOpen(perms []awsAPIPermission) bool {
	return slices.ContainsFunc(perms, func(p awsAPIPermission) bool {
		return p.allPorts() && (p.fromCIDR("0.0.0.0/0") || slices.ContainsFunc(p.Ranges6, func(r awsAPIRange6) bool { return r.CIDR == "::/0" }))
	})
}

// AWSNetworkPrepare finds or makes the network a new node can use in the default VPC of the
// spec's region ({"region","role":"controlplane|worker","omni":bool}): the first subnet that
// assigns a public IPv4 (else the first, with a warning), and the security group ichor-nodes,
// made once and tagged ichor-managed, allowing all traffic from itself and, unless the cluster is
// Omni, TCP 50000 from this phone's public IPv4 /32 (learned from checkip.amazonaws.com; when it
// cannot be learned the rule is skipped with a warning). Idempotent: a second call makes nothing
// and adds no rule it already has. Refused without a default VPC (the message says what to do at
// AWS) and in the demo. Recorded in the audit log: it writes at the provider. JSON: {"subnetId",
// "securityGroupId","vpcId","created","rules":[{"protocol","port","source","self",
// "description"}],"warnings","credential"}.
func AWSNetworkPrepare(configYAML, contextName, credJSON, specJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	var (
		spec   awsNetworkSpec
		result awsNetworkResult
	)

	audit := func() auditAction {
		params := "region=" + spec.Region + " role=" + spec.Role
		if result.SecurityGroupID != "" {
			params += " group=" + result.SecurityGroupID + " created=" + strconv.FormatBool(result.Created)
		}

		return auditAction{Action: awsNetworkAuditAction, Params: params}
	}

	err = recordedRun(configYAML, contextName, audit, func() error {
		if isTalosDemoContext(configYAML, contextName) {
			return errDemoUnavailable
		}

		if json.Unmarshal([]byte(specJSON), &spec) != nil {
			return errors.New("invalid network request")
		}

		if spec.Role != "controlplane" && spec.Role != "worker" {
			return fmt.Errorf("%q is not a node role (controlplane or worker)", spec.Role)
		}

		ctx, cancel := context.WithTimeout(context.Background(), awsOptionsTimeout)
		defer cancel()

		c, updated, err := newAWSClientOnline(ctx, credJSON)
		result.Credential = updated

		if err != nil {
			return err
		}

		if spec.Region != "" {
			if err := c.setRegion(spec.Region); err != nil {
				return err
			}
		}

		spec.Region = c.region

		prepared, err := c.prepareNetwork(ctx, spec, awsPhoneIPv4)
		prepared.Credential = updated
		result = prepared

		return err
	})
	if err != nil {
		return "", err
	}

	return toJSON(result)
}

// prepareNetwork is AWSNetworkPrepare once the keys and the region are known. phoneIP learns the
// phone's public IPv4.
func (c *awsClient) prepareNetwork(ctx context.Context, spec awsNetworkSpec, phoneIP func(context.Context) (string, error)) (awsNetworkResult, error) {
	result := awsNetworkResult{Rules: []awsNetworkRule{}, Warnings: []string{}}

	vpc, err := c.defaultVPC(ctx)
	if err != nil {
		return result, err
	}

	result.VPCID = vpc

	subnet, warning, err := c.publicSubnet(ctx, vpc)
	if err != nil {
		return result, err
	}

	result.SubnetID = subnet
	if warning != "" {
		result.Warnings = append(result.Warnings, warning)
	}

	group, err := c.findGroup(ctx, vpc)
	if err != nil {
		return result, err
	}

	if group == nil {
		id, err := c.createGroup(ctx, vpc)
		if err != nil {
			return result, err
		}

		result.Created = true
		group = &awsAPISecurityGroup{ID: id, VPCID: vpc, Tags: []awsTag{{Key: awsTagManaged, Value: "true"}}}
	}

	result.SecurityGroupID = group.ID

	if awsTagValue(group.Tags, awsTagManaged) != "true" {
		// Someone else's group with the app's name: used as it is, never changed.
		result.Warnings = append(result.Warnings, "a group named "+awsNetworkGroup+" exists that Ichor did not make: its rules are left as they are")

		return c.withRules(ctx, result)
	}

	if !slices.ContainsFunc(group.Permissions, func(p awsAPIPermission) bool { return p.Protocol == awsProtocolAll && p.fromGroup(group.ID) }) {
		params := url.Values{"GroupId": {group.ID}, "IpPermissions.1.IpProtocol": {awsProtocolAll}, "IpPermissions.1.Groups.1.GroupId": {group.ID}}
		if err := c.authorize(ctx, params); err != nil {
			return result, err
		}
	}

	if !spec.Omni {
		ip, warning := c.openTalosAPI(ctx, group, phoneIP)
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}

		result.PhoneIP = ip
	}

	return c.withRules(ctx, result)
}

// openTalosAPI adds TCP 50000 from the phone's /32, dated, unless the group has it, and says
// which address that is ("" when none). Trouble is a warning, not an error: the node can still
// be made, the follow may not reach it.
func (c *awsClient) openTalosAPI(ctx context.Context, group *awsAPISecurityGroup, phoneIP func(context.Context) (string, error)) (opened, warning string) {
	ip, err := phoneIP(ctx)
	if err != nil {
		return "", "could not learn this phone's address: port 50000 is not opened; the follow may not reach the node"
	}

	cidr := ip + "/32"

	if slices.ContainsFunc(group.Permissions, func(p awsAPIPermission) bool {
		return p.Protocol == awsProtocolTCP && p.FromPort <= awsTalosAPIPort && p.ToPort >= awsTalosAPIPort && p.fromCIDR(cidr)
	}) {
		return ip, ""
	}

	port := strconv.Itoa(awsTalosAPIPort)
	params := url.Values{
		"GroupId": {group.ID}, "IpPermissions.1.IpProtocol": {awsProtocolTCP},
		"IpPermissions.1.FromPort": {port}, "IpPermissions.1.ToPort": {port},
		"IpPermissions.1.IpRanges.1.CidrIp":      {cidr},
		"IpPermissions.1.IpRanges.1.Description": {"Ichor phone " + awsNetworkNow().UTC().Format(time.DateOnly)},
	}

	if err := c.authorize(ctx, params); err != nil {
		return "", "port 50000 could not be opened to this phone (" + friendlyError(err) + "): the follow may not reach the node"
	}

	return ip, ""
}

// authorize adds inbound rules; one the group already has is not an error.
func (c *awsClient) authorize(ctx context.Context, params url.Values) error {
	err := c.ec2(ctx, "AuthorizeSecurityGroupIngress", params, nil)

	var e *awsError
	if errors.As(err, &e) && e.code == awsCodeDuplicateRule {
		return nil
	}

	return err
}

// defaultVPC is the region's default VPC, refused with what to do when there is none.
func (c *awsClient) defaultVPC(ctx context.Context) (string, error) {
	var vpcs struct {
		Items []struct {
			ID string `xml:"vpcId"`
		} `xml:"vpcSet>item"`
	}

	params := url.Values{"Filter.1.Name": {"isDefault"}, "Filter.1.Value.1": {"true"}}
	if err := c.ec2(ctx, "DescribeVpcs", params, &vpcs); err != nil {
		return "", err
	}

	if len(vpcs.Items) == 0 {
		return "", fmt.Errorf("%w in %s: create one at AWS (VPC → Actions → Create default VPC), or pick a subnet and a security group yourself",
			errAWSNoDefaultVPC, c.region)
	}

	return vpcs.Items[0].ID, nil
}

// publicSubnet is the VPC's first subnet (by zone) that assigns a public IPv4, else its first
// with a warning.
func (c *awsClient) publicSubnet(ctx context.Context, vpc string) (id, warning string, err error) {
	var subnets struct {
		Items []awsAPISubnet `xml:"subnetSet>item"`
	}

	params := url.Values{"Filter.1.Name": {"vpc-id"}, "Filter.1.Value.1": {vpc}}
	if err := c.ec2(ctx, "DescribeSubnets", params, &subnets); err != nil {
		return "", "", err
	}

	if len(subnets.Items) == 0 {
		return "", "", fmt.Errorf("the default VPC of %s has no subnet: pick a subnet and a security group yourself", c.region)
	}

	slices.SortStableFunc(subnets.Items, func(a, b awsAPISubnet) int {
		return strings.Compare(a.AZ+a.ID, b.AZ+b.ID)
	})

	if i := slices.IndexFunc(subnets.Items, func(s awsAPISubnet) bool { return s.MapPubIP }); i >= 0 {
		return subnets.Items[i].ID, "", nil
	}

	return subnets.Items[0].ID, "no subnet assigns public IPs: the create will ask for one", nil
}

// findGroup is the VPC's ichor-nodes group, nil when there is none yet.
func (c *awsClient) findGroup(ctx context.Context, vpc string) (*awsAPISecurityGroup, error) {
	groups, err := c.describeGroups(ctx, url.Values{
		"Filter.1.Name": {"group-name"}, "Filter.1.Value.1": {awsNetworkGroup},
		"Filter.2.Name": {"vpc-id"}, "Filter.2.Value.1": {vpc},
	})
	if err != nil || len(groups) == 0 {
		return nil, err
	}

	return &groups[0], nil
}

func (c *awsClient) describeGroups(ctx context.Context, params url.Values) ([]awsAPISecurityGroup, error) {
	var groups struct {
		Items []awsAPISecurityGroup `xml:"securityGroupInfo>item"`
	}

	err := c.ec2(ctx, "DescribeSecurityGroups", params, &groups)

	return groups.Items, err
}

// createGroup makes the ichor-nodes group, tagged ichor-managed (the tag the policy's
// AuthorizeSecurityGroupIngress is conditioned on), with no rule yet.
func (c *awsClient) createGroup(ctx context.Context, vpc string) (string, error) {
	var created struct {
		ID string `xml:"groupId"`
	}

	params := url.Values{
		"GroupName": {awsNetworkGroup}, "GroupDescription": {awsNetworkGroupDesc}, "VpcId": {vpc},
		"TagSpecification.1.ResourceType": {"security-group"},
		"TagSpecification.1.Tag.1.Key":    {awsTagManaged}, "TagSpecification.1.Tag.1.Value": {"true"},
	}

	if err := c.ec2(ctx, "CreateSecurityGroup", params, &created); err != nil {
		return "", err
	}

	if created.ID == "" {
		return "", errors.New("AWS made the security group but did not say its id: pick it yourself")
	}

	return created.ID, nil
}

// withRules reads the group's rules as they are now, for the apps to show.
func (c *awsClient) withRules(ctx context.Context, result awsNetworkResult) (awsNetworkResult, error) {
	groups, err := c.describeGroups(ctx, url.Values{"GroupId.1": {result.SecurityGroupID}})
	if err != nil {
		return result, err
	}

	for _, g := range groups {
		result.Rules = append(result.Rules, awsNetworkRules(g)...)
	}

	return result, nil
}

// awsNetworkRules flattens a group's inbound rules, one per source.
func awsNetworkRules(g awsAPISecurityGroup) []awsNetworkRule {
	var out []awsNetworkRule

	for _, p := range g.Permissions {
		protocol, port := p.Protocol, p.FromPort
		if p.allPorts() {
			port = 0
		}

		if protocol == awsProtocolAll {
			protocol = "all"
		}

		for _, src := range p.Groups {
			out = append(out, awsNetworkRule{Protocol: protocol, Port: port, Source: src.ID, Self: src.ID == g.ID})
		}

		for _, r := range p.Ranges {
			out = append(out, awsNetworkRule{Protocol: protocol, Port: port, Source: r.CIDR, Description: r.Description})
		}

		for _, r := range p.Ranges6 {
			out = append(out, awsNetworkRule{Protocol: protocol, Port: port, Source: r.CIDR})
		}
	}

	return out
}

// awsPhoneIPv4 asks AWS's checkip for the phone's public IPv4, within awsCheckIPTimeout.
func awsPhoneIPv4(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, awsCheckIPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsCheckIPEndpoint(), nil)
	if err != nil {
		return "", err
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checkip answered %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, awsCheckIPLimit))
	if err != nil {
		return "", err
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(string(data)))
	if err != nil || !addr.Is4() || addr.IsPrivate() || addr.IsLoopback() {
		return "", errors.New("checkip did not answer a public IPv4")
	}

	return addr.String(), nil
}
