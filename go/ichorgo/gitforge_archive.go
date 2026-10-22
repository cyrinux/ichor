package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// A repository's whole tree at one ref, as the forge's tar.gz archive: one request where a
// clone would pull packfiles. GitHub answers with a redirect to a signed URL on another host
// (codeload.github.com): that one is fetched without the token.

// gitForgeMaxArchive bounds an archive as downloaded (compressed).
const gitForgeMaxArchive = 48 << 20

// gitForgeArchiveTimeout bounds an archive download, longer than an API call.
const gitForgeArchiveTimeout = 2 * time.Minute

var errGitForgeArchiveTooLarge = errors.New("the repository archive is over 48 MiB; browse it file by file")

// gitArchiver names where a forge serves a repository's archive at ref, below its API.
type gitArchiver interface {
	archivePath(repo gitRepoRef, ref string) (string, url.Values)
}

func (g gitHubForge) archivePath(repo gitRepoRef, ref string) (string, url.Values) {
	return reposPath(repo) + "/tarball/" + escapePath(ref), nil
}

func (g gitLabForge) archivePath(repo gitRepoRef, ref string) (string, url.Values) {
	return gitLabProject(repo) + "/repository/archive.tar.gz", url.Values{"sha": {ref}}
}

func (g giteaForge) archivePath(repo gitRepoRef, ref string) (string, url.Values) {
	return reposPath(repo) + "/archive/" + escapePath(ref) + ".tar.gz", nil
}

// gitCommitResolver tells the commit a branch, tag, short sha or HEAD stands for.
type gitCommitResolver interface {
	refCommit(ctx context.Context, repo gitRepoRef, ref string) (string, error)
}

func (g gitHubForge) refCommit(ctx context.Context, repo gitRepoRef, ref string) (string, error) {
	var c struct {
		SHA string `json:"sha"`
	}

	_, err := g.c.get(ctx, reposPath(repo)+"/commits/"+escapePath(ref), nil, &c)

	return c.SHA, err
}

func (g gitLabForge) refCommit(ctx context.Context, repo gitRepoRef, ref string) (string, error) {
	var c struct {
		ID string `json:"id"`
	}

	_, err := g.c.get(ctx, gitLabProject(repo)+"/repository/commits/"+url.PathEscape(ref), nil, &c)

	return c.ID, err
}

func (g giteaForge) refCommit(ctx context.Context, repo gitRepoRef, ref string) (string, error) {
	var list []struct {
		SHA string `json:"sha"`
	}

	q := url.Values{"sha": {ref}, "limit": {"1"}, "stat": {"false"}, "verification": {"false"}, "files": {"false"}}
	if _, err := g.c.get(ctx, reposPath(repo)+"/commits", q, &list); err != nil {
		return "", err
	}

	if len(list) == 0 {
		return "", errGitForgeNotFound
	}

	return list[0].SHA, nil
}

// gitForgeArchive downloads repo's archive at ref through h's API, at most gitForgeMaxArchive.
func gitForgeArchive(ctx context.Context, h gitForgeHost, repo gitRepoRef, ref string) ([]byte, error) {
	f := newGitForge(h)
	path, query := f.archivePath(repo, ref)

	return gitForgeClientOf(f).download(ctx, path, query)
}

// gitForgeClientOf is the client behind one of the three forges.
func gitForgeClientOf(f gitForge) gitForgeClient {
	switch f := f.(type) {
	case gitHubForge:
		return f.c
	case gitLabForge:
		return f.c
	case giteaForge:
		return f.c
	}

	panic(fmt.Sprintf("unknown forge %T", f))
}

// download reads the body of GET api+path?query, following one redirect: with the token on
// the forge's own address, without it anywhere else.
func (c gitForgeClient) download(ctx context.Context, path string, query url.Values) ([]byte, error) {
	target := c.api + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	privateToken := false

	resp, err := c.sendVia(ctx, gitForgeArchiveHTTP, http.MethodGet, target, nil, false)
	if err == nil && resp.StatusCode == http.StatusUnauthorized && c.host.Kind == gitForgeGitLab && c.host.Token != "" {
		resp.Body.Close() //nolint:errcheck

		privateToken = true
		resp, err = c.sendVia(ctx, gitForgeArchiveHTTP, http.MethodGet, target, nil, true)
	}

	if err != nil {
		return nil, c.scrub(fmt.Errorf("%s: %w", c.host.host, err))
	}

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := resp.Header.Get("Location")
		resp.Body.Close() //nolint:errcheck

		if resp, err = c.followArchive(ctx, target, loc, privateToken); err != nil {
			return nil, err
		}
	}

	defer resp.Body.Close() //nolint:errcheck

	if err := c.status(resp); err != nil {
		return nil, err
	}

	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && n > gitForgeMaxArchive {
		return nil, errGitForgeArchiveTooLarge
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, gitForgeMaxArchive+1))
	if err != nil {
		return nil, c.scrub(fmt.Errorf("%s: %w", c.host.host, err))
	}

	if len(body) > gitForgeMaxArchive {
		return nil, errGitForgeArchiveTooLarge
	}

	return body, nil
}

// followArchive fetches the redirect loc of from: https only (http for a loopback address),
// the token (in the header the first request used) only when loc is on the forge's own
// scheme and host. A second redirect is the answer (status words it).
func (c gitForgeClient) followArchive(ctx context.Context, from, loc string, privateToken bool) (*http.Response, error) {
	base, err := url.Parse(from)
	if err != nil {
		return nil, err
	}

	next, err := base.Parse(loc)
	if loc == "" || err != nil || next.Hostname() == "" {
		return nil, fmt.Errorf("%s answered with a redirect to nowhere", c.host.host)
	}

	if next.Scheme != "https" && !(next.Scheme == "http" && isLoopbackHost(next.Hostname())) {
		return nil, fmt.Errorf("%s redirected the archive to a plain http address", c.host.host)
	}

	if next.Scheme == base.Scheme && next.Host == base.Host {
		resp, err := c.sendVia(ctx, gitForgeArchiveHTTP, http.MethodGet, next.String(), nil, privateToken)
		if err != nil {
			return nil, c.scrub(fmt.Errorf("%s: %w", c.host.host, err))
		}

		return resp, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, next.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "ichor")

	resp, err := gitForgeArchiveHTTP.Do(req)
	if err != nil {
		// The signed URL is a credential of its own: never in an error.
		return nil, fmt.Errorf("%s: the archive download failed", next.Hostname())
	}

	return resp, nil
}

// gitForgeArchiveHTTP downloads archives: longer than an API call, never following a redirect.
var gitForgeArchiveHTTP = httpx.New(httpx.Opts{Timeout: gitForgeArchiveTimeout})
