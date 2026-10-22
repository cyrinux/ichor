package ichorgo

import (
	"regexp"
	"strings"

	"github.com/cyrinux/ichor/go/gitforge"
)

// Browsable links for what Argo CD deploys: the repository page and the commit a revision
// names. Built on the phone from the source's repoURL and the revision; nothing is fetched.

var (
	// gitCommitSHA is a full or abbreviated Git object name.
	gitCommitSHA = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// argoCommitURL links the commit revision of repo: GitLab uses /-/commit/, Bitbucket
// /commits/, every other forge (GitHub, Gitea, Forgejo, Codeberg, Azure DevOps, unknown
// hosts) /commit/. "" when revision is not a commit or repo cannot be browsed.
func argoCommitURL(repo, revision string) string {
	revision = strings.TrimSpace(revision)
	if !gitCommitSHA.MatchString(revision) {
		return ""
	}

	base := gitforge.RepoWebURL(repo)
	if base == "" {
		return ""
	}

	host := strings.ToLower(strings.TrimPrefix(base, "https://"))
	host, _, _ = strings.Cut(host, "/")

	switch {
	case strings.Contains(host, "gitlab"):
		return base + "/-/commit/" + revision
	case strings.Contains(host, "bitbucket"):
		return base + "/commits/" + revision
	default:
		return base + "/commit/" + revision
	}
}

// argoRevisionURL links the commit revisions name, paired by position with sources, as
// Argo CD pairs status.sync.revisions with spec.sources. A single revision pairs with the
// first Git (non-chart) source. "" when no source is a browsable Git repository.
func argoRevisionURL(sources []argoSourceObject, revisions []string, revision string) string {
	if len(revisions) > 0 {
		for i, r := range revisions {
			if i < len(sources) && sources[i].Chart == "" {
				if u := argoCommitURL(sources[i].RepoURL, r); u != "" {
					return u
				}
			}
		}

		return ""
	}

	for _, s := range sources {
		if s.Chart == "" {
			return argoCommitURL(s.RepoURL, revision)
		}
	}

	return ""
}
