package ichorgo

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Sample keys from AWS's documentation, an invented account and invented resources.
const (
	awsTestKeyID    = "AKIAIOSFODNN7EXAMPLE"
	awsTestSecret   = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	awsTestToken    = "sample-session-token"
	awsTestAccount  = "123456789012"
	awsTestCred     = `{"accessKeyId":"` + awsTestKeyID + `","secretAccessKey":"` + awsTestSecret + `","sessionToken":"` + awsTestToken + `","region":"eu-west-1"}`
	awsTestAMI      = "ami-0123456789abcdef0"
	awsTestAMIARM   = "ami-0123456789abcdef1"
	awsTestAMIOwn   = "ami-0123456789abcdef9"
	awsTestSubnet   = "subnet-0123456789abcdef0"
	awsTestVPC      = "vpc-0123456789abcdef0"
	awsTestSG       = "sg-0123456789abcdef0"
	awsTestSGOther  = "sg-0123456789abcdef1"
	awsTestInstance = "i-0123456789abcdef0"
)

var awsTestAuthPattern = regexp.MustCompile(`^AWS4-HMAC-SHA256 Credential=` + awsTestKeyID + `/\d{8}/([a-z0-9-]+)/([a-z0-9]+)/aws4_request, SignedHeaders=[a-z0-9;-]+, Signature=[0-9a-f]{64}$`)

// fakeEC2 is the part of EC2, STS, the Price List and the Talos release the app calls. It
// records every write and every signature scope.
type fakeEC2 struct {
	mu sync.Mutex

	noRunRight    bool
	noCloudImages bool
	noPricing     bool
	imageArch     string // the AMI's, x86_64 by default
	createStatus  int    // the status RunInstances answers, after creating when >= 500
	endState      string // where the instance ends up, running by default
	privateOnly   bool
	onRunning     func()

	scopes      []string // region/service of each signed call
	actions     []string
	dryRuns     []url.Values
	runs        []url.Values
	created     bool
	terminated  []string
	imageFetch  int
	priceFilter string
}

func startFakeEC2(t *testing.T, f *fakeEC2) {
	t.Helper()

	if f.imageArch == "" {
		f.imageArch = awsArchX86
	}

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client, ec2, sts, pricing, images, release := cloudHTTP, awsEC2Endpoint, awsSTSEndpoint, awsPricingEndpoint, talosCloudImagesURL, newTalosReleaseHTTP
	cloudHTTP = func() *http.Client { return srv.Client() }
	newTalosReleaseHTTP = func() *http.Client { return srv.Client() }
	awsEC2Endpoint = func(string) string { return srv.URL }
	awsSTSEndpoint = func(string) string { return srv.URL }
	awsPricingEndpoint = func() string { return srv.URL }
	talosCloudImagesURL = func(version string) string { return srv.URL + "/releases/" + version + "/cloud-images.json" }

	talosCloudImagesCache.Clear()

	t.Cleanup(func() {
		cloudHTTP, awsEC2Endpoint, awsSTSEndpoint, awsPricingEndpoint, talosCloudImagesURL, newTalosReleaseHTTP = client, ec2, sts, pricing, images, release
		talosCloudImagesCache.Clear()
	})
}

