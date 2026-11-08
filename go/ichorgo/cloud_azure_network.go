package ichorgo

import (
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

// S8 step 21: a network for the new node without a trip to the Azure portal. In the picked
// resource group: a network security group ichor-nodes and a virtual network ichor-nodes with one
// subnet behind that group, made once (tagged ichor-managed) and reused. Azure's default rules
// already let the network reach itself and refuse everything else inbound; for a plain Talos
// cluster one rule more lets this phone reach the node's Talos API (TCP 50000) from its public
// address of the day. Nothing is ever opened to the world, and neither is deleted by the app: they
// are shared by the nodes, not a run's resources.

const (
	azureNetworkName        = "ichor-nodes"
	azureNetworkSubnet      = "nodes"
	azureNetworkPhoneRule   = "talos-api-from-phone"
	azureTagManaged         = "ichor-managed"
	azureNetworkAuditAction = "azure-network-prepare"
	// azureNetworkTries bounds the /16 address spaces tried: 10.240.0.0/16 to 10.249.0.0/16.
	azureNetworkTries = 10
)

// azureNetworkActions are the writes AzureNetworkPrepare makes, checked before any of them.
var azureNetworkActions = []string{
	"Microsoft.Network/networkSecurityGroups/write", "Microsoft.Network/virtualNetworks/write",
	"Microsoft.Network/virtualNetworks/subnets/join/action", "Microsoft.Network/networkSecurityGroups/join/action",
}

// azureNetworkSpec is what AzureNetworkPrepare is asked.
type azureNetworkSpec struct {
	ResourceGroup string `json:"resourceGroup"`
	Location      string `json:"location"`
	Omni          bool   `json:"omni"`
}

type azureNetworkResult struct {
	NetworkID       string `json:"networkId"`
	SubnetID        string `json:"subnetId"`
	SecurityGroupID string `json:"securityGroupId"`
	// PhoneIP is the /32 port 50000 is open to, "" on Omni or when it cannot be learned.
	PhoneIP  string   `json:"phoneIp"`
	Created  []string `json:"created"`
	Reused   []string `json:"reused"`
	Warnings []string `json:"warnings"`
	// Credential is the credential JSON to seal again when the call rotated its refresh token.
	Credential string `json:"credential,omitempty"`
}

// azureNetworkRule is one security rule as ARM reads and writes it.
type azureNetworkRule struct {
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties"`
}

// azureNetworkResource is the part of a security group or virtual network the prepare reads.
type azureNetworkResource struct {
	ID         string            `json:"id"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`
	Properties struct {
		SecurityRules []azureNetworkRule `json:"securityRules"`
		AddressSpace  struct {
			AddressPrefixes []string `json:"addressPrefixes"`
		} `json:"addressSpace"`
		Subnets []azureAPISubnet `json:"subnets"`
	} `json:"properties"`
}

func (r azureNetworkResource) managed() bool { return r.Tags[azureTagManaged] == "true" }

// AzureNetworkPrepare finds or makes, in specJSON's resource group and location
// ({"resourceGroup","location","omni":bool}), the security group ichor-nodes (unless the cluster
// is Omni, one rule: TCP 50000 from this phone's public IPv4 /32, learned as the AWS prepare
// does; when it cannot be learned the rule is skipped with a warning) and the virtual network
// ichor-nodes with the subnet nodes behind it, on the first 10.24x.0.0/16 no network of the group
// uses. Idempotent: a second call reuses both and only refreshes the phone's address. A group or
// network of that name Ichor did not make is used as it is, with a warning. Refused, naming the
// action, when the role lacks a write; the demo answers samples and writes nothing. Recorded in
// the audit log. JSON: {"networkId","subnetId","securityGroupId","phoneIp","created":["nsg",
// "vnet"],"reused":[…],"warnings","credential"}.
func AzureNetworkPrepare(configYAML, contextName, credJSON, specJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoAzureNetwork())
	}

	var (
		spec   azureNetworkSpec
		result azureNetworkResult
	)

	audit := func() auditAction {
		params := "resourceGroup=" + spec.ResourceGroup + " location=" + spec.Location
		if result.NetworkID != "" {
			params += " created=" + strings.Join(result.Created, ",")
		}

		return auditAction{Action: azureNetworkAuditAction, Params: params}
	}

	err = recordedRun(configYAML, contextName, audit, func() error {
		if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
			return errors.New("invalid network request")
		}

		spec.ResourceGroup, spec.Location = strings.TrimSpace(spec.ResourceGroup), strings.ToLower(strings.TrimSpace(spec.Location))

		switch {
		case spec.ResourceGroup == "":
			return errors.New("pick the resource group the network goes in")
		case !azureLocationPattern.MatchString(spec.Location):
			return fmt.Errorf("%q is not an Azure location (westeurope)", spec.Location)
		}

		c, err := newAzureClient(credJSON)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), azureOptionsTimeout)
		defer cancel()

		result, err = c.prepareNetwork(ctx, spec, awsPhoneIPv4)
		result.Credential = c.updated()

		return err
	})
	if err != nil {
		return "", err
	}

	return toJSON(result)
}

