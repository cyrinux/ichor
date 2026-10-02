#!/usr/bin/env python3
"""Generates the Android vector drawables from the brand SVGs in docs/brand/.

  ichor.svg        -> drawable/ic_launcher_foreground.xml   (adaptive icon foreground)
  ichor-glyph.svg  -> drawable/ic_launcher_monochrome.xml   (themed icon layer)
                   -> drawable/ic_stat_ichor.xml            (status-bar icon)

The SVGs stick to what a VectorDrawable can draw: <path>/<circle> inside plain <g>s,
solid or userSpaceOnUse gradient fills and strokes, opacity, and fill-rule. Filters,
transforms and masks are rejected rather than silently dropped. Each SVG is placed on
the drawable's viewport by shifting its coordinates, so no <group> transform is needed.

Usage: scripts/brand-assets.py
"""
import re
import sys
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BRAND = ROOT / "docs/brand"
DRAWABLE = ROOT / "app/src/main/res/drawable"

SVG_NS = "{http://www.w3.org/2000/svg}"
INHERITED = (
    "fill", "fill-opacity", "fill-rule",
    "stroke", "stroke-width", "stroke-opacity", "stroke-linecap", "stroke-linejoin",
)
DEFAULTS = {"fill": "#000"}
PATH_TOKEN = re.compile(r"[A-Za-z]|-?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?")
# Which parameters of each absolute command are x / y coordinates.
COORDS = {
    "M": "xy", "L": "xy", "T": "xy", "H": "x", "V": "y",
    "C": "xyxyxy", "S": "xyxy", "Q": "xyxy", "A": "-----xy", "Z": "",
}


@dataclass(frozen=True)
class Target:
    source: str
    output: str
    size_dp: int
    viewport: float  # viewport edge, in the SVG's 512-unit space
    comment: str
    mono: str | None = None  # paint every shape this colour (single-colour icons)


TARGETS = (
    # The 108dp adaptive canvas masks to a 66dp safe zone: the 512-unit art (about
    # 496 units across its round silhouette) lands at about 62dp, clear of the mask.
    Target("ichor.svg", "ic_launcher_foreground.xml", 108, 860,
           "Ichor guardian on the 108dp adaptive-icon canvas (safe zone 66dp)."),
    Target("ichor-glyph.svg", "ic_launcher_monochrome.xml", 108, 920,
           "Themed-icon layer: the single-colour Ichor glyph.", mono="#FFFFFFFF"),
    Target("ichor-glyph.svg", "ic_stat_ichor.xml", 24, 544,
           "Monochrome status-bar icon: the Ichor glyph.", mono="#FFFFFFFF"),
)


def fail(message: str) -> None:
    sys.exit(f"brand-assets: {message}")


def shift_path(d: str, dx: float, dy: float) -> str:
    """Translates absolute path commands by (dx, dy); relative ones move with them."""
    out: list[str] = []
    command = ""
    params: list[str] = []
    first = True

    def flush() -> None:
        nonlocal first
        upper = command.upper()
        if upper not in COORDS:
            fail(f"unsupported path command {command!r}")
        roles = COORDS[upper]
        absolute = command.isupper() or (first and command == "m")
        if not roles:
            out.append(command)
            return
        if len(params) % len(roles):
            fail(f"bad parameter count for {command!r} in {d!r}")
        values = []
        for i, value in enumerate(params):
            role = roles[i % len(roles)]
            number = float(value)
            # A leading relative "m" is absolute for its first pair only.
            if absolute and role != "-" and (command.isupper() or i < 2):
                number += dx if role == "x" else dy
            values.append(f"{number:g}")
        out.append(command + ",".join(values))
        first = False

    for token in PATH_TOKEN.findall(d):
        if token.isalpha():
            if command:
                flush()
            command, params = token, []
        else:
            params.append(token)
    if command:
        flush()
    return " ".join(out)


def circle_path(element: ET.Element) -> str:
    cx, cy, r = (float(element.get(k)) for k in ("cx", "cy", "r"))
    return f"M{cx - r:g},{cy} A{r:g},{r:g} 0 1 0 {cx + r:g},{cy} A{r:g},{r:g} 0 1 0 {cx - r:g},{cy} Z"


def android_color(value: str, opacity: float = 1.0) -> str:
    hex_digits = value.lstrip("#")
    if len(hex_digits) == 3:
        hex_digits = "".join(c * 2 for c in hex_digits)
    if len(hex_digits) != 6:
        fail(f"unsupported colour {value!r}")
    return f"#{round(opacity * 255):02X}{hex_digits.upper()}"


def parse_gradients(root: ET.Element, dx: float, dy: float) -> dict[str, str]:
    gradients: dict[str, str] = {}
    for tag in ("linearGradient", "radialGradient"):
        for gradient in root.iter(SVG_NS + tag):
            if gradient.get("gradientUnits") != "userSpaceOnUse" or gradient.get("gradientTransform"):
                fail(f"gradient {gradient.get('id')} must be userSpaceOnUse without a transform")
            if tag == "linearGradient":
                x1, y1, x2, y2 = (float(gradient.get(k)) for k in ("x1", "y1", "x2", "y2"))
                head = (f'<gradient android:type="linear" android:startX="{x1 + dx:g}" android:startY="{y1 + dy:g}"'
                        f' android:endX="{x2 + dx:g}" android:endY="{y2 + dy:g}">')
            else:
                cx, cy, r = (float(gradient.get(k)) for k in ("cx", "cy", "r"))
                head = (f'<gradient android:type="radial" android:centerX="{cx + dx:g}" android:centerY="{cy + dy:g}"'
                        f' android:gradientRadius="{r:g}">')
            items = [
                f'<item android:offset="{float(stop.get("offset")):g}" android:color="'
                f'{android_color(stop.get("stop-color"), float(stop.get("stop-opacity", 1)))}"/>'
                for stop in gradient.iter(SVG_NS + "stop")
            ]
            gradients[gradient.get("id")] = head + "".join(items) + "</gradient>"
    return gradients


