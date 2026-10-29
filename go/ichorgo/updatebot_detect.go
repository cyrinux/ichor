package ichorgo

import "path"

// updateBotFiles are the configuration files of the dependency bots, by repository path.
var updateBotFiles = map[string]string{
	"renovate.json":           "renovate",
	"renovate.json5":          "renovate",
	".renovaterc":             "renovate",
	".renovaterc.json":        "renovate",
	".renovaterc.json5":       "renovate",
	".github/renovate.json":   "renovate",
	".github/renovate.json5":  "renovate",
	".gitlab/renovate.json":   "renovate",
	".gitlab/renovate.json5":  "renovate",
	".github/dependabot.yml":  "dependabot",
	".github/dependabot.yaml": "dependabot",
	".forgejo/renovate.json":  "renovate",
	".gitea/renovate.json":    "renovate",
	".config/renovate.json":   "renovate",
	".config/renovate.json5":  "renovate",
}

// updateBotPresent names the dependency bot configured among files (repository paths), ""
// and false when none is.
func updateBotPresent(files []string) (string, bool) {
	for _, f := range files {
		if bot := updateBotFiles[path.Clean(f)]; bot != "" {
			return bot, true
		}
	}

	return "", false
}

// markBotUpdates marks the items a dependency bot of the cluster's repository bumps: those a
// GitOps tool applies from it. A repository that cannot be read is noted in "skipped".
func markBotUpdates(r *clusterUpdates, hostJSON, repoURL, ref string) {
	if ref == "" {
		ref = "HEAD"
	}

	snap, _, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err != nil {
		r.Skipped = append(r.Skipped, updateSkipped{What: "dependency bot", Reason: friendlyError(err)})

		return
	}

	files := make([]string, 0, len(snap.sizes))
	for f := range snap.sizes {
		files = append(files, f)
	}

	if _, ok := updateBotPresent(files); !ok {
		return
	}

	inGit := func(o *updateOwner) bool { return o != nil && (o.Tool == "flux" || o.Tool == "argocd") }

	for _, img := range r.Images {
		if img.AutomatedBy == "" && inGit(img.Owner) {
			img.AutomatedBy = "bot"
		}
	}

	for _, ch := range r.Charts {
		if ch.AutomatedBy == "" && inGit(ch.Owner) {
			ch.AutomatedBy = "bot"
		}
	}
}
