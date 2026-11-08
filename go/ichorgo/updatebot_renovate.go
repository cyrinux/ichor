package ichorgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	pathpkg "path"
	"regexp"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/gitforge"
)

// The renovate.json UpdateBotConfig drafts. Its keys follow the bot's schema as of 2025, where
// managerFilePatterns replaced fileMatch (https://docs.renovatebot.com/configuration-options/
// #managerfilepatterns): every key lives in the structs below, so a rename is one edit.

const renovateSchema = "https://docs.renovatebot.com/renovate-schema.json"

var (
	botFluxGroup      = []byte("toolkit.fluxcd.io/")
	botArgoCDGroup    = []byte("argoproj.io/")
	botKubeKind       = regexp.MustCompile(`(?m)^kind:\s*\S`)
	botKubeImage      = regexp.MustCompile(`(?m)^\s*-?\s*image:\s*["']?[\w.-]+[/:]`)
	botArgoCDApp      = regexp.MustCompile(`(?m)^kind:\s*(Application|ApplicationSet)\s*$`)
	botTalosInstaller = regexp.MustCompile(`factory\.talos\.dev/(?:[a-z0-9-]+-)?installer/[0-9a-f]+:v\d`)
	botStableVersion  = `^(?<version>v\d+\.\d+\.\d+)$`
)

type renovateFiles struct {
	ManagerFilePatterns []string `json:"managerFilePatterns"`
}

// renovateRegex is a custom regex manager (https://docs.renovatebot.com/modules/manager/regex/).
type renovateRegex struct {
	CustomType             string   `json:"customType"`
	Description            string   `json:"description"`
	ManagerFilePatterns    []string `json:"managerFilePatterns"`
	MatchStrings           []string `json:"matchStrings"`
	DatasourceTemplate     string   `json:"datasourceTemplate"`
	DepNameTemplate        string   `json:"depNameTemplate"`
	ExtractVersionTemplate string   `json:"extractVersionTemplate"`
}

type renovateRule struct {
	MatchManagers    []string `json:"matchManagers"`
	MatchUpdateTypes []string `json:"matchUpdateTypes"`
	GroupName        string   `json:"groupName"`
}

type renovateConfig struct {
	Schema         string          `json:"$schema"`
	Extends        []string        `json:"extends"`
	Flux           *renovateFiles  `json:"flux,omitempty"`
	ArgoCD         *renovateFiles  `json:"argocd,omitempty"`
	Kubernetes     *renovateFiles  `json:"kubernetes,omitempty"`
	CustomManagers []renovateRegex `json:"customManagers,omitempty"`
	PackageRules   []renovateRule  `json:"packageRules"`
}

// updatables is what a bot could bump in the repository, with the files that say so.
type updatables struct {
	kubeDir, talosDir        string
	flux, argoCD, kubernetes []string
	helm                     []string // Chart.yaml and values files: the bot's defaults cover them
	talconfig                string
	installer                []string
	// partial: the Kubernetes tree was not read (repository too large), every manager is on.
	partial bool
}

func (u updatables) empty() bool {
	return !u.partial && len(u.flux)+len(u.argoCD)+len(u.kubernetes) == 0 && u.talconfig == "" && len(u.installer) == 0
}

// findUpdatables scans the YAML files of the repository: manifests under kubeDir (the whole
// repository when blank, never the Talos directory), talconfig.yaml and installer images under
// talosDir.
func findUpdatables(files map[string][]byte, talosDir, kubeDir string, partial bool) updatables {
	u := updatables{kubeDir: kubeDir, talosDir: talosDir, partial: partial}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		c := files[p]
		if !isYAMLPath(p) || strings.Contains(p, ".sops.") {
			continue
		}

		talos := talosDir != "" && botInDir(p, talosDir)
		if botInDir(p, talosDir) && pathpkg.Base(p) == "talconfig.yaml" && u.talconfig == "" {
			u.talconfig = p
		}

		if (talos || talosDir == "") && botTalosInstaller.Match(c) {
			u.installer = append(u.installer, p)
		}

		if talos || !botInDir(p, kubeDir) {
			continue
		}

		switch base := pathpkg.Base(p); {
		case bytes.Contains(c, botFluxGroup):
			u.flux = append(u.flux, p)
		case bytes.Contains(c, botArgoCDGroup) && botArgoCDApp.Match(c):
			u.argoCD = append(u.argoCD, p)
		case base == "Chart.yaml" || strings.HasPrefix(base, "values"):
			u.helm = append(u.helm, p)
		case botKubeKind.Match(c) && botKubeImage.Match(c):
			u.kubernetes = append(u.kubernetes, p)
		}
	}

	return u
}

func botYAMLUnder(dir string) []string {
	if dir == "" {
		return []string{"**/*.y*ml"}
	}

	return []string{dir + "/**/*.y*ml"}
}

