package ichorgo

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// A context's fingerprint (contextFingerprint) and its cluster id (clusterID) are hashes of
// its CA certificate: the apps keep everything they know about a cluster under them (pins,
// colours, names, history, alerts…) and share links name a cluster by its id. Rotating the
// Talos CA (StartTalosCARotation) would change both and make the cluster a stranger to the
// phone. The lineage remembers, for the hashes a rotation produced, the ones the cluster had
// before, so both keep answering the original values for the life of the cluster. It lives
// in the app's data directory (see SetDataDir) and travels in backups
// (FingerprintLineageExport / FingerprintLineageImport).

const fingerprintLineageFile = "fingerprints.json"

// fingerprintLineage is derived hash -> the hash before any rotation (the root).
type fingerprintLineage map[string]string

var lineage struct {
	sync.Mutex
	loaded bool
	roots  fingerprintLineage
}

// resolveFingerprint is the fingerprint or cluster id the apps know hash under: hash itself
// unless a rotation derived it from an earlier one.
func resolveFingerprint(hash string) string {
	lineage.Lock()
	defer lineage.Unlock()

	loadLineageLocked()

	if root, ok := lineage.roots[hash]; ok {
		return root
	}

	return hash
}

// recordFingerprintLineage makes derived answer like previous (which may itself be derived).
// Best effort: with no data directory the lineage lives until the process ends.
func recordFingerprintLineage(derived, previous string) {
	lineage.Lock()
	defer lineage.Unlock()

	loadLineageLocked()

	root := previous
	if r, ok := lineage.roots[previous]; ok {
		root = r
	}

	if derived == "" || derived == root || lineage.roots[derived] == root {
		return
	}

	if lineage.roots == nil {
		lineage.roots = fingerprintLineage{}
	}

	lineage.roots[derived] = root
	saveLineageLocked()
}

// loadLineageLocked reads the file once a data directory is known.
func loadLineageLocked() {
	if lineage.loaded {
		return
	}

	dir, aead := dataStore()
	if dir == "" || aead == nil {
		return
	}

	lineage.loaded = true

	data, err := os.ReadFile(filepath.Join(dir, fingerprintLineageFile))
	if err != nil {
		return
	}

	plain, err := openSealed(aead, data)
	if err != nil {
		return
	}

	saved := fingerprintLineage{}
	if json.Unmarshal(plain, &saved) != nil {
		return
	}

	for derived, root := range saved {
		if _, ok := lineage.roots[derived]; !ok {
			if lineage.roots == nil {
				lineage.roots = fingerprintLineage{}
			}

			lineage.roots[derived] = root
		}
	}
}

func saveLineageLocked() {
	dir, aead := dataStore()
	if dir == "" || aead == nil {
		return
	}

	data, err := json.Marshal(lineage.roots)
	if err != nil {
		return
	}

	_ = writeSealed(filepath.Join(dir, fingerprintLineageFile), data, aead)
}

// FingerprintLineageExport is the lineage as a JSON object {derived: original}, for a backup:
// restored with the talosconfig, it keeps the per-cluster options of a rotated cluster.
// "{}" when no CA was ever rotated from this device.
func FingerprintLineageExport() (out string, err error) {
	defer maskResult(&out, &err)

	lineage.Lock()
	defer lineage.Unlock()

	loadLineageLocked()

	if len(lineage.roots) == 0 {
		return "{}", nil
	}

	return toJSON(lineage.roots)
}

// FingerprintLineageImport merges a backup's lineage (FingerprintLineageExport) into this
// device's, before the restored talosconfig is parsed. Entries already known are kept.
func FingerprintLineageImport(lineageJSON string) (err error) {
	defer maskErr(&err)

	var imported fingerprintLineage
	if err := json.Unmarshal([]byte(lineageJSON), &imported); err != nil {
		return errors.New("the backup's fingerprint lineage is not valid JSON")
	}

	lineage.Lock()
	defer lineage.Unlock()

	loadLineageLocked()

	changed := false

	for derived, root := range imported {
		if derived == "" || root == "" || derived == root {
			continue
		}

		if _, ok := lineage.roots[derived]; ok {
			continue
		}

		if lineage.roots == nil {
			lineage.roots = fingerprintLineage{}
		}

		lineage.roots[derived] = root
		changed = true
	}

	if changed {
		saveLineageLocked()
		configs.reset()
	}

	return nil
}

// resetFingerprintLineage forgets everything, for the tests.
func resetFingerprintLineage() {
	lineage.Lock()
	defer lineage.Unlock()

	lineage.loaded, lineage.roots = false, nil
}
