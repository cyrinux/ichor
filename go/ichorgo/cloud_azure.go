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
	"strconv"
	"strings"
	"time"
)

// S8 step 16: Azure virtual machines, on Azure Resource Manager (JSON over HTTPS) with a service
// principal's client secret: the client credentials grant of the AKS sign-in, for ARM's audience
// (never an AKS server's token). No Azure SDK in the app. The app seals the credential and passes
// it per call. See https://learn.microsoft.com/en-us/rest/api/azure/.

const (
	azureSubscriptionsVersion = "2022-12-01"
	azureResourcesVersion     = "2021-04-01"
	azureSKUsVersion          = "2021-07-01"
	azureNetworkVersion       = "2024-05-01"
	azureComputeVersion       = "2025-04-01"
	azurePermissionsVersion   = "2022-04-01"
	azurePricesVersion        = "2023-01-01-preview"

	azureOptionsTimeout = 60 * time.Second
	azurePriceTimeout   = 20 * time.Second
	// azureMaxBody bounds an answer: a location's SKU page runs to a few MB.
	azureMaxBody = 16 << 20
	// azureMaxPages bounds the nextLink pages of one listing.
	azureMaxPages = 20

	// The tags of what a run makes, and of an image the app made (the image child).
	azureTagRun          = "ichor-run"
	azureTagCluster      = "ichor-cluster"
	azureTagTalosVersion = "ichor-talos-version"
	azureTagSchematic    = "ichor-schematic"
	// azureTagArch is a managed image's architecture: ARM gives one for gallery images only.
	azureTagArch = "ichor-arch"

	azureCodeAuthorizationFailed = "AuthorizationFailed"
	azureCodeBadPrincipal        = "invalid_client"
	azureCodeNotFound            = "ResourceNotFound"
	azureCodeGroupNotFound       = "ResourceGroupNotFound"

	azureOpSucceeded = "Succeeded"
)

// azureARMHosts are the Resource Manager hosts of each cloud azureLoginHosts knows.
var azureARMHosts = map[string]string{
	"":                       "management.azure.com",
	"AzurePublicCloud":       "management.azure.com",
	"AzureChinaCloud":        "management.chinacloudapi.cn",
	"AzureUSGovernmentCloud": "management.usgovcloudapi.net",
}

// Azure endpoints, overridable in tests (as azureAuthority).
var (
	azureARMEndpoint    = func(environment string) string { return "https://" + azureARMHosts[environment] }
	azurePricesEndpoint = func() string { return "https://prices.azure.com" }

	errAzureNoCredential = errors.New("add a service principal first: its tenant id, client id, client secret and subscription id")

	azureGUIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$`)
	azureObjectPattern   = regexp.MustCompile(`client '[^']*' with object id '[^']*'`)
	azureActionPattern   = regexp.MustCompile(`perform action '([^']+)'`)
	azureLocationPattern = regexp.MustCompile(`^[a-z0-9]{2,40}$`)
	azureSizePattern     = regexp.MustCompile(`^[A-Za-z0-9_]{2,64}$`)
)

// azureError is a refusal: ARM's {"error":{"code","message"}}, or the token endpoint's
// {"error","error_description"}. Its message never carries the secret or the credential's ids
// (see scrub).
type azureError struct {
	action  string // Microsoft.Compute/virtualMachines/write
	code    string
	message string
	status  int
}

func (e *azureError) Error() string {
	switch e.code {
	case azureCodeAuthorizationFailed, "LinkedAuthorizationFailed":
		return "Azure refused " + e.action + ": give the service principal (or the signed-in account) the printed role on the resource group"
	case "InvalidAuthenticationToken", "ExpiredAuthenticationToken", "AuthenticationFailed":
		return "Azure: the token expired: sign in again"
	case azureCodeBadPrincipal:
		return "Azure refused the service principal: check the tenant, client id and secret"
	}

	return "Azure " + e.action + ": " + cmp.Or(e.message, e.code, http.StatusText(e.status))
}

// azureScrubbed is a transport error whose message went through scrub (a URL names the
// subscription); errors.Is still sees the cause.
type azureScrubbed struct {
	message string
	err     error
}

func (e *azureScrubbed) Error() string { return e.message }
func (e *azureScrubbed) Unwrap() error { return e.err }

