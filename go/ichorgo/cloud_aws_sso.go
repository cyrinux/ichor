package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// S8 step 14: the EC2 create signed in with IAM Identity Center instead of access keys. The
// device flow, the account and role list and the role credentials are the EKS sign-in's
// (awsSSO); what it keeps lives in the credential JSON the app seals, never in Go. Every call
// that renews something answers with the updated JSON in "credential", which the app seals
// again. Nothing long-lived is made: the role credentials last the permission set's session (1
// to 12 h) and are fetched again with the access token, renewed with the refresh token while
// the Identity Center session lives.

const (
	// awsRoleMinLeft is what role credentials must have left to be used; a run is refused below.
	awsRoleMinLeft = 5 * time.Minute
	// awsRenewMinLeft is what AWSRenew makes sure of before a run (an AMI run takes ~10 min).
	awsRenewMinLeft  = 30 * time.Minute
	awsSignInTimeout = 60 * time.Second
)

var (
	errAWSSignInAgain = errors.New("sign in to IAM Identity Center again")
	errAWSRenewFirst  = errors.New("the IAM Identity Center role credentials have run out: open the AWS card again to renew them")
	errAWSNoRole      = errors.New("pick an account and a role of IAM Identity Center")
)

// awsCredentialFile is the credential JSON: access keys, or an Identity Center sign-in.
type awsCredentialFile struct {
	AccessKeyID     string `json:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty"`
	SessionToken    string `json:"sessionToken,omitempty"`
	// Region is the EC2 region the calls go to.
	Region string `json:"region"`

	SSOStartURL string `json:"ssoStartUrl,omitempty"`
	SSORegion   string `json:"ssoRegion,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	AccountName string `json:"accountName,omitempty"`
	RoleName    string `json:"roleName,omitempty"`

	SSOToken        *awsStoredSSOToken `json:"ssoToken,omitempty"`
	SSODevice       *awsStoredDevice   `json:"ssoDevice,omitempty"`
	SSORoles        []awsSSORole       `json:"ssoRoles,omitempty"`
	RoleCredentials *awsStoredRole     `json:"roleCredentials,omitempty"`
}

// awsStoredSSOToken is the Identity Center access token, its refresh token and the client
// registration that may renew it. Times are Unix seconds.
type awsStoredSSOToken struct {
	AccessToken     string `json:"accessToken,omitempty"`
	RefreshToken    string `json:"refreshToken,omitempty"`
	ExpiresAt       int64  `json:"expiresAt,omitempty"`
	ClientID        string `json:"clientId"`
	ClientSecret    string `json:"clientSecret"`
	ClientExpiresAt int64  `json:"clientExpiresAt,omitempty"`
}

// awsStoredDevice is the device code between AWSSignInStart and AWSSignInFinish.
type awsStoredDevice struct {
	DeviceCode string `json:"deviceCode"`
	ExpiresIn  int    `json:"expiresIn"`
	Interval   int    `json:"interval"`
}

type awsStoredRole struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken"`
	ExpiresAt       int64  `json:"expiresAt"`
}

func (f awsCredentialFile) usesSSO() bool {
	return strings.TrimSpace(f.AccessKeyID) == "" && strings.TrimSpace(f.SSOStartURL) != ""
}

func (f awsCredentialFile) sso() awsSSO { return awsSSO{region: f.SSORegion, http: awsHTTPClient()} }

// encode is the file as JSON; it holds plain strings and numbers, so it always encodes.
func (f awsCredentialFile) encode() string {
	data, _ := json.Marshal(f) //nolint:errcheck

	return string(data)
}

// parseAWSCredential reads credJSON; the parser's message is never passed on (it may quote a secret).
func parseAWSCredential(credJSON string) (awsCredentialFile, error) {
	var f awsCredentialFile

	if strings.TrimSpace(credJSON) != "" {
		if err := json.Unmarshal([]byte(credJSON), &f); err != nil {
			return f, errors.New("invalid AWS keys data") //nolint:nilerr // the parser's message may quote the secret
		}
	}

	return f, nil
}

