package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
)

// GKE with the user's Google account in the browser, as `gcloud auth application-default
// login` does on a computer: the same Desktop-client flow as the organisation's OAuth client
// (kube_auth_gke_oauth.go), with gcloud's own public client, so there is nothing to enter and
// it works on every build. The result is stored in the shape an imported
// application_default_credentials.json has (gcpUserCredentialsJson, type authorized_user), so
// the refresh, its rotation and invalid_grant follow mintForGoogleUser, and backups keep the
// session as they keep an imported file.

const (
	// gcpFieldGcloudSignIn marks the option (value "1"): the apps draw a Sign in button for
	// it, never a text field.
	gcpFieldGcloudSignIn = "gcpGcloudSignIn"
	// gcpGcloudRenewHint is shown once Google refuses the stored session.
	gcpGcloudRenewHint = "sign in again with your Google account in the browser"
)

// gcloudClient is the OAuth client `gcloud auth application-default login` signs in with,
// overridable in tests. Both values are DEFAULT_CREDENTIALS_DEFAULT_CLIENT_ID and
// DEFAULT_CREDENTIALS_DEFAULT_CLIENT_SECRET of gcloud's open-source
// googlecloudsdk/api_lib/auth/util.py
// (https://raw.githubusercontent.com/google-cloud-sdk-unofficial/google-cloud-sdk/master/lib/googlecloudsdk/api_lib/auth/util.py),
// and every application_default_credentials.json it writes carries them. They belong to
// Google, not to Ichor, and are not a secret: Google documents an installed app's client
// secret as not confidential. Keep them here only.
var gcloudClient = func() (id, secret string) {
	return "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com", "d-FL95Q19q7MQmFpd7hHD0Ty"
}

// gcloudSignInMethod is the browser sign-in with gcloud's client: loopback redirect, PKCE,
// access_type=offline and prompt=consent (google), scopes openid email cloud-platform.
func gcloudSignInMethod() *oidcMethod {
	id, secret := gcloudClient()

	return &oidcMethod{
		method: authGKE, issuer: gcpIssuer(), clientID: id, clientSecret: secret,
		scopes: []string{"email", gcpScope}, useAccessToken: true,
		listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1", tls: gcpOAuthTLS(), google: true,
	}
}

// gcloudSignInState is the state of the option entered (s): the marker, and the session a
// previous sign-in stored when it parses as gcloud user credentials.
func gcloudSignInState(s map[string]string) kubeAuthState {
	if creds, err := parseUserCredentials(s[gcpFieldUserCredentials]); err == nil && creds.Type == gcpTypeAuthorizedUser {
		return kubeAuthState{Secrets: pick(s, gcpFieldGcloudSignIn, gcpFieldUserCredentials)}
	}

	return kubeAuthState{Secrets: pick(s, gcpFieldGcloudSignIn)}
}

// errGcloudSignIn is the option before its browser sign-in: the app opens the browser on it.
var errGcloudSignIn = signInRequired(authGKE, "sign in with your Google account in the browser")

// signInGcloud is gkeMethod.signIn for the option: the browser sign-in, then its refresh
// token stored as gcloud user credentials and its access token kept in the session.
func signInGcloud(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	start := state.withSession(map[string]string{"refresh": "", "bearer": "", "bearerExpiry": ""})

	signed, err := gcloudSignInMethod().signIn(ctx, start, prompt, callbacks)
	if err != nil {
		return state, err
	}

	refresh := signed.session("refresh")
	if refresh == "" {
		return state, errors.New("google gave no refresh token: sign in again")
	}

	id, secret := gcloudClient()

	raw, err := json.Marshal(gcpUserCredentials{Type: gcpTypeAuthorizedUser, ClientID: id, ClientSecret: secret, RefreshToken: refresh})
	if err != nil {
		return state, err
	}

	out := signed.withSession(map[string]string{"refresh": ""})
	out.Secrets = maps.Clone(state.Secrets)

	if out.Secrets == nil {
		out.Secrets = map[string]string{}
	}

	out.Secrets[gcpFieldGcloudSignIn] = "1"
	out.Secrets[gcpFieldUserCredentials] = string(raw)

	return out, nil
}
