package gitforge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	pathpkg "path"
	"slices"
	"strings"
)

// A proposal with several files: one commit on GitLab and Gitea (their APIs take a list of
// actions), one commit per file on the same branch on GitHub (its contents API is per file),
// then one request.

// ProposeMaxFiles bounds a proposal's files: 20 × MaxFile stays a few MiB of JSON
// through gomobile.
const ProposeMaxFiles = 20

// ProposalFile is one file of a proposal with several: its new content, or Delete.
// FileSha is GitForgeFile's sha of the file edited; "" creates a new file.
type ProposalFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	FileSha string `json:"fileSha"`
	Delete  bool   `json:"delete"`
}

// proposalPath is a file path of the repository, trimmed: not the root, not climbing out of
// it, not to be cleaned.
func proposalPath(raw string) (string, error) {
	path := strings.Trim(strings.TrimSpace(raw), "/")

	switch {
	case slices.Contains(strings.Split(path, "/"), ".."):
		return "", fmt.Errorf("%s leaves the repository", path)
	case path == "" || path == "." || pathpkg.Clean(path) != path:
		return "", fmt.Errorf("%q is not a file path in the repository", raw)
	}

	return path, nil
}

// checkProposalFiles checks a proposal's files: at most ProposeMaxFiles, each path once,
// each under MaxFile, a deletion with the sha it deletes.
func checkProposalFiles(files []ProposalFile) ([]ProposalFile, error) {
	if len(files) > ProposeMaxFiles {
		return nil, fmt.Errorf("%d files: propose at most %d together", len(files), ProposeMaxFiles)
	}

	out := make([]ProposalFile, 0, len(files))
	seen := map[string]bool{}

	for _, f := range files {
		path, err := proposalPath(f.Path)
		if err != nil {
			return nil, err
		}

		sha := strings.TrimSpace(f.FileSha)

		switch {
		case seen[path]:
			return nil, fmt.Errorf("%s is in the proposal twice", path)
		case len(f.Content) > MaxFile:
			return nil, fmt.Errorf("%s is over 1 MiB: propose it from a computer", path)
		case f.Delete && sha == "":
			return nil, fmt.Errorf("%s: deleting a file needs the sha it was read at", path)
		}

		seen[path] = true
		out = append(out, ProposalFile{Path: path, Content: f.Content, FileSha: sha, Delete: f.Delete})
	}

	return out, nil
}

// ProposalPaths are the paths a proposal writes, for the audit entry.
func ProposalPaths(p Proposal) string {
	if len(p.Files) == 0 {
		return p.Path
	}

	paths := make([]string, len(p.Files))
	for i, f := range p.Files {
		paths[i] = f.Path
	}

	return strings.Join(paths, ", ")
}

// commitProposal commits p's files on p.Branch and returns the new commits (none when the
// forge does not tell them).
func commitProposal(ctx context.Context, f gitProposer, repo RepoRef, p Proposal) ([]string, error) {
	if len(p.Files) > 0 {
		return f.commitFiles(ctx, repo, p)
	}

	commit, err := f.commitFile(ctx, repo, p)
	if err != nil || commit == "" {
		return nil, err
	}

	return []string{commit}, nil
}

// gitFileError is a write refused for one file of a proposal with several.
type gitFileError struct {
	path string
	err  error
}

func (e gitFileError) Error() string { return e.path + ": " + e.err.Error() }

func (e gitFileError) Unwrap() error { return e.err }

// errStaleFiles is a multi-file commit refused because a file changed on the base: named when
// the forge said which (one commit per file), else "a staged file". Opening it again reads it
// anew.
func errStaleFiles(err error, baseRef string) error {
	var fe gitFileError
	if errors.As(err, &fe) {
		return fmt.Errorf("%s changed on %s since you opened it: open it again and stage it again", fe.path, baseRef)
	}

	return fmt.Errorf("a staged file changed on %s since you opened it: open each again and stage it again", baseRef)
}

// commitEach commits p's files one by one: put writes a file, del deletes one. With several
// files each message is suffixed (1/3), (2/3)... The first failure stops, naming its file;
// the caller rolls the branch back.
func commitEach(ctx context.Context, p Proposal, put, del func(context.Context, Proposal) (string, error)) ([]string, error) {
	commits := make([]string, 0, len(p.Files))

	for i, f := range p.Files {
		one := p
		one.Path, one.Content, one.FileSha, one.Files = f.Path, f.Content, f.FileSha, nil

		if len(p.Files) > 1 {
			one.Message = fmt.Sprintf("%s (%d/%d)", p.Message, i+1, len(p.Files))
		}

		write := put
		if f.Delete {
			write = del
		}

		commit, err := write(ctx, one)
		if err != nil {
			return nil, gitFileError{path: f.Path, err: err}
		}

		if commit != "" {
			commits = append(commits, commit)
		}
	}

	return commits, nil
}

