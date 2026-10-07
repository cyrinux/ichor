package ichorgo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/protobuf/encoding/protowire"
)

// Kubernetes through Omni: Omni's kube proxy takes an ID token of its OIDC issuer (client
// "native", scope cluster:<name>). kubectl gets it with kubelogin in a browser; the app
// already holds a key Omni trusts, so it does what Omni's own login page does: it opens
// the authorization request and approves it with a signed call (OIDCService.Authenticate).
// Tokens last 12h, without refresh: a new one is minted when needed.

const (
	omniOIDCClient   = "native"
	omniOIDCRedirect = "urn:ietf:wg:oauth:2.0:oob"
	// omniKubeMethod is the kube sign-in method of an Omni cluster (an Omni sign-in).
	omniKubeMethod = "omni"
	// omniKubeSourcePrefix starts the token source keys of Omni clusters (see omniAuthChanged).
	omniKubeSourcePrefix = "omni-kube\x00"
	// omniNoKubeGroups: Omni's kube proxy refuses a token without groups, what an identity
	// with only Omni's Reader role and no access policy gets.
	omniNoKubeGroups = "omni gives this identity no Kubernetes access: an Omni admin can grant it with an access policy (or the Operator role)"
)

// omniKubeTokens mints Omni ID tokens for one cluster, signed in as cfgCtx's identity.
type omniKubeTokens struct {
	ctx     *clientconfig.Context
	issuer  string
	cluster string
}

func (m *omniKubeTokens) name() string { return omniKubeMethod }

func (m *omniKubeTokens) mint(ctx context.Context, state kubeAuthState) (string, time.Time, kubeAuthState, error) {
	signer, err := loadOmniSigner(omniAuthKey(m.ctx), m.ctx)
	if err != nil {
		return "", time.Time{}, state, err
	}

	token, expiry, err := omniOIDCToken(ctx, m.ctx, signer, m.issuer, m.cluster)
	if err != nil {
		return "", time.Time{}, state, err
	}

	state.User = signer.identity

	return token, expiry, state, nil
}

func omniHTTPClient(cfgCtx *clientconfig.Context) (*http.Client, error) {
	tlsConfig, err := omniTLS(cfgCtx)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Timeout:   oidcHTTPTimeout,
		Transport: &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true},
		// The authorization request answers with a redirect to Omni's login page: read it.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// omniOIDCToken is an ID token for cluster's Kubernetes proxy and its expiry.
func omniOIDCToken(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner, issuer, cluster string) (string, time.Time, error) {
	httpc, err := omniHTTPClient(cfgCtx)
	if err != nil {
		return "", time.Time{}, err
	}

	var d oidcDiscovery
	if err := omniGetJSON(ctx, httpc, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration", &d); err != nil || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return "", time.Time{}, fmt.Errorf("Omni OIDC discovery: %w", errors.Join(err, errors.New("no endpoints")))
	}

	verifier := randomToken() + randomToken()
	challenge := sha256.Sum256([]byte(verifier))

	q := url.Values{
		"client_id":             {omniOIDCClient},
		"response_type":         {"code"},
		"scope":                 {"openid cluster:" + cluster},
		"redirect_uri":          {omniOIDCRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
		"state":                 {randomToken()},
		"nonce":                 {randomToken()},
	}

	requestID, err := omniAuthRequest(ctx, httpc, d.AuthorizationEndpoint+"?"+q.Encode())
	if err != nil {
		return "", time.Time{}, err
	}

	code, err := omniApprove(ctx, cfgCtx, signer, requestID)
	if err != nil {
		return "", time.Time{}, err
	}

	return omniExchange(ctx, httpc, d.TokenEndpoint, code, verifier)
}

func omniGetJSON(ctx context.Context, httpc *http.Client, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", u, resp.StatusCode)
	}

	return json.NewDecoder(io.LimitReader(resp.Body, oidcMaxBody)).Decode(out)
}

// omniAuthRequest opens an authorization request: Omni redirects to its login page, whose
// last path segment is the request's ID.
func omniAuthRequest(ctx context.Context, httpc *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("Omni OIDC authorization: %w", err)
	}

	resp.Body.Close() //nolint:errcheck,gosec

	login, err := url.Parse(resp.Header.Get("Location"))
	if err == nil {
		// A refusal comes back to the redirect URI (opaque, urn:…?error=…) with its reason.
		if q, _ := url.ParseQuery(login.RawQuery); q.Get("error") != "" {
			return "", fmt.Errorf("Omni OIDC authorization: %s %s", q.Get("error"), q.Get("error_description"))
		}
	}

	if err != nil || resp.StatusCode/100 != 3 || login.Path == "" {
		return "", fmt.Errorf("Omni OIDC authorization answered %d", resp.StatusCode)
	}

	return path.Base(login.Path), nil
}

