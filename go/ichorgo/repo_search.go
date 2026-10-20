package ichorgo

import (
	"errors"
	"slices"
	"strings"
	"unicode/utf8"
)

// Find in repository: every line of a repository's text files holding a word, from its
// snapshot (or a Flux source's artifact). Only the hits cross to the apps, never the files.

const (
	gitSearchDefaultHits = 200
	gitSearchMaxHits     = 1000
	// gitSearchLineMax bounds a hit's text, in characters.
	gitSearchLineMax = 200
)

var errGitSearchEmpty = errors.New("nothing to search for")

type repoSearchHit struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type repoSearchResult struct {
	SHA       string          `json:"sha"`
	Hits      []repoSearchHit `json:"hits"`
	Searched  int             `json:"searched"`
	Truncated bool            `json:"truncated"`
}

// GitRepoSearch finds query (case-insensitive) in repoURL's tree at ref, one hit per line, at
// most maxHits (0: 200, 1000 at most): {"sha","hits":[{"path","line","text"}],"searched",
// "truncated"}. The tree is GitRepoSnapshotPrepare's (downloaded once per commit). Files over
// 1 MiB and binaries are skipped; in a SOPS file only the keys are searched.
func GitRepoSearch(hostJSON, repoURL, ref, query string, maxHits int) (out string, err error) {
	defer maskResult(&out, &err)

	query = strings.TrimSpace(query)
	if query == "" {
		return "", errGitSearchEmpty
	}

	snap, _, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err != nil {
		return "", err
	}

	return toJSON(searchRepoFiles(snap.files, snap.sha, query, maxHits))
}

// FluxSourceSearch is GitRepoSearch over the artifact of the Flux source kind namespace/name
// (no token): sha is its Git commit. kubeServer: see KubePods.
func FluxSourceSearch(configYAML, contextName, kubeServer, kind, namespace, name, query string, maxHits int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	query = strings.TrimSpace(query)
	if query == "" {
		return "", errGitSearchEmpty
	}

	t, ns, nm, _, err := fluxTreeArgs(configYAML, contextName, kubeServer, kind, namespace, name, "")
	if err != nil {
		return "", err
	}

	tree, err := readFluxTreeFor(t, kind, ns, nm)
	if err != nil {
		return "", err
	}

	return toJSON(searchRepoFiles(tree.files, tree.commit, query, maxHits))
}

// searchRepoFiles searches query in files (by path), paths in order.
func searchRepoFiles(files map[string][]byte, sha, query string, maxHits int) repoSearchResult {
	if maxHits <= 0 {
		maxHits = gitSearchDefaultHits
	}

	maxHits = min(maxHits, gitSearchMaxHits)
	needle := strings.ToLower(query)
	res := repoSearchResult{SHA: sha, Hits: []repoSearchHit{}}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}

	slices.Sort(paths)

	for _, p := range paths {
		content := files[p]
		if len(content) > gitSnapshotMaxText {
			continue
		}

		res.Searched++
		sops := isSOPSPath(p)

		for i, line := range strings.Split(string(content), "\n") {
			hay := line
			if sops {
				hay, _, _ = strings.Cut(line, ":")
			}

			if !strings.Contains(strings.ToLower(hay), needle) {
				continue
			}

			if len(res.Hits) == maxHits {
				res.Truncated = true

				return res
			}

			res.Hits = append(res.Hits, repoSearchHit{Path: p, Line: i + 1, Text: clipLine(strings.TrimSpace(line))})
		}
	}

	return res
}

// isSOPSPath tells a SOPS-encrypted file by its name, as .sops.yaml creation rules usually do.
func isSOPSPath(p string) bool {
	base := strings.ToLower(p[strings.LastIndex(p, "/")+1:])
	for _, ext := range []string{".sops.yaml", ".sops.yml", ".sops.json", ".sops.env", ".enc.yaml", ".enc.yml", ".enc.json"} {
		if strings.HasSuffix(base, ext) {
			return true
		}
	}

	return false
}

// clipLine cuts s to gitSearchLineMax characters, with an ellipsis.
func clipLine(s string) string {
	if utf8.RuneCountInString(s) <= gitSearchLineMax {
		return s
	}

	return string([]rune(s)[:gitSearchLineMax]) + "…"
}
