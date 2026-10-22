package gitforge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// gitLabForge is gitlab.com or a self-hosted GitLab (<host>/api/v4). A project is named by
// its URL-encoded full path (subgroups included).
type gitLabForge struct{ c gitForgeClient }

func gitLabProject(repo RepoRef) string { return "/projects/" + url.PathEscape(repo.Full()) }

func (g gitLabForge) Check(ctx context.Context) (CheckResult, error) {
	var user struct {
		Username string `json:"username"`
	}

	if _, err := g.c.get(ctx, "/user", nil, &user); err != nil {
		return CheckResult{}, err
	}

	out := CheckResult{Login: user.Username, ScopesOK: true}

	// The repository API needs read_api (or api); an older GitLab without this endpoint is
	// trusted.
	var self struct {
		Scopes []string `json:"scopes"`
	}

	if _, err := g.c.get(ctx, "/personal_access_tokens/self", nil, &self); err == nil && len(self.Scopes) > 0 {
		if !slices.Contains(self.Scopes, "api") && !slices.Contains(self.Scopes, "read_api") {
			out.ScopesOK, out.Missing = false, []string{"read_api"}
		}
	}

	return out, nil
}

func (g gitLabForge) Tree(ctx context.Context, repo RepoRef, ref, dir string) ([]Entry, error) {
	var entries []Entry

	for page := 1; page <= gitForgeMaxPages; {
		var items []struct {
			Path string `json:"path"`
			Type string `json:"type"` // tree or blob
		}

		q := url.Values{"ref": {ref}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if dir != "" {
			q.Set("path", dir)
		}

		h, err := g.c.get(ctx, gitLabProject(repo)+"/repository/tree", q, &items)
		if err != nil {
			return nil, err
		}

		// GitLab answers an empty list, not a 404, for a path that is not a directory.
		if page == 1 && len(items) == 0 && dir != "" {
			return nil, ErrNotFound
		}

		for _, it := range items {
			kind := "file"
			if it.Type == "tree" {
				kind = "dir"
			}

			entries = append(entries, Entry{Path: it.Path, Type: kind})
		}

		next, err := strconv.Atoi(h.Get("X-Next-Page"))
		if err != nil || next <= page {
			break
		}

		page = next
	}

	return entries, nil
}

func (g gitLabForge) File(ctx context.Context, repo RepoRef, ref, path string) (File, error) {
	var it struct {
		FilePath     string `json:"file_path"`
		Size         int64  `json:"size"`
		Encoding     string `json:"encoding"`
		Content      string `json:"content"`
		LastCommitID string `json:"last_commit_id"`
	}

	// The file path is one URL-encoded segment (its slashes too).
	filePath, q := gitLabProject(repo)+"/repository/files/"+url.PathEscape(path), url.Values{"ref": {ref}}

	// HEAD tells the size first: GitLab sends a file's whole content, whatever its size.
	h, err := g.c.head(ctx, filePath, q)
	if err != nil {
		return File{}, err
	}

	if size, err := strconv.ParseInt(h.Get("X-Gitlab-Size"), 10, 64); err == nil && size > MaxFile {
		return File{Path: path, SHA: h.Get("X-Gitlab-Last-Commit-Id"), Size: size, Truncated: true}, nil
	}

	if _, err := g.c.get(ctx, filePath, q, &it); err != nil {
		return File{}, err
	}

	f := File{Path: it.FilePath, SHA: it.LastCommitID, Size: it.Size}
	if it.Size > MaxFile {
		f.Truncated = true

		return f, nil
	}

	if it.Encoding != "base64" {
		return File{}, fmt.Errorf("%s: unexpected encoding %q", path, it.Encoding)
	}

	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(it.Content, "\n", ""))
	if err != nil {
		return File{}, errors.Join(fmt.Errorf("%s: unreadable content", path), err)
	}

	f.Content = string(content)

	return f, nil
}

// branchFrom is baseRef itself: GitLab branches from a name.
func (g gitLabForge) branchFrom(_ context.Context, _ RepoRef, baseRef string) (string, error) {
	return baseRef, nil
}

func (g gitLabForge) createBranch(ctx context.Context, repo RepoRef, from, branch string) error {
	return g.c.call(ctx, http.MethodPost, gitLabProject(repo)+"/repository/branches", map[string]string{"branch": branch, "ref": from}, nil)
}

// commitFile updates with PUT and last_commit_id (the file's sha as GitForgeFile read it); a
// new file is a POST. GitLab does not answer the commit: the merge request tells it.
func (g gitLabForge) commitFile(ctx context.Context, repo RepoRef, p Proposal) (string, error) {
	in := map[string]string{"branch": p.Branch, "content": p.Content, "commit_message": p.Message}

	method := http.MethodPost
	if p.FileSha != "" {
		method, in["last_commit_id"] = http.MethodPut, p.FileSha
	}

	return "", g.c.call(ctx, method, gitLabProject(repo)+"/repository/files/"+url.PathEscape(p.Path), in, nil)
}

// openRequest opens the merge request, its branch removed once merged; a "Draft:" title is
// GitLab's draft.
func (g gitLabForge) openRequest(ctx context.Context, repo RepoRef, p Proposal) (gitRequest, error) {
	var mr struct {
		WebURL string `json:"web_url"`
		IID    int    `json:"iid"`
		SHA    string `json:"sha"`
	}

	in := map[string]any{
		"source_branch": p.Branch, "target_branch": p.BaseRef, "title": draftTitle(p, "Draft:"),
		"description": p.Body, "remove_source_branch": true,
	}
	if err := g.c.call(ctx, http.MethodPost, gitLabProject(repo)+"/merge_requests", in, &mr); err != nil {
		return gitRequest{}, err
	}

	return gitRequest{URL: mr.WebURL, Number: mr.IID, Commit: mr.SHA}, nil
}

func (g gitLabForge) deleteBranch(ctx context.Context, repo RepoRef, branch string) error {
	return g.c.call(ctx, http.MethodDelete, gitLabProject(repo)+"/repository/branches/"+url.PathEscape(branch), nil, nil)
}
