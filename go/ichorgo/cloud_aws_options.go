package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// What an EC2 instance for a new node can be made from: the region's instance types, subnets
// and security groups, and the Talos AMIs (the official ones of the cluster's version, from the
// release's cloud-images.json, and the account's own made by the app).

const (
	awsInstanceTypesPage = "100"
	// awsMaxTypePages bounds the instance type pages (some 900 types, 100 a page).
	awsMaxTypePages  = 20
	talosImagesLimit = 1 << 20
)

var (
	// talosCloudImagesURL is a Talos release's list of its official cloud images (a redirect to
	// a signed URL). Replaced in tests.
	talosCloudImagesURL = func(version string) string {
		return "https://github.com/siderolabs/talos/releases/download/" + url.PathEscape(version) + "/cloud-images.json"
	}
	// newTalosReleaseHTTP follows GitHub's redirect, HTTPS only. Replaced in tests.
	newTalosReleaseHTTP = func() *http.Client {
		return httpx.New(httpx.Opts{Timeout: oidcHTTPTimeout, FollowRedirects: true})
	}

	// talosCloudImagesCache holds each release's list for the process: a release never changes.
	talosCloudImagesCache sync.Map // version → []talosCloudImage

	talosVersionPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
)

// awsOptions is what an instance can be created from, in one region. Hourly prices are asked
// one type at a time (AWSPrice).
type awsOptions struct {
	Region         string             `json:"region"`
	Regions        []string           `json:"regions"`
	Types          []awsType          `json:"types"`
	Subnets        []awsSubnet        `json:"subnets"`
	SecurityGroups []awsSecurityGroup `json:"securityGroups"`
	Images         []awsImage         `json:"images"`
	Warnings       []string           `json:"warnings"`
}

type awsType struct {
	Name      string  `json:"name"`
	VCPUs     int     `json:"vcpus"`
	MemoryGB  float64 `json:"memoryGB"`
	Arch      string  `json:"arch"` // amd64 or arm64
	Burstable bool    `json:"burstable"`
}

type awsSubnet struct {
	ID         string `json:"id"`
	VPCID      string `json:"vpcId"`
	VPCDefault bool   `json:"vpcDefault"`
	CIDR       string `json:"cidr"`
	AZ         string `json:"az"`
	Public     bool   `json:"public"` // assigns a public IPv4 on launch
	Name       string `json:"name"`
}

