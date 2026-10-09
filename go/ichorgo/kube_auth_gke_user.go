package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GKE with a Google account: the application_default_credentials.json that
// `gcloud auth application-default login` writes (type authorized_user) carries gcloud's
// OAuth client and the user's refresh token; a refresh token grant gives the cloud-platform
// access token gke-gcloud-auth-plugin would. It is the user's gcloud session brought to the
// phone, not a sign-in of Ichor's own.

const (
	gcpTokenURL = "https://oauth2.googleapis.com/token"
	// gcpRenewHint is how to get a working credential again once Google refuses it.
	gcpRenewHint = "run `gcloud auth application-default login` again and import the new credentials file"
)

// gcpUserInfoTarget is where the account's email is read when the token answer carries no
// ID token (overridden in tests).
var gcpUserInfoTarget = func() string { return "https://openidconnect.googleapis.com/v1/userinfo" }

// gcpUserCredentials is application_default_credentials.json for a Google account.
type gcpUserCredentials struct {
	Type           string `json:"type"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	RefreshToken   string `json:"refresh_token"`
	QuotaProjectID string `json:"quota_project_id,omitempty"`
}

func parseUserCredentials(raw string) (gcpUserCredentials, error) {
	var c gcpUserCredentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, errors.New("the gcloud credentials file is not JSON")
	}

	switch {
	case c.Type == "external_account":
		return c, errors.New("a workforce identity login config is not supported yet (see Workforce Identity Federation)")
	case c.Type == "service_account":
		return c, errors.New("this is a service account key: choose Service account key")
	case c.Type != "authorized_user" || c.ClientID == "" || c.ClientSecret == "" || c.RefreshToken == "":
		return c, errors.New("not a gcloud user credential (type authorized_user with a refresh token)")
	}

	return c, nil
}

// gcpCredentialType is the "type" of a Google credentials JSON, "" when it has none.
func gcpCredentialType(raw string) string {
	var t struct {
		Type string `json:"type"`
	}

	_ = json.Unmarshal([]byte(raw), &t) //nolint:errcheck // not JSON: no type

	return t.Type
}

// userCredentialsState is the state of gcloud user credentials entered (s); the account is
// known once a token was minted.
func userCredentialsState(s map[string]string) (kubeAuthState, error) {
	if _, err := parseUserCredentials(s[gcpFieldUserCredentials]); err != nil {
		return kubeAuthState{}, err
	}

	return kubeAuthState{Secrets: pick(s, gcpFieldUserCredentials)}, nil
}

// oauthInvalidGrant tells a refresh token Google no longer takes: revoked, expired by the
// Workspace's Google Cloud session control, or a reauthentication asked (invalid_rapt).
func oauthInvalidGrant(err error) bool {
	var e *cloudHTTPError

	return errors.As(err, &e) && (e.code == "invalid_grant" || e.code == "invalid_rapt")
}

// mintForGoogleUser is gkeMethod.mint for gcloud user credentials: the access token kept in
// the session while it lasts, else a refresh token grant. A refresh token Google rotated
// replaces the stored one in Secrets, which backups keep.
func mintForGoogleUser(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	if bearer := state.session("bearer"); bearer != "" {
		exp, _ := strconv.ParseInt(state.session("bearerExpiry"), 10, 64)
		if time.Until(time.Unix(exp, 0)) > tokenRefreshMargin {
			return bearer, time.Unix(exp, 0), state, nil
		}
	}

	creds, err := parseUserCredentials(state.secret(gcpFieldUserCredentials))
	if err != nil {
		return "", time.Time{}, state, err
	}

	t, err := refreshGoogleToken(ctx, creds)
	if err != nil {
		if oauthInvalidGrant(err) || unauthorized(err) {
			return "", time.Time{}, state, signInRequired(authGKE, gcpRenewHint)
		}

		return "", time.Time{}, state, fmt.Errorf("get a Google token: %w", err)
	}

	expiry := time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600)) * time.Second)
	state = state.withSession(map[string]string{"bearer": t.AccessToken, "bearerExpiry": strconv.FormatInt(expiry.Unix(), 10)})

	if t.RefreshToken != "" && t.RefreshToken != creds.RefreshToken {
		if state, err = withRotatedRefreshToken(state, creds, t.RefreshToken); err != nil {
			return "", time.Time{}, state, err
		}
	}

	state.User = googleAccount(ctx, t, state.User)

	return t.AccessToken, expiry, state, nil
}

// refreshGoogleToken is the refresh token grant of creds.
func refreshGoogleToken(ctx context.Context, creds gcpUserCredentials) (oidcTokens, error) {
	form := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken},
		"client_id": {creds.ClientID}, "client_secret": {creds.ClientSecret},
	}

	var t oidcTokens

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gcpTokenTarget(gcpTokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if err := cloudJSON(req, &t); err != nil {
		return t, err
	}

	if t.AccessToken == "" {
		return t, errors.New("no access token in Google's answer")
	}

	return t, nil
}

// withRotatedRefreshToken is state with creds' refresh token replaced by refreshToken.
func withRotatedRefreshToken(state kubeAuthState, creds gcpUserCredentials, refreshToken string) (kubeAuthState, error) {
	creds.RefreshToken = refreshToken

	raw, err := json.Marshal(creds)
	if err != nil {
		return state, err
	}

	state.Secrets = maps.Clone(state.Secrets)
	state.Secrets[gcpFieldUserCredentials] = string(raw)

	return state, nil
}

// googleAccount is the email of the account t belongs to: from its ID token, else from the
// userinfo endpoint, else known (or a plain label).
func googleAccount(ctx context.Context, t oidcTokens, known string) string {
	if claims, err := readIDToken(t.IDToken); err == nil && claims.Email != "" {
		return claims.Email
	}

	var info struct {
		Email string `json:"email"`
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gcpUserInfoTarget(), nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+t.AccessToken)

		if cloudJSON(req, &info) == nil && info.Email != "" {
			return info.Email
		}
	}

	return cmpOr(known, "Google account")
}
