# Build

Part of the [Ichor documentation](../README.md#documentation).

## Repository layout

```
app/            Android: Kotlin + Jetpack Compose UI
wear/           Wear OS tile and complication (fed by the phone over the Data Layer)
core-widget/    status rules and phone-to-watch payload shared by app/ and wear/
ios/            iOS: SwiftUI app (XcodeGen) + IchorCore Swift package
go/ichorgo  Go core exposed to Kotlin and Swift (JSON in/out)
native/llm      on-device model engine (llama.cpp behind a C API), shared by app/ and ios/
go/cmd/probe    desktop CLI calling the same Go functions
flake.nix       Nix build environment (default)
build/          amd64 Docker toolchain (CI)
```

APKs are split per ABI (`arm64-v8a` for almost all phones, `armeabi-v7a`, `x86_64`), with
compressed native libraries. A release APK is about 16 MB.

```sh
./build.sh            # debug APKs -> app/build/outputs/apk/debug/app-<abi>-debug.apk
./build.sh release    # needs ICHOR_KEYSTORE, ICHOR_KEYSTORE_PASSWORD, ICHOR_KEY_ALIAS, ICHOR_KEY_PASSWORD
just install           # builds, then installs the APK matching the connected device's ABI
./build.sh play       # Google Play App Bundle -> app/build/outputs/bundle/play/app-play.aab
```

The Play build is the release build without the self-updater (Play delivers updates) and
without the donation links, with feature funding through Play Billing instead (the billing
library is only in that build; the others get the no-op store in `app/src/foss`). Its store listing, privacy policy and Console answers are in
`fastlane/` (see [fastlane/PLAY_CONSOLE.md](../fastlane/PLAY_CONSOLE.md)).

- **Default toolchain:** Nix (`flake.nix`). `BUILDER=docker ./build.sh` uses `build/Dockerfile`
  instead, and `IN_CONTAINER=1 ./build.sh` runs inside that image (CI, e.g. a Forgejo runner).
- **aarch64 Linux hosts:** the Android SDK and NDK are x86_64-only, and the Go runtime and the
  NDK clang crash under qemu-user. `build.sh` therefore keeps Go, gomobile, the JDK and Gradle
  native:
  - gomobile is built from a patched copy, because upstream refuses linux/arm64;
  - cgo uses a native clang (the NDK's version, 21 for r29) with the NDK sysroot (`.cache/ndk`);
  - only `aapt2` runs emulated.
- **Nix sandbox:** for the x86_64 SDK builds, `build.sh` exposes the binfmt qemu closure to the
  sandbox, which requires a trusted Nix user.
- **Page size:** native libraries are linked with 16 KB page alignment, as Android 15+ requires.
- **On-device model engine:** `build.sh` fetches the llama.cpp release pinned in
  `native/llama-cpp.env` (`scripts/fetch-llama.sh`, checked by commit) and builds
  `libichorllm.so` with the NDK and CMake for `arm64-v8a` and `x86_64` (`app/src/main/cpp`); the
  check variant skips it. `ICHOR_LOCAL_MODEL=0 ./build.sh` leaves it out, and the app then hides
  the "This phone" provider. On iOS, `scripts/ios-build.sh` builds `llama.xcframework` (Metal)
  from the same commit, once per commit into `.cache/`.
- **Downloadable models:** `go/ichorgo/local_models.json` lists them; `scripts/local-models.py`
  pins each to its Hugging Face revision, size and SHA-256 (an unpinned entry is not offered).

Go core only (fast, native):

```sh
cd go && go test ./...
go run ./cmd/probe overview   # also: services NODE, resources NODE, logs NODE SERVICE, dmesg NODE,
                              #       etcd, health, kubeconfig (prints size only), parse
go run ./cmd/probe -config ../talosconfig-phone overview   # test a role-limited config
```

## Layouts in other languages

Debug builds carry Android's pseudo-locales: in Developer options → Language, pick
**English (XA)** (every string longer and accented, so a clipped or hard-coded text shows) or
**Arabic (XB)** (right to left: the layout mirrors, the AutoMirrored icons flip). Walk the
overview, a node, Kubernetes and Settings. On iOS, the scheme's Run → Options → App Language
offers **Right-to-Left Pseudolanguage** and **Double-Length Pseudolanguage** for the same walk
in the Simulator.

## iOS

The iOS app (`ios/`) reuses the same Go core, built as `Ichorgo.xcframework` with
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
- Face ID / passcode lock, security keys (NFC and the Lightning 5Ci; USB-C YubiKeys on iPhone
  are best effort), themes, and role-based actions.

**What iOS does differently:**

- **Self-update:** not possible for sideloaded apps; reinstall new releases with Sideloadly or
  AltStore.
- **Screenshots:** iOS does not let apps block them. The app-switcher privacy cover hides the
  content instead.
- **Background alerts:** best effort. iOS decides when background checks run, and skips them
  while the device is locked, because the config cannot be decrypted then; with a security key
  required, also after iOS closed the app, until it is unlocked again.
- **Leftover data:** Keychain items survive uninstalling the app. Use Settings → Clusters & backup → Delete to
  remove the config.
- **Widget:** shares the last check with the app through an App Group, which a free Apple ID
  may not support when sideloading. In that case the widget stays empty.
- **Apple Watch:** a watch face complication (the open cluster's health, "N not ready", etcd
  alarms, the age once stale) and a small app listing the clusters and their nodes with state
  and reason, both from the iPhone's last background check. Read-only: the phone sends the
  watch the names (masked under the screenshot mode) and node states over WatchConnectivity,
  never a config, token or address. CI builds the watch app, but the IPA and App Store build
  do not include it yet.

**Building:** Xcode only runs on macOS.

```sh
just ios-test-linux   # core package tests, here on Linux (Nix swift shell)
just ios-test         # macOS: xcframework + core tests + simulator build
just ios-build        # macOS: unsigned IPA in ios/build/
```

**Installing:** CI produces an unsigned IPA. Sideloadly or AltStore re-sign it with your Apple
ID: a free account means reinstalling every 7 days, a paid one lasts a year. To sign in Xcode
instead, set `ICHOR_IOS_TEAM_ID` before `xcodegen generate`.

**App Store:** `scripts/ios-build.sh appstore` builds the App Store variant, compiled with
`APP_STORE`, which hides the donation links (App Review Guideline 3.2.2), signs it through Xcode
automatic signing with an App Store Connect API key, and uploads it to TestFlight. On a `v*` tag,
`ios.yml` runs it once these secrets of the `release` environment are set:

| Secret | Value |
|---|---|
| `IOS_TEAM_ID` | the Apple Developer team ID |
| `ASC_KEY_ID` | the App Store Connect API key ID (Users and Access → Integrations, Admin role) |
| `ASC_ISSUER_ID` | the issuer ID shown above the keys |
| `ASC_KEY_P8` | the contents of `AuthKey_<id>.p8` |

Register `name.levis.ichor`, `name.levis.ichor.widget` and the `group.name.levis.ichor` App
Group in the developer portal, and create the app in App Store Connect, before the first upload.
What to request from Apple and declare in App Store Connect (multicast entitlement for
Wake-on-LAN, privacy label and manifest, export compliance, review notes) is in
[fastlane/APP_STORE_CONNECT.md](../fastlane/APP_STORE_CONNECT.md).

## CI (GitHub Actions)

- **`.github/workflows/android.yml`** (Ubuntu, Nix flake): four parallel jobs, `./build.sh check`
  and the debug, release and Play builds (`ICHOR_SKIP_TESTS=1`, the check job runs the tests).
- **`.github/workflows/ios.yml`** (macOS 26): two parallel jobs, core tests with the simulator
  build, and the unsigned IPA; on `v*` tags, the App Store upload (see [iOS](#ios)).

The Nix store, Gradle user home, Go module and build caches and the Swift package checkouts
are restored from the Actions cache (`.github/actions/*-toolchain`); only the check and test
jobs write them, on `main`, and they delete the Nix and Go entries they replace, to stay under
GitHub's 10 GB per repository (`gh cache list` shows the entries and their sizes).

Both run on pushes to `main` and on pull requests. Release APKs are named
`ichor-v<version>-<abi>.apk`, or `…-unsigned.apk` when no keystore secrets are set;
the updater ignores unsigned ones. On a `v*` tag (`just release-tag 0.1.0`,
then `git push origin v0.1.0`), they attach the release APK and the IPA to the GitHub release.

`release.yml` creates that release as a **draft**, with the tag's changelog entry as its notes
(`scripts/discord-notes.py --markdown`). Its `verify` job rebuilds the arm64 APK from the tag
and compares it with the drafted one; only then does `promote` publish the draft, and the
Discord announcement follows. A draft is invisible to the self-updater and to the app's
changelog, so a release that fails verification never reaches anyone. When `verify` fails,
download the run's `reproducibility-differences` artifact, then either delete the draft and
the tag and tag again once fixed, or, if the difference is understood, publish it by hand:
`gh release edit v0.1.0 --draft=false --latest`.

To sign the release APK in CI, generate a key once and upload it as secrets of the `release`
environment (Settings → Environments; restricted to `v*` tags, so pull requests never see them):

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

Without them, and on every build that isn't a `v*` tag, the release APK is unsigned.

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

The same demo is the fixture of the **smoke test** (`app/src/androidTest/.../DemoSmokeTest.kt`,
CI job `ui-smoke`). With an x86_64 AVD (API 34) running, `./build.sh smoke` builds the
debug APK and the test APK, then `scripts/ui-smoke.sh` installs both and runs the test. It
fails when a test does, and keeps the output in `app/build/reports/ui-smoke.txt`.

On iOS the twin is `ios/IchorUITests/DemoSmokeTests.swift` (CI job "Core tests and simulator
build"). On a Mac, `scripts/ios-build.sh test` runs it on the first available iPhone simulator
after the build and writes `ios/build/ui-tests.xcresult`; in Xcode it is the Ichor scheme's
test action (⌘U). The launch environment `ICHOR_UI_TEST=demo` (or `demo-kube`) adds the demo at
launch and hides the tour (`ios/Ichor/App/UITestLaunch.swift`).

The iPhone images under `docs/screenshots/ios/` come from `ios/IchorUITests/DemoScreenshotTests.swift`,
which is skipped in that run. To retake the whole set on a Mac with Xcode 16 or newer and
`brew install webp`: `scripts/ios-build.sh screenshots && scripts/ios-screenshots.sh`; it fails on a
missing screen. Or run the `screenshots` workflow from the Actions tab, unzip its `screenshots-ios`
artifact at the root of the repository and commit it.
