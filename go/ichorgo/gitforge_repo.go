package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	pathpkg "path"
	"strings"
)

// A cluster's own repository (its Talos patches, its Kubernetes tree), saved per cluster on
// the phone: what the forge says of it, and the defaults of a pull request on one of its
// files. The browser, viewer and editor are the source browser's, at the repository root.

// gitLabDeveloper is GitLab's Developer access level: the lowest that pushes a branch.
const gitLabDeveloper = 30

// gitForgeRepoInfo is GitForgeRepo's answer. Branch is the branch asked, or the default one.
type gitForgeRepoInfo struct {
	DefaultBranch string `json:"defaultBranch"`
	Branch        string `json:"branch"`
	HeadSha       string `json:"headSha"`
	WebURL        string `json:"webURL"`
	Private       bool   `json:"private"`
	CanPush       bool   `json:"canPush"`
	Forge         string `json:"forge"`
}

// gitRepoReader reads a repository's metadata and a branch's head commit.
type gitRepoReader interface {
	repoInfo(ctx context.Context, repo gitRepoRef) (gitForgeRepoInfo, error)
	branchHead(ctx context.Context, repo gitRepoRef, branch string) (string, error)
}

// GitForgeRepo tells what hostJSON's token sees of repoURL and where branch ("" for the
// default one) stands: {"defaultBranch","branch","headSha","webURL","private","canPush",
// "forge"}. The demo host answers its sample repository.
func GitForgeRepo(hostJSON, repoURL, branch string) (out string, err error) {
	defer maskResult(&out, &err)

	h, repo, err := gitForgeRepoArgs(hostJSON, repoURL)
	if err != nil {
		return "", err
	}

	branch = strings.TrimSpace(branch)
	if branch != "" && !gitBranchName.MatchString(branch) {
		return "", fmt.Errorf("%q is not a branch name", branch)
	}

	if h.Kind == gitForgeDemo {
		return gitForgeJSON(demoGitForgeRepo(branch))
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitForgeTimeout)
	defer cancel()

	info, err := readGitForgeRepo(ctx, newGitForge(h), repo, branch)
	if err != nil {
		return "", gitForgeRepoError(err, h, repo, info.Branch)
	}

	info.Forge = h.Kind

	return gitForgeJSON(info)
}

// readGitForgeRepo reads the repository, then the head of branch (the default one if "").
func readGitForgeRepo(ctx context.Context, f gitRepoReader, repo gitRepoRef, branch string) (gitForgeRepoInfo, error) {
	info, err := f.repoInfo(ctx, repo)
	if err != nil {
		return gitForgeRepoInfo{}, err
	}

	info.Branch = cmp.Or(branch, info.DefaultBranch)
	if info.Branch == "" {
		return info, fmt.Errorf("%s has no branch yet", repo.full())
	}

	if info.HeadSha, err = f.branchHead(ctx, repo, info.Branch); err != nil {
		return info, err
	}

	return info, nil
}

// gitForgeRepoError words a 404: the repository, or the branch once the repository was read.
func gitForgeRepoError(err error, h gitForgeHost, repo gitRepoRef, branch string) error {
	if !errors.Is(err, errGitForgeNotFound) {
		return err
	}

	if branch != "" {
		return fmt.Errorf("branch %s is not in %s", branch, repo.full())
	}

	if h.Kind == gitForgeGitHub {
		return fmt.Errorf("%s: not found or no access with this token (a fine-grained token must have this repository selected)", repo.full())
	}

	return fmt.Errorf("%s: not found or no access with this token", repo.full())
}

// gitForgeRepoArgs decodes the host and the repository, as gitForgeArgs does without a ref.
func gitForgeRepoArgs(hostJSON, repoURL string) (gitForgeHost, gitRepoRef, error) {
	h, err := parseGitForgeHost(hostJSON)
	if err != nil {
		return gitForgeHost{}, gitRepoRef{}, err
	}

	if h.Kind == gitForgeDemo {
		return h, gitRepoRef{host: h.host, owner: "example-org", name: "cluster-config"}, nil
	}

	var repo gitRepoRef
	if repo.host, repo.owner, repo.name, err = gitForgeRepo(repoURL); err != nil {
		return gitForgeHost{}, gitRepoRef{}, err
	}

	if err := h.checkGitRepoHost(repo, repoURL); err != nil {
		return gitForgeHost{}, gitRepoRef{}, err
	}

	repo.host = h.host

	return h, repo, nil
}

