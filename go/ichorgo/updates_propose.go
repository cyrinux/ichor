package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cyrinux/ichor/go/gitforge"
)

// UpdateProposalPlan: several items of ClusterUpdates (the rows the user ticked, each with the
// candidate picked) turned into one pull request. Each item's file is found with
// UpdateEditTarget's resolver and read from the Git host at the branch the request goes to;
// its quick edit is applied on the running content, so two items of one file give one file
// with two changed lines. The apps stage the files and propose them with GitForgePropose.
// Nothing is written here.

// updateProposalItem is one itemsJSON entry: the item as ClusterUpdates gave it, plus
// "version" (the candidate picked, "" for the edit's own).
type updateProposalItem struct {
	updateEditItem

	Tag     string `json:"tag"`
	Current string `json:"current"`
	Patch   string `json:"patch"`
	Minor   string `json:"minor"`
	Major   string `json:"major"`
	RepoURL string `json:"repoURL"`
}

// updateProposalChange is one item's edit in a file: line is on the content already edited
// by the items before it.
type updateProposalChange struct {
	Item   string `json:"item"`
	Index  int    `json:"index"`
	Line   int    `json:"line"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type updateProposalFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	FileSha string `json:"fileSha"`
	// Original is the file as read, before the plan's edits.
	Original string                 `json:"original"`
	Changes  []updateProposalChange `json:"changes"`
}

type updateProposalSkip struct {
	Item   string `json:"item"`
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// updateProposalBump is one row of the body's table.
type updateProposalBump struct {
	name, short, from, to, kind, source string
}

type updateProposalPlan struct {
	Source  *gitopsSource          `json:"source,omitempty"`
	BaseRef string                 `json:"baseRef"`
	Files   []updateProposalFile   `json:"files"`
	Title   string                 `json:"title"`
	Message string                 `json:"message"`
	Branch  string                 `json:"branch"`
	Body    string                 `json:"body"`
	Skipped []updateProposalSkip   `json:"skipped"`
	bumps   []updateProposalBump   // the table's rows, in the items' order
	at      map[string]int         // index of a path in Files
	repo    *updateProposalRepoKey // the repository and branch every file is read from
}

type updateProposalRepoKey struct{ repoURL, base string }

// updateProposalRepo reads the files to change from the Git host: base is the branch a
// source's request goes to, file one file there.
type updateProposalRepo struct {
	base func(ctx context.Context, src gitopsSource) (string, error)
	file func(ctx context.Context, src gitopsSource, ref, path string) (gitforge.File, error)
}

// updateProposalMaxFiles is GitForgePropose's limit.
const updateProposalMaxFiles = 20

var errUpdateProposalNone = errors.New("select at least one update")

// UpdateProposalPlan plans one pull request for the items itemsJSON lists (ClusterUpdates'
// images[] and charts[] entries, each with "version"). → {"source","baseRef","files":[{path,
// content,original,fileSha,changes:[{item,index,line,before,after}]}],"title","message","branch",
// "body","skipped":[{item,index,reason}]}. Items in another repository than the first one
// planned, rendered by a chart, or with no file in Git are skipped with the reason. More than 20
// files is refused. hostJSON (GitForgeTree's) reads the files at the request's branch; the
// demo plans from its own files (proposing stays refused there). kubeServer: see KubePods.
func UpdateProposalPlan(configYAML, contextName, kubeServer, hostJSON, itemsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var items []updateProposalItem
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		return "", fmt.Errorf("items: %w", err)
	}

	if len(items) == 0 {
		return "", errUpdateProposalNone
	}

	for i := range items {
		revealUpdateItem(&items[i].updateEditItem)

		// A chart's own "version" is the one it runs: no candidate picked.
		if items[i].Version == items[i].Current {
			items[i].Version = ""
		}
	}

	contextName = unmaskContext(configYAML, contextName)
	if isDemoContext(configYAML, contextName) {
		plan, err := planUpdateProposal(context.Background(), demoUpdateEditEnv(), demoUpdateProposalRepo(), items)
		if err != nil {
			return "", err
		}

		return toJSON(plan)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitopsFindTimeout)
	defer cancel()

	target := kubeTarget{configYAML, contextName, kubeServer}

	plan, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (updateProposalPlan, error) {
		return planUpdateProposal(ctx, kubeUpdateEditEnv(k, target, hostJSON), forgeUpdateProposalRepo(hostJSON), items)
	})
	if err != nil {
		return "", err
	}

	return toJSON(plan)
}

func forgeUpdateProposalRepo(hostJSON string) updateProposalRepo {
	missing := func(src gitopsSource) error {
		return fmt.Errorf("add a Git host for %s in Settings → Git hosts to propose a change", cmp.Or(src.WebURL, src.RepoURL))
	}

	return updateProposalRepo{
		base: func(ctx context.Context, src gitopsSource) (string, error) {
			if b := updateProposalBranch(src.Tracked); b != "" {
				return b, nil
			}

			if strings.TrimSpace(hostJSON) == "" {
				return "", missing(src)
			}

			h, repo, err := gitforge.RepoArgs(hostJSON, src.RepoURL)
			if err != nil {
				return "", err
			}

			info, err := gitforge.ReadRepo(ctx, gitforge.New(h), repo, "")
			if err != nil {
				return "", gitforge.RepoError(err, h, repo, info.Branch)
			}

			return info.Branch, nil
		},
		file: func(ctx context.Context, src gitopsSource, ref, path string) (gitforge.File, error) {
			if strings.TrimSpace(hostJSON) == "" {
				return gitforge.File{}, missing(src)
			}

			h, repo, ref, path, err := gitforge.Args(hostJSON, src.RepoURL, ref, path)
			if err != nil {
				return gitforge.File{}, err
			}

			f, err := gitforge.New(h).File(ctx, repo, ref, path)
			if errors.Is(err, gitforge.ErrNotFound) {
				return gitforge.File{}, fmt.Errorf("%s is not in %s at %s", path, repo.Full(), ref)
			}

			return f, err
		},
	}
}

func demoUpdateProposalRepo() updateProposalRepo {
	return updateProposalRepo{
		base: func(context.Context, gitopsSource) (string, error) { return "main", nil },
		file: func(_ context.Context, _ gitopsSource, _, path string) (gitforge.File, error) {
			return demoGitForgeFile(path)
		},
	}
}

// updateProposalBranch is the branch a pull request from a source following tracked goes to,
// "" when tracked is no branch (a tag, a commit, a range, HEAD): the repository's default
// branch is taken then. Mirrors the apps' proposalBaseRef.
func updateProposalBranch(tracked string) string {
	ref := strings.TrimSpace(tracked)
	if rest, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
		ref = rest
	} else if ref == "HEAD" || strings.HasPrefix(ref, "refs/") || updateCommitSha.MatchString(ref) || updateTagLike.MatchString(ref) {
		return ""
	}

	if ref == "" || !gitforge.BranchName.MatchString(ref) || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") {
		return ""
	}

	return ref
}

func planUpdateProposal(ctx context.Context, env updateEditEnv, repo updateProposalRepo, items []updateProposalItem) (updateProposalPlan, error) {
	plan := updateProposalPlan{Files: []updateProposalFile{}, Skipped: []updateProposalSkip{}, at: map[string]int{}}

	for i, it := range items {
		reason, err := plan.add(ctx, env, repo, i, it)
		if err != nil {
			return updateProposalPlan{}, err
		}

		if reason != "" {
			plan.Skipped = append(plan.Skipped, updateProposalSkip{Item: it.display(), Index: i, Reason: reason})
		}
	}

	plan.Title, plan.Body = updateProposalText(plan.bumps, plan.Skipped)
	plan.Message = plan.Title
	plan.Branch = "ichor/" + gitforge.Slug(plan.Title) + "-" + gitforge.BranchSuffix()

	return plan, nil
}

// add plans item i: "" once its edit is in a file, else why it is skipped. An error ends the
// plan (too many files).
func (p *updateProposalPlan) add(ctx context.Context, env updateEditEnv, repo updateProposalRepo, i int, it updateProposalItem) (string, error) {
	target, err := resolveUpdateEdit(ctx, env, it.updateEditItem)
	if err != nil {
		return err.Error(), nil
	}

	if target.Edit == nil || target.Source == nil || target.Path == "" {
		return cmp.Or(target.Note, "no file in Git sets its version"), nil
	}

	src := *target.Source
	if reason := p.useSource(ctx, repo, src); reason != "" {
		return reason, nil
	}

	at, known := p.at[target.Path]

	file, reason, err := p.read(ctx, repo, src, target.Path)
	if err != nil || reason != "" {
		return reason, err
	}

	res, err := applyUpdateEdit([]byte(file.Content), *target.Edit)
	if err != nil {
		return err.Error(), nil
	}

	if !res.Changed {
		return target.Path + " already sets this version", nil
	}

	file.Changes = append(file.Changes, updateProposalChange{
		Item: it.display(), Index: i, Line: res.Line, Before: lineOf(file.Content, res.Line), After: lineOf(res.Content, res.Line),
	})
	file.Content = res.Content

	if known {
		p.Files[at] = file
	} else {
		p.at[target.Path] = len(p.Files)
		p.Files = append(p.Files, file)
	}

	p.bumps = append(p.bumps, it.bump(*target.Edit))

	return "", nil
}

// useSource takes src's repository for the plan when it has none yet: "" when src is in it,
// else why the item is skipped.
func (p *updateProposalPlan) useSource(ctx context.Context, repo updateProposalRepo, src gitopsSource) string {
	if p.repo != nil {
		if src.RepoURL != p.repo.repoURL {
			return "in another repository (" + cmp.Or(src.WebURL, src.RepoURL) + "): propose it on its own"
		}

		return ""
	}

	base, err := repo.base(ctx, src)
	if err != nil {
		return err.Error()
	}

	p.repo, p.Source, p.BaseRef = &updateProposalRepoKey{src.RepoURL, base}, &src, base

	return ""
}

// read is path as the plan has it (already edited), else as the Git host has it at the
// plan's branch; a reason when it cannot be read, an error past the file limit.
func (p *updateProposalPlan) read(ctx context.Context, repo updateProposalRepo, src gitopsSource, path string) (updateProposalFile, string, error) {
	if at, ok := p.at[path]; ok {
		return p.Files[at], "", nil
	}

	if len(p.Files) == updateProposalMaxFiles {
		return updateProposalFile{}, "", fmt.Errorf("one pull request changes %d files at most: select fewer updates", updateProposalMaxFiles)
	}

	f, err := repo.file(ctx, src, p.repo.base, path)
	if err != nil {
		return updateProposalFile{}, err.Error(), nil
	}

	if f.Truncated || int64(len(f.Content)) < f.Size {
		return updateProposalFile{}, path + " is too large to edit here", nil
	}

	return updateProposalFile{Path: path, Content: f.Content, FileSha: f.SHA, Original: f.Content}, "", nil
}

// lineOf is the 1-based line n of content, trimmed; "" past its end.
func lineOf(content string, n int) string {
	lines := strings.Split(content, "\n")
	if n < 1 || n > len(lines) {
		return ""
	}

	return strings.TrimSpace(lines[n-1])
}
