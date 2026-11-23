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
)

// S8 step 31: a security group for the new node without a trip to the Scaleway console. The
// project's default group lets everything in, so the app makes one group ichor-nodes per zone
// (tagged ichor-managed, stateful, inbound dropped by default) and reuses it: every protocol from
// each cluster member's public IPv4 (a Scaleway group cannot name itself as a source, so the
// members are listed) and, for a plain Talos cluster, TCP 50000 from this phone's public address
// of the day. Nothing is ever opened to the world, and the group is kept: it is not a run's
// resource, and no cleanup deletes it. Rules for members that left are not removed.

const (
	scwNetworkGroup       = "ichor-nodes"
	scwNetworkGroupDesc   = "Ichor: nodes of a Talos cluster"
	scwTagManaged         = "ichor-managed"
	scwNetworkAuditAction = "scaleway-network-prepare"
	scwProtocolAny        = "ANY"
	scwProtocolTCP        = "TCP"
	scwPolicyDrop         = "drop"
	scwPolicyAccept       = "accept"
	scwDirectionInbound   = "inbound"
)

var (
	// scwNetworkMembers lists the cluster's members through the session (replaced in tests).
	scwNetworkMembers = clusterMembersOf

	errScwNoNetwork = errors.New("this API key may not manage security groups: attach the printed policy to its application")
)

// scwNetworkSpec is what ScalewayNetworkPrepare is asked. Zone "" is the key's zone. Both roles
// get the same rules: a control plane joined from the phone is reached through the cluster's
// existing endpoint, so 6443 is never opened.
type scwNetworkSpec struct {
	Zone string `json:"zone"`
	Role string `json:"role"`
	Omni bool   `json:"omni"`
}

