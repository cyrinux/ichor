package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
)

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
	markSOPSEntries(entries)

	return gitForgeJSON(gitForgeTree{Ref: ref, Entries: nonNil(entries)})
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

// GitForgeRequest reads the pull request (merge request on GitLab, by its iid) number of
// repoURL with hostJSON's token: {"state":"open|merged|closed","title","url","headSha",
// "mergeSha","mergedAt","updatedAt","draft"}. The demo host answers its sample proposals
// (number 1 open, 2 merged).
func GitForgeRequest(hostJSON, repoURL string, number int) (out string, err error) {
	defer maskResult(&out, &err)

	if number <= 0 {
		return "", errGitRequestNumber
	}

	// A request is read by number: the ref the reads take is not used.
	h, repo, _, _, err := gitForgeArgs(hostJSON, repoURL, "HEAD", "")
	if err != nil {
		return "", err
	}

	if h.Kind == gitForgeDemo {
		return gitForgeJSON(demoGitRequest(repo, number))
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitForgeTimeout)
	defer cancel()

	state, err := newGitForge(h).request(ctx, repo, number)
	if errors.Is(err, errGitForgeNotFound) {
		return "", fmt.Errorf("pull request #%d is not in %s (or the token cannot read it)", number, repo.full())
	}

	if err != nil {
		return "", err
	}

	return gitForgeJSON(state)
}
