#!/usr/bin/env bash
# The App Store's iPhone screenshots in all ten languages, from the result bundle of
# `scripts/ios-build.sh store-screenshots` (build/store-screenshots.xcresult, attachments named
# FOLDER_N_NAME by IchorUITests/StoreScreenshotTests): exports them, checks that every language
# has the seven screens at a size App Store Connect takes for the 6.9" iPhone, and only then writes
#   fastlane/screenshots/ios/FOLDER/N_NAME.png
# under DIR (the repository by default). FOLDER is App Store Connect's language name. No crop and
# no resize: the overridden status bar is part of the picture, and Apple wants the exact size.
# The upload stays by hand (fastlane/APP_STORE_CONNECT.md, "Screenshots"). On a Mac with Xcode 16
# or newer:
#   scripts/ios-build.sh store-screenshots && scripts/ios-store-screenshots.sh [--force] [--out DIR]
# In the repository it refuses a dirty fastlane/screenshots unless --force.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUNDLE="$ROOT/ios/build/store-screenshots.xcresult"
FOLDERS=(en-US fr-FR es-ES uk de-DE it-IT zh-Hans ja pt-BR ru)
# The listing order, as scripts/store-screenshots.sh (Play) and ListingWalk.shots.
SHOTS=(overview node-services logs events graphs capture upgrade)
# App Store Connect's 6.9" iPhone sizes, portrait.
SIZES=(1320x2868 1290x2796 1260x2736)

out="$ROOT"
force=0
while (($#)); do
  case "$1" in
    --force) force=1 ;;
    --out) out="${2:?--out needs a directory}"; shift ;;
    *) echo "usage: $0 [--force] [--out DIR]" >&2; exit 2 ;;
  esac
  shift
done

[[ -d "$BUNDLE" ]] || { echo "$BUNDLE is missing (run scripts/ios-build.sh store-screenshots)" >&2; exit 1; }
if [[ "$out" == "$ROOT" && "$force" == 0 && -n "$(git -C "$ROOT" status --porcelain -- fastlane/screenshots)" ]]; then
  echo "fastlane/screenshots has uncommitted changes: commit them, or pass --force" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/raw" "$work/png"
xcrun xcresulttool export attachments --path "$BUNDLE" --output-path "$work/raw"
# Copies each attachment to $work/png/FOLDER_N_NAME.png, from the export's manifest.json: an
# attachment's suggested name is its own, then an index and a UUID. A test retried after a stall
# attached some screens twice: the latest copy wins.
python3 -I - "$work/raw" "$work/png" <<'PY'
import json, re, shutil, sys
from pathlib import Path

raw, png = Path(sys.argv[1]), Path(sys.argv[2])
attachments = [a for test in json.loads((raw / "manifest.json").read_text()) for a in test.get("attachments", [])]
for attachment in sorted(attachments, key=lambda a: a.get("timestamp", 0)):
    suggested = attachment.get("suggestedHumanReadableName", "")
    name = re.sub(r"_\d+_[0-9A-Fa-f-]{36}(\.\w+)?$", "", suggested)
    shutil.copyfile(raw / attachment["exportedFileName"], png / f"{name}.png")
PY

# Prints why App Store Connect would refuse a picture, if it would.
refusal() {
  local w h size
  w=$(sips -g pixelWidth "$1" | awk '/pixelWidth/ { print $2 }')
  h=$(sips -g pixelHeight "$1" | awk '/pixelHeight/ { print $2 }')
  for size in "${SIZES[@]}"; do
    [[ "${w}x${h}" == "$size" ]] && return 0
  done
  echo "${w}x${h} is not a 6.9\" iPhone size (${SIZES[*]})"
}

problems=()
for folder in "${FOLDERS[@]}"; do
  for i in "${!SHOTS[@]}"; do
    file="$work/png/${folder}_$((i + 1))_${SHOTS[$i]}.png"
    if [[ ! -f "$file" ]]; then
      problems+=("$folder/$((i + 1))_${SHOTS[$i]}: no screenshot from the test")
    elif why=$(refusal "$file") && [[ -n "$why" ]]; then
      problems+=("$folder/$((i + 1))_${SHOTS[$i]}: $why")
    fi
  done
done
if ((${#problems[@]})); then
  printf '%s\n' "${problems[@]}" >&2
  echo "nothing written: the set is all or nothing" >&2
  exit 1
fi

for folder in "${FOLDERS[@]}"; do
  dest="$out/fastlane/screenshots/ios/$folder"
  rm -rf "$dest"
  mkdir -p "$dest"
  for i in "${!SHOTS[@]}"; do
    cp "$work/png/${folder}_$((i + 1))_${SHOTS[$i]}.png" "$dest/$((i + 1))_${SHOTS[$i]}.png"
  done
done
echo "${#SHOTS[@]} screens in ${#FOLDERS[@]} languages in $out/fastlane/screenshots/ios"
