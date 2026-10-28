package upstream

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	tokenDefaultLife = 60 * time.Second
	tokenMaxLife     = 5 * time.Minute
)

// tokenCache keeps the registry tokens per host and repository until they expire.
type tokenCache struct {
	mu     sync.Mutex
	tokens map[string]token
}

type token struct {
	header  string
	expires time.Time
}

func newTokenCache() *tokenCache { return &tokenCache{tokens: map[string]token{}} }

// header is the cached Authorization header for host/path, nil when none is valid.
func (t *tokenCache) header(host, path string) *string {
	t.mu.Lock()
	defer t.mu.Unlock()

	tok, ok := t.tokens[host+"/"+path]
	if !ok || time.Now().After(tok.expires) {
		return nil
	}

	return &tok.header
}

func (t *tokenCache) put(host, path, header string, life time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.tokens) >= cacheEntries {
		for k, v := range t.tokens {
			if time.Now().After(v.expires) {
				delete(t.tokens, k)
			}
		}
	}

	t.tokens[host+"/"+path] = token{header: header, expires: time.Now().Add(life)}
}

var challengeParam = regexp.MustCompile(`(\w+)="([^"]*)"`)

type tokenAnswer struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// authorize answers a 401 challenge of host: a Bearer token (anonymous, or with basic), or
// basic itself on a Basic challenge. Without a way in it is the credentials reason.
func (c *Client) authorize(ctx context.Context, host, path, challenge string, basic *BasicAuth) (*string, error) {
	scheme, params, _ := strings.Cut(strings.TrimSpace(challenge), " ")

	switch strings.ToLower(scheme) {
	case "bearer":
	case "basic":
		if basic == nil {
			return nil, reasonError{ReasonCredentials}
		}

		h := basicHeader(basic)

		return &h, nil
	default:
		return nil, reasonError{ReasonCredentials}
	}

	p := map[string]string{}
	for _, m := range challengeParam.FindAllStringSubmatch(params, -1) {
		p[strings.ToLower(m[1])] = m[2]
	}

	realm, err := url.Parse(p["realm"])
	if err != nil || realm.Scheme != "https" || realm.Host == "" {
		return nil, fmt.Errorf("%s sent an unusable token realm", host)
	}

	q := realm.Query()
	if p["service"] != "" {
		q.Set("service", p["service"])
	}

	q.Set("scope", cmpOr(p["scope"], "repository:"+path+":pull"))
	realm.RawQuery = q.Encode()

	var authz *string

	if basic != nil {
		h := basicHeader(basic)
		authz = &h
	}

	resp, err := c.get(ctx, realm.String(), authz, "application/json")
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck

	if err := statusError(realm.Host, resp); err != nil {
		return nil, err
	}

	var ans tokenAnswer
	if err := decodeJSON(resp, 1<<20, &ans); err != nil {
		return nil, fmt.Errorf("read the token of %s: %w", host, err)
	}

	tok := cmpOr(ans.Token, ans.AccessToken)
	if tok == "" {
		return nil, fmt.Errorf("%s sent an empty token", host)
	}

	life := tokenDefaultLife
	if ans.ExpiresIn > 0 {
		life = min(time.Duration(ans.ExpiresIn)*time.Second, tokenMaxLife)
	}

	h := "Bearer " + tok
	c.tokens.put(host, path, h, life-life/10)

	return &h, nil
}

func basicHeader(b *BasicAuth) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(b.Username+":"+b.Password))
}

// cancelOnClose releases a request's timeout when its body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()

	return err
}
