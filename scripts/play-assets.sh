#!/usr/bin/env bash
set -euo pipefail

# Generates the Google Play store graphics into fastlane/metadata/android/en-US/images/:
#
#   icon.png                512x512, the launcher mark on its adaptive-icon background
#   featureGraphic.png      1024x500, headline + the overview screenshot
#
# The phone screenshots in images/phoneScreenshots/ are captured, not generated: the
# feature graphic reads the overview from there. Their N_ prefix sets the Play order.
#
# Needs ImageMagick 7 with the librsvg delegate (`magick -list format | grep RSVG`).
# Uses IBM Plex Sans/Mono (the website's fonts) when PLEX_DIR points at them or nix can
# fetch them, and falls back to DejaVu Sans otherwise.
#
# Usage: scripts/play-assets.sh

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
out="$repo_root/fastlane/metadata/android/en-US/images"
overview="$out/phoneScreenshots/1_overview.png"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Brand, from docs/index.html and the launcher icon.
BG_ICON="#101418"   # @color/ic_launcher_background
PHONE="#0F1A2A"
GLOW="#1E3350"
INK="#E8EEF5"
INK_2="#93A3B8"
EDGE="#2C3A4F"

setup_fonts() {
    local plex="${PLEX_DIR:-}"
    if [[ -z "$plex" ]] && command -v nix >/dev/null 2>&1; then
        plex="$(nix build --no-link --print-out-paths nixpkgs#ibm-plex 2>/dev/null | head -1 || true)"
    fi
    if [[ -n "$plex" && -d "$plex" ]]; then
        cat > "$work/fonts.conf" <<EOF
<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>
  <dir>$plex</dir>
  <cachedir>$work/fc-cache</cachedir>
</fontconfig>
EOF
        export FONTCONFIG_FILE="$work/fonts.conf"
        echo "fonts: IBM Plex from $plex"
    else
        echo "fonts: IBM Plex not found, falling back to DejaVu Sans" >&2
    fi
}

# The Ichor logo (docs/brand/ichor.svg) as an inline <svg> at x,y with the given edge, so
# librsvg draws it as vectors rather than loading an external image.
mark() {
    local x="$1" y="$2" size="$3"
    sed "s#<svg #<svg x=\"$x\" y=\"$y\" width=\"$size\" height=\"$size\" #" "$repo_root/docs/brand/ichor.svg"
}

render_icon() {
    # Play masks the icon itself, so the background fills the square; the logo keeps a
    # margin so the mask does not clip the crest.
    cat > "$work/icon.svg" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <rect width="512" height="512" fill="$BG_ICON"/>
  $(mark 40 40 432)
</svg>
EOF
    magick -background none "$work/icon.svg" -define png:exclude-chunks=date,time -depth 8 "PNG32:$out/icon.png"
}

render_feature_graphic() {
    # The phone on the right shows the top of the overview and bleeds off the bottom edge.
    local phone_w=300 phone_x=664 phone_y=64
    magick "$overview" -resize "${phone_w}x" "$work/overview.png"
    local phone_h
    phone_h="$(magick identify -format '%h' "$work/overview.png")"

    cat > "$work/feature.svg" <<EOF
<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="500" viewBox="0 0 1024 500">
  <defs>
    <radialGradient id="glow" cx="78%" cy="0%" r="85%">
      <stop offset="0" stop-color="$GLOW"/>
      <stop offset="1" stop-color="$PHONE"/>
    </radialGradient>
  </defs>
  <rect width="1024" height="500" fill="url(#glow)"/>

  $(mark 56 56 56)
  <text x="122" y="99" fill="$INK" font-family="IBM Plex Sans, DejaVu Sans" font-weight="600" font-size="26">Ichor<tspan dx="9" fill="$INK_2" font-weight="400">for Talos Linux</tspan></text>

  <text fill="$INK" font-family="IBM Plex Sans, DejaVu Sans" font-weight="600" font-size="56" letter-spacing="-1.2">
    <tspan x="64" y="218">Your Talos cluster,</tspan>
    <tspan x="64" y="286">from your phone.</tspan>
  </text>
  <text x="64" y="356" fill="$INK_2" font-family="IBM Plex Mono, DejaVu Sans Mono" font-size="22">nodes · logs · etcd · alerts</text>

  <rect x="$((phone_x - 8))" y="$((phone_y - 8))" width="$((phone_w + 16))" height="$((phone_h + 16))" rx="38" fill="#05090F" stroke="$EDGE" stroke-width="2"/>
</svg>
EOF
    # The screenshot is composited by ImageMagick rather than an SVG <image>, which librsvg
    # does not reliably load from a temporary directory.
    magick "$work/overview.png" \
        \( -size "${phone_w}x${phone_h}" xc:none -fill white -draw "roundrectangle 0,0 $((phone_w - 1)),$((phone_h - 1)) 30,30" \) \
        -alpha set -compose DstIn -composite "$work/overview-rounded.png"
    magick "$work/feature.svg" -background "$PHONE" -flatten \
        "$work/overview-rounded.png" -geometry "+${phone_x}+${phone_y}" -compose Over -composite \
        -define png:exclude-chunks=date,time -depth 8 "PNG24:$out/featureGraphic.png"
}

command -v magick >/dev/null || { echo "ImageMagick 7 (magick) is required" >&2; exit 1; }
[[ -f "$overview" ]] || { echo "missing $overview" >&2; exit 1; }
setup_fonts
render_icon
render_feature_graphic
magick identify -format '%f %wx%h\n' "$out/icon.png" "$out/featureGraphic.png" "$out"/phoneScreenshots/*.png
