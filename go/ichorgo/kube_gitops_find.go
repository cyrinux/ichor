package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"
)

// GitOpsSourceFind: from a drifted resource of an Argo CD or Flux diff to the file in Git that
// declares it. The files come from the Flux artifact (no token) or the Git host (token), only
// under the source's path, and the search is bounded so a monorepo does not stall the diff.

const (
	gitopsFindMaxFiles = 200
	gitopsFindMaxFile  = 1 << 20
	gitopsFindMaxDirs  = 100
	gitopsFindTimeout  = 10 * time.Second

	gitopsFindHelmNote = "rendered by Helm; open the chart's templates"
)

type gitopsFound struct {
	Matches  []manifestMatch `json:"matches"`
	Searched int             `json:"searched"`
	Note     string          `json:"note"`
}

// sourceFiles are the files read under a source's path; cut tells the bounds were reached.
type sourceFiles struct {
	files map[string][]byte
	cut   bool
	helm  bool
}

// GitOpsSourceFind finds the files declaring the resource resKind resNamespace/resName among
// the files of source sourceIndex (decimal, GitOpsSource's sources) of the app tool kind
// namespace/name: {"matches":[{"path","line","documentIndex"}],"searched","note"}. line is the
// line of metadata.name in the source file. hostJSON (GitForgeTree's) is needed when the files
// are not read from a Flux artifact. A Helm source has no match and says why in note.
// kubeServer: see KubePods.
func GitOpsSourceFind(configYAML, contextName, kubeServer, hostJSON, tool, kind, namespace, name, sourceIndex, resKind, resNamespace, resName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	resNamespace, resName = privacy.reveal(strings.TrimSpace(resNamespace)), privacy.reveal(strings.TrimSpace(resName))
	resKind = strings.TrimSpace(resKind)

	index, err := gitopsFindArgs(tool, kind, namespace, name, sourceIndex, resKind, resName)
	if err != nil {
		return "", err
	}

	q := manifestQuery{kind: resKind, namespace: resNamespace, name: resName}
	target := kubeTarget{configYAML, contextName, kubeServer}

	if isDemoContext(configYAML, contextName) {
		src, err := pickGitOpsSource(demoGitOpsSource(tool), index)
		if err != nil {
			return "", err
		}

		found, err := findInSource(context.Background(), src, q, func(context.Context) (sourceFiles, error) {
			return treeFilesUnder(demoFluxTree().files, src.Path), nil
		})
		if err != nil {
			return "", err
		}

		return toJSON(found)
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitopsFindTimeout)
	defer cancel()

	found, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (gitopsFound, error) {
		var sources gitopsSources

		var err error
		if tool == "argocd" {
			sources, err = readArgoSources(ctx, k, namespace, name)
		} else {
			sources, err = readFluxSources(ctx, k, kind, namespace, name)
		}

		if err != nil {
			return gitopsFound{}, err
		}

		src, err := pickGitOpsSource(sources, index)
		if err != nil {
			return gitopsFound{}, err
		}

		return findInSource(ctx, src, q, func(ctx context.Context) (sourceFiles, error) {
			return readSourceFiles(ctx, k, target, hostJSON, src)
		})
	})
	if err != nil {
		return "", err
	}

	return toJSON(found)
}

type manifestQuery struct{ kind, namespace, name string }

func gitopsFindArgs(tool, kind, namespace, name, sourceIndex, resKind, resName string) (int, error) {
	switch {
	case tool == "argocd" && kind == "Application":
	case tool == "flux" && (kind == "Kustomization" || kind == "HelmRelease"):
	default:
		return 0, errGitOpsSourceKind
	}

	if err := validateKubeName("app", namespace, name); err != nil {
		return 0, err
	}

	if resKind == "" || resName == "" {
		return 0, errors.New("no resource kind and name given")
	}

	index, err := strconv.Atoi(cmp.Or(strings.TrimSpace(sourceIndex), "0"))
	if err != nil || index < 0 {
		return 0, fmt.Errorf("invalid source index %q", sourceIndex)
	}

	return index, nil
}

func pickGitOpsSource(sources gitopsSources, index int) (gitopsSource, error) {
	if index >= len(sources.Sources) {
		return gitopsSource{}, fmt.Errorf("the app has no source %d", index)
	}

	return sources.Sources[index], nil
}

// findInSource reads the source's files with read and matches the resource in them.
func findInSource(ctx context.Context, src gitopsSource, q manifestQuery, read func(context.Context) (sourceFiles, error)) (gitopsFound, error) {
	if src.Kind == gitopsKindHelm || src.Kind == gitopsKindChart || src.Kind == gitopsKindValues {
		return gitopsFound{Matches: []manifestMatch{}, Note: gitopsFindHelmNote}, nil
	}

	if !src.FluxArtifact && !src.Browsable {
		return gitopsFound{Matches: []manifestMatch{}, Note: "the files of this source cannot be read"}, nil
	}

	sf, err := read(ctx)
	if err != nil {
		return gitopsFound{}, err
	}

	if sf.helm {
		return gitopsFound{Matches: []manifestMatch{}, Note: gitopsFindHelmNote}, nil
	}

	ns := q.namespace
	if src.setsNamespace {
		ns = ""
	}

	matches := findManifest(sf.files, q.kind, ns, q.name, src.namePrefix, src.nameSuffix)

	return gitopsFound{Matches: matches, Searched: len(sf.files), Note: gitopsFindNote(matches, sf.cut, len(sf.files), src.Path, q)}, nil
}

