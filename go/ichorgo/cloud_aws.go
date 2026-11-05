package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// S8 step 10: AWS EC2, on the Query API (a signed form POST, XML answers) with access keys and an
// optional session token, signed by the SigV4 code of the EKS sign-in: no AWS SDK in the app. The
// app seals the keys and passes them per call. See
// https://docs.aws.amazon.com/AWSEC2/latest/APIReference/Welcome.html.

const (
	awsEC2Version     = "2016-11-15"
	awsSTSVersion     = "2011-06-15"
	awsOptionsTimeout = 60 * time.Second
	awsPriceTimeout   = 20 * time.Second

	// awsPricingRegion: the Price List API answers in us-east-1 for every region.
	awsPricingRegion = "us-east-1"

	awsArchX86 = "x86_64"
	awsArchARM = "arm64"

	awsImageOfficial = "official"
	awsImageCustom   = "custom"

	// awsTagTalosVersion marks an AMI the app made (the custom AMI child), awsTagSchematic its
	// Image Factory schematic.
	awsTagTalosVersion = "ichor-talos-version"
	awsTagSchematic    = "ichor-schematic"
	awsTagRun          = "ichor-run"
	awsTagArch         = "ichor-arch"
	awsTagCluster      = "ichor-cluster"

	// awsCheckTalosVersion is the release whose official AMI AWSTest's dry run names: any
	// existing AMI of the region does, and v1.12.0's are published in every standard region.
	awsCheckTalosVersion = "v1.12.0"
	awsCheckType         = "t3.micro"

	awsCodeDryRun       = "DryRunOperation"
	awsCodeUnauthorized = "UnauthorizedOperation"
)

