#!/usr/bin/env bash
# Captures the phone's current screen over adb for the website and the store listings.
#
#   scripts/screenshot.sh NAME         capture one screen as NAME
#   scripts/screenshot.sh NAME FILE    crop FILE, a screencap of the same device, instead
#                                      (scripts/ci-screenshots.sh, on the CI emulator)
#
# Turn on Settings > Privacy > Screenshot mode in the app first, so no real addresses or
# node names end up in the images. The status and navigation bars are cropped off (their
# sizes are read from the phone), which also keeps tall phones within the Play Store's 2:1
# aspect limit.
#
# Writes:
#   fastlane/metadata/android/en-US/images/phoneScreenshots/NAME.png  (store, full size)
#   docs/screenshots/NAME.webp                                         (website, 540 px wide)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
source "$root/scripts/screenshot-lib.sh"
store="$root/fastlane/metadata/android/en-US/images/phoneScreenshots"
site="$root/docs/screenshots"

name="${1:?usage: $0 NAME [FILE]}"
from="${2:-}"
[[ "$name" =~ ^[a-z0-9-]+$ ]] || { echo "NAME must be lowercase letters, digits and dashes" >&2; exit 1; }
command -v magick >/dev/null || { echo "ImageMagick (magick) is required" >&2; exit 1; }
adb get-state >/dev/null

mkdir -p "$store" "$site"
raw="$(mktemp --suffix=.png)"
trap 'rm -f "$raw"' EXIT
if [[ -n "$from" ]]; then cp "$from" "$raw"; else adb exec-out screencap -p > "$raw"; fi
crop_bars "$raw" "$store/$name.png"
magick "$store/$name.png" -resize 540x -quality 82 "$site/$name.webp"
echo "$store/$name.png"
echo "$site/$name.webp"