def shapes(element: ET.Element, inherited: dict[str, str]):
    """Yields (path data, resolved style) for every drawable shape, in paint order."""
    for child in element:
        tag = child.tag.removeprefix(SVG_NS)
        if tag in ("title", "desc", "defs"):
            continue
        for unsupported in ("transform", "filter", "mask", "clip-path", "opacity", "style"):
            if child.get(unsupported) is not None:
                fail(f"<{tag}> uses {unsupported}=, which a VectorDrawable cannot draw")
        style = {**inherited, **{k: child.get(k) for k in INHERITED if child.get(k) is not None}}
        if tag == "g":
            yield from shapes(child, style)
        elif tag == "path":
            yield child.get("d"), style
        elif tag == "circle":
            yield circle_path(child), style
        else:
            fail(f"unsupported element <{tag}>")


def paint(kind: str, value: str | None, opacity: str | None, gradients: dict[str, str], mono: str | None):
    """Returns (attributes, aapt children) for a fill or stroke."""
    if value is None or value == "none":
        return [], []
    attr = "fillColor" if kind == "fill" else "strokeColor"
    alpha = [f'android:{kind}Alpha="{float(opacity):g}"'] if opacity is not None else []
    if mono:
        return [f'android:{attr}="{mono}"', *alpha], []
    match = re.fullmatch(r"url\(#(.+)\)", value)
    if match:
        if match.group(1) not in gradients:
            fail(f"unknown gradient {value}")
        return alpha, [f'<aapt:attr name="android:{attr}">{gradients[match.group(1)]}</aapt:attr>']
    return [f'android:{attr}="{android_color(value)}"', *alpha], []


def path_element(d: str, style: dict[str, str], gradients: dict[str, str], mono: str | None) -> str:
    fill_attrs, fill_children = paint("fill", style.get("fill"), style.get("fill-opacity"), gradients, mono)
    stroke_attrs, stroke_children = [], []
    if style.get("stroke", "none") != "none":
        stroke_attrs, stroke_children = paint("stroke", style["stroke"], style.get("stroke-opacity"), gradients, mono)
        stroke_attrs.append(f'android:strokeWidth="{float(style.get("stroke-width", 1)):g}"')
        if "stroke-linecap" in style:
            stroke_attrs.append(f'android:strokeLineCap="{style["stroke-linecap"]}"')
        if "stroke-linejoin" in style:
            stroke_attrs.append(f'android:strokeLineJoin="{style["stroke-linejoin"]}"')
    if style.get("fill-rule") == "evenodd":
        fill_attrs.append('android:fillType="evenOdd"')
    attrs = "\n        ".join([f'android:pathData="{d}"', *fill_attrs, *stroke_attrs])
    children = fill_children + stroke_children
    if not children:
        return f"    <path\n        {attrs} />"
    body = "\n".join(f"        {child}" for child in children)
    return f"    <path\n        {attrs}>\n{body}\n    </path>"


def convert(target: Target) -> str:
    root = ET.parse(BRAND / target.source).getroot()
    if root.get("viewBox") != "0 0 512 512":
        fail(f"{target.source}: expected viewBox 0 0 512 512")
    offset = (target.viewport - 512) / 2
    gradients = parse_gradients(root, offset, offset)
    paths = []
    root_style = {**DEFAULTS, **{k: root.get(k) for k in INHERITED if root.get(k) is not None}}
    for d, style in shapes(root, root_style):
        if style.get("fill", "none") == "none" and style.get("stroke", "none") == "none":
            continue
        paths.append(path_element(shift_path(d, offset, offset), style, gradients, target.mono))
    namespaces = 'xmlns:android="http://schemas.android.com/apk/res/android"'
    if any("aapt:attr" in p for p in paths):
        namespaces += '\n    xmlns:aapt="http://schemas.android.com/aapt"'
    return (
        '<?xml version="1.0" encoding="utf-8"?>\n'
        f"<!-- {target.comment}\n"
        f"     Generated from docs/brand/{target.source} by scripts/brand-assets.py; edit the SVG. -->\n"
        f"<vector {namespaces}\n"
        f'    android:width="{target.size_dp}dp"\n'
        f'    android:height="{target.size_dp}dp"\n'
        f'    android:viewportWidth="{target.viewport:g}"\n'
        f'    android:viewportHeight="{target.viewport:g}">\n'
        + "\n".join(paths)
        + "\n</vector>\n"
    )


def main() -> None:
    for target in TARGETS:
        (DRAWABLE / target.output).write_text(convert(target))
        print(f"{target.source} -> {(DRAWABLE / target.output).relative_to(ROOT)}")


if __name__ == "__main__":
    main()
