#!/usr/bin/env bash
# Builds the Go AAR and the APK (Nix dev shell, or the amd64 toolchain container).
#
#   ./build.sh            # debug APK, toolchain from the Nix flake if nix exists, else Docker
#   ./build.sh release    # release APK (needs signing env, see app/build.gradle.kts)
#   ./build.sh check      # formatting, vet and all unit tests (Go + Kotlin), no APK
#   BUILDER=docker ./build.sh   # force the container toolchain
#   IN_CONTAINER=1 ./build.sh   # tools already on PATH (CI runner using build/Dockerfile)
set -euo pipefail

VARIANT="${1:-debug}"
IMAGE="${IMAGE:-localhost/ichor-builder:latest}"
ROOT="$(cd "$(dirname "$0")" && pwd)"

case "$VARIANT" in
  debug) GRADLE_TASKS=(testDebugUnitTest assembleDebug) ;;
  # AGP 9 only creates unit-test tasks for debug; check/debug already run them.
  release) GRADLE_TASKS=(assembleRelease) ;;
  check) GRADLE_TASKS=(testDebugUnitTest) ;;
  *) echo "unknown variant: $VARIANT (debug|release|check)" >&2; exit 2 ;;
esac

# gomobile panics on linux/arm64 hosts although NDK r24+ keeps its (x86_64, emulated)
# toolchain under prebuilt/linux-x86_64. Build it from a patched copy of the pinned module
# (-overlay cannot touch GOMODCACHE files).
install_gomobile_linux_arm64() {
  local src copy="$ROOT/.cache/x-mobile"
  src="$(go list -m -f '{{.Dir}}' golang.org/x/mobile)"
  rm -rf "$copy" && cp -r "$src" "$copy" && chmod -R u+w "$copy"
  sed -i 's/if runtime.GOOS == "darwin" {/if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {/' \
    "$copy/cmd/gomobile/env.go"
  grep -q 'runtime.GOOS == "linux" {' "$copy/cmd/gomobile/env.go" || { echo "gomobile patch did not apply" >&2; exit 1; }
  (cd "$copy" && go build -o "$GOBIN/gomobile" ./cmd/gomobile)
}

