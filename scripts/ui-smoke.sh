#!/usr/bin/env bash
# The demo-mode smoke test on a running emulator (android.yml's ui-smoke; locally: an x86_64
# AVD started, then `./build.sh smoke && scripts/ui-smoke.sh`). Installs the debug APK
# and its androidTest APK built by `./build.sh smoke`, runs the instrumentation and fails when
# a test fails: `am instrument` itself exits 0 either way. The raw output goes to
# app/build/reports/ui-smoke.txt. The screenshot walks are left out: scripts/ci-screenshots.sh and
# scripts/store-screenshots.sh run them.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APK_DIR="$ROOT/app/build/outputs/apk"
abi="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
app="$(find "$APK_DIR/debug" -name "*-${abi}-*.apk" -o -name "*-universal-*.apk" | head -1)"
tests="$(find "$APK_DIR/androidTest" -name '*.apk' | head -1)"
[[ -n "$app" && -n "$tests" ]] || { echo "no APK for $abi under $APK_DIR (run ./build.sh smoke)" >&2; exit 1; }

adb install -r -g "$app"
adb install -r -g "$tests"
runner="$(adb shell pm list instrumentation | tr -d '\r' | sed -n 's/^instrumentation:\([^ ]*name\.levis\.ichor[^ ]*\) .*/\1/p' | head -1)"
[[ -n "$runner" ]] || { echo "the smoke test's instrumentation is not installed" >&2; exit 1; }

report="$ROOT/app/build/reports/ui-smoke.txt"
mkdir -p "$(dirname "$report")"
adb shell am instrument -w -r -e notClass name.levis.ichor.DemoScreenshots,name.levis.ichor.StoreScreenshotTest "$runner" | tee "$report"
# -r prints INSTRUMENTATION_CODE: -1 when the run completed, then "OK (n tests)" or FAILURES.
grep -q '^INSTRUMENTATION_CODE: -1' "$report" && grep -q '^OK (' "$report" || {
  echo "the smoke test failed: see $report" >&2
  exit 1
}
