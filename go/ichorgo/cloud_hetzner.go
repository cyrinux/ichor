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
)

// S8 step 5: the Hetzner Cloud API, plain REST with the project's token (hcloud-go's core
// package imports Prometheus, not worth its weight in the binding). A token is one project's
// and never expires; the app seals it and passes it per call. See
// https://docs.hetzner.cloud/ (POST /servers, GET /server_types, /images, /pricing).

const (
	hetznerMaxBody = 8 << 20
	hetznerPerPage = 50

	hetznerArchX86 = "x86"
	hetznerArchARM = "arm"

	hetznerCodeForbidden = "forbidden"
)

var (
	hetznerEndpoint = "https://api.hetzner.cloud/v1"

	errHetznerNoToken  = errors.New("add a Hetzner API token first")
	errHetznerReadOnly = errors.New("this Hetzner token is read-only: creating a server needs a Read & Write token")
)

// hetznerError is an answer out of 2xx: {"error":{"code","message"}}.
type hetznerError struct {
	status  int
	code    string
	message string
}

func (e *hetznerError) Error() string {
	if e.status == http.StatusUnauthorized {
		return "Hetzner refused the token: check it, or make a new one in the project's Security page"
	}

	return "Hetzner: " + e.message
}

// hetznerClient calls the API with one project's token, which stays in the request header:
// never in a URL, a message or a log.
type hetznerClient struct{ token string }

// newHetznerClient reads tokenJSON: {"token":"…"}.
func newHetznerClient(tokenJSON string) (hetznerClient, error) {
	var t struct {
		Token string `json:"token"`
	}

	if strings.TrimSpace(tokenJSON) != "" {
		if err := json.Unmarshal([]byte(tokenJSON), &t); err != nil {
			return hetznerClient{}, errors.New("invalid Hetzner token data") //nolint:nilerr // the parser's message may quote the token
		}
	}

	if t.Token = strings.TrimSpace(t.Token); t.Token == "" {
		return hetznerClient{}, errHetznerNoToken
	}

	return hetznerClient{token: t.Token}, nil
}

func (c hetznerClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, hetznerEndpoint+path, reader)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, hetznerMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}

		_ = json.Unmarshal(data, &e) //nolint:errcheck

		return &hetznerError{status: resp.StatusCode, code: e.Error.Code, message: cmp.Or(e.Error.Message, http.StatusText(resp.StatusCode))}
	}

	if out == nil {
		return nil
	}

	return json.Unmarshal(data, out)
}

// hetznerList reads every page of a list: its items are under key.
func hetznerList[T any](ctx context.Context, c hetznerClient, path, key string) ([]T, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}

	var items []T

	for page := 1; ; {
		var raw map[string]json.RawMessage

		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("%s%sper_page=%d&page=%d", path, sep, hetznerPerPage, page), nil, &raw); err != nil {
			return nil, err
		}

		var pageItems []T
		if err := json.Unmarshal(raw[key], &pageItems); err != nil {
			return nil, fmt.Errorf("read Hetzner's %s: %w", key, err)
		}

		items = append(items, pageItems...)

		var meta struct {
			Pagination struct {
				NextPage *int `json:"next_page"`
			} `json:"pagination"`
		}

		if m, ok := raw["meta"]; !ok || json.Unmarshal(m, &meta) != nil || meta.Pagination.NextPage == nil || *meta.Pagination.NextPage <= page {
			return items, nil
		}

		page = *meta.Pagination.NextPage
	}
}

// hetznerReadOnly tells a read-only token: an empty POST /ssh_keys cannot create anything (a
// name and a key are required), so a token that may write gets 422, a read-only one 403. An
// API that checks the body first would only postpone the refusal to POST /servers, which
// creates nothing either.
func (c hetznerClient) readOnly(ctx context.Context) (bool, error) {
	err := c.do(ctx, http.MethodPost, "/ssh_keys", struct{}{}, nil)

	var e *hetznerError

	switch {
	case errors.As(err, &e) && e.status == http.StatusForbidden && e.code == hetznerCodeForbidden:
		return true, nil
	case errors.As(err, &e) && e.status == http.StatusUnprocessableEntity:
		return false, nil
	case err == nil:
		return false, errors.New("hetzner accepted an empty SSH key: cannot tell what this token may do")
	}

	return false, err
}

type hetznerTestResult struct {
	ReadOnly bool `json:"readOnly"`
	Servers  int  `json:"servers"`
}

