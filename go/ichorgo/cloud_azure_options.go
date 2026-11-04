package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// What an Azure VM for a new node can be made from: the subscription's resource groups, the
// location's VM sizes, the virtual networks with their subnets, the network security groups, and
// the Talos images the subscription already holds (managed images and gallery image versions).
// There is no official Talos image on Azure: the person uploads the Image Factory's VHD once.

const (
	// azureMaxGalleries bounds the galleries read (one call per gallery and image definition).
	azureMaxGalleries     = 20
	azureMaxGalleryImages = 50

	azureImageManaged = "managed"
	azureImageGallery = "gallery"

	azureGenV1 = "V1"
	azureGenV2 = "V2"

	// azureTalosAPIPort is apid's: a rule letting the Internet reach it makes a group wide open.
	azureTalosAPIPort = 50000
)

// azureOptions is what a VM can be created from, in one location. Hourly prices are asked one
// size at a time (AzurePrice).
type azureOptions struct {
	Subscription   string               `json:"subscription"`
	Location       string               `json:"location"`
	Subscriptions  []azureSubscription  `json:"subscriptions"`
	Locations      []azureLocation      `json:"locations"`
	ResourceGroups []azureResourceGroup `json:"resourceGroups"`
	Sizes          []azureSize          `json:"sizes"`
	Networks       []azureNetwork       `json:"networks"`
	SecurityGroups []azureSecurityGroup `json:"securityGroups"`
	Images         []azureImage         `json:"images"`
	Warnings       []string             `json:"warnings"`
}

type azureSubscription struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type azureLocation struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type azureResourceGroup struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

type azureSize struct {
	Name        string   `json:"name"`
	VCPUs       int      `json:"vcpus"`
	MemoryGB    float64  `json:"memoryGB"`
	Arch        string   `json:"arch"` // amd64 or arm64
	Generations []string `json:"generations"`
}

type azureNetwork struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	ResourceGroup string        `json:"resourceGroup"`
	Location      string        `json:"location"`
	Subnets       []azureSubnet `json:"subnets"`
}

type azureSubnet struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	CIDR  string `json:"cidr"`
	NSGID string `json:"nsgId"`
	// NATGateway: the subnet reaches the Internet without a public IP on the VM.
	NATGateway bool `json:"natGateway"`
}

type azureSecurityGroup struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
	WideOpen      bool   `json:"wideOpen"`
}

type azureImage struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ResourceGroup string `json:"resourceGroup"`
	Source        string `json:"source"` // managed or gallery
	Arch          string `json:"arch"`
	Generation    string `json:"generation"`
	TalosVersion  string `json:"talosVersion"`
	Schematic     string `json:"schematic"`
}

// The API's objects, as far as they are read.
type azureAPIRef struct {
	ID string `json:"id"`
}