// cachedRole is the stored role credentials when they have minLeft left.
func (f awsCredentialFile) cachedRole(minLeft time.Duration) (awsCredentials, bool) {
	r := f.RoleCredentials
	if r == nil || r.AccessKeyID == "" || time.Until(time.Unix(r.ExpiresAt, 0)) < minLeft {
		return awsCredentials{}, false
	}

	return awsCredentials{r.AccessKeyID, r.SecretAccessKey, r.SessionToken}, true
}

// awsResolve is what to sign with: the keys, or the Identity Center role's credentials. online
// lets it fetch new role credentials (renewing the access token when needed); the file comes back
// changed when it did, also when it failed because the session is over (cleared of its tokens).
func awsResolve(ctx context.Context, f awsCredentialFile, online bool, minLeft time.Duration) (awsCredentials, awsCredentialFile, bool, error) {
	if !f.usesSSO() {
		creds := awsCredentials{strings.TrimSpace(f.AccessKeyID), strings.TrimSpace(f.SecretAccessKey), strings.TrimSpace(f.SessionToken)}
		if creds.accessKeyID == "" || creds.secretAccessKey == "" {
			return awsCredentials{}, f, false, errAWSNoCredential
		}

		return creds, f, false, nil
	}

	if f.AccountID == "" || f.RoleName == "" {
		return awsCredentials{}, f, false, errAWSNoRole
	}

	if c, ok := f.cachedRole(minLeft); ok {
		return c, f, false, nil
	}

	if !online {
		return awsCredentials{}, f, false, errAWSRenewFirst
	}

	return f.fetchRole(ctx)
}

// fetchRole asks the portal for the role's credentials; a cached access token it refuses is
// renewed once with the refresh token before asking to sign in again.
func (f awsCredentialFile) fetchRole(ctx context.Context) (awsCredentials, awsCredentialFile, bool, error) {
	token, next, renewed, err := f.accessToken(ctx, false)
	if err != nil {
		return awsCredentials{}, next, renewed, err
	}

	creds, expiry, err := next.sso().roleCredentials(ctx, token, next.AccountID, next.RoleName)

	var refused *errAWSSSOUnauthorized
	if errors.As(err, &refused) && !renewed {
		if token, next, _, err = next.accessToken(ctx, true); err != nil {
			return awsCredentials{}, next, true, err
		}

		creds, expiry, err = next.sso().roleCredentials(ctx, token, next.AccountID, next.RoleName)
	}

	if errors.As(err, &refused) {
		return awsCredentials{}, next.signedOut(), true, errAWSSignInAgain
	}

	if err != nil {
		return awsCredentials{}, next, renewed, err
	}

	next.RoleCredentials = &awsStoredRole{creds.accessKeyID, creds.secretAccessKey, creds.sessionToken, expiry.Unix()}

	return creds, next, true, nil
}

// signedOut is the file without its tokens: the start URL, region, account and role stay, so
// that signing in again is one tap.
func (f awsCredentialFile) signedOut() awsCredentialFile {
	f.SSOToken, f.SSODevice, f.SSORoles, f.RoleCredentials = nil, nil, nil, nil

	return f
}

// accessToken is the Identity Center access token, renewed with the refresh token when it ends
// within a minute (or always, with force).
func (f awsCredentialFile) accessToken(ctx context.Context, force bool) (string, awsCredentialFile, bool, error) {
	t := f.SSOToken
	if t == nil {
		return "", f, false, errAWSSignInAgain
	}

	if !force && t.AccessToken != "" && time.Until(time.Unix(t.ExpiresAt, 0)) > time.Minute {
		return t.AccessToken, f, false, nil
	}

	if t.RefreshToken == "" || t.ClientID == "" {
		return "", f, false, errAWSSignInAgain
	}

	answer, status, err := f.sso().refresh(ctx, t.ClientID, t.ClientSecret, t.RefreshToken)

	// As the EKS sign-in: only AWS refusing the refresh token ends the session.
	switch {
	case err != nil:
		return "", f, false, fmt.Errorf("renew the IAM Identity Center token: %w", err)
	case awsSessionOver(answer.Error):
		return "", f.signedOut(), true, fmt.Errorf("%s %w", awsSessionEnded, errAWSSignInAgain)
	case status >= http.StatusInternalServerError:
		return "", f, false, fmt.Errorf("renew the IAM Identity Center token: AWS answered %d", status)
	case answer.AccessToken == "":
		return "", f, false, fmt.Errorf("renew the IAM Identity Center token: %s", cmpOr(answer.Description, answer.Error, "AWS answered "+strconv.Itoa(status)))
	}

	f.SSOToken = f.SSOToken.with(answer)

	return answer.AccessToken, f, true, nil
}

