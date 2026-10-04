# Encrypted etcd snapshots

Status: **implemented** (Go, Android, iOS; YubiKey through `age1tag1` keys). Part of S1 (etcd care) in [../roadmap/sysadmin.md](../roadmap/sysadmin.md).

## Why

`StartEtcdSnapshot` (`go/ichorgo/etcdbackup.go`) writes the etcd database in clear: every
Kubernetes Secret is readable by anyone who gets the file (cloud drive, mail, a lost phone's
Downloads). The app only warns. The snapshot must be encrypted by default, and still be
restorable from any Unix machine with standard tools and short instructions.

## Key decisions

| # | Decision | Why |
|---|----------|-----|
| E1 | **[age](https://age-encryption.org) format** (`filippo.io/age`). | One small, audited Go library; the `age` CLI (or `rage`) is packaged everywhere (apt, dnf, pacman, brew, nix, Alpine); the same tool SOPS users already have. No custom crypto, no custom format. |
| E2 | **Two modes**: **public keys** (one or more `age1…`, `age1pq1…`, SSH `ssh-ed25519` / `ssh-rsa`, or YubiKey `age1tag1…` / `age1tagpq1…` keys) or a **passphrase**. `age1yubikey1…` is refused with a hint: encrypting to it needs the plugin binary, while age encrypts to the `age1tag1` form natively. | Public keys: the phone never holds anything that decrypts the backup, and an existing `~/.ssh/id_ed25519` works with no setup. Passphrase: nothing to prepare. |
| E3 | **Encrypt while streaming**: Go wraps the file writer in `age.Encrypt`; the clear database never touches the phone's storage. | The `.part` file is already ciphertext. |
| E4 | **Encrypted is the default**; saving in clear stays possible behind an explicit choice with the existing warning. | Safe by default without breaking anyone's workflow. |
| E5 | **The SHA-256 shown is the clear snapshot's**, labelled so, and is part of the instructions (`sha256sum` after decrypting). | Proves the decrypted file is the one etcd sent. |
| E6 | **Passphrase: scrypt work factor 2^16** (≈ 64 MB, instead of age's 2^18 = 256 MB) and at least 12 characters, typed twice; never stored. | 256 MB can be too much for a phone process; `age -d` accepts up to 2^22, so the file still opens everywhere. |
| E7 | **Public keys are remembered per cluster** (they are not secret) in the app's preferences, so the next backup is one tap. | The common case: the same admin key every time. |
| E8 | **File name ends in `.snapshot.age`** (clear: `.snapshot` as today). | Tells what to do with the file. |
| E9 | **Restore instructions in the app** (after saving, copyable, with the right command for the mode) **and in README**. | "Usable from a Unix machine" means the steps travel with the file. |

## Restore on a Unix machine

```sh
# install age: apt install age | dnf install age | pacman -S age | brew install age | nix-shell -p age
# public key mode (SSH key or age identity file):
age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot etcd-….snapshot.age
age -d -i key.txt           -o etcd.snapshot etcd-….snapshot.age
# YubiKey (age1tag1 key; needs age-plugin-yubikey and the key plugged in):
age -d -i age-yubikey-identity-….txt -o etcd.snapshot etcd-….snapshot.age
# passphrase mode (prompts):
age -d -o etcd.snapshot etcd-….snapshot.age

sha256sum etcd.snapshot      # must match the SHA-256 the app showed
talosctl -n <control-plane> bootstrap --recover-from=./etcd.snapshot
```

`age1pq1…` (post-quantum) keys need age ≥ 1.3 to decrypt.

## Go

- `etcdbackup.go`: `StartEtcdSnapshotEncrypted(config, ctx, node, destPath, recipients, passphrase, listener)`;
  `StartEtcdSnapshot` stays (clear). `writeSnapshot` takes an optional `wrap func(io.Writer) (io.WriteCloser, error)`;
  the hash and progress count clear bytes.
- `snapshotcrypt.go`: `parseSnapshotRecipients(text)` (one key per line, `#` comments, SSH keys
  through `agessh`, refuses private keys and empty input), `CheckSnapshotRecipients(text) (json)`
  for the UI (type + comment per key, or a line-numbered error), `snapshotEncryptor(recipients, passphrase)`.
- Tests: round trip with an X25519 identity, an SSH ed25519 key and a passphrase (decrypt with
  `age.Decrypt` and compare the hash), bad keys, private key pasted by mistake, short passphrase,
  failure leaves no file.
- Probe: `snapshot-probe` takes `-age-recipient`.

## Apps (both)

- Snapshot confirm step becomes a small sheet: **Encryption**: Public keys (default when keys are
  saved) / Passphrase / None (shows the clear-text warning). Keys field: multi-line, validated
  live with `CheckSnapshotRecipients`, "Paste from clipboard"; passphrase + confirmation.
- Done panel: "Encrypted with age for N keys" or "with a passphrase", the clear SHA-256, and
  **How to restore** (copy the commands above, filled with the file name and mode).
- Strings in en, fr, de, es, it, uk (Android `values-*`, iOS `Localizable.xcstrings`).

## Next: app backups in age too (proposed, not built)

App backups (`backup.go`) use Ichor's own `ICHORBAK` format: Argon2id (3 passes, 64 MiB,
4 threads) → AES-256-GCM, header authenticated. Sound, but only Ichor can open it, and the
payload holds the talosconfig: with the phone gone, the cluster access is stuck in the file.

Proposal: backup format **version 2 = age with a passphrase** (scrypt 2^16, same ≥ 12
characters), the JSON payload unchanged. Restore keeps reading version 1 forever. Then
`age -d ichor-backup.age | jq -r .talosconfig > talosconfig` recovers access from any Unix
machine. Passphrase only: a public-key backup could not be restored on a new phone without
putting the private key on it.

## Not in scope

Encrypting other exports (support bundle, kubeconfig export), uploading backups anywhere,
scheduled backups.