func (u updatables) config() renovateConfig {
	cfg := renovateConfig{Schema: renovateSchema, Extends: []string{"config:recommended"}, PackageRules: []renovateRule{}}
	managers := []string{}

	on := func(files []string, manager string) *renovateFiles {
		if len(files) == 0 && !u.partial {
			return nil
		}

		managers = append(managers, manager)

		return &renovateFiles{ManagerFilePatterns: botYAMLUnder(u.kubeDir)}
	}

	cfg.Flux, cfg.ArgoCD, cfg.Kubernetes = on(u.flux, "flux"), on(u.argoCD, "argocd"), on(u.kubernetes, "kubernetes")

	if u.talconfig != "" {
		cfg.CustomManagers = append(cfg.CustomManagers,
			talosVersionManager("Talos version in talconfig.yaml", u.talconfig, "talosVersion", "siderolabs/talos"),
			talosVersionManager("Kubernetes version in talconfig.yaml", u.talconfig, "kubernetesVersion", "kubernetes/kubernetes"))
	}

	if len(u.installer) > 0 {
		cfg.CustomManagers = append(cfg.CustomManagers, renovateRegex{
			CustomType:             "regex",
			Description:            "Talos installer images from the Image Factory",
			ManagerFilePatterns:    botYAMLUnder(u.talosDir),
			MatchStrings:           []string{`factory\.talos\.dev/(?:[a-z0-9-]+-)?installer/[0-9a-f]+:(?<currentValue>v\d+\.\d+\.\d+)`},
			DatasourceTemplate:     "github-releases",
			DepNameTemplate:        "siderolabs/talos",
			ExtractVersionTemplate: botStableVersion,
		})
	}

	if len(cfg.CustomManagers) > 0 {
		managers = append(managers, "custom.regex")
	}

	for _, m := range managers {
		cfg.PackageRules = append(cfg.PackageRules, renovateRule{
			MatchManagers: []string{m}, MatchUpdateTypes: []string{"patch"}, GroupName: m + " patch updates",
		})
	}

	return cfg
}

// talosVersionManager bumps key: vX.Y.Z in talconfig from repo's stable GitHub releases.
func talosVersionManager(description, talconfig, key, repo string) renovateRegex {
	return renovateRegex{
		CustomType:             "regex",
		Description:            description,
		ManagerFilePatterns:    []string{talconfig},
		MatchStrings:           []string{key + `:\s*["']?(?<currentValue>v\d+\.\d+\.\d+)`},
		DatasourceTemplate:     "github-releases",
		DepNameTemplate:        repo,
		ExtractVersionTemplate: botStableVersion,
	}
}

// botFilesNote is "N files under dir/ (for example first)".
func botFilesNote(files []string, dir string) string {
	n := fmt.Sprintf("%d files", len(files))
	if len(files) == 1 {
		n = "1 file"
	}

	return n + botUnderDir(dir) + " (for example " + files[0] + ")"
}

// notes says why each part of the draft is there.
func (u updatables) notes() []string {
	notes := []string{}

	if u.partial {
		notes = append(notes, "The repository is too large to scan from the phone: the Flux, Argo CD and Kubernetes managers are on for every YAML file"+botUnderDir(u.kubeDir)+".")
	}

	if len(u.flux) > 0 {
		notes = append(notes, "Flux: chart and source versions in "+botFilesNote(u.flux, u.kubeDir)+".")
	}

	if len(u.argoCD) > 0 {
		notes = append(notes, "Argo CD: the charts and images of Applications in "+botFilesNote(u.argoCD, u.kubeDir)+".")
	}

	if len(u.kubernetes) > 0 {
		notes = append(notes, "Kubernetes: container images in "+botFilesNote(u.kubernetes, u.kubeDir)+".")
	}

	if len(u.helm) > 0 {
		notes = append(notes, "Helm charts and values files are covered by the bot's defaults: "+botFilesNote(u.helm, u.kubeDir)+".")
	}

	if u.talconfig != "" {
		notes = append(notes, "Talos and Kubernetes versions in "+u.talconfig+", from their stable GitHub releases.")
	}

	if len(u.installer) > 0 {
		notes = append(notes, "Talos installer images from the Image Factory in "+botFilesNote(u.installer, u.talosDir)+".")
	}

	return append(notes, "Patch updates are grouped into one pull request per manager; no schedule and no automerge: you decide on the forge.")
}

// updateBotReader reads, from a repository too large for a snapshot, the files that tell a bot
// is configured (the root, .github, .github/workflows, .gitlab) and the Talos directory.
func updateBotReader(talosDir string) talosForgeReader {
	return func(ctx context.Context, f gitforge.Forge, repo gitforge.RepoRef, sha string) (map[string][]byte, error) {
		files := map[string][]byte{}

		for _, dir := range []string{"", ".github", ".github/workflows", ".gitlab"} {
			entries, err := f.Tree(ctx, repo, sha, dir)
			if errors.Is(err, gitforge.ErrNotFound) {
				continue
			}

			if err != nil {
				return nil, err
			}

			for _, e := range entries {
				if e.Type != "file" || e.Size > gitforge.MaxFile || !isUpdateBotFile(e.Path) {
					continue
				}

				file, err := f.File(ctx, repo, sha, e.Path)
				if err != nil {
					return nil, err
				}

				files[e.Path] = []byte(file.Content)
			}
		}

		if botDir(talosDir) == "" {
			return files, nil
		}

		talos, err := talosDirReader(talosDir)(ctx, f, repo, sha)
		if err != nil {
			return nil, err
		}

		maps.Copy(files, talos)

		return files, nil
	}
}

// isUpdateBotFile tells a file detectUpdateBot reads.
func isUpdateBotFile(p string) bool {
	return slices.Contains(updateBotConfigFiles, p) || p == ".github/dependabot.yml" || p == ".github/dependabot.yaml" ||
		(pathpkg.Dir(p) == ".github/workflows" && isYAMLPath(p))
}