type azureAPISKU struct {
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
	Capabilities []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"capabilities"`
	Restrictions []struct {
		ReasonCode string `json:"reasonCode"`
	} `json:"restrictions"`
}

func (s azureAPISKU) capability(name string) string {
	for _, c := range s.Capabilities {
		if c.Name == name {
			return c.Value
		}
	}

	return ""
}

// size is the SKU as a VM size, false when Talos cannot run it or the subscription may not.
func (s azureAPISKU) size() (azureSize, bool) {
	if !strings.EqualFold(s.ResourceType, "virtualMachines") {
		return azureSize{}, false
	}

	for _, r := range s.Restrictions {
		if r.ReasonCode == "NotAvailableForSubscription" {
			return azureSize{}, false
		}
	}

	arch := azureTalosArch(s.capability("CpuArchitectureType"))
	if arch == "" {
		return azureSize{}, false
	}

	vcpus, _ := strconv.Atoi(s.capability("vCPUs"))
	memory, _ := strconv.ParseFloat(s.capability("MemoryGB"), 64)

	return azureSize{Name: s.Name, VCPUs: vcpus, MemoryGB: memory, Arch: arch, Generations: splitCSV(cmp.Or(s.capability("HyperVGenerations"), azureGenV1))}, true
}

type azureAPISubnet struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Properties struct {
		AddressPrefix        string       `json:"addressPrefix"`
		NetworkSecurityGroup *azureAPIRef `json:"networkSecurityGroup"`
		NATGateway           *azureAPIRef `json:"natGateway"`
	} `json:"properties"`
}

type azureAPINetwork struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	Properties struct {
		Subnets []azureAPISubnet `json:"subnets"`
	} `json:"properties"`
}

type azureAPISecurityGroup struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	Properties struct {
		SecurityRules []struct {
			Properties struct {
				Direction             string   `json:"direction"`
				Access                string   `json:"access"`
				SourceAddressPrefix   string   `json:"sourceAddressPrefix"`
				SourceAddressPrefixes []string `json:"sourceAddressPrefixes"`
				DestinationPortRange  string   `json:"destinationPortRange"`
				DestinationPortRanges []string `json:"destinationPortRanges"`
			} `json:"properties"`
		} `json:"securityRules"`
	} `json:"properties"`
}

// wideOpen: an inbound Allow rule from anywhere reaches the Talos API port.
func (g azureAPISecurityGroup) wideOpen() bool {
	for _, r := range g.Properties.SecurityRules {
		p := r.Properties
		if !strings.EqualFold(p.Direction, "Inbound") || !strings.EqualFold(p.Access, "Allow") {
			continue
		}

		sources := append([]string{p.SourceAddressPrefix}, p.SourceAddressPrefixes...)
		anywhere := slices.ContainsFunc(sources, func(s string) bool { return s == "*" || strings.EqualFold(s, "Internet") || s == "0.0.0.0/0" })

		ports := append([]string{p.DestinationPortRange}, p.DestinationPortRanges...)
		if anywhere && slices.ContainsFunc(ports, func(r string) bool { return azurePortIn(r, azureTalosAPIPort) }) {
			return true
		}
	}

	return false
}

// azurePortIn: a port range ("*", "50000", "50000-50001") holds port.
func azurePortIn(r string, port int) bool {
	r = strings.TrimSpace(r)
	if r == "*" {
		return true
	}

	lo, hi, found := strings.Cut(r, "-")
	if !found {
		hi = lo
	}

	from, err1 := strconv.Atoi(strings.TrimSpace(lo))
	to, err2 := strconv.Atoi(strings.TrimSpace(hi))

	return err1 == nil && err2 == nil && from <= port && port <= to
}

type azureAPIImage struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`
	Properties struct {
		HyperVGeneration string `json:"hyperVGeneration"`
		OSType           string `json:"osType"`       // gallery image definitions
		Architecture     string `json:"architecture"` // gallery image definitions
		StorageProfile   struct {
			OSDisk struct {
				OSType string `json:"osType"`
			} `json:"osDisk"`
		} `json:"storageProfile"` // managed images
		PublishingProfile struct {
			TargetRegions []struct {
				Name string `json:"name"`
			} `json:"targetRegions"`
		} `json:"publishingProfile"` // gallery image versions
	} `json:"properties"`
}

// azureTalosArch maps Azure's architecture names to Talos's ("" and x64 are amd64).
func azureTalosArch(arch string) string {
	switch strings.ToLower(arch) {
	case "", "x64":
		return bootArchAMD64
	case "arm64":
		return bootArchARM64
	}

	return ""
}

// azureSameLocation compares a location name with a name or a display name ("West Europe").
func azureSameLocation(a, b string) bool {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "")) }

	return norm(a) == norm(b)
}

// azureResourceGroupOf is the resource group in a resource id.
func azureResourceGroupOf(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}

	return ""
}

// AzureOptions lists, for the principal's subscription: the subscriptions it sees, the
// locations, the resource groups, and in one location (credJSON's "location", else the first
// resource group's): the VM sizes Talos runs on (the subscription's restricted ones dropped),
// the virtual networks with their subnets, the network security groups (wideOpen when the
// Internet reaches port 50000), and the Linux images the subscription holds, tagged
// ichor-talos-version first. A listing Azure refuses is a warning, never an error. The demo
// context answers with samples, without a credential. JSON: {"subscription","location",
// "subscriptions":[{"id","name"}],"locations":[{"name","displayName"}],"resourceGroups":
// [{"name","location"}],"sizes":[{"name","vcpus","memoryGB","arch","generations"}],"networks":
// [{"id","name","resourceGroup","location","subnets":[{"id","name","cidr","nsgId","natGateway"}]}],
// "securityGroups":[{"id","name","resourceGroup","wideOpen"}],"images":[{"id","name",
// "resourceGroup","source","arch","generation","talosVersion","schematic"}],"warnings"}.
func AzureOptions(configYAML, contextName, credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoAzureOptions())
	}

	c, err := newAzureClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), azureOptionsTimeout)
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

	return toJSON(options)
}

