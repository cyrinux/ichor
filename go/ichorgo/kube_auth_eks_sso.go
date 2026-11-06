package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// awsSSO is IAM Identity Center in one region, apart from what it signs in to: the device flow
// (`aws sso login`), the token renewal, the account and role list and the role credentials.
// The EKS sign-in keeps its tokens in a kubeAuthState, the EC2 create in the app's sealed
// credential JSON; both call these.
type awsSSO struct {
	region string
	http   *http.Client
}

// awsSSOClient is the public client registered for one sign-in.
type awsSSOClient struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	// ExpiresAt is when the registration ends (Unix seconds); a refresh fails after it.
	ExpiresAt int64  `json:"clientSecretExpiresAt"`
	Error     string `json:"error"`
}

// awsDevice is the device authorization: the code the user approves in a browser.
type awsDevice struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

// errAWSSSOUnauthorized is the portal refusing the access token (expired, revoked): a new
// token or a new sign-in is needed. message is AWS's, without secrets.
type errAWSSSOUnauthorized struct{ message string }

func (e *errAWSSSOUnauthorized) Error() string {
	return "IAM Identity Center refused the sign-in: " + cmpOr(e.message, "sign in again")
}

func (s awsSSO) oidc() string { return awsSSOOIDCEndpoint(s.region) }

// deviceStart registers a public client allowed refresh tokens and asks for a device code.
func (s awsSSO) deviceStart(ctx context.Context, startURL string) (awsSSOClient, awsDevice, error) {
	var client awsSSOClient

	err := s.postJSON(ctx, s.oidc()+"/client/register", map[string]any{
		"clientName": "ichor", "clientType": "public", "scopes": []string{"sso:account:access"},
		"grantTypes": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
	}, &client)
	if err != nil || client.ClientID == "" {
		return client, awsDevice{}, fmt.Errorf("IAM Identity Center refused the app: %s", cmpOr(client.Error, errText(err)))
	}

	var device awsDevice

	err = s.postJSON(ctx, s.oidc()+"/device_authorization", map[string]any{
		"clientId": client.ClientID, "clientSecret": client.ClientSecret, "startUrl": startURL,
	}, &device)
	if err != nil || device.DeviceCode == "" {
		return client, device, fmt.Errorf("IAM Identity Center gave no device code: %s", errText(err))
	}

	return client, device, nil
}

// deviceFinish polls CreateToken until the user approved the code, refused it or it expired.
func (s awsSSO) deviceFinish(ctx context.Context, client awsSSOClient, device awsDevice) (awsSSOToken, error) {
	var token awsSSOToken

	_, err := pollDeviceToken(ctx, oidcDeviceAnswer{ExpiresIn: device.ExpiresIn, Interval: device.Interval}, func(ctx context.Context) (oidcTokens, error) {
		token = awsSSOToken{}

		err := s.postJSON(ctx, s.oidc()+"/token", map[string]any{
			"clientId": client.ClientID, "clientSecret": client.ClientSecret,
			"grantType": "urn:ietf:params:oauth:grant-type:device_code", "deviceCode": device.DeviceCode,
		}, &token)

		return oidcTokens{Error: awsDeviceError(token.Error)}, err
	})

	return token, err
}

// refresh asks for a new access token with the refresh token. The answer carries AWS's error
// code (see awsSessionOver) and the HTTP status (0 when there was none).
func (s awsSSO) refresh(ctx context.Context, clientID, clientSecret, refreshToken string) (awsSSOToken, int, error) {
	var t awsSSOToken

	status, err := s.postJSONStatus(ctx, s.oidc()+"/token", map[string]any{
		"clientId": clientID, "clientSecret": clientSecret, "grantType": "refresh_token", "refreshToken": refreshToken,
	}, &t)

	return t, status, err
}

// roleCredentials are the temporary keys of one account and role, with when they expire.
func (s awsSSO) roleCredentials(ctx context.Context, accessToken, accountID, roleName string) (awsCredentials, time.Time, error) {
	q := url.Values{"account_id": {accountID}, "role_name": {roleName}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsSSOPortalEndpoint(s.region)+"/federation/credentials?"+q.Encode(), nil)
	if err != nil {
		return awsCredentials{}, time.Time{}, err
	}

	req.Header.Set("X-Amz-Sso_bearer_token", accessToken)

	var answer struct {
		RoleCredentials struct {
			AccessKeyID     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			SessionToken    string `json:"sessionToken"`
			Expiration      int64  `json:"expiration"` // milliseconds
		} `json:"roleCredentials"`
		Message string `json:"message"`
	}

	status, err := s.doJSON(req, &answer)
	if err != nil {
		return awsCredentials{}, time.Time{}, err
	}

	rc := answer.RoleCredentials
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return awsCredentials{}, time.Time{}, &errAWSSSOUnauthorized{message: answer.Message}
	}

	if rc.AccessKeyID == "" {
		return awsCredentials{}, time.Time{}, fmt.Errorf("IAM Identity Center gave no role credentials: %s", cmpOr(answer.Message, strconv.Itoa(status)))
	}

	return awsCredentials{rc.AccessKeyID, rc.SecretAccessKey, rc.SessionToken}, time.UnixMilli(rc.Expiration), nil
}

func (s awsSSO) postJSON(ctx context.Context, endpoint string, body any, out any) error {
	_, err := s.postJSONStatus(ctx, endpoint, body, out)

	return err
}

// postJSONStatus is postJSON with the HTTP status of the answer (0 when there was none).
func (s awsSSO) postJSONStatus(ctx context.Context, endpoint string, body any, out any) (int, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")

	return s.doJSON(req, out)
}

// doJSON decodes the answer whatever its status (AWS sends errors as JSON too).
func (s awsSSO) doJSON(req *http.Request, out any) (int, error) {
	client := s.http
	if client == nil {
		client = awsHTTPClient()
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, awsMaxBody))
	if err != nil {
		return resp.StatusCode, err
	}

	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("AWS answered %d", resp.StatusCode)
		}
	}

	return resp.StatusCode, nil
}

// asSignInRequired turns the portal's refusal into the kube sign-in's error.
func asSignInRequired(err error) error {
	var refused *errAWSSSOUnauthorized
	if errors.As(err, &refused) {
		return signInRequired(authEKS, refused.message)
	}

	return err
}
