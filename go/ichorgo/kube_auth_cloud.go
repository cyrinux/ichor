package ichorgo

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The cloud sign-ins that need no browser: GKE with a service account key, DigitalOcean with
// an API token, Rancher with an API key. Each replaces the CLI plugin the kubeconfig names.

const (
	gcpFieldServiceAccount = "gcpServiceAccountJson"
	doFieldToken           = "doApiToken"
	rancherFieldKey        = "rancherApiKey"
	gcpScope               = "https://www.googleapis.com/auth/cloud-platform"
)

var (
	doAPIEndpoint  = "https://api.digitalocean.com"
	cloudHTTP      = func() *http.Client { return newHTTPClient(httpClientOpts{timeout: oidcHTTPTimeout}) }
	gcpTokenTarget = func(serviceAccountTokenURI string) string { return serviceAccountTokenURI }
)

func init() {
	extraSignInMethods[authGKE] = func(*kubeStoreUser, *kubeStoreCluster) (signInMethod, error) { return gkeMethod{}, nil }
	extraSignInMethods[authDigitalOcean] = func(user *kubeStoreUser, _ *kubeStoreCluster) (signInMethod, error) {
		return newDOMethod(user)
	}
	extraSignInMethods[authRancher] = func(*kubeStoreUser, *kubeStoreCluster) (signInMethod, error) { return rancherMethod{}, nil }
}

// cloudJSON makes a request and decodes its JSON answer; a status out of 2xx is an error
// carrying the answer's message.
func cloudJSON(req *http.Request, out any) error {
	resp, err := cloudHTTP().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	data, err := io.ReadAll(io.LimitReader(resp.Body, awsMaxBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Message     string `json:"message"`
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}

		_ = json.Unmarshal(data, &e) //nolint:errcheck

		return &cloudHTTPError{status: resp.StatusCode, message: cmpOr(e.Description, e.Message, e.Error, http.StatusText(resp.StatusCode))}
	}

	return json.Unmarshal(data, out)
}

type cloudHTTPError struct {
	status  int
	message string
}

func (e *cloudHTTPError) Error() string { return fmt.Sprintf("%d %s", e.status, e.message) }

func unauthorized(err error) bool {
	var e *cloudHTTPError

	return errors.As(err, &e) && (e.status == http.StatusUnauthorized || e.status == http.StatusForbidden)
}

// gkeMethod signs in like gke-gcloud-auth-plugin with a service account key: a signed JWT
// exchanged for an access token (OAuth 2.0 JWT bearer grant).
type gkeMethod struct{}

func (gkeMethod) name() string { return authGKE }

func (gkeMethod) fieldSets() [][]string { return [][]string{{gcpFieldServiceAccount}} }

type gcpServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	KeyID       string `json:"private_key_id"`
	TokenURI    string `json:"token_uri"`
}

func parseServiceAccount(raw string) (gcpServiceAccount, *rsa.PrivateKey, error) {
	var sa gcpServiceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return sa, nil, errors.New("the service account key is not JSON")
	}

	if sa.Type != "service_account" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return sa, nil, errors.New("not a service account key (type service_account with a private key)")
	}

	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return sa, nil, errors.New("the service account private key is not PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return sa, nil, fmt.Errorf("service account private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return sa, nil, errors.New("the service account private key is not RSA")
	}

	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	return sa, rsaKey, nil
}

func (gkeMethod) fromSecrets(s map[string]string) (kubeAuthState, error) {
	sa, _, err := parseServiceAccount(s[gcpFieldServiceAccount])
	if err != nil {
		return kubeAuthState{}, err
	}

	return kubeAuthState{Secrets: pick(s, gcpFieldServiceAccount), User: sa.ClientEmail}, nil
}

