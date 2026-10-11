package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/url"
	"strings"
)

// GKE with Workforce Identity Federation, signed in in the browser from the login config the
// organisation's administrator hands out (`gcloud iam workforce-pools create-login-config`),
// as `gcloud auth application-default login --login-config` does on a computer: Google's
// workforce page (auth_url) sends the browser on to the organisation's identity provider and
// back to a loopback redirect with a code, exchanged at Google STS (token_url) with the client
// in a Basic header. The result is stored in the shape an imported workforce session has
// (gcpUserCredentialsJson, type external_account_authorized_user), so the refresh, its
// rotation and invalid_grant follow mintForGoogleUser. The config itself holds no secret of
// the user and is remembered, so signing in again is one tap.

const (
	gcpFieldLoginConfig = "gcpLoginConfigJson"
	gcpTypeLoginConfig  = "external_account_authorized_user_login_config"
	// gcpWorkforceSignInHint is shown before the first sign-in and once STS refuses the session.
	gcpWorkforceSignInHint = "sign in again in the browser with your workforce login config"
)

// gcloudCLIClient is the OAuth client `gcloud auth login --login-config` and
// `gcloud auth application-default login --login-config` sign in with (a login config names
// none), overridable in tests. Both values are CLOUDSDK_CLIENT_ID and
// CLOUDSDK_CLIENT_NOTSOSECRET of gcloud's open-source googlecloudsdk/core/config.py
// (https://raw.githubusercontent.com/google-cloud-sdk-unofficial/google-cloud-sdk/master/lib/googlecloudsdk/core/config.py);
// Google's workforce page refuses the ADC client gcloudClient holds. They belong to Google,
// not to Ichor, and are not a secret (an installed app's client). Keep them here only.
var gcloudCLIClient = func() (id, secret string) {
	return "32555940559.apps.googleusercontent.com", "ZmssLNjJy2998hD4CTg2ejr2"
}

// gcpLoginConfig is a workforce identity login config. ClientID and ClientSecret are
// optional: gcloud's own configs carry none and gcloud's client is used then.
type gcpLoginConfig struct {
	Type         string `json:"type"`
	Audience     string `json:"audience"`
	AuthURL      string `json:"auth_url"`
	TokenURL     string `json:"token_url"`
	TokenInfoURL string `json:"token_info_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func parseLoginConfig(raw string) (gcpLoginConfig, error) {
	var c gcpLoginConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, errors.New("the workforce login config is not JSON")
	}

	switch {
	case c.Type == gcpTypeWorkforceUser:
		return c, errors.New("this is a workforce identity session, not a login config: choose gcloud user credentials")
	case c.Type != gcpTypeLoginConfig:
		return c, errors.New("not a workforce login config (type " + gcpTypeLoginConfig + ")")
	case c.providerName() == "":
		return c, errors.New("the login config names no workforce pool provider (audience)")
	case !httpsURL(c.AuthURL):
		return c, errors.New("the login config's auth_url is not an https address")
	case !httpsURL(c.TokenURL):
		return c, errors.New("the login config's token_url is not an https address")
	case c.ClientSecret != "" && c.ClientID == "":
		return c, errors.New("the login config has a client_secret without its client_id")
	}

	return c, nil
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)

	return err == nil && u.Scheme == "https" && u.Host != ""
}

// providerName is what the workforce page takes as provider_name, as gcloud sends it: the
// audience from "locations/" on (locations/global/workforcePools/<pool>/providers/<provider>).
func (c gcpLoginConfig) providerName() string {
	i := strings.Index(c.Audience, "/locations/")
	if i < 0 || !strings.Contains(c.Audience[i:], "/workforcePools/") || !strings.Contains(c.Audience[i:], "/providers/") {
		return ""
	}

	return c.Audience[i+1:]
}

// client is the config's OAuth client, else gcloud's.
func (c gcpLoginConfig) client() (id, secret string) {
	if c.ClientID != "" {
		return c.ClientID, c.ClientSecret
	}

	return gcloudCLIClient()
}

// workforceSignInMethod is the browser sign-in of a login config: its own endpoints (no
// discovery), the cloud-platform scope alone (the workforce page refuses openid), PKCE,
// access_type=offline and provider_name as gcloud sends them, a localhost loopback redirect,
// and the client in a Basic header at STS.
func workforceSignInMethod(cfg gcpLoginConfig) *oidcMethod {
	id, secret := cfg.client()

	return &oidcMethod{
		method: authGKE, clientID: id, clientSecret: secret,
		scopes: []string{gcpScope}, scopesOnly: true, useAccessToken: true,
		listen: []string{"127.0.0.1:0"}, redirectHost: "localhost", tls: gcpOAuthTLS(),
		endpoints:       &oidcDiscovery{AuthorizationEndpoint: cfg.AuthURL, TokenEndpoint: gcpTokenTarget(cfg.TokenURL)},
		basicClientAuth: true,
		authQuery:       url.Values{"access_type": {"offline"}, "provider_name": {cfg.providerName()}},
	}
}

// loginConfigState is the state of a login config entered (s), with the session a previous
// sign-in stored when it is a workforce session of the same pool provider.
func loginConfigState(s map[string]string) (kubeAuthState, error) {
	cfg, err := parseLoginConfig(s[gcpFieldLoginConfig])
	if err != nil {
		return kubeAuthState{}, err
	}

	if creds, err := parseUserCredentials(s[gcpFieldUserCredentials]); err == nil && creds.workforce() && creds.Audience == cfg.Audience {
		return kubeAuthState{Secrets: pick(s, gcpFieldLoginConfig, gcpFieldUserCredentials), User: workforceLabel(cfg.Audience)}, nil
	}

	return kubeAuthState{Secrets: pick(s, gcpFieldLoginConfig)}, nil
}

// errWorkforceSignIn is a login config before its browser sign-in: the app opens the browser.
var errWorkforceSignIn = signInRequired(authGKE, "sign in in the browser with your workforce login config")

// signInWorkforce is gkeMethod.signIn for a login config: the browser sign-in, then its
// refresh token stored as a workforce identity session and its access token kept in the
// session.
func signInWorkforce(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	cfg, err := parseLoginConfig(state.secret(gcpFieldLoginConfig))
	if err != nil {
		return state, err
	}

	start := state.withSession(map[string]string{"refresh": "", "bearer": "", "bearerExpiry": ""})

	signed, err := workforceSignInMethod(cfg).signIn(ctx, start, prompt, callbacks)
	if err != nil {
		return state, err
	}

	refresh := signed.session("refresh")
	if refresh == "" {
		return state, errors.New("google gave no refresh token: sign in again")
	}

	id, secret := cfg.client()

	raw, err := json.Marshal(gcpUserCredentials{
		Type: gcpTypeWorkforceUser, Audience: cfg.Audience, ClientID: id, ClientSecret: secret, RefreshToken: refresh,
		TokenURL: cfg.TokenURL, TokenInfoURL: cfg.TokenInfoURL,
	})
	if err != nil {
		return state, err
	}

	out := signed.withSession(map[string]string{"refresh": ""})
	out.Secrets = maps.Clone(state.Secrets)

	if out.Secrets == nil {
		out.Secrets = map[string]string{}
	}

	out.Secrets[gcpFieldUserCredentials] = string(raw)
	out.User = workforceLabel(cfg.Audience)

	return out, nil
}
