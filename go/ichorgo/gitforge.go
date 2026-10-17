package ichorgo

import (
	"bytes"
	"cmp"
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
)

// A Git forge's REST API, read with the phone's own token: list a directory, read a file,
// check that a token works. No clone, no cluster access; the token comes with every call and
// is never kept.

// The forges the client speaks; demo answers from fixed data, auto guesses from the host.
const (
	gitForgeGitHub = "github"
	gitForgeGitLab = "gitlab"
	gitForgeGitea  = "gitea"
	gitForgeAuto   = "auto"
	gitForgeDemo   = "demo"
)

const (
	gitForgeTimeout = 20 * time.Second
	// gitForgeMaxFile is the largest file read: GitHub's contents API stops there too.
	gitForgeMaxFile = 1 << 20
	// gitForgeMaxBody bounds an answer: a 1 MiB file is about 1.4 MiB in base64, a full
	// directory page well under it. A bigger answer is a file read as truncated.
	gitForgeMaxBody = 3 << 20
	// gitForgeMaxPages bounds a GitLab directory listing (100 entries a page).
	gitForgeMaxPages = 50
)

// gitForgeHTTP refuses redirects (newHTTPClient's default): the token is never carried to
// another address.
var gitForgeHTTP = newHTTPClient(httpClientOpts{timeout: gitForgeTimeout})

// errGitForgeNotFound is a 404, worded by the caller with the path and ref asked.
var errGitForgeNotFound = errors.New("not found")

// errGitForgeTooLarge is an answer over gitForgeMaxBody: a file read reports it truncated.
var errGitForgeTooLarge = errors.New("answer too large")

// gitForgeHost is where and how to reach a forge: {"kind","baseURL","token","aliases"};
// aliases are other host[:port] names of the same server (gitforge_alias.go).
type gitForgeHost struct {
	Kind    string   `json:"kind"`
	BaseURL string   `json:"baseURL"`
	Token   string   `json:"token"`
	Aliases []string `json:"aliases"`

	host    string   // baseURL's host
	aliases []string // Aliases, normalized
}

// gitRepoRef is a repository on a forge: owner may hold GitLab subgroups (group/sub).
type gitRepoRef struct {
	host, owner, name string
}

func (r gitRepoRef) full() string { return r.owner + "/" + r.name }

type gitForgeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // file or dir
	Size int64  `json:"size"`
}

type gitForgeTree struct {
	Ref     string          `json:"ref"`
	SHA     string          `json:"sha,omitempty"`
	Entries []gitForgeEntry `json:"entries"`
}

