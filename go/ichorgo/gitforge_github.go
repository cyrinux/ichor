package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

func (g gitHubForge) check(ctx context.Context) (gitForgeCheck, error) {
	var user struct {
		Login string `json:"login"`
	}

	h, err := g.c.get(ctx, "/user", nil, &user)
	if err != nil {
		return gitForgeCheck{}, err
	}

	out := gitForgeCheck{Login: user.Login, ScopesOK: true}

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

func (g gitHubForge) tree(ctx context.Context, repo gitRepoRef, ref, dir string) ([]gitForgeEntry, error) {
	return contentsTree(ctx, g.c, "/repos/"+url.PathEscape(repo.owner)+"/"+url.PathEscape(repo.name)+"/contents", ref, dir)
}

func (g gitHubForge) file(ctx context.Context, repo gitRepoRef, ref, path string) (gitForgeFile, error) {
	return contentsFile(ctx, g.c, "/repos/"+url.PathEscape(repo.owner)+"/"+url.PathEscape(repo.name)+"/contents", ref, path)
}

// contentsTree lists dir through a contents endpoint (GitHub, Gitea): an array for a
// directory; a file answers an object.
func contentsTree(ctx context.Context, c gitForgeClient, base, ref, dir string) ([]gitForgeEntry, error) {
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

	entries := make([]gitForgeEntry, 0, len(items))
	for _, it := range items {
		kind := "file"
		if it.Type == "dir" {
			kind = "dir"
		}

		entries = append(entries, gitForgeEntry{Path: it.Path, Type: kind, Size: it.Size})
	}

	return entries, nil
}

// contentsFile reads path through a contents endpoint (GitHub, Gitea); content is base64.
// GitHub sends no content over 1 MiB (encoding "none"); Gitea sends it all, so an answer
// too large to read is a truncated file, described from its directory's listing.
func contentsFile(ctx context.Context, c gitForgeClient, base, ref, path string) (gitForgeFile, error) {
	var raw json.RawMessage

	_, err := c.get(ctx, base+"/"+escapePath(path), url.Values{"ref": {ref}}, &raw)
	if errors.Is(err, errGitForgeTooLarge) {
		return contentsListed(ctx, c, base, ref, path)
	}

	if err != nil {
		return gitForgeFile{}, err
	}

	var it contentsEntry
	if err := json.Unmarshal(raw, &it); err != nil {
		return gitForgeFile{}, fmt.Errorf("%s is a directory, not a file", path)
	}

	if it.Type != "file" {
		return gitForgeFile{}, fmt.Errorf("%s is a %s, not a file", path, it.Type)
	}

	f := gitForgeFile{Path: it.Path, SHA: it.SHA, Size: it.Size}
	if it.Size > gitForgeMaxFile || it.Encoding == "none" {
		f.Truncated = true

		return f, nil
	}

	if it.Encoding != "base64" {
		return gitForgeFile{}, fmt.Errorf("%s: unexpected encoding %q", path, it.Encoding)
	}

	content, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(it.Content, "\n", ""))
	if err != nil {
		return gitForgeFile{}, fmt.Errorf("%s: unreadable content: %w", path, err)
	}

	f.Content = string(content)

	return f, nil
}

// contentsListed describes a file too large to read from its directory's listing: its sha
// and size, truncated.
func contentsListed(ctx context.Context, c gitForgeClient, base, ref, path string) (gitForgeFile, error) {
	dir := base
	if i := strings.LastIndex(path, "/"); i >= 0 {
		dir += "/" + escapePath(path[:i])
	}

	var items []contentsEntry
	if _, err := c.get(ctx, dir, url.Values{"ref": {ref}}, &items); err != nil {
		return gitForgeFile{}, err
	}

	for _, it := range items {
		if it.Path == path {
			return gitForgeFile{Path: it.Path, SHA: it.SHA, Size: it.Size, Truncated: true}, nil
		}
	}

	return gitForgeFile{}, errGitForgeNotFound
}
