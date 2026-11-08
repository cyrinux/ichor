package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// S8 step 27: a network for the new node without a trip to the Google Cloud console. The project's
// default network (auto mode: one subnet named default per region) and two VPC firewall rules
// made once and reused, both aimed at the ichor-node network tag every node the app makes
// carries: ichor-nodes-internal (everything from the tagged nodes to the tagged nodes) and, for a
// plain Talos cluster only, ichor-phone-50000 (TCP 50000 from this phone's public IPv4 /32, the
// date in its description). Nothing is opened to the world, 6443 never; no network or subnet is
// made, and no rule is ever deleted by the app: they are kept, not a run's resources.

const (
	gcpDefaultNetwork      = "default"
	gcpRuleInternal        = "ichor-nodes-internal"
	gcpRulePhone           = "ichor-phone-50000"
	gcpTalosAPIPort        = "50000"
	gcpNetworkAuditAction  = "gcp-network-prepare"
	gcpDirectionIngress    = "INGRESS"
	gcpProtocolAll         = "all"
	gcpProtocolTCP         = "tcp"
	gcpFirewallPriority    = 1000
	gcpReasonAlreadyExists = "alreadyExists"
)

var errGCPNoDefaultNetwork = errors.New("this project has no default network: create one at Google Cloud " +
	"(VPC network → Create VPC network → automatic subnets), or pick a network and subnet yourself")

// gcpNetworkPermissions are what the guided network needs, checked before any write.
// compute.networks.updatePolicy is what lets the app add firewall rules, and only to the
// project's own networks.
func gcpNetworkPermissions() []string {
	return []string{
		"compute.firewalls.create", "compute.firewalls.get", "compute.firewalls.list", "compute.firewalls.update",
		"compute.networks.updatePolicy", "compute.networks.get",
	}
}

// gcpNetworkSpec is what GCPNetworkPrepare is asked.
type gcpNetworkSpec struct {
	Project string `json:"project"`
	Zone    string `json:"zone"`
	Role    string `json:"role"`
	Omni    bool   `json:"omni"`
}

type gcpNetworkResult struct {
	Network     string `json:"network"`
	NetworkLink string `json:"networkLink"`
	// Subnetwork is regions/<region>/subnetworks/<name>; SubnetworkLink its URL, as the options list it.
	Subnetwork     string `json:"subnetwork"`
	SubnetworkLink string `json:"subnetworkLink"`
	// PhoneIP is the address port 50000 is open to, "" on Omni or when it cannot be learned.
	PhoneIP  string           `json:"phoneIp"`
	Created  []string         `json:"created"`
	Updated  []string         `json:"updated"`
	Rules    []gcpNetworkRule `json:"rules"`
	Warnings []string         `json:"warnings"`
}

// gcpNetworkRule is one source of an ingress rule aimed at the ichor-node tag: protocol all or
// tcp, ports "" for every port, source a CIDR or "tag:<tag>" (self when it is ichor-node).
type gcpNetworkRule struct {
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Ports       string `json:"ports"`
	Source      string `json:"source"`
	Self        bool   `json:"self"`
	Description string `json:"description"`
}

// gcpAPIFirewall is a VPC firewall rule, as far as it is read.
type gcpAPIFirewall struct {
	Name         string   `json:"name"`
	Network      string   `json:"network"` // a URL
	Direction    string   `json:"direction"`
	Disabled     bool     `json:"disabled"`
	Description  string   `json:"description"`
	SourceRanges []string `json:"sourceRanges"`
	SourceTags   []string `json:"sourceTags"`
	TargetTags   []string `json:"targetTags"`
	Allowed      []struct {
		IPProtocol string   `json:"IPProtocol"`
		Ports      []string `json:"ports"`
	} `json:"allowed"`
}

// ingress: an enabled rule for traffic coming in (Google's default direction).
func (f gcpAPIFirewall) ingress() bool {
	return !f.Disabled && (f.Direction == "" || f.Direction == gcpDirectionIngress)
}

// reachesNode: the rule applies to a node the app makes (no target tag, or ichor-node).
func (f gcpAPIFirewall) reachesNode() bool {
	return len(f.TargetTags) == 0 || slices.Contains(f.TargetTags, gcpNetworkTag)
}

// wideOpen: the rule lets the whole internet reach a node's Talos API, every port or every
// protocol.
func (f gcpAPIFirewall) wideOpen() bool {
	if !f.ingress() || !f.reachesNode() || !slices.ContainsFunc(f.SourceRanges, func(r string) bool { return r == "0.0.0.0/0" || r == "::/0" }) {
		return false
	}

	for _, a := range f.Allowed {
		protocol := strings.ToLower(a.IPProtocol)
		if protocol == gcpProtocolAll || (protocol == gcpProtocolTCP && gcpPortsCover(a.Ports, 50000)) {
			return true
		}
	}

	return false
}