type gitForgeFile struct {
	Path string `json:"path"`
	// SHA is what a later update must name: the blob sha (GitHub, Gitea) or the last commit
	// of the file (GitLab).
	SHA       string `json:"sha"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type gitForgeCheck struct {
	Login    string   `json:"login"`
	Host     string   `json:"host"`
	ScopesOK bool     `json:"scopesOK"`
	Missing  []string `json:"missing"`
}

// gitForge is one forge's API behind the three reads and the writes of a proposal
// (gitforge_propose.go).
type gitForge interface {
	check(ctx context.Context) (gitForgeCheck, error)
	tree(ctx context.Context, repo gitRepoRef, ref, dir string) ([]gitForgeEntry, error)
	file(ctx context.Context, repo gitRepoRef, ref, path string) (gitForgeFile, error)
	gitProposer
}

// GitForgeCheck tells whether hostJSON's token works: {"login","host","scopesOK","missing"}.
func GitForgeCheck(hostJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	h, err := parseGitForgeHost(hostJSON)
	if err != nil {
		return "", err
	}

	if h.Kind == gitForgeDemo {
		return gitForgeJSON(gitForgeCheck{Login: "demo", Host: "git.home.example", ScopesOK: true, Missing: []string{}})
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitForgeTimeout)
	defer cancel()

	c, err := newGitForge(h).check(ctx)
	if err != nil {
		return "", err
	}

	c.Host = h.host
	if c.Missing == nil {
		c.Missing = []string{}
	}

	return gitForgeJSON(c)
}

// GitForgeTree lists the directory dir ("" for the root) of repoURL at ref (a branch, tag or
// commit), directories first: {"ref","entries":[{path,type,size}]}. One level only.
func GitForgeTree(hostJSON, repoURL, ref, dir string) (out string, err error) {
	defer maskResult(&out, &err)

	h, repo, ref, dir, err := gitForgeArgs(hostJSON, repoURL, ref, dir)
	if err != nil {
		return "", err
	}

	var entries []gitForgeEntry

	if h.Kind == gitForgeDemo {
		entries, err = demoGitForgeTree(dir)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), gitForgeTimeout)
		defer cancel()

		entries, err = newGitForge(h).tree(ctx, repo, ref, dir)
	}

	if errors.Is(err, errGitForgeNotFound) {
		return "", fmt.Errorf("%s is not in %s at %s", cmp.Or(dir, "/"), repo.full(), ref)
	}

	if err != nil {
		return "", err
	}

	sortGitForgeEntries(entries)

	return gitForgeJSON(gitForgeTree{Ref: ref, Entries: nonNil(entries)})
}

// sortGitForgeEntries puts directories first, then sorts by path.
func sortGitForgeEntries(entries []gitForgeEntry) {
	slices.SortFunc(entries, func(a, b gitForgeEntry) int {
		if a.Type != b.Type {
			if a.Type == "dir" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Path, b.Path)
	})
}

// GitForgeFile reads the file path of repoURL at ref: {"path","sha","size","content",
// "truncated"}; a file over 1 MiB comes back truncated, without content.
func GitForgeFile(hostJSON, repoURL, ref, path string) (out string, err error) {
	defer maskResult(&out, &err)

	h, repo, ref, path, err := gitForgeArgs(hostJSON, repoURL, ref, path)
	if err != nil {
		return "", err
	}

	if path == "" {
		return "", errors.New("no file path given")
	}

	var f gitForgeFile

	if h.Kind == gitForgeDemo {
		f, err = demoGitForgeFile(path)
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), gitForgeTimeout)
		defer cancel()

		f, err = newGitForge(h).file(ctx, repo, ref, path)
	}

	if errors.Is(err, errGitForgeNotFound) {
		return "", fmt.Errorf("%s is not in %s at %s", path, repo.full(), ref)
	}

	if err != nil {
		return "", err
	}

	if f.Size > gitForgeMaxFile || len(f.Content) > gitForgeMaxFile {
		f.Content, f.Truncated = "", true
	}

	return gitForgeJSON(f)
}

func gitForgeJSON(v any) (string, error) {
	out, err := json.Marshal(v)

	return string(out), err
}

// gitForgeArgs decodes and checks what every read takes; the path is cleaned of leading and
// trailing slashes and may not climb out of the repository.
func gitForgeArgs(hostJSON, repoURL, ref, path string) (gitForgeHost, gitRepoRef, string, string, error) {
	h, err := parseGitForgeHost(hostJSON)
	if err != nil {
		return gitForgeHost{}, gitRepoRef{}, "", "", err
	}

	repo := gitRepoRef{host: "git.home.example", owner: "example-org", name: "platform"}
	if h.Kind != gitForgeDemo {
		if repo.host, repo.owner, repo.name, err = gitForgeRepo(repoURL); err != nil {
			return gitForgeHost{}, gitRepoRef{}, "", "", err
		}

		// The token is for h.host: a repository elsewhere is not read with it. One on an
		// alias is read from h.host, the only address any request goes to.
		if err := h.checkGitRepoHost(repo, repoURL); err != nil {
			return gitForgeHost{}, gitRepoRef{}, "", "", err
		}

		repo.host = h.host
	}

	ref = strings.TrimSpace(ref)
	if ref == "" {
		return gitForgeHost{}, gitRepoRef{}, "", "", errors.New("no branch, tag or commit given")
	}

	path = strings.Trim(strings.TrimSpace(path), "/")
	if slices.Contains(strings.Split(path, "/"), "..") {
		return gitForgeHost{}, gitRepoRef{}, "", "", fmt.Errorf("%s leaves the repository", path)
	}

	return h, repo, ref, path, nil
}

// parseGitForgeHost decodes {"kind","baseURL","token"}: https only (http for a loopback
// address), the kind guessed from the host when "auto".
func parseGitForgeHost(hostJSON string) (gitForgeHost, error) {
	var h gitForgeHost
	if err := json.Unmarshal([]byte(hostJSON), &h); err != nil {
		return gitForgeHost{}, fmt.Errorf("invalid Git host: %w", err)
	}

	h.Kind = strings.ToLower(strings.TrimSpace(h.Kind))
	if h.Kind == gitForgeDemo {
		h.host = "git.home.example"

		return h, nil
	}

	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(h.BaseURL), "/"))
	if err != nil || u.Hostname() == "" {
		return gitForgeHost{}, fmt.Errorf("invalid Git host address %q", h.BaseURL)
	}

	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname())) {
		return gitForgeHost{}, fmt.Errorf("%s: the Git host must be reached over https", u.Hostname())
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

	if h.Kind == "" || h.Kind == gitForgeAuto {
		if h.Kind, err = gitForgeKindOf(h.host); err != nil {
			return gitForgeHost{}, err
		}
	}

	switch h.Kind {
	case gitForgeGitHub, gitForgeGitLab, gitForgeGitea:
	default:
		return gitForgeHost{}, fmt.Errorf("unknown Git host kind %q: pick GitHub, GitLab or Gitea in Settings", h.Kind)
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
		return gitForgeGitHub, nil
	case strings.Contains(host, "gitlab"):
		return gitForgeGitLab, nil
	case strings.Contains(host, "gitea"), strings.Contains(host, "forgejo"), host == "codeberg.org":
		return gitForgeGitea, nil
	}

	return "", fmt.Errorf("unknown Git host %s: pick GitHub, GitLab or Gitea in Settings", host)
}

// gitForgeRepo splits a repository URL (https, ssh or scp-like, with or without .git) into
// its host, owner and name; a GitLab project in subgroups keeps them in owner.
func gitForgeRepo(repoURL string) (host, owner, name string, err error) {
	web := argoRepoWebURL(repoURL)
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

func newGitForge(h gitForgeHost) gitForge {
	switch h.Kind {
	case gitForgeGitLab:
		return gitLabForge{gitForgeClient{host: h, api: h.BaseURL + "/api/v4"}}
	case gitForgeGitea:
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
	host gitForgeHost
	api  string
}

// get decodes the JSON answer to GET api+path?query into out and returns its headers;
// errGitForgeTooLarge past gitForgeMaxBody.
func (c gitForgeClient) get(ctx context.Context, path string, query url.Values, out any) (http.Header, error) {
	resp, err := c.do(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck

	body := &io.LimitedReader{R: resp.Body, N: gitForgeMaxBody}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		if body.N == 0 {
			return nil, errGitForgeTooLarge
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
	if err == nil && resp.StatusCode == http.StatusUnauthorized && c.host.Kind == gitForgeGitLab && c.host.Token != "" {
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

	return gitForgeHTTP.Do(req)
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
		return errGitForgeNotFound
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
