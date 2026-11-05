package ichorgo

import (
	"bytes"
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
	"strconv"
	"strings"
	"time"
)

// S8 step 28: Scaleway Instances, plain REST with an IAM API key (an access key and a secret key
// sent as X-Auth-Token) that the person scopes to one project with the printed policy. The app
// seals the key and passes it per call. The readable mirror of the API is the Go SDK:
// https://github.com/scaleway/scaleway-sdk-go/blob/master/api/instance/v1/instance_sdk.go.

const (
	scwMaxBody        = 8 << 20
	scwOptionsTimeout = 60 * time.Second
	scwPerPage        = 100
	scwMaxPages       = 20
	scwDefaultZone    = "fr-par-1"

	// scwGB is the API's sizes' unit: bytes, in decimal gigabytes.
	scwGB int64 = 1_000_000_000

	scwVolumeLocal = "l_ssd"
	scwVolumeBlock = "sbs_volume"
)

var (
	// scwAPIEndpoint is the API's root (replaced in tests).
	scwAPIEndpoint = "https://api.scaleway.com"

	// scwZones are the zones Instances are sold in; each zone's own answer says what it sells.
	scwZones = []string{"fr-par-1", "fr-par-2", "fr-par-3", "it-mil-1", "nl-ams-1", "nl-ams-2", "nl-ams-3", "pl-waw-1", "pl-waw-2", "pl-waw-3"}

	scwZonePattern    = regexp.MustCompile(`^[a-z]{2}-[a-z]{3}-[0-9]$`)
	scwProjectPattern = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

	errScwNoCredential = errors.New("add a Scaleway API key first")
	errScwNoCreate     = errors.New("this API key may not create an Instance: attach the printed policy to its application")
)

// scwError is an answer out of 2xx: {"message","type","resource"}.
type scwError struct {
	status  int
	message string
}

func (e *scwError) Error() string {
	switch e.status {
	case http.StatusUnauthorized:
		return "Scaleway refused the API key: check its secret key"
	case http.StatusForbidden:
		return "Scaleway: " + e.message + ". Check the API key's policy against the one the app prints"
	}

	return "Scaleway: " + e.message
}

// scwStatus is err's HTTP status, 0 for no answer.
func scwStatus(err error) int {
	if e, ok := errors.AsType[*scwError](err); ok {
		return e.status
	}

	return 0
}

// scwClient calls the API with one key, whose secret stays in the request header: never in a
// URL, a message or a log.
type scwClient struct {
	accessKey string
	secretKey string
	project   string
	zone      string
}

// newScwClient reads credJSON: {"accessKey","secretKey","project","zone"} (zone "" for fr-par-1).
func newScwClient(credJSON string) (*scwClient, error) {
	var c struct {
		AccessKey string `json:"accessKey"`
		SecretKey string `json:"secretKey"`
		Project   string `json:"project"`
		Zone      string `json:"zone"`
	}

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &c); err != nil {
			return nil, errors.New("invalid Scaleway API key data") //nolint:nilerr // the parser's message may quote the secret
		}
	}

	c.SecretKey = strings.TrimSpace(c.SecretKey)
	if c.SecretKey == "" {
		return nil, errScwNoCredential
	}

	client := &scwClient{
		accessKey: strings.TrimSpace(c.AccessKey), secretKey: c.SecretKey,
		project: strings.TrimSpace(c.Project), zone: cmp.Or(strings.TrimSpace(c.Zone), scwDefaultZone),
	}

	if !scwProjectPattern.MatchString(client.project) {
		return nil, errors.New("add the Scaleway project id the key may use (a UUID, in the console's project settings)")
	}

	if err := client.setZone(client.zone); err != nil {
		return nil, err
	}

	return client, nil
}

// setZone moves the client to zone, refused unless it looks like one (it goes in the URL).
func (c *scwClient) setZone(zone string) error {
	if !scwZonePattern.MatchString(zone) {
		return fmt.Errorf("unknown Scaleway zone %q (fr-par-1, nl-ams-1…)", zone)
	}

	c.zone = zone

	return nil
}

