package ichorgo

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"filippo.io/age/tag"
	"golang.org/x/crypto/ssh"
)

const snapshotData = "etcd snapshot bytes "

// encryptedSnapshot writes a 2.4 MB snapshot through wrap and returns the file's bytes and
// the reported clear SHA-256.
func encryptedSnapshot(t *testing.T, wrap snapshotWrap) ([]byte, string, string) {
	t.Helper()

	data := strings.Repeat(snapshotData, 120_000)
	dest := filepath.Join(t.TempDir(), "etcd.snapshot.age")

	size, sum, err := writeSnapshot(strings.NewReader(data), dest, wrap, func(int64) {})
	if err != nil {
		t.Fatal(err)
	}

	if size != int64(len(data)) {
		t.Errorf("size = %d, want the clear size %d", size, len(data))
	}

	want := sha256.Sum256([]byte(data))
	if sum != hex.EncodeToString(want[:]) {
		t.Error("sha256 is not the clear snapshot's")
	}

	file, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(file, []byte(snapshotData)) {
		t.Fatal("clear data in the encrypted file")
	}

	return file, sum, data
}

func decryptSnapshot(t *testing.T, file []byte, id age.Identity) string {
	t.Helper()

	r, err := age.Decrypt(bytes.NewReader(file), id)
	if err != nil {
		t.Fatal(err)
	}

	clear, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	return string(clear)
}

func TestSnapshotEncryptedForAgeAndSSHKeys(t *testing.T) {
	ageID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	sshLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " admin@laptop"

	sshID, err := agessh.NewEd25519Identity(priv)
	if err != nil {
		t.Fatal(err)
	}

	keys := "# team keys\n" + ageID.Recipient().String() + "\n\n  " + sshLine + "  \n"

	wrap, err := snapshotEncryptor(keys, "")
	if err != nil {
		t.Fatal(err)
	}

	file, _, data := encryptedSnapshot(t, wrap)

	for name, id := range map[string]age.Identity{"age": ageID, "ssh": sshID} {
		if decryptSnapshot(t, file, id) != data {
			t.Errorf("%s: decrypted data differs", name)
		}
	}

	out, err := CheckSnapshotRecipients(keys)
	if err != nil {
		t.Fatal(err)
	}

	if out != `[{"type":"age","comment":""},{"type":"ssh-ed25519","comment":"admin@laptop"}]` {
		t.Errorf("described = %s", out)
	}
}

// A hardware key (YubiKey through age-plugin-yubikey) is given as its age1tag1 recipient;
// decrypting needs the device, so this checks the file is encrypted to it (p256tag stanza).
func TestSnapshotEncryptedForHardwareKey(t *testing.T) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	x, y := elliptic.Unmarshal(elliptic.P256(), priv.PublicKey().Bytes()) //nolint:staticcheck // only to build a test key
	hw, err := tag.NewClassicRecipient(elliptic.MarshalCompressed(elliptic.P256(), x, y))
	if err != nil {
		t.Fatal(err)
	}

	out, err := CheckSnapshotRecipients(hw.String())
	if err != nil {
		t.Fatal(err)
	}

	if out != `[{"type":"hardware","comment":""}]` {
		t.Errorf("described = %s", out)
	}

	wrap, err := snapshotEncryptor(hw.String(), "")
	if err != nil {
		t.Fatal(err)
	}

	file, _, _ := encryptedSnapshot(t, wrap)
	if !bytes.Contains(file, []byte("\n-> p256tag ")) {
		t.Error("no p256tag stanza for the hardware key")
	}

	_, err = snapshotEncryptor("age1yubikey1qwt50d05nh5vutpdzmlg5wn80xq5negm4uj9ghv0snvdd3yysf5yw3rhl3t", "")
	if err == nil || !strings.Contains(err.Error(), "age1tag1") {
		t.Errorf("age1yubikey1 error = %v", err)
	}
}

