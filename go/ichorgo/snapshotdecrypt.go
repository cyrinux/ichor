package ichorgo

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// The decrypt side of encrypted etcd snapshots, for a recovery from the phone: with the age
// secret key (AGE-SECRET-KEY-1…) or the passphrase. A YubiKey cannot be used here (it needs
// age-plugin-yubikey): such a file is decrypted on a laptop first.

const (
	ageBinaryHeader = "age-encryption.org/"
	ageArmorHeader  = armor.Header
	// maxSnapshotWorkFactor is the costliest passphrase file opened (2^18, about 256 MB, age's
	// own default): age accepts 2^22, gigabytes a phone process does not have.
	maxSnapshotWorkFactor = 18
)

// Recipient kinds of an encrypted snapshot, as SnapshotInspect names them.
const (
	snapshotHintX25519  = "x25519"
	snapshotHintScrypt  = "scrypt"
	snapshotHintUnknown = "unknown"
)

// snapshotInfo is what SnapshotInspect returns.
type snapshotInfo struct {
	Encrypted bool `json:"encrypted"`
	// RecipientsHint says what opens the file: "x25519" (an age secret key), "scrypt" (a
	// passphrase), "unknown" (a YubiKey, an SSH key…: not from the phone); "" in clear.
	RecipientsHint string `json:"recipientsHint"`
	Size           int64  `json:"size"`
	Sha256         string `json:"sha256"`
}

// SnapshotInspect describes an etcd snapshot file before a recovery: whether it is
// age-encrypted and with what (so the app asks for a passphrase or a secret key), its size
// and its sha256. Nothing is decrypted.
func SnapshotInspect(path string) (out string, err error) {
	defer maskResult(&out, &err)

	info, err := readSnapshotInfo(path)
	if err != nil {
		return "", err
	}

	return toJSON(info)
}

func readSnapshotInfo(path string) (snapshotInfo, error) {
	if strings.TrimSpace(path) == "" {
		return snapshotInfo{}, errors.New("no snapshot file given")
	}

	f, err := os.Open(path)
	if err != nil {
		return snapshotInfo{}, fmt.Errorf("cannot open the snapshot: %w", err)
	}

	defer f.Close() //nolint:errcheck

	info := snapshotInfo{}

	br := bufio.NewReader(f)

	info.Encrypted, err = isAgeFile(br)
	if err != nil {
		return snapshotInfo{}, err
	}

	if info.Encrypted {
		info.RecipientsHint = snapshotRecipientsHint(ageReader(br))
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return snapshotInfo{}, err
	}

	h := sha256.New()

	info.Size, err = io.Copy(h, f)
	if err != nil {
		return snapshotInfo{}, fmt.Errorf("cannot read the snapshot: %w", err)
	}

	if info.Size == 0 {
		return snapshotInfo{}, errors.New("the snapshot file is empty")
	}

	info.Sha256 = hex.EncodeToString(h.Sum(nil))

	return info, nil
}

// isAgeFile tells an age file (binary or armored) from a clear etcd database, without
// consuming br.
func isAgeFile(br *bufio.Reader) (bool, error) {
	head, err := br.Peek(len(ageArmorHeader))
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return false, fmt.Errorf("cannot read the snapshot: %w", err)
	}

	s := string(head)

	return strings.HasPrefix(s, ageBinaryHeader) || strings.HasPrefix(s, ageArmorHeader), nil
}

// ageReader unwraps the ASCII armor when the file has it.
func ageReader(br *bufio.Reader) io.Reader {
	if head, _ := br.Peek(len(ageArmorHeader)); string(head) == ageArmorHeader {
		return armor.NewReader(br)
	}

	return br
}

// stanzaRecorder is an age identity that matches nothing and keeps the stanza types.
type stanzaRecorder struct{ types []string }

func (r *stanzaRecorder) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	for _, s := range stanzas {
		r.types = append(r.types, s.Type)
	}

	return nil, age.ErrIncorrectIdentity
}

func snapshotRecipientsHint(r io.Reader) string {
	rec := &stanzaRecorder{}
	_, _ = age.Decrypt(r, rec) //nolint:errcheck // it never matches: only the header is read

	hint := ""

	for _, t := range rec.types {
		kind := snapshotHintUnknown

		switch t {
		case "X25519", "mlkem768x25519":
			kind = snapshotHintX25519
		case "scrypt":
			kind = snapshotHintScrypt
		}

		// One kind the phone can open is enough.
		if kind != snapshotHintUnknown {
			return kind
		}

		hint = kind
	}

	if hint == "" {
		return snapshotHintUnknown
	}

	return hint
}

// snapshotDecryptor returns the reader wrapper that decrypts a snapshot with identity (age
// secret keys, one per line) or passphrase; with neither, the file must be in clear. The
// wrapper fails at once (before any upload) when the key or passphrase does not open it.
func snapshotDecryptor(identity, passphrase string) (func(io.Reader) (io.Reader, error), error) {
	identity = strings.TrimSpace(identity)

	var ids []age.Identity

	switch {
	case identity != "" && passphrase != "":
		return nil, errors.New("give the secret key or the passphrase, not both")
	case identity != "":
		parsed, err := age.ParseIdentities(strings.NewReader(identity))
		if err != nil {
			// Never echo the key: the parse error may quote it.
			return nil, errors.New("not an age secret key: paste the AGE-SECRET-KEY-1… line")
		}

		ids = parsed
	case passphrase != "":
		id, err := age.NewScryptIdentity(passphrase)
		if err != nil {
			return nil, err
		}

		id.SetMaxWorkFactor(maxSnapshotWorkFactor)

		ids = []age.Identity{id}
	}

	return func(r io.Reader) (io.Reader, error) {
		br := bufio.NewReader(r)

		encrypted, err := isAgeFile(br)
		if err != nil {
			return nil, err
		}

		switch {
		case !encrypted && len(ids) > 0:
			return nil, errors.New("this snapshot is not encrypted: recover it without a key or passphrase")
		case !encrypted:
			return br, nil
		case len(ids) == 0:
			return nil, errors.New("this snapshot is encrypted: give its passphrase or age secret key")
		}

		plain, err := age.Decrypt(ageReader(br), ids...)
		if err != nil {
			return nil, decryptError(err)
		}

		return plain, nil
	}, nil
}

func decryptError(err error) error {
	var noMatch *age.NoIdentityMatchError
	if errors.As(err, &noMatch) {
		return errors.New("this key or passphrase does not open the snapshot")
	}

	return fmt.Errorf("cannot decrypt the snapshot: %w", err)
}