// do calls the API with a JSON body.
func (c *scwClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(data)
	}

	return c.send(ctx, method, path, "application/json", reader, out)
}

func (c *scwClient) send(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, scwAPIEndpoint+path, body)
	if err != nil {
		return err
	}

	req.Header.Set("X-Auth-Token", c.secretKey)

	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, scwMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}

		_ = json.Unmarshal(data, &e) //nolint:errcheck

		return &scwError{status: resp.StatusCode, message: cmp.Or(e.Message, http.StatusText(resp.StatusCode))}
	}

	if out == nil || len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, out)
}

// path is an Instance API path in the client's zone.
func (c *scwClient) path(rest string) string {
	return "/instance/v1/zones/" + c.zone + rest
}

// projectQuery is the query naming the client's project.
func (c *scwClient) projectQuery() string {
	return "project=" + url.QueryEscape(c.project)
}

// scwEachPage reads a listing page by page until a short page.
func scwEachPage[R any](ctx context.Context, c *scwClient, path string, count func(R) int) error {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}

	for page := 1; page <= scwMaxPages; page++ {
		var r R
		if err := c.do(ctx, http.MethodGet, path+sep+"per_page="+strconv.Itoa(scwPerPage)+"&page="+strconv.Itoa(page), nil, &r); err != nil {
			return err
		}

		if count(r) < scwPerPage {
			return nil
		}
	}

	return nil
}

// canCreate asks for a server with only a project, which cannot create one (a type and an image
// are required): a key that may create gets 400, one that may not 403. checked is false for any
// other answer.
func (c *scwClient) canCreate(ctx context.Context) (can, checked bool, err error) {
	err = c.do(ctx, http.MethodPost, c.path("/servers"), map[string]string{"project": c.project}, nil)

	switch scwStatus(err) {
	case http.StatusForbidden:
		return false, true, nil
	case http.StatusBadRequest:
		return true, true, nil
	case http.StatusUnauthorized:
		return false, false, err
	}

	if err == nil {
		return false, false, errors.New("scaleway accepted an empty server: cannot tell what this key may do")
	}

	return false, false, nil //nolint:nilerr // told apart by checked: the create says more
}

type scwTestResult struct {
	Project   string `json:"project"`
	Zone      string `json:"zone"`
	CanCreate bool   `json:"canCreate"`
	Checked   bool   `json:"checked"`
	Account   string `json:"account"`
}

// ScalewayTest checks an API key. JSON: {"project","zone","canCreate","checked","account"}:
// account is the key's access key (an id, not the secret), checked false when the create right
// could not be told. Creates nothing.
func ScalewayTest(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newScwClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	if err := c.do(ctx, http.MethodGet, c.path("/servers?per_page=1&"+c.projectQuery()), nil, nil); err != nil {
		return "", err
	}

	can, checked, err := c.canCreate(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(scwTestResult{Project: c.project, Zone: c.zone, CanCreate: can, Checked: checked, Account: c.accessKey})
}

// ScalewayPolicy is the IAM policy to attach to the key's application: the Instance permission
// sets on one project ("" prints a placeholder), and Block Storage for the data volume of a type
// without local storage. The person creates it at Scaleway (console or POST /iam/v1alpha1/policies).
func ScalewayPolicy(project string) string {
	project = cmp.Or(strings.TrimSpace(project), "PROJECT_ID")

	// A map of plain values: it always encodes.
	out, _ := toJSON(map[string]any{ //nolint:errcheck
		"name":           "ichor-add-node",
		"description":    "Create and delete Instances of one project for new Talos nodes",
		"application_id": "APPLICATION_ID",
		"rules": []map[string]any{{
			"project_ids":          []string{project},
			"permission_set_names": []string{"InstancesReadOnly", "InstancesFullAccess", "BlockStorageFullAccess"},
		}},
	})

	return out
}

// scwOptions is what an Instance can be created from in one zone. Hourly is in EUR before tax.
type scwOptions struct {
	Project        string             `json:"project"`
	Zone           string             `json:"zone"`
	Zones          []string           `json:"zones"`
	Types          []scwType          `json:"types"`
	Images         []scwImage         `json:"images"`
	SecurityGroups []scwSecurityGroup `json:"securityGroups"`
	Warnings       []string           `json:"warnings"`
}

type scwType struct {
	Name        string `json:"name"`
	Arch        string `json:"arch"`
	VCPUs       int    `json:"vcpus"`
	RAMGB       int    `json:"ramGB"`
	LocalDiskGB int    `json:"localDiskGB"`
	Hourly      string `json:"hourly"`
	Currency    string `json:"currency"`
	BlockOnly   bool   `json:"blockOnly"`
}

type scwImage struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Arch   string `json:"arch"`
	SizeGB int    `json:"sizeGB"`
	Talos  bool   `json:"talos"`
}

