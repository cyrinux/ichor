default:
    @just --list

# adb target. Empty = the only connected device; "ip:port" = wireless adb (connected first).
DEVICE := env_var_or_default("ICHOR_DEVICE", "")

# Open-source builds (debug/release); the Play build is the bare name.levis.ichor.
APP_ID := "name.levis.ichor.foss"
# Per-ABI APKs: app/build/outputs/apk/<buildType>/app-<abi>-<buildType>.apk
APK_DIR := "app/build/outputs/apk"

# Show the build identity every APK built from this checkout will carry:
# the release tag when HEAD is one, the tag plus commit distance and SHA when
# it is not, and a bare SHA when the repository has no tags yet.
version:
    @scripts/version.sh --json | python3 -m json.tool

# The version `just release-tag auto` (and the daily auto-release) would tag, and why.
next-version:
    @scripts/next-version.py --json | python3 -m json.tool

# Tag a release. The APK's versionName/versionCode derive from the tag and the
# commit count, so this is the only step a release needs. The tree must be
# clean, the tag is annotated with the changelog, and nothing is pushed for you.
# `auto` picks the version from the commits since the last tag (scripts/next-version.py:
# breaking -> major, feat -> minor, fix/perf -> patch), as the daily auto-release does.
# --yes accepts the drafted Google Play notes as is: no editor, terminal or not.
# --unsigned makes a plain annotated tag, for CI where there is no signing key.
release-tag version *flags:
    #!/usr/bin/env bash
    set -euo pipefail
    version="{{ version }}"
    version="${version#v}"
    yes=0
    sign=-s # signed, like the commits
    for flag in {{ flags }}; do
        case "$flag" in
            -y|--yes) yes=1 ;;
            --unsigned) sign=-a ;;
            *) echo "Unknown flag: $flag (expected --yes or --unsigned)" >&2; exit 2 ;;
        esac
    done
    if [[ "$version" == auto ]]; then
        version="$(scripts/next-version.py)" || {
            echo "No feat, fix, perf or breaking commit since the last tag: nothing to release." >&2
            exit 1
        }
        echo "Next version: ${version}"
    fi
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "Expected a MAJOR.MINOR.PATCH version, got: {{ version }}" >&2
        exit 2
    fi
    if [[ -n "$(git status --porcelain)" ]]; then
        echo "Refusing to tag a dirty tree; commit or stash first." >&2
        exit 1
    fi
    if git rev-parse -q --verify "refs/tags/v${version}" >/dev/null; then
        echo "Tag v${version} already exists." >&2
        exit 1
    fi
    # A tag ships: gate it on the same checks CI runs. ICHOR_RELEASE_SKIP_GATE=1
    # skips them for a hotfix whose commit already passed CI.
    if [[ "${ICHOR_RELEASE_SKIP_GATE:-}" != 1 ]]; then
        just check
    fi
    # Google Play's "What's new": drafted from the changelog for the commit about to be
    # tagged (HEAD + 1), edited, then committed. Empty it to let CI generate the notes;
    # without a terminal CI always does, unless --yes commits the draft unedited.
    # A file written beforehand is kept as is.
    if [[ "$yes" == 1 || -t 0 ]]; then
        notes="$(scripts/play-notes.py --build "$(( $(git rev-list --count HEAD) + 1 ))")"
        [[ "$yes" == 1 ]] || sh -c "${VISUAL:-${EDITOR:-vi}} \"\$1\"" editor "$notes" # as git runs it: "code --wait" works
        if [[ ! -s "$notes" ]]; then
            rm -f "$notes"
        elif [[ -n "$(git status --porcelain -- "$notes")" ]]; then
            git add "$notes"
            git commit -q -m "docs: Google Play release notes for v${version}"
        fi
    fi
    previous="$(git describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null || true)"
    range="${previous:+${previous}..}HEAD"
    {
        echo "Ichor v${version}"
        echo
        git log --no-merges --format='- %s' "$range"
    } | git tag "$sign" "v${version}" -F -
    echo "Tagged v${version}$( [[ -n "$previous" ]] && echo " (changes since ${previous})" )."
    echo "Push it with: git push origin v${version}"