// HetznerTest checks a Hetzner project token. The API does not name the project, so the app
// asks the person for a label. JSON: {"readOnly","servers"}. Creates nothing.
func HetznerTest(tokenJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newHetznerClient(tokenJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	servers, err := hetznerList[hetznerServer](ctx, c, "/servers", "servers")
	if err != nil {
		return "", err
	}

	readOnly, err := c.readOnly(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(hetznerTestResult{ReadOnly: readOnly, Servers: len(servers)})
}

// hetznerOptions is what a server can be created from. Prices are before tax, in the
// project owner's currency.
type hetznerOptions struct {
	Currency    string              `json:"currency"`
	ServerTypes []hetznerTypeOption `json:"serverTypes"`
	Locations   []hetznerLocation   `json:"locations"`
	Snapshots   []hetznerSnapshot   `json:"snapshots"`
}

type hetznerTypeOption struct {
	Name     string         `json:"name"`
	Cores    int            `json:"cores"`
	MemoryGB float64        `json:"memoryGB"`
	DiskGB   float64        `json:"diskGB"`
	Arch     string         `json:"arch"` // amd64 or arm64, as Talos names them
	Prices   []hetznerPrice `json:"prices"`
}

type hetznerPrice struct {
	Location string `json:"location"`
	Hourly   string `json:"hourly"`
	Monthly  string `json:"monthly"`
}

type hetznerLocation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	City        string `json:"city"`
	Country     string `json:"country"`
}

type hetznerSnapshot struct {
	ID          int64             `json:"id"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels"`
	Created     string            `json:"created"`
	Arch        string            `json:"arch"`
	OSFlavor    string            `json:"osFlavor"`
	Talos       bool              `json:"talos"`
}

// The API's objects, as far as they are read.
type hetznerAPIPrice struct {
	Location string `json:"location"`
	Hourly   struct {
		Net string `json:"net"`
	} `json:"price_hourly"`
	Monthly struct {
		Net string `json:"net"`
	} `json:"price_monthly"`
}

type hetznerAPIType struct {
	Name         string            `json:"name"`
	Cores        int               `json:"cores"`
	Memory       float64           `json:"memory"`
	Disk         float64           `json:"disk"`
	Architecture string            `json:"architecture"`
	Prices       []hetznerAPIPrice `json:"prices"`
	Deprecation  json.RawMessage   `json:"deprecation"`
	Locations    []struct {
		Name        string          `json:"name"`
		Deprecation json.RawMessage `json:"deprecation"`
	} `json:"locations"`
}

type hetznerAPIImage struct {
	ID           int64             `json:"id"`
	Description  string            `json:"description"`
	Labels       map[string]string `json:"labels"`
	Created      string            `json:"created"`
	Architecture string            `json:"architecture"`
	OSFlavor     string            `json:"os_flavor"`
}

// HetznerOptions lists the server types with their prices, the locations and the project's
// snapshots, Talos ones first (label os=talos or "talos" in the description). The demo
// context answers with samples, without a token. JSON: {"currency","serverTypes":[{"name",
// "cores","memoryGB","diskGB","arch","prices":[{"location","hourly","monthly"}]}],
// "locations":[{"name","description","city","country"}],"snapshots":[{"id","description",
// "labels","created","arch","osFlavor","talos"}]}.
func HetznerOptions(configYAML, contextName, tokenJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(demoHetznerOptions())
	}

	c, err := newHetznerClient(tokenJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), oidcHTTPTimeout)
	defer cancel()

	options, err := c.options(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(options)
}

func (c hetznerClient) options(ctx context.Context) (hetznerOptions, error) {
	var pricing struct {
		Pricing struct {
			Currency string `json:"currency"`
		} `json:"pricing"`
	}

	if err := c.do(ctx, http.MethodGet, "/pricing", nil, &pricing); err != nil {
		return hetznerOptions{}, err
	}

	types, err := hetznerList[hetznerAPIType](ctx, c, "/server_types", "server_types")
	if err != nil {
		return hetznerOptions{}, err
	}

	locations, err := hetznerList[hetznerLocation](ctx, c, "/locations", "locations")
	if err != nil {
		return hetznerOptions{}, err
	}

	images, err := hetznerList[hetznerAPIImage](ctx, c, "/images?type=snapshot&status=available", "images")
	if err != nil {
		return hetznerOptions{}, err
	}

	options := hetznerOptions{
		Currency:    pricing.Pricing.Currency,
		ServerTypes: make([]hetznerTypeOption, 0, len(types)),
		Locations:   locations,
		Snapshots:   hetznerSnapshots(images),
	}

	for _, t := range types {
		if option, ok := t.option(); ok {
			options.ServerTypes = append(options.ServerTypes, option)
		}
	}

	return options, nil
}

