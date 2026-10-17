package ichorgo

import (
	"context"
	"net/http"
)

// giteaForge is a Gitea or Forgejo instance (Codeberg included), <host>/api/v1: its contents
// API answers like GitHub's.
type giteaForge struct{ c gitForgeClient }

func (g giteaForge) base(repo gitRepoRef) string {
	return reposPath(repo) + "/contents"
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

// branchFrom is baseRef itself: Gitea branches from a name.
func (g giteaForge) branchFrom(_ context.Context, _ gitRepoRef, baseRef string) (string, error) {
	return baseRef, nil
}

func (g giteaForge) createBranch(ctx context.Context, repo gitRepoRef, from, branch string) error {
	return g.c.call(ctx, http.MethodPost, reposPath(repo)+"/branches", map[string]string{"new_branch_name": branch, "old_branch_name": from}, nil)
}

// commitFile updates with PUT and the sha; a new file is a POST (Gitea's PUT needs a sha).
func (g giteaForge) commitFile(ctx context.Context, repo gitRepoRef, p gitProposal) (string, error) {
	method := http.MethodPut
	if p.FileSha == "" {
		method = http.MethodPost
	}

	return putContents(ctx, g.c, method, g.base(repo)+"/"+escapePath(p.Path), p)
}

// openRequest opens the pull request; Gitea has no draft field, a "WIP:" title is its draft.
func (g giteaForge) openRequest(ctx context.Context, repo gitRepoRef, p gitProposal) (gitRequest, error) {
	var pr struct {
		HTMLURL string `json:"html_url"`
		Number  int    `json:"number"`
	}

	in := map[string]string{"head": p.Branch, "base": p.BaseRef, "title": draftTitle(p, "WIP:"), "body": p.Body}
	if err := g.c.call(ctx, http.MethodPost, reposPath(repo)+"/pulls", in, &pr); err != nil {
		return gitRequest{}, err
	}

	return gitRequest{URL: pr.HTMLURL, Number: pr.Number}, nil
}

func (g giteaForge) deleteBranch(ctx context.Context, repo gitRepoRef, branch string) error {
	return g.c.call(ctx, http.MethodDelete, reposPath(repo)+"/branches/"+escapePath(branch), nil, nil)
}
