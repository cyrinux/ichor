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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// S8 step 8: OVHcloud Public Cloud, plain REST with an IAM service account (OAuth2 client
// credentials): no application key to ship, and a policy the person scopes to one project. The
// app seals the account and passes it per call. See https://eu.api.ovh.com/1.0/cloud.json.

const (
	ovhMaxBody        = 8 << 20
	ovhCatalogMaxBody = 32 << 20
	ovhCatalogTimeout = 20 * time.Second
	ovhOptionsTimeout = 60 * time.Second
	ovhTokenMargin    = time.Minute

	ovhSiteEU = "eu"
	ovhSiteCA = "ca"

	ovhOSLinux = "linux"

	// ovhPriceUnit: the catalog's prices are in 1e-8 of the currency.
	ovhPriceUnit = 1e8
)

// ovhSite is one of OVHcloud's two API regions.
type ovhSite struct {
	api, token, catalog string
}

var (
	ovhSites = map[string]ovhSite{
		ovhSiteEU: {api: "https://eu.api.ovh.com/1.0", token: "https://www.ovh.com/auth/oauth2/token", catalog: "FR"},
		ovhSiteCA: {api: "https://ca.api.ovh.com/1.0", token: "https://ca.ovh.com/auth/oauth2/token", catalog: "CA"},
	}

	errOVHNoCredential = errors.New("add an OVHcloud service account first")
	errOVHNoCreate     = errors.New("this service account may not create an instance: add the actions of the printed policy to its IAM policy")
)

// ovhError is an answer out of 2xx: {"message"}.
type ovhError struct {
	status  int
	message string
}

func (e *ovhError) Error() string {
	switch e.status {
	case http.StatusUnauthorized:
		return "OVHcloud refused the service account: check its id and secret"
	case http.StatusForbidden:
		return "OVHcloud: " + e.message + ". Check the service account's IAM policy against the one the app prints"
	}

	return "OVHcloud: " + e.message
}

// ovhClient calls the API with one service account, whose secret and token stay in the request
// headers: never in a URL, a message or a log.
type ovhClient struct {
	site     ovhSite
	id       string
	secret   string
	project  string
	mu       sync.Mutex
	token    string
	tokenEnd time.Time
}

