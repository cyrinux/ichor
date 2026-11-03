package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// What a Compute Engine VM create checks before any write: the request, the image against the
// machine type, the subnet against the zone, the name the run would take, and the key's
// permissions (Google has no dry run: testIamPermissions is the gate).

var (
	// gcpNamePattern is a Compute Engine resource name (RFC 1035, lowercase, at most 63).
	gcpNamePattern       = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)
	gcpImagePathPattern  = regexp.MustCompile(`(?:^|/)projects/([^/?#\s]+)/global/images/([^/?#\s]+)$`)
	gcpSubnetPathPattern = regexp.MustCompile(`(?:^|/)(?:projects/([^/?#\s]+)/)?regions/([a-z0-9-]+)/subnetworks/([^/?#\s]+)$`)
)

// checkSpec refuses a request that is incomplete or whose names would not fit in a path, and
// reads the image and the subnet out of their names or links.
func (h *gcpCreate) checkSpec() error {
	s := &h.spec
	s.Project = cmp.Or(strings.TrimSpace(s.Project), h.client.project)
	s.Zone, s.MachineType = strings.ToLower(strings.TrimSpace(s.Zone)), strings.ToLower(strings.TrimSpace(s.MachineType))
	s.Image, s.Subnetwork = strings.TrimSpace(s.Image), strings.TrimSpace(s.Subnetwork)

	if s.Zone == "" || s.MachineType == "" || s.Image == "" || s.Subnetwork == "" {
		return errors.New("pick a zone, a machine type, an image and a subnet")
	}

	switch {
	case !gcpProjectPattern.MatchString(s.Project):
		return fmt.Errorf("%q is not a project ID", s.Project)
	case !gcpZonePattern.MatchString(s.Zone):
		return fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", s.Zone)
	case !gcpMachineTypePattern.MatchString(s.MachineType):
		return fmt.Errorf("%q is not a machine type (e2-medium)", s.MachineType)
	case s.DiskGB != 0 && (s.DiskGB < gcpMinDiskGB || s.DiskGB > gcpMaxDiskGB):
		return fmt.Errorf("a boot disk of %d GB: Compute Engine takes %d to %d", s.DiskGB, gcpMinDiskGB, gcpMaxDiskGB)
	}

	h.imageName = s.Image
	if m := gcpImagePathPattern.FindStringSubmatch(s.Image); m != nil {
		if m[1] != s.Project {
			return fmt.Errorf("image %s is not in project %s: pick one of its own", m[2], s.Project)
		}

		h.imageName = m[2]
	}

	h.subnetProject, h.subnetRegion, h.subnetName = s.Project, gcpRegionOf(s.Zone), s.Subnetwork
	if m := gcpSubnetPathPattern.FindStringSubmatch(s.Subnetwork); m != nil {
		h.subnetProject, h.subnetRegion, h.subnetName = cmp.Or(m[1], s.Project), m[2], m[3]
	}

	for _, name := range []string{h.imageName, h.subnetName} {
		if !gcpNamePattern.MatchString(name) {
			return fmt.Errorf("%q is not a Compute Engine name", name)
		}
	}

	if !gcpProjectPattern.MatchString(h.subnetProject) {
		return fmt.Errorf("%q is not a project ID", h.subnetProject)
	}

	if s.DiskGB == 0 {
		s.DiskGB = gcpDefaultDiskGB
	}

	return nil
}

// gcpNotFound: Google answered 404.
func gcpNotFound(err error) bool {
	var e *gcpError

	return errors.As(err, &e) && e.status == http.StatusNotFound
}

