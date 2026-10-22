package ichorgo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// A repository at one commit, downloaded as one archive and kept on the phone: the whole tree
// for the features that need all of it (find in repository), where browsing reads one
// directory at a time from the forge. Kept in memory (a few) and as the archive downloaded,
// in the data directory (a few more, encrypted like the rest of it), keyed by repository and
// commit.

const (
	gitSnapshotTimeout = gitForgeArchiveTimeout
	// gitSnapshotMemory is how many unpacked snapshots stay in memory.
	gitSnapshotMemory = 3
	// gitSnapshotMemoryBytes bounds their text, all together (the newest is always kept).
	gitSnapshotMemoryBytes = 160 << 20
	// gitSnapshotDiskEntries and gitSnapshotDiskBytes bound the archives kept on disk.
	gitSnapshotDiskEntries = 5
	gitSnapshotDiskBytes   = 200 << 20
	// gitSnapshotMaxText is the largest file kept as text; bigger ones are listed by size.
	gitSnapshotMaxText = 1 << 20
	gitSnapshotDir     = "repo-snapshots"
	gitSnapshotExt     = ".tgz.sealed"
)

var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// repoSnapshot is a repository's tree at sha: the text files' content, every file's size.
type repoSnapshot struct {
	files map[string][]byte
	sizes map[string]int64
	sha   string
	bytes int64 // the archive as downloaded
	text  int64 // the text kept in files
}

