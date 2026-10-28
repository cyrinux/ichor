package ichorgo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
)

// A node that is down cannot say its hostname or role, so the overview gives it the ones it
// last answered with instead of only its address. They are kept per cluster (context
// fingerprint) in a file of the app's data directory (see SetDataDir), on this device only,
// encrypted with a key the app keeps in its Keystore or Keychain: names and roles, no other
// cluster data. Clusters gone from the talosconfig and nodes no longer targeted are forgotten.

const nodeNamesFile = "node-names.json"

type knownNode struct {
	Hostname string `json:"hostname,omitempty"`
	Role     string `json:"role,omitempty"`
}

// knownNodes is context fingerprint -> node address -> its last known names.
type knownNodes map[string]map[string]knownNode

var nodeNames struct {
	sync.Mutex
	dir  string
	aead cipher.AEAD
	// reason is why the last SetDataDir left the store off ("" when it is on).
	reason string
}

// dataKeySize is the length of the key SetDataDir takes: AES-256.
const dataKeySize = 32

var (
	errDataDirEmpty = errors.New("no data directory")
	errDataKeySize  = errors.New("data key must be 32 bytes")
)

// SetDataDir sets the private directory where the core keeps what it remembers across
// launches (node names, the action audit log), and the 32-byte key that encrypts it (AES-256-GCM), which the app
// keeps in its Keystore or Keychain. Call it once at startup, before any other call; until
// then, or with an empty directory or a key of another size, nothing is remembered, and the
// error says why (DataStoreStatus keeps it for the Support screen).
func SetDataDir(dir string, key []byte) (err error) {
	defer maskErr(&err)

	nodeNames.Lock()
	defer nodeNames.Unlock()

	nodeNames.dir, nodeNames.aead = "", nil

	defer func() {
		nodeNames.reason = ""
		if err != nil {
			nodeNames.reason = err.Error()
		}
	}()

	switch {
	case dir == "":
		return errDataDirEmpty
	case len(key) != dataKeySize:
		return fmt.Errorf("%w, got %d", errDataKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("data key: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("data cipher: %w", err)
	}

	nodeNames.dir, nodeNames.aead = dir, aead

	return nil
}

// dataStoreStatus is what DataStoreStatus reports: whether the core remembers anything, the
// last element of its directory, and why not when it does not.
type dataStoreStatus struct {
	Persisting bool   `json:"persisting"`
	Dir        string `json:"dir"`
	Reason     string `json:"reason"`
}

// DataStoreStatus says whether the action audit log and the node names are being saved (the
// last SetDataDir), for the apps' Support screen: {"persisting", "dir", "reason"}. The
// directory is reduced to its last element, so no device path leaves the core.
func DataStoreStatus() (out string, err error) {
	defer maskResult(&out, &err)

	nodeNames.Lock()
	status := dataStoreStatus{Persisting: nodeNames.aead != nil, Reason: nodeNames.reason}
	if nodeNames.dir != "" {
		status.Dir = filepath.Base(nodeNames.dir)
	}
	nodeNames.Unlock()

	return toJSON(status)
}

// dataDir is the directory given to SetDataDir ("" when there is none).
func dataDir() string {
	nodeNames.Lock()
	defer nodeNames.Unlock()

	return nodeNames.dir
}

// dataStore is the directory and cipher given to SetDataDir (nil cipher when there is none).
func dataStore() (string, cipher.AEAD) {
	nodeNames.Lock()
	defer nodeNames.Unlock()

	return nodeNames.dir, nodeNames.aead
}

// rememberNodeNames records the names of the nodes that said them and gives those that could
// not the last ones recorded. live lists the fingerprints of every context of the talosconfig.
// Best effort: an unreadable or unwritable file only means nothing is remembered.
func rememberNodeNames(live []string, fingerprint string, nodes []nodeOverview) {
	nodeNames.Lock()
	defer nodeNames.Unlock()

	if nodeNames.dir == "" {
		return
	}

	path := filepath.Join(nodeNames.dir, nodeNamesFile)
	saved, plaintext := readKnownNodes(path, nodeNames.aead)
	updated := mergeKnownNodes(saved, live, fingerprint, nodes)

	fillKnownNames(nodes, updated[fingerprint])

	// A file from before encryption is rewritten encrypted even if nothing changed.
	if plaintext || !reflect.DeepEqual(saved, updated) {
		_ = writeKnownNodes(path, updated, nodeNames.aead)
	}
}

// mergeKnownNodes is saved with the clusters not in live dropped and fingerprint's nodes
// replaced by nodes, each keeping its last known names for what it did not say this time.
func mergeKnownNodes(saved knownNodes, live []string, fingerprint string, nodes []nodeOverview) knownNodes {
	out := make(knownNodes, len(live))
	for _, fp := range live {
		if names, ok := saved[fp]; ok {
			out[fp] = names
		}
	}

	current := make(map[string]knownNode, len(nodes))
	for _, n := range nodes {
		if k := knownNodeOf(n, saved[fingerprint][n.Node]); k != (knownNode{}) {
			current[n.Node] = k
		}
	}

	delete(out, fingerprint)
	if len(current) > 0 {
		out[fingerprint] = current
	}

	return out
}

// knownNodeOf is last updated with what n said: a hostname other than its address, a role.
func knownNodeOf(n nodeOverview, last knownNode) knownNode {
	if n.Hostname != n.Node {
		last.Hostname = n.Hostname
	}

	if n.Role != roleUnknown {
		last.Role = n.Role
	}

	return last
}

// fillKnownNames gives each node that did not say its hostname or role the known one.
func fillKnownNames(nodes []nodeOverview, known map[string]knownNode) {
	for i, n := range nodes {
		k := known[n.Node]
		if n.Hostname == n.Node && k.Hostname != "" {
			nodes[i].Hostname = k.Hostname
		}

		if n.Role == roleUnknown && k.Role != "" {
			nodes[i].Role = k.Role
		}
	}
}

// contextFingerprints lists the fingerprint of every context of the talosconfig.
func contextFingerprints(configYAML string) []string {
	p := configs.get(configYAML)
	if _, err := p.parsed(); err != nil {
		return nil
	}

	return slices.Clone(p.fingerprints)
}

// readKnownNodes decrypts the file with aead. plaintext reports a file written in clear by
// a version before encryption: read once, to be rewritten encrypted. A file that does not
// decrypt (the key was reset) is as good as none.
func readKnownNodes(path string, aead cipher.AEAD) (known knownNodes, plaintext bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return knownNodes{}, false
	}

	if out := (knownNodes{}); json.Unmarshal(data, &out) == nil {
		return out, true
	}

	plain, err := openSealed(aead, data)
	if err != nil {
		return knownNodes{}, false
	}

	out := knownNodes{}
	if json.Unmarshal(plain, &out) != nil {
		return knownNodes{}, false
	}

	return out, false
}

// writeKnownNodes replaces the file atomically (see writeSealed).
func writeKnownNodes(path string, known knownNodes, aead cipher.AEAD) error {
	data, err := json.Marshal(known)
	if err != nil {
		return err
	}

	return writeSealed(path, data, aead)
}

// seal is a fresh random nonce followed by data encrypted and authenticated with aead.
func seal(aead cipher.AEAD, data []byte) ([]byte, error) {
	nonceSize, overhead := aead.NonceSize(), aead.Overhead()
	maxInt := int(^uint(0) >> 1)
	if nonceSize < 0 || overhead < 0 {
		return nil, errors.New("invalid AEAD size")
	}
	if nonceSize > maxInt-overhead || len(data) > maxInt-nonceSize-overhead {
		return nil, errors.New("sealed data too large")
	}

	nonce := make([]byte, nonceSize, nonceSize+len(data)+overhead)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return aead.Seal(nonce, nonce, data, nil), nil
}

func openSealed(aead cipher.AEAD, sealed []byte) ([]byte, error) {
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("sealed data too short")
	}

	return aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], nil)
}
