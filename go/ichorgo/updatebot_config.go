package ichorgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	pathpkg "path"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/gitforge"
)

// A dependency-update bot for the cluster repository: the phone cannot run one, but it can
// tell whether the repository already has one and, when it has none, write the configuration
// file a person would otherwise have to learn the format of: a renovate.json enabling only
// the managers whose files the repository holds.

type updateBotFile struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // renovate, workflow, dependabot
}

type updateBotDraft struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type updateBotConfig struct {
	SHA     string          `json:"sha"`
	Present []updateBotFile `json:"present"`
	// Draft is empty (no path, no content) when a bot is configured or nothing is updatable.
	Draft updateBotDraft `json:"draft"`
	Notes []string       `json:"notes"`
}

const updateBotDraftPath = "renovate.json"

// updateBotConfigFiles are the bot's configuration files, at the root or in a forge directory
// (https://docs.renovatebot.com/configuration-options/: "Configuration file names").
var updateBotConfigFiles = []string{
	"renovate.json", "renovate.json5", ".renovaterc", ".renovaterc.json",
	".github/renovate.json", ".github/renovate.json5", ".gitlab/renovate.json", ".gitlab/renovate.json5",
}

// UpdateBotConfig tells whether repoURL at ref has a dependency-update bot configured and, when
// it has none, drafts one: {"sha","present":[{"path","kind"}],"draft":{"path","content"},
// "notes":[…]}. present lists the bot's configuration files, a workflow running it, or a
// Dependabot configuration; any of them leaves draft empty. The draft is a renovate.json for
// the Flux, Argo CD and Kubernetes manifests under kubeDir (the whole repository when blank)
// and the Talos and Kubernetes versions of talosDir/talconfig.yaml; notes say why each part is
// there and how to install the bot on the forge. Read from the repository's snapshot; one too
// large for it has its bot files and talosDir read through the forge, and every manager
// enabled under kubeDir. Cluster-free: the Updates screen reads present through it too.
func UpdateBotConfig(hostJSON, repoURL, ref, talosDir, kubeDir string) (out string, err error) {
	defer maskResult(&out, &err)

	h, err := gitforge.ParseHost(hostJSON)
	if err != nil {
		return "", err
	}

	repo, err := readTalosRepo(hostJSON, repoURL, ref, updateBotReader(talosDir))
	if err != nil {
		return "", err
	}

	cfg, err := updateBotConfigFor(repo.files, talosDir, kubeDir, h.Kind, repo.note != "")
	if err != nil {
		return "", err
	}

	cfg.SHA = repo.sha

	return toJSON(cfg)
}

// updateBotConfigFor is UpdateBotConfig on files (the repository's text files by path). partial:
// only the bot files and the Talos directory were read (too large for a snapshot).
func updateBotConfigFor(files map[string][]byte, talosDir, kubeDir, forge string, partial bool) (updateBotConfig, error) {
	cfg := updateBotConfig{Present: detectUpdateBot(files), Notes: []string{}}
	if len(cfg.Present) > 0 {
		names := make([]string, len(cfg.Present))
		for i, p := range cfg.Present {
			names[i] = p.Path
		}

		cfg.Notes = append(cfg.Notes, "A dependency-update bot is already configured: "+strings.Join(names, ", ")+".")

		return cfg, nil
	}

	found := findUpdatables(files, botDir(talosDir), botDir(kubeDir), partial)
	if found.empty() {
		cfg.Notes = append(cfg.Notes, "Nothing for a dependency-update bot to bump was found: no Flux, Argo CD or Kubernetes manifests"+
			botUnderDir(kubeDir)+", and no talconfig.yaml"+botUnderDir(talosDir)+".")

		return cfg, nil
	}

	content, err := renovateDraft(found)
	if err != nil {
		return updateBotConfig{}, err
	}

	cfg.Draft = updateBotDraft{Path: updateBotDraftPath, Content: content}
	cfg.Notes = append(cfg.Notes, found.notes()...)
	cfg.Notes = append(cfg.Notes, updateBotInstallNote(forge))

	if botHasSopsFiles(files) {
		cfg.Notes = append(cfg.Notes, "Files encrypted with SOPS are not touched by the managers above.")
	}

	return cfg, nil
}

// detectUpdateBot lists what already runs a dependency-update bot on the repository.
func detectUpdateBot(files map[string][]byte) []updateBotFile {
	present := []updateBotFile{}

	for _, p := range updateBotConfigFiles {
		if _, ok := files[p]; ok {
			present = append(present, updateBotFile{Path: p, Kind: "renovate"})
		}
	}

	workflows := []string{}

	for p, content := range files {
		if pathpkg.Dir(p) == ".github/workflows" && isYAMLPath(p) &&
			(bytes.Contains(content, []byte("renovatebot/github-action")) || bytes.Contains(content, []byte("renovate/renovate"))) {
			workflows = append(workflows, p)
		}
	}

	slices.Sort(workflows)

	for _, p := range workflows {
		present = append(present, updateBotFile{Path: p, Kind: "workflow"})
	}

	for _, p := range []string{".github/dependabot.yml", ".github/dependabot.yaml"} {
		if _, ok := files[p]; ok {
			present = append(present, updateBotFile{Path: p, Kind: "dependabot"})
		}
	}

	return present
}

func botHasSopsFiles(files map[string][]byte) bool {
	for p := range files {
		if p == ".sops.yaml" || strings.HasSuffix(p, ".sops.yaml") || strings.HasSuffix(p, ".sops.yml") {
			return true
		}
	}

	return false
}

// updateBotInstallNote says how the bot reaches a repository on the forge kind.
func updateBotInstallNote(forge string) string {
	switch forge {
	case gitforge.KindGitHub:
		return "After merging, install the Renovate GitHub app on this repository (github.com/apps/renovate): it opens its update pull requests from there."
	case gitforge.KindGitLab:
		return "After merging, run Renovate for this project: the hosted Mend Renovate app on gitlab.com, or a scheduled pipeline with the renovate/renovate image on a self-hosted GitLab."
	case gitforge.KindGitea:
		return "After merging, run Renovate yourself for this repository (Gitea and Forgejo have no hosted app): a scheduled Actions job with the renovate/renovate image and a bot account's token."
	default:
		return "After merging, install Renovate for this repository on its forge."
	}
}

func botDir(dir string) string { return strings.Trim(strings.TrimSpace(dir), "/") }

func botUnderDir(dir string) string {
	if d := botDir(dir); d != "" {
		return " under " + d + "/"
	}

	return ""
}

func botInDir(p, dir string) bool { return dir == "" || strings.HasPrefix(p, dir+"/") }

// renovateDraft is the configuration for found, as pretty JSON with a stable key order.
func renovateDraft(found updatables) (string, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(found.config()); err != nil {
		return "", fmt.Errorf("renovate.json: %w", err)
	}

	return buf.String(), nil
}
