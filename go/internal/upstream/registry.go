package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ociPages   = 5
	ociPageLen = 1000
	hubPages   = 3
	hubPageLen = 100
	bodyLimit  = 4 << 20
)

// lookupImage compares the tag of image reference s with the tags of its repository.
func (c *Client) lookupImage(ctx context.Context, s string, auth Auth) (Candidate, error) {
	r := parseRef(s)

	switch {
	case r.tag == "" && r.digest != "":
		return Candidate{Reason: ReasonDigest}, nil
	case r.path == "" || r.registry == "":
		return Candidate{}, fmt.Errorf("unreadable image reference %q", s)
	}

	cand := classify(r.tag, nil)
	if cand.Reason != "" || needsSigV4(r.registry) {
		cand.Reason = cmpOr(cand.Reason, ReasonCredentials)

		return cand, nil
	}

	got, err := c.cache.get(ctx, "image:"+r.registry+"/"+r.path, func() (fetched, error) {
		return c.imageTags(ctx, r, auth)
	})
	if err != nil {
		return cand, err
	}

	out := classify(r.tag, got.tags)
	out.Source = got.source

	return out, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

// imageTags lists the tags of r: the Docker Hub API for docker.io (its registry API only
// when the Hub API rate limits or refuses), the OCI tags list everywhere else.
func (c *Client) imageTags(ctx context.Context, r ref, auth Auth) (fetched, error) {
	if r.registry != dockerHub {
		return c.ociTags(ctx, r.registry, r.path, auth.basic(r.registry))
	}

	got, err := c.hubTags(ctx, r.path)
	if err == nil {
		return got, nil
	}

	if _, ok := err.(hubFallback); ok { //nolint:errorlint // returned unwrapped by hubTags
		return c.ociTags(ctx, c.dockerRegistry, r.path, auth.basic(dockerHub))
	}

	return fetched{}, err
}

// hubFallback: the Hub API cannot answer (429, or a private repository), try the registry.
type hubFallback struct{ status string }

func (e hubFallback) Error() string { return "Docker Hub answered " + e.status }

type hubPage struct {
	Next    string `json:"next"`
	Results []struct {
		Name string `json:"name"`
	} `json:"results"`
}

func (c *Client) hubTags(ctx context.Context, path string) (fetched, error) {
	base, err := url.Parse(c.hubURL)
	if err != nil {
		return fetched{}, err
	}

	source := c.hubURL + "/v2/repositories/" + path + "/tags"
	next := fmt.Sprintf("%s?page_size=%d&ordering=last_updated", source, hubPageLen)
	out := fetched{source: source}

	for page := 0; page < hubPages && next != ""; page++ {
		resp, err := c.get(ctx, next, nil, "application/json")
		if err != nil {
			return fetched{}, err
		}

		switch resp.StatusCode {
		case http.StatusOK:
		case http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusForbidden:
			resp.Body.Close() //nolint:errcheck,gosec

			return fetched{}, hubFallback{resp.Status}
		case http.StatusNotFound:
			resp.Body.Close() //nolint:errcheck,gosec

			return fetched{}, fmt.Errorf("%s is not on Docker Hub", path)
		default:
			resp.Body.Close() //nolint:errcheck,gosec

			return fetched{}, fmt.Errorf("the Docker Hub API answered %s", resp.Status)
		}

		var p hubPage

		err = decodeJSON(resp, bodyLimit, &p)
		resp.Body.Close() //nolint:errcheck,gosec

		if err != nil {
			return fetched{}, fmt.Errorf("read Docker Hub tags: %w", err)
		}

		for _, t := range p.Results {
			out.tags = append(out.tags, t.Name)
		}

		next = sameHost(base, p.Next)
	}

	return out, nil
}

// sameHost is link when it points to base's host over https, else "" (a token or a
// credential is never sent elsewhere).
func sameHost(base *url.URL, link string) string {
	if link == "" {
		return ""
	}

	u, err := base.Parse(link)
	if err != nil || u.Host != base.Host || u.Scheme != "https" {
		return ""
	}

	return u.String()
}

type tagsList struct {
	Tags []string `json:"tags"`
}

// ociTags follows the tags list of host/path, at most 5 pages, with the token dance on a
// Bearer challenge.
func (c *Client) ociTags(ctx context.Context, host, path string, basic *BasicAuth) (fetched, error) {
	source := "https://" + host + "/v2/" + path + "/tags/list"
	base, err := url.Parse(source)
	if err != nil {
		return fetched{}, err
	}

	next := fmt.Sprintf("%s?n=%d", source, ociPageLen)
	out := fetched{source: source}
	authz, retried := c.tokens.header(host, path), false

	for page := 0; page < ociPages && next != ""; page++ {
		resp, err := c.get(ctx, next, authz, "application/json")
		if err != nil {
			return fetched{}, err
		}

		// One token dance per lookup: a cached token may have expired since.
		if resp.StatusCode == http.StatusUnauthorized && !retried {
			retried = true
			challenge := resp.Header.Get("WWW-Authenticate")
			resp.Body.Close() //nolint:errcheck,gosec

			if authz, err = c.authorize(ctx, host, path, challenge, basic); err != nil {
				return fetched{}, err
			}

			if resp, err = c.get(ctx, next, authz, "application/json"); err != nil {
				return fetched{}, err
			}
		}

		if err := statusError(host, resp); err != nil {
			resp.Body.Close() //nolint:errcheck,gosec

			return fetched{}, err
		}

		var list tagsList

		err = decodeJSON(resp, bodyLimit, &list)
		link := resp.Header.Get("Link")
		resp.Body.Close() //nolint:errcheck,gosec

		if err != nil {
			return fetched{}, fmt.Errorf("read the tags of %s/%s: %w", host, path, err)
		}

		out.tags = append(out.tags, list.Tags...)
		next = sameHost(base, nextLink(link))
	}

	return out, nil
}

var linkPattern = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func nextLink(header string) string {
	if m := linkPattern.FindStringSubmatch(header); m != nil {
		return m[1]
	}

	return ""
}

// statusError turns a non-200 answer into an error: rate limits and refused credentials
// are reasons, the rest failures.
func statusError(host string, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		return rateLimited(host, retryAfter(resp.Header))
	case http.StatusForbidden:
		if hasRateLimitHeaders(resp.Header) {
			return rateLimited(host, retryAfter(resp.Header))
		}

		return reasonError{ReasonCredentials}
	case http.StatusUnauthorized:
		return reasonError{ReasonCredentials}
	case http.StatusNotFound:
		return fmt.Errorf("%s has no such repository", host)
	default:
		return fmt.Errorf("%s answered %s", host, resp.Status)
	}
}