// azureClient calls ARM with one service principal. The secret goes only to the token
// endpoint, the token only to ARM: never in a URL, a message or a log.
type azureClient struct {
	environment, authority, arm                  string
	tenant, clientID, clientSecret, subscription string
	// resourceGroup and location are the credential's hints (AzureTest, AzureOptions).
	resourceGroup, location string
	token                   string
	expiry                  time.Time
	// user is the Microsoft-account sign-in (cloud_azure_signin.go), nil for a service principal.
	user *azureUser
}

// azureCredential is credJSON: {"tenantId","clientId","clientSecret","subscriptionId",
// "environment"}, plus the optional "resourceGroup" and "location"; a Microsoft-account sign-in
// has "kind":"user" and a refresh token instead of the secret.
type azureCredential struct {
	TenantID       string `json:"tenantId"`
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
	SubscriptionID string `json:"subscriptionId"`
	Environment    string `json:"environment"`
	ResourceGroup  string `json:"resourceGroup"`
	Location       string `json:"location"`
	azureUserCredential
}

func parseAzureCredential(credJSON string) (azureCredential, error) {
	var c azureCredential

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &c); err != nil {
			return c, errors.New("invalid Azure service principal data") //nolint:nilerr // the parser's message may quote the secret
		}
	}

	c.Environment = strings.TrimSpace(c.Environment)
	if _, ok := azureARMHosts[c.Environment]; !ok {
		return c, fmt.Errorf("%q is not an Azure cloud (AzurePublicCloud, AzureChinaCloud, AzureUSGovernmentCloud)", c.Environment)
	}

	return c, nil
}

func newAzureClient(credJSON string) (*azureClient, error) {
	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return nil, err
	}

	if c.isUser() {
		return newAzureUserClient(c)
	}

	tenant, clientID, subscription := strings.TrimSpace(c.TenantID), strings.TrimSpace(c.ClientID), strings.TrimSpace(c.SubscriptionID)
	if tenant == "" || clientID == "" || c.ClientSecret == "" || subscription == "" {
		return nil, errAzureNoCredential
	}

	for _, id := range []struct{ name, value string }{{"tenant id", tenant}, {"client id", clientID}, {"subscription id", subscription}} {
		if !azureGUIDPattern.MatchString(id.value) {
			return nil, fmt.Errorf("the %s is not a GUID (32 hex digits in groups of 8-4-4-4-12)", id.name)
		}
	}

	location := strings.ToLower(strings.TrimSpace(c.Location))
	if location != "" && !azureLocationPattern.MatchString(location) {
		return nil, fmt.Errorf("%q is not an Azure location (westeurope)", location)
	}

	return &azureClient{
		environment: c.Environment, authority: azureAuthority(c.Environment, tenant), arm: strings.TrimSuffix(azureARMEndpoint(c.Environment), "/"),
		tenant: tenant, clientID: clientID, clientSecret: c.ClientSecret, subscription: subscription,
		resourceGroup: strings.TrimSpace(c.ResourceGroup), location: location,
	}, nil
}

// scope is ARM's audience in the client's cloud: a token for an AKS server is refused there.
func (c *azureClient) scope() string { return "https://" + azureARMHosts[c.environment] + "/.default" }

// scrub drops what a message must not carry: the secret, the token, the credential's ids (in
// any case), and the principal named in a refusal.
func (c *azureClient) scrub(message string) string {
	message = azureObjectPattern.ReplaceAllString(message, "client …")

	secrets := []string{c.clientSecret, c.token}
	if c.user != nil {
		secrets = append(secrets, c.user.cred.RefreshToken)
	}

	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "…")
		}
	}

	for _, id := range []string{c.tenant, c.clientID, c.subscription} {
		if id != "" {
			message = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(id)).ReplaceAllString(message, "…")
		}
	}

	return message
}