// AWS endpoints, overridable in tests (as awsSTSEndpoint).
var (
	awsEC2Endpoint = func(region string) string {
		return "https://ec2." + region + ".amazonaws.com" + awsDomainSuffix(region)
	}
	awsPricingEndpoint = func() string { return "https://api.pricing." + awsPricingRegion + ".amazonaws.com" }

	errAWSNoCredential = errors.New("add AWS access keys first")
	errAWSNoRegion     = errors.New("pick an AWS region")
	errAWSNoCreate     = errors.New("these keys may not run instances: add ec2:RunInstances and ec2:CreateTags of the printed policy to their IAM user or role")

	awsRegionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d+$`)
	awsAccountPattern = regexp.MustCompile(`\b\d{12}\b`)
)

// awsError is a refusal: EC2's <Response><Errors><Error>, STS's <ErrorResponse><Error>, or
// the Price List's {"__type","message"}. Its message never carries the keys (see scrub).
type awsError struct {
	action  string // ec2:RunInstances
	code    string
	message string
	status  int
}

func (e *awsError) Error() string {
	switch e.code {
	case awsCodeUnauthorized, "AccessDenied", "AccessDeniedException":
		return "AWS refused " + e.action + ": add the actions of the printed policy to the keys' IAM user or role"
	case "AuthFailure", "InvalidClientTokenId", "SignatureDoesNotMatch", "IncompleteSignature", "UnrecognizedClientException", "InvalidAccessKeyId":
		return "AWS refused the keys: check the access key, its secret and the session token"
	case "ExpiredToken", "RequestExpired":
		return "AWS: the session token has expired: paste fresh keys"
	}

	return "AWS " + e.action + ": " + cmp.Or(e.message, e.code, http.StatusText(e.status))
}

// awsClient calls EC2, STS and the Price List with one set of keys, which stay in the signature
// (and the session token in its header): never in a URL, a message or a log.
type awsClient struct {
	creds  awsCredentials
	region string
}

// newAWSClient reads credJSON: {"accessKeyId","secretAccessKey","sessionToken","region"}.
func newAWSClient(credJSON string) (*awsClient, error) {
	var c struct {
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		SessionToken    string `json:"sessionToken"`
		Region          string `json:"region"`
	}

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &c); err != nil {
			return nil, errors.New("invalid AWS keys data") //nolint:nilerr // the parser's message may quote the secret
		}
	}

	creds := awsCredentials{strings.TrimSpace(c.AccessKeyID), strings.TrimSpace(c.SecretAccessKey), strings.TrimSpace(c.SessionToken)}
	if creds.accessKeyID == "" || creds.secretAccessKey == "" {
		return nil, errAWSNoCredential
	}

	client := &awsClient{creds: creds}

	return client, client.setRegion(c.Region)
}

// setRegion checks a region name: it goes into a host name.
func (c *awsClient) setRegion(region string) error {
	region = strings.TrimSpace(region)

	switch {
	case region == "":
		return errAWSNoRegion
	case !awsRegionPattern.MatchString(region):
		return fmt.Errorf("%q is not an AWS region (eu-west-1)", region)
	}

	c.region = region

	return nil
}

// scrub drops what a message must not carry: the keys, the account id, and the encoded
// authorization message (which decodes to the account's details).
func (c *awsClient) scrub(message string) string {
	if i := strings.Index(message, "Encoded authorization failure message"); i >= 0 {
		message = strings.TrimRight(message[:i], " .:")
	}

	for _, secret := range []string{c.creds.accessKeyID, c.creds.secretAccessKey, c.creds.sessionToken} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "…")
		}
	}

	return awsAccountPattern.ReplaceAllString(message, "ACCOUNT")
}

// The XML errors of EC2 and STS, whatever the root element.
type awsXMLErrors struct {
	Errors []awsXMLError `xml:"Errors>Error"`
	Error  awsXMLError   `xml:"Error"`
}

type awsXMLError struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// query sends one Query API call (EC2, STS) and decodes its XML answer into out.
func (c *awsClient) query(ctx context.Context, endpoint, service, action, version string, params url.Values, out any) error {
	form := url.Values{"Action": {action}, "Version": {version}}
	for k, v := range params {
		form[k] = v
	}

	body := []byte(form.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	sigV4Sign(req, sha256Hex(body), c.creds, c.region, service, time.Now())

	data, status, err := c.send(req)
	if err != nil {
		return err
	}

	if status < 200 || status > 299 {
		var e awsXMLErrors

		_ = xml.Unmarshal(data, &e) //nolint:errcheck

		first := e.Error
		if len(e.Errors) > 0 {
			first = e.Errors[0]
		}

		return &awsError{action: service + ":" + action, code: first.Code, message: c.scrub(first.Message), status: status}
	}

	if out == nil {
		return nil
	}

	return xml.Unmarshal(data, out)
}

func (c *awsClient) send(req *http.Request) ([]byte, int, error) {
	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, awsMaxBody))

	return data, resp.StatusCode, err
}

// ec2 calls one EC2 action in the client's region.
func (c *awsClient) ec2(ctx context.Context, action string, params url.Values, out any) error {
	return c.query(ctx, awsEC2Endpoint(c.region), "ec2", action, awsEC2Version, params, out)
}

type awsIdentity struct {
	Account string `xml:"GetCallerIdentityResult>Account"`
	Arn     string `xml:"GetCallerIdentityResult>Arn"`
}

// sts is GetCallerIdentity: the keys work, and whose they are. It needs no permission.
func (c *awsClient) sts(ctx context.Context) (awsIdentity, error) {
	var id awsIdentity

	err := c.query(ctx, awsSTSEndpoint(c.region), "sts", "GetCallerIdentity", awsSTSVersion, nil, &id)

	return id, err
}

// pricing is the on-demand Linux hourly price in USD of an instance type in the client's
// region, "" when the Price List does not answer in time or says nothing (a missing
// pricing:GetProducts right included): not worth failing for.
func (c *awsClient) pricing(ctx context.Context, instanceType string) string {
	return awsHourlyPrice(c.products(ctx, 1,
		[2]string{"regionCode", c.region}, [2]string{"instanceType", instanceType}, [2]string{"operatingSystem", "Linux"},
		[2]string{"tenancy", "Shared"}, [2]string{"preInstalledSw", "NA"}, [2]string{"capacitystatus", "Used"}))
}

// products is the Price List's GetProducts answer for EC2 products matching every filter (field,
// value), nil when it does not answer in time or refuses.
func (c *awsClient) products(ctx context.Context, maxResults int, filters ...[2]string) []byte {
	ctx, cancel := context.WithTimeout(ctx, awsPriceTimeout)
	defer cancel()

	terms := make([]map[string]string, 0, len(filters))
	for _, f := range filters {
		terms = append(terms, map[string]string{"Type": "TERM_MATCH", "Field": f[0], "Value": f[1]})
	}

	body, err := json.Marshal(map[string]any{"ServiceCode": "AmazonEC2", "FormatVersion": "aws_v1", "MaxResults": maxResults, "Filters": terms})
	if err != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, awsPricingEndpoint()+"/", bytes.NewReader(body))
	if err != nil {
		return nil
	}

	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSPriceListService.GetProducts")
	sigV4Sign(req, sha256Hex(body), c.creds, awsPricingRegion, "pricing", time.Now())

	data, status, err := c.send(req)
	if err != nil || status != http.StatusOK {
		return nil
	}

	return data
}

// awsHourlyPrice reads GetProducts' answer: PriceList holds products as JSON strings.
func awsHourlyPrice(data []byte) string {
	return awsProductPrice(data, "Hrs", func(string) bool { return true })
}

// awsProductPrice is the USD price per unit of the first product of GetProducts' answer whose
// usage type passes keep, with 4 decimals, "" for none.
func awsProductPrice(data []byte, unit string, keep func(usageType string) bool) string {
	var answer struct {
		PriceList []string `json:"PriceList"`
	}

	if json.Unmarshal(data, &answer) != nil {
		return ""
	}

	for _, raw := range answer.PriceList {
		var product struct {
			Product struct {
				Attributes struct {
					UsageType string `json:"usagetype"`
				} `json:"attributes"`
			} `json:"product"`
			Terms struct {
				OnDemand map[string]struct {
					PriceDimensions map[string]struct {
						Unit         string            `json:"unit"`
						PricePerUnit map[string]string `json:"pricePerUnit"`
					} `json:"priceDimensions"`
				} `json:"OnDemand"`
			} `json:"terms"`
		}

		if json.Unmarshal([]byte(raw), &product) != nil || !keep(product.Product.Attributes.UsageType) {
			continue
		}

		for _, term := range product.Terms.OnDemand {
			for _, dim := range term.PriceDimensions {
				if price, err := strconv.ParseFloat(dim.PricePerUnit["USD"], 64); err == nil && dim.Unit == unit {
					return strconv.FormatFloat(price, 'f', 4, 64)
				}
			}
		}
	}

	return ""
}

// canRun asks for an instance with DryRun: EC2 answers DryRunOperation when the keys may run
// it, UnauthorizedOperation when not, and creates nothing either way. Any other answer (an
// image of no region, no default VPC) cannot tell: checked false.
func (c *awsClient) canRun(ctx context.Context, params url.Values) (can, checked bool, err error) {
	dry := url.Values{"DryRun": {"true"}}
	for k, v := range params {
		dry[k] = v
	}

	err = c.ec2(ctx, "RunInstances", dry, nil)

	var e *awsError

	switch {
	case errors.As(err, &e) && e.code == awsCodeDryRun:
		return true, true, nil
	case errors.As(err, &e) && e.code == awsCodeUnauthorized:
		return false, true, nil
	case err == nil:
		return false, false, errors.New("EC2 answered a dry run as a real one: cannot tell what these keys may do")
	}

	return false, false, err
}

type awsTestResult struct {
	Account   string `json:"account"`
	Arn       string `json:"arn"`
	Region    string `json:"region"`
	CanCreate bool   `json:"canCreate"`
	Checked   bool   `json:"checked"`
}

// AWSTest checks access keys. JSON: {"account","arn","region","canCreate","checked"}: canCreate
// from a RunInstances dry run naming the region's official Talos AMI, checked false when that
// AMI cannot be found (the release's cloud-images.json does not answer) or EC2 answers anything
// but yes or no. Creates nothing.
func AWSTest(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newAWSClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsOptionsTimeout)
	defer cancel()

	id, err := c.sts(ctx)
	if err != nil {
		return "", err
	}

	result := awsTestResult{Account: id.Account, Arn: id.Arn, Region: c.region}

	images, err := talosAWSImages(ctx, awsCheckTalosVersion, c.region)
	if i := slices.IndexFunc(images, func(i awsImage) bool { return i.Arch == bootArchAMD64 }); err == nil && i >= 0 {
		params := url.Values{"ImageId": {images[i].ID}, "InstanceType": {awsCheckType}, "MinCount": {"1"}, "MaxCount": {"1"}}
		result.CanCreate, result.Checked, _ = c.canRun(ctx, params) //nolint:errcheck // told apart by checked
	}

	return toJSON(result)
}

type awsPolicyStatement struct {
	Sid       string         `json:"Sid"`
	Effect    string         `json:"Effect"`
	Action    []string       `json:"Action"`
	Resource  string         `json:"Resource"`
	Condition map[string]any `json:"Condition,omitempty"`
}

// AWSIAMPolicy is the IAM policy to attach to the keys' user or role: only the calls the app
// makes. Tags may be set only by the calls that create (RunInstances, CreateSnapshot,
// RegisterImage), and the terminate, stop and delete calls reach only what is tagged ichor-run:
// the app can remove what it created, nothing else. The person pastes it.
func AWSIAMPolicy() string {
	// Plain values: it always encodes.
	out, _ := toJSON(struct { //nolint:errcheck
		Version   string               `json:"Version"`
		Statement []awsPolicyStatement `json:"Statement"`
	}{
		Version: "2012-10-17",
		Statement: []awsPolicyStatement{
			{
				Sid: "IchorRead", Effect: "Allow", Resource: "*",
				Action: []string{
					"sts:GetCallerIdentity", "ec2:DescribeRegions", "ec2:DescribeInstanceTypes", "ec2:DescribeSubnets",
					"ec2:DescribeVpcs", "ec2:DescribeSecurityGroups", "ec2:DescribeImages", "ec2:DescribeInstances", "pricing:GetProducts",
					"ec2:DescribeSnapshots", "ec2:GetConsoleOutput",
				},
			},
			// CreateSnapshot and RegisterImage make the Talos AMI (StartAWSImage).
			{Sid: "IchorRun", Effect: "Allow", Resource: "*", Action: []string{"ec2:RunInstances", "ec2:CreateSnapshot", "ec2:RegisterImage"}},
			{
				Sid: "IchorTagOnCreate", Effect: "Allow", Resource: "*", Action: []string{"ec2:CreateTags"},
				Condition: map[string]any{"StringEquals": map[string][]string{"ec2:CreateAction": {"RunInstances", "CreateSnapshot", "RegisterImage"}}},
			},
			{
				Sid: "IchorTerminateOwn", Effect: "Allow", Resource: "*", Action: []string{"ec2:TerminateInstances"},
				Condition: map[string]any{"StringLike": map[string]string{"aws:ResourceTag/" + awsTagRun: "*"}},
			},
			{
				Sid: "IchorCleanUpOwn", Effect: "Allow", Resource: "*", Action: []string{"ec2:StopInstances", "ec2:DeleteSnapshot", "ec2:DeregisterImage"},
				Condition: map[string]any{"StringLike": map[string]string{"aws:ResourceTag/" + awsTagRun: "*"}},
			},
		},
	})

	return out
}

type awsPriceResult struct {
	Hourly   string `json:"hourly"`
	Currency string `json:"currency,omitempty"`
}

// AWSPrice is the on-demand Linux hourly price of one instance type in the keys' region, before
// tax: {"hourly":"0.0208","currency":"USD"}, or {"hourly":""} when the Price List does not
// answer (the keys need pricing:GetProducts). One call: the app asks for the chosen type only.
func AWSPrice(credJSON, instanceType string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newAWSClient(credJSON)
	if err != nil {
		return "", err
	}

	instanceType = strings.TrimSpace(instanceType)
	if instanceType == "" {
		return "", errors.New("pick an instance type")
	}

	hourly := c.pricing(context.Background(), instanceType)
	if hourly == "" {
		return toJSON(awsPriceResult{})
	}

	return toJSON(awsPriceResult{Hourly: hourly, Currency: "USD"})
}