// with is the stored token after a CreateToken answer (a refresh answer may omit the refresh token).
func (t *awsStoredSSOToken) with(answer awsSSOToken) *awsStoredSSOToken {
	out := *t
	out.AccessToken = answer.AccessToken
	out.RefreshToken = cmpOr(answer.RefreshToken, t.RefreshToken)
	out.ExpiresAt = time.Now().Add(time.Duration(cmpOrInt(answer.ExpiresIn, 3600)) * time.Second).Unix()

	return &out
}

// newAWSClientOnline is newAWSClient that may renew the sign-in: updated is the credential JSON
// to seal again, "" when nothing changed.
func newAWSClientOnline(ctx context.Context, credJSON string) (client *awsClient, updated string, err error) {
	f, err := parseAWSCredential(credJSON)
	if err != nil {
		return nil, "", err
	}

	creds, next, changed, err := awsResolve(ctx, f, true, awsRoleMinLeft)
	if changed {
		updated = next.encode()
	}

	if err != nil {
		return nil, updated, err
	}

	client = &awsClient{creds: creds}

	return client, updated, client.setRegion(f.Region)
}

// AWSRenew makes sure an Identity Center credential has role credentials for a run (30 min or
// more); access keys pass as they are. JSON: {"credential"}: the JSON to seal again and to give
// the run, "" when the stored one is good as it is. Called by the apps right before a create or an
// AMI run, which use only what they are given.
func AWSRenew(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	f, err := parseAWSCredential(credJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsSignInTimeout)
	defer cancel()

	_, next, changed, err := awsResolve(ctx, f, true, awsRenewMinLeft)

	result := awsCredentialResult{}
	if changed {
		result.Credential = next.encode()
	}

	if err != nil {
		return "", err
	}

	return toJSON(result)
}

type awsCredentialResult struct {
	Credential string `json:"credential,omitempty"`
}