// mint gets a token for ARM with the client credentials grant, as azureSPNMethod.mint does, and
// keeps it for the run (minted again under the refresh margin).
func (c *azureClient) mint(ctx context.Context) error {
	if c.token != "" && time.Until(c.expiry) > tokenRefreshMargin {
		return nil
	}

	if c.user != nil {
		return c.mintUser(ctx)
	}

	o := &oidcMethod{method: authAzure, issuer: c.authority, clientID: c.clientID, clientSecret: c.clientSecret, tls: azureTLS()}

	d, err := o.discover(ctx)
	if err != nil {
		return &azureScrubbed{message: "Azure sign-in: " + c.scrub(err.Error()), err: err}
	}

	var t oidcTokens

	status, err := o.postForm(ctx, d.TokenEndpoint, url.Values{"grant_type": {"client_credentials"}, "scope": {c.scope()}}, &t)
	if err != nil {
		return &azureScrubbed{message: "Azure sign-in: " + c.scrub(err.Error()), err: err}
	}

	if t.Error != "" || t.AccessToken == "" {
		code := t.Error
		if code == "unauthorized_client" || strings.Contains(t.Description, "AADSTS7000215") || strings.Contains(t.Description, "AADSTS700016") ||
			strings.Contains(t.Description, "AADSTS90002") {
			code = azureCodeBadPrincipal
		}

		return &azureError{action: "sign-in", code: code, message: c.scrub(cmp.Or(t.Description, t.Error, "no access token")), status: status}
	}

	c.token, c.expiry = t.AccessToken, time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600))*time.Second)

	return nil
}

// url is ARM's address of path (relative, or an absolute nextLink or operation on the ARM
// host: the token goes nowhere else) with query added.
func (c *azureClient) url(path string, query url.Values) (string, error) {
	raw := c.arm + path
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		if !strings.HasPrefix(path, c.arm+"/") {
			return "", errors.New("an answer named an address outside Azure Resource Manager: refused")
		}

		raw = path
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("an Azure address that is not one")
	}

	q := u.Query()
	for k, v := range query {
		q[k] = v
	}

	u.RawQuery = q.Encode()

	return u.String(), nil
}

// azureAPIError is ARM's error body, and an operation's.
type azureAPIError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends one ARM call with the token and decodes its JSON answer into out. query carries the
// api-version.
func (c *azureClient) do(ctx context.Context, method, path string, query url.Values, body, out any) (int, http.Header, error) {
	if err := c.mint(ctx); err != nil {
		return 0, nil, err
	}

	u, err := c.url(path, query)
	if err != nil {
		return 0, nil, err
	}

	var reader io.Reader

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}

		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return 0, nil, &azureScrubbed{message: c.scrub(err.Error()), err: err}
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, azureMaxBody))
	if err != nil {
		return resp.StatusCode, resp.Header, &azureScrubbed{message: c.scrub(err.Error()), err: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e azureAPIError

		_ = json.Unmarshal(data, &e) //nolint:errcheck

		action := azureActionOf(method, path)
		if m := azureActionPattern.FindStringSubmatch(e.Error.Message); m != nil {
			action = m[1]
		}

		return resp.StatusCode, resp.Header, &azureError{action: action, code: e.Error.Code, message: c.scrub(e.Error.Message), status: resp.StatusCode}
	}

	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return resp.StatusCode, resp.Header, nil
	}

	return resp.StatusCode, resp.Header, json.Unmarshal(data, out)
}

// get is do for a read.
func (c *azureClient) get(ctx context.Context, path, version string, out any) error {
	_, _, err := c.do(ctx, http.MethodGet, path, url.Values{"api-version": {version}}, nil, out)

	return err
}

