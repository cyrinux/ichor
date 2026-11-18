package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/siderolabs/talos/pkg/machinery/api/machine"
)

// A read-only file browser on a node, like talosctl ls and talosctl read (os:reader): one
// directory's entries, and one file's text, size-capped.

const (
	// nodeFilesTimeout bounds one listing or one read.
	nodeFilesTimeout = 20 * time.Second
	// nodeFilesMaxEntries caps a directory's listing (a pod log directory on a busy node).
	nodeFilesMaxEntries = 2000
	// nodeFileMaxBytes caps a read: the text crosses gomobile as one JSON string.
	nodeFileMaxBytes = 512 << 10
	// nodeFileSniffBytes is how much of a file decides whether it is text.
	nodeFileSniffBytes = 8 << 10

	nodeFileTypeFile = "file"
	nodeFileTypeDir  = "dir"
	nodeFileTypeLink = "link"

	// machineConfigPath is the node's machine config: shown with its secrets redacted.
	machineConfigPath = "/system/state/config.yaml"
	nodeStatePrefix   = "/system/state/"
)

// nodeFileRoots are the starting points the first screen offers: where a log, the config
// or a network table usually is. Any other absolute path can be typed.
var nodeFileRoots = []string{"/var/log", "/system/state", "/etc", "/proc/net", "/run"}

// nodeDirListing is one directory: directories first, then by name.
type nodeDirListing struct {
	Dir     string          `json:"dir"`
	Roots   []string        `json:"roots"`
	Entries []nodeFileEntry `json:"entries"`
	// Truncated says the listing stopped at nodeFilesMaxEntries.
	Truncated bool `json:"truncated"`
}

type nodeFileEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // file, dir or link
	Size int64  `json:"size"`
	// Modified is RFC 3339, "" when the node does not say.
	Modified string `json:"modified"`
	// Target is a link's target, "" otherwise.
	Target string `json:"target"`
}