// newOVHClient reads credJSON: {"endpoint":"eu"|"ca","clientId","clientSecret","project"}.
func newOVHClient(credJSON string) (*ovhClient, error) {
	var c struct {
		Endpoint     string `json:"endpoint"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Project      string `json:"project"`
	}

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &c); err != nil {
			return nil, errors.New("invalid OVHcloud service account data") //nolint:nilerr // the parser's message may quote the secret
		}
	}

	c.ClientID, c.ClientSecret = strings.TrimSpace(c.ClientID), strings.TrimSpace(c.ClientSecret)
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, errOVHNoCredential
	}

	site, ok := ovhSites[cmp.Or(c.Endpoint, ovhSiteEU)]
	if !ok {
		return nil, fmt.Errorf("unknown OVHcloud endpoint %q (eu, ca)", c.Endpoint)
	}

	return &ovhClient{site: site, id: c.ClientID, secret: c.ClientSecret, project: strings.TrimSpace(c.Project)}, nil
}

// bearer is the cached access token, renewed a minute before it ends.
func (c *ovhClient) bearer(ctx context.Context, renew bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !renew && c.token != "" && time.Now().Before(c.tokenEnd) {
		return c.token, nil
	}

	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"all"}}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.site.token, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}

	req.SetBasicAuth(c.id, c.secret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, ovhMaxBody))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Any refusal of the exchange is the account's: its answer is not worth repeating.
		return "", &ovhError{status: http.StatusUnauthorized}
	}

	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		return "", errors.New("OVHcloud answered the sign-in without a token")
	}

	c.token, c.tokenEnd = t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn)*time.Second-ovhTokenMargin)

	return c.token, nil
}

// do calls the API; a token OVHcloud no longer takes is renewed once.
func (c *ovhClient) do(ctx context.Context, method, path string, body, out any) error {
	err := c.call(ctx, method, path, body, out, false)

	var e *ovhError
	if errors.As(err, &e) && e.status == http.StatusUnauthorized && e.message != "" {
		return c.call(ctx, method, path, body, out, true)
	}

	return err
}

func (c *ovhClient) call(ctx context.Context, method, path string, body, out any, renew bool) error {
	token, err := c.bearer(ctx, renew)
	if err != nil {
		return err
	}

	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.site.api+path, reader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, ovhMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}

		_ = json.Unmarshal(data, &e) //nolint:errcheck

		return &ovhError{status: resp.StatusCode, message: cmp.Or(e.Message, http.StatusText(resp.StatusCode))}
	}

	if out == nil {
		return nil
	}

	return json.Unmarshal(data, out)
}

func (c *ovhClient) path(rest string) string {
	return "/cloud/project/" + url.PathEscape(c.project) + rest
}

// pickProject sets the project: the one asked if the account sees it, else its first.
func (c *ovhClient) pickProject(ctx context.Context) ([]string, error) {
	var projects []string
	if err := c.do(ctx, http.MethodGet, "/cloud/project", nil, &projects); err != nil {
		return nil, err
	}

	if len(projects) == 0 {
		return nil, errors.New("this service account sees no Public Cloud project: check its IAM policy")
	}

	if !slices.Contains(projects, c.project) {
		c.project = projects[0]
	}

	return projects, nil
}

// canCreate asks for an instance with an empty body, which cannot create one (a name, a region and
// a flavor are required): an account that may create gets 400, one that may not 403. checked is
// false for any other answer.
func (c *ovhClient) canCreate(ctx context.Context) (can, checked bool, err error) {
	err = c.do(ctx, http.MethodPost, c.path("/instance"), struct{}{}, nil)

	var e *ovhError

	switch {
	case errors.As(err, &e) && e.status == http.StatusForbidden:
		return false, true, nil
	case errors.As(err, &e) && e.status == http.StatusBadRequest:
		return true, true, nil
	case errors.As(err, &e) && e.status == http.StatusUnauthorized:
		return false, false, err
	case err == nil:
		return false, false, errors.New("OVHcloud accepted an empty instance: cannot tell what this account may do")
	}

	return false, false, nil //nolint:nilerr // told apart by checked: the create says more
}

type ovhTestResult struct {
	Project   string   `json:"project"`
	Projects  []string `json:"projects"`
	CanCreate bool     `json:"canCreate"`
	Checked   bool     `json:"checked"`
}

// OVHTest checks a service account. JSON: {"project","projects","canCreate","checked"}: project is
// the one asked or the account's first, checked false when the create right could not be told.
// Creates nothing.
func OVHTest(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newOVHClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	projects, err := c.pickProject(ctx)
	if err != nil {
		return "", err
	}

	can, checked, err := c.canCreate(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(ovhTestResult{Project: c.project, Projects: projects, CanCreate: can, Checked: checked})
}

// OVHServiceAccountPolicy is the IAM policy to give the service account: only the calls the app
// makes, on one project ("" prints a placeholder). The person pastes it at OVHcloud.
func OVHServiceAccountPolicy(project string) string {
	project = cmp.Or(strings.TrimSpace(project), "PROJECT_ID")

	allow := []map[string]string{}
	for _, action := range []string{"get", "region/get", "flavor/get", "image/get", "instance/get", "instance/create", "instance/delete", "operation/get"} {
		allow = append(allow, map[string]string{"action": "publicCloudProject:apiovh:" + action})
	}

	// A map of plain values: it always encodes.
	out, _ := toJSON(map[string]any{ //nolint:errcheck
		"name":        "ichor-add-node",
		"description": "Create and delete Public Cloud instances of one project",
		"identities":  []string{"SERVICE_ACCOUNT_URN"},
		"permissions": map[string]any{"allow": allow},
		"resources":   []map[string]string{{"urn": "urn:v1:eu:resource:publicCloudProject:" + project}},
	})

	return out
}

// ovhOptions is what an instance can be created from. Hourly is before tax, "" with no catalog
// (currency "" then). Flavors and images are per region.
type ovhOptions struct {
	Project  string      `json:"project"`
	Currency string      `json:"currency"`
	Regions  []string    `json:"regions"`
	Flavors  []ovhFlavor `json:"flavors"`
	Images   []ovhImage  `json:"images"`
}

type ovhFlavor struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Region string `json:"region"`
	VCPUs  int    `json:"vcpus"`
	RAMGB  int    `json:"ramGB"`
	DiskGB int    `json:"diskGB"`
	Hourly string `json:"hourly"`
}

type ovhImage struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Region  string `json:"region"`
	Private bool   `json:"private"`
	Talos   bool   `json:"talos"`
}

// The API's objects, as far as they are read.
type ovhAPIFlavor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	VCPUs     int    `json:"vcpus"`
	RAM       int    `json:"ram"`
	Disk      int    `json:"disk"`
	OSType    string `json:"osType"`
	Available bool   `json:"available"`
	PlanCodes struct {
		Hourly string `json:"hourly"`
	} `json:"planCodes"`
}

type ovhAPIImage struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Region     string `json:"region"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
}

// OVHOptions lists the project's regions, flavors with their hourly price and images, Talos and
// private ones first. Without the public price catalog the prices stay empty: the app says so.
// The demo context answers with samples, without an account. JSON: {"project","currency",
// "regions","flavors":[{"id","name","region","vcpus","ramGB","diskGB","hourly"}],
// "images":[{"id","name","region","private","talos"}]}.
func OVHOptions(configYAML, contextName, credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoOVHOptions())
	}

	c, err := newOVHClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), ovhOptionsTimeout)
	defer cancel()

	options, err := c.options(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(options)
}

