package ichorgo

import (
	"context"
	"net/url"
)

// giteaForge is a Gitea or Forgejo instance (Codeberg included), <host>/api/v1: its contents
// API answers like GitHub's.
type giteaForge struct{ c gitForgeClient }

func (g giteaForge) base(repo gitRepoRef) string {
	return "/repos/" + url.PathEscape(repo.owner) + "/" + url.PathEscape(repo.name) + "/contents"
}

func (g giteaForge) check(ctx context.Context) (gitForgeCheck, error) {
	var user struct {
		Login string `json:"login"`
	}

	if _, err := g.c.get(ctx, "/user", nil, &user); err != nil {
		return gitForgeCheck{}, err
	}

	// Gitea does not tell a token's scopes: a repository it cannot read answers 404 later.
	return gitForgeCheck{Login: user.Login, ScopesOK: true}, nil
}

func (g giteaForge) tree(ctx context.Context, repo gitRepoRef, ref, dir string) ([]gitForgeEntry, error) {
	return contentsTree(ctx, g.c, g.base(repo), ref, dir)
}

func (g giteaForge) file(ctx context.Context, repo gitRepoRef, ref, path string) (gitForgeFile, error) {
	return contentsFile(ctx, g.c, g.base(repo), ref, path)
}