// azureActionOf is the RBAC action of an ARM call: its resource type and the verb (GET read, PUT
// write, DELETE delete). It names the action in a refusal, and the tests check the printed role
// against the calls.
func azureActionOf(method, path string) string {
	if u, err := url.Parse(path); err == nil {
		path = u.Path
	}

	verb := map[string]string{http.MethodGet: "read", http.MethodPut: "write", http.MethodDelete: "delete"}[method]
	if verb == "" {
		verb = "action"
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")

	for i, p := range parts {
		if strings.EqualFold(p, "providers") && i+1 < len(parts) {
			// providers/<namespace>/<type>/<name>/<type>/<name>…: the types.
			types := []string{parts[i+1]}
			for j := i + 2; j < len(parts); j += 2 {
				types = append(types, parts[j])
			}

			return strings.Join(types, "/") + "/" + verb
		}
	}

	// /subscriptions/<id>[/resourcegroups|/locations]
	types := []string{"Microsoft.Resources/subscriptions"}
	if len(parts) >= 3 {
		types = append(types, map[string]string{"resourcegroups": "resourceGroups", "locations": "locations"}[strings.ToLower(parts[2])])
	}

	return strings.Join(types, "/") + "/" + verb
}

// azureOperation is an Azure-AsyncOperation's answer.
type azureOperation struct {
	Status string `json:"status"`
	azureAPIError
}

// wait follows a long-running operation (its Azure-AsyncOperation, else its Location) until it
// ends, every interval or the Retry-After it asks for (at most 30 s).
func (c *azureClient) wait(ctx context.Context, headers http.Header, interval time.Duration) error {
	async, location := headers.Get("Azure-AsyncOperation"), headers.Get("Location")
	if async == "" && location == "" {
		return nil
	}

	for {
		delay := interval
		if s, err := strconv.Atoi(headers.Get("Retry-After")); err == nil && time.Duration(s)*time.Second > delay {
			delay = min(time.Duration(s)*time.Second, 30*time.Second)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		var op azureOperation

		target := cmp.Or(async, location)

		status, h, err := c.do(ctx, http.MethodGet, target, nil, nil, &op)

		switch {
		case err != nil:
			return err
		case async == "" && status == http.StatusAccepted:
			headers = h
		case async == "":
			return nil
		case op.Status == azureOpSucceeded:
			return nil
		case op.Status == "Failed" || op.Status == "Canceled":
			return &azureError{action: "operation", code: op.Error.Code, message: c.scrub(cmp.Or(op.Error.Message, op.Status)), status: status}
		default:
			headers = h
		}
	}
}

// azurePrice is one item of the Retail Prices API.
type azurePrice struct {
	RetailPrice   float64 `json:"retailPrice"`
	UnitOfMeasure string  `json:"unitOfMeasure"`
	CurrencyCode  string  `json:"currencyCode"`
	ProductName   string  `json:"productName"`
	MeterName     string  `json:"meterName"`
}

// azurePricing is the pay-as-you-go Linux hourly price in USD of a VM size in a location, from
// the unauthenticated Retail Prices API; "" when it does not answer in time or says nothing: not
// worth failing for. The public cloud only.
func azurePricing(ctx context.Context, location, size string) string {
	ctx, cancel := context.WithTimeout(ctx, azurePriceTimeout)
	defer cancel()

	filter := fmt.Sprintf("serviceName eq 'Virtual Machines' and armRegionName eq '%s' and armSkuName eq '%s' and priceType eq 'Consumption'", location, size)
	u := azurePricesEndpoint() + "/api/retail/prices?" + url.Values{"api-version": {azurePricesVersion}, "$filter": {filter}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}

	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close() //nolint:errcheck

	var answer struct {
		Items []azurePrice `json:"Items"`
	}

	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, azureMaxBody)).Decode(&answer) != nil {
		return ""
	}

	return azureHourlyPrice(answer.Items)
}

// azureHourlyPrice picks the Linux pay-as-you-go item: not Windows, not Spot, not Low Priority.
func azureHourlyPrice(items []azurePrice) string {
	for _, i := range items {
		if i.UnitOfMeasure != "1 Hour" || i.CurrencyCode != "USD" || strings.HasSuffix(i.ProductName, "Windows") ||
			strings.Contains(i.MeterName, "Spot") || strings.Contains(i.MeterName, "Low Priority") {
			continue
		}

		return strconv.FormatFloat(i.RetailPrice, 'f', 4, 64)
	}

	return ""
}

// azurePermission is one entry of a permissions listing: a role's actions less its notActions.
type azurePermission struct {
	Actions    []string `json:"actions"`
	NotActions []string `json:"notActions"`
}

// azureGlobMatch matches an RBAC action pattern (* anywhere, case-insensitive).
func azureGlobMatch(pattern, action string) bool {
	quoted := strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, `.*`)

	re, err := regexp.Compile(`(?i)^` + quoted + `$`)

	return err == nil && re.MatchString(action)
}

// azureAllowed: one entry allows action and does not take it back. A notActions hit wins over
// its own entry's actions only, as Azure computes it.
func azureAllowed(perms []azurePermission, action string) bool {
	for _, p := range perms {
		allowed := false

		for _, a := range p.Actions {
			if azureGlobMatch(a, action) {
				allowed = true

				break
			}
		}

		for _, n := range p.NotActions {
			if azureGlobMatch(n, action) {
				allowed = false

				break
			}
		}

		if allowed {
			return true
		}
	}

	return false
}