// scwNetworkRule is one inbound accept rule of the group: protocol "all" or "tcp" (or "udp",
// "icmp" on a rule added by hand), port 0 for every port, source a CIDR. Description is the
// member's hostname, "this phone", or "" for a rule the app cannot name (Scaleway rules carry no
// description).
type scwNetworkRule struct {
	Protocol    string `json:"protocol"`
	Port        int    `json:"port"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
}

type scwNetworkResult struct {
	SecurityGroupID string `json:"securityGroupId"`
	Name            string `json:"name"`
	Created         bool   `json:"created"`
	// PhoneIP is the address port 50000 was opened to, "" on Omni or when it cannot be learned:
	// the apps tell this phone's rule from older ones by it.
	PhoneIP  string           `json:"phoneIp,omitempty"`
	Rules    []scwNetworkRule `json:"rules"`
	Warnings []string         `json:"warnings"`
}

// The API's objects, as far as they are read.
type scwAPISecurityGroup struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Project              string   `json:"project"`
	Tags                 []string `json:"tags"`
	ProjectDefault       bool     `json:"project_default"`
	InboundDefaultPolicy string   `json:"inbound_default_policy"`
}

func (g scwAPISecurityGroup) managed() bool {
	return slices.Contains(g.Tags, scwTagManaged)
}

type scwAPIRule struct {
	Protocol     string  `json:"protocol"`
	Direction    string  `json:"direction"`
	Action       string  `json:"action"`
	IPRange      string  `json:"ip_range"`
	DestPortFrom *uint32 `json:"dest_port_from"`
	DestPortTo   *uint32 `json:"dest_port_to"`
}

// source is the rule's range as a prefix: the API answers "1.2.3.4" or "1.2.3.4/32".
func (r scwAPIRule) source() string {
	if p, err := netip.ParsePrefix(r.IPRange); err == nil {
		return p.Masked().String()
	}

	if a, err := netip.ParseAddr(r.IPRange); err == nil {
		return netip.PrefixFrom(a, a.BitLen()).String()
	}

	return r.IPRange
}

func (r scwAPIRule) inboundAccept() bool {
	return strings.EqualFold(r.Direction, scwDirectionInbound) && strings.EqualFold(r.Action, scwPolicyAccept)
}

// allPorts says the rule covers every port of its protocol.
func (r scwAPIRule) allPorts() bool {
	return r.DestPortFrom == nil || (*r.DestPortFrom <= 1 && r.DestPortTo != nil && *r.DestPortTo >= awsMaxPort)
}

// covers says the rule lets protocol in from cidr on port (0: every port).
func (r scwAPIRule) covers(protocol, cidr string, port int) bool {
	if !r.inboundAccept() || r.source() != cidr {
		return false
	}

	if !strings.EqualFold(r.Protocol, scwProtocolAny) && (port == 0 || !strings.EqualFold(r.Protocol, protocol)) {
		return false
	}

	if r.allPorts() {
		return true
	}

	from, to := int(*r.DestPortFrom), int(*r.DestPortFrom)
	if r.DestPortTo != nil {
		to = int(*r.DestPortTo)
	}

	return port != 0 && from <= port && port <= to
}

// scwMemberSource is a member's address the group lets in, with the member's hostname.
type scwMemberSource struct {
	cidr     string
	hostname string
}

// ScalewayNetworkPrepare finds or makes, in the key's project and the spec's zone
// ({"zone","role":"controlplane|worker","omni":bool}), the security group ichor-nodes (tagged
// ichor-managed, stateful, inbound dropped by default, not the project's default) and adds the
// inbound rules it lacks: every protocol from each cluster member's public IPv4 /32 and, unless
// the cluster is Omni, TCP 50000 from this phone's public IPv4 /32 (when it cannot be learned the
// rule is skipped with a warning). Idempotent: a second call makes nothing and adds no rule it
// already has; nothing is ever deleted. Refused in the demo and when no member has a public IPv4
// (the group would cut the new node off). Recorded in the audit log: it writes at the provider.
// JSON: {"securityGroupId","name","created","phoneIp","rules":[{"protocol","port","source",
// "description"}],"warnings"}.
func ScalewayNetworkPrepare(configYAML, contextName, credJSON, specJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	var (
		spec   scwNetworkSpec
		result scwNetworkResult
	)

	audit := func() auditAction {
		params := "zone=" + spec.Zone + " role=" + spec.Role
		if result.SecurityGroupID != "" {
			params += " group=" + result.SecurityGroupID + " created=" + strconv.FormatBool(result.Created)
		}

		return auditAction{Action: scwNetworkAuditAction, Params: params}
	}

	err = recordedRun(configYAML, contextName, audit, func() error {
		c, err := scwNetworkClient(configYAML, contextName, credJSON, specJSON, &spec)
		if err != nil {
			return err
		}

		members, err := scwNetworkMembers(configYAML, contextName)
		if err != nil {
			return fmt.Errorf("could not list the cluster's members, whose addresses the group lets in: %s", friendlyError(err))
		}

		ctx, cancel := context.WithTimeout(context.Background(), scwOptionsTimeout)
		defer cancel()

		result, err = c.prepareNetwork(ctx, spec, members, awsPhoneIPv4)
		if scwStatus(err) == http.StatusForbidden {
			return errScwNoNetwork
		}

		return err
	})
	if err != nil {
		return "", err
	}

	return toJSON(result)
}

// scwNetworkClient checks the request and opens the key in the spec's zone; spec gets the zone
// the call runs in.
func scwNetworkClient(configYAML, contextName, credJSON, specJSON string, spec *scwNetworkSpec) (*scwClient, error) {
	if isTalosDemoContext(configYAML, contextName) {
		return nil, errDemoUnavailable
	}

	if json.Unmarshal([]byte(specJSON), spec) != nil {
		return nil, errors.New("invalid network request")
	}

	if spec.Role != joinRoleControlPlane && spec.Role != joinRoleWorker {
		return nil, fmt.Errorf("%q is not a node role (controlplane or worker)", spec.Role)
	}

	c, err := newScwClient(credJSON)
	if err != nil {
		return nil, err
	}

	if spec.Zone != "" {
		if err := c.setZone(spec.Zone); err != nil {
			return nil, err
		}
	}

	spec.Zone = c.zone

	return c, nil
}

// clusterMembersOf lists the cluster's members (Talos cluster discovery) through the context.
func clusterMembersOf(configYAML, contextName string) ([]clusterMember, error) {
	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) ([]clusterMember, error) {
		return listMembers(ctx, s.client, targetNodes(s.context))
	})
}

// scwMemberSources is each member's public IPv4 /32, deduplicated, and a warning per member that
// has none: its private or overlay addresses are not what a new node sees it come from.
func scwMemberSources(members []clusterMember) (sources []scwMemberSource, warnings []string) {
	seen := map[string]bool{}

	for _, m := range members {
		var private []string

		public := false

		for _, a := range m.addresses {
			a = a.Unmap()

			switch {
			case !a.Is4() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified():
			case a.IsPrivate() || cgnat.Contains(a):
				private = append(private, a.String())
			default:
				public = true

				cidr := netip.PrefixFrom(a, 32).String()
				if !seen[cidr] {
					seen[cidr] = true
					sources = append(sources, scwMemberSource{cidr: cidr, hostname: m.hostname})
				}
			}
		}

		if !public {
			name := cmp.Or(m.hostname, strings.Join(private, ", "), "a member")
			warnings = append(warnings, "the member "+name+" has no public IPv4 (only "+cmp.Or(strings.Join(private, ", "), "IPv6 or none")+
				"): the new node will not see it as a source, so the group does not let it in")
		}
	}

	slices.SortFunc(sources, func(a, b scwMemberSource) int { return strings.Compare(a.cidr, b.cidr) })

	return sources, warnings
}

// prepareNetwork is ScalewayNetworkPrepare once the key, the zone and the members are known.
// phoneIP learns the phone's public IPv4.
func (c *scwClient) prepareNetwork(ctx context.Context, spec scwNetworkSpec, members []clusterMember,
	phoneIP func(context.Context) (string, error),
) (scwNetworkResult, error) {
	result := scwNetworkResult{Name: scwNetworkGroup, Rules: []scwNetworkRule{}, Warnings: []string{}}

	sources, warnings := scwMemberSources(members)
	result.Warnings = append(result.Warnings, warnings...)

	if len(sources) == 0 {
		return result, errors.New("no member of the cluster has a public IPv4: a group that drops the rest would cut the new node off; pick a security group yourself")
	}

	group, err := c.findNetworkGroup(ctx)
	if err != nil {
		return result, err
	}

	if group == nil {
		if group, err = c.createNetworkGroup(ctx); err != nil {
			return result, err
		}

		result.Created = true
	}

	result.SecurityGroupID = group.ID

	rules, err := c.groupRules(ctx, group.ID)
	if err != nil {
		return result, err
	}

	if !group.managed() {
		// Someone else's group with the app's name: used as it is, never changed.
		result.Warnings = append(result.Warnings, "a group named "+scwNetworkGroup+" exists that Ichor did not make: its rules are left as they are")

		return c.withNetworkRules(ctx, result, sources, rules)
	}

	if !result.Created && !strings.EqualFold(group.InboundDefaultPolicy, scwPolicyDrop) {
		result.Warnings = append(result.Warnings, "the group "+scwNetworkGroup+" lets in by default what no rule drops: it is left as it is; set its inbound policy to drop at Scaleway")
	}

	for _, s := range sources {
		if slices.ContainsFunc(rules, func(r scwAPIRule) bool { return r.covers(scwProtocolAny, s.cidr, 0) }) {
			continue
		}

		if err := c.addRule(ctx, group.ID, scwProtocolAny, s.cidr, 0); err != nil {
			return result, err
		}
	}

	if !spec.Omni {
		ip, warning := c.openTalosAPI(ctx, group.ID, rules, phoneIP)
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}

		result.PhoneIP = ip
	}

	return c.withNetworkRules(ctx, result, sources, nil)
}

// openTalosAPI adds TCP 50000 from the phone's /32 unless the group has it, and says which
// address that is ("" when none). An unknown address is a warning, never a wider rule.
func (c *scwClient) openTalosAPI(ctx context.Context, group string, rules []scwAPIRule, phoneIP func(context.Context) (string, error)) (opened, warning string) {
	ip, err := phoneIP(ctx)
	if err != nil {
		return "", "could not learn this phone's address: port 50000 is not opened; the follow may not reach the node"
	}

	cidr := ip + "/32"

	if slices.ContainsFunc(rules, func(r scwAPIRule) bool { return r.covers(scwProtocolTCP, cidr, awsTalosAPIPort) }) {
		return ip, ""
	}

	if err := c.addRule(ctx, group, scwProtocolTCP, cidr, awsTalosAPIPort); err != nil {
		if scwStatus(err) == http.StatusForbidden {
			return "", errScwNoNetwork.Error()
		}

		return "", "port 50000 could not be opened to this phone (" + friendlyError(err) + "): the follow may not reach the node"
	}

	return ip, ""
}

// findNetworkGroup is the project's ichor-nodes group in the zone, nil when there is none yet. The
// name filter matches parts of names, so the name is compared again.
func (c *scwClient) findNetworkGroup(ctx context.Context) (*scwAPISecurityGroup, error) {
	var found *scwAPISecurityGroup

	path := c.path("/security_groups?name=" + url.QueryEscape(scwNetworkGroup) + "&" + c.projectQuery())

	err := scwEachPage(ctx, c, path, func(r struct {
		SecurityGroups []scwAPISecurityGroup `json:"security_groups"`
	}) int {
		for _, g := range r.SecurityGroups {
			if g.Name == scwNetworkGroup && found == nil {
				found = &g
			}
		}

		return len(r.SecurityGroups)
	})

	return found, err
}

// createNetworkGroup makes the ichor-nodes group with no rule yet: stateful (answers go out),
// inbound dropped unless a rule lets it in, outbound open, and never the project's default.
func (c *scwClient) createNetworkGroup(ctx context.Context) (*scwAPISecurityGroup, error) {
	var created struct {
		SecurityGroup scwAPISecurityGroup `json:"security_group"`
	}

	body := map[string]any{
		"name": scwNetworkGroup, "description": scwNetworkGroupDesc, "project": c.project,
		"tags": []string{scwTagManaged}, "stateful": true, "project_default": false,
		"inbound_default_policy": scwPolicyDrop, "outbound_default_policy": scwPolicyAccept,
	}

	if err := c.do(ctx, http.MethodPost, c.path("/security_groups"), body, &created); err != nil {
		return nil, err
	}

	if created.SecurityGroup.ID == "" {
		return nil, errors.New("scaleway made the security group but did not say its id: pick it yourself")
	}

	return &created.SecurityGroup, nil
}

// addRule adds one inbound accept rule from cidr: every port of protocol when port is 0.
func (c *scwClient) addRule(ctx context.Context, group, protocol, cidr string, port int) error {
	body := map[string]any{"protocol": protocol, "direction": scwDirectionInbound, "action": scwPolicyAccept, "ip_range": cidr}
	if port != 0 {
		body["dest_port_from"], body["dest_port_to"] = port, port
	}

	return c.do(ctx, http.MethodPost, c.path("/security_groups/"+url.PathEscape(group)+"/rules"), body, nil)
}

func (c *scwClient) groupRules(ctx context.Context, group string) ([]scwAPIRule, error) {
	var rules []scwAPIRule

	err := scwEachPage(ctx, c, c.path("/security_groups/"+url.PathEscape(group)+"/rules"), func(r struct {
		Rules []scwAPIRule `json:"rules"`
	}) int {
		rules = append(rules, r.Rules...)

		return len(r.Rules)
	})

	return rules, err
}

// withNetworkRules lists the group's inbound accept rules for the apps to show, read again
// unless rules is the list as it is now.
func (c *scwClient) withNetworkRules(ctx context.Context, result scwNetworkResult, sources []scwMemberSource, rules []scwAPIRule) (scwNetworkResult, error) {
	if rules == nil {
		var err error
		if rules, err = c.groupRules(ctx, result.SecurityGroupID); err != nil {
			return result, err
		}
	}

	names := map[string]string{}
	for _, s := range sources {
		names[s.cidr] = s.hostname
	}

	if result.PhoneIP != "" {
		names[result.PhoneIP+"/32"] = "this phone"
	}

	for _, r := range rules {
		if !r.inboundAccept() {
			continue
		}

		rule := scwNetworkRule{Protocol: strings.ToLower(r.Protocol), Source: r.source(), Description: names[r.source()]}
		if rule.Protocol == "any" {
			rule.Protocol = "all"
		}

		if !r.allPorts() {
			rule.Port = int(*r.DestPortFrom)
		}

		result.Rules = append(result.Rules, rule)
	}

	return result, nil
}
