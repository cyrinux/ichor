// Package gitforge is the Git forge transport: GitHub, GitLab and Gitea/Forgejo, each reached
// over https with one token that comes with every call and is never kept. It lists a
// directory, reads a file or an archive, checks a token, and proposes a change as a branch and
// a pull or merge request. It does no masking of shared output, no audit log, no demo answers
// and no prefilled proposal text: the app's Go facade adds those.
package gitforge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// A Git forge's REST API, read with the phone's own token: list a directory, read a file,
// check that a token works. No clone, no cluster access; the token comes with every call and
// is never kept.

// The forges the client speaks; demo answers from fixed data, auto guesses from the host.
const (
	KindGitHub = "github"
	KindGitLab = "gitlab"
	KindGitea  = "gitea"
	KindAuto   = "auto"
	KindDemo   = "demo"
)

const (
	Timeout = 20 * time.Second
	// MaxFile is the largest file read: GitHub's contents API stops there too.
	MaxFile = 1 << 20
	// MaxBody bounds an answer: a 1 MiB file is about 1.4 MiB in base64, a full
	// directory page well under it. A bigger answer is a file read as truncated.
	MaxBody = 3 << 20
	// gitForgeMaxPages bounds a GitLab directory listing (100 entries a page).
	gitForgeMaxPages = 50
)

// gitForgeHTTP refuses redirects (httpx.New's default): the token is never carried to
// another address.
var gitForgeHTTP = httpx.New(httpx.Opts{Timeout: Timeout})

// ErrNotFound is a 404, worded by the caller with the path and ref asked.
var ErrNotFound = errors.New("not found")

// ErrTooLarge is an answer over MaxBody: a file read reports it truncated.
var ErrTooLarge = errors.New("answer too large")

// Host is where and how to reach a forge: {"kind","baseURL","token","aliases"};
// aliases are other host[:port] names of the same server (alias.go).
type Host struct {
	Kind    string   `json:"kind"`
	BaseURL string   `json:"baseURL"`
	Token   string   `json:"token"`
	Aliases []string `json:"aliases"`

	host    string   // baseURL's host
	aliases []string // Aliases, normalized
}

// HostName is the host of BaseURL, as ParseHost read it.
func (h Host) HostName() string { return h.host }

// RepoRef is a repository on a forge: owner may hold GitLab subgroups (group/sub).
type RepoRef struct {
	Host, Owner, Name string
}

func (r RepoRef) Full() string { return r.Owner + "/" + r.Name }

type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"` // file or dir
	Size int64  `json:"size"`
	// SOPS: a file SOPS encrypts, by its name (*.sops.yaml, *.enc.yaml).
	SOPS bool `json:"sops,omitempty"`
}

type Tree struct {
	Ref     string  `json:"ref"`
	SHA     string  `json:"sha,omitempty"`
	Entries []Entry `json:"entries"`
}