type scwSecurityGroup struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// The API's objects, as far as they are read.
type scwAPIType struct {
	HourlyPrice       float64 `json:"hourly_price"`
	NCPUs             int     `json:"ncpus"`
	RAM               int64   `json:"ram"`
	Arch              string  `json:"arch"`
	EndOfService      bool    `json:"end_of_service"`
	VolumesConstraint struct {
		MinSize int64 `json:"min_size"`
		MaxSize int64 `json:"max_size"`
	} `json:"volumes_constraint"`
}

type scwAPIImage struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Arch       string   `json:"arch"`
	State      string   `json:"state"`
	Project    string   `json:"project"`
	Tags       []string `json:"tags"`
	RootVolume struct {
		Size       int64  `json:"size"`
		VolumeType string `json:"volume_type"`
	} `json:"root_volume"`
}

// talos: a Talos image says so in its name or a tag; one uploaded by hand may not.
func (i scwAPIImage) talos() bool {
	return strings.Contains(strings.ToLower(i.Name), "talos") ||
		slices.ContainsFunc(i.Tags, func(t string) bool { return strings.Contains(strings.ToLower(t), "talos") })
}

// ScalewayOptions lists, in the key's zone (credJSON's), the server types with their hourly price,
// the project's images (Talos first) and security groups. The demo context answers with samples,
// without a key. JSON: {"project","zone","zones","types":[{"name","arch","vcpus","ramGB",
// "localDiskGB","hourly","currency","blockOnly"}],"images":[{"id","name","arch","sizeGB","talos"}],
// "securityGroups":[{"id","name","default"}],"warnings"}; blockOnly types have no local storage
// (the EPHEMERAL volume is then a Block Storage one).
func ScalewayOptions(configYAML, contextName, credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoScalewayOptions())
	}

	c, err := newScwClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), scwOptionsTimeout)
	defer cancel()

	options, err := c.options(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(options)
}

func (c *scwClient) options(ctx context.Context) (scwOptions, error) {
	types, err := c.serverTypes(ctx)
	if err != nil {
		return scwOptions{}, err
	}

	var images []scwAPIImage

	err = scwEachPage(ctx, c, c.path("/images?"+c.projectQuery()), func(r struct {
		Images []scwAPIImage `json:"images"`
	}) int {
		images = append(images, r.Images...)

		return len(r.Images)
	})
	if err != nil {
		return scwOptions{}, err
	}

	var groups struct {
		SecurityGroups []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			ProjectDefault bool   `json:"project_default"`
		} `json:"security_groups"`
	}

	if err := c.do(ctx, http.MethodGet, c.path("/security_groups?per_page=100&"+c.projectQuery()), nil, &groups); err != nil {
		return scwOptions{}, err
	}

	options := scwOptions{
		Project: c.project, Zone: c.zone, Zones: slices.Clone(scwZones),
		Types: scwTypes(types), Images: scwImages(images), SecurityGroups: []scwSecurityGroup{},
		Warnings: []string{"Plain Talos: the node's security group must let the phone reach TCP 50000 (and 6443 on a control plane)"},
	}

	for _, g := range groups.SecurityGroups {
		options.SecurityGroups = append(options.SecurityGroups, scwSecurityGroup{ID: g.ID, Name: g.Name, Default: g.ProjectDefault})
	}

	if !slices.ContainsFunc(options.Images, func(i scwImage) bool { return i.Talos }) {
		options.Warnings = append(options.Warnings, "This project holds no Talos image in "+c.zone+": upload one once per the Talos guide; making one from the phone is the next step")
	}

	return options, nil
}

