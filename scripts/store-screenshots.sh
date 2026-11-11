#!/usr/bin/env bash
# The Play listing's screenshots from the offline demo, on a running emulator of API 33 or newer
# (`./build.sh smoke && just store-screenshots`, or the store and store-tablets jobs of workflow
# screenshots.yml). Runs androidTest StoreScreenshotTest in the dark theme and crops its raw
# screencaps. Nothing is written unless every picture passes Play's limits: release.yml uploads
# whatever the folders hold, so a partial set must never land.
#
#   scripts/store-screenshots.sh [--size phone|7in|10in] [--force] [--out DIR]
#
# --size says which set the running emulator takes (phone by default):
#   phone  all ten languages, a phone (the workflow's pixel_4)
#   7in    en-US only, a 7" tablet in portrait (the workflow's "Nexus 7 2013", 600 dp wide)
#   10in   en-US only, a 10" tablet in landscape (the workflow's pixel_tablet, 1280 dp wide)
# The emulator is not checked against the size: start the right one.
#
# Writes under DIR (the repository by default):
#   fastlane/metadata/android/LOCALE/images/phoneScreenshots/N_NAME.png  (N sets the Play order)
#   fastlane/metadata/android/en-US/images/sevenInchScreenshots/N_NAME.png  (--size 7in)
#   fastlane/metadata/android/en-US/images/tenInchScreenshots/N_NAME.png    (--size 10in)
#   docs/screenshots/NAME.webp                                  (phone only, from en-US, 540 px wide)
# In the repository it refuses a dirty fastlane/metadata unless --force. After a phone run, run
# scripts/play-assets.sh: the feature graphic shows the new overview.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
source "$ROOT/scripts/screenshot-lib.sh"

DEVICE_DIR=/data/local/tmp/ichor-store-screenshots
LOCALES=(en-US fr-FR es-ES uk de-DE it-IT zh-CN ru-RU pt-BR ja-JP)
# The order of the listing; DemoWalk.captureListingScreens takes these.
SHOTS=(overview node-services logs events graphs capture upgrade)
MIN_WIDTH=1080
MAX_SIDE=3840

out="$ROOT"
force=0
size=phone
while (($#)); do
  case "$1" in
    --force) force=1 ;;
    --out) out="${2:?--out needs a directory}"; shift ;;
    --size) size="${2:?--size needs phone, 7in or 10in}"; shift ;;
    *) echo "usage: $0 [--size phone|7in|10in] [--force] [--out DIR]" >&2; exit 2 ;;
  esac
  shift
done

# The set's folder under images/, and the languages it is taken in (AppLocale tags; empty = all).
case "$size" in
  phone) set_folder=phoneScreenshots; only="" ;;
  7in) set_folder=sevenInchScreenshots; only=en; LOCALES=(en-US) ;;
  10in) set_folder=tenInchScreenshots; only=en; LOCALES=(en-US) ;;
  *) echo "--size must be phone, 7in or 10in, not $size" >&2; exit 2 ;;
esac

command -v magick >/dev/null || { echo "ImageMagick (magick) is required" >&2; exit 1; }
if [[ "$out" == "$ROOT" && "$force" == 0 && -n "$(git -C "$ROOT" status --porcelain -- fastlane/metadata)" ]]; then
  echo "fastlane/metadata has uncommitted changes: commit them, or pass --force" >&2
  exit 1
fi

install_test_apks "$ROOT"

report="$ROOT/app/build/reports/store-screenshots.txt"
mkdir -p "$(dirname "$report")"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

adb shell cmd uimode night yes
adb shell rm -rf "$DEVICE_DIR"
adb shell am instrument -w -r -e screenshots true ${only:+-e languages "$only"} \
  -e class name.levis.ichor.StoreScreenshotTest "$runner" | tee "$report"
grep -q '^INSTRUMENTATION_CODE: -1' "$report" && grep -q '^OK (' "$report" || {
  # What the test captured, a failure.png among them, for the job's failure artifact.
  mkdir -p "$ROOT/app/build/reports/store-screenshots"
  adb pull "$DEVICE_DIR/." "$ROOT/app/build/reports/store-screenshots/" >/dev/null 2>&1 || true
  echo "the store screenshot test failed: see $report" >&2
  exit 1
}
mkdir -p "$work/raw"
adb pull "$DEVICE_DIR/." "$work/raw" >/dev/null

# Prints why a picture would be refused by Play, if it would.
refusal() {
  local w h long short
  read -r w h < <(magick identify -format '%w %h\n' "$1")
  long=$((w > h ? w : h))
  short=$((w > h ? h : w))
  ((w >= MIN_WIDTH)) || echo "width $w under $MIN_WIDTH"
  ((long <= MAX_SIDE)) || echo "side $long over $MAX_SIDE"
  ((long <= 2 * short)) || echo "${w}x${h} over 2:1"
}

for locale in "${LOCALES[@]}"; do
  folder="$work/fastlane/metadata/android/$locale/images/$set_folder"
  mkdir -p "$folder"
  for i in "${!SHOTS[@]}"; do
    name="${SHOTS[$i]}"
    raw="$work/raw/$locale/$name.png"
    [[ -f "$raw" ]] || { echo "no screenshot $locale/$name.png from the test" >&2; exit 1; }
    file="$folder/$((i + 1))_$name.png"
    crop_bars "$raw" "$file"
    problem="$(refusal "$file")"
    [[ -z "$problem" ]] || { echo "$locale/$name.png: $problem" >&2; exit 1; }
  done
  echo "$locale"
done

# The website shows the phone set only.
if [[ "$size" == phone ]]; then
  mkdir -p "$work/docs/screenshots"
  for i in "${!SHOTS[@]}"; do
    name="${SHOTS[$i]}"
    magick "$work/fastlane/metadata/android/en-US/images/phoneScreenshots/$((i + 1))_$name.png" \
      -resize 540x -quality 82 "$work/docs/screenshots/$name.webp"
  done
fi

mkdir -p "$out"
cp -r "$work/fastlane" "$out/"
[[ ! -d "$work/docs" ]] || cp -r "$work/docs" "$out/"