type repoSnapshotStatus struct {
	SHA    string `json:"sha"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	Cached bool   `json:"cached"`
}

type cachedSnapshot struct {
	key  string
	snap *repoSnapshot
}

// repoSnapshots holds the unpacked snapshots, the most recently used first.
var repoSnapshots struct {
	sync.Mutex
	list []cachedSnapshot
}

// GitRepoSnapshotPrepare makes repoURL's tree at ref (a branch, tag or commit) ready for the
// whole-repository features: a branch is resolved to its commit, then the archive is read
// from memory, the data directory (encrypted with SetDataDir's key), or the forge with
// hostJSON's token (at most 48 MiB).
// {"sha","files","bytes","cached"}; the demo host answers its sample repository offline.
func GitRepoSnapshotPrepare(hostJSON, repoURL, ref string) (out string, err error) {
	defer maskResult(&out, &err)

	snap, cached, err := loadRepoSnapshot(hostJSON, repoURL, ref)
	if err != nil {
		return "", err
	}

	return toJSON(repoSnapshotStatus{SHA: snap.sha, Files: len(snap.sizes), Bytes: snap.bytes, Cached: cached})
}

// loadRepoSnapshot is the snapshot of repoURL at ref, and whether it was already on the phone.
func loadRepoSnapshot(hostJSON, repoURL, ref string) (*repoSnapshot, bool, error) {
	h, repo, ref, _, err := gitForgeArgs(hostJSON, repoURL, ref, "")
	if err != nil {
		return nil, false, err
	}

	if h.Kind == gitForgeDemo {
		return demoRepoSnapshot(ref), true, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitSnapshotTimeout)
	defer cancel()

	sha, err := snapshotCommit(ctx, newGitForge(h), repo, ref)
	if err != nil {
		return nil, false, err
	}

	key := snapshotKey(h.HostName()+"/"+repo.Full(), sha)

	// A search submitted again while the first still downloads waits for it.
	unlock := lockSnapshotKey(key)
	defer unlock()

	if snap := cachedRepoSnapshot(key); snap != nil {
		return snap, true, nil
	}

	if data := readSnapshotFile(key); data != nil {
		if snap, err := unpackRepoArchive(data, sha); err == nil {
			storeRepoSnapshot(key, snap)

			return snap, true, nil
		}
	}

	data, err := gitForgeArchive(ctx, h, repo, sha)
	if errors.Is(err, errGitForgeNotFound) {
		return nil, false, fmt.Errorf("%s is not in %s", ref, repo.Full())
	}

	if err != nil {
		return nil, false, err
	}

	snap, err := unpackRepoArchive(data, sha)
	if err != nil {
		return nil, false, err
	}

	writeSnapshotFile(key, data)
	storeRepoSnapshot(key, snap)

	return snap, false, nil
}

// snapshotCommit is the full commit ref names (a branch, a tag, a short sha, HEAD), asked of
// the forge: the cache is keyed by commits only, so a ref that moves is never served stale.
func snapshotCommit(ctx context.Context, f gitForge, repo gitRepoRef, ref string) (string, error) {
	if commitSHA.MatchString(ref) {
		return ref, nil
	}

	sha, err := f.RefCommit(ctx, repo, ref)
	if errors.Is(err, errGitForgeNotFound) {
		return "", fmt.Errorf("%s is not in %s", ref, repo.Full())
	}

	if err != nil {
		return "", err
	}

	if sha = strings.ToLower(sha); !commitSHA.MatchString(sha) {
		return "", fmt.Errorf("%s: no commit for %s", repo.Full(), ref)
	}

	return sha, nil
}

// snapshotKey names a snapshot on disk: the repository hashed, then its commit.
func snapshotKey(repo, sha string) string {
	sum := sha256.Sum256([]byte(repo))

	return hex.EncodeToString(sum[:8]) + "-" + sha
}

// errRepoTooLarge refuses a snapshot over the unpack bounds: the Talos features then read
// their directory alone (talos_repo_forge.go).
var errRepoTooLarge = errors.New("the repository is larger than the app unpacks")

// unpackRepoArchive reads a forge's tar.gz streaming: regular files only, the top directory
// the forges wrap the tree in stripped, text files up to 1 MiB kept, bounded like a Flux
// artifact.
func unpackRepoArchive(data []byte, sha string) (*repoSnapshot, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read the repository archive: %w", err)
	}

	snap := &repoSnapshot{files: map[string][]byte{}, sizes: map[string]int64{}, sha: sha, bytes: int64(len(data))}
	tr := tar.NewReader(gz)
	total := int64(0)

	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return snap, nil
		}

		if err != nil {
			return nil, fmt.Errorf("read the repository archive: %w", err)
		}

		if h.Typeflag != tar.TypeReg {
			continue
		}

		// Cleaned from the root: no path climbs out. The first element is the archive's own.
		_, rel, ok := strings.Cut(strings.TrimPrefix(pathpkg.Clean("/"+h.Name), "/"), "/")
		if !ok || rel == "" {
			continue
		}

		total += h.Size
		if len(snap.sizes) >= fluxArtifactMaxFiles || total > fluxArtifactMaxUnpacked {
			return nil, fmt.Errorf("%w (%d files, %d MiB)", errRepoTooLarge, fluxArtifactMaxFiles, fluxArtifactMaxUnpacked>>20)
		}

		snap.sizes[rel] = h.Size
		if h.Size > gitSnapshotMaxText {
			continue
		}

		b, err := io.ReadAll(io.LimitReader(tr, gitSnapshotMaxText+1))
		if err != nil {
			return nil, fmt.Errorf("read the repository archive: %w", err)
		}

		if isTextContent(b) {
			snap.files[rel] = b
			snap.text += int64(len(b))
		}
	}
}

// isTextContent tells a text file: valid UTF-8 with no NUL byte in its first 8 KiB.
func isTextContent(b []byte) bool {
	head := b[:min(len(b), 8<<10)]

	return !bytes.Contains(head, []byte{0}) && utf8.Valid(b)
}

func cachedRepoSnapshot(key string) *repoSnapshot {
	repoSnapshots.Lock()
	defer repoSnapshots.Unlock()

	i := slices.IndexFunc(repoSnapshots.list, func(c cachedSnapshot) bool { return c.key == key })
	if i < 0 {
		return nil
	}

	hit := repoSnapshots.list[i]
	repoSnapshots.list = append([]cachedSnapshot{hit}, slices.Delete(slices.Clone(repoSnapshots.list), i, i+1)...)

	return hit.snap
}

func storeRepoSnapshot(key string, snap *repoSnapshot) {
	repoSnapshots.Lock()
	defer repoSnapshots.Unlock()

	rest := slices.DeleteFunc(slices.Clone(repoSnapshots.list), func(c cachedSnapshot) bool { return c.key == key })
	list := append([]cachedSnapshot{{key, snap}}, rest...)

	n, total := 1, snap.text
	for n < min(len(list), gitSnapshotMemory) && total+list[n].snap.text <= gitSnapshotMemoryBytes {
		total += list[n].snap.text
		n++
	}

	repoSnapshots.list = list[:n]
}

// snapshotLoads holds a lock per snapshot being read, so one download serves every caller.
var snapshotLoads struct {
	sync.Mutex
	keys map[string]*snapshotLoad
}

type snapshotLoad struct {
	sync.Mutex
	waiters int
}

// lockSnapshotKey waits for the other loads of key, then holds it until the returned unlock.
func lockSnapshotKey(key string) func() {
	snapshotLoads.Lock()
	if snapshotLoads.keys == nil {
		snapshotLoads.keys = map[string]*snapshotLoad{}
	}

	l := snapshotLoads.keys[key]
	if l == nil {
		l = &snapshotLoad{}
		snapshotLoads.keys[key] = l
	}

	l.waiters++
	snapshotLoads.Unlock()

	l.Lock()

	return func() {
		l.Unlock()

		snapshotLoads.Lock()
		if l.waiters--; l.waiters == 0 {
			delete(snapshotLoads.keys, key)
		}
		snapshotLoads.Unlock()
	}
}

// snapshotStore is where archives are kept and the key they are sealed with; "" without a
// data directory and its key (nothing is kept on disk then).
func snapshotStore() (string, cipher.AEAD) {
	dir, aead := dataStore()
	if dir == "" || aead == nil {
		return "", nil
	}

	return filepath.Join(dir, gitSnapshotDir), aead
}

// readSnapshotFile is the archive kept for key, nil when none is or it does not open; a hit
// is marked recent.
func readSnapshotFile(key string) []byte {
	dir, aead := snapshotStore()
	if dir == "" {
		return nil
	}

	p := filepath.Join(dir, key+gitSnapshotExt)

	sealed, err := os.ReadFile(p)
	if err != nil {
		return nil
	}

	data, err := openSealed(aead, sealed)
	if err != nil {
		_ = os.Remove(p)

		return nil
	}

	now := time.Now()
	_ = os.Chtimes(p, now, now)

	return data
}

// writeSnapshotFile keeps the archive for key, sealed, then drops the oldest beyond the
// bounds. Best effort: a snapshot that cannot be kept is downloaded again next time.
func writeSnapshotFile(key string, data []byte) {
	dir, aead := snapshotStore()
	if dir == "" || os.MkdirAll(dir, 0o700) != nil {
		return
	}

	if writeSealed(filepath.Join(dir, key+gitSnapshotExt), data, aead) != nil {
		return
	}

	pruneSnapshotFiles(dir, gitSnapshotDiskEntries, gitSnapshotDiskBytes)
}

// pruneSnapshotFiles keeps the most recent archives of dir within maxEntries and maxBytes.
func pruneSnapshotFiles(dir string, maxEntries int, maxBytes int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type kept struct {
		path string
		size int64
		at   time.Time
	}

	var files []kept

	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !strings.HasSuffix(e.Name(), gitSnapshotExt) {
			continue
		}

		files = append(files, kept{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
	}

	slices.SortFunc(files, func(a, b kept) int { return b.at.Compare(a.at) })

	total := int64(0)

	for i, f := range files {
		total += f.size
		if i >= maxEntries || total > maxBytes {
			_ = os.Remove(f.path)
		}
	}
}

// demoRepoSnapshot is the demo repository as a snapshot at ref.
func demoRepoSnapshot(ref string) *repoSnapshot {
	snap := &repoSnapshot{files: map[string][]byte{}, sizes: map[string]int64{}, sha: ref}
	for p, c := range demoGitForgeFiles {
		snap.files[p] = []byte(c)
		snap.sizes[p] = int64(len(c))
		snap.bytes += int64(len(c))
		snap.text += int64(len(c))
	}

	return snap
}
