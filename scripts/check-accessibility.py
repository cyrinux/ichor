#!/usr/bin/env python3
"""Fails on accessibility regressions a screen reader or a large text size would hit.

iOS (ios/Ichor/**/*.swift):
- a Button whose whole label is an SF Symbol needs .accessibilityLabel (or .help), else
  VoiceOver reads the symbol's name ("arrow.up");
- a literal .system(size:) below 12 (in .font() or a Font constant) is too small and ignores
  Dynamic Type: use a text style or a @ScaledMetric size.

Android (app/src/main/java/**/*.kt):
- a fixed .sp below 12 ignores the theme's typography: use MaterialTheme.typography;
- an IconButton whose icon has contentDescription = null and no other label is read as
  "button", with nothing saying what it does.

Run: python3 scripts/check-accessibility.py
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
IOS_SOURCES = ROOT / "ios/Ichor"
ANDROID_SOURCES = ROOT / "app/src/main/java"

LABELLED = re.compile(r"\.accessibilityLabel\(|\.help\(|\.accessibilityHidden\(true\)")
BUTTON_OPEN = re.compile(r"\bButton\s*(\([^{}]*\))?\s*\{\s*$|\blabel:\s*\{\s*$")
IMAGE_ONLY = re.compile(r"^\s*Image\(systemName:[^)]*\)(\s*\.\w+\([^()]*\))*\s*$")
# Button(action: f) { Image(…) }, or Button { f() } label: { Image(…) }, on one line.
ONE_LINE = re.compile(
    r"\bButton\s*(\([^{}]*\))?\s*\{\s*Image\(systemName:[^{}]*\}"
    r"|\blabel:\s*\{\s*Image\(systemName:[^{}]*\}\s*$"
)
FIXED_SWIFT_FONT = re.compile(r"\.system\(size:\s*(\d+(?:\.\d+)?)\b(?!\s*\*)")
SMALL_SP = re.compile(r"\b(\d+(?:\.\d+)?)\.sp\b")
SP_EXEMPT = re.compile(r"(letterSpacing|minFontSize)\s*=\s*$")
MIN_SP = 12
ICON_BUTTON = re.compile(r"\b\w*IconButton\(")
# A description on the icon or a label on the button itself.
ICON_LABEL = re.compile(r"contentDescription\s*=(?!\s*null)|semantics\s*[({]|onClickLabel")


def _labelled(lines: list[str], end: int) -> bool:
    """Whether line end, or the modifier chain right after it, labels the view."""
    if LABELLED.search(lines[end]):
        return True
    for line in lines[end + 1 :]:
        if not line.strip().startswith("."):
            return False
        if LABELLED.search(line):
            return True
    return False


def unlabeled_image_buttons(source: str) -> list[int]:
    """1-based lines of buttons whose label is only an SF Symbol and that carry no label."""
    lines = source.splitlines()
    found = []
    for i, line in enumerate(lines):
        if ONE_LINE.search(line):
            if not _labelled(lines, i):
                found.append(i + 1)
            continue
        if not BUTTON_OPEN.search(line):
            continue
        body, j = [], i + 1
        while j < len(lines) and not lines[j].strip().startswith("}"):
            body.append(lines[j])
            j += 1
        content = [b for b in body if b.strip()]
        if j < len(lines) and len(content) == 1 and IMAGE_ONLY.match(content[0]) and not _labelled(lines, j):
            found.append(i + 1)
    return found


def small_swift_fonts(source: str) -> list[int]:
    """Lines with a literal .font(.system(size:)) below MIN_SP. Large hero symbols and sizes
    derived from a frame (size * 0.4) keep a fixed size on purpose."""
    found = []
    for i, line in enumerate(source.splitlines()):
        if any(float(m.group(1)) < MIN_SP for m in FIXED_SWIFT_FONT.finditer(line)):
            found.append(i + 1)
    return found


def small_sp(source: str) -> list[int]:
    """Lines with a text size below MIN_SP. Letter spacing and an auto-size floor are not sizes."""
    found = []
    for i, line in enumerate(source.splitlines()):
        sizes = [m for m in SMALL_SP.finditer(line) if not SP_EXEMPT.search(line[: m.start()])]
        if any(float(m.group(1)) < MIN_SP for m in sizes):
            found.append(i + 1)
    return found


def _closing(source: str, start: int, open_char: str, close_char: str) -> int:
    """Index just past the bracket that closes the one at start."""
    depth = 0
    for k in range(start, len(source)):
        if source[k] == open_char:
            depth += 1
        elif source[k] == close_char:
            depth -= 1
            if depth == 0:
                return k + 1
    return len(source)


def unlabeled_icon_buttons(source: str) -> list[int]:
    """1-based lines of IconButtons whose icon has no description and nothing else labels them."""
    found = []
    for m in ICON_BUTTON.finditer(source):
        end = _closing(source, m.end() - 1, "(", ")")
        rest = source[end:]
        if rest.lstrip().startswith("{"):
            brace = end + len(rest) - len(rest.lstrip())
            end = _closing(source, brace, "{", "}")
        call = source[m.start() : end]
        if "contentDescription = null" in call and not ICON_LABEL.search(call):
            found.append(source.count("\n", 0, m.start()) + 1)
    return found


def main() -> int:
    problems = []
    for path in sorted(IOS_SOURCES.rglob("*.swift")):
        text = path.read_text()
        rel = path.relative_to(ROOT)
        problems += [f"{rel}:{n}: image-only Button without .accessibilityLabel" for n in unlabeled_image_buttons(text)]
        problems += [f"{rel}:{n}: .font(.system(size:)) below {MIN_SP}, use a text style" for n in small_swift_fonts(text)]
    for path in sorted(ANDROID_SOURCES.rglob("*.kt")):
        text = path.read_text()
        rel = path.relative_to(ROOT)
        problems += [f"{rel}:{n}: text below {MIN_SP}.sp, use MaterialTheme.typography" for n in small_sp(text)]
        problems += [f"{rel}:{n}: IconButton whose icon has no contentDescription" for n in unlabeled_icon_buttons(text)]
    for p in problems:
        print(p, file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