# Debug APK (Go tests, gomobile AAR, Kotlin unit tests, assemble).
build:
    ./build.sh debug

# R8-shrunk release APK, signed when ICHOR_KEYSTORE* are set (see android-keystore-gen).
build-release:
    ./build.sh release

# Google Play App Bundle (app/build/outputs/bundle/play/app-play.aab): the release build
# without the self-updater and donation links, signed with the ICHOR_KEYSTORE* upload key.
build-play:
    ./build.sh play

# Formatting, vet and every unit test (Go + Kotlin), without packaging an APK.
check:
    ./build.sh check

# Every UI string translated (fr, es, uk, de, it) with matching placeholders, Android and iOS,
# and every website string in docs-i18n/.
i18n-check:
    python3 scripts/check-translations.py
    python3 scripts/site-i18n.py --check

# Translated website: `just site` writes docs/<lang>/ (as CI does before publishing);
# `just site --sync` lists new English strings in docs-i18n/*.json for translation.
site *args:
    python3 scripts/site-i18n.py {{ args }}

# Refresh the bundled app-inventory icons from go/ichorgo/appcatalog.json (needs network
# and ImageMagick). `just app-icons --check` only validates the catalog and the bundle.
app-icons *args:
    python3 scripts/sync-app-icons.py {{ args }}

# Go core only: fast, native, no Android toolchain.
test:
    cd go && go test ./...

# Run the Go core against a real cluster, e.g. `just probe overview`,
# `just probe logs 10.0.0.2 kubelet`, `just probe -config talosconfig-phone etcd`.
probe *args:
    cd go && go run ./cmd/probe {{ args }}

# Generate the release signing key. Back it up: an app signed with another key
# cannot update the installed one.
android-keystore-gen path=env_var_or_default("ICHOR_KEYSTORE", home_directory() + "/ichor-release.jks") alias=env_var_or_default("ICHOR_KEY_ALIAS", "ichor"):
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -e "{{ path }}" ]; then
        echo "{{ path }} already exists; not overwriting" >&2
        exit 1
    fi
    # keytool comes with the JDK in the Nix dev shell.
    nix develop --command keytool -genkeypair -v -keystore "{{ path }}" -alias "{{ alias }}" \
        -keyalg RSA -keysize 4096 -validity 10000 \
        -dname "CN=Ichor, OU=Android release, O=Cyril Levis, C=FR"
    echo
    echo "Created {{ path }}. Add to .envrc:"
    echo "  export ICHOR_KEYSTORE=\"{{ path }}\""
    echo "  export ICHOR_KEYSTORE_PASSWORD=..."
    echo "  export ICHOR_KEY_ALIAS=\"{{ alias }}\""
    echo "Back it up: it cannot be regenerated."

# Show the release key's fingerprint, to check every build machine uses the same .jks.
android-keystore-info:
    #!/usr/bin/env bash
    set -euo pipefail
    : "${ICHOR_KEYSTORE:?ICHOR_KEYSTORE is not set (see just android-keystore-gen)}"
    : "${ICHOR_KEYSTORE_PASSWORD:?ICHOR_KEYSTORE_PASSWORD is not set}"
    nix develop --command keytool -list -v -keystore "$ICHOR_KEYSTORE" -storepass "$ICHOR_KEYSTORE_PASSWORD" \
        -alias "${ICHOR_KEY_ALIAS:-ichor}" | grep -E "Alias name|Owner|SHA256"

