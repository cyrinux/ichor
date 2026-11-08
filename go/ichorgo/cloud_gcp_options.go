package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// What a Compute Engine VM for a new node can be made from: the projects the key sees, the
// zones, the zone's machine types, the VPC networks with the region's subnets, and the Talos
// images the project already holds. There is no official Talos image on GCP: the person
// registers the Image Factory's gcp-<arch>.raw.tar.gz once (the Talos guide's gcloud compute
// images create). The price of one machine type comes from the Cloud Billing Catalog (GCPPrice).

const (
	gcpArchX86 = "X86_64"
	gcpArchARM = "ARM64"

	gcpFeatureUEFI = "UEFI_COMPATIBLE"
	gcpStatusUp    = "UP"
	gcpImageReady  = "READY"
)

// gcpOptions is what a VM can be created from, in one zone. Hourly prices are asked one machine
// type at a time (GCPPrice).
type gcpOptions struct {
	Project      string           `json:"project"`
	Zone         string           `json:"zone"`
	Projects     []gcpProject     `json:"projects"`
	Zones        []gcpZone        `json:"zones"`
	MachineTypes []gcpMachineType `json:"machineTypes"`
	Networks     []gcpNetwork     `json:"networks"`
	Images       []gcpImage       `json:"images"`
	Warnings     []string         `json:"warnings"`
	// Credential is the credential JSON to seal again when a Google sign-in changed, else "".
	Credential string `json:"credential,omitempty"`
}

type gcpProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type gcpZone struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

type gcpMachineType struct {
	Name     string  `json:"name"`
	VCPUs    int     `json:"vcpus"`
	MemoryGB float64 `json:"memoryGB"`
	Arch     string  `json:"arch"` // amd64 or arm64
}

type gcpNetwork struct {
	Name        string      `json:"name"`
	SelfLink    string      `json:"selfLink"`
	AutoSubnets bool        `json:"autoSubnets"`
	Subnets     []gcpSubnet `json:"subnets"`
}

type gcpSubnet struct {
	Name     string `json:"name"`
	SelfLink string `json:"selfLink"`
	Region   string `json:"region"`
	CIDR     string `json:"cidr"`
}

type gcpImage struct {
	Name         string `json:"name"`
	SelfLink     string `json:"selfLink"`
	Arch         string `json:"arch"`
	SizeGB       int64  `json:"sizeGB"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
	UEFI         bool   `json:"uefi"`
}

// gcpInt is an int64 Google writes as a string ("20") or a number.
type gcpInt int64

func (n *gcpInt) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(string(data), `"`)
	if raw == "" || raw == "null" {
		*n = 0

		return nil
	}

	v, err := strconv.ParseInt(raw, 10, 64)
	*n = gcpInt(v)

	return err
}

// The API's objects, as far as they are read.
type gcpAPIZone struct {
	Name   string `json:"name"`
	Region string `json:"region"` // a URL
	Status string `json:"status"`
}

type gcpAPIMachineType struct {
	Name         string `json:"name"`
	GuestCpus    int    `json:"guestCpus"`
	MemoryMb     int    `json:"memoryMb"`
	Architecture string `json:"architecture"`
	Deprecated   *struct {
		State string `json:"state"`
	} `json:"deprecated"`
}

// arch is the type's Talos architecture: an old entry without one is x86.
func (t gcpAPIMachineType) arch() string { return gcpTalosArch(t.Architecture) }

// usable: not deprecated, obsolete or deleted.
func (t gcpAPIMachineType) usable() bool {
	return t.Deprecated == nil || t.Deprecated.State == "" || t.Deprecated.State == "ACTIVE"
}

type gcpAPINetwork struct {
	Name                  string   `json:"name"`
	SelfLink              string   `json:"selfLink"`
	AutoCreateSubnetworks bool     `json:"autoCreateSubnetworks"`
	Subnetworks           []string `json:"subnetworks"`
}

type gcpAPISubnet struct {
	Name        string `json:"name"`
	SelfLink    string `json:"selfLink"`
	Region      string `json:"region"`  // a URL
	Network     string `json:"network"` // a URL
	IPCidrRange string `json:"ipCidrRange"`
}

type gcpAPIImage struct {
	Name            string            `json:"name"`
	SelfLink        string            `json:"selfLink"`
	Status          string            `json:"status"`
	Architecture    string            `json:"architecture"`
	DiskSizeGb      gcpInt            `json:"diskSizeGb"`
	Labels          map[string]string `json:"labels"`
	GuestOsFeatures []struct {
		Type string `json:"type"`
	} `json:"guestOsFeatures"`
}

