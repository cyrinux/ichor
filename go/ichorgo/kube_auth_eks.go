package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// EKS sign-in, as `aws eks get-token` and aws-iam-authenticator do it: the bearer is a
// presigned STS GetCallerIdentity URL naming the cluster. The AWS credentials come from
// IAM Identity Center (a device code, renewed with its refresh token) or from access keys the
// user enters; with --role-arn the token is for that role (AssumeRole).

const (
	eksTokenPrefix = "k8s-aws-v1."
	// eksTokenLife: the API server accepts a token for 15 minutes; renewed before.
	eksTokenLife = 14 * time.Minute
	awsMaxBody   = 1 << 20
)

// AWS endpoints, overridable in tests.
var (
	awsSTSEndpoint = func(region string) string {
		return "https://sts." + region + ".amazonaws.com" + awsDomainSuffix(region)
	}
	awsSSOOIDCEndpoint = func(region string) string {
		return "https://oidc." + region + ".amazonaws.com" + awsDomainSuffix(region)
	}
	awsSSOPortalEndpoint = func(region string) string {
		return "https://portal.sso." + region + ".amazonaws.com" + awsDomainSuffix(region)
	}
	awsHTTPClient = func() *http.Client { return &http.Client{Timeout: oidcHTTPTimeout} }
)

// awsDomainSuffix completes the China partition's domain.
func awsDomainSuffix(region string) string {
	if strings.HasPrefix(region, "cn-") {
		return ".cn"
	}

	return ""
}

// The secrets an EKS sign-in asks for, keys or IAM Identity Center.
const (
	awsFieldAccessKey    = "awsAccessKeyId"
	awsFieldSecretKey    = "awsSecretAccessKey"
	awsFieldSessionToken = "awsSessionToken"
	awsFieldSSOStartURL  = "awsSsoStartUrl"
	awsFieldSSORegion    = "awsSsoRegion"
	awsFieldAccountID    = "awsAccountId"
	awsFieldRoleName     = "awsRoleName"
)

type eksMethod struct {
	cluster, region, roleARN string
	http                     *http.Client
}

func init() {
	extraSignInMethods[authEKS] = func(user *kubeStoreUser, _ *kubeStoreCluster) (signInMethod, error) {
		return newEKSMethod(user)
	}
}

func newEKSMethod(user *kubeStoreUser) (*eksMethod, error) {
	e := user.User.Exec
	if e == nil {
		return nil, errors.New("not an EKS kubeconfig")
	}

	env := map[string]string{}
	for _, v := range e.Env {
		env[v.Name] = v.Value
	}

	m := &eksMethod{
		cluster: flagValue(e.Args, "--cluster-name", "--cluster-id", "-i"),
		region:  cmp.Or(flagValue(e.Args, "--region"), env["AWS_REGION"], env["AWS_DEFAULT_REGION"], "us-east-1"),
		roleARN: cmp.Or(flagValue(e.Args, "--role-arn", "-r"), env["AWS_ROLE_ARN"]),
		http:    awsHTTPClient(),
	}

	if m.cluster == "" {
		return nil, errors.New("the kubeconfig names no EKS cluster (--cluster-name)")
	}

	return m, nil
}

func (m *eksMethod) name() string { return authEKS }

func (m *eksMethod) fieldSets() [][]string {
	return [][]string{
		{awsFieldSSOStartURL, awsFieldSSORegion, awsFieldAccountID, awsFieldRoleName},
		{awsFieldAccessKey, awsFieldSecretKey, awsFieldSessionToken},
	}
}

// rememberedFields are shown again when the Identity Center session has to be renewed; the
// access keys never come back out.
func (m *eksMethod) rememberedFields() []string {
	return []string{awsFieldSSOStartURL, awsFieldSSORegion, awsFieldAccountID, awsFieldRoleName}
}

