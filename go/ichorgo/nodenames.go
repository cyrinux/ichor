package ichorgo

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
}

// dataKeySize is the length of the key SetDataDir takes: AES-256.
const dataKeySize = 32

// SetDataDir sets the private directory where the core keeps what it remembers across
// launches (node names), and the 32-byte key that encrypts it (AES-256-GCM), which the app
// keeps in its Keystore or Keychain. Call it once at startup, before any other call; until
// then, or with an empty directory or a key of another size, nothing is remembered.
func SetDataDir(dir string, key []byte) {
	nodeNames.Lock()
	defer nodeNames.Unlock()

	nodeNames.dir, nodeNames.aead = "", nil
	if dir == "" || len(key) != dataKeySize {
		return
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return
	}

	nodeNames.dir, nodeNames.aead = dir, aead
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
	cfg, err := loadConfig(configYAML)
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(cfg.Contexts))
	for name, ctx := range cfg.Contexts {
		out = append(out, contextFingerprint(name, ctx))
	}

	return out
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

// writeKnownNodes replaces the file atomically, so a crash never leaves half of it.
func writeKnownNodes(path string, known knownNodes, aead cipher.AEAD) error {
	data, err := json.Marshal(known)
	if err != nil {
		return err
	}

	sealed, err := seal(aead, data)
	if err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// seal is a fresh random nonce followed by data encrypted and authenticated with aead.
func seal(aead cipher.AEAD, data []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(data)+aead.Overhead())
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
