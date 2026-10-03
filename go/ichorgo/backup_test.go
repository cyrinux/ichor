package ichorgo

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const backupTestPassphrase = "correct horse battery"

func backupTestPayload(t *testing.T) string {
	t.Helper()

	raw, err := json.Marshal(map[string]any{
		"format":      BackupPayloadFormat,
		"talosconfig": testConfig(t, time.Now().Add(time.Hour)),
		"settings":    map[string]any{"themeMode": "DARK"},
	})
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func mustEncryptBackup(t *testing.T, payload string) []byte {
	t.Helper()

	out, err := EncryptBackup(payload, backupTestPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func requireBackupErr(t *testing.T, err error, code string) {
	t.Helper()

	if err == nil || !strings.HasPrefix(err.Error(), code) {
		t.Fatalf("want %s error, got %v", code, err)
	}
}

func TestBackupRoundTrip(t *testing.T) {
	payload := backupTestPayload(t)
	sealed := mustEncryptBackup(t, payload)

	if bytes.Contains(sealed, []byte("talosconfig")) || bytes.Contains(sealed, []byte("DARK")) {
		t.Fatal("the payload must not appear in clear")
	}

	got, err := DecryptBackup(sealed, backupTestPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	if got != payload {
		t.Fatalf("payload changed:\n%s\n%s", got, payload)
	}
}

func TestBackupFreshSaltAndNonce(t *testing.T) {
	payload := backupTestPayload(t)
	a := mustEncryptBackup(t, payload)
	b := mustEncryptBackup(t, payload)

	if bytes.Equal(a[:backupHeaderLen], b[:backupHeaderLen]) {
		t.Fatal("each backup must get its own salt and nonce")
	}
}

func TestBackupWrongPassphrase(t *testing.T) {
	sealed := mustEncryptBackup(t, backupTestPayload(t))

	_, err := DecryptBackup(sealed, "correct horse battery!")
	requireBackupErr(t, err, BackupErrWrongPassphrase)
}

func TestBackupTamperedFile(t *testing.T) {
	sealed := mustEncryptBackup(t, backupTestPayload(t))

	for _, at := range []int{backupHeaderLen - 1, backupHeaderLen + 3, len(sealed) - 1} {
		damaged := bytes.Clone(sealed)
		damaged[at] ^= 0x01

		_, err := DecryptBackup(damaged, backupTestPassphrase)
		requireBackupErr(t, err, BackupErrWrongPassphrase)
	}
}

func TestBackupHeaderIsAuthenticated(t *testing.T) {
	sealed := mustEncryptBackup(t, backupTestPayload(t))

	// A cheaper KDF cost in the header must not decrypt.
	weaker := bytes.Clone(sealed)
	binary.BigEndian.PutUint32(weaker[len(backupMagic)+2:], 1)

	_, err := DecryptBackup(weaker, backupTestPassphrase)
	requireBackupErr(t, err, BackupErrWrongPassphrase)
}

func TestBackupRejectsHostileParameters(t *testing.T) {
	sealed := mustEncryptBackup(t, backupTestPayload(t))
	memoryAt := len(backupMagic) + 2 + 4

	huge := bytes.Clone(sealed)
	binary.BigEndian.PutUint32(huge[memoryAt:], 4<<20)

	_, err := DecryptBackup(huge, backupTestPassphrase)
	requireBackupErr(t, err, BackupErrUnsupported)

	noThreads := bytes.Clone(sealed)
	noThreads[memoryAt+4] = 0

	_, err = DecryptBackup(noThreads, backupTestPassphrase)
	requireBackupErr(t, err, BackupErrUnsupported)
}

func TestBackupNotABackup(t *testing.T) {
	for _, in := range [][]byte{nil, []byte("context: lab\n"), bytes.Repeat([]byte{0}, 200)} {
		_, err := DecryptBackup(in, backupTestPassphrase)
		requireBackupErr(t, err, BackupErrNotBackup)
	}
}

func TestBackupUnsupportedVersion(t *testing.T) {
	sealed := mustEncryptBackup(t, backupTestPayload(t))
	sealed[len(backupMagic)] = backupVersion + 1

	_, err := DecryptBackup(sealed, backupTestPassphrase)
	requireBackupErr(t, err, BackupErrUnsupported)
}

func TestBackupShortPassphrase(t *testing.T) {
	_, err := EncryptBackup(backupTestPayload(t), "short pass")
	requireBackupErr(t, err, BackupErrPassphraseShort)

	// Counted in characters, not bytes: 12 accented letters pass.
	if _, err := EncryptBackup(backupTestPayload(t), strings.Repeat("é", BackupMinPassphrase)); err != nil {
		t.Fatal(err)
	}
}

func TestBackupValidatesPayload(t *testing.T) {
	cases := map[string]string{
		"not json":      "nope",
		"no format":     `{"talosconfig":"context: a"}`,
		"bad config":    `{"format":1,"talosconfig":"::"}`,
		"empty config":  `{"format":1,"talosconfig":""}`,
		"newer payload": `{"format":99,"talosconfig":"x"}`,
	}

	for name, payload := range cases {
		if _, err := EncryptBackup(payload, backupTestPassphrase); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