func (m *eksMethod) fromSecrets(s map[string]string) (kubeAuthState, error) {
	switch {
	case s[awsFieldSSOStartURL] != "":
		for _, f := range []string{awsFieldSSORegion, awsFieldAccountID, awsFieldRoleName} {
			if s[f] == "" {
				return kubeAuthState{}, fmt.Errorf("IAM Identity Center needs %s", f)
			}
		}

		if u, err := url.Parse(s[awsFieldSSOStartURL]); err != nil || u.Scheme != "https" {
			return kubeAuthState{}, errors.New("the IAM Identity Center start URL must be https")
		}

		return kubeAuthState{Secrets: pick(s, awsFieldSSOStartURL, awsFieldSSORegion, awsFieldAccountID, awsFieldRoleName)}, nil
	case s[awsFieldAccessKey] != "" && s[awsFieldSecretKey] != "":
		return kubeAuthState{Secrets: pick(s, awsFieldAccessKey, awsFieldSecretKey, awsFieldSessionToken)}, nil
	}

	return kubeAuthState{}, errors.New("enter an access key and its secret, or an IAM Identity Center start URL")
}

// pick copies the non-empty keys of s.
func pick(s map[string]string, keys ...string) map[string]string {
	out := map[string]string{}

	for _, k := range keys {
		if s[k] != "" {
			out[k] = s[k]
		}
	}

	return out
}

func (m *eksMethod) usesSSO(state kubeAuthState) bool { return state.secret(awsFieldSSOStartURL) != "" }

func (m *eksMethod) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	creds, state, err := m.credentials(ctx, state)
	if err != nil {
		return "", time.Time{}, state, err
	}

	if m.roleARN != "" {
		if creds, state, err = m.assumedRole(ctx, creds, state); err != nil {
			return "", time.Time{}, state, err
		}
	}

	now := time.Now()
	u, _ := url.Parse(awsSTSEndpoint(m.region) + "/?Action=GetCallerIdentity&Version=2011-06-15")
	headers := http.Header{"X-K8s-Aws-Id": {m.cluster}}
	presigned := sigV4Presign(u, headers, creds, m.region, "sts", emptySHA256, 60*time.Second, now)

	return eksTokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(presigned)), now.Add(eksTokenLife), state, nil
}

// credentials are the AWS credentials to sign with: the keys entered, or role credentials
// from IAM Identity Center (cached in the session until they expire).
func (m *eksMethod) credentials(ctx context.Context, state kubeAuthState) (awsCredentials, kubeAuthState, error) {
	if !m.usesSSO(state) {
		if state.secret(awsFieldAccessKey) == "" {
			return awsCredentials{}, state, signInRequired(authEKS, "")
		}

		return awsCredentials{state.secret(awsFieldAccessKey), state.secret(awsFieldSecretKey), state.secret(awsFieldSessionToken)}, state, nil
	}

	if c, ok := sessionCredentials(state, "role"); ok {
		return c, state, nil
	}

	token, state, err := m.ssoAccessToken(ctx, state)
	if err != nil {
		return awsCredentials{}, state, err
	}

	return m.ssoRoleCredentials(ctx, token, state)
}

// sessionCredentials reads cached credentials stored under prefix, if still valid a while.
func sessionCredentials(state kubeAuthState, prefix string) (awsCredentials, bool) {
	exp, _ := strconv.ParseInt(state.session(prefix+"Expiry"), 10, 64)
	if state.session(prefix+"Key") == "" || time.Until(time.Unix(exp, 0)) < 5*time.Minute {
		return awsCredentials{}, false
	}

	return awsCredentials{state.session(prefix + "Key"), state.session(prefix + "Secret"), state.session(prefix + "Token")}, true
}

func withSessionCredentials(state kubeAuthState, prefix string, c awsCredentials, expiry time.Time) kubeAuthState {
	return state.withSession(map[string]string{
		prefix + "Key": c.accessKeyID, prefix + "Secret": c.secretAccessKey, prefix + "Token": c.sessionToken,
		prefix + "Expiry": strconv.FormatInt(expiry.Unix(), 10),
	})
}

