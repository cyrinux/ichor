package ichorgo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// gitLabForge is gitlab.com or a self-hosted GitLab (<host>/api/v4). A project is named by
// its URL-encoded full path (subgroups included).
type gitLabForge struct{ c gitForgeClient }

func gitLabProject(repo gitRepoRef) string { return "/projects/" + url.PathEscape(repo.full()) }

func (g gitLabForge) check(ctx context.Context) (gitForgeCheck, error) {
	var user struct {
		Username string `json:"username"`
	}

	if _, err := g.c.get(ctx, "/user", nil, &user); err != nil {
		return gitForgeCheck{}, err
	}

	out := gitForgeCheck{Login: user.Username, ScopesOK: true}

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

func (g gitLabForge) tree(ctx context.Context, repo gitRepoRef, ref, dir string) ([]gitForgeEntry, error) {
	var entries []gitForgeEntry

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
			return nil, errGitForgeNotFound
		}

		for _, it := range items {
			kind := "file"
			if it.Type == "tree" {
				kind = "dir"
			}

			entries = append(entries, gitForgeEntry{Path: it.Path, Type: kind})
		}

		next, err := strconv.Atoi(h.Get("X-Next-Page"))
		if err != nil || next <= page {
			break
		}

		page = next
	}

	return entries, nil
}

func (g gitLabForge) file(ctx context.Context, repo gitRepoRef, ref, path string) (gitForgeFile, error) {
	var it struct {
		FilePath     string `json:"file_path"`
		Size         int64  `json:"size"`
		Encoding     string `json:"encoding"`
		Content      string `json:"content"`
		LastCommitID string `json:"last_commit_id"`
	}

	// The file path is one URL-encoded segment (its slashes too).
	if _, err := g.c.get(ctx, gitLabProject(repo)+"/repository/files/"+url.PathEscape(path), url.Values{"ref": {ref}}, &it); err != nil {
		return gitForgeFile{}, err
	}

	f := gitForgeFile{Path: it.FilePath, SHA: it.LastCommitID, Size: it.Size}
	if it.Size > gitForgeMaxFile {
		f.Truncated = true

		return f, nil
	}

	if it.Encoding != "base64" {
		return gitForgeFile{}, fmt.Errorf("%s: unexpected encoding %q", path, it.Encoding)
	}

	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(it.Content, "\n", ""))
	if err != nil {
		return gitForgeFile{}, errors.Join(fmt.Errorf("%s: unreadable content", path), err)
	}

	f.Content = string(content)

	return f, nil
}
