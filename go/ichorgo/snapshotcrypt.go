package ichorgo

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/tag"
)

// snapshotWorkFactor is the scrypt work factor (2^16, about 64 MB) for passphrase-encrypted
// snapshots. age's default (2^18) needs 256 MB, too much for a phone process; `age -d`
// accepts up to 2^22, so the file still opens everywhere.
const snapshotWorkFactor = 16

// minSnapshotPassphrase is the shortest passphrase accepted: the file may sit on a cloud
// drive for years, and scrypt at 2^16 is cheaper to brute-force than age's default.
const minSnapshotPassphrase = 12

// maxSnapshotRecipients bounds the keys field (a pasted file of a whole team is fine).
const maxSnapshotRecipients = 50

// snapshotRecipient describes one public key the snapshot is encrypted for, for the UI.
type snapshotRecipient struct {
	Type    string `json:"type"`    // "age", "age-pq", "hardware", "hardware-pq", "ssh-ed25519", "ssh-rsa"
	Comment string `json:"comment"` // the SSH key's comment, "" otherwise
}

// CheckSnapshotRecipients validates the public keys typed or pasted for an encrypted etcd
// snapshot (one per line, "#" comments allowed) and describes them, so the app can show
// what the backup will be encrypted for before taking it.
func CheckSnapshotRecipients(text string) (out string, err error) {
	defer maskResult(&out, &err)

	_, described, err := parseSnapshotRecipients(text)
	if err != nil {
		return "", err
	}

	return toJSON(described)
}

// parseSnapshotRecipients reads age (age1…, age1pq1…) and SSH (ssh-ed25519, ssh-rsa) public
// keys, one per line. A private key pasted by mistake is refused without echoing it.
func parseSnapshotRecipients(text string) ([]age.Recipient, []snapshotRecipient, error) {
	var (
		recipients []age.Recipient
		described  []snapshotRecipient
	)

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		r, d, err := parseSnapshotRecipient(line)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}

		recipients = append(recipients, r)
		described = append(described, d)

		if len(recipients) > maxSnapshotRecipients {
			return nil, nil, fmt.Errorf("more than %d keys", maxSnapshotRecipients)
		}
	}

	if len(recipients) == 0 {
		return nil, nil, errors.New("no public key: paste an age (age1…) or SSH (ssh-ed25519 …) public key")
	}

	// age refuses to encrypt to post-quantum and classic recipients together; say so now,
	// not after the file picker.
	pq := 0

	for _, d := range described {
		if strings.HasSuffix(d.Type, "-pq") {
			pq++
		}
	}

	if pq > 0 && pq < len(described) {
		return nil, nil, errors.New("post-quantum keys (age1pq1…, age1tagpq1…) cannot be mixed with classic keys: use one kind only")
	}

	return recipients, described, nil
}

func parseSnapshotRecipient(line string) (age.Recipient, snapshotRecipient, error) {
	if !utf8.ValidString(line) {
		return nil, snapshotRecipient{}, errors.New("not valid text")
	}

	if looksLikePrivateKey(line) {
		return nil, snapshotRecipient{}, errors.New("this is a private key: paste the public key instead (never share the private one)")
	}

	if strings.HasPrefix(line, "ssh-") {
		r, err := agessh.ParseRecipient(line)
		if err != nil {
			return nil, snapshotRecipient{}, fmt.Errorf("not a supported SSH public key (ssh-ed25519 or ssh-rsa): %w", err)
		}

		fields := strings.Fields(line)
		d := snapshotRecipient{Type: fields[0]}

		if len(fields) > 2 {
			d.Comment = strings.Join(fields[2:], " ")
		}

		return r, d, nil
	}

	switch {
	case strings.HasPrefix(line, "age1tag1"), strings.HasPrefix(line, "age1tagpq1"):
		// The public side of a key held in hardware (a YubiKey through age-plugin-yubikey):
		// age encrypts to it natively, the plugin is only needed to decrypt.
		r, err := tag.ParseRecipient(line)
		if err != nil {
			return nil, snapshotRecipient{}, fmt.Errorf("not a valid hardware key recipient: %w", err)
		}

		if strings.HasPrefix(line, "age1tagpq1") {
			return r, snapshotRecipient{Type: "hardware-pq"}, nil
		}

		return r, snapshotRecipient{Type: "hardware"}, nil
	case strings.HasPrefix(line, "age1yubikey1"):
		return nil, snapshotRecipient{}, errors.New("age1yubikey1 keys need the plugin to encrypt: use the age1tag1… form of the same YubiKey key, printed by a recent age-plugin-yubikey (age-plugin-yubikey --list)")
	case strings.HasPrefix(line, "age1pq1"):
		r, err := age.ParseHybridRecipient(line)
		if err != nil {
			return nil, snapshotRecipient{}, fmt.Errorf("not a valid age public key: %w", err)
		}

		return r, snapshotRecipient{Type: "age-pq"}, nil
	case strings.HasPrefix(line, "age1"):
		r, err := age.ParseX25519Recipient(line)
		if err != nil {
			return nil, snapshotRecipient{}, fmt.Errorf("not a valid age public key: %w", err)
		}

		return r, snapshotRecipient{Type: "age"}, nil
	}

	return nil, snapshotRecipient{}, errors.New("not an age (age1…) or SSH (ssh-ed25519, ssh-rsa) public key")
}

func looksLikePrivateKey(line string) bool {
	upper := strings.ToUpper(line)

	return strings.HasPrefix(upper, "AGE-SECRET-KEY-") || strings.HasPrefix(upper, "AGE-PLUGIN-") ||
		strings.Contains(upper, "PRIVATE KEY")
}

// snapshotEncryptor returns the writer wrapper for an encrypted snapshot: for the public keys
// when recipients is not empty, else for the passphrase. Exactly one must be given.
func snapshotEncryptor(recipients, passphrase string) (func(io.Writer) (io.WriteCloser, error), error) {
	hasKeys := strings.TrimSpace(recipients) != ""

	switch {
	case hasKeys && passphrase != "":
		return nil, errors.New("choose public keys or a passphrase, not both")
	case hasKeys:
		recs, _, err := parseSnapshotRecipients(recipients)
		if err != nil {
			return nil, err
		}

		return func(w io.Writer) (io.WriteCloser, error) { return age.Encrypt(w, recs...) }, nil
	case passphrase != "":
		if utf8.RuneCountInString(passphrase) < minSnapshotPassphrase {
			return nil, fmt.Errorf("the passphrase needs at least %d characters", minSnapshotPassphrase)
		}

		r, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return nil, err
		}

		r.SetWorkFactor(snapshotWorkFactor)

		return func(w io.Writer) (io.WriteCloser, error) { return age.Encrypt(w, r) }, nil
	}

	return nil, errors.New("no public key or passphrase to encrypt the snapshot with")
}
