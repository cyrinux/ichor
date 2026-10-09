package ichorgo

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"strconv"
	"time"
)

// AKS with Entra ID, as Azure's kubelogin does it: an Entra access token for the AKS server
// application (--server-id) is the bearer. A user signs in with a device code (or in the
// browser with --login interactive); a service principal with its client secret.

const (
	azureFieldClientID     = "azureClientId"
	azureFieldClientSecret = "azureClientSecret"
)

// azureLoginHosts are the Entra ID hosts of each cloud kubelogin knows.
var azureLoginHosts = map[string]string{
	"":                       "login.microsoftonline.com",
	"AzurePublicCloud":       "login.microsoftonline.com",
	"AzureChinaCloud":        "login.chinacloudapi.cn",
	"AzureUSGovernmentCloud": "login.microsoftonline.us",
}

// azureTLS is the TLS setup for Entra ID, overridable in tests.
var azureTLS = func() *tls.Config { return baseTLS(nil, false) }

// azureAuthority is overridable in tests.
var azureAuthority = func(environment, tenant string) string {
	host, ok := azureLoginHosts[environment]
	if !ok {
		host = azureLoginHosts[""]
	}

	return "https://" + host + "/" + url.PathEscape(tenant) + "/v2.0"
}

func init() {
	extraSignInMethods[authAzure] = func(user *kubeStoreUser, _ *kubeStoreCluster) (signInMethod, error) {
		return newAzureMethod(user)
	}
}

func newAzureMethod(user *kubeStoreUser) (signInMethod, error) {
	var login, serverID, clientID, tenant, environment string

	if e := user.User.Exec; e != nil {
		login = cmpOr(flagValue(e.Args, "--login", "-l"), "devicecode")
		serverID = flagValue(e.Args, "--server-id")
		clientID = flagValue(e.Args, "--client-id")
		tenant = flagValue(e.Args, "--tenant-id", "-t")
		environment = flagValue(e.Args, "--environment", "-e")
	} else {
		cfg, _ := user.User.AuthProvider["config"].(map[string]any)
		str := func(key string) string { s, _ := cfg[key].(string); return s }
		login, serverID, clientID, tenant, environment = "devicecode", str("apiserver-id"), str("client-id"), str("tenant-id"), str("environment")
	}

	if serverID == "" || tenant == "" {
		return nil, errors.New("the kubeconfig names no AKS server ID and tenant")
	}

	if login == "spn" {
		return &azureSPNMethod{authority: azureAuthority(environment, tenant), serverID: serverID, clientID: clientID}, nil
	}

	// devicecode, interactive, and the logins a phone cannot do (azurecli, msi,
	// workloadidentity): a user sign-in for the same server.
	return &oidcMethod{
		method: authAzure, issuer: azureAuthority(environment, tenant), clientID: clientID,
		scopes: []string{serverID + "/.default"}, deviceCode: login != "interactive", useAccessToken: true,
		listen: []string{"127.0.0.1:0"}, redirectHost: "localhost", tls: azureTLS(),
	}, nil
}

// azureSPNMethod is a service principal: the client credentials grant.
type azureSPNMethod struct {
	authority, serverID, clientID string
}

func (m *azureSPNMethod) name() string { return authAzure }

func (m *azureSPNMethod) fieldSets() [][]string {
	return [][]string{{azureFieldClientID, azureFieldClientSecret}}
}

// rememberedFields: the client ID is shown again when the secret was rotated; the secret is not.
func (m *azureSPNMethod) rememberedFields() []string { return []string{azureFieldClientID} }

func (m *azureSPNMethod) fromSecrets(s map[string]string) (kubeAuthState, error) {
	if s[azureFieldClientSecret] == "" || cmpOr(s[azureFieldClientID], m.clientID) == "" {
		return kubeAuthState{}, errors.New("enter the service principal's client ID and secret")
	}

	return kubeAuthState{Secrets: pick(s, azureFieldClientID, azureFieldClientSecret)}, nil
}

func (m *azureSPNMethod) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	if bearer := state.session("bearer"); bearer != "" {
		exp, _ := strconv.ParseInt(state.session("bearerExpiry"), 10, 64)
		if time.Until(time.Unix(exp, 0)) > tokenRefreshMargin {
			return bearer, time.Unix(exp, 0), state, nil
		}
	}

	secret := state.secret(azureFieldClientSecret)
	if secret == "" {
		return "", time.Time{}, state, signInRequired(authAzure, "")
	}

	clientID := cmpOr(state.secret(azureFieldClientID), m.clientID)
	o := &oidcMethod{method: authAzure, issuer: m.authority, clientID: clientID, clientSecret: secret, tls: azureTLS()}

	d, err := o.discover(ctx)
	if err != nil {
		return "", time.Time{}, state, err
	}

	var t oidcTokens
	if _, err := o.postForm(ctx, d.TokenEndpoint, url.Values{"grant_type": {"client_credentials"}, "scope": {m.serverID + "/.default"}}, &t); err != nil {
		return "", time.Time{}, state, err
	}

	if t.Error != "" || t.AccessToken == "" {
		return "", time.Time{}, state, signInRequired(authAzure, cmpOr(t.Description, t.Error, "no access token"))
	}

	expiry := time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600)) * time.Second)
	state = state.withSession(map[string]string{"bearer": t.AccessToken, "bearerExpiry": strconv.FormatInt(expiry.Unix(), 10)})
	state.User = clientID

	return t.AccessToken, expiry, state, nil
}
