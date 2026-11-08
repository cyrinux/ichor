package gitforge

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
	"regexp"
	"strings"
	"time"
)

// A proposed change: one file (or several, propose_files.go) committed on a new
// branch and a pull request (GitHub, Gitea) or merge request (GitLab) opened from it, for a
// human to merge on the forge. Ichor never commits to the tracked branch. Four requests at
// most for one file, no packfile.

const (
	// ProposeTimeout bounds the whole proposal: four writes and a rollback.
	ProposeTimeout = 60 * time.Second
	// gitProposeRollbackTimeout bounds the branch deletion after a failure, on its own clock.
	gitProposeRollbackTimeout = 10 * time.Second
	// gitProposeSlugMax bounds the title's part of a generated branch name.
	gitProposeSlugMax = 40
)

var (
	BranchName   = regexp.MustCompile(`^[\w./-]+$`)
	gitSlugBreak = regexp.MustCompile(`[^a-z0-9]+`)
)

// Proposal is GitForgePropose's optionsJSON.
type Proposal struct {
	RepoURL string `json:"repoURL"`
	BaseRef string `json:"baseRef"`
	Path    string `json:"path"`
	Content string `json:"content"`
	// FileSha is GitForgeFile's sha of the file edited; "" creates a new file.
	FileSha string `json:"fileSha"`
	// Files replaces Path, Content and FileSha for a proposal with several files.
	Files   []ProposalFile `json:"files,omitempty"`
	Message string         `json:"message"`
	Branch  string         `json:"branch"`
	Title   string         `json:"title"`
	Body    string         `json:"body"`
	Draft   bool           `json:"draft"`
}

type ProposeResult struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Branch string `json:"branch"`
	Commit string `json:"commit"`
	// Commits are every commit made, in order (one per file on GitHub).
	Commits []string `json:"commits,omitempty"`
	Forge   string   `json:"forge"`
}

// gitRequest is an opened pull or merge request; Commit is its head when the forge tells it.
type gitRequest struct {
	URL    string
	Number int
	Commit string
}

// gitProposer is one forge's writes. branchFrom is what a branch is created from (GitHub
// needs the base's commit, the others take its name); commitFile returns the new commit
// ("" when the forge does not tell it), commitFiles the commits for p.Files.
type gitProposer interface {
	branchFrom(ctx context.Context, repo RepoRef, baseRef string) (string, error)
	createBranch(ctx context.Context, repo RepoRef, from, branch string) error
	commitFile(ctx context.Context, repo RepoRef, p Proposal) (string, error)
	commitFiles(ctx context.Context, repo RepoRef, p Proposal) ([]string, error)
	openRequest(ctx context.Context, repo RepoRef, p Proposal) (gitRequest, error)
	deleteBranch(ctx context.Context, repo RepoRef, branch string) error
}

// CheckProposal checks a proposal and its repository against the host.
func CheckProposal(hostJSON string, p Proposal) (Host, RepoRef, Proposal, error) {
	h, repo, baseRef, path, err := Args(hostJSON, strings.TrimSpace(p.RepoURL), p.BaseRef, p.Path)
	if err != nil {
		return Host{}, RepoRef{}, Proposal{}, err
	}

	p.BaseRef, p.Path, p.FileSha, p.Branch = baseRef, path, strings.TrimSpace(p.FileSha), strings.TrimSpace(p.Branch)
	p.Message, p.Title = strings.TrimSpace(p.Message), strings.TrimSpace(p.Title)

	if p.Files != nil {
		if p.Path != "" || p.Content != "" || p.FileSha != "" {
			return Host{}, RepoRef{}, Proposal{}, errProposalPathAndFiles
		}

		if p.Files, err = checkProposalFiles(p.Files); err != nil {
			return Host{}, RepoRef{}, Proposal{}, err
		}
	} else if p.Path, err = proposalPath(path); err != nil {
		return Host{}, RepoRef{}, Proposal{}, err
	}

	switch {
	case p.Files != nil && len(p.Files) == 0:
		return Host{}, RepoRef{}, Proposal{}, errors.New("no file to propose")
	case len(p.Content) > MaxFile:
		return Host{}, RepoRef{}, Proposal{}, fmt.Errorf("%s is over 1 MiB: propose it from a computer", path)
	case p.Message == "":
		return Host{}, RepoRef{}, Proposal{}, errors.New("no commit message given")
	case p.Title == "":
		return Host{}, RepoRef{}, Proposal{}, errors.New("no title given")
	case p.Branch != "" && (!BranchName.MatchString(p.Branch) || strings.Contains(p.Branch, "..")):
		return Host{}, RepoRef{}, Proposal{}, fmt.Errorf("%q is not a branch name", p.Branch)
	case p.Branch == p.BaseRef:
		return Host{}, RepoRef{}, Proposal{}, fmt.Errorf("the change goes on a new branch, not on %s", p.BaseRef)
	}

	return h, repo, p, nil
}

