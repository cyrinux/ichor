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
// phone, not a sign-in of Ichor's own. With `--login-config` (Workforce Identity Federation)
// the file is an external_account_authorized_user: the same refresh, at Google STS
// (token_url) with the client in a Basic header.

const (
	gcpTokenURL = "https://oauth2.googleapis.com/token"
	// gcpRenewHint is how to get a working credential again once Google refuses it.
	gcpRenewHint = "run `gcloud auth application-default login` again and import the new credentials file"
	// gcpWorkforceRenewHint is the same for a workforce identity session.
	gcpWorkforceRenewHint = "run `gcloud auth application-default login --login-config=<your login config>` again and import the new credentials file"

	gcpTypeAuthorizedUser = "authorized_user"
	gcpTypeWorkforceUser  = "external_account_authorized_user"
)

// gcpUserInfoTarget is where the account's email is read when the token answer carries no
// ID token (overridden in tests).
var gcpUserInfoTarget = func() string { return "https://openidconnect.googleapis.com/v1/userinfo" }

// gcpUserCredentials is application_default_credentials.json for a Google account, or for a
// workforce identity session (Audience, TokenURL, TokenInfoURL).
type gcpUserCredentials struct {
	Type           string `json:"type"`
	ClientID       string `json:"client_id"`
	ClientSecret   string `json:"client_secret"`
	RefreshToken   string `json:"refresh_token"`
	QuotaProjectID string `json:"quota_project_id,omitempty"`
	Audience       string `json:"audience,omitempty"`
	TokenURL       string `json:"token_url,omitempty"`
	TokenInfoURL   string `json:"token_info_url,omitempty"`
}

// workforce tells a workforce identity session from a Google account's credentials.
func (c gcpUserCredentials) workforce() bool { return c.Type == gcpTypeWorkforceUser }

// renewHint is how to get working credentials again once Google refuses these.
func (c gcpUserCredentials) renewHint() string {
	if c.workforce() {
		return gcpWorkforceRenewHint
	}

	return gcpRenewHint
}

func parseUserCredentials(raw string) (gcpUserCredentials, error) {
	var c gcpUserCredentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, errors.New("the gcloud credentials file is not JSON")
	}

	complete := c.ClientID != "" && c.ClientSecret != "" && c.RefreshToken != ""

	switch c.Type {
	case "external_account":
		return c, errors.New("a workforce identity login config or external account file cannot sign in by itself: " +
			"run `gcloud auth application-default login --login-config=<file>` and import the credentials it writes")
	case "service_account":
		return c, errors.New("this is a service account key: choose Service account key")
	case gcpTypeWorkforceUser:
		if !complete || c.TokenURL == "" {
			return c, errors.New("not a workforce identity session (external_account_authorized_user with a refresh token and token_url)")
		}

		if u, err := url.Parse(c.TokenURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return c, errors.New("the workforce identity session's token_url is not https")
		}
	case gcpTypeAuthorizedUser:
		if !complete {
			return c, errors.New("not a gcloud user credential (type authorized_user with a refresh token)")
		}
	default:
		return c, errors.New("not a gcloud user credential (type authorized_user with a refresh token)")
	}

	return c, nil
}

// isGCPUserCredentials tells the credentials files gcloud writes for a user (a Google account
// or a workforce identity session) by their raw JSON.
func isGCPUserCredentials(raw string) bool {
	t := gcpCredentialType(raw)

	return t == gcpTypeAuthorizedUser || t == gcpTypeWorkforceUser
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
			return "", time.Time{}, state, signInRequired(authGKE, creds.renewHint())
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

	if creds.workforce() {
		state.User = workforceLabel(creds.Audience)
	} else {
		state.User = googleAccount(ctx, t, state.User)
	}

	return t.AccessToken, expiry, state, nil
}

// refreshGoogleToken is the refresh token grant of creds: at Google's token endpoint with the
// client in the form, or at STS (the session's token_url) with the client in a Basic header.
func refreshGoogleToken(ctx context.Context, creds gcpUserCredentials) (oidcTokens, error) {
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {creds.RefreshToken}}
	target := gcpTokenURL

	if creds.workforce() {
		target = creds.TokenURL
	} else {
		form.Set("client_id", creds.ClientID)
		form.Set("client_secret", creds.ClientSecret)
	}

	var t oidcTokens

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gcpTokenTarget(target), strings.NewReader(form.Encode()))
	if err != nil {
		return t, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if creds.workforce() {
		req.SetBasicAuth(creds.ClientID, creds.ClientSecret)
	}

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

// workforceLabel names a workforce identity session by its pool, from the audience
// (//iam.googleapis.com/locations/global/workforcePools/<pool>/providers/<provider>).
func workforceLabel(audience string) string {
	if _, rest, ok := strings.Cut(audience, "/workforcePools/"); ok {
		if pool, _, _ := strings.Cut(rest, "/"); pool != "" {
			return pool + " (workforce identity)"
		}
	}

	return "Workforce identity"
}
