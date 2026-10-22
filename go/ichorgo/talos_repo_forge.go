package ichorgo

import (
	"context"
	"errors"
	"fmt"
	pathpkg "path"
	"strings"
)

// Talos from Git on a repository too big for a snapshot: a Talos directory is usually a few
// small files, so only it is read, through the forge's tree and file calls (TalosRepoFiles),
// or only the files asked for (TalosRepoCompare).

const (
	// talosDirMaxFiles bounds the YAML files read from the Talos directory.
	talosDirMaxFiles = 200
	// talosDirMaxDepth is how many directory levels below the Talos directory are walked.
	talosDirMaxDepth = 2

	noteTalosDirOnly = "Read from the Talos directory only: the repository is too large for a snapshot."
)

var errTalosDirBlank = errors.New("the repository is too large for a snapshot: set its Talos directory in the cluster repository settings, and only that directory is read")

// talosRepoRead is what the Talos features read from repoURL at ref: the snapshot's files and
// commit, or, when the snapshot is refused for its size, the files read reads through the
// forge at that commit, with a note saying so.
type talosRepoRead struct {
	files map[string][]byte
	sha   string
	note  string
}

// talosForgeReader reads the files a Talos feature needs from the forge, at commit sha.
type talosForgeReader func(ctx context.Context, f gitForge, repo gitRepoRef, sha string) (map[string][]byte, error)

func readTalosRepo(hostJSON, repoURL, ref string, read talosForgeReader) (talosRepoRead, error) {
	snap, _, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err == nil {
		return talosRepoRead{files: snap.files, sha: snap.sha}, nil
	}

	if !errors.Is(err, errRepoTooLarge) && !errors.Is(err, errGitForgeArchiveTooLarge) {
		return talosRepoRead{}, err
	}

	h, repo, ref, _, err := gitForgeArgs(hostJSON, repoURL, ref, "")
	if err != nil {
		return talosRepoRead{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitSnapshotTimeout)
	defer cancel()

	f := newGitForge(h)

	sha, err := snapshotCommit(ctx, f, repo, ref)
	if err != nil {
		return talosRepoRead{}, err
	}

	files, err := read(ctx, f, repo, sha)
	if err != nil {
		return talosRepoRead{}, err
	}

	return talosRepoRead{files: files, sha: sha, note: noteTalosDirOnly}, nil
}

// talosDirReader walks dir, at most talosDirMaxDepth levels below it, and reads its YAML
// files (at most talosDirMaxFiles, each under gitForgeMaxFile). A blank dir is refused: the
// whole repository would be walked one directory per request.
func talosDirReader(dir string) talosForgeReader {
	return func(ctx context.Context, f gitForge, repo gitRepoRef, sha string) (map[string][]byte, error) {
		root := strings.Trim(strings.TrimSpace(dir), "/")
		if root == "" {
			return nil, errTalosDirBlank
		}

		files := map[string][]byte{}

		var walk func(d string, depth int) error

		walk = func(d string, depth int) error {
			entries, err := f.Tree(ctx, repo, sha, d)
			if errors.Is(err, errGitForgeNotFound) {
				return fmt.Errorf("%s is not in %s at %.7s", d, repo.Full(), sha)
			}

			if err != nil {
				return err
			}

			for _, e := range entries {
				switch {
				case e.Type == "dir" && depth < talosDirMaxDepth:
					if err := walk(e.Path, depth+1); err != nil {
						return err
					}
				case e.Type == "file" && isYAMLPath(e.Path) && e.Size <= gitForgeMaxFile:
					if len(files) == talosDirMaxFiles {
						return fmt.Errorf("%s holds more than %d YAML files: the app reads at most that many", root, talosDirMaxFiles)
					}

					file, err := f.File(ctx, repo, sha, e.Path)
					if err != nil {
						return err
					}

					files[e.Path] = []byte(file.Content)
				}
			}

			return nil
		}

		return files, walk(root, 0)
	}
}

// talosPathsReader reads the files paths name (an inline talconfig patch's file once); a file
// missing at the commit is left out, for loadTalosPatches to skip it with why.
func talosPathsReader(paths []string) talosForgeReader {
	return func(ctx context.Context, f gitForge, repo gitRepoRef, sha string) (map[string][]byte, error) {
		files := map[string][]byte{}

		for _, p := range paths {
			name, _, _ := strings.Cut(p, talosInlineMark)
			if _, ok := files[name]; ok {
				continue
			}

			file, err := f.File(ctx, repo, sha, name)
			if errors.Is(err, errGitForgeNotFound) {
				continue
			}

			if err != nil {
				return nil, err
			}

			files[name] = []byte(file.Content)
		}

		return files, nil
	}
}

// isYAMLPath tells a .yaml or .yml file.
func isYAMLPath(p string) bool {
	ext := pathpkg.Ext(p)

	return ext == ".yaml" || ext == ".yml"
}