func hasRateLimitHeaders(h http.Header) bool {
	for k := range h {
		k = strings.ToLower(k)
		if strings.HasPrefix(k, "ratelimit-") || strings.HasPrefix(k, "x-ratelimit-") {
			return true
		}
	}

	return false
}

// retryAfter reads Retry-After (seconds or a date), then RateLimit-Reset / X-RateLimit-Reset
// (seconds, or a Unix time); 60 s when none says.
func retryAfter(h http.Header) int {
	if v := h.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}

		if t, err := http.ParseTime(v); err == nil {
			return max(0, int(time.Until(t).Seconds()))
		}
	}

	for _, k := range []string{"RateLimit-Reset", "X-RateLimit-Reset"} {
		n, err := strconv.ParseInt(h.Get(k), 10, 64)
		if err != nil || n < 0 {
			continue
		}

		if n > 1e9 { // a Unix time
			return max(0, int(time.Until(time.Unix(n, 0)).Seconds()))
		}

		return int(n)
	}

	return 60
}

// get sends a GET with the given Authorization header (nil for none).
func (c *Client) get(ctx context.Context, target string, authz *string, accept string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		cancel()

		return nil, err
	}

	req.Header.Set("Accept", accept)

	if authz != nil {
		req.Header.Set("Authorization", *authz)
	}

	resp, err := c.do(req)
	if err != nil {
		cancel()

		return nil, fmt.Errorf("query %s: %w", req.URL.Host, err)
	}

	resp.Body = cancelOnClose{resp.Body, cancel}

	return resp, nil
}
