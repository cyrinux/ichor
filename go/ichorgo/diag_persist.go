package ichorgo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Opt-in persistence of the diagnostics ring (SetDiagPersist): every entry is also appended
// to <dataDir>/diag-log.jsonl, so the lines written before a crash are in the next export.
// The file is plain text, not sealed like the audit log: every line is already redacted when
// it is written (see diag.go), and the directory is the app's private, no-backup one. At
// diagMaxBytes it rotates to diag-log.1.jsonl; one previous generation is kept. Without a
// data directory the ring stays in memory, and that is not an error.

const (
	diagFile    = "diag-log.jsonl"
	diagOldFile = "diag-log.1.jsonl"
)

// persistState is the ring's persistence: on is the setting, dir the directory the ring is
// written to now ("" while it is not), size the current file's length.
type persistState struct {
	on   bool
	dir  string
	size int64
}

// SetDiagPersist keeps the diagnostics ring in the data directory (on) or in memory only
// (off, the default; the files are deleted). Turning it on loads the lines kept by the
// previous runs into the ring, oldest first.
func SetDiagPersist(on bool) (err error) {
	defer maskErr(&err)

	return diagLog.setPersist(on, dataDir())
}

func (r *diagRing) setPersist(on bool, dir string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if on {
		r.on = true
		r.syncLocked(dir)

		return nil
	}

	r.on = false

	var errs []error
	for _, d := range []string{r.dir, dir} {
		if d != "" {
			errs = append(errs, removeDiagFiles(d))
		}
	}

	r.dir, r.size = "", 0

	return errors.Join(errs...)
}

// diagDataDirChanged follows SetDataDir: the ring moves to the new directory, seeded from
// its files, or back to memory only.
func diagDataDirChanged(dir string) {
	diagLog.mu.Lock()
	defer diagLog.mu.Unlock()

	diagLog.syncLocked(dir)
}

// syncLocked makes the ring persist to dir when persistence is on. On a new directory the
// ring becomes that directory's lines followed by the ones in memory, which are written out.
func (r *diagRing) syncLocked(dir string) {
	if !r.on {
		r.dir, r.size = "", 0

		return
	}

	if dir == r.dir {
		return
	}

	r.dir, r.size = dir, 0
	if dir == "" {
		return
	}

	current := r.lines
	r.lines, r.bytes = nil, 0

	for _, name := range []string{diagOldFile, diagFile} {
		for _, line := range readDiagFile(filepath.Join(dir, name)) {
			r.pushLocked(line)
		}
	}

	if fi, err := os.Stat(filepath.Join(dir, diagFile)); err == nil {
		r.size = fi.Size()
	}

	for _, line := range current {
		r.pushLocked(line)
		r.persistLocked(line)
	}
}

// readDiagFile is a persisted file's valid lines, oldest first (none when it is missing).
func readDiagFile(path string) [][]byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var out [][]byte

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), diagMaxBytes)

	for sc.Scan() {
		if line := sc.Bytes(); json.Valid(line) {
			out = append(out, bytes.Clone(line))
		}
	}

	return out
}

// persistLocked appends line to the current file, rotating it first when it would grow past
// diagMaxBytes. Best effort: a line that cannot be written stays in memory only.
func (r *diagRing) persistLocked(line []byte) {
	if r.dir == "" {
		return
	}

	path := filepath.Join(r.dir, diagFile)

	if r.size > 0 && r.size+int64(len(line))+1 > diagMaxBytes {
		if os.Rename(path, filepath.Join(r.dir, diagOldFile)) != nil {
			return
		}

		r.size = 0
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}

	n, _ := f.Write(append(bytes.Clone(line), '\n')) //nolint:errcheck // counted below
	r.size += int64(n)

	_ = f.Close() //nolint:errcheck // best effort
}

// clearFilesLocked deletes the persisted lines, keeping persistence on.
func (r *diagRing) clearFilesLocked() {
	if r.dir != "" {
		_ = removeDiagFiles(r.dir) //nolint:errcheck // best effort
		r.size = 0
	}
}

func removeDiagFiles(dir string) error {
	var errs []error

	for _, name := range []string{diagFile, diagOldFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