// omniApprove approves the authorization request as the signed-in identity; it returns the
// authorization code.
func omniApprove(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner, requestID string) (string, error) {
	cc, err := omniDial(cfgCtx, &omniSigning{method: omniMethodName(cfgCtx), signer: signer})
	if err != nil {
		return "", fmt.Errorf("connect to Omni: %w", err)
	}

	defer cc.Close() //nolint:errcheck

	var req []byte
	req = protowire.AppendTag(req, 1, protowire.BytesType)
	req = protowire.AppendString(req, requestID)

	resp, err := omniInvoke(ctx, cc, "/oidc.OIDCService/Authenticate", req)
	if err != nil {
		return "", omniAPIError(err)
	}

	if code, _ := protoField(resp, 2); len(code) > 0 {
		return string(code), nil
	}

	redirect, _ := protoField(resp, 1)
	if u, err := url.Parse(string(redirect)); err == nil && u.Query().Get("code") != "" {
		return u.Query().Get("code"), nil
	}

	return "", errors.New("omni approved the sign-in without a code")
}

func omniExchange(ctx context.Context, httpc *http.Client, endpoint, code, verifier string) (string, time.Time, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {omniOIDCRedirect},
		"client_id":     {omniOIDCClient},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpc.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("Omni OIDC token: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var t oidcTokens
	if err := json.NewDecoder(io.LimitReader(resp.Body, oidcMaxBody)).Decode(&t); err != nil {
		return "", time.Time{}, fmt.Errorf("Omni OIDC token: %w", err)
	}

	if t.IDToken == "" {
		return "", time.Time{}, fmt.Errorf("Omni OIDC token: %s %s", cmpOr(t.Error, resp.Status), t.Description)
	}

	expiry := time.Now().Add(time.Duration(cmpOrInt(t.ExpiresIn, 3600)) * time.Second)
	if claims, err := readIDToken(t.IDToken); err == nil && claims.Expires > 0 {
		expiry = time.Unix(claims.Expires, 0)
	}

	return t.IDToken, expiry, nil
}

// omniKubeClient opens the Kubernetes API of an Omni session's cluster: Omni's kubeconfig
// for the server (its kube proxy: an address set for the cluster does not apply), the app's
// own tokens for the user.
func omniKubeClient(ctx context.Context, s *session) (*kubeClient, error) {
	kubeYAML, issuer, err := omniClusterKubeconfig(ctx, s.context, s.signing.current())
	if err != nil {
		return nil, err
	}

	creds, err := parseKubeconfig(kubeYAML)
	if err != nil {
		return nil, err
	}

	tokens := kubeAuth.source(omniKubeSourcePrefix+s.authKey+"\x00"+s.context.Cluster, &omniKubeTokens{ctx: s.context, issuer: issuer, cluster: s.context.Cluster})

	if _, err := tokens.get(ctx); err != nil {
		return nil, err
	}

	creds.token, creds.tokens = "", tokens

	k, err := openKubeClientCreds(context.Background(), creds, nil, "")
	if err != nil {
		var apiErr *kubeAPIError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusUnauthorized {
			return nil, errors.New(omniNoKubeGroups)
		}

		return nil, err
	}

	k.unauthorized = omniNoKubeGroups

	return k, nil
}

// omniClusterKubeconfig is Omni's kubeconfig of cfgCtx's cluster with its user's exec
// replaced by a placeholder token (parseKubeconfig refuses plugins), and its OIDC issuer.
func omniClusterKubeconfig(ctx context.Context, cfgCtx *clientconfig.Context, signer omniSigner) (kubeYAML, issuer string, err error) {
	cc, err := omniDial(cfgCtx, &omniSigning{method: omniMethodName(cfgCtx), signer: signer})
	if err != nil {
		return "", "", fmt.Errorf("connect to Omni: %w", err)
	}

	defer cc.Close() //nolint:errcheck

	raw, err := omniKubeconfig(ctx, cc, cfgCtx.Cluster)
	if err != nil {
		return "", "", omniAPIError(err)
	}

	doc, err := loadKubeconfigDoc(string(raw))
	if err != nil {
		return "", "", fmt.Errorf("Omni's kubeconfig: %w", err)
	}

	single, err := doc.single(doc.currentName())
	if err != nil {
		return "", "", fmt.Errorf("Omni's kubeconfig: %w", err)
	}

	if exec := single.Users[0].User.Exec; exec != nil {
		issuer = flagValue(exec.Args, "--oidc-issuer-url")
	}

	if issuer == "" {
		issuer = "https://" + omniHost(cfgCtx) + "/oidc"
	}

	single.Users[0].User = kubeStoreUser{}.User
	single.Users[0].User.Token = "signed-in"

	kubeYAML, err = encodeKubeconfigDoc(single)

	return kubeYAML, issuer, err
}
