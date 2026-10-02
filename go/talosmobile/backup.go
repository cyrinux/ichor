package talosmobile

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// An app backup is the app's JSON payload (talosconfig, settings, per-cluster options; built by
// each app) sealed with a passphrase, so it can leave the device: the stores it comes from are
// bound to the device's keystore. Both apps share this format, so a backup made on Android
// restores on iOS and back.
//
// File layout (all integers big-endian):
//
//	magic    8 bytes  "ICHORBAK"
//	version  1 byte   1
//	kdf      1 byte   1 = Argon2id
//	time     4 bytes  Argon2 passes
//	memory   4 bytes  Argon2 memory, KiB
//	threads  1 byte   Argon2 parallelism
//	salt     16 bytes
//	nonce    12 bytes
//	sealed   AES-256-GCM(payload), header above as additional data
//
// The whole header is authenticated, so lowering the KDF cost or swapping the salt of a file
// fails decryption like a wrong passphrase.

// Error messages start with one of these codes, which the apps map to localized text.
const (
	BackupErrPassphraseShort = "backup-passphrase-short"
	BackupErrWrongPassphrase = "backup-wrong-passphrase"
	BackupErrNotBackup       = "backup-not-a-backup"
	BackupErrUnsupported     = "backup-unsupported"
	BackupErrInvalidContent  = "backup-invalid-content"
)

// BackupMinPassphrase is the shortest passphrase EncryptBackup accepts, in characters.
const BackupMinPassphrase = 12

// BackupPayloadFormat is the payload's "format" field this core understands.
const BackupPayloadFormat = 1

const (
	backupMagic       = "ICHORBAK"
	backupVersion     = 1
	backupKDFArgon2id = 1
	backupSaltLen     = 16
	backupNonceLen    = 12
	backupHeaderLen   = len(backupMagic) + 1 + 1 + 4 + 4 + 1 + backupSaltLen + backupNonceLen
	backupKeyLen      = 32

	// OWASP's Argon2id guidance, with more memory: a few hundred milliseconds on a phone.
	backupTime    = 3
	backupMemory  = 64 * 1024
	backupThreads = 4

	// Bounds on what a file may ask for, so a crafted backup cannot exhaust memory or CPU.
	backupMaxTime    = 10
	backupMaxMemory  = 256 * 1024
	backupMaxThreads = 16

	// A talosconfig with many clusters and the settings stay far below this.
	backupMaxPayload = 4 << 20
)

type backupParams struct {
	time    uint32
	memory  uint32
	threads uint8
	salt    []byte
	nonce   []byte
}

// EncryptBackup seals a backup payload (JSON) with a passphrase and returns the file contents.
func EncryptBackup(payloadJSON, passphrase string) (out []byte, err error) {
	defer maskErr(&err)

	if utf8.RuneCountInString(passphrase) < BackupMinPassphrase {
		return nil, fmt.Errorf("%s: use at least %d characters", BackupErrPassphraseShort, BackupMinPassphrase)
	}

	if _, err := validateBackupPayload([]byte(payloadJSON)); err != nil {
		return nil, err
	}

	p := backupParams{
		time:    backupTime,
		memory:  backupMemory,
		threads: backupThreads,
		salt:    make([]byte, backupSaltLen),
		nonce:   make([]byte, backupNonceLen),
	}

	if _, err := rand.Read(p.salt); err != nil {
		return nil, fmt.Errorf("random salt: %w", err)
	}

	if _, err := rand.Read(p.nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}

	header := p.header()

	aead, err := backupAEAD(passphrase, p)
	if err != nil {
		return nil, err
	}

	return aead.Seal(header, p.nonce, []byte(payloadJSON), header), nil
}

// DecryptBackup opens a backup file with its passphrase and returns the validated payload JSON.
func DecryptBackup(backup []byte, passphrase string) (out string, err error) {
	defer maskErr(&err)

	p, err := parseBackupHeader(backup)
	if err != nil {
		return "", err
	}

	aead, err := backupAEAD(passphrase, p)
	if err != nil {
		return "", err
	}

	header := backup[:backupHeaderLen]

	plain, err := aead.Open(nil, p.nonce, backup[backupHeaderLen:], header)
	if err != nil {
		return "", fmt.Errorf("%s: wrong passphrase or damaged file", BackupErrWrongPassphrase)
	}

	if _, err := validateBackupPayload(plain); err != nil {
		return "", err
	}

	return string(plain), nil
}

func (p backupParams) header() []byte {
	var b bytes.Buffer

	b.WriteString(backupMagic)
	b.WriteByte(backupVersion)
	b.WriteByte(backupKDFArgon2id)
	_ = binary.Write(&b, binary.BigEndian, p.time)
	_ = binary.Write(&b, binary.BigEndian, p.memory)
	b.WriteByte(p.threads)
	b.Write(p.salt)
	b.Write(p.nonce)

	return b.Bytes()
}

func parseBackupHeader(backup []byte) (backupParams, error) {
	if len(backup) < backupHeaderLen+16 || string(backup[:len(backupMagic)]) != backupMagic {
		return backupParams{}, fmt.Errorf("%s: not an Ichor backup", BackupErrNotBackup)
	}

	if len(backup) > backupMaxPayload+backupHeaderLen+16 {
		return backupParams{}, fmt.Errorf("%s: file too large", BackupErrNotBackup)
	}

	rest := backup[len(backupMagic):]
	if rest[0] != backupVersion || rest[1] != backupKDFArgon2id {
		return backupParams{}, fmt.Errorf("%s: backup version %d, kdf %d", BackupErrUnsupported, rest[0], rest[1])
	}

	rest = rest[2:]
	p := backupParams{
		time:    binary.BigEndian.Uint32(rest[0:4]),
		memory:  binary.BigEndian.Uint32(rest[4:8]),
		threads: rest[8],
	}
	rest = rest[9:]
	p.salt = rest[:backupSaltLen]
	p.nonce = rest[backupSaltLen : backupSaltLen+backupNonceLen]

	if p.time == 0 || p.time > backupMaxTime || p.memory < 8*uint32(p.threads) ||
		p.memory > backupMaxMemory || p.threads == 0 || p.threads > backupMaxThreads {
		return backupParams{}, fmt.Errorf("%s: key derivation parameters out of range", BackupErrUnsupported)
	}

	return p, nil
}

func backupAEAD(passphrase string, p backupParams) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(passphrase), p.salt, p.time, p.memory, p.threads, backupKeyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

// backupPayload is the part of the payload the core checks; the apps own the other fields.
type backupPayload struct {
	Format      int    `json:"format"`
	Talosconfig string `json:"talosconfig"`
}

func validateBackupPayload(raw []byte) (backupPayload, error) {
	if len(raw) > backupMaxPayload {
		return backupPayload{}, fmt.Errorf("%s: payload too large", BackupErrInvalidContent)
	}

	var p backupPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return backupPayload{}, fmt.Errorf("%s: %w", BackupErrInvalidContent, err)
	}

	if p.Format < 1 {
		return backupPayload{}, fmt.Errorf("%s: missing format", BackupErrInvalidContent)
	}

	if p.Format > BackupPayloadFormat {
		return backupPayload{}, fmt.Errorf("%s: payload format %d, update the app", BackupErrUnsupported, p.Format)
	}

	cfg, err := loadConfig(p.Talosconfig)
	if err != nil {
		return backupPayload{}, fmt.Errorf("%s: %w", BackupErrInvalidContent, err)
	}

	if len(cfg.Contexts) == 0 {
		return backupPayload{}, errors.New(BackupErrInvalidContent + ": no cluster in the talosconfig")
	}

	return p, nil
}
