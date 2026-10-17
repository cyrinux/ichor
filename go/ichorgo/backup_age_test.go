package ichorgo

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"filippo.io/age"
)

// The committed vector was made once with EncryptBackupAge from an invented payload (a
// documentation-range address, no real cluster) and this passphrase; `age -d` opens it too.
const (
	backupAgeVectorPath       = "testdata/backup_age_v1.age"
	backupAgeVectorPassphrase = "test vector passphrase 42"
)

func newTestAgeIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()

	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	return id
}

func TestBackupAgePassphraseRoundTrip(t *testing.T) {
	payload := backupTestPayload(t)

	sealed, err := EncryptBackupAge(payload, backupTestPassphrase, "")
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(sealed, []byte("age-encryption.org/v1\n-> scrypt ")) {
		t.Fatalf("not a binary age scrypt file: %q", sealed[:40])
	}

	if bytes.Contains(sealed, []byte("talosconfig")) || bytes.Contains(sealed, []byte("DARK")) {
		t.Fatal("the payload must not appear in clear")
	}

	got, err := DecryptBackup(sealed, backupTestPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	if got != payload {
		t.Fatal("round trip changed the payload")
	}

	// The age library itself opens it, as `age -d` would.
	id, err := age.NewScryptIdentity(backupTestPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	r, err := age.Decrypt(bytes.NewReader(sealed), id)
	if err != nil {
		t.Fatal(err)
	}

	plain, err := io.ReadAll(r)
	if err != nil || string(plain) != payload {
		t.Fatalf("age library: %v", err)
	}
}

func TestBackupAgeRecipientsOnly(t *testing.T) {
	payload := backupTestPayload(t)
	a, b := newTestAgeIdentity(t), newTestAgeIdentity(t)
	recipients := "\n  " + a.Recipient().String() + "  \n\n" + b.Recipient().String() + "\n"

	sealed, err := EncryptBackupAge(payload, "", recipients)
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []*age.X25519Identity{a, b} {
		r, err := age.Decrypt(bytes.NewReader(sealed), id)
		if err != nil {
			t.Fatal(err)
		}

		plain, err := io.ReadAll(r)
		if err != nil || string(plain) != payload {
			t.Fatalf("identity did not open the file: %v", err)
		}
	}

	_, err = DecryptBackup(sealed, backupTestPassphrase)
	requireBackupErr(t, err, BackupErrNeedsIdentity)

	_, err = DecryptBackup(sealed, "")
	requireBackupErr(t, err, BackupErrNeedsIdentity)
}

func TestBackupAgeWrongPassphrase(t *testing.T) {
	sealed, err := EncryptBackupAge(backupTestPayload(t), backupTestPassphrase, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = DecryptBackup(sealed, "another long passphrase")
	requireBackupErr(t, err, BackupErrWrongPassphrase)
}

func TestBackupAgeTampered(t *testing.T) {
	sealed, err := EncryptBackupAge(backupTestPayload(t), backupTestPassphrase, "")
	if err != nil {
		t.Fatal(err)
	}

	headerEnd := bytes.Index(sealed, []byte("\n---"))
	if headerEnd < 0 {
		t.Fatal("no header end")
	}

	for name, at := range map[string]int{
		"payload":  len(sealed) - 5,
		"salt":     len("age-encryption.org/v1\n-> scrypt ") + 2,
		"work":     bytes.Index(sealed, []byte(" 16\n")) + 2,
		"mac line": headerEnd + 6,
	} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(sealed)
			bad[at] ^= 0x01

			_, err := DecryptBackup(bad, backupTestPassphrase)
			requireBackupErr(t, err, BackupErrWrongPassphrase)
		})
	}

	_, err = DecryptBackup(sealed[:len(sealed)-10], backupTestPassphrase)
	requireBackupErr(t, err, BackupErrWrongPassphrase)
}

