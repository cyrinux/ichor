#!/usr/bin/env bash
# Builds the Go AAR and the APK (Nix dev shell, or the amd64 toolchain container).
#
#   ./build.sh            # debug APK, toolchain from the Nix flake if nix exists, else Docker
#   ./build.sh release    # release APK (needs signing env, see app/build.gradle.kts)
#   ./build.sh play       # Google Play App Bundle: release without self-update/donations
#   ./build.sh check      # formatting, vet, staticcheck and all unit tests (Go + Kotlin), no APK
#   BUILDER=docker ./build.sh   # force the container toolchain
#   IN_CONTAINER=1 ./build.sh   # tools already on PATH (CI runner using build/Dockerfile)
#   ICHOR_SKIP_TESTS=1 ./build.sh debug   # no Go or Kotlin unit tests (CI: the check job runs them)
#   ICHOR_LOCAL_MODEL=0 ./build.sh        # without the on-device model engine (llama.cpp)
set -euo pipefail

VARIANT="${1:-debug}"
IMAGE="${IMAGE:-localhost/ichor-builder:latest}"
ROOT="$(cd "$(dirname "$0")" && pwd)"

case "$VARIANT" in
  debug) GRADLE_TASKS=(testDebugUnitTest assembleDebug) ;;
  # AGP 9 only creates unit-test tasks for debug; check/debug already run them.
  release) GRADLE_TASKS=(assembleRelease) ;;
  play) GRADLE_TASKS=(bundlePlay) ;;
  # Also assembles the Wear OS app (tile and complication): its manifest and resources are checked.
  check) GRADLE_TASKS=(testDebugUnitTest :wear:assembleDebug) ;;
  *) echo "unknown variant: $VARIANT (debug|release|play|check)" >&2; exit 2 ;;
esac
# The CI build jobs skip the tests, which their check job runs once (see android.yml).
if [[ "${ICHOR_SKIP_TESTS:-0}" == 1 && "$VARIANT" == debug ]]; then
  GRADLE_TASKS=(assembleDebug)