// deleteContents deletes p's file through a contents endpoint (GitHub, Gitea) on p.Branch,
// at the sha it was read at. It returns the new commit.
func deleteContents(ctx context.Context, c gitForgeClient, path string, p Proposal) (string, error) {
	in := map[string]string{"message": p.Message, "sha": p.FileSha, "branch": p.Branch}

	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := c.call(ctx, http.MethodDelete, path, in, &out); err != nil {
		return "", err
	}

	return out.Commit.SHA, nil
}

// commitFiles on GitHub is one contents write per file: its API has no multi-file commit
// short of the git data API.
func (g gitHubForge) commitFiles(ctx context.Context, repo RepoRef, p Proposal) ([]string, error) {
	return commitEach(ctx, p, func(ctx context.Context, one Proposal) (string, error) {
		return g.commitFile(ctx, repo, one)
	}, func(ctx context.Context, one Proposal) (string, error) {
		return deleteContents(ctx, g.c, reposPath(repo)+"/contents/"+escapePath(one.Path), one)
	})
}

// commitFiles on GitLab is one commit with an action per file; last_commit_id makes an
// update or a deletion fail when the file changed since it was read.
func (g gitLabForge) commitFiles(ctx context.Context, repo RepoRef, p Proposal) ([]string, error) {
	actions := make([]map[string]string, 0, len(p.Files))

	for _, f := range p.Files {
		a := map[string]string{"file_path": f.Path}

		switch {
		case f.Delete:
			a["action"] = "delete"
		case f.FileSha == "":
			a["action"], a["content"] = "create", f.Content
		default:
			a["action"], a["content"] = "update", f.Content
		}

		if f.FileSha != "" {
			a["last_commit_id"] = f.FileSha
		}

		actions = append(actions, a)
	}

	var out struct {
		ID string `json:"id"`
	}

	in := map[string]any{"branch": p.Branch, "commit_message": p.Message, "actions": actions}
	if err := g.c.call(ctx, http.MethodPost, gitLabProject(repo)+"/repository/commits", in, &out); err != nil {
		return nil, err
	}

	if out.ID == "" {
		return nil, nil
	}

	return []string{out.ID}, nil
}

// commitFiles on Gitea and Forgejo is one commit through the files endpoint (Gitea 1.20+);
// an older server without it answers 404 or 405 and gets one commit per file.
func (g giteaForge) commitFiles(ctx context.Context, repo RepoRef, p Proposal) ([]string, error) {
	files := make([]map[string]string, 0, len(p.Files))

	for _, f := range p.Files {
		e := map[string]string{"path": f.Path}

		switch {
		case f.Delete:
			e["operation"] = "delete"
		case f.FileSha == "":
			e["operation"], e["content"] = "create", base64.StdEncoding.EncodeToString([]byte(f.Content))
		default:
			e["operation"], e["content"] = "update", base64.StdEncoding.EncodeToString([]byte(f.Content))
		}

		if f.FileSha != "" {
			e["sha"] = f.FileSha
		}

		files = append(files, e)
	}

	var out struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	in := map[string]any{"branch": p.Branch, "message": p.Message, "files": files}

	err := g.c.call(ctx, http.MethodPost, g.base(repo), in, &out)
	if s := StatusCode(err); s == http.StatusNotFound || s == http.StatusMethodNotAllowed {
		return commitEach(ctx, p, func(ctx context.Context, one Proposal) (string, error) {
			return g.commitFile(ctx, repo, one)
		}, func(ctx context.Context, one Proposal) (string, error) {
			return deleteContents(ctx, g.c, g.base(repo)+"/"+escapePath(one.Path), one)
		})
	}

	if err != nil {
		return nil, err
	}

	if out.Commit.SHA == "" {
		return nil, nil
	}

	return []string{out.Commit.SHA}, nil
}

// errProposalPathAndFiles refuses a proposal that names a file both ways.
var errProposalPathAndFiles = errors.New("give either path and content, or files, not both")
