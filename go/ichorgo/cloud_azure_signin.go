package ichorgo

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// S8 step 20: the VM create signed in with a Microsoft account instead of a service principal:
// Entra ID's device code, as `az login --use-device-code`, for ARM's audience with offline_access.
// The refresh token is the credential the app seals; the client mints ARM tokens with it and
// hands the rotated one back ("credential" in AzureTest and AzureOptions), which the app seals
// again. Entra ID keeps a replaced refresh token valid until it ends, so a run that rotates it
// and returns nothing loses nothing. Nothing is kept in Go between calls.

const (
	azureKindUser = "user"
	// azureCLIClientID is Azure CLI's public client: device code and ARM tokens in any tenant
	// that does not block it. A person's own registration replaces it.
	azureCLIClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"
	// azureAnyWorkTenant signs in a work or school account of any tenant.
	azureAnyWorkTenant  = "organizations"
	azureSignInTimeout  = 60 * time.Second
	azureCodeSignInUser = "invalid_grant"
)

var (
	errAzureSignInAgain = errors.New("the Microsoft account sign-in has ended: sign in again")
	errAzureNoUser      = errors.New("sign in with a Microsoft account first")

	// azureTenantPattern is a tenant id, organizations, or a verified domain (contoso.onmicrosoft.com).
	azureTenantPattern = regexp.MustCompile(`^([0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}|organizations|common|[a-z0-9-]+(\.[a-z0-9-]+)+)$`)
)