func (i gcpAPIImage) uefi() bool {
	return slices.ContainsFunc(i.GuestOsFeatures, func(f struct {
		Type string `json:"type"`
	}) bool {
		return f.Type == gcpFeatureUEFI
	})
}

func (i gcpAPIImage) image() gcpImage {
	return gcpImage{
		Name: i.Name, SelfLink: i.SelfLink, Arch: gcpTalosArch(i.Architecture), SizeGB: int64(i.DiskSizeGb),
		TalosVersion: gcpVersionOfLabel(i.Labels[gcpLabelTalosVersion]), Schematic: i.Labels[gcpLabelSchematic], UEFI: i.uefi(),
	}
}

type gcpAPIRouter struct {
	Network string            `json:"network"` // a URL
	NATs    []json.RawMessage `json:"nats"`
}

// gcpTalosArch maps Google's architecture names to Talos's ("" is x86).
func gcpTalosArch(arch string) string {
	if strings.EqualFold(arch, gcpArchARM) {
		return bootArchARM64
	}

	return bootArchAMD64
}

// lastSegment is the name at the end of a URL or a path (a zone's region, a subnet's network).
func lastSegment(u string) string { return u[strings.LastIndexByte(u, '/')+1:] }

// gcpRegionOf is the region of a zone (europe-west1-b → europe-west1).
func gcpRegionOf(zone string) string {
	if i := strings.LastIndexByte(zone, '-'); i > 0 {
		return zone[:i]
	}

	return zone
}

// GCPOptions lists, for the key's project (credJSON's "project", else the key's project_id): the
// projects the key sees, the zones that are up, and in one zone (credJSON's "zone", else the
// first of them, with a warning): the zone's machine types (deprecated ones dropped, by vCPUs
// then memory), the VPC networks with the region's subnets (a warning for a network with no Cloud
// NAT there: without a public address the node has no route out), and the project's ready
// images, labelled ichor-talos-version first. A listing Google refuses is a warning, never an
// error, except the zones: the key's own. The demo context answers with samples, without a
// credential. JSON: {"project","zone","projects":[{"id","name"}],"zones":[{"name","region"}],
// "machineTypes":[{"name","vcpus","memoryGB","arch"}],"networks":[{"name","selfLink",
// "autoSubnets","subnets":[{"name","selfLink","region","cidr"}]}],"images":[{"name","selfLink",
// "arch","sizeGB","talosVersion","schematic","uefi"}],"warnings"}.
func GCPOptions(configYAML, contextName, credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoGCPOptions())
	}

	c, err := newGCPClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpOptionsTimeout)
	defer cancel()

	// The cluster's Talos version, for the warning when no image carries it; none: no warning.
	version := ""
	if strings.TrimSpace(configYAML) != "" {
		if media, err := cloudBootMedia(configYAML, contextName, ""); err == nil {
			version = media.TalosVersion
		}
	}

	options, err := c.options(ctx, version)
	if err != nil {
		return "", err
	}

	options.Credential = c.updated()

	return toJSON(options)
}

func (c *gcpClient) options(ctx context.Context, version string) (gcpOptions, error) {
	o := gcpOptions{
		Project: c.project, Projects: []gcpProject{}, Zones: []gcpZone{}, MachineTypes: []gcpMachineType{},
		Networks: []gcpNetwork{}, Images: []gcpImage{}, Warnings: []string{},
	}

	// The project's own zones: a refusal here is the key's (or the API's), an error.
	zones, err := gcpList[gcpAPIZone](ctx, c, c.compute(c.project, "/zones"), "items")
	if err != nil {
		return o, err
	}

	for _, z := range zones {
		if z.Status == gcpStatusUp {
			o.Zones = append(o.Zones, gcpZone{Name: z.Name, Region: lastSegment(z.Region)})
		}
	}

	slices.SortFunc(o.Zones, func(a, b gcpZone) int { return strings.Compare(a.Name, b.Name) })

	o.Projects = c.projects(ctx, &o.Warnings)

	o.Zone = c.zone
	if o.Zone == "" && len(o.Zones) > 0 {
		o.Zone = o.Zones[0].Name
		o.Warnings = append(o.Warnings, "no zone given: listing "+o.Zone)
	}

	if o.Zone == "" {
		o.Warnings = append(o.Warnings, "no zone is up in this project")

		return o, nil
	}

	region := gcpRegionOf(o.Zone)

	o.MachineTypes = c.machineTypes(ctx, o.Zone, &o.Warnings)
	o.Networks = c.networks(ctx, region, &o.Warnings)
	o.Images = c.images(ctx, &o.Warnings)

	if version != "" && !slices.ContainsFunc(o.Images, func(i gcpImage) bool { return i.TalosVersion == version }) {
		o.Warnings = append(o.Warnings, "no image labelled with Talos "+version+": pick one of your own or make one")
	}

	return o, nil
}

