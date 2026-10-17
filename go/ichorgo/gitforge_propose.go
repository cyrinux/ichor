package ichorgo

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"regexp"
	"strings"
	"time"
)

// A proposed change: one file committed on a new branch and a pull request (GitHub, Gitea)
// or merge request (GitLab) opened from it, for a human to merge on the forge. Ichor never
// commits to the tracked branch. Four requests at most, no packfile.

const (
	// gitProposeTimeout bounds the whole proposal: four writes and a rollback.
	gitProposeTimeout = 60 * time.Second
	// gitProposeRollbackTimeout bounds the branch deletion after a failure, on its own clock.
	gitProposeRollbackTimeout = 10 * time.Second
	// gitProposeSlugMax bounds the title's part of a generated branch name.
	gitProposeSlugMax = 40
)

var (
	gitBranchName = regexp.MustCompile(`^[\w./-]+$`)
	gitSlugBreak  = regexp.MustCompile(`[^a-z0-9]+`)
)

// gitProposal is GitForgePropose's optionsJSON.
type gitProposal struct {
	RepoURL string `json:"repoURL"`
	BaseRef string `json:"baseRef"`
	Path    string `json:"path"`
	Content string `json:"content"`
	// FileSha is GitForgeFile's sha of the file edited; "" creates a new file.
	FileSha string `json:"fileSha"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Draft   bool   `json:"draft"`
}

type gitProposeResult struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	Forge  string `json:"forge"`
}

// gitRequest is an opened pull or merge request; Commit is its head when the forge tells it.
type gitRequest struct {
	URL    string
	Number int
	Commit string
}

// gitProposer is one forge's writes. branchFrom is what a branch is created from (GitHub
// needs the base's commit, the others take its name); commitFile returns the new commit
// ("" when the forge does not tell it).
type gitProposer interface {
	branchFrom(ctx context.Context, repo gitRepoRef, baseRef string) (string, error)
	createBranch(ctx context.Context, repo gitRepoRef, from, branch string) error
	commitFile(ctx context.Context, repo gitRepoRef, p gitProposal) (string, error)
	openRequest(ctx context.Context, repo gitRepoRef, p gitProposal) (gitRequest, error)
	deleteBranch(ctx context.Context, repo gitRepoRef, branch string) error
}

// GitForgePropose commits one file of repoURL on a new branch from baseRef and opens a pull
// (GitHub, Gitea) or merge (GitLab) request, with hostJSON's token. optionsJSON is
// {"repoURL","baseRef","path","content","fileSha","message","branch","title","body","draft"};
// fileSha is GitForgeFile's sha ("" for a new file), branch defaults to ichor/<title>-<hex>.
// The answer is {"url","number","branch","commit","forge"}. configYAML and contextName name
// the app's cluster, for the audit entry and the demo check only.
func GitForgePropose(configYAML, contextName, hostJSON, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	var (
		res    gitProposeResult
		target string
	)

	// recordedRun builds the entry once the request is open, with its URL; never the token
	// nor the content.
	err = recordedRun(configYAML, contextName, func() auditAction {
		return auditAction{Action: "gitops-propose", Object: target, Params: res.URL}
	}, func() error {
		var err error
		res, err = gitForgePropose(configYAML, contextName, hostJSON, optionsJSON, &target)

		return err
	})
	if err != nil {
		return "", err
	}

	return gitForgeJSON(res)
}

// gitForgePropose is GitForgePropose once its context is unmasked; target gets the
// repository and path once they are known.
func gitForgePropose(configYAML, contextName, hostJSON, optionsJSON string, target *string) (gitProposeResult, error) {
	h, err := parseGitForgeHost(hostJSON)
	if err != nil {
		return gitProposeResult{}, err
	}

	if h.Kind == gitForgeDemo || configYAML != "" && isDemoContext(configYAML, contextName) {
		return gitProposeResult{}, errDemoUnavailable
	}

	var p gitProposal
	if err := json.Unmarshal([]byte(optionsJSON), &p); err != nil {
		return gitProposeResult{}, fmt.Errorf("invalid proposal: %w", err)
	}

	h, repo, p, err := checkGitProposal(hostJSON, p)
	if err != nil {
		return gitProposeResult{}, err
	}

	*target = repo.full() + ":" + p.Path

	ctx, cancel := context.WithTimeout(context.Background(), gitProposeTimeout)
	defer cancel()

	res, err := propose(ctx, newGitForge(h), repo, p)
	res.Forge = h.Kind

	return res, err
}

// checkGitProposal checks a proposal and its repository against the host; the texts written
// about masked data (content, message, title, body) get their real names back.
func checkGitProposal(hostJSON string, p gitProposal) (gitForgeHost, gitRepoRef, gitProposal, error) {
	h, repo, baseRef, path, err := gitForgeArgs(hostJSON, strings.TrimSpace(p.RepoURL), p.BaseRef, p.Path)
	if err != nil {
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, err
	}

	p.BaseRef, p.Path, p.FileSha, p.Branch = baseRef, path, strings.TrimSpace(p.FileSha), strings.TrimSpace(p.Branch)
	p.Content, p.Body = privacy.reveal(p.Content), privacy.reveal(p.Body)
	p.Message, p.Title = privacy.reveal(strings.TrimSpace(p.Message)), privacy.reveal(strings.TrimSpace(p.Title))

	switch {
	case path == "" || pathpkg.Clean(path) != path:
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, fmt.Errorf("%q is not a file path in the repository", p.Path)
	case len(p.Content) > gitForgeMaxFile:
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, fmt.Errorf("%s is over 1 MiB: propose it from a computer", path)
	case p.Message == "":
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, errors.New("no commit message given")
	case p.Title == "":
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, errors.New("no title given")
	case p.Branch != "" && (!gitBranchName.MatchString(p.Branch) || strings.Contains(p.Branch, "..")):
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, fmt.Errorf("%q is not a branch name", p.Branch)
	case p.Branch == p.BaseRef:
		return gitForgeHost{}, gitRepoRef{}, gitProposal{}, fmt.Errorf("the change goes on a new branch, not on %s", p.BaseRef)
	}

	return h, repo, p, nil
}

// propose creates the branch (once more with a new suffix when the name is taken), commits
// the file and opens the request; a failure once the branch exists deletes it.
func propose(ctx context.Context, f gitProposer, repo gitRepoRef, p gitProposal) (gitProposeResult, error) {
	from, err := f.branchFrom(ctx, repo, p.BaseRef)
	if err != nil {
		return gitProposeResult{}, branchError(err, repo, p.BaseRef)
	}

	base := p.Branch
	if base == "" {
		base = "ichor/" + gitSlug(p.Title)
		p.Branch = base + "-" + gitBranchSuffix()
	}

	err = f.createBranch(ctx, repo, from, p.Branch)
	if branchTaken(err) {
		p.Branch = base + "-" + gitBranchSuffix()
		err = f.createBranch(ctx, repo, from, p.Branch)
	}

	if err != nil {
		return gitProposeResult{}, branchError(err, repo, p.BaseRef)
	}

	res, err := commitAndOpen(ctx, f, repo, p)
	if err != nil {
		rollbackBranch(f, repo, p.Branch)

		return gitProposeResult{}, err
	}

	return res, nil
}

func commitAndOpen(ctx context.Context, f gitProposer, repo gitRepoRef, p gitProposal) (gitProposeResult, error) {
	commit, err := f.commitFile(ctx, repo, p)
	if staleFile(err) {
		return gitProposeResult{}, fmt.Errorf("the file changed on %s since you opened it: reload and try again", p.BaseRef)
	}

	if err != nil {
		return gitProposeResult{}, err
	}

	req, err := f.openRequest(ctx, repo, p)
	if err != nil {
		return gitProposeResult{}, err
	}

	return gitProposeResult{URL: req.URL, Number: req.Number, Branch: p.Branch, Commit: cmp.Or(commit, req.Commit)}, nil
}

// rollbackBranch deletes the proposal's branch, best effort, on its own clock: the
// proposal's may be what ran out.
func rollbackBranch(f gitProposer, repo gitRepoRef, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitProposeRollbackTimeout)
	defer cancel()

	_ = f.deleteBranch(ctx, repo, branch)
}

// branchError words a 404 on the base or the new branch: a GitHub fine-grained token not
// granted on the repository answers 404, not 403.
func branchError(err error, repo gitRepoRef, baseRef string) error {
	if errors.Is(err, errGitForgeNotFound) {
		return fmt.Errorf("the token cannot see %s, or it has no branch %s", repo.full(), baseRef)
	}

	return err
}

// branchTaken tells a branch name already in use: GitHub 422 "Reference already exists",
// GitLab 400 "Branch already exists", Gitea 409.
func branchTaken(err error) bool {
	switch gitForgeStatus(err) {
	case http.StatusConflict:
		return true
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return strings.Contains(strings.ToLower(err.Error()), "already exists")
	}

	return false
}

// staleFile tells a commit refused because fileSha is not the file's sha on the base any
// more: GitHub 409, Gitea 409 or 422 "sha does not match", GitLab 400 "has changed since";
// a new file that already exists is one too.
func staleFile(err error) bool {
	msg := ""
	if err != nil {
		msg = strings.ToLower(err.Error())
	}

	switch gitForgeStatus(err) {
	case http.StatusConflict:
		return true
	case http.StatusUnprocessableEntity:
		return strings.Contains(msg, "sha") || strings.Contains(msg, "already exists")
	case http.StatusBadRequest:
		return strings.Contains(msg, "changed") || strings.Contains(msg, "already exists")
	}

	return false
}

// gitForgeStatus is the HTTP status of a forge's error answer, 0 for another error.
func gitForgeStatus(err error) int {
	var s gitForgeStatusError
	if errors.As(err, &s) {
		return s.code
	}

	return 0
}

// gitSlug is a title as a branch name part: lower case, dashes, at most gitProposeSlugMax.
func gitSlug(title string) string {
	slug := strings.Trim(gitSlugBreak.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > gitProposeSlugMax {
		slug = strings.TrimRight(slug[:gitProposeSlugMax], "-")
	}

	return cmp.Or(slug, "change")
}

// gitBranchSuffix is 4 random hex digits (fixed by the tests).
var gitBranchSuffix = func() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}

// call sends in as JSON (nil for no body) and decodes the answer into out (nil to discard
// it): the writes of a proposal.
func (c gitForgeClient) call(ctx context.Context, method, path string, in, out any) error {
	var body []byte

	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}

	resp, err := c.doBody(ctx, method, path, nil, body)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(&io.LimitedReader{R: resp.Body, N: gitForgeMaxBody}).Decode(out); err != nil {
		return fmt.Errorf("%s: unreadable answer: %w", c.host.host, err)
	}

	return nil
}

// draftTitle marks a draft in the title, for the forges without a draft field (GitLab
// "Draft:", Gitea "WIP:").
func draftTitle(p gitProposal, prefix string) string {
	if p.Draft {
		return prefix + " " + p.Title
	}

	return p.Title
}