func TestBackupAgeRefusesMix(t *testing.T) {
	recipient := newTestAgeIdentity(t).Recipient().String()

	_, err := EncryptBackupAge(backupTestPayload(t), backupTestPassphrase, recipient)
	requireBackupErr(t, err, BackupErrInvalidContent)

	if !strings.Contains(err.Error(), "passphrase cannot be combined with age keys") {
		t.Fatalf("unclear message: %v", err)
	}
}

func TestBackupAgeNeitherPassphraseNorKeys(t *testing.T) {
	for _, recipients := range []string{"", " \n\t\n"} {
		_, err := EncryptBackupAge(backupTestPayload(t), "", recipients)
		requireBackupErr(t, err, BackupErrPassphraseShort)
	}

	_, err := EncryptBackupAge(backupTestPayload(t), "short", "")
	requireBackupErr(t, err, BackupErrPassphraseShort)
}

func TestBackupAgeBadRecipient(t *testing.T) {
	good := newTestAgeIdentity(t)
	secret := good.String()

	for name, list := range map[string]string{
		"garbage":     good.Recipient().String() + "\nnot-a-key-xyz",
		"bad bech32":  good.Recipient().String()[:20] + "qqqq",
		"private key": secret,
		"ssh key":     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample user@example",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := EncryptBackupAge(backupTestPayload(t), "", list)
			requireBackupErr(t, err, BackupErrInvalidContent)

			if !strings.Contains(err.Error(), "line ") {
				t.Fatalf("the error must name the line: %v", err)
			}

			for _, line := range strings.Split(list, "\n") {
				if strings.Contains(err.Error(), line) {
					t.Fatalf("the error echoes the key list: %v", err)
				}
			}
		})
	}
}

func TestBackupAgeRefusesInvalidPayload(t *testing.T) {
	_, err := EncryptBackupAge(`{"format":1}`, backupTestPassphrase, "")
	requireBackupErr(t, err, BackupErrInvalidContent)
}

func TestBackupAgeCommittedVector(t *testing.T) {
	file, err := os.ReadFile(backupAgeVectorPath)
	if err != nil {
		t.Fatal(err)
	}

	if f, _ := BackupFormatOf(file); f != BackupFormatAge {
		t.Fatalf("format %q", f)
	}

	got, err := DecryptBackup(file, backupAgeVectorPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	// The age library as the oracle (what `age -d` runs).
	id, err := age.NewScryptIdentity(backupAgeVectorPassphrase)
	if err != nil {
		t.Fatal(err)
	}

	r, err := age.Decrypt(bytes.NewReader(file), id)
	if err != nil {
		t.Fatal(err)
	}

	oracle, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	if got != string(oracle) || !strings.Contains(got, `"vectorNote":"invented test vector"`) {
		t.Fatalf("vector payload: %s", got)
	}

	_, err = DecryptBackup(file, "not the vector passphrase")
	requireBackupErr(t, err, BackupErrWrongPassphrase)
}

func TestBackupFormatOf(t *testing.T) {
	ichor := mustEncryptBackup(t, backupTestPayload(t))

	armored := []byte("-----BEGIN AGE ENCRYPTED FILE-----\nYWdl\n-----END AGE ENCRYPTED FILE-----\n")

	for name, tc := range map[string]struct {
		file []byte
		want string
	}{
		"ichorbak": {ichor, BackupFormatIchor},
		"age":      {[]byte("age-encryption.org/v1\n-> X25519 abc\n"), BackupFormatAge},
		"armored":  {armored, BackupFormatAge},
		"json":     {[]byte(`{"format":3}`), ""},
		"empty":    {nil, ""},
		"other v":  {[]byte("age-encryption.org/v2\n"), ""},
	} {
		got, err := BackupFormatOf(tc.file)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", name, got, err, tc.want)
		}
	}
}

func TestDecryptBackupNotABackup(t *testing.T) {
	for _, file := range [][]byte{nil, []byte("hello"), []byte(`{"format":3}`)} {
		_, err := DecryptBackup(file, backupTestPassphrase)
		requireBackupErr(t, err, BackupErrNotBackup)
	}
}
