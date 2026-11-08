# etcd snapshots

Part of the [Ichor documentation](../README.md#documentation).


**etcd → Backup → Save snapshot…** saves the etcd database, like `talosctl etcd snapshot`
(`os:operator`, `os:etcd:backup` or `os:admin`), to a file you choose. The snapshot holds every
Kubernetes Secret, so it is **encrypted with [age](https://age-encryption.org) by default**, while
it downloads: the clear database never touches the phone's storage. Choose how:

- **Public keys** (default once you have saved some for the cluster): one or more age (`age1…`),
  SSH (`ssh-ed25519`, `ssh-rsa`, e.g. `~/.ssh/id_ed25519.pub`) or YubiKey keys. For a YubiKey,
  paste the `age1tag1…` form of its key, printed by a recent
  [age-plugin-yubikey](https://github.com/str4d/age-plugin-yubikey); the older `age1yubikey1…`
  form needs the plugin to encrypt and is refused. The phone holds nothing that can decrypt the
  file. The keys are remembered per cluster (they are not secret).
- **Passphrase**: at least 12 characters, never stored.
- **None**: the old clear file, with a warning.

The file is named `etcd-<cluster>-<node>-<date>.snapshot.age`. After saving, **How to restore**
shows the commands for the file. On any Unix machine:

```sh
# install age: apt install age | dnf install age | pacman -S age | brew install age
age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot etcd-….snapshot.age   # public key (or -i key.txt)
age -d -i age-yubikey-identity-….txt -o etcd.snapshot etcd-….snapshot.age  # YubiKey, needs age-plugin-yubikey
age -d -o etcd.snapshot etcd-….snapshot.age                         # passphrase (prompts)
sha256sum etcd.snapshot   # must match the SHA-256 the app showed
talosctl -n <control-plane-ip> bootstrap --recover-from=./etcd.snapshot
```

Post-quantum `age1pq1…` / `age1tagpq1…` keys need age 1.3 or later to decrypt.

Or from the phone, when no etcd member answers any more: **etcd → Recover from snapshot…**
(os:admin) picks the file, asks for its passphrase or age secret key (`AGE-SECRET-KEY-1…`, never
stored), the control plane to recover on, the typed cluster name and an acknowledgement, then
uploads it and bootstraps etcd there with one member. The file is decrypted while it uploads: the
clear database never touches the phone's storage. A snapshot encrypted for a YubiKey or an SSH key
cannot be opened on the phone: decrypt it on a laptop first. The action is refused while any
member still answers (a recovery would split a live cluster). Afterwards, reset the other control
planes so they join the new one, as in the
[Talos disaster recovery guide](https://www.talos.dev/latest/advanced/disaster-recovery/).

### NOSPACE fix

When etcd raises its `NOSPACE` alarm, the cluster accepts no more writes. **etcd → Alarms → Fix
NOSPACE…** (os:admin) runs the usual sequence as one followed run:

1. A snapshot, as above, unless you switch it off.
2. A defragmentation of every member, one at a time: followers first, the leader last.
3. The alarm disarmed.
4. etcd read again.

The run stops at the first failure and says which member failed. When the alarm comes back at
once, the database is still over its quota: raise `quota-backend-bytes` in the machine config,
or delete data.