// checkChoice refuses an image the machine type cannot boot, a subnet of another region than the
// zone, and a name the zone already holds.
func (h *gcpCreate) checkChoice(ctx context.Context) error {
	c, s := h.client, h.spec

	var image gcpAPIImage
	if err := c.get(ctx, c.compute(s.Project, "/global/images/"+url.PathEscape(h.imageName)), &image); err != nil {
		if gcpNotFound(err) {
			return fmt.Errorf("image %s is not in project %s: pick another", h.imageName, s.Project)
		}

		return err
	}

	if image.Status != gcpImageReady {
		return fmt.Errorf("image %s is %s, not ready: wait for it or pick another", h.imageName, strings.ToLower(cmp.Or(image.Status, "unknown")))
	}

	var machineType gcpAPIMachineType
	if err := c.get(ctx, c.compute(s.Project, "/zones/"+s.Zone+"/machineTypes/"+s.MachineType), &machineType); err != nil {
		if gcpNotFound(err) {
			return fmt.Errorf("no machine type %s in %s: pick another", s.MachineType, s.Zone)
		}

		return err
	}

	if !machineType.usable() {
		return fmt.Errorf("machine type %s is %s: pick another", s.MachineType, strings.ToLower(machineType.Deprecated.State))
	}

	switch arch := gcpTalosArch(image.Architecture); {
	case arch != machineType.arch():
		return fmt.Errorf("this image is %s, the machine type is %s: pick another", arch, machineType.arch())
	case arch == bootArchARM64 && !image.uefi():
		return errors.New("an Arm machine type boots an image with the UEFI_COMPATIBLE feature only: register it with --guest-os-features=UEFI_COMPATIBLE")
	case int64(image.DiskSizeGb) > int64(s.DiskGB):
		return fmt.Errorf("image %s needs a boot disk of %d GB at least: ask for more", h.imageName, image.DiskSizeGb)
	}

	if err := h.checkNetwork(ctx); err != nil {
		return err
	}

	err := c.get(ctx, h.instanceURL(), nil)

	switch {
	case err == nil:
		return fmt.Errorf("an instance named %s exists in %s: pick another host name", s.Hostname, s.Zone)
	case !gcpNotFound(err):
		return err
	}

	return nil
}

// checkNetwork refuses a subnet of another region than the zone's, and notes a subnet with no
// route out (no public address asked, no Cloud NAT on its network there).
func (h *gcpCreate) checkNetwork(ctx context.Context) error {
	c, region := h.client, gcpRegionOf(h.spec.Zone)

	var subnet gcpAPISubnet

	u := c.compute(h.subnetProject, "/regions/"+url.PathEscape(h.subnetRegion)+"/subnetworks/"+url.PathEscape(h.subnetName))
	if err := c.get(ctx, u, &subnet); err != nil {
		if gcpNotFound(err) {
			return fmt.Errorf("subnet %s cannot be found in %s: pick another", h.subnetName, h.subnetRegion)
		}

		return err
	}

	if got := lastSegment(cmp.Or(subnet.Region, h.subnetRegion)); got != region {
		return fmt.Errorf("subnet %s is in %s, the zone %s in %s: pick a subnet of %s", h.subnetName, got, h.spec.Zone, region, region)
	}

	if h.spec.AssociatePublicIP {
		return nil
	}

	// Optional: a router list refused is no refusal.
	routers, err := gcpList[gcpAPIRouter](ctx, c, c.compute(h.subnetProject, "/regions/"+url.PathEscape(region)+"/routers"), "items")
	h.noRoute = err == nil && !gcpHasNAT(routers, lastSegment(subnet.Network))

	return nil
}

// checkPermissions refuses a key that may not make or delete what the run makes: Google has no
// dry run, so testIamPermissions on the project is the check before any write. A check that
// cannot be made refuses too.
func (h *gcpCreate) checkPermissions(ctx context.Context) error {
	wanted := gcpCreatePermissions(h.spec.AssociatePublicIP)

	if err := h.checkGranted(ctx, h.spec.Project, wanted); err != nil {
		return err
	}

	if h.subnetProject == h.spec.Project {
		return nil
	}

	// A shared VPC's subnet is used in its host project.
	use := []string{"compute.subnetworks.use", "compute.subnetworks.get"}
	if h.spec.AssociatePublicIP {
		use = append(use, "compute.subnetworks.useExternalIp")
	}

	return h.checkGranted(ctx, h.subnetProject, use)
}

func (h *gcpCreate) checkGranted(ctx context.Context, project string, wanted []string) error {
	granted, err := h.client.testPermissions(ctx, project, wanted)
	if err != nil {
		return fmt.Errorf("the service account's permissions in project %s cannot be checked, so nothing is created: %w", project, err)
	}

	if missing := missingPermissions(granted, wanted); len(missing) > 0 {
		return fmt.Errorf("this service account may not use %s in project %s: grant the printed role", strings.Join(missing, ", "), project)
	}

	return nil
}
