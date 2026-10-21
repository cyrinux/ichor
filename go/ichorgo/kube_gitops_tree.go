package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// A Flux source's files, read from the artifact the source-controller holds: no Git token.
// The unpacked tree is kept a few minutes, so walking directories fetches the tarball once.

const (
	fluxTreeTTL = 5 * time.Minute
	// fluxTreeCacheMax bounds the unpacked trees kept, all sources together.
	fluxTreeCacheMax = 32 << 20
)

// fluxTreeKinds are the Flux sources whose artifact is a tree of files.
var fluxTreeKinds = []string{"GitRepository", "OCIRepository", "Bucket"}

type fluxTree struct {
	files    map[string][]byte // path relative to the root → content
	size     int64
	revision string
	commit   string // the Git commit, "" for an OCI or Bucket artifact
	at       time.Time
}

var fluxTrees = struct {
	sync.Mutex
	m map[string]*fluxTree
}{m: map[string]*fluxTree{}}

// FluxSourceTree lists the directory dir ("" for the root) of the artifact of the Flux
// source kind namespace/name (a GitRepository, OCIRepository or Bucket: GitOpsSource's
// fluxSource), directories first: {"ref","sha","entries":[{path,type,size}]}, as
// GitForgeTree. kubeServer: see KubePods.
func FluxSourceTree(configYAML, contextName, kubeServer, kind, namespace, name, dir string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	t, ns, nm, dir, err := fluxTreeArgs(configYAML, contextName, kubeServer, kind, namespace, name, dir)
	if err != nil {
		return "", err
	}

	tree, err := readFluxTreeFor(t, kind, ns, nm)
	if err != nil {
		return "", err
	}

	entries := listTreeDir(tree.sizes(), dir)
	if entries == nil {
		return "", fmt.Errorf("%s is not in %s/%s at %s", cmpOrRoot(dir), ns, nm, tree.revision)
	}

	sortGitForgeEntries(entries)
	markSOPSEntries(entries)

	return toJSON(gitForgeTree{Ref: tree.revision, SHA: tree.commit, Entries: entries})
}

// FluxSourceFile reads the file path of the artifact of the Flux source kind namespace/name:
// {"path","sha","size","content","truncated"}, as GitForgeFile; sha is the Git commit.
func FluxSourceFile(configYAML, contextName, kubeServer, kind, namespace, name, filePath string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	t, ns, nm, filePath, err := fluxTreeArgs(configYAML, contextName, kubeServer, kind, namespace, name, filePath)
	if err != nil {
		return "", err
	}

	if filePath == "" {
		return "", errors.New("no file path given")
	}

	tree, err := readFluxTreeFor(t, kind, ns, nm)
	if err != nil {
		return "", err
	}

	content, ok := tree.files[filePath]
	if !ok {
		return "", fmt.Errorf("%s is not in %s/%s at %s", filePath, ns, nm, tree.revision)
	}

	f := gitForgeFile{Path: filePath, SHA: tree.commit, Size: int64(len(content)), Content: string(content)}
	if f.Size > gitForgeMaxFile {
		f.Content, f.Truncated = "", true
	}

	return toJSON(f)
}

func cmpOrRoot(dir string) string {
	if dir == "" {
		return "/"
	}

	return dir
}

func fluxTreeArgs(configYAML, contextName, kubeServer, kind, namespace, name, p string) (kubeTarget, string, string, string, error) {
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if !slices.Contains(fluxTreeKinds, kind) {
		return kubeTarget{}, "", "", "", fmt.Errorf("a Flux %s has no file tree: only a GitRepository, OCIRepository or Bucket", kind)
	}

	if err := validateKubeName("source", namespace, name); err != nil {
		return kubeTarget{}, "", "", "", err
	}

	p = strings.Trim(strings.TrimSpace(p), "/")
	if slices.Contains(strings.Split(p, "/"), "..") {
		return kubeTarget{}, "", "", "", fmt.Errorf("%s leaves the source", p)
	}

	return kubeTarget{configYAML, contextName, kubeServer}, namespace, name, p, nil
}

// readFluxTreeFor is the demo repository in the demo, the source's unpacked artifact
// otherwise.
func readFluxTreeFor(t kubeTarget, kind, namespace, name string) (*fluxTree, error) {
	if isDemoContext(t.config, t.context) {
		return demoFluxTree(), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), fluxDiffTimeout)
	defer cancel()

	return withKubeContext(ctx, t, func(ctx context.Context, k *kubeClient) (*fluxTree, error) {
		return readFluxTree(ctx, k, t.context+"|"+t.server, kind, namespace, name)
	})
}

