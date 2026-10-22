package gitforge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// gitHubForge is github.com (api.github.com) or a GitHub Enterprise Server (<host>/api/v3).
// Gitea's contents API has the same shape (giteaForge reuses the decoding).
type gitHubForge struct{ c gitForgeClient }

// contentsEntry is one item of a contents answer: a directory's list, or a file alone.
type contentsEntry struct {
	Type     string `json:"type"` // file, dir, symlink, submodule
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

func (g gitHubForge) Check(ctx context.Context) (CheckResult, error) {
	var user struct {
		Login string `json:"login"`
	}

	h, err := g.c.get(ctx, "/user", nil, &user)
	if err != nil {
		return CheckResult{}, err
	}

	out := CheckResult{Login: user.Login, ScopesOK: true}

	// A classic token lists its scopes; a fine-grained one lists none (its repository access
	// is checked when used).
	if scopes := h.Get("X-OAuth-Scopes"); scopes != "" {
		var list []string
		for _, s := range strings.Split(scopes, ",") {
			list = append(list, strings.TrimSpace(s))
		}

		if !slices.Contains(list, "repo") {
			out.ScopesOK, out.Missing = false, []string{"repo"}
		}
	}

	return out, nil
}

func (g gitHubForge) Tree(ctx context.Context, repo RepoRef, ref, dir string) ([]Entry, error) {
	return contentsTree(ctx, g.c, reposPath(repo)+"/contents", ref, dir)
}

func (g gitHubForge) File(ctx context.Context, repo RepoRef, ref, path string) (File, error) {
	return contentsFile(ctx, g.c, reposPath(repo)+"/contents", ref, path)
}

// contentsTree lists dir through a contents endpoint (GitHub, Gitea): an array for a
// directory; a file answers an object.
func contentsTree(ctx context.Context, c gitForgeClient, base, ref, dir string) ([]Entry, error) {
	path := base
	if dir != "" {
		path += "/" + escapePath(dir)
	}

	var raw json.RawMessage
	if _, err := c.get(ctx, path, url.Values{"ref": {ref}}, &raw); err != nil {
		return nil, err
	}

	var items []contentsEntry
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("%s is a file, not a directory", dir)
	}

	entries := make([]Entry, 0, len(items))
	for _, it := range items {
		kind := "file"
		if it.Type == "dir" {
			kind = "dir"
		}

		entries = append(entries, Entry{Path: it.Path, Type: kind, Size: it.Size})
	}

	return entries, nil
}

// contentsFile reads path through a contents endpoint (GitHub, Gitea); content is base64.
// GitHub sends no content over 1 MiB (encoding "none"); Gitea sends it all, so an answer
// too large to read is a truncated file, described from its directory's listing.
func contentsFile(ctx context.Context, c gitForgeClient, base, ref, path string) (File, error) {
	var raw json.RawMessage

	_, err := c.get(ctx, base+"/"+escapePath(path), url.Values{"ref": {ref}}, &raw)
	if errors.Is(err, ErrTooLarge) {
		return contentsListed(ctx, c, base, ref, path)
	}

	if err != nil {
		return File{}, err
	}

	var it contentsEntry
	if err := json.Unmarshal(raw, &it); err != nil {
		return File{}, fmt.Errorf("%s is a directory, not a file", path)
	}

	if it.Type != "file" {
		return File{}, fmt.Errorf("%s is a %s, not a file", path, it.Type)
	}

	f := File{Path: it.Path, SHA: it.SHA, Size: it.Size}
	if it.Size > MaxFile || it.Encoding == "none" {
		f.Truncated = true

		return f, nil
	}

	if it.Encoding != "base64" {
		return File{}, fmt.Errorf("%s: unexpected encoding %q", path, it.Encoding)
	}

	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(it.Content, "\n", ""))
	if err != nil {
		return File{}, fmt.Errorf("%s: unreadable content: %w", path, err)
	}

	f.Content = string(content)

	return f, nil
}

// contentsListed describes a file too large to read from its directory's listing: its sha
// and size, truncated.
func contentsListed(ctx context.Context, c gitForgeClient, base, ref, path string) (File, error) {
	dir := base
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir += "/" + escapePath(path[:i])
	}

	var items []contentsEntry
	if _, err := c.get(ctx, dir, url.Values{"ref": {ref}}, &items); err != nil {
		return File{}, err
	}

	for _, it := range items {
		if it.Path == path {
			return File{Path: it.Path, SHA: it.SHA, Size: it.Size, Truncated: true}, nil
		}
	}

	return File{}, ErrNotFound
}

// reposPath is a repository's API path on GitHub and Gitea.
func reposPath(repo RepoRef) string {
	return "/repos/" + url.PathEscape(repo.Owner) + "/" + url.PathEscape(repo.Name)
}

// branchFrom is the commit baseRef points to: a GitHub ref is created from a sha.
func (g gitHubForge) branchFrom(ctx context.Context, repo RepoRef, baseRef string) (string, error) {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}

	if err := g.c.call(ctx, http.MethodGet, reposPath(repo)+"/git/ref/heads/"+escapePath(baseRef), nil, &ref); err != nil {
		return "", err
	}

	if ref.Object.SHA == "" {
		return "", fmt.Errorf("%s: no commit for %s", repo.Full(), baseRef)
	}

	return ref.Object.SHA, nil
}

func (g gitHubForge) createBranch(ctx context.Context, repo RepoRef, from, branch string) error {
	return g.c.call(ctx, http.MethodPost, reposPath(repo)+"/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": from}, nil)
}

func (g gitHubForge) commitFile(ctx context.Context, repo RepoRef, p Proposal) (string, error) {
	return putContents(ctx, g.c, http.MethodPut, reposPath(repo)+"/contents/"+escapePath(p.Path), p)
}

func (g gitHubForge) openRequest(ctx context.Context, repo RepoRef, p Proposal) (gitRequest, error) {
	var pr struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}

	in := map[string]any{"head": p.Branch, "base": p.BaseRef, "title": p.Title, "body": p.Body, "draft": p.Draft}
	if err := g.c.call(ctx, http.MethodPost, reposPath(repo)+"/pulls", in, &pr); err != nil {
		return gitRequest{}, err
	}

	return gitRequest{URL: pr.HTMLURL, Number: pr.Number}, nil
}

func (g gitHubForge) deleteBranch(ctx context.Context, repo RepoRef, branch string) error {
	return g.c.call(ctx, http.MethodDelete, reposPath(repo)+"/git/refs/heads/"+escapePath(branch), nil, nil)
}

// putContents writes p's file through a contents endpoint (GitHub, Gitea) on p.Branch:
// base64 on one line (Gitea refuses line breaks), the sha only for an update. It returns
// the new commit.
func putContents(ctx context.Context, c gitForgeClient, method, path string, p Proposal) (string, error) {
	in := map[string]string{"message": p.Message, "content": base64.StdEncoding.EncodeToString([]byte(p.Content)), "branch": p.Branch}
	if p.FileSha != "" {
		in["sha"] = p.FileSha
	}

	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := c.call(ctx, method, path, in, &out); err != nil {
		return "", err
	}

	return out.Commit.SHA, nil
}