// gitHubRepo is the part of a GitHub or Gitea repository answer read here.
type gitHubRepo struct {
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Private       bool   `json:"private"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func (r gitHubRepo) info() gitForgeRepoInfo {
	return gitForgeRepoInfo{DefaultBranch: r.DefaultBranch, WebURL: r.HTMLURL, Private: r.Private, CanPush: r.Permissions.Push}
}

func (g gitHubForge) repoInfo(ctx context.Context, repo gitRepoRef) (gitForgeRepoInfo, error) {
	var r gitHubRepo
	if _, err := g.c.get(ctx, reposPath(repo), nil, &r); err != nil {
		return gitForgeRepoInfo{}, err
	}

	return r.info(), nil
}

func (g gitHubForge) branchHead(ctx context.Context, repo gitRepoRef, branch string) (string, error) {
	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if _, err := g.c.get(ctx, reposPath(repo)+"/branches/"+escapePath(branch), nil, &b); err != nil {
		return "", err
	}

	return b.Commit.SHA, nil
}

func (g giteaForge) repoInfo(ctx context.Context, repo gitRepoRef) (gitForgeRepoInfo, error) {
	var r gitHubRepo
	if _, err := g.c.get(ctx, reposPath(repo), nil, &r); err != nil {
		return gitForgeRepoInfo{}, err
	}

	return r.info(), nil
}

func (g giteaForge) branchHead(ctx context.Context, repo gitRepoRef, branch string) (string, error) {
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}

	if _, err := g.c.get(ctx, reposPath(repo)+"/branches/"+escapePath(branch), nil, &b); err != nil {
		return "", err
	}

	return b.Commit.ID, nil
}

func (g gitLabForge) repoInfo(ctx context.Context, repo gitRepoRef) (gitForgeRepoInfo, error) {
	type access struct {
		AccessLevel int `json:"access_level"`
	}

	var p struct {
		DefaultBranch string `json:"default_branch"`
		WebURL        string `json:"web_url"`
		Visibility    string `json:"visibility"`
		Permissions   struct {
			Project *access `json:"project_access"`
			Group   *access `json:"group_access"`
		} `json:"permissions"`
	}

	if _, err := g.c.get(ctx, gitLabProject(repo), nil, &p); err != nil {
		return gitForgeRepoInfo{}, err
	}

	level := 0
	for _, a := range []*access{p.Permissions.Project, p.Permissions.Group} {
		if a != nil {
			level = max(level, a.AccessLevel)
		}
	}

	return gitForgeRepoInfo{
		DefaultBranch: p.DefaultBranch, WebURL: p.WebURL, Private: p.Visibility != "public", CanPush: level >= gitLabDeveloper,
	}, nil
}

func (g gitLabForge) branchHead(ctx context.Context, repo gitRepoRef, branch string) (string, error) {
	var b struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}

	if _, err := g.c.get(ctx, gitLabProject(repo)+"/repository/branches/"+url.PathEscape(branch), nil, &b); err != nil {
		return "", err
	}

	return b.Commit.ID, nil
}

// GitForgeRepoProposalDefaults is the prefilled pull request for a change of the file path
// of the cluster repository repoURL: {"title","message","branch","body"} as
// GitOpsProposalDefaults answers it, titled "Update <path> for <cluster>", branch
// ichor/<cluster>-<file>-<4 hex>. No network.
func GitForgeRepoProposalDefaults(repoURL, path, cluster string) (out string, err error) {
	defer maskResult(&out, &err)

	p, err := gitForgeRepoProposalDefaults(repoURL, path, cluster)
	if err != nil {
		return "", err
	}

	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func gitForgeRepoProposalDefaults(repoURL, path, cluster string) (gitOpsProposal, error) {
	_, owner, name, err := gitForgeRepo(repoURL)
	if err != nil {
		return gitOpsProposal{}, err
	}

	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return gitOpsProposal{}, errTextDiffName
	}

	cluster = strings.TrimSpace(cluster)
	file := strings.TrimSuffix(pathpkg.Base(path), pathpkg.Ext(path))

	title, body := "Update "+path, "Proposed from Ichor in the cluster repository "+owner+"/"+name
	if cluster != "" {
		title += " for " + cluster
		body += " of " + cluster
	}

	return gitOpsProposal{
		Title:   title,
		Message: title,
		Branch:  "ichor/" + gitSlug(strings.Trim(cluster+"-"+file, "-")) + "-" + gitBranchSuffix(),
		Body:    body + ".",
	}, nil
}