type awsSignInStartResult struct {
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete"`
	UserCode                string `json:"userCode"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
	Credential              string `json:"credential"`
}

// AWSSignInStart begins an IAM Identity Center sign-in, as `aws sso login`: credJSON names the
// start URL (https), the SSO region and the EC2 region (the SSO region's when ""). JSON:
// {"verificationUri","verificationUriComplete","userCode","expiresIn","interval","credential"}:
// the app shows the code, opens the link, and passes credential to AWSSignInFinish.
func AWSSignInStart(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	f, err := parseAWSCredential(credJSON)
	if err != nil {
		return "", err
	}

	f = awsCredentialFile{
		SSOStartURL: strings.TrimSpace(f.SSOStartURL), SSORegion: strings.TrimSpace(f.SSORegion),
		Region: cmpOr(strings.TrimSpace(f.Region), strings.TrimSpace(f.SSORegion)),
	}

	if u, err := url.Parse(f.SSOStartURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return "", errors.New("the IAM Identity Center start URL must be https (https://example.awsapps.com/start)")
	}

	if !awsRegionPattern.MatchString(f.SSORegion) {
		return "", fmt.Errorf("%q is not an AWS region (eu-west-1)", f.SSORegion)
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsSignInTimeout)
	defer cancel()

	client, device, err := f.sso().deviceStart(ctx, f.SSOStartURL)
	if err != nil {
		return "", err
	}

	f.SSOToken = &awsStoredSSOToken{ClientID: client.ClientID, ClientSecret: client.ClientSecret, ClientExpiresAt: client.ExpiresAt}
	f.SSODevice = &awsStoredDevice{DeviceCode: device.DeviceCode, ExpiresIn: device.ExpiresIn, Interval: device.Interval}

	return toJSON(awsSignInStartResult{
		VerificationURI: device.VerificationURI, VerificationURIComplete: cmpOr(device.VerificationURIComplete, device.VerificationURI),
		UserCode: device.UserCode, ExpiresIn: device.ExpiresIn, Interval: device.Interval, Credential: f.encode(),
	})
}

// awsSignInWait is the AWSSignInFinish waiting for the browser, which AWSSignInCancel ends.
var awsSignInWait struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

type awsSignInFinishResult struct {
	Credential string       `json:"credential"`
	Roles      []awsSSORole `json:"roles"`
}

// AWSSignInFinish waits until the code of AWSSignInStart is approved in the browser (or refused,
// or expired, or AWSSignInCancel), then lists the accounts and roles the user may assume. JSON:
// {"credential","roles":[{"accountId","accountName","roleName"}]}, sorted by account name then
// role; the app picks one (the only one by itself) and calls AWSSignInChoose.
func AWSSignInFinish(credJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	f, err := parseAWSCredential(credJSON)
	if err != nil {
		return "", err
	}

	if f.SSODevice == nil || f.SSOToken == nil || f.SSOToken.ClientID == "" {
		return "", errors.New("start the IAM Identity Center sign-in first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cmpOrInt(int64(f.SSODevice.ExpiresIn), 600))*time.Second+awsSignInTimeout)
	defer cancel()

	awsSignInWait.mu.Lock()
	if awsSignInWait.cancel != nil {
		awsSignInWait.cancel()
	}
	awsSignInWait.cancel = cancel
	awsSignInWait.mu.Unlock()

	client := awsSSOClient{ClientID: f.SSOToken.ClientID, ClientSecret: f.SSOToken.ClientSecret}
	device := awsDevice{DeviceCode: f.SSODevice.DeviceCode, ExpiresIn: f.SSODevice.ExpiresIn, Interval: f.SSODevice.Interval}

	token, err := f.sso().deviceFinish(ctx, client, device)
	if errors.Is(err, context.Canceled) {
		return "", errors.New("sign-in cancelled")
	}

	if err != nil {
		return "", err
	}

	f.SSODevice = nil
	f.SSOToken = f.SSOToken.with(token)

	roles, err := f.sso().roles(ctx, token.AccessToken)
	if err != nil {
		return "", err
	}

	if len(roles) == 0 {
		return "", errors.New("IAM Identity Center gives this user no account and role to assume")
	}

	f.SSORoles = roles

	return toJSON(awsSignInFinishResult{Credential: f.encode(), Roles: roles})
}

// AWSSignInCancel ends a waiting AWSSignInFinish (the sheet closed).
func AWSSignInCancel() {
	awsSignInWait.mu.Lock()
	defer awsSignInWait.mu.Unlock()

	if awsSignInWait.cancel != nil {
		awsSignInWait.cancel()
		awsSignInWait.cancel = nil
	}
}

type awsSignInChooseResult struct {
	Credential string `json:"credential"`
	Account    string `json:"account"`
	Arn        string `json:"arn"`
}

// AWSSignInChoose takes one account and role of AWSSignInFinish's list: their credentials, and
// whose they are. JSON: {"credential","account","arn"}; the app then tests and seals credential
// under account, as it does access keys.
func AWSSignInChoose(credJSON, accountID, roleName string) (out string, err error) {
	defer maskResult(&out, &err)

	f, err := parseAWSCredential(credJSON)
	if err != nil {
		return "", err
	}

	if !f.usesSSO() {
		return "", errors.New("sign in to IAM Identity Center first")
	}

	f.AccountID, f.RoleName, f.AccountName = strings.TrimSpace(accountID), strings.TrimSpace(roleName), ""

	for _, r := range f.SSORoles {
		if r.AccountID == f.AccountID && r.RoleName == f.RoleName {
			f.AccountName = r.AccountName
		}
	}

	f.SSORoles, f.RoleCredentials = nil, nil

	ctx, cancel := context.WithTimeout(context.Background(), awsSignInTimeout)
	defer cancel()

	creds, next, _, err := awsResolve(ctx, f, true, awsRoleMinLeft)
	if err != nil {
		return "", err
	}

	client := &awsClient{creds: creds}
	if err := client.setRegion(next.Region); err != nil {
		return "", err
	}

	id, err := client.sts(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(awsSignInChooseResult{Credential: next.encode(), Account: id.Account, Arn: id.Arn})
}
