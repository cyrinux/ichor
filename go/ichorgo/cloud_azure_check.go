package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// What an Azure VM create checks before any write: the request, the principal's permissions
// (Azure has no dry run), the image against the size, the network against the location, and the
// names the run would take.

// checkSpec refuses a request that is incomplete or whose names would not fit in a path.
func (h *azureCreate) checkSpec() error {
	s := &h.spec
	s.Location, s.ResourceGroup, s.Size = strings.ToLower(strings.TrimSpace(s.Location)), strings.TrimSpace(s.ResourceGroup), strings.TrimSpace(s.Size)

	if s.ResourceGroup == "" || s.Location == "" || s.Size == "" || s.ImageID == "" || s.SubnetID == "" {
		return errors.New("pick a resource group, a location, a VM size, an image and a subnet")
	}

	switch {
	case !azureGroupPattern.MatchString(s.ResourceGroup) || strings.HasSuffix(s.ResourceGroup, "."):
		return fmt.Errorf("%q is not a resource group name", s.ResourceGroup)
	case !azureLocationPattern.MatchString(s.Location):
		return fmt.Errorf("%q is not an Azure location (westeurope)", s.Location)
	case !azureSizePattern.MatchString(s.Size):
		return fmt.Errorf("%q is not a VM size (Standard_B2s)", s.Size)
	case s.OSDiskGB < 0 || s.OSDiskGB > azureMaxDiskGB:
		return fmt.Errorf("an OS disk of %d GB: Azure takes 1 to %d", s.OSDiskGB, azureMaxDiskGB)
	}

	for _, id := range []string{s.ImageID, s.SubnetID, s.SecurityGroupID} {
		if id != "" && (!azureIDPattern.MatchString(id) || strings.Contains(id, "..")) {
			return fmt.Errorf("%q is not an Azure resource id", id)
		}
	}

	if !strings.Contains(strings.ToLower(s.SubnetID), "/subnets/") {
		return fmt.Errorf("%q is not a subnet id", s.SubnetID)
	}

	return nil
}

// checkPermissions refuses a principal that may not make or delete what the run makes: Azure
// has no dry run, so its permissions listing is the check before any write.
func (h *azureCreate) checkPermissions(ctx context.Context) error {
	perms, err := h.client.permissions(ctx, h.spec.ResourceGroup)

	var e *azureError
	if errors.As(err, &e) && e.status == http.StatusForbidden {
		return errors.New("this service principal may not read its own permissions: add Microsoft.Authorization/permissions/read (paste the printed role)")
	}

	if err != nil {
		return err
	}

	actions := azureCreateActions(h.spec.AssociatePublicIP)
	if h.spec.SecurityGroupID != "" {
		actions = append(actions, "Microsoft.Network/networkSecurityGroups/join/action")
	}

	if missing := missingAction(perms, actions); missing != "" {
		return fmt.Errorf("this service principal may not %s in resource group %s: paste the printed role", missing, h.spec.ResourceGroup)
	}

	return nil
}

// azureNotFound: Azure answered 404.
func azureNotFound(err error) bool {
	var e *azureError

	return errors.As(err, &e) && e.status == http.StatusNotFound
}

// checkChoice refuses an image the size cannot run, a subnet or security group of another
// location, and a name the resource group already holds.
func (h *azureCreate) checkChoice(ctx context.Context) error {
	arch, generation, err := h.imageKind(ctx)
	if err != nil {
		return err
	}

	size, err := h.size(ctx)
	if err != nil {
		return err
	}

	if !slices.Contains(size.Generations, generation) {
		return fmt.Errorf("this image is %s, the size runs %s only: pick another", generation, strings.Join(size.Generations, ", "))
	}

	if arch != size.Arch {
		return fmt.Errorf("this image is %s, the size is %s: pick another", arch, size.Arch)
	}

	if err := h.checkNetwork(ctx); err != nil {
		return err
	}

	for _, r := range []struct{ word, path string }{{"VM", h.vmPath()}, {"network interface", h.nicPath()}, {"public IP address", h.ipPath()}} {
		err := h.client.get(ctx, r.path, azureVersionOf(r.path), nil)

		var e *azureError

		switch {
		case err == nil:
			return fmt.Errorf("a %s named %s exists in %s: pick another host name", r.word, r.path[strings.LastIndex(r.path, "/")+1:], h.spec.ResourceGroup)
		case errors.As(err, &e) && e.code == azureCodeGroupNotFound:
			return fmt.Errorf("resource group %s does not exist: pick another", h.spec.ResourceGroup)
		case !azureNotFound(err):
			return err
		}
	}

	return nil
}