// gcpPortsCover: the ports ("22", "30000-60000"; none is every port) include port.
func gcpPortsCover(ports []string, port int) bool {
	if len(ports) == 0 {
		return true
	}

	for _, p := range ports {
		low, high, isRange := strings.Cut(p, "-")
		if !isRange {
			high = low
		}

		from, err1 := strconv.Atoi(strings.TrimSpace(low))
		to, err2 := strconv.Atoi(strings.TrimSpace(high))

		if err1 == nil && err2 == nil && from <= port && port <= to {
			return true
		}
	}

	return false
}

// GCPNetworkPrepare readies the default network of specJSON's project for a new node in its zone
// ({"project","zone","role":"controlplane|worker","omni":bool}): the region's subnet of the
// default network, the rule ichor-nodes-internal (every protocol from the ichor-node tag to it)
// and, unless the cluster is Omni, ichor-phone-50000 (TCP 50000 from this phone's public IPv4
// /32, dated; patched to add today's /32 when it has another). Idempotent: a second call makes
// nothing. Refused without the default network (the message says what to do at Google Cloud),
// and, naming them, without the firewall permissions, before any write. The demo answers samples
// and writes nothing. Recorded in the audit log. JSON: {"network","networkLink","subnetwork",
// "subnetworkLink","phoneIp","created","updated","rules":[{"name","protocol","ports","source",
// "self","description"}],"warnings"}.
func GCPNetworkPrepare(configYAML, contextName, credJSON, specJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoGCPNetwork())
	}

	var (
		spec   gcpNetworkSpec
		result gcpNetworkResult
	)

	audit := func() auditAction {
		params := "project=" + spec.Project + " zone=" + spec.Zone + " omni=" + strconv.FormatBool(spec.Omni)
		if result.Network != "" {
			params += " created=" + strings.Join(result.Created, ",") + " updated=" + strings.Join(result.Updated, ",")
		}

		return auditAction{Action: gcpNetworkAuditAction, Params: params}
	}

	err = recordedRun(configYAML, contextName, audit, func() error {
		c, err := gcpNetworkClient(credJSON, specJSON, &spec)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), gcpOptionsTimeout)
		defer cancel()

		result, err = c.prepareNetwork(ctx, spec, awsPhoneIPv4)

		return err
	})
	if err != nil {
		return "", err
	}

	return toJSON(result)
}

// gcpNetworkClient reads specJSON into spec (the key's project and zone by default) and makes the
// client.
func gcpNetworkClient(credJSON, specJSON string, spec *gcpNetworkSpec) (*gcpClient, error) {
	if json.Unmarshal([]byte(specJSON), spec) != nil {
		return nil, errors.New("invalid network request")
	}

	if spec.Role != "controlplane" && spec.Role != "worker" {
		return nil, fmt.Errorf("%q is not a node role (controlplane or worker)", spec.Role)
	}

	c, err := newGCPClient(credJSON)
	if err != nil {
		return nil, err
	}

	spec.Project = cmp.Or(strings.TrimSpace(spec.Project), c.project)
	spec.Zone = cmp.Or(strings.ToLower(strings.TrimSpace(spec.Zone)), c.zone)

	switch {
	case !gcpProjectPattern.MatchString(spec.Project):
		return nil, fmt.Errorf("%q is not a project ID", spec.Project)
	case !gcpZonePattern.MatchString(spec.Zone):
		return nil, fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", spec.Zone)
	}

	return c, nil
}

func demoGCPNetwork() gcpNetworkResult {
	const (
		region = "europe-west1"
		base   = "https://www.googleapis.com/compute/v1/projects/example-project"
		subnet = "regions/" + region + "/subnetworks/" + gcpDefaultNetwork
	)

	return gcpNetworkResult{
		Network: gcpDefaultNetwork, NetworkLink: base + "/global/networks/" + gcpDefaultNetwork,
		Subnetwork: subnet, SubnetworkLink: base + "/" + subnet, PhoneIP: "203.0.113.7",
		Created: []string{}, Updated: []string{},
		Rules: []gcpNetworkRule{
			{Name: gcpRuleInternal, Protocol: gcpProtocolAll, Source: "tag:" + gcpNetworkTag, Self: true, Description: "Ichor nodes to Ichor nodes"},
			{Name: gcpRulePhone, Protocol: gcpProtocolTCP, Ports: gcpTalosAPIPort, Source: "203.0.113.7/32", Description: "Ichor phone 2026-11-02"},
		},
		Warnings: []string{"Demo cluster: nothing was created."},
	}
}