func (c *azureClient) options(ctx context.Context, version string) (azureOptions, error) {
	o := azureOptions{Subscription: c.subscription, Subscriptions: []azureSubscription{}, Warnings: []string{}}

	// The subscription's own listings: a refusal here is the credential's, an error.
	subs, err := azureList[struct {
		ID   string `json:"subscriptionId"`
		Name string `json:"displayName"`
	}](ctx, c, "/subscriptions", azureSubscriptionsVersion, nil)
	if err != nil {
		return o, err
	}

	for _, s := range subs {
		o.Subscriptions = append(o.Subscriptions, azureSubscription(s))
	}

	groups, err := azureList[azureResourceGroup](ctx, c, "/subscriptions/"+c.subscription+"/resourcegroups", azureResourcesVersion, nil)
	if err != nil {
		return o, err
	}

	o.ResourceGroups = groups

	o.Locations = c.locations(ctx, &o.Warnings)
	o.Location = c.location

	if o.Location == "" && len(groups) > 0 {
		o.Location = strings.ToLower(groups[0].Location)
		o.Warnings = append(o.Warnings, "no location given: listing "+o.Location+", resource group "+groups[0].Name+"'s")
	}

	if o.Location == "" {
		o.Warnings = append(o.Warnings, "no resource group in this subscription: make one first")
		o.Sizes, o.Networks, o.SecurityGroups, o.Images = []azureSize{}, []azureNetwork{}, []azureSecurityGroup{}, []azureImage{}

		return o, nil
	}

	o.Sizes = c.sizes(ctx, o.Location, &o.Warnings)
	o.Networks = c.networks(ctx, o.Location, &o.Warnings)
	o.SecurityGroups = c.securityGroups(ctx, o.Location, &o.Warnings)
	o.Images = c.images(ctx, o.Location, &o.Warnings)

	if version != "" && !slices.ContainsFunc(o.Images, func(i azureImage) bool { return i.TalosVersion == version }) {
		o.Warnings = append(o.Warnings, "no image tagged with Talos "+version+": pick one of your own or make one")
	}

	return o, nil
}

// azureWarnRefusal turns a refused listing into a warning, and keeps any other error.
func azureWarnRefusal(err error, warnings *[]string) error {
	var e *azureError
	if errors.As(err, &e) && (e.status == http.StatusForbidden || e.code == azureCodeAuthorizationFailed) {
		*warnings = append(*warnings, e.Error())

		return nil
	}

	return err
}

// listOrWarn is azureList whose failures are warnings: the options are what can be read.
func listOrWarn[T any](ctx context.Context, c *azureClient, path, version string, query url.Values, warnings *[]string) []T {
	items, err := azureList[T](ctx, c, path, version, query)
	if err != nil {
		if azureWarnRefusal(err, warnings) != nil {
			*warnings = append(*warnings, friendlyError(err))
		}

		return []T{}
	}

	return items
}