func (c *ovhClient) options(ctx context.Context) (ovhOptions, error) {
	if _, err := c.pickProject(ctx); err != nil {
		return ovhOptions{}, err
	}

	var regions []string
	if err := c.do(ctx, http.MethodGet, c.path("/region"), nil, &regions); err != nil {
		return ovhOptions{}, err
	}

	var flavors []ovhAPIFlavor
	if err := c.do(ctx, http.MethodGet, c.path("/flavor"), nil, &flavors); err != nil {
		return ovhOptions{}, err
	}

	var images []ovhAPIImage
	if err := c.do(ctx, http.MethodGet, c.path("/image"), nil, &images); err != nil {
		return ovhOptions{}, err
	}

	currency, prices := c.catalog(ctx)

	slices.Sort(regions)

	return ovhOptions{
		Project: c.project, Currency: currency, Regions: regions,
		Flavors: ovhFlavors(flavors, prices), Images: ovhImages(images),
	}, nil
}

// ovhFlavors keeps what a Linux instance can be ordered on, the smallest first.
func ovhFlavors(flavors []ovhAPIFlavor, prices map[string]string) []ovhFlavor {
	out := make([]ovhFlavor, 0, len(flavors))

	for _, f := range flavors {
		if f.Available && f.OSType == ovhOSLinux {
			out = append(out, ovhFlavor{
				ID: f.ID, Name: f.Name, Region: f.Region, VCPUs: f.VCPUs, RAMGB: f.RAM, DiskGB: f.Disk,
				Hourly: prices[f.PlanCodes.Hourly],
			})
		}
	}

	slices.SortStableFunc(out, func(a, b ovhFlavor) int {
		return cmp.Or(cmp.Compare(a.VCPUs, b.VCPUs), cmp.Compare(a.RAMGB, b.RAMGB), strings.Compare(a.Name, b.Name))
	})

	return out
}

// ovhImages puts the Talos images first, then the project's own. Every usable image is listed: one
// uploaded by hand may not carry the word. Windows cannot run Talos.
func ovhImages(images []ovhAPIImage) []ovhImage {
	out := make([]ovhImage, 0, len(images))

	for _, i := range images {
		if i.Type == "windows" || (i.Status != "" && i.Status != "active") {
			continue
		}

		out = append(out, ovhImage{
			ID: i.ID, Name: i.Name, Region: i.Region, Private: i.Visibility == "private",
			Talos: strings.Contains(strings.ToLower(i.Name), "talos"),
		})
	}

	rank := func(i ovhImage) int {
		switch {
		case i.Talos:
			return 0
		case i.Private:
			return 1
		}

		return 2 //nolint:mnd // the rest
	}

	slices.SortStableFunc(out, func(a, b ovhImage) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), strings.Compare(a.Name, b.Name))
	})

	return out
}

// catalog reads the public price list, large and not worth failing for: a plan code → hourly price
// map, empty when it does not answer in time.
func (c *ovhClient) catalog(ctx context.Context) (string, map[string]string) {
	ctx, cancel := context.WithTimeout(ctx, ovhCatalogTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.site.api+"/order/catalog/public/cloud?ovhSubsidiary="+c.site.catalog, nil)
	if err != nil {
		return "", nil
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	var catalog struct {
		Locale struct {
			CurrencyCode string `json:"currencyCode"`
		} `json:"locale"`
		Addons []struct {
			PlanCode string `json:"planCode"`
			Pricings []struct {
				IntervalUnit string  `json:"intervalUnit"`
				Price        float64 `json:"price"`
			} `json:"pricings"`
		} `json:"addons"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, ovhCatalogMaxBody)).Decode(&catalog); err != nil {
		return "", nil
	}

	prices := make(map[string]string, len(catalog.Addons))

	for _, a := range catalog.Addons {
		for _, p := range a.Pricings {
			if p.IntervalUnit == "hour" {
				prices[a.PlanCode] = strconv.FormatFloat(p.Price/ovhPriceUnit, 'f', 4, 64)

				break
			}
		}
	}

	return catalog.Locale.CurrencyCode, prices
}

func demoOVHOptions() ovhOptions {
	const region = "GRA11"

	return ovhOptions{
		Project: "scratch", Currency: "EUR", Regions: []string{"DE1", region},
		Flavors: []ovhFlavor{
			{ID: "demo-b3-8", Name: "b3-8", Region: region, VCPUs: 2, RAMGB: 8, DiskGB: 50, Hourly: "0.0512"},
			{ID: "demo-b3-16", Name: "b3-16", Region: region, VCPUs: 4, RAMGB: 16, DiskGB: 100, Hourly: "0.1024"},
			{ID: "demo-b3-8-de", Name: "b3-8", Region: "DE1", VCPUs: 2, RAMGB: 8, DiskGB: 50, Hourly: "0.0512"},
		},
		Images: []ovhImage{
			{ID: "demo-talos", Name: "talos " + demoTalosVersion + " openstack", Region: region, Private: true, Talos: true},
			{ID: "demo-talos-de", Name: "talos " + demoTalosVersion + " openstack", Region: "DE1", Private: true, Talos: true},
		},
	}
}