// azureUserCredential is the part of the credential JSON a Microsoft-account sign-in adds.
type azureUserCredential struct {
	Kind         string `json:"kind,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	Account      string `json:"account,omitempty"`
	// HomeTenantID is the account's own tenant, the one a subscription without a tenant uses.
	HomeTenantID string `json:"homeTenantId,omitempty"`
	// Device is the code between AzureSignInStart and AzureSignInFinish.
	Device *azureStoredDevice `json:"device,omitempty"`
	// Choices are the subscriptions between AzureSignInFinish and AzureSignInChoose.
	Choices []azureSubscriptionChoice `json:"choices,omitempty"`
}

type azureStoredDevice struct {
	DeviceCode string `json:"deviceCode"`
	ExpiresIn  int    `json:"expiresIn"`
	Interval   int    `json:"interval"`
}

type azureSubscriptionChoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	TenantID string `json:"tenantId"`
}

func (c azureCredential) isUser() bool { return c.Kind == azureKindUser }

// encode is the credential as JSON; plain strings and numbers, so it always encodes.
func (c azureCredential) encode() string {
	data, _ := json.Marshal(c) //nolint:errcheck

	return string(data)
}

// azureUser is a client's Microsoft-account side: the refresh token it mints with, and the
// credential to hand back once Entra ID rotated it.
type azureUser struct {
	cred    azureCredential
	rotated bool
}

// newAzureUserClient is newAzureClient for a signed-in Microsoft account.
func newAzureUserClient(c azureCredential) (*azureClient, error) {
	tenant, clientID, subscription := strings.TrimSpace(c.TenantID), cmp.Or(strings.TrimSpace(c.ClientID), azureCLIClientID), strings.TrimSpace(c.SubscriptionID)

	switch {
	case c.RefreshToken == "":
		return nil, errAzureSignInAgain
	case subscription == "":
		return nil, errors.New("pick a subscription of the Microsoft account")
	case !azureGUIDPattern.MatchString(subscription) || !azureGUIDPattern.MatchString(clientID):
		return nil, errors.New("the subscription id or client id is not a GUID (32 hex digits in groups of 8-4-4-4-12)")
	case !azureTenantPattern.MatchString(tenant):
		return nil, fmt.Errorf("%q is not an Azure tenant (a tenant id or organizations)", tenant)
	}

	location := strings.ToLower(strings.TrimSpace(c.Location))
	if location != "" && !azureLocationPattern.MatchString(location) {
		return nil, fmt.Errorf("%q is not an Azure location (westeurope)", location)
	}

	c.ClientID = clientID

	return &azureClient{
		environment: c.Environment, authority: azureAuthority(c.Environment, tenant), arm: strings.TrimSuffix(azureARMEndpoint(c.Environment), "/"),
		tenant: tenant, clientID: clientID, subscription: subscription, resourceGroup: strings.TrimSpace(c.ResourceGroup), location: location,
		user: &azureUser{cred: c},
	}, nil
}

// updated is the credential JSON to seal again when this client's refresh token rotated, else "".
func (c *azureClient) updated() string {
	if c.user == nil || !c.user.rotated {
		return ""
	}

	return c.user.cred.encode()
}

// account is the signed-in account, "" for a service principal.
func (c *azureClient) account() string {
	if c.user == nil {
		return ""
	}

	return c.user.cred.Account
}

// mintUser gets an ARM token with the refresh token (the refresh grant), keeping the new refresh
// token Entra ID sends with it.
func (c *azureClient) mintUser(ctx context.Context) error {
	t, status, err := azureTokenRequest(ctx, c.authority, c.clientID, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {c.user.cred.RefreshToken}, "scope": {c.scope() + " offline_access"},
	})
	if err != nil {
		return &azureScrubbed{message: "Azure sign-in: " + c.scrub(err.Error()), err: err}
	}

	if t.Error != "" || t.AccessToken == "" {
		if refusal := azureSignInRefusal(t); refusal != nil {
			return refusal
		}

		return &azureError{action: "sign-in", code: t.Error, message: c.scrub(cmp.Or(t.Description, t.Error, "no access token")), status: status}
	}

	if t.RefreshToken != "" && t.RefreshToken != c.user.cred.RefreshToken {
		c.user.cred.RefreshToken, c.user.rotated = t.RefreshToken, true
	}

	c.token, c.expiry = t.AccessToken, time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600))*time.Second)

	return nil
}

// azureTokenRequest posts form to the token endpoint of authority as the public client clientID.
func azureTokenRequest(ctx context.Context, authority, clientID string, form url.Values) (oidcTokens, int, error) {
	o := &oidcMethod{method: authAzure, issuer: authority, clientID: clientID, tls: azureTLS()}

	d, err := o.discover(ctx)
	if err != nil {
		return oidcTokens{}, 0, err
	}

	var t oidcTokens

	status, err := o.postForm(ctx, d.TokenEndpoint, form, &t)

	return t, status, err
}

// azureSignInRefusal names what to fix in a refused sign-in or refresh: the tenant, the app
// registration, or signing in again. nil for anything else. Never quotes a token.
func azureSignInRefusal(t oidcTokens) error {
	switch d := t.Description; {
	case strings.Contains(d, "AADSTS50059") || strings.Contains(d, "AADSTS90002"):
		return errors.New("this tenant is unknown to Azure: enter the tenant id (a personal Microsoft account needs the tenant of its subscription)")
	case strings.Contains(d, "AADSTS7000218") || strings.Contains(d, "AADSTS70002"):
		return errors.New("the client id was refused: the app registration must allow public client flows")
	case strings.Contains(d, "AADSTS53003") || strings.Contains(d, "AADSTS50105"):
		return errors.New("the tenant blocks this client: enter the client id of an app registration it allows")
	case t.Error == azureCodeSignInUser || t.Error == "interaction_required":
		return errAzureSignInAgain
	}

	return nil
}

// azureIDClaims is what the sign-in reads of Entra ID's ID token: who, and their tenant.
type azureIDClaims struct {
	Username string `json:"preferred_username"`
	UPN      string `json:"upn"`
	TenantID string `json:"tid"`
}

func readAzureIDToken(token string) azureIDClaims {
	var c azureIDClaims

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c
	}

	if payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "=")); err == nil {
		_ = json.Unmarshal(payload, &c) //nolint:errcheck // an unreadable ID token names nobody
	}

	return c
}

type azureSignInStartResult struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
	Credential      string `json:"credential"`
}

// AzureSignInStart begins a Microsoft-account sign-in with a device code, as `az login
// --use-device-code`: credJSON names the tenant ("organizations" when ""), the client id (Azure
// CLI's when "") and the cloud. JSON: {"verificationUri","userCode","expiresIn","interval",
// "credential"}: the app shows the code, opens the link, and passes credential to
// AzureSignInFinish.
func AzureSignInStart(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return "", err
	}

	c = azureCredential{
		TenantID: cmp.Or(strings.TrimSpace(c.TenantID), azureAnyWorkTenant), ClientID: cmp.Or(strings.TrimSpace(c.ClientID), azureCLIClientID),
		Environment: c.Environment, azureUserCredential: azureUserCredential{Kind: azureKindUser},
	}

	if !azureTenantPattern.MatchString(c.TenantID) {
		return "", fmt.Errorf("%q is not an Azure tenant (a tenant id or organizations)", c.TenantID)
	}

	if !azureGUIDPattern.MatchString(c.ClientID) {
		return "", errors.New("the client id is not a GUID (32 hex digits in groups of 8-4-4-4-12)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), azureSignInTimeout)
	defer cancel()

	a, err := azureDeviceStart(ctx, c)
	if err != nil {
		return "", err
	}

	c.Device = &azureStoredDevice{DeviceCode: a.DeviceCode, ExpiresIn: a.ExpiresIn, Interval: a.Interval}

	return toJSON(azureSignInStartResult{
		VerificationURI: cmp.Or(a.VerificationURI, a.VerificationURL), UserCode: a.UserCode, ExpiresIn: a.ExpiresIn, Interval: a.Interval,
		Credential: c.encode(),
	})
}

// azureDeviceScope is what the sign-in asks: ARM, a refresh token, and who signed in.
func azureDeviceScope(environment string) string {
	return "https://" + azureARMHosts[environment] + "/.default offline_access openid profile"
}

func azureDeviceStart(ctx context.Context, c azureCredential) (oidcDeviceAnswer, error) {
	o := &oidcMethod{method: authAzure, issuer: azureAuthority(c.Environment, c.TenantID), clientID: c.ClientID, tls: azureTLS()}

	d, err := o.discover(ctx)
	if err != nil {
		return oidcDeviceAnswer{}, fmt.Errorf("sign-in to Azure: %w", err)
	}

	if d.DeviceAuthorizationEndpoint == "" {
		return oidcDeviceAnswer{}, errors.New("sign-in to Azure: no device code is offered here")
	}

	var a oidcDeviceAnswer
	if _, err := o.postForm(ctx, d.DeviceAuthorizationEndpoint, url.Values{"scope": {azureDeviceScope(c.Environment)}}, &a); err != nil {
		return oidcDeviceAnswer{}, fmt.Errorf("sign-in to Azure: %w", err)
	}

	if a.Error != "" || a.DeviceCode == "" {
		if refusal := azureSignInRefusal(oidcTokens{Error: a.Error, Description: a.Description}); refusal != nil {
			return oidcDeviceAnswer{}, refusal
		}

		return oidcDeviceAnswer{}, fmt.Errorf("the device code was refused: %s", cmp.Or(a.Error, "no code"))
	}

	return a, nil
}

// azureSignInWait is the AzureSignInFinish waiting for the browser, which AzureSignInCancel ends.
var azureSignInWait struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

type azureSignInFinishResult struct {
	Credential    string                    `json:"credential"`
	Account       string                    `json:"account"`
	Subscriptions []azureSubscriptionChoice `json:"subscriptions"`
}

// AzureSignInFinish waits until the code of AzureSignInStart is approved in the browser (or
// refused, or expired, or AzureSignInCancel), then lists the subscriptions the account sees.
// JSON: {"credential","account","subscriptions":[{"id","name","tenantId"}]}, sorted by name; the
// app picks one (the only one by itself) and calls AzureSignInChoose.
func AzureSignInFinish(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return "", err
	}

	if !c.isUser() || c.Device == nil || c.Device.DeviceCode == "" {
		return "", errors.New("start the Microsoft account sign-in first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cmpOrInt(int64(c.Device.ExpiresIn), 900))*time.Second+azureSignInTimeout)
	defer cancel()

	azureSignInWait.mu.Lock()
	if azureSignInWait.cancel != nil {
		azureSignInWait.cancel()
	}
	azureSignInWait.cancel = cancel
	azureSignInWait.mu.Unlock()

	t, err := azureDeviceFinish(ctx, c)
	if err != nil {
		return "", err
	}

	claims := readAzureIDToken(t.IDToken)
	c.RefreshToken, c.Account, c.HomeTenantID, c.Device = t.RefreshToken, cmp.Or(claims.Username, claims.UPN), claims.TenantID, nil

	subs, err := azureUserSubscriptions(ctx, c, t.AccessToken)
	if err != nil {
		return "", err
	}

	c.Choices = subs

	return toJSON(azureSignInFinishResult{Credential: c.encode(), Account: c.Account, Subscriptions: subs})
}

func azureDeviceFinish(ctx context.Context, c azureCredential) (oidcTokens, error) {
	device := oidcDeviceAnswer{DeviceCode: c.Device.DeviceCode, ExpiresIn: c.Device.ExpiresIn, Interval: c.Device.Interval}
	authority := azureAuthority(c.Environment, c.TenantID)

	t, err := pollDeviceToken(ctx, device, func(ctx context.Context) (oidcTokens, error) {
		t, _, err := azureTokenRequest(ctx, authority, c.ClientID, url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {device.DeviceCode},
		})
		if refusal := azureSignInRefusal(t); err == nil && refusal != nil && t.Error != azureCodeSignInUser {
			return oidcTokens{}, refusal
		}

		return t, err
	})

	switch {
	case errors.Is(err, context.Canceled):
		return oidcTokens{}, errors.New("sign-in cancelled")
	case err != nil:
		return oidcTokens{}, fmt.Errorf("sign-in to Azure: %w", err)
	case t.RefreshToken == "" || t.AccessToken == "":
		return oidcTokens{}, errors.New("sign-in to Azure: no refresh token came back (offline_access refused)")
	}

	return t, nil
}

// azureUserSubscriptions lists the enabled subscriptions the account sees, with the access token
// the sign-in just got (its tenant's: a subscription of another tenant is reached on Choose).
func azureUserSubscriptions(ctx context.Context, cred azureCredential, token string) ([]azureSubscriptionChoice, error) {
	c := &azureClient{
		environment: cred.Environment, arm: strings.TrimSuffix(azureARMEndpoint(cred.Environment), "/"), token: token,
		expiry: time.Now().Add(time.Hour), clientID: cred.ClientID, user: &azureUser{cred: cred},
	}

	subs, err := azureList[struct {
		ID       string `json:"subscriptionId"`
		Name     string `json:"displayName"`
		TenantID string `json:"tenantId"`
		State    string `json:"state"`
	}](ctx, c, "/subscriptions", azureSubscriptionsVersion, nil)
	if err != nil {
		return nil, err
	}

	out := []azureSubscriptionChoice{}

	for _, s := range subs {
		if s.State == "" || s.State == "Enabled" {
			out = append(out, azureSubscriptionChoice{ID: s.ID, Name: s.Name, TenantID: cmp.Or(s.TenantID, cred.HomeTenantID)})
		}
	}

	if len(out) == 0 {
		return nil, errors.New("this Microsoft account sees no enabled Azure subscription")
	}

	slices.SortFunc(out, func(a, b azureSubscriptionChoice) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })

	return out, nil
}

// AzureSignInCancel ends a waiting AzureSignInFinish (the sheet closed).
func AzureSignInCancel() {
	azureSignInWait.mu.Lock()
	defer azureSignInWait.mu.Unlock()

	if azureSignInWait.cancel != nil {
		azureSignInWait.cancel()
		azureSignInWait.cancel = nil
	}
}

type azureSignInChooseResult struct {
	Credential       string `json:"credential"`
	Account          string `json:"account"`
	SubscriptionName string `json:"subscriptionName"`
}

// AzureSignInChoose takes one subscription of AzureSignInFinish's list: the credential is now
// bound to it and to its tenant, and a token for that tenant is minted to prove it. JSON:
// {"credential","account","subscriptionName"}; the app then tests and seals credential under the
// subscription, as it does a service principal.
func AzureSignInChoose(credJSON, subscriptionID string) (out string, err error) {
	defer maskResult(&out, &err)

	c, err := parseAzureCredential(credJSON)
	if err != nil {
		return "", err
	}

	if !c.isUser() || c.RefreshToken == "" {
		return "", errAzureNoUser
	}

	subscriptionID = strings.TrimSpace(subscriptionID)

	i := slices.IndexFunc(c.Choices, func(s azureSubscriptionChoice) bool { return strings.EqualFold(s.ID, subscriptionID) })
	if i < 0 {
		return "", errors.New("pick one of the subscriptions the sign-in listed")
	}

	choice := c.Choices[i]
	c.SubscriptionID, c.TenantID, c.Choices = choice.ID, cmp.Or(choice.TenantID, c.HomeTenantID, c.TenantID), nil

	client, err := newAzureUserClient(c)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), azureSignInTimeout)
	defer cancel()

	if err := client.mint(ctx); err != nil {
		return "", err
	}

	return toJSON(azureSignInChooseResult{Credential: client.user.cred.encode(), Account: c.Account, SubscriptionName: choice.Name})
}
