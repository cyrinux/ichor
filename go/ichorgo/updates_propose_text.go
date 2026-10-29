package ichorgo

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
)

// The text of UpdateProposalPlan's pull request: its title, and a body with one table row per
// bump (name, from, to, kind, where the version comes from) and the items left out. English:
// it is text of the repository, not of the app.

var (
	updateCommitSha = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)
	updateTagLike   = regexp.MustCompile(`^[vV]?\d+(\.(\d+|[xX*]))*([-+][0-9A-Za-z.-]+)?$`)
)

// display is the item as the body and the apps name it: the image repository or the chart.
func (it updateProposalItem) display() string {
	return cmp.Or(it.Repo, it.Name)
}

// short is the item's last name segment, for a one-update title ("podinfo").
func (it updateProposalItem) short() string {
	name := it.display()

	return name[strings.LastIndexByte(name, '/')+1:]
}

// bump is the item's table row for the edit e that was applied.
func (it updateProposalItem) bump(e updateQuickEdit) updateProposalBump {
	to := it.Version
	if to == "" {
		to = e.Value
		if e.Op == "image" {
			to = cmp.Or(imageTag(e.Image), e.Image)
		}
	}

	kind := ""

	switch to {
	case it.Patch:
		kind = "patch"
	case it.Minor:
		kind = "minor"
	case it.Major:
		kind = "major"
	}

	return updateProposalBump{name: it.display(), short: it.short(), from: cmp.Or(it.Current, it.Tag), to: to, kind: kind, source: it.sourceURL()}
}

// sourceURL is where the item's versions are listed: the chart's repository (its index or
// OCI registry), the image's Docker Hub page, or its registry.
func (it updateProposalItem) sourceURL() string {
	if it.Repo == "" {
		return it.RepoURL
	}

	repo := it.Repo
	host, rest, ok := strings.Cut(repo, "/")

	if ok && (strings.ContainsAny(host, ".:") || host == "localhost") {
		if host != "docker.io" && host != "index.docker.io" && host != "registry-1.docker.io" {
			return "https://" + repo
		}

		repo = rest
	}

	if lib, ok := strings.CutPrefix(repo, "library/"); ok {
		repo = lib
	}

	if !strings.Contains(repo, "/") {
		return "https://hub.docker.com/_/" + repo
	}

	return "https://hub.docker.com/r/" + repo
}

// updateProposalText is the pull request's title and body for bumps; skipped are listed
// under the table.
func updateProposalText(bumps []updateProposalBump, skipped []updateProposalSkip) (string, string) {
	title := "Update " + strconv.Itoa(len(bumps)) + " dependencies"
	if len(bumps) == 1 {
		title = "Update " + bumps[0].short + " to " + bumps[0].to
	}

	var b strings.Builder

	b.WriteString("| Name | From | To | Kind | Source |\n|---|---|---|---|---|\n")

	for _, u := range bumps {
		b.WriteString("| `" + u.name + "` | `" + u.from + "` | `" + u.to + "` | " + cmp.Or(u.kind, "–") + " | " + cmp.Or(u.source, "–") + " |\n")
	}

	if len(skipped) > 0 {
		b.WriteString("\nNot in this pull request:\n\n")

		for _, s := range skipped {
			b.WriteString("- `" + s.Item + "`: " + s.Reason + "\n")
		}
	}

	b.WriteString("\nProposed from the Updates screen of Ichor.")

	return title, b.String()
}