func awsFail(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Response><Errors><Error><Code>%s</Code><Message>%s</Message></Error></Errors><RequestID>sample</RequestID></Response>`, code, message)
}

func (f *fakeEC2) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.HasPrefix(r.URL.Path, "/releases/"):
		// GitHub's redirect to a signed URL.
		http.Redirect(w, r, "/signed"+strings.TrimPrefix(r.URL.Path, "/releases"), http.StatusFound)

		return
	case strings.HasPrefix(r.URL.Path, "/signed/"):
		f.cloudImages(w)

		return
	}

	m := awsTestAuthPattern.FindStringSubmatch(r.Header.Get("Authorization"))
	if m == nil || r.Header.Get("X-Amz-Security-Token") != awsTestToken || r.Header.Get("X-Amz-Date") == "" {
		awsFail(w, http.StatusUnauthorized, "AuthFailure", "AWS was not able to validate the provided access credentials")

		return
	}

	f.scopes = append(f.scopes, m[1]+"/"+m[2])

	if r.Header.Get("X-Amz-Target") == "AWSPriceListService.GetProducts" {
		f.pricing(w, r)

		return
	}

	if err := r.ParseForm(); err != nil {
		awsFail(w, http.StatusBadRequest, "MalformedQueryString", err.Error())

		return
	}

	action := r.PostForm.Get("Action")
	f.actions = append(f.actions, action)

	if action == "GetCallerIdentity" {
		fmt.Fprintf(w, `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/"><GetCallerIdentityResult><Arn>arn:aws:iam::%[1]s:user/ichor</Arn><UserId>AIDASAMPLE</UserId><Account>%[1]s</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`, awsTestAccount)

		return
	}

	if r.PostForm.Get("Version") != awsEC2Version {
		awsFail(w, http.StatusBadRequest, "InvalidAction", "wrong version")

		return
	}

	f.ec2(w, action, r.PostForm)
}

func (f *fakeEC2) ec2(w http.ResponseWriter, action string, form url.Values) {
	const ns = ` xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"`

	switch action {
	case "DescribeRegions":
		fmt.Fprint(w, `<DescribeRegionsResponse`+ns+`><regionInfo><item><regionName>us-east-1</regionName></item><item><regionName>eu-west-1</regionName></item></regionInfo></DescribeRegionsResponse>`)
	case "DescribeInstanceTypes":
		fmt.Fprint(w, `<DescribeInstanceTypesResponse`+ns+`>`+awsTestTypes(form)+`</DescribeInstanceTypesResponse>`)
	case "DescribeVpcs":
		fmt.Fprint(w, `<DescribeVpcsResponse`+ns+`><vpcSet><item><vpcId>`+awsTestVPC+`</vpcId><isDefault>true</isDefault></item><item><vpcId>vpc-0123456789abcdef1</vpcId><isDefault>false</isDefault></item></vpcSet></DescribeVpcsResponse>`)
	case "DescribeSubnets":
		fmt.Fprint(w, `<DescribeSubnetsResponse`+ns+`><subnetSet>`+awsTestSubnets(form)+`</subnetSet></DescribeSubnetsResponse>`)
	case "DescribeSecurityGroups":
		fmt.Fprint(w, `<DescribeSecurityGroupsResponse`+ns+`><securityGroupInfo>`+awsTestGroups(form)+`</securityGroupInfo></DescribeSecurityGroupsResponse>`)
	case "DescribeImages":
		fmt.Fprint(w, `<DescribeImagesResponse`+ns+`><imagesSet>`+f.images(form)+`</imagesSet></DescribeImagesResponse>`)
	case "RunInstances":
		f.run(w, form)
	case "DescribeInstances":
		fmt.Fprint(w, `<DescribeInstancesResponse`+ns+`><reservationSet>`+f.instances(form)+`</reservationSet></DescribeInstancesResponse>`)
	case "TerminateInstances":
		f.terminated = append(f.terminated, form.Get("InstanceId.1"))
		fmt.Fprint(w, `<TerminateInstancesResponse`+ns+`><instancesSet><item><instanceId>`+form.Get("InstanceId.1")+`</instanceId></item></instancesSet></TerminateInstancesResponse>`)
	default:
		awsFail(w, http.StatusBadRequest, "InvalidAction", action)
	}
}

