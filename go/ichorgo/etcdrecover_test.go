package ichorgo

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"golang.org/x/crypto/ssh"
)

const recoverPassphrase = "correct horse battery staple"

// etcdDB stands for a snapshot's clear bytes: anything not starting like an age file.
var etcdDB = bytes.Repeat([]byte("bbolt-page-"), 4096)

// snapshotFile writes data to a file, encrypted with recipients or passphrase when given.
func snapshotFile(t *testing.T, data []byte, recipients, passphrase string) string {
	t.Helper()

	var buf bytes.Buffer

	if recipients == "" && passphrase == "" {
		buf.Write(data)
	} else {
		wrap, err := snapshotEncryptor(recipients, passphrase)
		if err != nil {
			t.Fatal(err)
		}

		w, err := wrap(&buf)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}

		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(t.TempDir(), "etcd.snapshot")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func decryptFile(t *testing.T, path, identity, passphrase string) ([]byte, error) {
	t.Helper()

	decrypt, err := snapshotDecryptor(identity, passphrase)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close() //nolint:errcheck

	r, err := decrypt(f)
	if err != nil {
		return nil, err
	}

	return io.ReadAll(r)
}

func TestSnapshotDecryptRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	keyFile := snapshotFile(t, etcdDB, id.Recipient().String(), "")
	passFile := snapshotFile(t, etcdDB, "", recoverPassphrase)
	clearFile := snapshotFile(t, etcdDB, "", "")

	tests := []struct {
		name, path, identity, passphrase, wantErr string
	}{
		{"secret key", keyFile, "# my key\n" + id.String() + "\n", "", ""},
		{"passphrase", passFile, "", recoverPassphrase, ""},
		{"clear", clearFile, "", "", ""},
		{"wrong passphrase", passFile, "", "not the passphrase", "does not open the snapshot"},
		{"wrong key", keyFile, other.String(), "", "does not open the snapshot"},
		{"passphrase for a key file", keyFile, "", recoverPassphrase, "does not open the snapshot"},
		{"encrypted, nothing given", keyFile, "", "", "this snapshot is encrypted"},
		{"clear, key given", clearFile, id.String(), "", "is not encrypted"},
		{"not a key", keyFile, "age1notasecret", "", "not an age secret key"},
		{"both", keyFile, id.String(), recoverPassphrase, "not both"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decryptFile(t, tt.path, tt.identity, tt.passphrase)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}

				if key := strings.TrimSpace(tt.identity); key != "" && strings.Contains(err.Error(), key) {
					t.Fatalf("the error quotes the key: %v", err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got, etcdDB) {
				t.Fatalf("decrypted %d bytes, want the %d of the database", len(got), len(etcdDB))
			}
		})
	}
}

func TestSnapshotDecryptArmored(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	aw := armor.NewWriter(&buf)

	w, err := age.Encrypt(aw, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}

	_, _ = w.Write(etcdDB) //nolint:errcheck
	_ = w.Close()          //nolint:errcheck
	_ = aw.Close()         //nolint:errcheck

	path := filepath.Join(t.TempDir(), "etcd.snapshot.age")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := decryptFile(t, path, id.String(), "")
	if err != nil || !bytes.Equal(got, etcdDB) {
		t.Fatalf("armored: %v", err)
	}

	if info, err := readSnapshotInfo(path); err != nil || !info.Encrypted || info.RecipientsHint != snapshotHintX25519 {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

func TestSnapshotInspect(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(etcdDB)

	tests := []struct {
		name      string
		path      string
		encrypted bool
		hint      string
	}{
		{"clear", snapshotFile(t, etcdDB, "", ""), false, ""},
		{"secret key", snapshotFile(t, etcdDB, id.Recipient().String(), ""), true, snapshotHintX25519},
		{"passphrase", snapshotFile(t, etcdDB, "", recoverPassphrase), true, snapshotHintScrypt},
		{"ssh key", snapshotFile(t, etcdDB, sshPublicKey(t), ""), true, snapshotHintUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := SnapshotInspect(tt.path)
			if err != nil {
				t.Fatal(err)
			}

			var info snapshotInfo
			if err := json.Unmarshal([]byte(out), &info); err != nil {
				t.Fatal(err)
			}

			if info.Encrypted != tt.encrypted || info.RecipientsHint != tt.hint {
				t.Errorf("info = %+v", info)
			}

			if !tt.encrypted && (info.Sha256 != hex.EncodeToString(sum[:]) || info.Size != int64(len(etcdDB))) {
				t.Errorf("clear file: size %d sha256 %s", info.Size, info.Sha256)
			}
		})
	}

	if _, err := SnapshotInspect(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file must fail")
	}
}

// sshPublicKey is a fresh ssh-ed25519 public key line: a recipient the phone cannot open.
func sshPublicKey(t *testing.T) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " sample"
}

// recoverRecorder is an EtcdRecoverListener that keeps every progress event.
type recoverRecorder struct {
	mu     sync.Mutex
	events []etcdRecoverProgress
	done   chan string
}

func newRecoverRecorder() *recoverRecorder { return &recoverRecorder{done: make(chan string, 1)} }

func (r *recoverRecorder) OnProgress(s string) {
	var p etcdRecoverProgress
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	r.mu.Lock()
	r.events = append(r.events, p)
	r.mu.Unlock()
}