func demoAzureNetwork() azureNetworkResult {
	group := "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/talos/providers/Microsoft.Network/"

	return azureNetworkResult{
		NetworkID: group + "virtualNetworks/" + azureNetworkName, SubnetID: group + "virtualNetworks/" + azureNetworkName + "/subnets/" + azureNetworkSubnet,
		SecurityGroupID: group + "networkSecurityGroups/" + azureNetworkName, PhoneIP: "203.0.113.7/32",
		Created: []string{}, Reused: []string{"nsg", "vnet"}, Warnings: []string{"Demo cluster: nothing was created."},
	}
}

// prepareNetwork is AzureNetworkPrepare once the credential and the spec are known. phoneIP learns
// the phone's public IPv4.
func (c *azureClient) prepareNetwork(ctx context.Context, spec azureNetworkSpec, phoneIP func(context.Context) (string, error)) (azureNetworkResult, error) {
	result := azureNetworkResult{Created: []string{}, Reused: []string{}, Warnings: []string{}}

	perms, err := c.permissions(ctx, spec.ResourceGroup)
	if err == nil && len(perms) > 0 {
		if action := missingAction(perms, azureNetworkActions); action != "" {
			return result, fmt.Errorf("the role may not %s: create the role again from the printed definition", action)
		}
	}

	base := "/subscriptions/" + c.subscription + "/resourceGroups/" + url.PathEscape(spec.ResourceGroup) + "/providers/Microsoft.Network/"
	result.SecurityGroupID = base + "networkSecurityGroups/" + azureNetworkName

	if err := c.prepareSecurityGroup(ctx, spec, phoneIP, &result); err != nil {
		return result, err
	}

	result.NetworkID = base + "virtualNetworks/" + azureNetworkName

	if err := c.prepareVirtualNetwork(ctx, spec, base, &result); err != nil {
		return result, err
	}

	return result, nil
}

// getOptional reads path into out; false when it does not exist yet.
func (c *azureClient) getOptional(ctx context.Context, path string, out any) (bool, error) {
	err := c.get(ctx, path, azureNetworkVersion, out)

	var e *azureError
	if errors.As(err, &e) && e.status == http.StatusNotFound && e.code != azureCodeGroupNotFound {
		return false, nil
	}

	return err == nil, err
}

// putAndWait writes body to path and follows the operation until it ends.
func (c *azureClient) putAndWait(ctx context.Context, path string, body any) error {
	_, headers, err := c.do(ctx, http.MethodPut, path, url.Values{"api-version": {azureNetworkVersion}}, body, nil)
	if err != nil {
		return err
	}

	return c.wait(ctx, headers, azurePollInterval)
}

// prepareSecurityGroup makes or refreshes the ichor-nodes group: on plain Talos, the phone's rule
// with today's address; every other rule of the group is kept as it is.
func (c *azureClient) prepareSecurityGroup(ctx context.Context, spec azureNetworkSpec, phoneIP func(context.Context) (string, error), result *azureNetworkResult) error {
	var existing azureNetworkResource

	found, err := c.getOptional(ctx, result.SecurityGroupID, &existing)
	if err != nil {
		return err
	}

	if found && !existing.managed() {
		result.Reused = append(result.Reused, "nsg")
		result.Warnings = append(result.Warnings, "a security group named "+azureNetworkName+" exists that Ichor did not make: its rules are left as they are")

		return nil
	}

	rules := slices.DeleteFunc(slices.Clone(existing.Properties.SecurityRules), func(r azureNetworkRule) bool { return r.Name == azureNetworkPhoneRule })

	if !spec.Omni {
		ip, err := phoneIP(ctx)
		if err != nil {
			result.Warnings = append(result.Warnings, "could not learn this phone's address: the Talos API rule was not added, and the follow needs a route to port 50000; "+
				"prepare the network again from the network the phone is on, or follow the node through Omni")
		} else {
			result.PhoneIP = ip + "/32"
			rules = append(rules, azurePhoneRule(result.PhoneIP))
		}
	}

	if found && result.PhoneIP == "" {
		// Nothing to refresh: the group stays as it is (an old phone rule included).
		result.Reused = append(result.Reused, "nsg")

		return nil
	}

	body := map[string]any{
		"location": spec.Location, "tags": map[string]string{azureTagManaged: "true"},
		"properties": map[string]any{"securityRules": rules},
	}
	if found {
		body["location"] = existing.Location
	}

	if err := c.putAndWait(ctx, result.SecurityGroupID, body); err != nil {
		return err
	}

	if found {
		result.Reused = append(result.Reused, "nsg")
	} else {
		result.Created = append(result.Created, "nsg")
	}

	return nil
}

