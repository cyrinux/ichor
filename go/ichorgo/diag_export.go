package ichorgo

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The diagnostics zip: what the user looks at and shares themself with a bug report. It is
// made on the phone, from the ring and what the app hands in, with no cluster call and no
// network, and every member is redacted like the ring (see diag.go).

const diagReadme = `Ichor diagnostics

This zip was made on the phone by its user, from Settings > Support, to go with a bug
report. Nothing was sent anywhere by the app: it leaves the phone only if the user shares it.

Every line is redacted: no secret, no cluster or context name, no address, no namespace and
no object name (names are replaced with neutral ones such as "cluster-1" or "<redacted>").

  app.json       the app's version and build, the OS version, the device model, the locale
                 and whether the core keeps its data
  log.jsonl      the core's diagnostics log, oldest first, one JSON entry per line
                 {at (unix ms), level, component, msg, attrs, err}
  platform.json  the crash and exit records the OS kept for the app ({} when none)
  actions.md     the actions the app made on clusters (outcomes only, names replaced)
`

// diagExportSummary is what DiagExport returns.
type diagExportSummary struct {
	Path    string   `json:"path"`
	Bytes   int64    `json:"bytes"`
	Entries int      `json:"entries"`
	Files   []string `json:"files"`
}

var errDiagDest = errors.New("the diagnostics zip must be written in the app's directories or the temporary directory")

// DiagExport writes the diagnostics zip at destPath and returns {"path", "bytes",
// "entries", "files"}. appJSON is the app's own facts (version, build, OS version, device
// model, locale, DataStoreStatus), platformJSON the OS's crash records it collected ("" or
// "{}" for none); both are redacted. destPath must be under the data directory's parent
// (the app's private directories) or the OS temporary directory.
func DiagExport(destPath, appJSON, platformJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	dest, err := diagDestination(destPath)
	if err != nil {
		return "", err
	}

	lines := diagLog.snapshot()
	members := []struct {
		name string
		data []byte
	}{
		{"README.txt", []byte(diagReadme)},
		{"app.json", diagDocument(appJSON)},
		{"log.jsonl", diagJSONLines(lines)},
		{"platform.json", diagDocument(platformJSON)},
		{"actions.md", []byte(diagActions())},
	}

	summary := diagExportSummary{Path: dest, Entries: len(lines)}

	now := diagClock()
	tmp := dest + ".part"

	if err := writeDiagZip(tmp, func(zw *zip.Writer) error {
		for _, m := range members {
			w, err := zw.CreateHeader(&zip.FileHeader{Name: m.name, Method: zip.Deflate, Modified: now})
			if err != nil {
				return err
			}

			if _, err := w.Write(m.data); err != nil {
				return err
			}

			summary.Files = append(summary.Files, m.name)
		}

		return nil
	}); err != nil {
		_ = os.Remove(tmp) //nolint:errcheck // the error below says what failed

		return "", fmt.Errorf("diagnostics zip: %w", err)
	}

	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("diagnostics zip: %w", err)
	}

	if fi, err := os.Stat(dest); err == nil {
		summary.Bytes = fi.Size()
	}

	return toJSON(summary)
}

// diagJSONLines is the ring as JSON lines, oldest first.
func diagJSONLines(lines [][]byte) []byte {
	if len(lines) == 0 {
		return nil
	}

	return append(bytes.Join(lines, []byte("\n")), '\n')
}

func writeDiagZip(path string, fill func(*zip.Writer) error) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(f)

	return errors.Join(fill(zw), zw.Close(), f.Close())
}

// diagDestination is destPath made absolute, once it is known to be inside the data
// directory's parent or the temporary directory.
func diagDestination(destPath string) (string, error) {
	if strings.TrimSpace(destPath) == "" {
		return "", errDiagDest
	}

	dest, err := filepath.Abs(destPath)
	if err != nil {
		return "", err
	}

	dir := resolvedPath(filepath.Dir(dest))

	roots := []string{os.TempDir()}
	if d := dataDir(); d != "" {
		roots = append(roots, filepath.Dir(d))
	}

	for _, root := range roots {
		if within(resolvedPath(root), dir) {
			return dest, nil
		}
	}

	return "", errDiagDest
}

// resolvedPath is p with its symbolic links resolved (iOS and macOS temp dirs are links),
// or p cleaned when it cannot be.
func resolvedPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}

	return filepath.Clean(p)
}

// within says whether path is root or under it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// diagDocument is a JSON document the app handed in, with its string values redacted; "{}"
// when empty, and the redacted text wrapped in {"invalid_json": …} when it is not JSON.
func diagDocument(doc string) []byte {
	doc = strings.TrimSpace(doc)

	switch {
	case doc == "":
		return []byte("{}")
	case json.Valid([]byte(doc)):
		return []byte(maskJSONStrings(doc, diagRedact))
	default:
		b, _ := json.Marshal(map[string]string{"invalid_json": diagScrub(doc, diagMaxBytes)}) //nolint:errcheck // a string map always encodes

		return b
	}
}

// diagActions is the action audit log for the zip: clusters and nodes renamed cluster-N and
// node-N in order of appearance, namespaces dropped, objects reduced to their kind, the
// parameters and errors scrubbed like the ring.
func diagActions() string {
	entries := auditEntries("", "")
	clusters, nodes := labeler("cluster"), labeler("node")

	out := make([]auditEntry, 0, len(entries))

	for _, e := range entries {
		kind, _, _ := strings.Cut(e.Object, "/")

		out = append(out, auditEntry{
			At: e.At, Cluster: clusters(e.Cluster), Node: nodes(e.Node), Object: kind, Action: e.Action,
			Params: diagScrub(e.Params, diagMaxErr), Outcome: e.Outcome, Error: diagScrub(e.Error, diagMaxErr),
			Demo: e.Demo,
		})
	}

	return auditMarkdown(out)
}

// labeler gives each distinct non-empty name prefix-1, prefix-2... in order of appearance.
func labeler(prefix string) func(string) string {
	seen := map[string]string{}

	return func(name string) string {
		if name == "" {
			return ""
		}

		if l, ok := seen[name]; ok {
			return l
		}

		l := fmt.Sprintf("%s-%d", prefix, len(seen)+1)
		seen[name] = l

		return l
	}
}