// awsTestTypes answers two pages, or the one type asked by name.
func awsTestTypes(form url.Values) string {
	typ := func(name, archs string, vcpus, mib int, burstable bool) string {
		return fmt.Sprintf(`<item><instanceType>%s</instanceType><currentGeneration>true</currentGeneration><processorInfo><supportedArchitectures>%s</supportedArchitectures></processorInfo><vCpuInfo><defaultVCpus>%d</defaultVCpus></vCpuInfo><memoryInfo><sizeInMiB>%d</sizeInMiB></memoryInfo><burstablePerformanceSupported>%t</burstablePerformanceSupported></item>`,
			name, archs, vcpus, mib, burstable)
	}

	x86, arm := `<item>x86_64</item>`, `<item>arm64</item>`
	all := map[string]string{
		"t3.small":   typ("t3.small", x86, 2, 2048, true),
		"t2.micro":   typ("t2.micro", `<item>i386</item>`+x86, 1, 1024, true),
		"mac1.metal": typ("mac1.metal", `<item>x86_64_mac</item>`, 12, 32768, false),
		"t4g.small":  typ("t4g.small", arm, 2, 2048, true),
		"m6i.large":  typ("m6i.large", x86, 2, 8192, false),
	}

	if name := form.Get("InstanceType.1"); name != "" {
		return `<instanceTypeSet>` + all[name] + `</instanceTypeSet>`
	}

	if form.Get("Filter.1.Name") != "current-generation" || form.Get("MaxResults") == "" {
		return `<instanceTypeSet/>`
	}

	if form.Get("NextToken") == "page-2" {
		return `<instanceTypeSet>` + all["t4g.small"] + all["m6i.large"] + `</instanceTypeSet>`
	}

	return `<instanceTypeSet>` + all["t2.micro"] + all["mac1.metal"] + all["t3.small"] + `</instanceTypeSet><nextToken>page-2</nextToken>`
}

func awsTestSubnets(form url.Values) string {
	subnets := map[string]string{
		awsTestSubnet:              `<item><subnetId>` + awsTestSubnet + `</subnetId><vpcId>` + awsTestVPC + `</vpcId><cidrBlock>172.31.0.0/20</cidrBlock><availabilityZone>eu-west-1a</availabilityZone><mapPublicIpOnLaunch>true</mapPublicIpOnLaunch><tagSet><item><key>Name</key><value>nodes</value></item></tagSet></item>`,
		"subnet-0123456789abcdef1": `<item><subnetId>subnet-0123456789abcdef1</subnetId><vpcId>vpc-0123456789abcdef1</vpcId><cidrBlock>10.0.0.0/24</cidrBlock><availabilityZone>eu-west-1b</availabilityZone><mapPublicIpOnLaunch>false</mapPublicIpOnLaunch></item>`,
	}

	if id := form.Get("SubnetId.1"); id != "" {
		return subnets[id]
	}

	return subnets[awsTestSubnet] + subnets["subnet-0123456789abcdef1"]
}

func awsTestGroups(form url.Values) string {
	groups := map[string]string{
		awsTestSG:      `<item><groupId>` + awsTestSG + `</groupId><vpcId>` + awsTestVPC + `</vpcId><groupName>talos</groupName><groupDescription>Talos nodes</groupDescription></item>`,
		awsTestSGOther: `<item><groupId>` + awsTestSGOther + `</groupId><vpcId>vpc-0123456789abcdef1</vpcId><groupName>other</groupName><groupDescription>another VPC</groupDescription></item>`,
	}

	if form.Get("GroupId.1") == "" {
		return groups[awsTestSG] + groups[awsTestSGOther]
	}

	var out string

	for i := 1; form.Get(fmt.Sprintf("GroupId.%d", i)) != ""; i++ {
		out += groups[form.Get(fmt.Sprintf("GroupId.%d", i))]
	}

	return out
}

func (f *fakeEC2) images(form url.Values) string {
	if form.Get("Owner.1") == "self" && form.Get("Filter.1.Name") == "tag-key" && form.Get("Filter.1.Value.1") == awsTagTalosVersion {
		return `<item><imageId>` + awsTestAMIOwn + `</imageId><name>talos-own</name><architecture>arm64</architecture><imageState>available</imageState><tagSet><item><key>ichor-talos-version</key><value>v1.14.2</value></item><item><key>ichor-schematic</key><value>sample-schematic</value></item></tagSet></item>`
	}

	if form.Get("ImageId.1") == awsTestAMI {
		return `<item><imageId>` + awsTestAMI + `</imageId><architecture>` + f.imageArch + `</architecture><imageState>available</imageState></item>`
	}

	return ""
}