// ssoAccessToken is the IAM Identity Center access token, renewed with the refresh token.
func (m *eksMethod) ssoAccessToken(ctx context.Context, state kubeAuthState) (string, kubeAuthState, error) {
	exp, _ := strconv.ParseInt(state.session("ssoAccessExpiry"), 10, 64)
	if token := state.session("ssoAccess"); token != "" && time.Until(time.Unix(exp, 0)) > time.Minute {
		return token, state, nil
	}

	if state.session("ssoRefresh") == "" || state.session("ssoClientId") == "" {
		return "", state, signInRequired(authEKS, "")
	}

	var t awsSSOToken

	err := m.postJSON(ctx, awsSSOOIDCEndpoint(state.secret(awsFieldSSORegion))+"/token", map[string]any{
		"clientId": state.session("ssoClientId"), "clientSecret": state.session("ssoClientSecret"),
		"grantType": "refresh_token", "refreshToken": state.session("ssoRefresh"),
	}, &t)
	if err != nil || t.AccessToken == "" {
		cleared := state.withSession(map[string]string{"ssoRefresh": "", "ssoAccess": ""})

		return "", cleared, signInRequired(authEKS, cmp.Or(t.Description, t.Error, errText(err)))
	}

	return t.AccessToken, m.withSSOToken(state, t), nil
}

type awsSSOToken struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (m *eksMethod) withSSOToken(state kubeAuthState, t awsSSOToken) kubeAuthState {
	return state.withSession(map[string]string{
		"ssoAccess":       t.AccessToken,
		"ssoAccessExpiry": strconv.FormatInt(time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600))*time.Second).Unix(), 10),
		"ssoRefresh":      cmp.Or(t.RefreshToken, state.session("ssoRefresh")),
	})
}

func (m *eksMethod) ssoRoleCredentials(ctx context.Context, token string, state kubeAuthState) (awsCredentials, kubeAuthState, error) {
	q := url.Values{"account_id": {state.secret(awsFieldAccountID)}, "role_name": {state.secret(awsFieldRoleName)}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, awsSSOPortalEndpoint(state.secret(awsFieldSSORegion))+"/federation/credentials?"+q.Encode(), nil)
	if err != nil {
		return awsCredentials{}, state, err
	}

	req.Header.Set("X-Amz-Sso_bearer_token", token)

	var answer struct {
		RoleCredentials struct {
			AccessKeyID     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			SessionToken    string `json:"sessionToken"`
			Expiration      int64  `json:"expiration"` // milliseconds
		} `json:"roleCredentials"`
		Message string `json:"message"`
	}

	status, err := m.doJSON(req, &answer)
	if err != nil {
		return awsCredentials{}, state, err
	}

	rc := answer.RoleCredentials
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return awsCredentials{}, state.withSession(map[string]string{"ssoAccess": ""}), signInRequired(authEKS, answer.Message)
	}

	if rc.AccessKeyID == "" {
		return awsCredentials{}, state, fmt.Errorf("IAM Identity Center gave no role credentials: %s", cmp.Or(answer.Message, strconv.Itoa(status)))
	}

	c := awsCredentials{rc.AccessKeyID, rc.SecretAccessKey, rc.SessionToken}

	return c, withSessionCredentials(state, "role", c, time.UnixMilli(rc.Expiration)), nil
}

// assumedRole switches to roleARN with STS AssumeRole, cached until it expires.
func (m *eksMethod) assumedRole(ctx context.Context, base awsCredentials, state kubeAuthState) (awsCredentials, kubeAuthState, error) {
	if c, ok := sessionCredentials(state, "assumed"); ok {
		return c, state, nil
	}

	form := url.Values{"Action": {"AssumeRole"}, "Version": {"2011-06-15"}, "RoleArn": {m.roleARN}, "RoleSessionName": {"ichor"}, "DurationSeconds": {"3600"}}
	body := []byte(form.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, awsSTSEndpoint(m.region)+"/", bytes.NewReader(body))
	if err != nil {
		return awsCredentials{}, state, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	sigV4Sign(req, sha256Hex(body), base, m.region, "sts", time.Now())

	resp, err := m.http.Do(req)
	if err != nil {
		return awsCredentials{}, state, err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, awsMaxBody))
	if err != nil {
		return awsCredentials{}, state, err
	}

	var answer struct {
		Credentials struct {
			AccessKeyID     string    `xml:"AccessKeyId"`
			SecretAccessKey string    `xml:"SecretAccessKey"`
			SessionToken    string    `xml:"SessionToken"`
			Expiration      time.Time `xml:"Expiration"`
		} `xml:"AssumeRoleResult>Credentials"`
		Error struct {
			Message string `xml:"Message"`
		} `xml:"Error"`
	}

	if err := xml.Unmarshal(data, &answer); err != nil || answer.Credentials.AccessKeyID == "" {
		return awsCredentials{}, state, fmt.Errorf("AssumeRole %s: %s", m.roleARN, cmp.Or(answer.Error.Message, strconv.Itoa(resp.StatusCode)))
	}

	c := answer.Credentials
	creds := awsCredentials{c.AccessKeyID, c.SecretAccessKey, c.SessionToken}

	return creds, withSessionCredentials(state, "assumed", creds, c.Expiration), nil
}