func (r *recoverRecorder) OnDone(errMessage string) { r.done <- errMessage }

func (r *recoverRecorder) wait(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(60 * time.Second):
		t.Fatal("OnDone not called")

		return ""
	}
}

func (r *recoverRecorder) phases() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string

	for _, p := range r.events {
		if len(out) == 0 || out[len(out)-1] != p.Phase {
			out = append(out, p.Phase)
		}
	}

	return strings.Join(out, ",")
}

// lostCluster is three control planes whose etcd no longer answers.
func lostCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f, cfg := threeControlPlanes(t, true)
	f.etcdDown = map[string]bool{"192.0.2.51": true, "192.0.2.52": true, "192.0.2.53": true}

	return f, cfg
}

func TestStartEtcdRecoverFake(t *testing.T) {
	withDataDir(t)

	f, cfg := lostCluster(t)
	path := snapshotFile(t, etcdDB, "", recoverPassphrase)

	r := newRecoverRecorder()
	StartEtcdRecover(cfg, "fake", "192.0.2.52", path, "", recoverPassphrase, false, r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("recovery failed: %s", msg)
	}

	if p := r.phases(); p != "decrypting,uploading,bootstrapping,waiting,done" {
		t.Errorf("phases = %s", p)
	}

	f.mu.Lock()
	got, boots := f.recovered["192.0.2.52"], f.bootstraps
	f.mu.Unlock()

	if !bytes.Equal(got, etcdDB) {
		t.Errorf("the node received %d bytes, want the %d clear ones", len(got), len(etcdDB))
	}

	if len(boots) != 1 || !boots[0].GetRecoverEtcd() || boots[0].GetRecoverSkipHashCheck() {
		t.Errorf("bootstraps = %v", boots)
	}

	if calls := f.called("EtcdRecover"); len(calls) != 1 || calls[0] != "EtcdRecover 192.0.2.52" {
		t.Errorf("EtcdRecover calls = %v", calls)
	}

	entries := readAudit(t, "fake", "etcd-recover")
	if len(entries) != 1 || entries[0].Node != "192.0.2.52" || !strings.HasPrefix(entries[0].Params, "sha256=") || entries[0].Outcome != auditOK {
		t.Errorf("audit = %+v", entries)
	}

	if strings.Contains(entries[0].Params, recoverPassphrase) {
		t.Error("the audit entry holds the passphrase")
	}
}

func TestStartEtcdRecoverClearWithSkipHashCheck(t *testing.T) {
	withDataDir(t)

	f, cfg := lostCluster(t)

	r := newRecoverRecorder()
	StartEtcdRecover(cfg, "fake", "192.0.2.51", snapshotFile(t, etcdDB, "", ""), "", "", true, r)

	if msg := r.wait(t); msg != "" {
		t.Fatalf("recovery failed: %s", msg)
	}

	// A clear file has nothing to decrypt.
	if p := r.phases(); p != "uploading,bootstrapping,waiting,done" {
		t.Errorf("phases = %s", p)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.bootstraps) != 1 || !f.bootstraps[0].GetRecoverSkipHashCheck() {
		t.Errorf("bootstraps = %v", f.bootstraps)
	}
}

func TestStartEtcdRecoverRefused(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	_, lost := lostCluster(t)
	healthy, live := threeControlPlanes(t, true)
	healthy.etcdDown = map[string]bool{"192.0.2.51": true}

	worker := newFakeTalos()
	worker.addNode(t, "192.0.2.51", "v1.11.0", machine.TypeControlPlane)
	worker.addNode(t, "192.0.2.61", "v1.11.0", machine.TypeWorker)
	worker.etcdDown = map[string]bool{"192.0.2.51": true}
	withWorker := worker.start(t, "192.0.2.51", "192.0.2.61")

	file := snapshotFile(t, etcdDB, "", recoverPassphrase)

	tests := []struct {
		name, cfg, context, node, path, passphrase, want string
	}{
		{"demo", demo, "Demo cluster", "", file, recoverPassphrase, errDemoUnavailable.Error()},
		{"members still answer", live, "fake", "192.0.2.51", file, recoverPassphrase, "etcd still answers on 192.0.2.52, 192.0.2.53"},
		{"worker", withWorker, "fake", "192.0.2.61", file, recoverPassphrase, "192.0.2.61 is not a control plane"},
		{"two nodes", lost, "fake", "192.0.2.51,192.0.2.52", file, recoverPassphrase, "one control plane only"},
		{"unknown node", lost, "fake", "192.0.2.99", file, recoverPassphrase, "not part of this context"},
		{"wrong passphrase", lost, "fake", "192.0.2.51", file, "not the passphrase", "does not open the snapshot"},
		{"no file", lost, "fake", "192.0.2.51", "", "", "no snapshot file given"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRecoverRecorder()
			StartEtcdRecover(tt.cfg, tt.context, tt.node, tt.path, "", tt.passphrase, false, r)

			if msg := r.wait(t); !strings.Contains(msg, tt.want) {
				t.Errorf("OnDone = %q, want %q", msg, tt.want)
			}
		})
	}

	if len(healthy.called("EtcdRecover")) != 0 || len(healthy.called("Bootstrap")) != 0 {
		t.Error("a refused recovery must upload and bootstrap nothing")
	}
}