// prepareNetwork is GCPNetworkPrepare once the key and the spec are known. phoneIP learns the
// phone's public IPv4.
func (c *gcpClient) prepareNetwork(ctx context.Context, spec gcpNetworkSpec, phoneIP func(context.Context) (string, error)) (gcpNetworkResult, error) {
	result := gcpNetworkResult{Created: []string{}, Updated: []string{}, Rules: []gcpNetworkRule{}, Warnings: []string{}}

	if err := c.defaultSubnet(ctx, spec, &result); err != nil {
		return result, err
	}

	if err := gcpCheckGranted(ctx, c, spec.Project, gcpNetworkPermissions()); err != nil {
		return result, err
	}

	firewalls, err := gcpList[gcpAPIFirewall](ctx, c, c.compute(spec.Project, "/global/firewalls"), "items")
	if err != nil {
		return result, err
	}

	byName := map[string]gcpAPIFirewall{}
	for _, f := range firewalls {
		byName[f.Name] = f
	}

	if err := c.prepareInternalRule(ctx, spec.Project, byName, &result); err != nil {
		return result, err
	}

	if !spec.Omni {
		if err := c.preparePhoneRule(ctx, spec.Project, byName, phoneIP, &result); err != nil {
			return result, err
		}
	}

	return c.withFirewallRules(ctx, spec.Project, result)
}

// defaultSubnet reads the default network and picks its subnet in the zone's region: refused
// when the project has no default network.
func (c *gcpClient) defaultSubnet(ctx context.Context, spec gcpNetworkSpec, result *gcpNetworkResult) error {
	var network gcpAPINetwork

	err := c.get(ctx, c.compute(spec.Project, "/global/networks/"+gcpDefaultNetwork), &network)

	var e *gcpError
	if errors.As(err, &e) && e.status == http.StatusNotFound {
		return errGCPNoDefaultNetwork
	}

	if err != nil {
		return err
	}

	region := gcpRegionOf(spec.Zone)
	inRegion := slices.DeleteFunc(slices.Clone(network.Subnetworks), func(u string) bool {
		return !strings.Contains(u, "/regions/"+region+"/subnetworks/")
	})

	if len(inRegion) == 0 {
		return fmt.Errorf("the default network has no subnet in %s: pick a network and subnet yourself", region)
	}

	link := inRegion[0]
	if i := slices.IndexFunc(inRegion, func(u string) bool { return lastSegment(u) == gcpDefaultNetwork }); i >= 0 {
		link = inRegion[i]
	}

	result.Network, result.NetworkLink = gcpDefaultNetwork, network.SelfLink
	result.Subnetwork, result.SubnetworkLink = "regions/"+region+"/subnetworks/"+lastSegment(link), link

	return nil
}

// prepareInternalRule makes ichor-nodes-internal on the default network when it is not there.
func (c *gcpClient) prepareInternalRule(ctx context.Context, project string, byName map[string]gcpAPIFirewall, result *gcpNetworkResult) error {
	if existing, ok := byName[gcpRuleInternal]; ok {
		if lastSegment(existing.Network) != gcpDefaultNetwork {
			result.Warnings = append(result.Warnings, "the rule "+gcpRuleInternal+" belongs to network "+lastSegment(existing.Network)+
				": the default network's own rules apply between the nodes")
		}

		return nil
	}

	body := gcpFirewallBody(gcpRuleInternal, "Ichor nodes to Ichor nodes")
	body["sourceTags"] = []string{gcpNetworkTag}
	body["allowed"] = []gcpJSON{{"IPProtocol": gcpProtocolAll}}

	created, err := c.insertFirewall(ctx, project, body)
	if created {
		result.Created = append(result.Created, gcpRuleInternal)
	}

	return err
}

// preparePhoneRule opens TCP 50000 to this phone's /32: the rule made, or today's /32 added to
// it. An address that cannot be learned is a warning: the node can still be made.
func (c *gcpClient) preparePhoneRule(ctx context.Context, project string, byName map[string]gcpAPIFirewall,
	phoneIP func(context.Context) (string, error), result *gcpNetworkResult,
) error {
	ip, err := phoneIP(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, "could not learn this phone's address: port 50000 is not opened; the follow may not reach the node")

		return nil
	}

	cidr, err := gcpPhoneCIDR(ip)
	if err != nil {
		return err
	}

	description := "Ichor phone " + awsNetworkNow().UTC().Format(time.DateOnly)
	existing, found := byName[gcpRulePhone]

	switch {
	case found && lastSegment(existing.Network) != gcpDefaultNetwork:
		result.Warnings = append(result.Warnings, "the rule "+gcpRulePhone+" belongs to network "+lastSegment(existing.Network)+
			": port 50000 is not opened on the default network")

		return nil
	case found && slices.Contains(existing.SourceRanges, cidr):
	case found:
		patch := gcpJSON{"sourceRanges": append(slices.Clone(existing.SourceRanges), cidr), "description": description}
		if err := c.writeFirewall(ctx, project, http.MethodPatch, "/global/firewalls/"+url.PathEscape(gcpRulePhone), patch); err != nil {
			return err
		}

		result.Updated = append(result.Updated, gcpRulePhone)
	default:
		body := gcpFirewallBody(gcpRulePhone, description)
		body["sourceRanges"] = []string{cidr}
		body["allowed"] = []gcpJSON{{"IPProtocol": gcpProtocolTCP, "ports": []string{gcpTalosAPIPort}}}

		created, err := c.insertFirewall(ctx, project, body)
		if err != nil {
			return err
		}

		if created {
			result.Created = append(result.Created, gcpRulePhone)
		}
	}

	result.PhoneIP = ip

	return nil
}

