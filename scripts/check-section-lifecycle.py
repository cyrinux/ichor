#!/usr/bin/env python3
"""Fails on a SwiftUI view whose body is a Section carrying a lifecycle modifier.

In a List or Form, SwiftUI gives a Section's modifiers to each of its rows, not to the section
once. A `.task`, `.onAppear`, `.onDisappear` or `.onChange` on a body Section therefore runs once
per row, again when rows come and go, and `.onDisappear` can fire while the section is still on
screen. Put the modifier on a row that is always shown instead (see GCPGoogleAccountSections).

Conservative on purpose: only a `body` whose first statement is `Section`, and only modifiers
chained after the Section's closing brace at the same indentation.

Run: python3 scripts/check-section-lifecycle.py
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
# Build output and tool caches present in a local checkout.
SKIPPED_DIRS = {"build", "DerivedData"}

BODY = re.compile(r"\bvar\s+body\s*:\s*some\s+View\s*\{\s*$")
SECTION = re.compile(r"^Section\b")
LIFECYCLE = re.compile(r"^\.(task|onAppear|onDisappear|onChange)\b")


def indent(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def first_statement(lines: list[str], start: int) -> int | None:
    """Index of the first line after start that is neither blank nor a comment."""
    for i in range(start, len(lines)):
        text = lines[i].strip()
        if text and not text.startswith("//"):
            return i
    return None


def section_modifiers(lines: list[str], at: int) -> list[tuple[int, str]]:
    """(line index, modifier) for each lifecycle modifier chained on the Section at `at`."""
    found = []
    level = indent(lines[at])
    for i in range(at + 1, len(lines)):
        line = lines[i]
        text = line.strip()
        if not text or text.startswith("//") or indent(line) > level:
            continue
        if indent(line) < level:
            break
        if text.startswith(("}", ")")):
            continue  # the Section's own close, `} header: {`, or a modifier closure's close
        if not text.startswith("."):
            break
        match = LIFECYCLE.match(text)
        if match:
            found.append((i, match.group(1)))
    return found


def file_offenders(text: str) -> list[tuple[int, str]]:
    """(1-based line, modifier) for each lifecycle modifier on a body Section in one file."""
    lines = text.splitlines()
    found = []
    for i, line in enumerate(lines):
        if not BODY.search(line):
            continue
        at = first_statement(lines, i + 1)
        if at is not None and SECTION.match(lines[at].strip()):
            found.extend((n + 1, name) for n, name in section_modifiers(lines, at))
    return found


def skipped(rel: str) -> bool:
    parts = rel.split("/")[:-1]
    return any(p.startswith(".") or p in SKIPPED_DIRS for p in parts)


def offenders(root: Path) -> list[str]:
    """One "path:line: .modifier on a body Section" message per site, sorted by path."""
    found = []
    base = root / "ios"
    if not base.is_dir():
        return found
    for path in sorted(base.rglob("*.swift")):
        rel = path.relative_to(root).as_posix()
        if skipped(rel) or not path.is_file():
            continue
        for line, name in file_offenders(path.read_text(encoding="utf-8")):
            found.append(f"{rel}:{line}: .{name} on a body Section")
    return found


def main() -> int:
    problems = offenders(ROOT)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(
            "a List gives a Section's modifiers to each row: move it onto a row that is always shown",
            file=sys.stderr,
        )
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