func (f *fakeEC2) run(w http.ResponseWriter, form url.Values) {
	if form.Get("DryRun") == "true" {
		f.dryRuns = append(f.dryRuns, form)

		if f.noRunRight {
			awsFail(w, http.StatusForbidden, awsCodeUnauthorized, "You are not authorized to perform this operation. User: arn:aws:iam::"+awsTestAccount+":user/ichor is not authorized. Encoded authorization failure message: c2FtcGxl")

			return
		}

		awsFail(w, http.StatusPreconditionFailed, awsCodeDryRun, "Request would have succeeded, but DryRun flag is set.")

		return
	}

	f.runs = append(f.runs, form)

	if f.createStatus != 0 {
		f.created = f.createStatus >= 500
		awsFail(w, f.createStatus, "InternalError", "something broke")

		return
	}

	f.created = true

	fmt.Fprint(w, `<RunInstancesResponse xmlns="http://ec2.amazonaws.com/doc/2016-11-15/"><reservationId>r-sample</reservationId><groupSet/><instancesSet><item><instanceId>`+awsTestInstance+`</instanceId><instanceState><code>0</code><name>pending</name></instanceState></item></instancesSet></RunInstancesResponse>`)
}

func (f *fakeEC2) instances(form url.Values) string {
	if form.Get("Filter.1.Name") == "client-token" {
		if !f.created || len(f.runs) == 0 || form.Get("Filter.1.Value.1") != f.runs[0].Get("ClientToken") {
			return ""
		}

		return `<item><instancesSet><item><instanceId>` + awsTestInstance + `</instanceId><instanceState><name>pending</name></instanceState></item></instancesSet></item>`
	}

	if form.Get("InstanceId.1") != awsTestInstance {
		return ""
	}

	state, reason, address := cmpOr(f.endState, awsStateRunning), "", `<ipAddress>192.0.2.20</ipAddress>`
	if f.endState != "" {
		reason = `<stateReason><code>Server.InternalError</code><message>Server.InternalError: Internal error on launch</message></stateReason>`
	}

	if f.privateOnly {
		address = ""
	}

	if state == awsStateRunning && f.onRunning != nil {
		f.onRunning()
		f.onRunning = nil
	}

	return `<item><instancesSet><item><instanceId>` + awsTestInstance + `</instanceId><instanceState><name>` + state + `</name></instanceState>` + reason +
		`<privateIpAddress>172.31.5.20</privateIpAddress>` + address + `</item></instancesSet></item>`
}

func (f *fakeEC2) cloudImages(w http.ResponseWriter) {
	f.imageFetch++

	if f.noCloudImages {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	fmt.Fprint(w, `[
		{"cloud":"aws","version":"v1.14.2","region":"eu-west-1","arch":"amd64","type":"hvm","id":"`+awsTestAMI+`"},
		{"cloud":"aws","version":"v1.14.2","region":"eu-west-1","arch":"arm64","type":"hvm","id":"`+awsTestAMIARM+`"},
		{"cloud":"aws","version":"v1.14.2","region":"us-east-1","arch":"amd64","type":"hvm","id":"ami-0123456789abcdef2"},
		{"cloud":"gcp","version":"v1.14.2","region":"eu-west-1","arch":"amd64","type":"hvm","id":"sample-gcp"}
	]`)
}

func (f *fakeEC2) pricing(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.priceFilter = string(body)

	if f.noPricing {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"__type":"AccessDeniedException","message":"not authorized to perform pricing:GetProducts"}`)

		return
	}

	product := `{\"product\":{\"sku\":\"SAMPLE\"},\"terms\":{\"OnDemand\":{\"SAMPLE.JRTCKXETXF\":{\"priceDimensions\":{\"SAMPLE.JRTCKXETXF.6YS6EN2CT7\":{\"unit\":\"Hrs\",\"pricePerUnit\":{\"USD\":\"0.0208000000\"}}}}}}}`
	fmt.Fprint(w, `{"FormatVersion":"aws_v1","PriceList":["`+product+`"]}`)
}