// readFluxTree unpacks the artifact of the source kind namespace/name, or reuses the tree
// unpacked for the same artifact a few minutes ago.
func readFluxTree(ctx context.Context, k *kubeClient, cluster, kind, namespace, name string) (*fluxTree, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return nil, err
	}

	p, err := fluxPath(groups, kind, namespace, name)
	if err != nil {
		return nil, err
	}

	var src struct {
		Status struct {
			Artifact *fluxArtifact `json:"artifact"`
		} `json:"status"`
	}

	if err := k.get(ctx, p, &src); err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("no Flux %s %s/%s", kind, namespace, name)
		}

		return nil, err
	}

	art := src.Status.Artifact
	if art == nil {
		return nil, errFluxNoArtifact
	}

	key := cluster + "|" + kind + "|" + art.Path + "|" + art.Revision
	if t := cachedFluxTree(key, time.Now()); t != nil {
		return t, nil
	}

	tgz, err := fetchFluxArtifact(ctx, k, art)
	if err != nil {
		return nil, err
	}

	t, err := unpackFluxTree(tgz, art.Revision)
	if err != nil {
		return nil, err
	}

	if kind == "GitRepository" {
		t.commit = fluxCommitOf(art.Revision)
	}

	storeFluxTree(key, t)

	return t, nil
}

// unpackFluxTree unpacks a tarball through untarFluxArtifact (regular files only, paths kept
// inside, bounded) into a map of the files.
func unpackFluxTree(tgz []byte, revision string) (*fluxTree, error) {
	fsys := filesys.MakeFsInMemory()
	if err := untarFluxArtifact(tgz, fsys, fluxSourceRoot); err != nil {
		return nil, err
	}

	t := &fluxTree{files: map[string][]byte{}, revision: revision}

	err := fsys.Walk(fluxSourceRoot, func(p string, info fs.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		b, err := fsys.ReadFile(p)
		if err != nil {
			return err
		}

		t.files[strings.TrimPrefix(p, fluxSourceRoot+"/")] = b
		t.size += int64(len(b))

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}

	return t, nil
}

func (t *fluxTree) sizes() map[string]int64 {
	out := make(map[string]int64, len(t.files))
	for p, b := range t.files {
		out[p] = int64(len(b))
	}

	return out
}

func cachedFluxTree(key string, now time.Time) *fluxTree {
	fluxTrees.Lock()
	defer fluxTrees.Unlock()

	t := fluxTrees.m[key]
	if t == nil || now.Sub(t.at) > fluxTreeTTL {
		return nil
	}

	return t
}

// storeFluxTree keeps t, dropping expired trees then the oldest ones past fluxTreeCacheMax.
// A tree larger than the whole cache is not kept.
func storeFluxTree(key string, t *fluxTree) {
	if t.size > fluxTreeCacheMax {
		return
	}

	fluxTrees.Lock()
	defer fluxTrees.Unlock()

	now := time.Now()
	t.at = now
	fluxTrees.m[key] = t

	total := int64(0)

	for k, c := range fluxTrees.m {
		if now.Sub(c.at) > fluxTreeTTL {
			delete(fluxTrees.m, k)

			continue
		}

		total += c.size
	}

	keys := slices.SortedFunc(maps.Keys(fluxTrees.m), func(a, b string) int {
		return fluxTrees.m[a].at.Compare(fluxTrees.m[b].at)
	})

	for _, k := range keys {
		if total <= fluxTreeCacheMax || k == key {
			break
		}

		total -= fluxTrees.m[k].size
		delete(fluxTrees.m, k)
	}
}

// listTreeDir lists the direct children of dir among files (path → size): files with their
// size, directories once. nil when dir holds nothing.
func listTreeDir(files map[string]int64, dir string) []gitForgeEntry {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}

	seen := map[string]bool{}

	var entries []gitForgeEntry

	for p, size := range files {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok {
			continue
		}

		if name, _, isDir := strings.Cut(rest, "/"); isDir {
			if !seen[prefix+name] {
				seen[prefix+name] = true
				entries = append(entries, gitForgeEntry{Path: prefix + name, Type: "dir"})
			}

			continue
		}

		entries = append(entries, gitForgeEntry{Path: p, Type: "file", Size: size})
	}

	return entries
}
