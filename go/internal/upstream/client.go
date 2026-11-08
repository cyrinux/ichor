package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

const (
	requestTimeout  = 10 * time.Second
	maxRegistries   = 4 // registries queried at once
	maxPerHost      = 2 // requests in flight per host
	defaultHubURL   = "https://hub.docker.com"
	defaultDockerV2 = "registry-1.docker.io"
	dockerHub       = "docker.io"
)

// Target is an image or a chart to look up.
type Target struct {
	Kind    string `json:"kind"`              // image | chart
	Ref     string `json:"ref,omitempty"`     // image: reference with its tag
	RepoURL string `json:"repoURL,omitempty"` // chart: https index or oci://
	Name    string `json:"name,omitempty"`    // chart name
	Version string `json:"version,omitempty"` // chart version
}

// Candidate is the newer versions of a target. Patch is the newest with the same
// major.minor, Minor the newest with the same major, Major the newest overall; each is empty
// when there is none or when it repeats a lower one. Prerelease tells the current version is
// a prerelease (prereleases were then counted). Reason says why nothing could be compared.
type Candidate struct {
	Current    string `json:"current"`
	Patch      string `json:"patch,omitempty"`
	Minor      string `json:"minor,omitempty"`
	Major      string `json:"major,omitempty"`
	Prerelease bool   `json:"prerelease,omitempty"`
	Shape      string `json:"shape,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Source     string `json:"source,omitempty"`
}

// Auth holds the credentials a lookup may use: a Docker config.json ("auths" keyed by
// registry) and a basic auth for https chart repositories.
type Auth struct {
	Auths map[string]AuthEntry `json:"auths,omitempty"`
	Helm  *BasicAuth           `json:"helm,omitempty"`
}

// AuthEntry is one registry of a Docker config.json.
type AuthEntry struct {
	Auth     string `json:"auth,omitempty"` // base64 user:password
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// BasicAuth is a user name and a password.
type BasicAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// basic is the credentials auth holds for host, nil when none.
func (a Auth) basic(host string) *BasicAuth {
	for key, e := range a.Auths {
		if normalizeHost(key) != host {
			continue
		}

		if e.Username != "" {
			return &BasicAuth{Username: e.Username, Password: e.Password}
		}

		raw, err := base64.StdEncoding.DecodeString(e.Auth)
		if user, pass, ok := strings.Cut(string(raw), ":"); err == nil && ok {
			return &BasicAuth{Username: user, Password: pass}
		}
	}

	return nil
}

// normalizeHost is the registry a Docker config key names ("https://index.docker.io/v1/" is
// docker.io), as image references name it.
func normalizeHost(key string) string {
	host := key
	if _, rest, ok := strings.Cut(host, "://"); ok {
		host = rest
	}

	host, _, _ = strings.Cut(host, "/")

	switch host = strings.ToLower(host); host {
	case "", "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com":
		return dockerHub
	}

	return host
}

// Options sets up a Client; the zero value queries the real registries.
type Options struct {
	HTTP           *http.Client // nil: httpx.New with a 10 s timeout
	HubURL         string       // Docker Hub API, https://hub.docker.com
	DockerRegistry string       // Docker Hub's registry API host, registry-1.docker.io
}

// Client looks up versions, with a shared cache and per-host request bounds.
type Client struct {
	http           *http.Client
	hubURL         string
	dockerRegistry string
	cache          *cache
	tokens         *tokenCache

	mu    sync.Mutex
	slots map[string]chan struct{}
}

// New is a Client set up by o.
func New(o Options) *Client {
	c := &Client{http: o.HTTP, hubURL: o.HubURL, dockerRegistry: o.DockerRegistry, cache: newCache(), tokens: newTokenCache(), slots: map[string]chan struct{}{}}
	if c.http == nil {
		c.http = httpx.New(httpx.Opts{FollowRedirects: true, Timeout: requestTimeout})
	}

	if c.hubURL == "" {
		c.hubURL = defaultHubURL
	}

	if c.dockerRegistry == "" {
		c.dockerRegistry = defaultDockerV2
	}

	return c
}

// reasonError is an answer that is not a failure of the lookup: the registry wants
// credentials, or rate limits. It becomes Candidate.Reason.
type reasonError struct{ reason string }

func (e reasonError) Error() string { return e.reason }

func rateLimited(host string, seconds int) error {
	return reasonError{fmt.Sprintf("rate limited by %s, retry after %d s", host, seconds)}
}

// Lookup says which newer versions of t exist. Credentials and rate limits are reported in
// Reason, not as an error.
func (c *Client) Lookup(ctx context.Context, t Target, auth Auth) (Candidate, error) {
	var (
		cand Candidate
		err  error
	)

	switch t.Kind {
	case "image":
		cand, err = c.lookupImage(ctx, t.Ref, auth)
	case "chart":
		cand, err = c.lookupChart(ctx, t, auth)
	default:
		return Candidate{}, fmt.Errorf("unknown target kind %q (image or chart)", t.Kind)
	}

	var re reasonError
	if errors.As(err, &re) {
		cand.Reason, err = re.reason, nil
	}

	return cand, err
}

// Result is one answer of LookupAll.
type Result struct {
	Candidate Candidate
	Err       error
}

// LookupAll looks up every target, at most 4 registries at once and 2 requests per
// registry, and answers in the input order.
func (c *Client) LookupAll(ctx context.Context, targets []Target, auth Auth) []Result {
	out := make([]Result, len(targets))
	groups := map[string][]int{}
	order := []string{}

	for i, t := range targets {
		h := targetHost(t)
		if _, ok := groups[h]; !ok {
			order = append(order, h)
		}

		groups[h] = append(groups[h], i)
	}

	sem := make(chan struct{}, maxRegistries)

	var wg sync.WaitGroup

	for _, h := range order {
		wg.Add(1)

		go func(idx []int) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			c.lookupGroup(ctx, targets, idx, auth, out)
		}(groups[h])
	}

	wg.Wait()

	return out
}

// lookupGroup runs the targets of one registry, two at a time; each writes its own slot.
func (c *Client) lookupGroup(ctx context.Context, targets []Target, idx []int, auth Auth, out []Result) {
	sem := make(chan struct{}, maxPerHost)

	var wg sync.WaitGroup

	for _, i := range idx {
		wg.Add(1)

		sem <- struct{}{}

		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			cand, err := c.Lookup(ctx, targets[i], auth)
			out[i] = Result{Candidate: cand, Err: err}
		}(i)
	}

	wg.Wait()
}

// targetHost groups targets by the host they query.
func targetHost(t Target) string {
	if t.Kind == "chart" {
		u := strings.TrimPrefix(strings.TrimPrefix(t.RepoURL, "oci://"), "https://")
		host, _, _ := strings.Cut(u, "/")

		return host
	}

	return parseRef(t.Ref).registry
}

// do sends req once a slot of its host is free (at most 2 in flight per host).
func (c *Client) do(req *http.Request) (*http.Response, error) {
	slot := c.slot(req.URL.Host)

	select {
	case slot <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}

	defer func() { <-slot }()

	return c.http.Do(req)
}

func (c *Client) slot(host string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.slots[host]
	if !ok {
		s = make(chan struct{}, maxPerHost)
		c.slots[host] = s
	}

	return s
}

// ref is a parsed image reference: docker.io/library/nginx:1.27 for "nginx:1.27" (the same
// rules as ichorgo's parseImageRef).
type ref struct {
	registry, path, tag, digest string
}

func parseRef(s string) ref {
	var r ref

	s, r.digest, _ = strings.Cut(strings.TrimSpace(s), "@")
	if slash, colon := strings.LastIndex(s, "/"), strings.LastIndex(s, ":"); colon > slash {
		s, r.tag = s[:colon], s[colon+1:]
	}

	if first, rest, ok := strings.Cut(s, "/"); ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		r.registry, r.path = normalizeHost(first), rest
	} else {
		r.registry, r.path = dockerHub, s
	}

	if r.registry == dockerHub && !strings.Contains(r.path, "/") {
		r.path = "library/" + r.path
	}

	return r
}

// needsSigV4 is a registry that only takes AWS-signed requests.
func needsSigV4(host string) bool {
	return host == "public.ecr.aws" || (strings.Contains(host, ".dkr.ecr.") && strings.HasSuffix(host, ".amazonaws.com"))
}

// decodeJSON reads a bounded JSON body.
func decodeJSON(resp *http.Response, limit int64, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, limit)).Decode(v)
}