func (gkeMethod) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	raw := state.secret(gcpFieldServiceAccount)
	if raw == "" {
		return "", time.Time{}, state, signInRequired(authGKE, "")
	}

	sa, key, err := parseServiceAccount(raw)
	if err != nil {
		return "", time.Time{}, state, err
	}

	now := time.Now()
	assertion, err := signJWT(key, sa.KeyID, map[string]any{
		"iss": sa.ClientEmail, "scope": gcpScope, "aud": sa.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", time.Time{}, state, err
	}

	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gcpTokenTarget(sa.TokenURI), strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, state, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var t oidcTokens
	if err := cloudJSON(req, &t); err != nil {
		if unauthorized(err) {
			return "", time.Time{}, state, signInRequired(authGKE, err.Error())
		}

		return "", time.Time{}, state, fmt.Errorf("Google token: %w", err)
	}

	state.User = sa.ClientEmail

	return t.AccessToken, now.Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600)) * time.Second), state, nil
}

// signJWT signs claims with RS256.
func signJWT(key *rsa.PrivateKey, keyID string, claims map[string]any) (string, error) {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if keyID != "" {
		header["kid"] = keyID
	}

	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}

	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signing := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
	digest := sha256.Sum256([]byte(signing))

	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// doMethod signs in like `doctl kubernetes cluster kubeconfig exec-credential`: short-lived
// cluster credentials from the DigitalOcean API, with the user's API token.
type doMethod struct{ clusterID string }

func newDOMethod(user *kubeStoreUser) (doMethod, error) {
	e := user.User.Exec
	if e == nil || len(e.Args) == 0 {
		return doMethod{}, errors.New("not a doctl kubeconfig")
	}

	id := e.Args[len(e.Args)-1]
	if strings.HasPrefix(id, "-") || !kubeNamePattern.MatchString(id) {
		return doMethod{}, errors.New("the kubeconfig names no DigitalOcean cluster ID")
	}

	return doMethod{clusterID: id}, nil
}

func (doMethod) name() string          { return authDigitalOcean }
func (doMethod) fieldSets() [][]string { return [][]string{{doFieldToken}} }

func (doMethod) fromSecrets(s map[string]string) (kubeAuthState, error) {
	if s[doFieldToken] == "" {
		return kubeAuthState{}, errors.New("enter a DigitalOcean API token")
	}

	return kubeAuthState{Secrets: pick(s, doFieldToken)}, nil
}

func (m doMethod) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	token := state.secret(doFieldToken)
	if token == "" {
		return "", time.Time{}, state, signInRequired(authDigitalOcean, "")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doAPIEndpoint+"/v2/kubernetes/clusters/"+url.PathEscape(m.clusterID)+"/credentials", nil)
	if err != nil {
		return "", time.Time{}, state, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	var creds struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := cloudJSON(req, &creds); err != nil {
		if unauthorized(err) {
			return "", time.Time{}, state, signInRequired(authDigitalOcean, err.Error())
		}

		return "", time.Time{}, state, fmt.Errorf("DigitalOcean credentials: %w", err)
	}

	if creds.Token == "" {
		return "", time.Time{}, state, errors.New("DigitalOcean gave no cluster token")
	}

	return creds.Token, creds.ExpiresAt, state, nil
}

// rancherMethod uses a Rancher API key (created under "Account & API Keys") as the bearer,
// which is what the rancher CLI hands kubectl.
type rancherMethod struct{}

// rancherKeyCache is how long the app keeps the key before reading it again from the state.
const rancherKeyCache = time.Hour

func (rancherMethod) name() string          { return authRancher }
func (rancherMethod) fieldSets() [][]string { return [][]string{{rancherFieldKey}} }

func (rancherMethod) fromSecrets(s map[string]string) (kubeAuthState, error) {
	key := s[rancherFieldKey]
	if !strings.HasPrefix(key, "token-") || !strings.Contains(key, ":") {
		return kubeAuthState{}, errors.New("a Rancher API key looks like token-xxxxx:secret")
	}

	user, _, _ := strings.Cut(key, ":")

	return kubeAuthState{Secrets: pick(s, rancherFieldKey), User: user}, nil
}

func (rancherMethod) mint(_ context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	key := state.secret(rancherFieldKey)
	if key == "" {
		return "", time.Time{}, state, signInRequired(authRancher, "")
	}

	return key, time.Now().Add(rancherKeyCache), state, nil
}
