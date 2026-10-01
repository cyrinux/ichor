#!/usr/bin/env bash
# Captures the phone's current screen over adb for the website and the store listings.
#
#   scripts/screenshot.sh NAME     capture one screen as NAME
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
store="$root/fastlane/metadata/android/en-US/images/phoneScreenshots"
site="$root/docs/screenshots"

name="${1:?usage: $0 NAME}"
[[ "$name" =~ ^[a-z0-9-]+$ ]] || { echo "NAME must be lowercase letters, digits and dashes" >&2; exit 1; }
command -v magick >/dev/null || { echo "ImageMagick (magick) is required" >&2; exit 1; }
adb get-state >/dev/null

# Bar heights in pixels, e.g. "type=statusBars frame=[0,0][1200,144]".
bars="$(adb shell dumpsys window | grep -oE 'type=(statusBars|navigationBars) frame=\[[0-9]+,[0-9]+\]\[[0-9]+,[0-9]+\]' | sort -u)"
top="$(sed -nE 's/.*statusBars frame=\[0,0\]\[[0-9]+,([0-9]+)\].*/\1/p' <<<"$bars" | head -1)"
nav="$(sed -nE 's/.*navigationBars frame=\[0,([0-9]+)\]\[[0-9]+,[0-9]+\].*/\1/p' <<<"$bars" | head -1)"

mkdir -p "$store" "$site"
raw="$(mktemp --suffix=.png)"
trap 'rm -f "$raw"' EXIT
adb exec-out screencap -p > "$raw"
height="$(magick identify -format '%h' "$raw")"
width="$(magick identify -format '%w' "$raw")"
top="${top:-0}"
nav="${nav:-$height}"
magick "$raw" -crop "${width}x$((nav - top))+0+${top}" +repage "$store/$name.png"
magick "$store/$name.png" -resize 540x -quality 82 "$site/$name.webp"
echo "$store/$name.png"
echo "$site/$name.webp"
