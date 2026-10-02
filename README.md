# Ichor for Talos Linux

**Your Talos cluster, from your phone.** Ichor is a free, open-source Android and iOS app to
monitor and operate [Talos Linux](https://www.talos.dev) clusters: node health, logs, etcd,
KubeSpan and live graphs in your pocket, and a notification the moment a node goes down.

Website: <https://cyrinux.github.io/ichor/>

Want to explore without a Talos cluster? Tap **Try demo** on the import screen.
The built-in demo works offline and shows five sample nodes, live metrics, services,
logs, events, etcd, networking, storage, hardware and resources. The overview marks
it as demo data; cluster changes and credential exports are unavailable. It can
sit alongside your real clusters and be removed from **Manage clusters**.

> Ichor is an independent community project, not affiliated with or endorsed by Sidero Labs.
> Talos is a trademark of Sidero Labs, Inc.

- **Viewing:** node status, services, resources, logs, etcd and the cluster health check.
- **Actions:** reboot or shut down a node, and wake a powered-off one with Wake-on-LAN (set a
  MAC address and, optionally, a broadcast address and port per node, from its action sheet;
  the phone sends the magic packet itself, so it has to reach the node's network). The MACs of
  each node's network cards are recorded on the phone while it is up, so a node that goes down
  before it was set up can still be woken.
- **Kubernetes:** export a kubeconfig to open the cluster in kubenav.
- **Background:** alerts and a home-screen widget.
- **Several clusters:** switch from the header, give each one a color and a name of your own, and
  open any of them straight from the app icon (long press: a shortcut / quick action per cluster).
- **Protection:** optional fingerprint/PIN lock.
- **AI diagnosis:** optional, off by default: ask Claude or an OpenAI model what is wrong and
  how to fix it, or hand the question to an assistant app (see [AI diagnosis](#ai-diagnosis-optional)).

The Talos API layer is the official Go client (`siderolabs/talos/pkg/machinery`, same code as
`talosctl`) compiled with gomobile, so talosconfig parsing, Ed25519 mTLS and endpoint→node
proxying behave exactly like `talosctl`.

[<img src="https://raw.githubusercontent.com/ImranR98/Obtainium/main/assets/graphics/badge_obtainium.png" alt="Get it on Obtainium" height="54">](https://apps.obtainium.imranr.dev/redirect?r=obtainium://app/%7B%22id%22%3A%22name.levis.ichor.foss%22%2C%22url%22%3A%22https%3A%2F%2Fgithub.com%2Fcyrinux%2Fichor%22%2C%22author%22%3A%22cyrinux%22%2C%22name%22%3A%22Ichor%22%2C%22additionalSettings%22%3A%22%7B%5C%22apkFilterRegEx%5C%22%3A%20%5C%22%5E%28%3F%21.%2Aunsigned%29.%2A%5C%5C%5C%5C.apk%24%5C%22%7D%22%7D)

**Install on Android:**

- **[Obtainium](https://obtainium.imranr.dev)** installs and updates the app straight from this
  repository's GitHub releases. The button above pre-fills it, with a filter that skips
  unsigned APKs.
- **Manually:** download `ichor-v<version>-<abi>.apk` from the
  [latest release](https://github.com/cyrinux/ichor/releases/latest); `arm64-v8a`
  fits almost all phones.

**F-Droid:** not listed yet. The planned route is [IzzyOnDroid](https://apt.izzysoft.de/fdroid/),
an F-Droid repository that ships the signed release APKs.

```
app/            Android: Kotlin + Jetpack Compose UI
ios/            iOS: SwiftUI app (XcodeGen) + IchorCore Swift package
go/talosmobile  Go core exposed to Kotlin and Swift (JSON in/out)
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
| AI diagnosis report (optional) | the calls above, `Time`, `Events`, `Containers`, `NodeStatus`, `StaticPodStatus` | `os:reader` |
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
renew, generate a new file and import it (Settings → Add a cluster): a cluster already
imported, i.e. the same context name and CA, is updated in place.

### Importing it

Pick one of these:

- **File:** `adb push talosconfig-phone /sdcard/Download/`, then choose it in the app.
- **Paste:** paste the YAML.
- **QR code:** run `qrencode -r talosconfig-phone -o talosconfig.png` and scan it. This works
  for configs up to about 2.9 KB.

Delete `talosconfig.png` and the copy in `Download/` afterwards; both contain the private key.
The app stores the config AES-GCM encrypted with an Android Keystore key, excluded from backups.

### Several clusters

Every context of a talosconfig is a cluster in the app, and importing another talosconfig
adds its contexts to the ones already there (like `talosctl config merge`: a context whose
name is taken by another cluster is added as `name-1`). To switch cluster:

- **Android:** swipe the overview's top bar left or right, or tap its title for the list.
- **iOS:** swipe the row of dots under the overview's title, or tap it for the list.

The list is also where a cluster is removed (its credentials are deleted from the device) and
where its color is chosen. Each cluster gets a color of its own, and the app's palette (light,
dark and true black alike) is generated from the color of the cluster on screen, so it is
always clear which cluster a reboot is about to hit. Background alerts and the widget follow
the cluster on screen.

### Backup and restore

The stored config's key never leaves the phone, so moving to a new phone takes a backup:
**Settings → Back up clusters and settings** (with the app lock on, you authenticate
first). Restore it from the same section, or from **Restore a backup** on the first screen of
a fresh install. Backups move between Android and iOS.

- **What it holds:** the talosconfig (every cluster's credentials), the cluster on screen,
  theme, language, screenshot mode, monitoring, and each cluster's name, color, VPN-only
  setting and Wake-on-LAN targets (the last two exist on Android only).
- **What it leaves out:** AI diagnosis settings and API keys, the app lock and "Allow
  screenshots" (set them again on the new phone).
- **Encryption:** a passphrase of at least 12 characters. The key comes from Argon2id (64 MiB,
  3 passes) and the file is sealed with AES-256-GCM. Without the passphrase it cannot be opened,
  and the passphrase cannot be recovered.
- **Restoring replaces** every cluster and setting on the phone with those of the backup.

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

## AI diagnosis (optional)

Off by default. Turn it on in Settings → AI diagnosis; until then the app shows no trace of it
and never contacts a model provider.

- **What it does:** it reads the cluster state into a report, shows you that report, and only
  when you tap *Ask* sends it to the model you chose. The answer names the likely cause, the
  evidence and the steps to fix it, and is written as it arrives.
- **The report:** node readiness and stage, Talos services, memory, disks, clock offset, etcd,
  control-plane static pods, pods without a running container, recent warning and error events,
  and the last 40 log lines of services that are not healthy. It never contains the
  talosconfig, a machine configuration or a kubeconfig.
- **Anonymized by default:** the nodes' names, IP addresses and domain are replaced with
  placeholders (`cp-1`, `10.0.0.7`, `homelab.lan`) before the report leaves the phone, and the
  real ones are put back in the answer on the phone. The mask only knows the cluster's own
  names: the rest of the log lines (pod names, other host names such as a registry or an API
  endpoint) is sent as it is, so read the report first if your logs are sensitive.
- **Providers:** Anthropic (Claude, default `claude-opus-5-5`) or OpenAI (default
  `gpt-6-astra`), with your own API key; any model can be typed or picked from the provider's
  list. The key is stored encrypted (Android Keystore, iOS Keychain) and is only sent over
  HTTPS, to the provider or to the server URL you set for it.
- **Your own server:** set *Server URL* to a gateway or a local model speaking the Anthropic
  Messages API (`https://host`) or the OpenAI Chat Completions API (`http://host:11434/v1`).
  A server that needs no key may use plain HTTP; the report then crosses your network in
  clear, so keep that to a network you trust.
- **Without an API key:** *Share with an assistant app* hands the same question and report to
  the Claude, ChatGPT or Gemini app (or any app taking text) through the system share sheet.
- **Check the answer:** a model can be wrong. Read a command before running it, especially one
  that resets a node or changes etcd membership.

From a workstation, the same code runs against a real cluster:

```sh
cd go
go run ./cmd/probe diagnose-report        # the anonymized report; nothing is sent
# These two send the report with the real names (not anonymized) and print the answer:
ANTHROPIC_API_KEY=... go run ./cmd/probe diagnose anthropic
OPENAI_API_KEY=... go run ./cmd/probe diagnose openai gpt-6-astra
```

## Cluster insights

Open **Cluster insights** from the overview to compare selected effective settings or
record an incident. These features are read-only and work with `os:reader`.

- **Configuration drift:** compares DNS, NTP, interface MTUs, extension versions,
  Secure Boot, UKI and Talos versions. Without a saved baseline, each role is compared
  with its first reachable node; save a baseline to compare the same nodes over time.
  Differences can be intentional. Failed or unavailable fields stay unknown, and a
  node absent from the baseline is not treated as a configuration change.
- **Incident recorder:** collects live Talos events and samples node readiness,
  service state, network link state and counters about every five seconds (plus API
  call time). It stops after ten minutes or when its screen leaves the foreground.
  Review the latest saved timeline when you return; starting a new session replaces
  it. The timeline retains up to 600 entries, with bounded detail per entry, and
  reports discarded older entries. Metric samples can be shown or hidden.
- **Bottleneck metrics:** the node’s **Live** tab now includes CPU I/O wait and VM
  steal time, per-disk throughput/activity/average I/O time, and per-interface
  throughput/errors/drops. Rates use consecutive counters; rebooted nodes, new
  devices and failed reads do not produce misleading rate spikes.

Baselines and recordings are encrypted on the device, excluded from backups, and
separated by cluster and screenshot-mode settings. Delete them from the insights
screen. No machine configurations, kubeconfigs or raw logs are collected. Event
messages and node details can still contain operational information.

The recorder is a foreground troubleshooting tool, not continuous monitoring.
Sampled changes between polls can be missed. Events use node clocks, sampled
changes use the phone clock, and chronological proximity does not establish cause.
Disk activity alone does not establish saturation, especially on parallel devices.

## Updates

The Android app updates itself from this repository's GitHub releases (Settings → Updates):

- **When it checks:** once a day, or when you tap "Check now".
- **What it downloads:** the signed APK for the phone's ABI, after you confirm.
- **Verification before installing:**
  1. the SHA-256 checksum GitHub publishes for the file;
  2. the package name;
  3. that it is signed with exactly the installed app's key.
- **Installing:** Android shows its own confirmation, and asks once to allow
  "Install unknown apps" for Ichor.

Updates only work between builds signed with the same key, which means CI release builds with
the `ICHOR_KEYSTORE*` secrets set. A **debug** build, signed with your machine's debug key,
cannot be replaced by a release. To switch once:

1. uninstall the debug build;
2. install `ichor-vX.Y.Z-<abi>.apk` from a release;
3. re-import the talosconfig.

There is no self-update on iOS, where sideloaded apps are reinstalled with Sideloadly or AltStore.

## Build

APKs are split per ABI (`arm64-v8a` for almost all phones, `armeabi-v7a`, `x86_64`), with
compressed native libraries. A release APK is about 16 MB.

```sh
./build.sh            # debug APKs -> app/build/outputs/apk/debug/app-<abi>-debug.apk
./build.sh release    # needs ICHOR_KEYSTORE, ICHOR_KEYSTORE_PASSWORD, ICHOR_KEY_ALIAS, ICHOR_KEY_PASSWORD
just install           # builds, then installs the APK matching the connected device's ABI
./build.sh play       # Google Play App Bundle -> app/build/outputs/bundle/play/app-play.aab
```

The Play build is the release build without the self-updater (Play delivers updates) and
without the donation links. Its store listing, privacy policy and Console answers are in
`fastlane/` (see [fastlane/PLAY_CONSOLE.md](fastlane/PLAY_CONSOLE.md)).

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

## iOS

The iOS app (`ios/`) reuses the same Go core, built as `Talosmobile.xcframework` with
gomobile, around a SwiftUI UI. The pure logic lives in the `IchorCore` Swift package:
models, formatting, lock state and power-request rules.

**Feature parity with Android:**

- import from a file, by pasting, or by QR code (VisionKit);
- the config is encrypted to a Secure Enclave key (Keychain fallback without one);
- overview, node services, resources, logs and live graphs;
- etcd, KubeSpan and the cluster health check;
- reboot and shutdown with the same modes and typed confirmation;
- the debug shell (SwiftTerm terminal);
- kubeconfig export;
- background alerts and a home-screen widget;
- Face ID / passcode lock, themes, and role-based actions.

**What iOS does differently:**

- **Self-update:** not possible for sideloaded apps; reinstall new releases with Sideloadly or
  AltStore.
- **Screenshots:** iOS does not let apps block them. The app-switcher privacy cover hides the
  content instead.
- **Background alerts:** best effort. iOS decides when background checks run, and skips them
  while the device is locked, because the config cannot be decrypted then.
- **Leftover data:** Keychain items survive uninstalling the app. Use Settings → Delete to
  remove the config.
- **Widget:** shares the last check with the app through an App Group, which a free Apple ID
  may not support when sideloading. In that case the widget stays empty.

**Building:** Xcode only runs on macOS.

```sh
just ios-test-linux   # core package tests, here on Linux (Nix swift shell)
just ios-test         # macOS: xcframework + core tests + simulator build
just ios-build        # macOS: unsigned IPA in ios/build/
```

**Installing:** CI produces an unsigned IPA. Sideloadly or AltStore re-sign it with your Apple
ID: a free account means reinstalling every 7 days, a paid one lasts a year. To sign in Xcode
instead, set `ICHOR_IOS_TEAM_ID` before `xcodegen generate`.

## CI (GitHub Actions)

- **`.github/workflows/android.yml`** (Ubuntu, Nix flake): `./build.sh check`, then the debug
  and release APKs.
- **`.github/workflows/ios.yml`** (macOS 15): core tests, simulator build, unsigned IPA.

Both run on pushes to `main` and on pull requests. Release APKs are named
`ichor-v<version>-<abi>.apk`, or `…-unsigned.apk` when no keystore secrets are set;
the updater ignores unsigned ones. On a `v*` tag (`just release-tag 0.1.0`,
then `git push origin v0.1.0`), they attach the release APK and the IPA to the GitHub release.

To sign the release APK in CI, generate a key once and upload it as repository secrets:

```sh
just android-keystore-gen       # then export ICHOR_KEYSTORE, ICHOR_KEYSTORE_PASSWORD, ICHOR_KEY_ALIAS
just github-secrets             # sets the three secrets below with gh (values via stdin)
```

The secrets it sets:

| Secret | Value |
|---|---|
| `ICHOR_KEYSTORE_BASE64` | `base64 -w0 ~/ichor-release.jks` |
| `ICHOR_KEYSTORE_PASSWORD` | the keystore password |
| `ICHOR_KEY_ALIAS` | the key alias, e.g. `ichor` |

Without them, the release APK is unsigned.

## Languages

The app is available in English, French, Spanish, Ukrainian, German and Italian.

- **Android:** pick a language in Settings → Appearance → Language. On Android 13+, the
  system's per-app language settings work too.
- **iOS:** iOS sets the app's language in the Settings app; the app's Language row opens it.

When you add or change a UI string, translate it in all six languages:

| Platform | English source | Translations |
|---|---|---|
| Android | `app/src/main/res/values/strings.xml` | `values-{fr,es,uk,de,it}/strings.xml` |
| iOS | `ios/**/Localizable.xcstrings` | the same catalogs (String Catalogs) |

Then run:

```sh
just i18n-check   # also part of `just check` and the iOS CI build
```

It fails on a missing translation, or when placeholders (`%1$s`, `%@`, `%lld`) differ from
English.

## Support the project

Ichor is free, with no ads or tracking. To help keep it going:

- [GitHub Sponsors](https://github.com/sponsors/cyrinux)
- Bitcoin: `bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl`
- Ethereum: `0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804`

The app's About section and the [website](https://cyrinux.github.io/ichor/#support) show them as QR codes.

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE) for third-party components.

### Android demo deep link

Use `ichor://demo` in Google Play Console's Pre-launch report deep link settings
after uploading a build containing this support. It opens the offline demo overview
and preserves imported clusters. An enabled app lock still requires unlock.

Test a stopped app, then repeat while it is open:

```sh
adb shell am force-stop name.levis.ichor
adb shell am start -W -a android.intent.action.VIEW -d 'ichor://demo' -p name.levis.ichor
adb shell am start -W -a android.intent.action.VIEW -d 'ichor://demo' -p name.levis.ichor
```
