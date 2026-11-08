#!/usr/bin/env bash
# The website and store screenshots from the offline demo, on a running emulator (workflow
# screenshots.yml, through scripts/ci-emulator.sh). Installs the APKs `./build.sh smoke` built,
# runs androidTest DemoScreenshots in the dark then the light theme, pulls its raw screencaps and
# crops each with scripts/screenshot.sh. Locally: an x86_64 AVD started, then
# `./build.sh smoke && scripts/ci-screenshots.sh`.
#
# Writes docs/screenshots/NAME.webp (dark) and NAME-light.webp, and the dark ones as
# fastlane/.../phoneScreenshots/N_NAME.png, N being the gallery order below. The other languages'
# store pictures come from scripts/store-screenshots.sh, which retakes en-US with them.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "$ROOT/scripts/screenshot-lib.sh"
STORE="$ROOT/fastlane/metadata/android/en-US/images/phoneScreenshots"
DEVICE_DIR=/data/local/tmp/ichor-screenshots
# DemoScreenshots.NODE_SHOTS, after the overview.
SHOTS=(overview node-services logs events graphs capture upgrade)
# DemoScreenshots.FEATURE_SHOTS and its own: the website's feature cards, not for the store.
FEATURES=(plugins gitops capacity argocd-diff argocd metrics data-services castai cgroups apps
  workloads pods cronjobs netperf drift import kube-home)

install_test_apks "$ROOT"

report="$ROOT/app/build/reports/screenshots.txt"
mkdir -p "$(dirname "$report")"
raw="$(mktemp -d)"
trap 'rm -rf "$raw"' EXIT

# One walk in the given system theme (the app follows it), its screencaps into $raw/$1.
walk() {
  adb shell cmd uimode night "$([[ $1 == dark ]] && echo yes || echo no)"
  adb shell rm -rf "$DEVICE_DIR"
  adb shell am instrument -w -r -e class name.levis.ichor.DemoScreenshots "$runner" | tee "$report"
  grep -q '^INSTRUMENTATION_CODE: -1' "$report" && grep -q '^OK (' "$report" || {
    # What the test captured, a failure.png among them, for the job's failure artifact.
    mkdir -p "$ROOT/app/build/reports/screenshots"
    adb pull "$DEVICE_DIR/." "$ROOT/app/build/reports/screenshots/" >/dev/null 2>&1 || true
    echo "the screenshot test failed in the $1 theme: see $report" >&2
    exit 1
  }
  mkdir -p "$raw/$1"
  adb pull "$DEVICE_DIR/." "$raw/$1" >/dev/null
}

walk dark
walk light

for i in "${!SHOTS[@]}"; do
  name="${SHOTS[$i]}"
  [[ -f "$raw/dark/$name.png" && -f "$raw/light/$name.png" ]] || { echo "no screenshot $name.png from the test" >&2; exit 1; }
  "$ROOT/scripts/screenshot.sh" "$name" "$raw/dark/$name.png" >/dev/null
  mv "$STORE/$name.png" "$STORE/$((i + 1))_$name.png"
  # The light ones are for the website alone: the store keeps one set.
  "$ROOT/scripts/screenshot.sh" "$name-light" "$raw/light/$name.png" >/dev/null
  rm "$STORE/$name-light.png"
  echo "$name"
done

for feature in "${FEATURES[@]}"; do
  name="feature-$feature"
  [[ -f "$raw/dark/$name.png" && -f "$raw/light/$name.png" ]] || { echo "no screenshot $name.png from the test" >&2; exit 1; }
  "$ROOT/scripts/screenshot.sh" "$name" "$raw/dark/$name.png" >/dev/null
  "$ROOT/scripts/screenshot.sh" "$name-light" "$raw/light/$name.png" >/dev/null
  rm "$STORE/$name.png" "$STORE/$name-light.png"
done