func gitopsFindNote(matches []manifestMatch, cut bool, read int, dir string, q manifestQuery) string {
	if slices.ContainsFunc(matches, func(m manifestMatch) bool { return m.renamed }) {
		return "the name is rewritten by kustomize (namePrefix/nameSuffix): matched without them"
	}

	if len(matches) > 0 {
		return ""
	}

	if cut {
		return fmt.Sprintf("no match in the %d files read under %s: the search stops at %d files, %d folders or %s",
			read, cmpOrRoot(dir), gitopsFindMaxFiles, gitopsFindMaxDirs, gitopsFindTimeout)
	}

	return fmt.Sprintf("no file under %s declares %s %s: it may be generated (Helm, a kustomize generator or a patch)", cmpOrRoot(dir), q.kind, q.name)
}

// readSourceFiles reads the files under src.Path from its Flux artifact, or else from the
// Git host with hostJSON's token.
func readSourceFiles(ctx context.Context, k *kubeClient, target kubeTarget, hostJSON string, src gitopsSource) (sourceFiles, error) {
	if src.FluxArtifact && src.FluxSource != nil && slices.Contains(fluxTreeKinds, src.FluxSource.Kind) {
		fs := src.FluxSource

		tree, err := readFluxTree(ctx, k, target.context+"|"+target.server, fs.Kind, fs.Namespace, fs.Name)
		if err != nil {
			return sourceFiles{}, err
		}

		return treeFilesUnder(tree.files, src.Path), nil
	}

	if strings.TrimSpace(hostJSON) == "" {
		return sourceFiles{}, fmt.Errorf("add a Git host for %s in Settings → Git hosts to search its files", cmp.Or(src.WebURL, src.RepoURL))
	}

	r, err := newSourceForge(hostJSON, src)
	if err != nil {
		return sourceFiles{}, err
	}

	return walkForgeFiles(ctx, r, src.Path)
}

// treeFilesUnder are the manifest files under dir among files, in path order, within the
// bounds.
func treeFilesUnder(files map[string][]byte, dir string) sourceFiles {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}

	out := sourceFiles{files: map[string][]byte{}}
	if _, ok := files[prefix+"Chart.yaml"]; ok {
		out.helm = true

		return out
	}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		if !strings.HasPrefix(p, prefix) || !isManifestFile(p) && !isKustomizationFile(p) || len(files[p]) > gitopsFindMaxFile {
			continue
		}

		if len(out.files) == gitopsFindMaxFiles {
			out.cut = true

			break
		}

		out.files[p] = files[p]
	}

	return out
}

// sourceForge reads one repository at one ref: the demo's or a Git host's.
type sourceForge struct {
	tree func(ctx context.Context, dir string) ([]gitForgeEntry, error)
	file func(ctx context.Context, p string) (gitForgeFile, error)
}

// newSourceForge checks hostJSON against the source's repository (gitForgeArgs: the token only
// goes to its own host) and reads at the commit the cluster runs, or the tracked ref.
func newSourceForge(hostJSON string, src gitopsSource) (sourceForge, error) {
	h, repo, ref, _, err := gitForgeArgs(hostJSON, src.RepoURL, cmp.Or(src.Revision, src.Tracked), src.Path)
	if err != nil {
		return sourceForge{}, err
	}

	if h.Kind == gitForgeDemo {
		return sourceForge{
			tree: func(_ context.Context, dir string) ([]gitForgeEntry, error) { return demoGitForgeTree(dir) },
			file: func(_ context.Context, p string) (gitForgeFile, error) { return demoGitForgeFile(p) },
		}, nil
	}

	f := newGitForge(h)

	return sourceForge{
		tree: func(ctx context.Context, dir string) ([]gitForgeEntry, error) { return f.tree(ctx, repo, ref, dir) },
		file: func(ctx context.Context, p string) (gitForgeFile, error) { return f.file(ctx, repo, ref, p) },
	}, nil
}

// walkForgeFiles lists dir and its subdirectories breadth first and reads the manifest files,
// within the bounds; running out of time ends the walk with what was read.
func walkForgeFiles(ctx context.Context, r sourceForge, dir string) (sourceFiles, error) {
	out := sourceFiles{files: map[string][]byte{}}

	var paths []string

	queue := []string{dir}
	for dirs := 0; len(queue) > 0 && !out.cut; dirs++ {
		if dirs == gitopsFindMaxDirs || ctx.Err() != nil {
			out.cut = true

			break
		}

		d := queue[0]
		queue = queue[1:]

		entries, err := r.tree(ctx, d)
		if err != nil {
			if ctx.Err() != nil {
				out.cut = true

				break
			}

			if errors.Is(err, errGitForgeNotFound) {
				return sourceFiles{}, fmt.Errorf("%s is not in the repository", cmpOrRoot(d))
			}

			return sourceFiles{}, err
		}

		sortGitForgeEntries(entries)

		if d == dir && slices.ContainsFunc(entries, func(e gitForgeEntry) bool { return path.Base(e.Path) == "Chart.yaml" && e.Type == "file" }) {
			out.helm = true

			return out, nil
		}

		for _, e := range entries {
			switch {
			case e.Type == "dir":
				queue = append(queue, e.Path)
			case e.Size > gitopsFindMaxFile || !isManifestFile(e.Path) && !isKustomizationFile(e.Path):
			case len(paths) == gitopsFindMaxFiles:
				out.cut = true
			default:
				paths = append(paths, e.Path)
			}
		}
	}

	for _, p := range paths {
		f, err := r.file(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				out.cut = true

				break
			}

			if errors.Is(err, errGitForgeNotFound) || errors.Is(err, errGitForgeTooLarge) {
				continue
			}

			return sourceFiles{}, err
		}

		if f.Truncated || len(f.Content) > gitopsFindMaxFile {
			continue
		}

		out.files[p] = []byte(f.Content)
	}

	return out, nil
}