type awsSecurityGroup struct {
	ID          string `json:"id"`
	VPCID       string `json:"vpcId"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type awsImage struct {
	ID           string `json:"id"`
	Arch         string `json:"arch"`
	Source       string `json:"source"` // official or custom
	Name         string `json:"name"`
	TalosVersion string `json:"talosVersion"`
	Schematic    string `json:"schematic"`
}

// The API's objects, as far as they are read.
type awsTag struct {
	Key   string `xml:"key"`
	Value string `xml:"value"`
}

func awsTagValue(tags []awsTag, key string) string {
	for _, t := range tags {
		if t.Key == key {
			return t.Value
		}
	}

	return ""
}

type awsAPIType struct {
	Name      string   `xml:"instanceType"`
	VCPUs     int      `xml:"vCpuInfo>defaultVCpus"`
	MemoryMiB int64    `xml:"memoryInfo>sizeInMiB"`
	Archs     []string `xml:"processorInfo>supportedArchitectures>item"`
	Burstable bool     `xml:"burstablePerformanceSupported"`
}

// arch is the Talos architecture the type runs, "" for none (i386, Mac).
func (t awsAPIType) arch() string {
	for _, a := range t.Archs {
		if arch := awsTalosArch(a); arch != "" {
			return arch
		}
	}

	return ""
}

type awsAPISubnet struct {
	ID       string   `xml:"subnetId"`
	VPCID    string   `xml:"vpcId"`
	CIDR     string   `xml:"cidrBlock"`
	AZ       string   `xml:"availabilityZone"`
	MapPubIP bool     `xml:"mapPublicIpOnLaunch"`
	Tags     []awsTag `xml:"tagSet>item"`
}

type awsAPISecurityGroup struct {
	ID          string `xml:"groupId"`
	VPCID       string `xml:"vpcId"`
	Name        string `xml:"groupName"`
	Description string `xml:"groupDescription"`
}

type awsAPIImage struct {
	ID    string   `xml:"imageId"`
	Name  string   `xml:"name"`
	Arch  string   `xml:"architecture"`
	State string   `xml:"imageState"`
	Tags  []awsTag `xml:"tagSet>item"`
}

// awsTalosArch maps AWS's architecture names to Talos's.
func awsTalosArch(arch string) string {
	switch arch {
	case awsArchX86:
		return bootArchAMD64
	case awsArchARM:
		return bootArchARM64
	}

	return ""
}

// AWSOptions lists, in the keys' region: the regions, the current-generation instance types
// Talos runs on (no t2), the subnets with their VPC and whether they assign a public IPv4, the
// security groups, and the Talos AMIs: the official ones of the cluster's Talos version (both
// architectures) and the account's own tagged ichor-talos-version. A cloud-images.json that does
// not answer, or a version that cannot be read, is a warning, never an error. The demo context
// answers with samples, without keys. JSON: {"region","regions","types":[{"name","vcpus",
// "memoryGB","arch","burstable"}],"subnets":[{"id","vpcId","vpcDefault","cidr","az","public",
// "name"}],"securityGroups":[{"id","vpcId","name","description"}],"images":[{"id","arch",
// "source","name","talosVersion","schematic"}],"warnings"}.
func AWSOptions(configYAML, contextName, credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoAWSOptions())
	}

	c, err := newAWSClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsOptionsTimeout)
	defer cancel()

	version, warning := awsClusterTalosVersion(configYAML, contextName)

	options, err := c.options(ctx, version)
	if err != nil {
		return "", err
	}

	if warning != "" {
		options.Warnings = append([]string{warning}, options.Warnings...)
	}

	return toJSON(options)
}

// awsClusterTalosVersion is the cluster's Talos version, or a warning saying why not.
func awsClusterTalosVersion(configYAML, contextName string) (string, string) {
	const without = "no official Talos AMI listed"

	if strings.TrimSpace(configYAML) == "" {
		return "", "no cluster given: " + without
	}

	media, err := cloudBootMedia(configYAML, contextName, "")
	if err != nil {
		return "", "the cluster's Talos version cannot be read (" + friendlyError(err) + "): " + without
	}

	return media.TalosVersion, ""
}

func (c *awsClient) options(ctx context.Context, version string) (awsOptions, error) {
	var regions struct {
		Names []string `xml:"regionInfo>item>regionName"`
	}

	if err := c.ec2(ctx, "DescribeRegions", nil, &regions); err != nil {
		return awsOptions{}, err
	}

	types, err := c.instanceTypes(ctx)
	if err != nil {
		return awsOptions{}, err
	}

	subnets, err := c.subnets(ctx)
	if err != nil {
		return awsOptions{}, err
	}

	var groups struct {
		Items []awsAPISecurityGroup `xml:"securityGroupInfo>item"`
	}

	if err := c.ec2(ctx, "DescribeSecurityGroups", nil, &groups); err != nil {
		return awsOptions{}, err
	}

	images, err := c.customImages(ctx)
	if err != nil {
		return awsOptions{}, err
	}

	o := awsOptions{
		Region: c.region, Regions: regions.Names, Types: types, Subnets: subnets,
		SecurityGroups: make([]awsSecurityGroup, 0, len(groups.Items)), Warnings: []string{},
	}

	for _, g := range groups.Items {
		o.SecurityGroups = append(o.SecurityGroups, awsSecurityGroup(g))
	}

	slices.Sort(o.Regions)

	if version != "" {
		official, err := talosAWSImages(ctx, version, c.region)

		switch {
		case err != nil:
			o.Warnings = append(o.Warnings, "the official Talos AMIs cannot be listed ("+friendlyError(err)+"): pick an AMI of your own")
		case len(official) == 0:
			o.Warnings = append(o.Warnings, fmt.Sprintf("Talos %s has no official AMI in %s: pick another region, or an AMI of your own", version, c.region))
		}

		images = append(official, images...)
	}

	o.Images = images
	if o.Images == nil {
		o.Images = []awsImage{}
	}

	return o, nil
}

// instanceTypes lists the current generation, every page. T2 types do not run Talos.
func (c *awsClient) instanceTypes(ctx context.Context) ([]awsType, error) {
	params := url.Values{"Filter.1.Name": {"current-generation"}, "Filter.1.Value.1": {"true"}, "MaxResults": {awsInstanceTypesPage}}
	out := []awsType{}

	for range awsMaxTypePages {
		var page struct {
			Types     []awsAPIType `xml:"instanceTypeSet>item"`
			NextToken string       `xml:"nextToken"`
		}

		if err := c.ec2(ctx, "DescribeInstanceTypes", params, &page); err != nil {
			return nil, err
		}

		for _, t := range page.Types {
			if arch := t.arch(); arch != "" && !strings.HasPrefix(t.Name, "t2.") {
				out = append(out, awsType{Name: t.Name, VCPUs: t.VCPUs, MemoryGB: float64(t.MemoryMiB) / 1024, Arch: arch, Burstable: t.Burstable}) //nolint:mnd // MiB → GiB
			}
		}

		if page.NextToken == "" {
			break
		}

		params = url.Values{"MaxResults": {awsInstanceTypesPage}, "NextToken": {page.NextToken},
			"Filter.1.Name": {"current-generation"}, "Filter.1.Value.1": {"true"}}
	}

	slices.SortStableFunc(out, func(a, b awsType) int {
		return cmp.Or(cmp.Compare(a.VCPUs, b.VCPUs), cmp.Compare(a.MemoryGB, b.MemoryGB), strings.Compare(a.Name, b.Name))
	})

	return out, nil
}

// subnets lists the region's subnets, with whether their VPC is the default one.
func (c *awsClient) subnets(ctx context.Context) ([]awsSubnet, error) {
	var subnets struct {
		Items []awsAPISubnet `xml:"subnetSet>item"`
	}

	if err := c.ec2(ctx, "DescribeSubnets", nil, &subnets); err != nil {
		return nil, err
	}

	var vpcs struct {
		Items []struct {
			ID      string `xml:"vpcId"`
			Default bool   `xml:"isDefault"`
		} `xml:"vpcSet>item"`
	}

	if err := c.ec2(ctx, "DescribeVpcs", nil, &vpcs); err != nil {
		return nil, err
	}

	defaults := map[string]bool{}
	for _, v := range vpcs.Items {
		defaults[v.ID] = v.Default
	}

	out := make([]awsSubnet, 0, len(subnets.Items))

	for _, s := range subnets.Items {
		out = append(out, awsSubnet{
			ID: s.ID, VPCID: s.VPCID, VPCDefault: defaults[s.VPCID], CIDR: s.CIDR, AZ: s.AZ, Public: s.MapPubIP, Name: awsTagValue(s.Tags, "Name"),
		})
	}

	slices.SortStableFunc(out, func(a, b awsSubnet) int {
		return cmp.Or(strings.Compare(a.VPCID, b.VPCID), strings.Compare(a.AZ, b.AZ), strings.Compare(a.ID, b.ID))
	})

	return out, nil
}

// customImages lists the account's own Talos AMIs, tagged by the app when it made them.
func (c *awsClient) customImages(ctx context.Context) ([]awsImage, error) {
	params := url.Values{"Owner.1": {"self"}, "Filter.1.Name": {"tag-key"}, "Filter.1.Value.1": {awsTagTalosVersion}}

	var images struct {
		Items []awsAPIImage `xml:"imagesSet>item"`
	}

	if err := c.ec2(ctx, "DescribeImages", params, &images); err != nil {
		return nil, err
	}

	out := make([]awsImage, 0, len(images.Items))

	for _, i := range images.Items {
		if i.State != "" && i.State != "available" {
			continue
		}

		out = append(out, awsImage{
			ID: i.ID, Arch: awsTalosArch(i.Arch), Source: awsImageCustom, Name: i.Name,
			TalosVersion: awsTagValue(i.Tags, awsTagTalosVersion), Schematic: awsTagValue(i.Tags, awsTagSchematic),
		})
	}

	return out, nil
}

// talosCloudImage is one entry of a release's cloud-images.json.
type talosCloudImage struct {
	Cloud   string `json:"cloud"`
	Version string `json:"version"`
	Region  string `json:"region"`
	Arch    string `json:"arch"`
	Type    string `json:"type"`
	ID      string `json:"id"`
}

// talosAWSImages is the official AMIs of a Talos release in a region, amd64 first.
func talosAWSImages(ctx context.Context, version, region string) ([]awsImage, error) {
	all, err := talosCloudImages(ctx, version)
	if err != nil {
		return nil, err
	}

	var out []awsImage

	for _, i := range all {
		if i.Cloud == "aws" && i.Region == region && (i.Arch == bootArchAMD64 || i.Arch == bootArchARM64) {
			out = append(out, awsImage{
				ID: i.ID, Arch: i.Arch, Source: awsImageOfficial, Name: "Talos " + version + " (" + i.Arch + ")", TalosVersion: version,
			})
		}
	}

	slices.SortStableFunc(out, func(a, b awsImage) int { return strings.Compare(a.Arch, b.Arch) })

	return out, nil
}

// talosCloudImages reads a release's cloud-images.json, once per process.
func talosCloudImages(ctx context.Context, version string) ([]talosCloudImage, error) {
	if !talosVersionPattern.MatchString(version) {
		return nil, fmt.Errorf("%q is not a Talos version", version)
	}

	if cached, ok := talosCloudImagesCache.Load(version); ok {
		return cached.([]talosCloudImage), nil //nolint:forcetypeassert // only this function stores
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, talosCloudImagesURL(version), nil)
	if err != nil {
		return nil, err
	}

	resp, err := newTalosReleaseHTTP().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Talos %s release answered %d for its cloud images", version, resp.StatusCode)
	}

	var images []talosCloudImage
	if err := json.NewDecoder(io.LimitReader(resp.Body, talosImagesLimit)).Decode(&images); err != nil {
		return nil, errors.New("the Talos " + version + " release's cloud-images.json cannot be read")
	}

	talosCloudImagesCache.Store(version, images)

	return images, nil
}

func demoAWSOptions() awsOptions {
	const (
		region = "eu-west-1"
		vpc    = "vpc-0123456789abcdef0"
	)

	return awsOptions{
		Region: region, Regions: []string{"eu-central-1", region, "us-east-1"},
		Types: []awsType{
			{Name: "t3.small", VCPUs: 2, MemoryGB: 2, Arch: bootArchAMD64, Burstable: true},
			{Name: "t4g.small", VCPUs: 2, MemoryGB: 2, Arch: bootArchARM64, Burstable: true},
			{Name: "m6i.large", VCPUs: 2, MemoryGB: 8, Arch: bootArchAMD64},
		},
		Subnets: []awsSubnet{
			{ID: "subnet-0123456789abcdef0", VPCID: vpc, VPCDefault: true, CIDR: "172.31.0.0/20", AZ: region + "a", Public: true},
			{ID: "subnet-0123456789abcdef1", VPCID: vpc, VPCDefault: true, CIDR: "172.31.16.0/20", AZ: region + "b", Name: "private"},
		},
		SecurityGroups: []awsSecurityGroup{{ID: "sg-0123456789abcdef0", VPCID: vpc, Name: "talos-nodes", Description: "Talos and Kubernetes APIs"}},
		Images: []awsImage{
			{ID: "ami-0123456789abcdef0", Arch: bootArchAMD64, Source: awsImageOfficial, Name: "Talos " + demoTalosVersion + " (amd64)", TalosVersion: demoTalosVersion},
			{ID: "ami-0123456789abcdef1", Arch: bootArchARM64, Source: awsImageOfficial, Name: "Talos " + demoTalosVersion + " (arm64)", TalosVersion: demoTalosVersion},
		},
		Warnings: []string{},
	}
}