// azurePhoneRule lets cidr (the phone's /32) reach the Talos API, and nothing else.
func azurePhoneRule(cidr string) azureNetworkRule {
	return azureNetworkRule{Name: azureNetworkPhoneRule, Properties: map[string]any{
		"priority": 1000, "direction": "Inbound", "access": "Allow", "protocol": "Tcp",
		"sourceAddressPrefix": cidr, "sourcePortRange": "*", "destinationAddressPrefix": "*",
		"destinationPortRange": strconv.Itoa(azureTalosAPIPort), "description": "Ichor: this phone reaches the Talos API",
	}}
}

// prepareVirtualNetwork reuses the ichor-nodes network (its subnet nodes), or makes it on the
// first /16 the group's networks leave free, with the subnet behind the security group.
func (c *azureClient) prepareVirtualNetwork(ctx context.Context, spec azureNetworkSpec, base string, result *azureNetworkResult) error {
	var existing azureNetworkResource

	found, err := c.getOptional(ctx, result.NetworkID, &existing)
	if err != nil {
		return err
	}

	if found {
		result.Reused = append(result.Reused, "vnet")
		result.SubnetID = result.NetworkID + "/subnets/" + azureNetworkSubnet

		if i := slices.IndexFunc(existing.Properties.Subnets, func(s azureAPISubnet) bool { return s.Name == azureNetworkSubnet }); i >= 0 && existing.Properties.Subnets[i].ID != "" {
			result.SubnetID = existing.Properties.Subnets[i].ID
		} else if i < 0 && len(existing.Properties.Subnets) > 0 {
			result.SubnetID = existing.Properties.Subnets[0].ID
		}

		if !existing.managed() {
			result.Warnings = append(result.Warnings, "a virtual network named "+azureNetworkName+" exists that Ichor did not make: it is used as it is")
		}

		return nil
	}

	space, err := c.freeAddressSpace(ctx, base)
	if err != nil {
		return err
	}

	subnet := strings.Replace(space, "/16", "/24", 1)
	body := map[string]any{
		"location": spec.Location, "tags": map[string]string{azureTagManaged: "true"},
		"properties": map[string]any{
			"addressSpace": map[string]any{"addressPrefixes": []string{space}},
			"subnets": []any{map[string]any{"name": azureNetworkSubnet, "properties": map[string]any{
				"addressPrefix": subnet, "networkSecurityGroup": map[string]string{"id": result.SecurityGroupID},
			}}},
		},
	}

	if err := c.putAndWait(ctx, result.NetworkID, body); err != nil {
		return err
	}

	result.Created = append(result.Created, "vnet")
	result.SubnetID = result.NetworkID + "/subnets/" + azureNetworkSubnet

	return nil
}

// freeAddressSpace is the first of 10.240.0.0/16 … 10.249.0.0/16 that no network of the resource
// group overlaps.
func (c *azureClient) freeAddressSpace(ctx context.Context, base string) (string, error) {
	networks, err := azureList[azureNetworkResource](ctx, c, base+"virtualNetworks", azureNetworkVersion, nil)

	var e *azureError
	if errors.As(err, &e) && e.code == azureCodeNotFound {
		networks, err = nil, nil
	}

	if err != nil {
		return "", err
	}

	var used []netip.Prefix

	for _, n := range networks {
		for _, p := range n.Properties.AddressSpace.AddressPrefixes {
			if prefix, err := netip.ParsePrefix(p); err == nil {
				used = append(used, prefix)
			}
		}
	}

	for i := range azureNetworkTries {
		candidate := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(240 + i), 0, 0}), 16)
		if !slices.ContainsFunc(used, candidate.Overlaps) {
			return candidate.String(), nil
		}
	}

	return "", errors.New("10.240.0.0/16 to 10.249.0.0/16 are all used by networks of this resource group: pick a subnet yourself")
}