// option is the type as the apps show it, with the locations it can still be ordered in;
// false for a deprecated type, or one with no such location.
func (t hetznerAPIType) option() (hetznerTypeOption, bool) {
	if hetznerDeprecated(t.Deprecation) {
		return hetznerTypeOption{}, false
	}

	option := hetznerTypeOption{
		Name: t.Name, Cores: t.Cores, MemoryGB: t.Memory, DiskGB: t.Disk,
		Arch: hetznerTalosArch(t.Architecture), Prices: []hetznerPrice{},
	}

	for _, p := range t.Prices {
		if t.orderableIn(p.Location) {
			option.Prices = append(option.Prices, hetznerPrice{
				Location: p.Location, Hourly: hetznerAmount(p.Hourly.Net, 4), Monthly: hetznerAmount(p.Monthly.Net, 2),
			})
		}
	}

	return option, len(option.Prices) > 0
}

// orderableIn: not deprecated at location. An older answer without locations says nothing.
func (t hetznerAPIType) orderableIn(location string) bool {
	if len(t.Locations) == 0 {
		return true
	}

	for _, l := range t.Locations {
		if l.Name == location {
			return !hetznerDeprecated(l.Deprecation)
		}
	}

	return false
}

func hetznerDeprecated(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))

	return s != "" && s != "null"
}

// hetznerSnapshots puts the Talos snapshots first, then the newest. Every snapshot is listed:
// one made by hand may carry neither the label nor the word.
func hetznerSnapshots(images []hetznerAPIImage) []hetznerSnapshot {
	out := make([]hetznerSnapshot, 0, len(images))

	for _, i := range images {
		out = append(out, hetznerSnapshot{
			ID: i.ID, Description: i.Description, Labels: i.Labels, Created: i.Created,
			Arch: hetznerTalosArch(i.Architecture), OSFlavor: i.OSFlavor,
			Talos: strings.EqualFold(i.Labels["os"], "talos") || strings.Contains(strings.ToLower(i.Description), "talos"),
		})
	}

	slices.SortStableFunc(out, func(a, b hetznerSnapshot) int {
		if a.Talos != b.Talos {
			if a.Talos {
				return -1
			}

			return 1
		}

		return strings.Compare(b.Created, a.Created)
	})

	return out
}

func hetznerTalosArch(arch string) string {
	switch arch {
	case hetznerArchX86:
		return bootArchAMD64
	case hetznerArchARM:
		return bootArchARM64
	}

	return arch
}

// hetznerAmount rounds one of the API's decimal strings ("4.1500000000"); one it cannot read
// is shown as it came.
func hetznerAmount(s string, decimals int) string {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}

	return strconv.FormatFloat(v, 'f', decimals, 64)
}

// hetznerImagePath is GET /images/{id}.
func hetznerImagePath(id int64) string { return "/images/" + strconv.FormatInt(id, 10) }

// hetznerTypePath finds one server type by its name.
func hetznerTypePath(name string) string { return "/server_types?name=" + url.QueryEscape(name) }

func demoHetznerOptions() hetznerOptions {
	price := func(hourly, monthly string) []hetznerPrice {
		return []hetznerPrice{{Location: "fsn1", Hourly: hourly, Monthly: monthly}, {Location: "hel1", Hourly: hourly, Monthly: monthly}}
	}

	return hetznerOptions{
		Currency: "EUR",
		ServerTypes: []hetznerTypeOption{
			{Name: "cx23", Cores: 2, MemoryGB: 4, DiskGB: 40, Arch: bootArchAMD64, Prices: price("0.0060", "3.49")},
			{Name: "cx33", Cores: 4, MemoryGB: 8, DiskGB: 80, Arch: bootArchAMD64, Prices: price("0.0090", "5.49")},
			{Name: "cax11", Cores: 2, MemoryGB: 4, DiskGB: 40, Arch: bootArchARM64, Prices: price("0.0070", "3.79")},
		},
		Locations: []hetznerLocation{
			{Name: "fsn1", Description: "Falkenstein DC Park 1", City: "Falkenstein", Country: "DE"},
			{Name: "hel1", Description: "Helsinki DC Park 1", City: "Helsinki", Country: "FI"},
		},
		Snapshots: []hetznerSnapshot{
			{ID: 1001, Description: "talos " + demoTalosVersion + " amd64", Labels: map[string]string{"os": "talos"}, Created: "2026-10-01T09:00:00+00:00", Arch: bootArchAMD64, OSFlavor: "unknown", Talos: true},
			{ID: 1002, Description: "talos " + demoTalosVersion + " arm64", Labels: map[string]string{"os": "talos"}, Created: "2026-10-01T09:00:00+00:00", Arch: bootArchARM64, OSFlavor: "unknown", Talos: true},
		},
	}
}