// Propose creates the branch (once more with a new suffix when the name is taken), commits
// the file and opens the request; a failure once the branch exists deletes it.
func Propose(ctx context.Context, f gitProposer, repo RepoRef, p Proposal) (ProposeResult, error) {
	from, err := f.branchFrom(ctx, repo, p.BaseRef)
	if err != nil {
		return ProposeResult{}, branchError(err, repo, p.BaseRef)
	}

	base := p.Branch
	if base == "" {
		base = "ichor/" + Slug(p.Title)
		p.Branch = base + "-" + BranchSuffix()
	}

	err = f.createBranch(ctx, repo, from, p.Branch)
	if branchTaken(err) {
		p.Branch = base + "-" + BranchSuffix()
		err = f.createBranch(ctx, repo, from, p.Branch)
	}

	if err != nil {
		return ProposeResult{}, branchError(err, repo, p.BaseRef)
	}

	res, err := commitAndOpen(ctx, f, repo, p)
	if err != nil {
		rollbackBranch(f, repo, p.Branch)

		return ProposeResult{}, err
	}

	return res, nil
}

func commitAndOpen(ctx context.Context, f gitProposer, repo RepoRef, p Proposal) (ProposeResult, error) {
	commits, err := commitProposal(ctx, f, repo, p)

	switch {
	case staleFile(err) && len(p.Files) > 0:
		return ProposeResult{}, errStaleFiles(err, p.BaseRef)
	case staleFile(err):
		return ProposeResult{}, fmt.Errorf("the file changed on %s since you opened it: reload and try again", p.BaseRef)
	}

	if err != nil {
		return ProposeResult{}, err
	}

	req, err := f.openRequest(ctx, repo, p)
	if err != nil {
		return ProposeResult{}, err
	}

	var last string
	if len(commits) > 0 {
		last = commits[len(commits)-1]
	}

	return ProposeResult{URL: req.URL, Number: req.Number, Branch: p.Branch, Commit: cmp.Or(last, req.Commit), Commits: commits}, nil
}

// rollbackBranch deletes the proposal's branch, best effort, on its own clock: the
// proposal's may be what ran out.
func rollbackBranch(f gitProposer, repo RepoRef, branch string) {
	ctx, cancel := context.WithTimeout(context.Background(), gitProposeRollbackTimeout)
	defer cancel()

	_ = f.deleteBranch(ctx, repo, branch)
}

// branchError words a 404 on the base or the new branch: a GitHub fine-grained token not
// granted on the repository answers 404, not 403.
func branchError(err error, repo RepoRef, baseRef string) error {
	if errors.Is(err, ErrNotFound) {
		return fmt.Errorf("the token cannot see %s, or it has no branch %s", repo.Full(), baseRef)
	}

	return err
}

// branchTaken tells a branch name already in use: GitHub 422 "Reference already exists",
// GitLab 400 "Branch already exists", Gitea 409.
func branchTaken(err error) bool {
	switch StatusCode(err) {
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
	var s gitForgeStatusError
	if !errors.As(err, &s) {
		return false
	}

	msg := strings.ToLower(s.message)

	switch s.code {
	case http.StatusConflict:
		return !strings.Contains(msg, "empty")
	case http.StatusUnprocessableEntity:
		return strings.Contains(msg, "sha") || strings.Contains(msg, "already exists")
	case http.StatusBadRequest:
		return strings.Contains(msg, "changed") || strings.Contains(msg, "already exists")
	}

	return false
}

// StatusCode is the HTTP status of a forge's error answer, 0 for another error.
func StatusCode(err error) int {
	var s gitForgeStatusError
	if errors.As(err, &s) {
		return s.code
	}

	return 0
}

// Slug is a title as a branch name part: lower case, dashes, at most gitProposeSlugMax.
func Slug(title string) string {
	slug := strings.Trim(gitSlugBreak.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > gitProposeSlugMax {
		slug = strings.TrimRight(slug[:gitProposeSlugMax], "-")
	}

	return cmp.Or(slug, "change")
}

// BranchSuffix is 4 random hex digits (fixed by the tests).
var BranchSuffix = func() string {
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

	if err := json.NewDecoder(&io.LimitedReader{R: resp.Body, N: MaxBody}).Decode(out); err != nil {
		return fmt.Errorf("%s: unreadable answer: %w", c.host.host, err)
	}

	return nil
}

// draftTitle marks a draft in the title, for the forges without a draft field (GitLab
// "Draft:", Gitea "WIP:").
func draftTitle(p Proposal, prefix string) string {
	if p.Draft {
		return prefix + " " + p.Title
	}

	return p.Title
}