func TestSnapshotEncryptedWithPassphrase(t *testing.T) {
	const pass = "correct horse battery"

	wrap, err := snapshotEncryptor("", pass)
	if err != nil {
		t.Fatal(err)
	}

	file, _, data := encryptedSnapshot(t, wrap)

	if !bytes.Contains(file, []byte("-> scrypt ")) || !bytes.Contains(file, []byte(" 16\n")) {
		t.Error("expected an scrypt stanza with work factor 16")
	}

	id, err := age.NewScryptIdentity(pass)
	if err != nil {
		t.Fatal(err)
	}

	if decryptSnapshot(t, file, id) != data {
		t.Error("decrypted data differs")
	}

	wrong, _ := age.NewScryptIdentity("not the passphrase")
	if _, err := age.Decrypt(bytes.NewReader(file), wrong); err == nil {
		t.Error("wrong passphrase decrypted the snapshot")
	}
}

func TestSnapshotEncryptorRefusals(t *testing.T) {
	ageID, _ := age.GenerateX25519Identity()

	cases := map[string]struct{ keys, pass, want string }{
		"nothing":       {"", "", "no public key or passphrase"},
		"both":          {ageID.Recipient().String(), "long enough passphrase", "not both"},
		"short":         {"", "short", "at least 12"},
		"comments only": {"# nobody\n\n", "", "no public key: paste"},
		"age private":   {ageID.String(), "", "private key"},
		"pem private":   {"-----BEGIN OPENSSH PRIVATE KEY-----", "", "private key"},
		"garbage":       {"hello", "", "line 1: not an age"},
		"bad age":       {"age1notreallyakey", "", "not a valid age public key"},
		"bad ssh":       {"ssh-ed25519 AAAA", "", "not a supported SSH public key"},
		"dsa":           {"ssh-dss AAAAB3NzaC1kc3MAAACBAP", "", "not a supported SSH public key"},
	}

	for name, c := range cases {
		_, err := snapshotEncryptor(c.keys, c.pass)
		if err == nil {
			t.Errorf("%s: accepted", name)

			continue
		}

		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q, want %q", name, err, c.want)
		}

		if strings.Contains(err.Error(), "AGE-SECRET-KEY") {
			t.Errorf("%s: error echoes the private key", name)
		}
	}
}

func TestSnapshotRecipientsRefuseMixingPostQuantumAndClassic(t *testing.T) {
	classic, _ := age.GenerateX25519Identity()

	pq, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}

	pqOnly := pq.Recipient().String()
	if _, err := CheckSnapshotRecipients(pqOnly); err != nil {
		t.Fatalf("post-quantum alone refused: %v", err)
	}

	_, err = CheckSnapshotRecipients(pqOnly + "\n" + classic.Recipient().String())
	if err == nil || !strings.Contains(err.Error(), "cannot be mixed") {
		t.Errorf("mixed keys: err = %v", err)
	}

	// The check matches what age itself accepts.
	wrap, err := snapshotEncryptor(pqOnly, "")
	if err != nil {
		t.Fatal(err)
	}

	file, _, data := encryptedSnapshot(t, wrap)
	if decryptSnapshot(t, file, pq) != data {
		t.Error("post-quantum round trip differs")
	}
}

func TestEncryptedSnapshotFailureLeavesNoFile(t *testing.T) {
	ageID, _ := age.GenerateX25519Identity()

	wrap, err := snapshotEncryptor(ageID.Recipient().String(), "")
	if err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "etcd.snapshot.age")

	if _, _, err := writeSnapshot(&failingReader{n: 3}, dest, wrap, func(int64) {}); err == nil {
		t.Fatal("expected error")
	}

	for _, p := range []string{dest, dest + ".part"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after failure", p)
		}
	}
}

type snapshotResult struct {
	done chan string
}

func (snapshotResult) OnProgress(int64) {}

func (r snapshotResult) OnDone(_ string, _ int64, _ string, errMessage string) { r.done <- errMessage }

func TestStartEtcdSnapshotEncryptedReportsBadKeys(t *testing.T) {
	r := snapshotResult{done: make(chan string, 1)}

	StartEtcdSnapshotEncrypted("", "", "10.0.0.1", filepath.Join(t.TempDir(), "x.age"), "nope", "", r)

	if msg := <-r.done; !strings.Contains(msg, "not an age") {
		t.Errorf("OnDone error = %q", msg)
	}
}
