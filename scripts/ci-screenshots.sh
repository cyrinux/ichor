#!/usr/bin/env bash
# The website and store screenshots from the offline demo, on a running emulator (workflow
# screenshots.yml, through scripts/ci-emulator.sh). Installs the APKs `./build.sh smoke` built,
# runs androidTest DemoScreenshots in the dark theme, pulls its raw screencaps and crops each
# with scripts/screenshot.sh. Locally: an x86_64 AVD started, then
# `./build.sh smoke && scripts/ci-screenshots.sh`.
#
# Writes docs/screenshots/NAME.webp and fastlane/.../phoneScreenshots/N_NAME.png, N being
# the gallery order below.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APK_DIR="$ROOT/app/build/outputs/apk"
STORE="$ROOT/fastlane/metadata/android/en-US/images/phoneScreenshots"
DEVICE_DIR=/data/local/tmp/ichor-screenshots
# DemoScreenshots.NODE_SHOTS, after the overview.
SHOTS=(overview node-services logs events graphs capture upgrade)

abi="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
app="$(find "$APK_DIR/debug" -name "*-${abi}-*.apk" -o -name "*-universal-*.apk" | head -1)"
tests="$(find "$APK_DIR/androidTest" -name '*.apk' | head -1)"
[[ -n "$app" && -n "$tests" ]] || { echo "no APK for $abi under $APK_DIR (run ./build.sh smoke)" >&2; exit 1; }

adb shell cmd uimode night yes
adb shell rm -rf "$DEVICE_DIR"
adb install -r -g "$app"
adb install -r -g "$tests"
runner="$(adb shell pm list instrumentation | tr -d '\r' | sed -n 's/^instrumentation:\([^ ]*name\.levis\.ichor[^ ]*\) .*/\1/p' | head -1)"
[[ -n "$runner" ]] || { echo "the screenshot test's instrumentation is not installed" >&2; exit 1; }

report="$ROOT/app/build/reports/screenshots.txt"
mkdir -p "$(dirname "$report")"
adb shell am instrument -w -r -e class name.levis.ichor.DemoScreenshots "$runner" | tee "$report"
grep -q '^INSTRUMENTATION_CODE: -1' "$report" && grep -q '^OK (' "$report" || {
  # What the test captured, a failure.png among them, for the job's failure artifact.
  mkdir -p "$ROOT/app/build/reports/screenshots"
  adb pull "$DEVICE_DIR/." "$ROOT/app/build/reports/screenshots/" >/dev/null 2>&1 || true
  echo "the screenshot test failed: see $report" >&2
  exit 1
}

raw="$(mktemp -d)"
trap 'rm -rf "$raw"' EXIT
adb pull "$DEVICE_DIR/." "$raw" >/dev/null
for i in "${!SHOTS[@]}"; do
  name="${SHOTS[$i]}"
  [[ -f "$raw/$name.png" ]] || { echo "no screenshot $name.png from the test" >&2; exit 1; }
  "$ROOT/scripts/screenshot.sh" "$name" "$raw/$name.png" >/dev/null
  mv "$STORE/$name.png" "$STORE/$((i + 1))_$name.png"
  echo "$name"
done