// azureCreateActions are the writes a run makes: a NIC joined to the subnet (and the security
// group), the VM, and their deletes; a public address when one is asked.
func azureCreateActions(publicIP bool) []string {
	actions := []string{
		"Microsoft.Network/networkInterfaces/write", "Microsoft.Network/networkInterfaces/join/action",
		"Microsoft.Network/virtualNetworks/subnets/join/action", "Microsoft.Compute/virtualMachines/write",
		"Microsoft.Compute/virtualMachines/delete", "Microsoft.Network/networkInterfaces/delete",
	}

	if publicIP {
		actions = append(actions, "Microsoft.Network/publicIPAddresses/write", "Microsoft.Network/publicIPAddresses/join/action",
			"Microsoft.Network/publicIPAddresses/delete")
	}

	return actions
}

// permissions lists what the principal may do in a resource group ("" for the subscription).
func (c *azureClient) permissions(ctx context.Context, resourceGroup string) ([]azurePermission, error) {
	scope := "/subscriptions/" + c.subscription
	if resourceGroup != "" {
		scope += "/resourceGroups/" + url.PathEscape(resourceGroup)
	}

	return azureList[azurePermission](ctx, c, scope+"/providers/Microsoft.Authorization/permissions", azurePermissionsVersion, nil)
}

// missingAction is the first of actions the listing does not allow, "" when all are.
func missingAction(perms []azurePermission, actions []string) string {
	for _, a := range actions {
		if !azureAllowed(perms, a) {
			return a
		}
	}

	return ""
}

// azureList reads every page of an ARM listing ({"value":[…],"nextLink"}).
func azureList[T any](ctx context.Context, c *azureClient, path, version string, query url.Values) ([]T, error) {
	q := url.Values{"api-version": {version}}
	for k, v := range query {
		q[k] = v
	}

	out := []T{}

	for range azureMaxPages {
		var page struct {
			Value    []T    `json:"value"`
			NextLink string `json:"nextLink"`
		}

		if _, _, err := c.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}

		out = append(out, page.Value...)

		if page.NextLink == "" {
			break
		}

		// The next link carries its own query.
		path, q = page.NextLink, nil
	}

	return out, nil
}

type azureTestResult struct {
	Tenant           string `json:"tenant"`
	Subscription     string `json:"subscription"`
	SubscriptionName string `json:"subscriptionName"`
	CanCreate        bool   `json:"canCreate"`
	Checked          bool   `json:"checked"`
	// Account is the signed-in Microsoft account, "" for a service principal.
	Account string `json:"account,omitempty"`
	// Credential is the credential JSON to seal again when the call rotated its refresh token.
	Credential string `json:"credential,omitempty"`
}

// AzureTest checks a service principal. JSON: {"tenant","subscription","subscriptionName",
// "canCreate","checked","account","credential"}: a token for ARM, the subscription (refused when disabled), then the
// principal's permissions on credJSON's "resourceGroup" (else the subscription) matched against
// the writes a run makes; checked false when the listing cannot be read or is empty. Azure has
// no dry run: this reads only, and creates nothing.
func AzureTest(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := newAzureClient(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), azureOptionsTimeout)
	defer cancel()

	var sub struct {
		DisplayName string `json:"displayName"`
		State       string `json:"state"`
	}

	if err := c.get(ctx, "/subscriptions/"+c.subscription, azureSubscriptionsVersion, &sub); err != nil {
		return "", err
	}

	if sub.State == "Disabled" || sub.State == "Deleted" {
		return "", fmt.Errorf("the subscription %s is %s: Azure creates nothing in it", sub.DisplayName, strings.ToLower(sub.State))
	}

	result := azureTestResult{Tenant: c.tenant, Subscription: c.subscription, SubscriptionName: sub.DisplayName, Account: c.account()}

	perms, err := c.permissions(ctx, c.resourceGroup)
	if err == nil && len(perms) > 0 {
		result.Checked = true
		result.CanCreate = missingAction(perms, azureCreateActions(false)) == ""
	}

	result.Credential = c.updated()

	return toJSON(result)
}