// gcpWarnRefusal turns a refused listing into a warning, and keeps any other error.
func gcpWarnRefusal(err error, warnings *[]string) error {
	var e *gcpError
	if errors.As(err, &e) && (e.status == http.StatusForbidden || e.reason == gcpReasonDisabled) {
		*warnings = append(*warnings, e.Error())

		return nil
	}

	return err
}

// gcpListOrWarn is gcpList whose failures are warnings: the options are what can be read.
func gcpListOrWarn[T any](ctx context.Context, c *gcpClient, u, key string, warnings *[]string) []T {
	items, err := gcpList[T](ctx, c, u, key)
	if err != nil {
		if gcpWarnRefusal(err, warnings) != nil {
			*warnings = append(*warnings, friendlyError(err))
		}

		return []T{}
	}

	return items
}

// projects lists the active projects the key sees (Cloud Resource Manager), at most
// gcpMaxProjects; the key's own when it sees none or the API refuses.
func (c *gcpClient) projects(ctx context.Context, warnings *[]string) []gcpProject {
	all := gcpListOrWarn[struct {
		ProjectID string `json:"projectId"`
		Name      string `json:"name"`
	}](ctx, c, gcpCRMEndpoint+"/v1/projects?"+url.Values{"filter": {"lifecycleState:ACTIVE"}, "pageSize": {"100"}}.Encode(), "projects", warnings)

	out := []gcpProject{}

	for _, p := range all {
		if len(out) < gcpMaxProjects {
			out = append(out, gcpProject{ID: p.ProjectID, Name: p.Name})
		}
	}

	if !slices.ContainsFunc(out, func(p gcpProject) bool { return p.ID == c.project }) {
		out = append(out, gcpProject{ID: c.project, Name: c.project})
	}

	slices.SortFunc(out, func(a, b gcpProject) int { return strings.Compare(a.ID, b.ID) })

	return out
}

// machineTypes lists the zone's usable machine types, by vCPUs then memory.
func (c *gcpClient) machineTypes(ctx context.Context, zone string, warnings *[]string) []gcpMachineType {
	all := gcpListOrWarn[gcpAPIMachineType](ctx, c, c.compute(c.project, "/zones/"+url.PathEscape(zone)+"/machineTypes?maxResults=500"), "items", warnings)
	out := []gcpMachineType{}

	for _, t := range all {
		if t.usable() {
			out = append(out, gcpMachineType{Name: t.Name, VCPUs: t.GuestCpus, MemoryGB: float64(t.MemoryMb) / 1024, Arch: t.arch()})
		}
	}

	slices.SortStableFunc(out, func(a, b gcpMachineType) int {
		return cmp.Or(cmp.Compare(a.VCPUs, b.VCPUs), cmp.Compare(a.MemoryGB, b.MemoryGB), strings.Compare(a.Name, b.Name))
	})

	return out
}

