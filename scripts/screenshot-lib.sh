#!/usr/bin/env bash
# Sourced by the screenshot scripts: what they share about the running device.

# Crops the status and navigation bars off the screencap $1 into $2, their heights read from the
# device. This also keeps tall phones within the Play Store's 2:1 aspect limit.
crop_bars() {
  local raw="$1" out="$2" bars top nav width height
  # e.g. "type=statusBars frame=[0,0][1200,144]".
  bars="$(adb shell dumpsys window | grep -oE 'type=(statusBars|navigationBars) frame=\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]' | sort -u)"
  top="$(sed -nE 's/.*statusBars frame=\[0,0\]\[[0-9]+,([0-9]+)\].*/\1/p' <<<"$bars" | head -1)"
  nav="$(sed -nE 's/.*navigationBars frame=\[0,([0-9]+)\]\[[0-9]+,[0-9]+\].*/\1/p' <<<"$bars" | head -1)"
  height="$(magick identify -format '%h' "$raw")"
  width="$(magick identify -format '%w' "$raw")"
  top="${top:-0}"
  nav="${nav:-$height}"
  magick "$raw" -crop "${width}x$((nav - top))+0+${top}" +repage "$out"
}

# Installs the APKs `./build.sh smoke` built from $1/app/build/outputs/apk; sets `runner`.
install_test_apks() {
  local apk_dir="$1/app/build/outputs/apk" abi app tests
  abi="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
  app="$(find "$apk_dir/debug" -name "*-${abi}-*.apk" -o -name "*-universal-*.apk" | head -1)"
  tests="$(find "$apk_dir/androidTest" -name '*.apk' | head -1)"
  [[ -n "$app" && -n "$tests" ]] || { echo "no APK for $abi under $apk_dir (run ./build.sh smoke)" >&2; exit 1; }
  adb install -r -g "$app"
  adb install -r -g "$tests"
  runner="$(adb shell pm list instrumentation | tr -d '\r' | sed -n 's/^instrumentation:\([^ ]*name\.levis\.ichor[^ ]*\) .*/\1/p' | head -1)"
  [[ -n "$runner" ]] || { echo "the test's instrumentation is not installed" >&2; exit 1; }
}