# The NDK's x86_64 clang is unreliable under qemu-user. On linux/arm64, point gomobile at a
# shadow NDK whose compiler entries call native clang with the NDK's sysroot and runtime.
use_native_ndk_linux_arm64() {
  local real="$ANDROID_NDK_HOME" shadow="$ROOT/.cache/ndk" entry tool
  local prebuilt="$real/toolchains/llvm/prebuilt/linux-x86_64"
  local bin="$shadow/toolchains/llvm/prebuilt/linux-x86_64/bin"
  rm -rf "$shadow" && mkdir -p "$bin"
  for entry in "$real"/*; do
    [[ "$(basename "$entry")" == toolchains ]] || ln -s "$entry" "$shadow/"
  done
  ln -s "$prebuilt/sysroot" "$prebuilt/lib" "$shadow/toolchains/llvm/prebuilt/linux-x86_64/"
  # gomobile runs bin/clang{,++} itself and passes --target=<triple><api>.
  for tool in clang clang++; do
    printf '#!/bin/sh\nexec %s --sysroot=%s/sysroot -resource-dir %s/lib/clang/18 -fuse-ld=lld -Wno-unused-command-line-argument "$@"\n' \
      "$tool" "$prebuilt" "$prebuilt" >"$bin/$tool"
    chmod +x "$bin/$tool"
  done
  export ANDROID_NDK_HOME="$shadow"
}

build_inside() {
  # versionName/versionCode for Gradle (see scripts/version.sh).
  set -a
  eval "$("$ROOT/scripts/version.sh" --env)"
  set +a
  echo "version $ICHOR_VERSION (code $ICHOR_BUILD_NUMBER)"
  # Release notes bundled in the app, for the "what's new" shown after an update.
  python3 "$ROOT/scripts/changelog.py" --limit 30 -o "$ROOT/app/src/main/assets/changelog.json"

  cd "$ROOT/go"
  if [[ "$VARIANT" == "check" ]]; then
    python3 "$ROOT/scripts/check-translations.py"
    unformatted="$(gofmt -l .)"
    [[ -z "$unformatted" ]] || { echo "gofmt needed:" >&2; echo "$unformatted" >&2; exit 1; }
    go vet ./...
  fi
  go test ./...
  # gomobile/gobind versions are pinned by the tool directive in go/go.mod.
  export GOBIN="$ROOT/.cache/gobin" PATH="$ROOT/.cache/gobin:$PATH"
  go install tool
  if [[ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" == "linux/arm64" ]]; then
    install_gomobile_linux_arm64
    use_native_ndk_linux_arm64
  fi
  # app/libs only holds this generated (gitignored) AAR, so a fresh checkout lacks it.
  mkdir -p "$ROOT/app/libs"
  # One APK per ABI is split from this AAR (see splits in app/build.gradle.kts).
  gomobile bind -v -target=android/arm64,android/arm,android/amd64 -androidapi 26 -javapkg=name.levis \
    -ldflags="-s -w -extldflags=-Wl,-z,max-page-size=16384" -o "$ROOT/app/libs/talosmobile.aar" ./talosmobile
  cd "$ROOT"
  gradle --no-daemon "${GRADLE_TASKS[@]}"
  [[ "$VARIANT" == "check" ]] || find "$ROOT/app/build/outputs/apk/$VARIANT" -name '*.apk' -printf '%s\t%p\n' | sort -n
}

if [[ "${IN_CONTAINER:-0}" == "1" ]]; then
  build_inside
  exit 0
fi

BUILDER="${BUILDER:-$(command -v nix >/dev/null && echo nix || echo docker)}"

# On NixOS aarch64 hosts with boot.binfmt.emulatedSystems, the x86_64 binfmt handler is a
# wrapper that execs qemu from /nix/store; expose that closure to the build sandbox so
# emulated builders can start (needs a trusted Nix user).
nix_emulation_opts() {
  local wrapper=/run/binfmt/x86_64-linux qemu
  [[ "$(uname -m)" == "x86_64" || ! -e "$wrapper" ]] && return 0
  qemu="$(strings "$wrapper" | grep -m1 -o '/nix/store/[^/]*-qemu-user-[^/]*' || true)"
  [[ -z "$qemu" ]] && return 0
  printf '%s\n' --option extra-sandbox-paths "$(nix-store -qR "$qemu" "$(readlink -f "$wrapper")" | tr '\n' ' ')"
}

if [[ "$BUILDER" == "nix" ]]; then
  # Native shell; the x86_64 Android SDK it references is built with the emulation options.
  mapfile -t NIX_OPTS < <(nix_emulation_opts)
  exec nix develop "${NIX_OPTS[@]}" "$ROOT" \
    --command env IN_CONTAINER=1 "$ROOT/build.sh" "$VARIANT"
fi

mkdir -p "$ROOT/.cache"
docker build --platform linux/amd64 -t "$IMAGE" "$ROOT/build"
docker run --rm --platform linux/amd64 \
  -u "$(id -u):$(id -g)" -e HOME=/tmp/home -e IN_CONTAINER=1 \
  -e GOMODCACHE=/cache/gomod -e GOCACHE=/cache/gobuild -e GRADLE_USER_HOME=/cache/gradle \
  -e GOFLAGS=-buildvcs=false \
  -e TALOS_KEYSTORE -e TALOS_KEYSTORE_PASSWORD -e TALOS_KEY_ALIAS -e TALOS_KEY_PASSWORD \
  -e ICHOR_VERSION -e ICHOR_BUILD_NUMBER \
  -v "$ROOT:/src" -v "$ROOT/.cache:/cache" \
  "$IMAGE" bash -c 'mkdir -p /tmp/home && /src/build.sh '"$VARIANT"
