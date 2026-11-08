package ichorgo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"filippo.io/age"
)

// An app backup can also be a standard age file (age-encryption.org/v1), so it opens on a
// computer with `age -d backup.age | jq .` without the app: sealed with a passphrase (an age
// scrypt recipient, restorable on the phone) or for one or more pasted age public keys (only
// the matching age identity opens it, on a computer). DecryptBackup tells both formats apart.

// Backup file formats, as BackupFormatOf names them.
const (
	BackupFormatIchor = "ichorbak"
	BackupFormatAge   = "age"
)

// backupAgeOverhead bounds what age adds around a backupMaxPayload payload: the header with
// up to maxSnapshotRecipients stanzas, the nonce and a 16-byte tag per 64 KiB chunk.
const backupAgeOverhead = 64 << 10

// EncryptBackupAge seals a backup payload (JSON) as an age file, either with a passphrase or
// for recipients (newline-separated age1… public keys), never both: age allows a passphrase
// only as the file's sole recipient.
func EncryptBackupAge(payloadJSON, passphrase, recipients string) (out []byte, err error) {
	defer maskErr(&err)

	hasKeys := strings.TrimSpace(recipients) != ""

	if hasKeys && passphrase != "" {
		return nil, errors.New(BackupErrInvalidContent + ": a passphrase cannot be combined with age keys: use one or the other")
	}

	var recs []age.Recipient

	if hasKeys {
		recs, err = parseBackupRecipients(recipients)
		if err != nil {
			return nil, err
		}
	} else {
		if utf8.RuneCountInString(passphrase) < BackupMinPassphrase {
			return nil, fmt.Errorf("%s: use at least %d characters", BackupErrPassphraseShort, BackupMinPassphrase)
		}

		r, err := age.NewScryptRecipient(passphrase)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", BackupErrPassphraseShort, err)
		}

		r.SetWorkFactor(snapshotWorkFactor)
		recs = []age.Recipient{r}
	}

	if _, err := validateBackupPayload([]byte(payloadJSON)); err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	w, err := age.Encrypt(&buf, recs...)
	if err != nil {
		return nil, errors.New(BackupErrInvalidContent + ": these age keys cannot be used together")
	}

	if _, err := io.WriteString(w, payloadJSON); err != nil {
		return nil, fmt.Errorf("encrypt the backup: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("encrypt the backup: %w", err)
	}

	return buf.Bytes(), nil
}

// parseBackupRecipients reads age1… public keys, one per line (blank lines and "#" comments
// skipped). Errors name the line only: a pasted key list never ends up in a message.
func parseBackupRecipients(text string) ([]age.Recipient, error) {
	var recs []age.Recipient

	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if looksLikePrivateKey(line) {
			return nil, fmt.Errorf("%s: line %d is a private key: paste the public key (age1…) instead", BackupErrInvalidContent, i+1)
		}

		// Bech32 allows an all upper case key (as a QR code may carry it); age parses lower case.
		key := strings.ToLower(line)
		if !strings.HasPrefix(key, "age1") {
			return nil, fmt.Errorf("%s: line %d is not an age public key (age1…)", BackupErrInvalidContent, i+1)
		}

		r, _, err := parseSnapshotRecipient(key)
		if err != nil {
			return nil, fmt.Errorf("%s: line %d is not a valid age public key", BackupErrInvalidContent, i+1)
		}

		recs = append(recs, r)

		if len(recs) > maxSnapshotRecipients {
			return nil, fmt.Errorf("%s: more than %d age keys", BackupErrInvalidContent, maxSnapshotRecipients)
		}
	}

	if len(recs) == 0 {
		return nil, errors.New(BackupErrInvalidContent + ": no age public key")
	}

	return recs, nil
}

// BackupFormatOf names the format of a backup file for the apps' file pickers: "ichorbak",
// "age", or "" when it is neither.
func BackupFormatOf(file []byte) (out string, err error) {
	defer maskErr(&err)

	return backupFormat(file), nil
}

func backupFormat(file []byte) string {
	switch {
	case bytes.HasPrefix(file, []byte(backupMagic)):
		return BackupFormatIchor
	case bytes.HasPrefix(file, []byte(ageBinaryHeader+"v1\n")), bytes.HasPrefix(file, []byte(ageArmorHeader)):
		return BackupFormatAge
	}

	return ""
}

// decryptBackupAge opens an age backup with its passphrase. A file sealed for age keys only
// (no scrypt stanza) cannot be opened here: BackupErrNeedsIdentity.
func decryptBackupAge(file []byte, passphrase string) ([]byte, error) {
	if len(file) > backupMaxPayload+backupAgeOverhead {
		return nil, fmt.Errorf("%s: file too large", BackupErrNotBackup)
	}

	// The recorder sees the stanzas first, so a file without a scrypt stanza is told apart
	// from a wrong passphrase. An empty passphrase has no scrypt identity, only the recorder.
	rec := &stanzaRecorder{}
	ids := []age.Identity{rec}

	if id, err := age.NewScryptIdentity(passphrase); err == nil {
		id.SetMaxWorkFactor(maxSnapshotWorkFactor)
		ids = append(ids, id)
	}

	plain, err := age.Decrypt(ageReader(bufio.NewReader(bytes.NewReader(file))), ids...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) && len(rec.types) > 0 && !slices.Contains(rec.types, "scrypt") {
			return nil, errors.New(BackupErrNeedsIdentity + ": this backup opens with your age key on a computer")
		}

		return nil, fmt.Errorf("%s: wrong passphrase or damaged file", BackupErrWrongPassphrase)
	}

	out, err := io.ReadAll(io.LimitReader(plain, backupMaxPayload+1))
	if err != nil {
		return nil, fmt.Errorf("%s: wrong passphrase or damaged file", BackupErrWrongPassphrase)
	}

	if len(out) > backupMaxPayload {
		return nil, fmt.Errorf("%s: payload too large", BackupErrInvalidContent)
	}

	return out, nil
}
