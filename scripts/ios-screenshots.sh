#!/usr/bin/env bash
# The website's iPhone screenshots, from the result bundles of `scripts/ios-build.sh screenshots`
# (build/screenshots-dark.xcresult and -light, attachments named after each screen): exports them,
# checks every subject is there, and writes docs/screenshots/ios/NAME.webp (dark) and
# NAME-light.webp, 540 px wide, quality 82, with cwebp (`brew install webp`). No crop: the
# overridden status bar is part of the picture. The runner's iPhone gives 1206x2622, so the images
# are 540x1174. On a Mac with Xcode 16 or newer:
#   scripts/ios-build.sh screenshots && scripts/ios-screenshots.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="$ROOT/ios/build"
OUT="$ROOT/docs/screenshots/ios"
# The names of scripts/ci-screenshots.sh, the Android set: SHOTS, then feature-FEATURES.
SHOTS=(overview node-services logs events graphs capture upgrade)
FEATURES=(plugins gitops capacity argocd-diff argocd metrics data-services castai cgroups apps
  workloads pods cronjobs netperf drift import kube-home)
NAMES=("${SHOTS[@]}" "${FEATURES[@]/#/feature-}")

command -v cwebp >/dev/null || { echo "cwebp is required (brew install webp)" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Copies each attachment of one appearance to $work/$1/NAME.png, from the export's manifest.json:
# an attachment's suggested name is its own, then an index and a UUID.
export_appearance() {
  local bundle="$BUILD/screenshots-$1.xcresult"
  [[ -d "$bundle" ]] || { echo "$bundle is missing (run scripts/ios-build.sh screenshots)" >&2; exit 1; }
  mkdir -p "$work/$1/raw" "$work/$1/png"
  xcrun xcresulttool export attachments --path "$bundle" --output-path "$work/$1/raw"
  python3 -I - "$work/$1/raw" "$work/$1/png" <<'PY'
import json, re, shutil, sys
from pathlib import Path

raw, png = Path(sys.argv[1]), Path(sys.argv[2])
for test in json.loads((raw / "manifest.json").read_text()):
    for attachment in test.get("attachments", []):
        suggested = attachment.get("suggestedHumanReadableName", "")
        name = re.sub(r"_\d+_[0-9A-Fa-f-]{36}(\.\w+)?$", "", suggested)
        shutil.copyfile(raw / attachment["exportedFileName"], png / f"{name}.png")
PY
}

missing=()
for appearance in dark light; do
  export_appearance "$appearance"
  for name in "${NAMES[@]}"; do
    [[ -f "$work/$appearance/png/$name.png" ]] || missing+=("$appearance/$name")
  done
done
if ((${#missing[@]})); then
  printf 'no screenshot from the test: %s\n' "${missing[@]}" >&2
  exit 1
fi

mkdir -p "$OUT"
for name in "${NAMES[@]}"; do
  cwebp -quiet -resize 540 0 -q 82 "$work/dark/png/$name.png" -o "$OUT/$name.webp"
  cwebp -quiet -resize 540 0 -q 82 "$work/light/png/$name.png" -o "$OUT/$name-light.webp"
done
echo "${#NAMES[@]} screens in both appearances in docs/screenshots/ios"