// serverTypes is the zone's server types by name.
func (c *scwClient) serverTypes(ctx context.Context) (map[string]scwAPIType, error) {
	types := map[string]scwAPIType{}

	err := scwEachPage(ctx, c, c.path("/products/servers"), func(r struct {
		Servers map[string]scwAPIType `json:"servers"`
	}) int {
		for name, t := range r.Servers {
			types[name] = t
		}

		return len(r.Servers)
	})

	return types, err
}

// scwTypes keeps what can still be ordered, the cheapest first.
func scwTypes(types map[string]scwAPIType) []scwType {
	out := make([]scwType, 0, len(types))

	for name, t := range types {
		if t.EndOfService {
			continue
		}

		out = append(out, scwType{
			Name: name, Arch: t.Arch, VCPUs: t.NCPUs, RAMGB: int(t.RAM >> 30),
			LocalDiskGB: int(t.VolumesConstraint.MaxSize / scwGB), BlockOnly: t.VolumesConstraint.MaxSize == 0,
			Hourly: strconv.FormatFloat(t.HourlyPrice, 'f', 4, 64), Currency: "EUR",
		})
	}

	slices.SortStableFunc(out, func(a, b scwType) int {
		pa, _ := strconv.ParseFloat(a.Hourly, 64) //nolint:errcheck // made by FormatFloat above
		pb, _ := strconv.ParseFloat(b.Hourly, 64) //nolint:errcheck

		return cmp.Or(cmp.Compare(pa, pb), strings.Compare(a.Name, b.Name))
	})

	return out
}

// scwImages keeps the usable images, Talos ones first.
func scwImages(images []scwAPIImage) []scwImage {
	out := make([]scwImage, 0, len(images))

	for _, i := range images {
		if i.State != "" && i.State != "available" {
			continue
		}

		out = append(out, scwImage{ID: i.ID, Name: i.Name, Arch: i.Arch, SizeGB: int(i.RootVolume.Size / scwGB), Talos: i.talos()})
	}

	slices.SortStableFunc(out, func(a, b scwImage) int {
		if a.Talos != b.Talos {
			if a.Talos {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})

	return out
}

func demoScalewayOptions() scwOptions {
	return scwOptions{
		Project: "11111111-1111-1111-1111-111111111111", Zone: scwDefaultZone, Zones: slices.Clone(scwZones),
		Types: []scwType{
			{Name: "DEV1-S", Arch: "x86_64", VCPUs: 2, RAMGB: 2, LocalDiskGB: 20, Hourly: "0.0099", Currency: "EUR"},
			{Name: "DEV1-M", Arch: "x86_64", VCPUs: 3, RAMGB: 4, LocalDiskGB: 40, Hourly: "0.0198", Currency: "EUR"},
			{Name: "COPARM1-2C-8G", Arch: "arm64", VCPUs: 2, RAMGB: 8, Hourly: "0.0426", Currency: "EUR", BlockOnly: true},
		},
		Images: []scwImage{
			{ID: "demo-talos", Name: "talos " + demoTalosVersion + " scaleway", Arch: "x86_64", SizeGB: 10, Talos: true},
		},
		SecurityGroups: []scwSecurityGroup{{ID: "demo-sg", Name: "Default security group", Default: true}},
		Warnings:       []string{},
	}
}