// azureVersionOf is the api-version of a resource path.
func azureVersionOf(path string) string {
	if strings.Contains(path, "/Microsoft.Network/") {
		return azureNetworkVersion
	}

	return azureComputeVersion
}

// imageKind is the image's architecture and Hyper-V generation: a managed image's own, a
// gallery version's from its image definition.
func (h *azureCreate) imageKind(ctx context.Context) (string, string, error) {
	id := h.spec.ImageID
	missing := func(err error) error {
		if azureNotFound(err) {
			return fmt.Errorf("image %s is not in this subscription: pick another", id[strings.LastIndex(id, "/")+1:])
		}

		return err
	}

	lower := strings.ToLower(id)
	if i := strings.LastIndex(lower, "/versions/"); i >= 0 && strings.Contains(lower, "/galleries/") {
		var version, def azureAPIImage
		if err := h.client.get(ctx, id, azureComputeVersion, &version); err != nil {
			return "", "", missing(err)
		}

		if err := h.client.get(ctx, id[:i], azureComputeVersion, &def); err != nil {
			return "", "", missing(err)
		}

		if !strings.EqualFold(def.Properties.OSType, "Linux") || azureTalosArch(def.Properties.Architecture) == "" {
			return "", "", errors.New("this gallery image is not a Linux image Talos runs: pick another")
		}

		return azureTalosArch(def.Properties.Architecture), cmp.Or(def.Properties.HyperVGeneration, azureGenV1), nil
	}

	var image azureAPIImage
	if err := h.client.get(ctx, id, azureComputeVersion, &image); err != nil {
		return "", "", missing(err)
	}

	if !strings.EqualFold(image.Properties.StorageProfile.OSDisk.OSType, "Linux") {
		return "", "", errors.New("this image is not a Linux image: pick another")
	}

	return image.managedArch(), cmp.Or(image.Properties.HyperVGeneration, azureGenV1), nil
}

// size is the chosen VM size in the location, refused when the subscription may not run it.
func (h *azureCreate) size(ctx context.Context) (azureSize, error) {
	return azureFindSize(ctx, h.client, h.spec.Location, h.spec.Size)
}

// azureFindSize is one VM size in a location, refused when the subscription may not run it.
func azureFindSize(ctx context.Context, c *azureClient, location, name string) (azureSize, error) {
	skus, err := azureList[azureAPISKU](ctx, c, "/subscriptions/"+c.subscription+"/providers/Microsoft.Compute/skus",
		azureSKUsVersion, url.Values{"$filter": {"location eq '" + location + "'"}})
	if err != nil {
		return azureSize{}, err
	}

	for _, s := range skus {
		if size, ok := s.size(); ok && strings.EqualFold(s.Name, name) {
			return size, nil
		}
	}

	return azureSize{}, fmt.Errorf("no VM size %s in %s for this subscription: pick another", name, location)
}

// checkNetwork refuses a subnet or a security group of another location than the VM's.
func (h *azureCreate) checkNetwork(ctx context.Context) error {
	nat, err := azureCheckNetwork(ctx, h.client, h.spec.SubnetID, h.spec.SecurityGroupID, h.spec.Location)
	if err != nil {
		return err
	}

	h.noRoute = !h.spec.AssociatePublicIP && !nat

	return nil
}

// azureCheckNetwork refuses a subnet or a security group ("" for none) of another location, and
// says whether the subnet has a NAT gateway.
func azureCheckNetwork(ctx context.Context, c *azureClient, subnetID, securityGroupID, location string) (bool, error) {
	var subnet azureAPISubnet
	if err := c.get(ctx, subnetID, azureNetworkVersion, &subnet); err != nil {
		if azureNotFound(err) {
			return false, errors.New("the subnet cannot be found: pick another")
		}

		return false, err
	}

	var network azureAPINetwork

	vnet := subnetID[:strings.LastIndex(strings.ToLower(subnetID), "/subnets/")]
	if err := c.get(ctx, vnet, azureNetworkVersion, &network); err != nil {
		return false, err
	}

	if !azureSameLocation(network.Location, location) {
		return false, fmt.Errorf("virtual network %s is in %s, the VM in %s: pick a subnet of %s", network.Name, network.Location, location, location)
	}

	nat := subnet.Properties.NATGateway != nil

	if securityGroupID == "" {
		return nat, nil
	}

	var group azureAPISecurityGroup
	if err := c.get(ctx, securityGroupID, azureNetworkVersion, &group); err != nil {
		if azureNotFound(err) {
			return false, errors.New("the network security group cannot be found: pick another")
		}

		return false, err
	}

	if !azureSameLocation(group.Location, location) {
		return false, fmt.Errorf("network security group %s is in %s, the VM in %s: pick one of %s", group.Name, group.Location, location, location)
	}

	return nat, nil
}
