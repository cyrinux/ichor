package ichorgo

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
)

// GKE with the organisation's own OAuth client: a Desktop app client (consent screen
// Internal, so no Google verification) whose ID and secret the user enters once; the sign-in
// then runs in the browser (PKCE, loopback redirect, as kubelogin does) and the app keeps the
// refresh token. The client fields are Secrets (backed up), the tokens the Session.

const (
	gcpFieldOAuthClientID     = "gcpOAuthClientId"
	gcpFieldOAuthClientSecret = "gcpOAuthClientSecret"
	gcpClientIDSuffix         = ".apps.googleusercontent.com"
)

var (
	// gcpIssuer is Google's OpenID issuer, overridable in tests.
	gcpIssuer = func() string { return "https://accounts.google.com" }
	// gcpOAuthTLS is the TLS setup for Google's sign-in, overridable in tests.
	gcpOAuthTLS = func() *tls.Config { return baseTLS(nil, false) }
)

// googleOAuthMethod is the browser sign-in with the OAuth client state holds.
func googleOAuthMethod(state kubeAuthState) *oidcMethod {
	return &oidcMethod{
		method: authGKE, issuer: gcpIssuer(),
		clientID: state.secret(gcpFieldOAuthClientID), clientSecret: state.secret(gcpFieldOAuthClientSecret),
		scopes: []string{"email", gcpScope}, useAccessToken: true,
		listen: []string{"127.0.0.1:0"}, redirectHost: "127.0.0.1", tls: gcpOAuthTLS(), google: true,
	}
}

// oauthClientState is the state of an OAuth client entered (s); signed in once the browser
// sign-in ran.
func oauthClientState(s map[string]string) (kubeAuthState, error) {
	id, secret := s[gcpFieldOAuthClientID], s[gcpFieldOAuthClientSecret]

	switch {
	case !strings.HasSuffix(id, gcpClientIDSuffix) || len(id) == len(gcpClientIDSuffix):
		return kubeAuthState{}, errors.New("not a Google OAuth client ID (…" + gcpClientIDSuffix + ")")
	case secret == "":
		return kubeAuthState{}, errors.New("enter the OAuth client secret")
	}

	return kubeAuthState{Secrets: pick(s, gcpFieldOAuthClientID, gcpFieldOAuthClientSecret)}, nil
}

// rememberedFields: the OAuth client is shown again when the session has to be renewed (a
// Desktop client's secret is not confidential, Google says), so signing in again is one tap.
func (gkeMethod) rememberedFields() []string {
	return []string{gcpFieldOAuthClientID, gcpFieldOAuthClientSecret}
}

// signIn runs the browser sign-in of the OAuth client entered; the other GKE credentials
// need none.
func (gkeMethod) signIn(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), callbacks <-chan string) (kubeAuthState, error) {
	if state.secret(gcpFieldOAuthClientID) == "" {
		return state, errors.New("enter the OAuth client ID and secret first")
	}

	return googleOAuthMethod(state).signIn(ctx, state, prompt, callbacks)
}