// gcpPhoneCIDR is ip's /32: never a wider range, whatever checkip answered.
func gcpPhoneCIDR(ip string) (string, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsPrivate() || addr.IsLoopback() {
		return "", errors.New("the phone's address is not a public IPv4: nothing is opened")
	}

	return netip.PrefixFrom(addr, 32).String(), nil
}

// gcpFirewallBody is an ingress rule of the default network aimed at the ichor-node tag.
func gcpFirewallBody(name, description string) gcpJSON {
	return gcpJSON{
		"name": name, "network": "global/networks/" + gcpDefaultNetwork, "direction": gcpDirectionIngress,
		"priority": gcpFirewallPriority, "targetTags": []string{gcpNetworkTag}, "description": description,
	}
}

// insertFirewall makes a rule; one made meanwhile is not an error (false: not made by this call).
func (c *gcpClient) insertFirewall(ctx context.Context, project string, body gcpJSON) (bool, error) {
	err := c.writeFirewall(ctx, project, http.MethodPost, "/global/firewalls", body)

	var e *gcpError
	if errors.As(err, &e) && (e.reason == gcpReasonAlreadyExists || e.status == http.StatusConflict) {
		return false, nil
	}

	return err == nil, err
}

// writeFirewall sends a firewall write and follows its global operation until it ends.
func (c *gcpClient) writeFirewall(ctx context.Context, project, method, path string, body gcpJSON) error {
	var op gcpOperation
	if _, err := c.do(ctx, method, c.compute(project, path), body, &op); err != nil {
		return err
	}

	return c.waitOperation(ctx, project, "", op.Name, gcpPollInterval)
}

// withFirewallRules reads the default network's rules aimed at the ichor-node tag as they are now,
// for the apps to show.
func (c *gcpClient) withFirewallRules(ctx context.Context, project string, result gcpNetworkResult) (gcpNetworkResult, error) {
	firewalls, err := gcpList[gcpAPIFirewall](ctx, c, c.compute(project, "/global/firewalls"), "items")
	if err != nil {
		return result, err
	}

	slices.SortFunc(firewalls, func(a, b gcpAPIFirewall) int { return strings.Compare(a.Name, b.Name) })

	for _, f := range firewalls {
		if lastSegment(f.Network) == gcpDefaultNetwork && f.ingress() && slices.Contains(f.TargetTags, gcpNetworkTag) {
			result.Rules = append(result.Rules, gcpFirewallRules(f)...)
		}
	}

	return result, nil
}

// gcpFirewallRules flattens a rule, one entry per protocol and source.
func gcpFirewallRules(f gcpAPIFirewall) []gcpNetworkRule {
	var out []gcpNetworkRule

	for _, a := range f.Allowed {
		base := gcpNetworkRule{Name: f.Name, Protocol: strings.ToLower(a.IPProtocol), Ports: strings.Join(a.Ports, ","), Description: f.Description}

		for _, tag := range f.SourceTags {
			r := base
			r.Source, r.Self = "tag:"+tag, tag == gcpNetworkTag
			out = append(out, r)
		}

		for _, cidr := range f.SourceRanges {
			r := base
			r.Source = cidr
			out = append(out, r)
		}
	}

	return out
}

// markFirewalls flags, from the project's firewall rules, the networks a rule opens to the world
// and those the guided network prepared; rules that cannot be read are a warning, and no flag.
func (c *gcpClient) markFirewalls(ctx context.Context, networks []gcpNetwork, warnings *[]string) {
	firewalls, err := gcpList[gcpAPIFirewall](ctx, c, c.compute(c.project, "/global/firewalls"), "items")
	if err != nil {
		*warnings = append(*warnings, "the firewall rules cannot be read ("+friendlyError(err)+"): a network open to the world is not flagged")

		return
	}

	for i := range networks {
		for _, f := range firewalls {
			if lastSegment(f.Network) != networks[i].Name {
				continue
			}

			networks[i].WideOpen = networks[i].WideOpen || f.wideOpen()
			networks[i].Managed = networks[i].Managed || f.Name == gcpRuleInternal
		}
	}
}