fi

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
  # The NDK's clang major (lib/clang/<major>); the native clang in flake.nix must match it.
  local resources=("$prebuilt"/lib/clang/*)
  local bin="$shadow/toolchains/llvm/prebuilt/linux-x86_64/bin"
  rm -rf "$shadow" && mkdir -p "$bin"
  for entry in "$real"/*; do
    [[ "$(basename "$entry")" == toolchains ]] || ln -s "$entry" "$shadow/"
  done
  ln -s "$prebuilt/sysroot" "$prebuilt/lib" "$shadow/toolchains/llvm/prebuilt/linux-x86_64/"
  # gomobile runs bin/clang{,++} itself and passes --target=<triple><api>.
  for tool in clang clang++; do
    printf '#!/bin/sh\nexec %s --sysroot=%s/sysroot -resource-dir %s -fuse-ld=lld -Wno-unused-command-line-argument "$@"\n' \
      "$tool" "$prebuilt" "${resources[0]}" >"$bin/$tool"
    chmod +x "$bin/$tool"
  done
  export ANDROID_NDK_HOME="$shadow"
}

# The on-device model engine (Settings → AI diagnosis → This phone): llama.cpp at the release
# pinned in native/llama-cpp.env (scripts/fetch-llama.sh), built with the NDK into app/build/llm-jni/<abi>/
# (app/src/main/cpp). 64-bit ABIs only: 32-bit phones lack the memory for a model.
# ICHOR_LOCAL_MODEL=0 leaves it out, and the app then hides the provider.
LLM_ABIS=(arm64-v8a x86_64)

build_llm_jni() {
  local src out abi build flags arch
  src="$("$ROOT/scripts/fetch-llama.sh")"
  out="$ROOT/app/build/llm-jni"
  rm -rf "$out"
  for abi in "${LLM_ABIS[@]}"; do
    build="$ROOT/.cache/llm-build/$abi"
    # The same bytes from any checkout: no source paths in the library. One section per
    # function and datum, so the linker drops what the engine never calls (--gc-sections).
    flags="-ffile-prefix-map=$ROOT=. -ffile-prefix-map=$src=llama.cpp -ffunction-sections -fdata-sections"
    arch=()
    # Dot-product instructions (every 64-bit phone with the memory for a model has them);
    # the app checks the CPU before loading the library.
    [[ "$abi" == arm64-v8a ]] && arch=(-DGGML_CPU_ARM_ARCH=armv8.2-a+dotprod)
    cmake -S "$ROOT/app/src/main/cpp" -B "$build" -G Ninja \
      -DCMAKE_TOOLCHAIN_FILE="$ANDROID_NDK_HOME/build/cmake/android.toolchain.cmake" \
      -DANDROID_ABI="$abi" -DANDROID_PLATFORM=android-26 -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_C_FLAGS="$flags" -DCMAKE_CXX_FLAGS="$flags" -DLLAMA_SRC="$src" "${arch[@]}" >/dev/null
    cmake --build "$build" --target ichorllm
    mkdir -p "$out/$abi"
    cp "$build/libichorllm.so" "$out/$abi/"
    echo "libichorllm.so ($abi): $(wc -c <"$out/$abi/libichorllm.so") bytes"
  done
}

# The Go core as app/libs/ichorgo.aar, one library per ABI (see splits in app/build.gradle.kts),
# the same bytes from any checkout: -trimpath keeps the source paths out, and gomobile's
# `replace <module> => <dir>`, which Go keeps in the library's build info, names the same
# directory everywhere because the bind runs from a fixed link to this checkout's go/.
GOBIND_LINK=/tmp/ichor-gobind/go
bind_go_core() {
  mkdir -p "$(dirname "$GOBIND_LINK")"
  # Two builds at once would swap the link under each other.
  if command -v flock >/dev/null; then
    exec 9>"$(dirname "$GOBIND_LINK").lock"
    flock 9
  fi
  ln -sfn "$ROOT/go" "$GOBIND_LINK"
  (cd "$GOBIND_LINK" && gomobile bind -v -target=android/arm64,android/arm,android/amd64 -androidapi 26 \
    -javapkg=name.levis -trimpath -ldflags="-s -w -extldflags=-Wl,-z,max-page-size=16384" \
    -o "$ROOT/app/libs/ichorgo.aar" ./ichorgo)
  check_go_core_paths
  if command -v flock >/dev/null; then flock -u 9; fi
}

# Fails when the arm64 library's build info names a directory other than GOBIND_LINK: then
# a rebuild from another checkout would not give the published bytes (CONTRIBUTING.md
# "Reproducible builds").
check_go_core_paths() {
  local lib="$ROOT/.cache/gobind-check.so" paths
  python3 -c 'import sys, zipfile; sys.stdout.buffer.write(zipfile.ZipFile(sys.argv[1]).read("jni/arm64-v8a/libgojni.so"))' \
    "$ROOT/app/libs/ichorgo.aar" >"$lib"
  paths="$(go version -m "$lib" | awk '$1 == "=>" && $2 ~ /^\// { print $2 }' | sort -u)"
  rm -f "$lib"
  if [[ "$paths" != "$GOBIND_LINK" ]]; then
    echo "libgojni.so names ${paths:-no directory}, not $GOBIND_LINK: the APK would depend on the checkout's path" >&2
    exit 1
  fi
}

build_inside() {
  # versionName/versionCode for Gradle (see scripts/version.sh).
  set -a
  eval "$("$ROOT/scripts/version.sh" --env)"
  set +a
  echo "version $ICHOR_VERSION (code $ICHOR_BUILD_NUMBER)"
  # Release notes bundled in the app, for the "what's new" shown after an update.
  python3 "$ROOT/scripts/changelog.py" --limit 30 -o "$ROOT/app/src/main/assets/changelog.json"
  # The Go core's modules for the "Open-source licenses" screen (Gradle cannot see into the AAR).
  python3 "$ROOT/scripts/go-licenses.py"

  cd "$ROOT/go"
  if [[ "$VARIANT" == "check" ]]; then
    python3 "$ROOT/scripts/check-translations.py"
    python3 "$ROOT/scripts/check-ios-strings.py"
    python3 "$ROOT/scripts/check-unused-strings.py"
    python3 "$ROOT/scripts/check-accessibility.py"
    python3 "$ROOT/scripts/check-file-length.py"
    python3 "$ROOT/scripts/check-play-listing.py"
    python3 "$ROOT/scripts/site-i18n.py" --check
    python3 "$ROOT/scripts/local-models.py" --check
    python3 -m unittest discover -s "$ROOT/scripts" -p 'test_*.py'
    unformatted="$(gofmt -l .)"
    [[ -z "$unformatted" ]] || { echo "gofmt needed:" >&2; echo "$unformatted" >&2; exit 1; }
    go vet ./...
    staticcheck ./...
    # = just plugin-check (just is not in the dev shell): every built-in plugin validates.
    go run ./cmd/probe plugin-validate plugin/builtin/*.yaml
    # Plugin drafts (crd-request.yml) are not embedded, but they must not rot red either.
    drafts=(plugin/drafts/*.yaml)
    [[ ! -e "${drafts[0]}" ]] || go run ./cmd/probe plugin-validate "${drafts[@]}"
  fi
  if [[ "${ICHOR_SKIP_TESTS:-0}" == 1 ]]; then
    echo "ICHOR_SKIP_TESTS=1: skipping go test"
  else
    go test ./...
  fi
  # gomobile/gobind versions are pinned by the tool directive in go/go.mod.
  export GOBIN="$ROOT/.cache/gobin" PATH="$ROOT/.cache/gobin:$PATH"
  go install tool
  if [[ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" == "linux/arm64" ]]; then
    install_gomobile_linux_arm64
    use_native_ndk_linux_arm64
  fi
  # app/libs only holds this generated (gitignored) AAR, so a fresh checkout lacks it.
  mkdir -p "$ROOT/app/libs"
  bind_go_core
  # The check job compiles and tests Kotlin only: the engine is not loaded by unit tests.
  local gradle_props=()
  if [[ "${ICHOR_LOCAL_MODEL:-1}" == 0 ]]; then
    gradle_props=(-Pichor.localModel=false)
  elif [[ "$VARIANT" != check ]]; then
    build_llm_jni
  fi
  cd "$ROOT"
  gradle --no-daemon "${gradle_props[@]}" "${GRADLE_TASKS[@]}"
  case "$VARIANT" in
    check) ;;
    play) find "$ROOT/app/build/outputs/bundle/play" -name '*.aab' -printf '%s\t%p\n' ;;
    *) find "$ROOT/app/build/outputs/apk/$VARIANT" -name '*.apk' -printf '%s\t%p\n' | sort -n ;;
  esac
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
  -e ICHOR_KEYSTORE -e ICHOR_KEYSTORE_PASSWORD -e ICHOR_KEY_ALIAS -e ICHOR_KEY_PASSWORD \
  -e ICHOR_VERSION -e ICHOR_BUILD_NUMBER -e ICHOR_SKIP_TESTS -e ICHOR_LOCAL_MODEL \
  -v "$ROOT:/src" -v "$ROOT/.cache:/cache" \
  "$IMAGE" bash -c 'mkdir -p /tmp/home && /src/build.sh '"$VARIANT"
