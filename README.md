# Talos Viewer

Android app for a [Talos](https://www.talos.dev) cluster:

- **Viewing:** node status, services, resources, logs, etcd and the cluster health check.
- **Actions:** reboot or shut down a node.
- **Kubernetes:** export a kubeconfig to open the cluster in kubenav.
- **Background:** alerts and a home-screen widget.
- **Protection:** optional fingerprint/PIN lock.

The Talos API layer is the official Go client (`siderolabs/talos/pkg/machinery`, same code as
`talosctl`) compiled with gomobile, so talosconfig parsing, Ed25519 mTLS and endpoint→node
proxying behave exactly like `talosctl`.

```
app/            Kotlin + Jetpack Compose UI
go/talosmobile  Go core exposed to Kotlin (JSON in/out)
go/cmd/probe    desktop CLI calling the same Go functions
flake.nix       Nix build environment (default)
build/          amd64 Docker toolchain (CI)
```

## Features and the talosconfig role they need

Talos enforces access with the roles baked into the talosconfig client certificate. The app
reads those roles and explains up front when a feature needs more.

| Feature | Talos API | Minimum role |
|---|---|---|
| Cluster overview (readiness, stage, version, role) | `Version`, COSI resources | `os:reader` |
| Node services and resources | `ServiceList`, `Memory`, `LoadAvg`, `SystemStat`, `CPUInfo`, `Mounts` | `os:reader` |
| Service logs and kernel log (dmesg) | `Logs`, `Dmesg` | `os:reader` |
| etcd members, leader, DB size, alarms | `EtcdMemberList`, `EtcdStatus`, `EtcdAlarmList` | `os:reader` |
| Background alerts and widget | same as the overview and etcd | `os:reader` |
| **Reboot (`-m default\|powercycle\|force`) / shutdown (`--force`)** | `Reboot`, `Shutdown` | **`os:operator`** |
| **Cluster health check** | `ClusterService/HealthCheck` | **`os:admin`** |
| **Kubeconfig export** | `Kubeconfig` | **`os:admin`** |

Notes:

- **Source:** the rules come from Talos v1.14 (`internal/app/machined/pkg/system/services/machined.go`).
- **Health check:** the call itself is allowed for `os:reader`. Talos then runs the check with the
  caller's roles, and the Kubernetes checks need an admin kubeconfig. With a lower role, the
  overview and etcd screens give the same node and etcd picture.
- **Roles combine:** an `os:operator` config can do everything an `os:reader` one can.

### Creating a talosconfig for the phone

Generate a dedicated identity instead of copying your admin `~/.talos/config`. If the phone is
lost, its certificate only grants what you chose, and it expires on its own.

`talosctl config new` needs exactly one node to sign the certificate: use a control-plane node.

```sh
CP=10.0.0.2   # any control-plane node

# Monitoring only (recommended default):
talosctl -n $CP config new talosconfig-phone --roles os:reader --crt-ttl 8760h

# Monitoring + reboot/shutdown:
talosctl -n $CP config new talosconfig-phone --roles os:operator --crt-ttl 8760h

# Everything, including the health check and kubeconfig export (treat the phone like a laptop):
talosctl -n $CP config new talosconfig-phone --roles os:admin --crt-ttl 8760h
```

Check what was generated. The app queries the context's `nodes`, falling back to its
`endpoints` when the list is empty, so make sure every node is listed:

```sh
talosctl --talosconfig talosconfig-phone config info
talosctl --talosconfig talosconfig-phone config node 10.0.0.2 10.0.0.3 ...   # all nodes
talosctl --talosconfig talosconfig-phone -n $CP version                                # sanity check
```

The `talosconfig*` filenames are gitignored in this repository.

**Renewal:** the certificate lasts `--crt-ttl` (default one year). Settings shows the expiry
date. When background alerts are on, the app notifies daily from 14 days before expiry. To
renew, generate a new file and import it (Settings → Import a new talosconfig).

### Importing it

Pick one of these:

- **File:** `adb push talosconfig-phone /sdcard/Download/`, then choose it in the app.
- **Paste:** paste the YAML.
- **QR code:** run `qrencode -r talosconfig-phone -o talosconfig.png` and scan it. This works
  for configs up to about 2.9 KB.

Delete `talosconfig.png` and the copy in `Download/` afterwards; both contain the private key.
The app stores the config AES-GCM encrypted with an Android Keystore key, excluded from backups.

## Safety features

- **Reboot / shutdown:** the same options as talosctl, from the Talos v1.14 sequencer:

  | Option | What happens |
  |---|---|
  | Reboot `default` | Stop pods and services, then reboot (kexec fast reboot when available). No cordon/drain. |
  | Reboot `powercycle` | Same graceful stop, then a full firmware reboot instead of kexec. |
  | Reboot `force` | Reboot immediately, with no pod or service shutdown. Only for a stuck node. |
  | Shutdown `--force` | Skip the Kubernetes cordon/drain, e.g. when the API is down. Pods and services are still stopped. |

  Every option goes through the same confirmation:
  1. You type the node's hostname to confirm.
  2. Control-plane nodes get an etcd-quorum warning.
  3. With the app lock on, you also authenticate with fingerprint or PIN.
- **App lock (Settings → Security):**
  - **Methods:** fingerprint, with the device PIN, pattern or password as fallback.
  - **When it asks:** at launch, after 30 s in the background, and before reboot, shutdown and kubeconfig export.
  - **Privacy:** it hides the app in recent apps and blocks screenshots, unless you turn on
    "Allow screenshots", which also requires authenticating.
  - **Changing it:** turning it on or off requires authenticating.
- **Notifications:** with the app lock on, alerts show only a generic text on the lock screen.
  The widget shows counts only, no hostnames.
- **Kubeconfig export:** the kubeconfig is written only to the file you choose. kubenav has no
  app-to-app import, so add the cluster from that file in kubenav, then delete the file.

## Build

```sh
./build.sh            # debug APK  -> app/build/outputs/apk/debug/app-debug.apk
./build.sh release    # needs TALOS_KEYSTORE, TALOS_KEYSTORE_PASSWORD, TALOS_KEY_ALIAS, TALOS_KEY_PASSWORD
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

- **Default toolchain:** Nix (`flake.nix`). `BUILDER=docker ./build.sh` uses `build/Dockerfile`
  instead, and `IN_CONTAINER=1 ./build.sh` runs inside that image (CI, e.g. a Forgejo runner).
- **aarch64 Linux hosts:** the Android SDK and NDK are x86_64-only, and the Go runtime and the
  NDK clang crash under qemu-user. `build.sh` therefore keeps Go, gomobile, the JDK and Gradle
  native:
  - gomobile is built from a patched copy, because upstream refuses linux/arm64;
  - cgo uses a native clang 18 with the NDK sysroot (`.cache/ndk`);
  - only `aapt2` runs emulated.
- **Nix sandbox:** for the x86_64 SDK builds, `build.sh` exposes the binfmt qemu closure to the
  sandbox, which requires a trusted Nix user.
- **Page size:** native libraries are linked with 16 KB page alignment, as Android 15+ requires.

Go core only (fast, native):

```sh
cd go && go test ./...
go run ./cmd/probe overview   # also: services NODE, resources NODE, logs NODE SERVICE, dmesg NODE,
                              #       etcd, health, kubeconfig (prints size only), parse
go run ./cmd/probe -config ../talosconfig-phone overview   # test a role-limited config
```