// azureRole is a custom role definition, as az role definition create reads it.
type azureRole struct {
	Name             string   `json:"Name"`
	IsCustom         bool     `json:"IsCustom"`
	Description      string   `json:"Description"`
	Actions          []string `json:"Actions"`
	NotActions       []string `json:"NotActions"`
	AssignableScopes []string `json:"AssignableScopes"`
}

// AzureRoleDefinition is the custom role to create (az role definition create --role-definition)
// and give the service principal on the resource group: only the calls the app makes. Azure
// cannot limit a delete to tagged resources: the app deletes only what its run named after the
// host name and tagged ichor-run. Placeholders stand for an empty id. The person pastes it.
func AzureRoleDefinition(subscriptionID, resourceGroup string) string {
	subscriptionID = cmp.Or(strings.TrimSpace(subscriptionID), "SUBSCRIPTION_ID")
	resourceGroup = cmp.Or(strings.TrimSpace(resourceGroup), "RESOURCE_GROUP")

	// Plain values: it always encodes.
	out, _ := toJSON(azureRole{ //nolint:errcheck
		Name: "Ichor node creator", IsCustom: true,
		Description: "Lets Ichor create a Talos node VM (with its NIC, public IP and OS disk) in this resource group, make a Talos image there " +
			"with a throwaway helper VM, and delete what it created",
		Actions: append([]string{
			"Microsoft.Resources/subscriptions/read", "Microsoft.Resources/subscriptions/locations/read",
			"Microsoft.Resources/subscriptions/resourceGroups/read", "Microsoft.Compute/skus/read", "Microsoft.Compute/images/read",
			"Microsoft.Compute/galleries/read", "Microsoft.Compute/galleries/images/read", "Microsoft.Compute/galleries/images/versions/read",
			"Microsoft.Compute/virtualMachines/read", "Microsoft.Compute/virtualMachines/instanceView/read",
			"Microsoft.Compute/virtualMachines/write", "Microsoft.Compute/virtualMachines/delete",
			"Microsoft.Compute/disks/read", "Microsoft.Compute/disks/delete",
			"Microsoft.Network/virtualNetworks/read", "Microsoft.Network/virtualNetworks/subnets/read",
			"Microsoft.Network/virtualNetworks/subnets/join/action", "Microsoft.Network/networkSecurityGroups/read",
			"Microsoft.Network/networkSecurityGroups/join/action", "Microsoft.Network/networkInterfaces/read",
			"Microsoft.Network/networkInterfaces/write", "Microsoft.Network/networkInterfaces/join/action",
			"Microsoft.Network/networkInterfaces/delete", "Microsoft.Network/publicIPAddresses/read",
			"Microsoft.Network/publicIPAddresses/write", "Microsoft.Network/publicIPAddresses/join/action",
			"Microsoft.Network/publicIPAddresses/delete", "Microsoft.Authorization/permissions/read",
		}, azureImageActions...),
		NotActions:       []string{},
		AssignableScopes: []string{"/subscriptions/" + subscriptionID + "/resourceGroups/" + resourceGroup},
	})

	return out
}

type azurePriceResult struct {
	Hourly   string `json:"hourly"`
	Currency string `json:"currency,omitempty"`
}

// AzurePrice is the pay-as-you-go Linux hourly price of one VM size in a location, before tax:
// {"hourly":"0.0480","currency":"USD"}, or {"hourly":""} when the Retail Prices API does not
// answer, or in a sovereign cloud (it prices the public one only). Unauthenticated: credJSON is
// read for its "environment" only. One call: the app asks for the chosen size only.
func AzurePrice(credJSON, location, size string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return "", err
	}

	location, size = strings.ToLower(strings.TrimSpace(location)), strings.TrimSpace(size)

	switch {
	case !azureLocationPattern.MatchString(location):
		return "", fmt.Errorf("%q is not an Azure location (westeurope)", location)
	case !azureSizePattern.MatchString(size):
		return "", fmt.Errorf("%q is not a VM size (Standard_B2s)", size)
	}

	if azureARMHosts[c.Environment] != azureARMHosts[""] {
		return toJSON(azurePriceResult{})
	}

	hourly := azurePricing(context.Background(), location, size)
	if hourly == "" {
		return toJSON(azurePriceResult{})
	}

	return toJSON(azurePriceResult{Hourly: hourly, Currency: "USD"})
}
