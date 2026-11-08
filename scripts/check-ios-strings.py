#!/usr/bin/env python3
"""Fails on an iOS key with a placeholder that never receives its arguments.

String(localized: "wants %@ replicas") returns the translated text with the %@ still in it:
the screen shows "wants %@ replicas". A key that carries a printf specifier (%@, %1$@, %lld …)
must be the format of String(format:) or String.localizedStringWithFormat(), on the same
statement:

    String(format: String(localized: "wants %@ replicas"), count)

Not flagged: a literal percent sign (%%, or "\\(n)% of"), a key built by interpolation
(String(localized: "\\(n) nodes") is a LocalizationValue that formats itself), Text("…"),
comments and #Preview blocks. Raw strings (#"…"#) and nested /* */ comments are lexed
as Swift does, so they do not hide the lines after them.

Run: python3 scripts/check-ios-strings.py [path …]   (default: ios/)
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
IOS_SOURCES = ROOT / "ios"

LOCALIZED = re.compile(r"\bString\(\s*localized:\s*(?=#*\")")
SPECIFIER = re.compile(r"%(?:\d+\$)?[-+#0]*\d*(?:\.\d+)?(?:hh|h|ll|l|q|z|t|j)?[@dDiuUxXoOfFeEgGcCsSp]")
PREVIEW = re.compile(r"#Preview\b")
# "…", """…""", #"…"# (raw: \#( interpolates, \( is text).
STRING_OPEN = re.compile(r'(#*)("""|")')
# Calls whose first argument is a format: String(format: …), NSString(format: …),
# String.localizedStringWithFormat(…).
FORMAT_CALL = re.compile(r"\b(?:NS)?String\s*$")
FORMAT_LABEL = re.compile(r"^\s*format:")
FORMAT_FUNCTION = re.compile(r"\blocalizedStringWithFormat\s*$")


def _blank(chars: list[str], start: int, end: int) -> None:
    for k in range(start, end):
        if chars[k] != "\n":
            chars[k] = " "


def _read_string(src: str, i: int) -> tuple[int, str, list[tuple[int, int]]]:
    """Reads the literal opening at i; returns its end offset, its text without
    interpolations, and the (start, end) offsets of each interpolation's code."""
    m = STRING_OPEN.match(src, i)
    close, escape = m.group(2) + m.group(1), "\\" + m.group(1)
    k, parts, bodies = m.end(), [], []
    while k < len(src):
        if src.startswith(escape + "(", k):
            start = k + len(escape) + 1
            k = _skip_interpolation(src, start)
            bodies.append((start, k - 1))
        elif src.startswith(escape, k):
            parts.append(src[k : k + len(escape) + 1])
            k += len(escape) + 1
        elif src.startswith(close, k):
            return k + len(close), "".join(parts), bodies
        else:
            parts.append(src[k])
            k += 1
    return len(src), "".join(parts), bodies


def _skip_interpolation(src: str, k: int) -> int:
    """Offset just past the ) that closes an interpolation whose body starts at k."""
    depth = 1
    while k < len(src) and depth:
        if STRING_OPEN.match(src, k):
            k, _, _ = _read_string(src, k)
            continue
        depth += {"(": 1, ")": -1}.get(src[k], 0)
        k += 1
    return k


def _block_comment_end(src: str, i: int) -> int:
    """Offset just past the */ closing the comment at i (Swift block comments nest)."""
    depth, k = 0, i
    while k < len(src):
        if src.startswith("/*", k):
            depth, k = depth + 1, k + 2
        elif src.startswith("*/", k):
            depth, k = depth - 1, k + 2
            if not depth:
                return k
        else:
            k += 1
    return len(src)


def _mask_range(src: str, chars: list[str], literals: dict[int, str], i: int, hi: int) -> None:
    while i < hi:
        string = STRING_OPEN.match(src, i)
        if src.startswith("//", i):
            end = src.find("\n", i)
            end = hi if end < 0 else min(end, hi)
            _blank(chars, i, end)
        elif src.startswith("/*", i):
            end = _block_comment_end(src, i)
            _blank(chars, i, end)
        elif string:
            end, literals[i], bodies = _read_string(src, i)
            _blank(chars, string.end(), end)
            for start, stop in bodies:  # interpolations are code
                chars[start:stop] = src[start:stop]
                _mask_range(src, chars, literals, start, stop)
        else:
            end = i + 1
        i = end


def mask(src: str) -> tuple[str, dict[int, str]]:
    """The source with comments and string contents blanked (offsets and newlines kept, the
    code inside interpolations too), and each literal's text by the offset where it opens."""
    chars, literals = list(src), {}
    _mask_range(src, chars, literals, 0, len(src))
    return "".join(chars), literals


def _preview_spans(code: str) -> list[tuple[int, int]]:
    spans = []
    for m in PREVIEW.finditer(code):
        start = code.find("{", m.end())
        if start < 0:
            continue
        depth, k = 0, start
        while k < len(code):
            depth += {"{": 1, "}": -1}.get(code[k], 0)
            k += 1
            if depth == 0:
                break
        spans.append((m.start(), k))
    return spans


def _formatted(code: str, pos: int) -> bool:
    """Whether a call enclosing pos (within its statement) takes it as its format, the first
    argument."""
    depth, later_argument = 0, False
    for k in range(pos - 1, -1, -1):
        c = code[k]
        if c in ")]":
            depth += 1
        elif c in "([" and depth:
            depth -= 1
        elif c == "(":
            before, after = code[:k], code[k + 1 :]
            format_call = FORMAT_CALL.search(before) and FORMAT_LABEL.match(after) or FORMAT_FUNCTION.search(before)
            if format_call and not later_argument:
                return True
            later_argument = False
        elif c == "[":
            later_argument = False
        elif c == "," and not depth:
            later_argument = True
        elif c in "{};" and not depth:
            return False
    return False


def unformatted_keys(src: str) -> list[tuple[int, str]]:
    """(1-based line, key) of each String(localized:) whose key needs arguments it never gets."""
    code, literals = mask(src)
    previews = _preview_spans(code)
    found = []
    for m in LOCALIZED.finditer(code):
        if any(start <= m.start() < end for start, end in previews):
            continue
        key = literals[m.end()]
        if SPECIFIER.search(key.replace("%%", "")) and not _formatted(code, m.start()):
            found.append((code.count("\n", 0, m.start()) + 1, key))
    return found


def findings(paths: list[Path]) -> list[tuple[Path, int, str]]:
    files = sorted(f for p in paths for f in ([p] if p.is_file() else p.rglob("*.swift")))
    return [(f, line, key) for f in files for line, key in unformatted_keys(f.read_text(encoding="utf-8"))]


def main(argv: list[str]) -> int:
    paths = [Path(a) for a in argv] or [IOS_SOURCES]
    found = findings(paths)
    for path, line, key in found:
        shown = path.relative_to(ROOT) if path.is_relative_to(ROOT) else path
        print(f"{shown}:{line}: {key}")
    if found:
        print(
            f"{len(found)} String(localized:) with a placeholder and no arguments: "
            "wrap it in String(format: …, args)",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
