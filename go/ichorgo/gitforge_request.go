package ichorgo

import (
	"context"
	"errors"
	"strconv"
)

// The state of a pull or merge request the phone opened, read again later: open, merged or
// closed, its head and merge commits (a squash merge's differs from the head), when.

const (
	gitRequestOpen   = "open"
	gitRequestMerged = "merged"
	gitRequestClosed = "closed"
)

var errGitRequestNumber = errors.New("no pull request number given")

type gitRequestState struct {
	State     string `json:"state"` // open, merged, closed
	Title     string `json:"title"`
	URL       string `json:"url"`
	HeadSHA   string `json:"headSha"`
	MergeSHA  string `json:"mergeSha"`
	MergedAt  string `json:"mergedAt"`
	UpdatedAt string `json:"updatedAt"`
	Draft     bool   `json:"draft"`
}

// gitRequestReader reads one pull or merge request of a repository.
type gitRequestReader interface {
	request(ctx context.Context, repo gitRepoRef, number int) (gitRequestState, error)
}

func (g gitHubForge) request(ctx context.Context, repo gitRepoRef, number int) (gitRequestState, error) {
	return g.c.gitHubStylePullRequest(ctx, repo, number)
}

// gitHubStylePullRequest reads a pull request from GitHub, or from Gitea which mirrors
// GitHub's pull-request JSON.
func (c gitForgeClient) gitHubStylePullRequest(ctx context.Context, repo gitRepoRef, number int) (gitRequestState, error) {
	var pr struct {
		State          string `json:"state"`
		Title          string `json:"title"`
		HTMLURL        string `json:"html_url"`
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedAt       string `json:"merged_at"`
		UpdatedAt      string `json:"updated_at"`
		Draft          bool   `json:"draft"`
		Head           struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	if _, err := c.get(ctx, reposPath(repo)+"/pulls/"+strconv.Itoa(number), nil, &pr); err != nil {
		return gitRequestState{}, err
	}

	return gitRequestState{
		State: requestState(pr.State, pr.Merged), Title: pr.Title, URL: pr.HTMLURL, HeadSHA: pr.Head.SHA,
		MergeSHA: mergedOnly(pr.Merged, pr.MergeCommitSHA), MergedAt: pr.MergedAt, UpdatedAt: pr.UpdatedAt, Draft: pr.Draft,
	}, nil
}

func (g gitLabForge) request(ctx context.Context, repo gitRepoRef, number int) (gitRequestState, error) {
	var mr struct {
		State           string `json:"state"` // opened, merged, closed, locked
		Title           string `json:"title"`
		WebURL          string `json:"web_url"`
		SHA             string `json:"sha"`
		MergeCommitSHA  string `json:"merge_commit_sha"`
		SquashCommitSHA string `json:"squash_commit_sha"`
		MergedAt        string `json:"merged_at"`
		UpdatedAt       string `json:"updated_at"`
		Draft           bool   `json:"draft"`
	}

	if _, err := g.c.get(ctx, gitLabProject(repo)+"/merge_requests/"+strconv.Itoa(number), nil, &mr); err != nil {
		return gitRequestState{}, err
	}

	merged := mr.State == gitRequestMerged
	sha := mr.MergeCommitSHA
	if sha == "" {
		sha = mr.SquashCommitSHA
	}

	return gitRequestState{
		State: requestState(mr.State, merged), Title: mr.Title, URL: mr.WebURL, HeadSHA: mr.SHA,
		MergeSHA: mergedOnly(merged, sha), MergedAt: mr.MergedAt, UpdatedAt: mr.UpdatedAt, Draft: mr.Draft,
	}, nil
}

func (g giteaForge) request(ctx context.Context, repo gitRepoRef, number int) (gitRequestState, error) {
	return g.c.gitHubStylePullRequest(ctx, repo, number)
}

// requestState is open, merged or closed from a forge's state ("open", GitLab's "opened"
// read as "open"; anything else not merged is closed).
func requestState(state string, merged bool) string {
	switch {
	case merged:
		return gitRequestMerged
	case state == gitRequestOpen || state == "opened":
		return gitRequestOpen
	default:
		return gitRequestClosed
	}
}

// mergedOnly is sha for a merged request, "" otherwise (an open one's is a test merge).
func mergedOnly(merged bool, sha string) string {
	if merged {
		return sha
	}

	return ""
}