// networks lists the project's VPC networks with their subnets in region, and warns for those
// with no Cloud NAT there.
func (c *gcpClient) networks(ctx context.Context, region string, warnings *[]string) []gcpNetwork {
	all := gcpListOrWarn[gcpAPINetwork](ctx, c, c.compute(c.project, "/global/networks"), "items", warnings)
	subnets := c.regionSubnets(ctx, region, warnings)
	routers, routersErr := gcpList[gcpAPIRouter](ctx, c, c.compute(c.project, "/regions/"+url.PathEscape(region)+"/routers"), "items")

	if routersErr != nil && len(subnets) > 0 {
		*warnings = append(*warnings, "the Cloud NAT of "+region+" cannot be read ("+friendlyError(routersErr)+"): without a public address, check the node has a route out")
	}

	out := []gcpNetwork{}

	for _, n := range all {
		network := gcpNetwork{Name: n.Name, SelfLink: n.SelfLink, AutoSubnets: n.AutoCreateSubnetworks, Subnets: []gcpSubnet{}}

		for _, s := range subnets {
			if lastSegment(s.Network) == n.Name {
				network.Subnets = append(network.Subnets, gcpSubnet{Name: s.Name, SelfLink: s.SelfLink, Region: lastSegment(s.Region), CIDR: s.IPCidrRange})
			}
		}

		if len(network.Subnets) > 0 && routersErr == nil && !gcpHasNAT(routers, n.Name) {
			*warnings = append(*warnings, "network "+n.Name+" has no Cloud NAT in "+region+
				": without a public address, the node needs a route out to reach the Image Factory or Omni")
		}

		out = append(out, network)
	}

	slices.SortFunc(out, func(a, b gcpNetwork) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// gcpHasNAT: a router of the network has a Cloud NAT.
func gcpHasNAT(routers []gcpAPIRouter, network string) bool {
	return slices.ContainsFunc(routers, func(r gcpAPIRouter) bool { return lastSegment(r.Network) == network && len(r.NATs) > 0 })
}

// regionSubnets is the project's subnets in region, from the aggregated listing
// ({"items":{"regions/<r>":{"subnetworks":[…]}}}).
func (c *gcpClient) regionSubnets(ctx context.Context, region string, warnings *[]string) []gcpAPISubnet {
	out := []gcpAPISubnet{}

	err := gcpPages(ctx, c, c.compute(c.project, "/aggregated/subnetworks"), func(page map[string]json.RawMessage) error {
		var items map[string]struct {
			Subnetworks []gcpAPISubnet `json:"subnetworks"`
		}

		if raw, ok := page["items"]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return err
			}
		}

		out = append(out, items["regions/"+region].Subnetworks...)

		return nil
	})
	if err != nil {
		if gcpWarnRefusal(err, warnings) != nil {
			*warnings = append(*warnings, friendlyError(err))
		}

		return []gcpAPISubnet{}
	}

	slices.SortFunc(out, func(a, b gcpAPISubnet) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// images lists the project's ready images, labelled with a Talos version first.
func (c *gcpClient) images(ctx context.Context, warnings *[]string) []gcpImage {
	out := []gcpImage{}

	for _, i := range gcpListOrWarn[gcpAPIImage](ctx, c, c.compute(c.project, "/global/images"), "items", warnings) {
		if i.Status == gcpImageReady {
			out = append(out, i.image())
		}
	}

	slices.SortStableFunc(out, func(a, b gcpImage) int {
		return cmp.Or(cmp.Compare(gcpUnlabelled(a), gcpUnlabelled(b)), strings.Compare(b.TalosVersion, a.TalosVersion), strings.Compare(a.Name, b.Name))
	})

	return out
}

func gcpUnlabelled(i gcpImage) int {
	if i.TalosVersion != "" {
		return 0
	}

	return 1
}

type gcpPriceResult struct {
	Hourly   string `json:"hourly"`
	Currency string `json:"currency,omitempty"`
}

// gcpSharedCores are the vCPUs E2's shared-core types are billed for.
var gcpSharedCores = map[string]float64{"e2-micro": 0.25, "e2-small": 0.5, "e2-medium": 1}

// GCPPrice is the on-demand hourly price in USD of one machine type in a zone, before tax:
// {"hourly":"0.0335","currency":"USD"}, or {"hourly":""} when it cannot be read (the Cloud
// Billing API not enabled on the key's project, no answer within ten seconds, a type the catalog
// prices otherwise). The vCPU and memory SKUs of the type's series in the zone's region, summed.
// One call: the app asks for the chosen type only.
func GCPPrice(credJSON, zone, machineType string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newGCPClient(credJSON)
	if err != nil {
		return "", err
	}

	zone, machineType = strings.ToLower(strings.TrimSpace(zone)), strings.ToLower(strings.TrimSpace(machineType))

	switch {
	case !gcpZonePattern.MatchString(zone):
		return "", fmt.Errorf("%q is not a Compute Engine zone (europe-west1-b)", zone)
	case !gcpMachineTypePattern.MatchString(machineType):
		return "", fmt.Errorf("%q is not a machine type (e2-medium)", machineType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gcpPriceTimeout)
	defer cancel()

	hourly := c.pricing(ctx, zone, machineType)
	if hourly == "" {
		return toJSON(gcpPriceResult{})
	}

	return toJSON(gcpPriceResult{Hourly: hourly, Currency: "USD"})
}

// gcpSKU is one SKU of the Cloud Billing Catalog, as far as it is read.
type gcpSKU struct {
	Description string `json:"description"`
	Category    struct {
		ResourceFamily string `json:"resourceFamily"`
		UsageType      string `json:"usageType"`
	} `json:"category"`
	ServiceRegions []string `json:"serviceRegions"`
	PricingInfo    []struct {
		PricingExpression struct {
			TieredRates []struct {
				UnitPrice struct {
					CurrencyCode string `json:"currencyCode"`
					Units        string `json:"units"`
					Nanos        int64  `json:"nanos"`
				} `json:"unitPrice"`
			} `json:"tieredRates"`
		} `json:"pricingExpression"`
	} `json:"pricingInfo"`
}

// rate is the SKU's first USD unit price, false without one.
func (s gcpSKU) rate() (float64, bool) {
	if len(s.PricingInfo) == 0 || len(s.PricingInfo[0].PricingExpression.TieredRates) == 0 {
		return 0, false
	}

	p := s.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice

	units, err := strconv.ParseInt(cmp.Or(p.Units, "0"), 10, 64)
	if err != nil || p.CurrencyCode != "USD" {
		return 0, false
	}

	return float64(units) + float64(p.Nanos)/1e9, true
}

// pricing is the type's hourly price, "" when anything does not match.
func (c *gcpClient) pricing(ctx context.Context, zone, machineType string) string {
	t, ok := c.machineType(ctx, zone, machineType)
	if !ok {
		return ""
	}

	return gcpHourly(c.catalog(ctx), zone, machineType, t)
}

// machineType reads one machine type of zone for its price, false when it cannot be read.
func (c *gcpClient) machineType(ctx context.Context, zone, machineType string) (gcpAPIMachineType, bool) {
	var t gcpAPIMachineType
	if err := c.get(ctx, c.compute(c.project, "/zones/"+zone+"/machineTypes/"+machineType), &t); err != nil || t.GuestCpus == 0 {
		return t, false
	}

	return t, true
}

// catalog is Compute Engine's SKUs in the Cloud Billing Catalog, nil when it cannot be read.
func (c *gcpClient) catalog(ctx context.Context) []gcpSKU {
	skus, err := gcpList[gcpSKU](ctx, c, gcpBillingEndpoint+"/v1/services/"+gcpComputeService+"/skus?pageSize=5000", "skus")
	if err != nil {
		return nil
	}

	return skus
}

// gcpHourly is machine type t's on-demand hourly price in zone from skus, "" when a SKU is missing.
func gcpHourly(skus []gcpSKU, zone, machineType string, t gcpAPIMachineType) string {
	region := gcpRegionOf(zone)
	series, _, _ := strings.Cut(machineType, "-")
	series = strings.ToUpper(series)

	core, ram, found := 0.0, 0.0, 0
	for _, s := range skus {
		if s.Category.ResourceFamily != "Compute" || s.Category.UsageType != "OnDemand" || !slices.Contains(s.ServiceRegions, region) {
			continue
		}

		rate, ok := s.rate()

		switch {
		case !ok:
		case strings.HasPrefix(s.Description, series+" Instance Core running in "):
			core, found = rate, found|1
		case strings.HasPrefix(s.Description, series+" Instance Ram running in "):
			ram, found = rate, found|2
		}
	}

	if found != 3 {
		return ""
	}

	cores := cmp.Or(gcpSharedCores[machineType], float64(t.GuestCpus))
	hourly := cores*core + float64(t.MemoryMb)/1024*ram

	return strconv.FormatFloat(hourly, 'f', 4, 64)
}

func demoGCPOptions() gcpOptions {
	const (
		project = "example-project"
		zone    = "europe-west1-b"
		region  = "europe-west1"
		base    = "https://www.googleapis.com/compute/v1/projects/" + project
	)

	image := "talos-" + gcpLabelOfVersion(demoTalosVersion) + "-amd64"

	return gcpOptions{
		Project: project, Zone: zone,
		Projects: []gcpProject{{ID: project, Name: "Example project"}},
		Zones:    []gcpZone{{Name: zone, Region: region}, {Name: "europe-west1-c", Region: region}},
		MachineTypes: []gcpMachineType{
			{Name: "e2-medium", VCPUs: 2, MemoryGB: 4, Arch: bootArchAMD64},
			{Name: "t2a-standard-2", VCPUs: 2, MemoryGB: 8, Arch: bootArchARM64},
			{Name: "e2-standard-2", VCPUs: 2, MemoryGB: 8, Arch: bootArchAMD64},
		},
		Networks: []gcpNetwork{{
			Name: "default", SelfLink: base + "/global/networks/default", AutoSubnets: true,
			Subnets: []gcpSubnet{{Name: "default", SelfLink: base + "/regions/" + region + "/subnetworks/default", Region: region, CIDR: "10.132.0.0/20"}},
		}},
		Images: []gcpImage{{
			Name: image, SelfLink: base + "/global/images/" + image, Arch: bootArchAMD64, SizeGB: 10, TalosVersion: demoTalosVersion,
		}},
		Warnings: []string{},
	}
}