func (m *eksMethod) postJSON(ctx context.Context, endpoint string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	_, err = m.doJSON(req, out)

	return err
}

// doJSON decodes the answer whatever its status (AWS sends errors as JSON too).
func (m *eksMethod) doJSON(req *http.Request, out any) (int, error) {
	resp, err := m.http.Do(req)
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

// signIn runs the IAM Identity Center device flow (like `aws sso login`): register a public
// client allowed refresh tokens, show the code, wait for approval.
func (m *eksMethod) signIn(ctx context.Context, state kubeAuthState, prompt func(signInPrompt), _ <-chan string) (kubeAuthState, error) {
	if !m.usesSSO(state) {
		return state, errors.New("enter AWS access keys or an IAM Identity Center start URL first")
	}

	endpoint := awsSSOOIDCEndpoint(state.secret(awsFieldSSORegion))

	var client struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Error        string `json:"error"`
	}

	err := m.postJSON(ctx, endpoint+"/client/register", map[string]any{
		"clientName": "ichor", "clientType": "public", "scopes": []string{"sso:account:access"},
		"grantTypes": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
	}, &client)
	if err != nil || client.ClientID == "" {
		return state, fmt.Errorf("IAM Identity Center refused the app: %s", cmp.Or(client.Error, errText(err)))
	}

	var device struct {
		DeviceCode              string `json:"deviceCode"`
		UserCode                string `json:"userCode"`
		VerificationURI         string `json:"verificationUri"`
		VerificationURIComplete string `json:"verificationUriComplete"`
		ExpiresIn               int    `json:"expiresIn"`
		Interval                int    `json:"interval"`
	}

	err = m.postJSON(ctx, endpoint+"/device_authorization", map[string]any{
		"clientId": client.ClientID, "clientSecret": client.ClientSecret, "startUrl": state.secret(awsFieldSSOStartURL),
	}, &device)
	if err != nil || device.DeviceCode == "" {
		return state, fmt.Errorf("IAM Identity Center gave no device code: %s", errText(err))
	}

	prompt(signInPrompt{Kind: "device", URL: cmp.Or(device.VerificationURIComplete, device.VerificationURI), UserCode: device.UserCode, VerificationURL: device.VerificationURI, ExpiresIn: device.ExpiresIn})

	var token awsSSOToken

	_, err = pollDeviceToken(ctx, oidcDeviceAnswer{ExpiresIn: device.ExpiresIn, Interval: device.Interval}, func(ctx context.Context) (oidcTokens, error) {
		token = awsSSOToken{}

		err := m.postJSON(ctx, endpoint+"/token", map[string]any{
			"clientId": client.ClientID, "clientSecret": client.ClientSecret,
			"grantType": "urn:ietf:params:oauth:grant-type:device_code", "deviceCode": device.DeviceCode,
		}, &token)

		return oidcTokens{Error: awsDeviceError(token.Error)}, err
	})
	if err != nil {
		return state, err
	}

	signed := m.withSSOToken(state.withSession(map[string]string{"ssoClientId": client.ClientID, "ssoClientSecret": client.ClientSecret}), token)
	signed.User = state.secret(awsFieldRoleName) + " @ " + state.secret(awsFieldAccountID)

	return signed, nil
}

// awsDeviceError maps AWS's error names to RFC 8628's.
func awsDeviceError(e string) string {
	switch e {
	case "AuthorizationPendingException":
		return "authorization_pending"
	case "SlowDownException":
		return "slow_down"
	}

	return e
}