# Upload the release signing key to the `release` environment's GitHub Actions secrets
# (what android.yml signs with on v* tags),
# from the same env as android-keystore-info. Values go through stdin, never argv.
# Defaults to the current repository; e.g. `just github-secrets cyrinux/ichor`.
github-secrets repo="":
    #!/usr/bin/env bash
    set -euo pipefail
    : "${ICHOR_KEYSTORE:?ICHOR_KEYSTORE is not set (see just android-keystore-gen)}"
    : "${ICHOR_KEYSTORE_PASSWORD:?ICHOR_KEYSTORE_PASSWORD is not set}"
    [[ -f "$ICHOR_KEYSTORE" ]] || { echo "$ICHOR_KEYSTORE does not exist" >&2; exit 1; }
    alias="${ICHOR_KEY_ALIAS:-ichor}"
    repo=({{ if repo == "" { "" } else { "--repo " + repo } }})
    # Fail early on a wrong password/alias rather than in CI.
    nix develop --command keytool -list -keystore "$ICHOR_KEYSTORE" -alias "$alias" \
        -storepass:env ICHOR_KEYSTORE_PASSWORD >/dev/null
    base64 -w0 "$ICHOR_KEYSTORE" | gh secret set ICHOR_KEYSTORE_BASE64 --env release "${repo[@]}"
    printf '%s' "$ICHOR_KEYSTORE_PASSWORD" | gh secret set ICHOR_KEYSTORE_PASSWORD --env release "${repo[@]}"
    printf '%s' "$alias" | gh secret set ICHOR_KEY_ALIAS --env release "${repo[@]}"
    gh secret list --env release "${repo[@]}"

# Build and install the debug APK, e.g. `just install` or `just install 192.168.1.50:37000`.
install device=DEVICE: build
    @just _adb-install "{{ device }}" debug

# Build and install the release APK.
install-release device=DEVICE: build-release
    @just _adb-install "{{ device }}" release

# Install the debug APK and start the app.
run device=DEVICE: (install device)
    adb {{ if device == "" { "" } else { "-s " + device } }} shell am start -n {{ APP_ID }}/name.levis.ichor.MainActivity

# Follow the app's logcat (the app must be running).
logs device=DEVICE:
    #!/usr/bin/env bash
    set -euo pipefail
    adb=(adb {{ if device == "" { "" } else { "-s " + device } }})
    pid="$("${adb[@]}" shell pidof {{ APP_ID }} || true)"
    [[ -n "$pid" ]] || { echo "{{ APP_ID }} is not running (try: just run)" >&2; exit 1; }
    "${adb[@]}" logcat --pid="$pid"

# Show a talosconfig as a QR code in the terminal, for the app's QR import.
# It contains the private key: run it where nobody can photograph your screen.
qr config="talosconfig-phone":
    qrencode -t ansiutf8 -r "{{ config }}"

# Remove build outputs and caches (Gradle, Go AAR, gomobile/NDK shims).
clean:
    rm -rf app/build build/reports .gradle .cache/ndk .cache/x-mobile .cache/gobin app/libs/ichorgo.aar

[private]
_adb-install device build_type:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ device }}" == *:* ]]; then
        adb connect "{{ device }}" >/dev/null
    fi
    adb=(adb {{ if device == "" { "" } else { "-s " + device } }})
    # Pick the APK matching the device's preferred ABI.
    abi="$("${adb[@]}" shell getprop ro.product.cpu.abi | tr -d '\r')"
    apk="$(ls {{ APK_DIR }}/{{ build_type }}/app-"$abi"-{{ build_type }}*.apk 2>/dev/null | head -1)"
    [[ -n "$apk" ]] || { echo "no {{ build_type }} APK for ABI $abi in {{ APK_DIR }}/{{ build_type }}" >&2; exit 1; }
    echo "installing $apk"
    "${adb[@]}" install -r "$apk"

# iOS core package tests on Linux (models, formatting, lock, power rules).
ios-test-linux:
    nix develop .#swift --command ios/IchorCore/test-linux.sh

# macOS only: Go xcframework, core tests and a simulator build.
ios-test:
    scripts/ios-build.sh test

# macOS only: unsigned Release IPA in ios/build/ (re-sign with Sideloadly/AltStore).
ios-build:
    scripts/ios-build.sh ipa

# Capture the phone's current screen for the website and store (turn on screenshot mode first)
screenshot name:
    scripts/screenshot.sh {{name}}

# The release history as JSON, from the release tags and conventional commits
changelog *args:
    @scripts/changelog.py {{args}}