type File struct {
	Path string `json:"path"`
	// SHA is what a later update must name: the blob sha (GitHub, Gitea) or the last commit
	// of the file (GitLab).
	SHA       string `json:"sha"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type CheckResult struct {
	Login    string   `json:"login"`
	Host     string   `json:"host"`
	ScopesOK bool     `json:"scopesOK"`
	Missing  []string `json:"missing"`
}

// Forge is one forge's API behind the three reads and the writes of a proposal
// (propose.go).
type Forge interface {
	Check(ctx context.Context) (CheckResult, error)
	Tree(ctx context.Context, repo RepoRef, ref, dir string) ([]Entry, error)
	File(ctx context.Context, repo RepoRef, ref, path string) (File, error)
	gitRepoReader
	gitArchiver
	gitCommitResolver
	gitProposer
	gitRequestReader
}

// SortEntries puts directories first, then sorts by path.
func SortEntries(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int {
		if a.Type != b.Type {
			if a.Type == "dir" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Path, b.Path)
	})
}

// Args decodes and checks what every read takes; the path is cleaned of leading and
// trailing slashes and may not climb out of the repository.
func Args(hostJSON, repoURL, ref, path string) (Host, RepoRef, string, string, error) {
	h, err := ParseHost(hostJSON)
	if err != nil {
		return Host{}, RepoRef{}, "", "", err
	}

	repo := RepoRef{Host: "git.home.example", Owner: "example-org", Name: "platform"}
	if h.Kind != KindDemo {
		if repo.Host, repo.Owner, repo.Name, err = ParseRepoURL(repoURL); err != nil {
			return Host{}, RepoRef{}, "", "", err
		}

		// The token is for h.host: a repository elsewhere is not read with it. One on an
		// alias is read from h.host, the only address any request goes to.
		if err := h.checkGitRepoHost(repo, repoURL); err != nil {
			return Host{}, RepoRef{}, "", "", err
		}

		repo.Host = h.host
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Host{}, RepoRef{}, "", "", errors.New("no branch, tag or commit given")
	}

	path = strings.Trim(strings.TrimSpace(path), "/")
	if slices.Contains(strings.Split(path, "/"), "..") {
		return Host{}, RepoRef{}, "", "", fmt.Errorf("%s leaves the repository", path)
	}

	return h, repo, ref, path, nil
}

// ParseHost decodes {"kind","baseURL","token"}: https only (http for a loopback
// address), the kind guessed from the host when "auto".
func ParseHost(hostJSON string) (Host, error) {
	var h Host
	if err := json.Unmarshal([]byte(hostJSON), &h); err != nil {
		return Host{}, fmt.Errorf("invalid Git host: %w", err)
	}

	h.Kind = strings.ToLower(strings.TrimSpace(h.Kind))
	if h.Kind == KindDemo {
		h.host = "git.home.example"

		return h, nil
	}

	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(h.BaseURL), "/"))
	if err != nil || u.Hostname() == "" {
		return Host{}, fmt.Errorf("invalid Git host address %q", h.BaseURL)
	}

	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return Host{}, fmt.Errorf("%s: the Git host must be reached over https", u.Hostname())
	}

	// A pasted API address is the forge's own: the API path is added back per forge.
	path := strings.TrimRight(u.Path, "/")
	for _, api := range []string{"/api/v1", "/api/v3", "/api/v4"} {
		path = strings.TrimSuffix(path, api)
	}

	h.BaseURL, h.host = u.Scheme+"://"+u.Host+path, gitForgeHostName(u.Hostname())
	h.aliases = normalizeGitHostAliases(h.Aliases)
	if h.host == "github.com" {
		h.BaseURL = "https://github.com"
	}

	if h.Kind == "" || h.Kind == KindAuto {
		if h.Kind, err = gitForgeKindOf(h.host); err != nil {
			return Host{}, err
		}
	}

	switch h.Kind {
	case KindGitHub, KindGitLab, KindGitea:
	default:
		return Host{}, fmt.Errorf("unknown Git host kind %q: pick GitHub, GitLab or Gitea/Forgejo in Settings", h.Kind)
	}

	return h, nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

// gitForgeHostName is the forge a host name stands for: GitHub's API, www and SSH hosts are
// github.com, GitLab's SSH-over-443 host is gitlab.com.
func gitForgeHostName(host string) string {
	host = strings.ToLower(host)

	switch host {
	case "api.github.com", "www.github.com", "ssh.github.com":
		return "github.com"
	case "altssh.gitlab.com":
		return "gitlab.com"
	}

	return host
}

// gitForgeKindOf guesses a forge from its host name.
func gitForgeKindOf(host string) (string, error) {
	switch {
	case host == "github.com":
		return KindGitHub, nil
	case strings.Contains(host, "gitlab"):
		return KindGitLab, nil
	case strings.Contains(host, "gitea"), strings.Contains(host, "forgejo"), host == "codeberg.org":
		return KindGitea, nil
	}

	return "", fmt.Errorf("unknown Git host %s: pick GitHub, GitLab or Gitea/Forgejo in Settings", host)
}

// ParseRepoURL splits a repository URL (https, ssh or scp-like, with or without .git) into
// its host, owner and name; a GitLab project in subgroups keeps them in owner.
func ParseRepoURL(repoURL string) (host, owner, name string, err error) {
	web := RepoWebURL(repoURL)
	if web == "" {
		return "", "", "", fmt.Errorf("%s is not a Git repository address", repoURL)
	}

	rest := strings.TrimPrefix(web, "https://")
	host, path, _ := strings.Cut(rest, "/")

	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 || slices.Contains(segs, "") || slices.Contains(segs, ".") || slices.Contains(segs, "..") {
		return "", "", "", fmt.Errorf("%s does not name an owner and a repository", repoURL)
	}

	return gitForgeHostName(host), strings.Join(segs[:len(segs)-1], "/"), segs[len(segs)-1], nil
}

func New(h Host) Forge {
	switch h.Kind {
	case KindGitLab:
		return gitLabForge{gitForgeClient{host: h, api: h.BaseURL + "/api/v4"}}
	case KindGitea:
		return giteaForge{gitForgeClient{host: h, api: h.BaseURL + "/api/v1"}}
	default:
		api := h.BaseURL + "/api/v3" // GitHub Enterprise Server
		if h.host == "github.com" {
			api = "https://api.github.com"
		}

		return gitHubForge{gitForgeClient{host: h, api: api}}
	}
}

// escapePath escapes each segment of a repository path.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}

	return strings.Join(segs, "/")
}

// gitForgeClient sends the GETs of one forge with its token.
type gitForgeClient struct {
	host Host
	api  string
}

// get decodes the JSON answer to GET api+path?query into out and returns its headers;
// ErrTooLarge past MaxBody.
func (c gitForgeClient) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	resp, err := c.do(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck

	body := &io.LimitedReader{R: resp.Body, N: MaxBody}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if body.N == 0 {
			return nil, ErrTooLarge
		}

		return nil, fmt.Errorf("%s: unreadable answer: %w", c.host.host, err)
	}

	return resp.Header, nil
}

// head returns the headers of HEAD api+path?query.
func (c gitForgeClient) head(ctx context.Context, path string, query url.Values) (http.Header, error) {
	resp, err := c.do(ctx, http.MethodHead, path, query)
	if err != nil {
		return nil, err
	}

	resp.Body.Close() //nolint:errcheck

	return resp.Header, nil
}

// do sends one request and returns its 2xx answer. The token goes in Authorization: Bearer
// (GitLab behind a proxy that strips it: PRIVATE-TOKEN on a 401); a redirect is refused,
// never followed.
func (c gitForgeClient) do(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	return c.doBody(ctx, method, path, query, nil)
}

// doBody is do with a JSON body (nil for none), sent again as is with PRIVATE-TOKEN. A
// non-2xx answer is a gitForgeStatusError.
func (c gitForgeClient) doBody(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Response, error) {
	target := c.api + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	resp, err := c.send(ctx, method, target, body, false)
	if err == nil && resp.StatusCode == http.StatusUnauthorized && c.host.Kind == KindGitLab && c.host.Token != "" {
		resp.Body.Close() //nolint:errcheck

		resp, err = c.send(ctx, method, target, body, true)
	}

	if err != nil {
		return nil, c.scrub(fmt.Errorf("%s: %w", c.host.host, err))
	}

	if err := c.status(resp); err != nil {
		resp.Body.Close() //nolint:errcheck

		var s gitForgeStatusError
		if errors.As(err, &s) {
			return nil, s
		}

		return nil, gitForgeStatusError{code: resp.StatusCode, err: err}
	}

	return resp, nil
}

// gitForgeStatusError is a non-2xx answer as status words it; code and the forge's own
// message (token scrubbed) tell a conflict apart.
type gitForgeStatusError struct {
	code    int
	message string
	err     error
}

func (e gitForgeStatusError) Error() string { return e.err.Error() }
func (e gitForgeStatusError) Unwrap() error { return e.err }

func (c gitForgeClient) send(ctx context.Context, method, target string, body []byte, privateToken bool) (*http.Response, error) {
	return c.sendVia(ctx, gitForgeHTTP, method, target, body, privateToken)
}

// sendVia is send through client (an archive download takes longer than an API call).
func (c gitForgeClient) sendVia(ctx context.Context, client *http.Client, method, target string, body []byte, privateToken bool) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ichor")

	if c.host.Token != "" {
		if privateToken {
			req.Header.Set("PRIVATE-TOKEN", c.host.Token)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.host.Token)
		}
	}

	return client.Do(req)
}

// status words a non-2xx answer: never with the token in it.
func (c gitForgeClient) status(resp *http.Response) error {
	host := c.host.host

	switch code := resp.StatusCode; {
	case code >= 200 && code < 300:
		return nil
	case code >= 300 && code < 400:
		return fmt.Errorf("%s answered with a redirect: the token is not sent elsewhere; check the Git host address", host)
	case code == http.StatusUnauthorized:
		return fmt.Errorf("the token for %s was refused", host)
	case code == http.StatusNotFound:
		return ErrNotFound
	case code == http.StatusTooManyRequests || code == http.StatusForbidden && rateLimited(resp.Header):
		if until := rateLimitReset(resp.Header); !until.IsZero() {
			return fmt.Errorf("rate limited by %s until %s", host, until.Local().Format("15:04"))
		}

		return fmt.Errorf("rate limited by %s", host)
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096)) //nolint:errcheck

	msg := c.scrub(errors.New(forgeErrorMessage(raw))).Error()
	if msg == "" {
		return gitForgeStatusError{code: resp.StatusCode, err: fmt.Errorf("%s answered %s", host, resp.Status)}
	}

	return gitForgeStatusError{code: resp.StatusCode, message: msg, err: fmt.Errorf("%s answered %s: %s", host, resp.Status, msg)}
}

// scrub removes the token from an error, wherever it came from.
func (c gitForgeClient) scrub(err error) error {
	if c.host.Token == "" || !strings.Contains(err.Error(), c.host.Token) {
		return err
	}

	return errors.New(strings.ReplaceAll(err.Error(), c.host.Token, "…"))
}

// forgeErrorMessage is the "message" of a forge's error body (GitHub, GitLab, Gitea all use it).
func forgeErrorMessage(raw []byte) string {
	var body struct {
		Message any `json:"message"`
	}

	if json.Unmarshal(raw, &body) != nil || body.Message == nil {
		return ""
	}

	if s, ok := body.Message.(string); ok {
		return s
	}

	out, _ := json.Marshal(body.Message) //nolint:errcheck

	return string(out)
}

func rateLimited(h http.Header) bool {
	return h.Get("X-RateLimit-Remaining") == "0" || h.Get("RateLimit-Remaining") == "0" || h.Get("Retry-After") != ""
}

// rateLimitReset is when a rate limit ends: Retry-After seconds or a unix time (GitHub
// X-RateLimit-Reset, GitLab RateLimit-Reset); zero when unknown.
func rateLimitReset(h http.Header) time.Time {
	// Retry-After first: GitHub's secondary limits send it with the primary window's reset.
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s > 0 {
		return time.Now().Add(time.Duration(s) * time.Second)
	}

	for _, key := range []string{"X-RateLimit-Reset", "RateLimit-Reset"} {
		if s, err := strconv.ParseInt(h.Get(key), 10, 64); err == nil && s > 0 {
			return time.Unix(s, 0)
		}
	}

	return time.Time{}
}