// nodeFileText is one file: its text up to nodeFileMaxBytes, or binary with no text.
type nodeFileText struct {
	Path string `json:"path"`
	// Size is the bytes read (a /proc file reports size 0 in its listing).
	Size      int64  `json:"size"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Binary    bool   `json:"binary"`
	// Redacted says secrets were hidden (the machine config, files under /system/state).
	Redacted bool `json:"redacted"`
}

// NodeFiles lists the entries directly below dir on node (os:reader), as JSON
// {"dir","roots","entries":[{"name","path","type","size","modified","target"}],"truncated"}:
// directories first, then by name, at most nodeFilesMaxEntries. dir "" lists /var/log.
func NodeFiles(configYAML, contextName, node, dir string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	dir, err = cleanNodePath(cmpOr(dir, nodeFileRoots[0]))
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		if _, err := demoRead("NodeMounts", configYAML, contextName, node); err != nil {
			return "", err
		}

		listing, err := demoNodeFiles(dir)
		if err != nil {
			return "", err
		}

		return toJSON(listing)
	}

	return withNodeSession(configYAML, contextName, node, nodeFilesTimeout, func(ctx context.Context, s *session) (string, error) {
		listing, err := listNodeDir(ctx, s, dir)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		return toJSON(listing)
	})
}

// NodeFileRead reads path on node (os:reader) as JSON {"path","size","text","truncated",
// "binary","redacted"}: at most nodeFileMaxBytes, no text when the start of the file is not
// UTF-8 text, the machine config's secrets redacted.
func NodeFileRead(configYAML, contextName, node, filePath string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	filePath, err = cleanNodePath(filePath)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		if _, err := demoRead("NodeMounts", configYAML, contextName, node); err != nil {
			return "", err
		}

		f, err := demoNodeFileRead(filePath)
		if err != nil {
			return "", err
		}

		return toJSON(f)
	}

	return withNodeSession(configYAML, contextName, node, nodeFilesTimeout, func(ctx context.Context, s *session) (string, error) {
		r, err := s.client.Read(ctx, filePath)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		defer r.Close() //nolint:errcheck

		f, err := readNodeFile(filePath, r)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		return toJSON(f)
	})
}

// cleanNodePath is p cleaned; it must be absolute and must not climb with "..".
func cleanNodePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q is not an absolute path", p)
	}

	if slices.Contains(strings.Split(p, "/"), "..") {
		return "", fmt.Errorf("%q climbs with ..: give the path it means", p)
	}

	return path.Clean(p), nil
}

// listNodeDir lists dir's direct entries; an entry the node could not stat is left out.
func listNodeDir(ctx context.Context, s *session, dir string) (nodeDirListing, error) {
	stream, err := s.client.LS(ctx, &machine.ListRequest{Root: dir, Recurse: false})
	if err != nil {
		return nodeDirListing{}, err
	}

	var infos []*machine.FileInfo

	for {
		info, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nodeDirListing{}, err
		}

		if e := metaError(info.GetMetadata()); e != "" {
			return nodeDirListing{}, errors.New(e)
		}

		infos = append(infos, info)
	}

	return buildNodeDirListing(dir, infos), nil
}

// buildNodeDirListing turns LS answers into the listing: the root itself and entries
// deeper than one level are skipped, directories come first.
func buildNodeDirListing(dir string, infos []*machine.FileInfo) nodeDirListing {
	out := nodeDirListing{Dir: dir, Roots: nodeFileRoots, Entries: []nodeFileEntry{}}

	for _, info := range infos {
		if info.GetError() != "" {
			continue
		}

		full := path.Clean(info.GetName())
		if full == dir || info.GetRelativeName() == "." {
			continue
		}

		name := path.Base(full)
		if path.Dir(full) != dir {
			continue
		}

		if len(out.Entries) == nodeFilesMaxEntries {
			out.Truncated = true

			break
		}

		out.Entries = append(out.Entries, nodeEntry(dir, name, info))
	}

	slices.SortFunc(out.Entries, compareNodeEntries)

	return out
}

// compareNodeEntries orders directories first, then by name.
func compareNodeEntries(a, b nodeFileEntry) int {
	if (a.Type == nodeFileTypeDir) != (b.Type == nodeFileTypeDir) {
		if a.Type == nodeFileTypeDir {
			return -1
		}

		return 1
	}

	return strings.Compare(a.Name, b.Name)
}

func nodeEntry(dir, name string, info *machine.FileInfo) nodeFileEntry {
	e := nodeFileEntry{Name: name, Path: path.Join(dir, name), Type: nodeFileTypeFile, Size: info.GetSize()}

	switch {
	case info.GetIsDir():
		e.Type, e.Size = nodeFileTypeDir, 0
	case fs.FileMode(info.GetMode())&fs.ModeSymlink != 0 || info.GetLink() != "":
		e.Type, e.Target = nodeFileTypeLink, info.GetLink()
	}

	if m := info.GetModified(); m > 0 {
		e.Modified = time.Unix(m, 0).UTC().Format(time.RFC3339)
	}

	return e
}

// readNodeFile reads r up to nodeFileMaxBytes (one byte more tells truncated), never the
// whole of a large file, and decides text or binary on its first nodeFileSniffBytes.
func readNodeFile(filePath string, r io.Reader) (nodeFileText, error) {
	data, err := io.ReadAll(io.LimitReader(r, nodeFileMaxBytes+1))
	if err != nil {
		return nodeFileText{}, err
	}

	out := nodeFileText{Path: filePath}

	if len(data) > nodeFileMaxBytes {
		data, out.Truncated = data[:nodeFileMaxBytes], true
	}

	out.Size = int64(len(data))

	if !looksLikeText(data) {
		out.Binary = true

		return out, nil
	}

	// A cut can split a rune: drop the broken tail rather than send invalid UTF-8.
	text := strings.ToValidUTF8(string(data), "")
	out.Text, out.Redacted = redactNodeFile(filePath, text)

	return out, nil
}

// looksLikeText says the start of data holds no NUL byte and is UTF-8 (a rune cut at the
// sniff boundary does not count against it).
func looksLikeText(data []byte) bool {
	head := data[:min(len(data), nodeFileSniffBytes)]
	if slices.Contains(head, 0) {
		return false
	}

	for i := 0; i < len(head); {
		r, size := utf8.DecodeRune(head[i:])
		if r == utf8.RuneError && size <= 1 {
			return len(head)-i < utf8.UTFMax && len(head) == nodeFileSniffBytes
		}

		i += size
	}

	return true
}

// redactNodeFile hides secrets before the text leaves the core: the machine config through
// its own redaction (credential-looking strings when it does not parse), and every other
// file under /system/state by pattern.
func redactNodeFile(filePath, text string) (string, bool) {
	switch {
	case filePath == machineConfigPath:
		if out, err := redactConfig(text); err == nil {
			return out, true
		}

		return redactSecrets(text), true
	case strings.HasPrefix(filePath, nodeStatePrefix):
		return redactSecrets(text), true
	}

	return text, false
}