func (c *azureClient) locations(ctx context.Context, warnings *[]string) []azureLocation {
	all := listOrWarn[struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Metadata    struct {
			RegionType string `json:"regionType"`
		} `json:"metadata"`
	}](ctx, c, "/subscriptions/"+c.subscription+"/locations", azureSubscriptionsVersion, nil, warnings)

	out := []azureLocation{}

	for _, l := range all {
		if l.Metadata.RegionType != "Logical" {
			out = append(out, azureLocation{Name: l.Name, DisplayName: l.DisplayName})
		}
	}

	slices.SortFunc(out, func(a, b azureLocation) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// sizes lists the location's VM sizes, by vCPUs then memory.
func (c *azureClient) sizes(ctx context.Context, location string, warnings *[]string) []azureSize {
	skus := listOrWarn[azureAPISKU](ctx, c, "/subscriptions/"+c.subscription+"/providers/Microsoft.Compute/skus", azureSKUsVersion,
		url.Values{"$filter": {"location eq '" + location + "'"}}, warnings)

	out := []azureSize{}

	for _, s := range skus {
		if size, ok := s.size(); ok {
			out = append(out, size)
		}
	}

	slices.SortStableFunc(out, func(a, b azureSize) int {
		return cmp.Or(cmp.Compare(a.VCPUs, b.VCPUs), cmp.Compare(a.MemoryGB, b.MemoryGB), strings.Compare(a.Name, b.Name))
	})

	return out
}

func (c *azureClient) networks(ctx context.Context, location string, warnings *[]string) []azureNetwork {
	all := listOrWarn[azureAPINetwork](ctx, c, "/subscriptions/"+c.subscription+"/providers/Microsoft.Network/virtualNetworks", azureNetworkVersion, nil, warnings)
	out := []azureNetwork{}

	for _, n := range all {
		if !azureSameLocation(n.Location, location) {
			continue
		}

		network := azureNetwork{ID: n.ID, Name: n.Name, ResourceGroup: azureResourceGroupOf(n.ID), Location: n.Location, Subnets: []azureSubnet{}}

		for _, s := range n.Properties.Subnets {
			subnet := azureSubnet{ID: s.ID, Name: s.Name, CIDR: s.Properties.AddressPrefix, NATGateway: s.Properties.NATGateway != nil}
			if s.Properties.NetworkSecurityGroup != nil {
				subnet.NSGID = s.Properties.NetworkSecurityGroup.ID
			}

			network.Subnets = append(network.Subnets, subnet)
		}

		out = append(out, network)
	}

	slices.SortFunc(out, func(a, b azureNetwork) int {
		return cmp.Or(strings.Compare(a.ResourceGroup, b.ResourceGroup), strings.Compare(a.Name, b.Name))
	})

	return out
}

func (c *azureClient) securityGroups(ctx context.Context, location string, warnings *[]string) []azureSecurityGroup {
	all := listOrWarn[azureAPISecurityGroup](ctx, c, "/subscriptions/"+c.subscription+"/providers/Microsoft.Network/networkSecurityGroups",
		azureNetworkVersion, nil, warnings)
	out := []azureSecurityGroup{}

	for _, g := range all {
		if azureSameLocation(g.Location, location) {
			out = append(out, azureSecurityGroup{ID: g.ID, Name: g.Name, ResourceGroup: azureResourceGroupOf(g.ID), WideOpen: g.wideOpen()})
		}
	}

	slices.SortFunc(out, func(a, b azureSecurityGroup) int { return strings.Compare(a.Name, b.Name) })

	return out
}

// images lists the subscription's Linux managed images of the location and its own galleries'
// Linux image versions published there, tagged ichor-talos-version first.
func (c *azureClient) images(ctx context.Context, location string, warnings *[]string) []azureImage {
	out := []azureImage{}

	base := "/subscriptions/" + c.subscription + "/providers/Microsoft.Compute/"

	for _, i := range listOrWarn[azureAPIImage](ctx, c, base+"images", azureComputeVersion, nil, warnings) {
		if strings.EqualFold(i.Properties.StorageProfile.OSDisk.OSType, "Linux") && azureSameLocation(i.Location, location) {
			out = append(out, azureImage{
				ID: i.ID, Name: i.Name, ResourceGroup: azureResourceGroupOf(i.ID), Source: azureImageManaged, Arch: bootArchAMD64,
				Generation:   cmp.Or(i.Properties.HyperVGeneration, azureGenV1),
				TalosVersion: i.Tags[azureTagTalosVersion], Schematic: i.Tags[azureTagSchematic],
			})
		}
	}

	out = append(out, c.galleryImages(ctx, base, location, warnings)...)

	slices.SortStableFunc(out, func(a, b azureImage) int {
		return cmp.Or(cmp.Compare(azureUntagged(a), azureUntagged(b)), strings.Compare(b.TalosVersion, a.TalosVersion), strings.Compare(a.Name, b.Name))
	})

	return out
}

func azureUntagged(i azureImage) int {
	if i.TalosVersion != "" {
		return 0
	}

	return 1
}

// galleryImages lists the Linux image versions of the subscription's galleries (the first
// azureMaxGalleries, and azureMaxGalleryImages definitions in all) published in location.
func (c *azureClient) galleryImages(ctx context.Context, base, location string, warnings *[]string) []azureImage {
	galleries := listOrWarn[azureAPIImage](ctx, c, base+"galleries", azureComputeVersion, nil, warnings)
	if len(galleries) > azureMaxGalleries {
		*warnings = append(*warnings, "more than "+strconv.Itoa(azureMaxGalleries)+" galleries: the first ones are listed")
		galleries = galleries[:azureMaxGalleries]
	}

	out, read := []azureImage{}, 0

	for _, g := range galleries {
		for _, def := range listOrWarn[azureAPIImage](ctx, c, g.ID+"/images", azureComputeVersion, nil, warnings) {
			if !strings.EqualFold(def.Properties.OSType, "Linux") || read >= azureMaxGalleryImages {
				continue
			}

			read++

			arch := azureTalosArch(def.Properties.Architecture)

			for _, v := range listOrWarn[azureAPIImage](ctx, c, def.ID+"/versions", azureComputeVersion, nil, warnings) {
				if arch == "" || !azurePublishedIn(v, location) {
					continue
				}

				out = append(out, azureImage{
					ID: v.ID, Name: g.Name + "/" + def.Name + "/" + v.Name, ResourceGroup: azureResourceGroupOf(v.ID), Source: azureImageGallery,
					Arch: arch, Generation: cmp.Or(def.Properties.HyperVGeneration, azureGenV1),
					TalosVersion: cmp.Or(v.Tags[azureTagTalosVersion], def.Tags[azureTagTalosVersion]),
					Schematic:    cmp.Or(v.Tags[azureTagSchematic], def.Tags[azureTagSchematic]),
				})
			}
		}
	}

	return out
}

// azurePublishedIn: a gallery image version is replicated to location.
func azurePublishedIn(v azureAPIImage, location string) bool {
	regions := v.Properties.PublishingProfile.TargetRegions
	if len(regions) == 0 {
		return azureSameLocation(v.Location, location)
	}

	return slices.ContainsFunc(regions, func(r struct {
		Name string `json:"name"`
	}) bool {
		return azureSameLocation(r.Name, location)
	})
}

func demoAzureOptions() azureOptions {
	const (
		sub      = "00000000-0000-0000-0000-000000000000"
		location = "westeurope"
		rg       = "/subscriptions/" + sub + "/resourceGroups/talos/providers/"
		vnet     = rg + "Microsoft.Network/virtualNetworks/talos-vnet"
		nsg      = rg + "Microsoft.Network/networkSecurityGroups/talos-nodes"
	)

	return azureOptions{
		Subscription: sub, Location: location,
		Subscriptions:  []azureSubscription{{ID: sub, Name: "Scratch"}},
		Locations:      []azureLocation{{Name: "northeurope", DisplayName: "North Europe"}, {Name: location, DisplayName: "West Europe"}},
		ResourceGroups: []azureResourceGroup{{Name: "talos", Location: location}},
		Sizes: []azureSize{
			{Name: "Standard_B2s", VCPUs: 2, MemoryGB: 4, Arch: bootArchAMD64, Generations: []string{azureGenV1, azureGenV2}},
			{Name: "Standard_D2ps_v5", VCPUs: 2, MemoryGB: 8, Arch: bootArchARM64, Generations: []string{azureGenV2}},
			{Name: "Standard_D2s_v5", VCPUs: 2, MemoryGB: 8, Arch: bootArchAMD64, Generations: []string{azureGenV1, azureGenV2}},
		},
		Networks: []azureNetwork{{
			ID: vnet, Name: "talos-vnet", ResourceGroup: "talos", Location: location,
			Subnets: []azureSubnet{{ID: vnet + "/subnets/nodes", Name: "nodes", CIDR: "10.0.0.0/24", NSGID: nsg}},
		}},
		SecurityGroups: []azureSecurityGroup{{ID: nsg, Name: "talos-nodes", ResourceGroup: "talos"}},
		Images: []azureImage{{
			ID: rg + "Microsoft.Compute/images/talos-" + demoTalosVersion, Name: "talos-" + demoTalosVersion, ResourceGroup: "talos",
			Source: azureImageManaged, Arch: bootArchAMD64, Generation: azureGenV1, TalosVersion: demoTalosVersion,
		}},
		Warnings: []string{},
	}
}
