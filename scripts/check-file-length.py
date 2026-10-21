#!/usr/bin/env python3
"""Fails on a Go, Kotlin or Swift source file over the project's length limit.

- source files: 800 lines (CONTRIBUTING.md, "Before you open a pull request");
- tests (*_test.go, ios/**/Tests/**): 1,200 lines.

KNOWN is the only allowlist: a file listed there may not grow past its recorded length,
whatever its limit. Shrink the number when the file shrinks; split the file to drop it.

Run: python3 scripts/check-file-length.py
"""
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOURCE_LIMIT = 800
TEST_LIMIT = 1200

# (directory, glob) pairs walked under ROOT.
SOURCES = (
    ("go", "*.go"),
    ("app/src/main", "*.kt"),
    ("core-widget/src", "*.kt"),
    ("wear/src", "*.kt"),
    ("ios", "*.swift"),
)
# Build output and tool caches present in a local checkout.
SKIPPED_DIRS = {"build", "DerivedData"}

# Path (relative to ROOT) → the length it may not exceed.
KNOWN = {
    "go/ichorgo/talosfake_test.go": 1055,
    "go/ichorgo/imagescan_test.go": 989,
    "go/plugin/validate.go": 821,
}


def is_test(rel: str) -> bool:
    return rel.endswith("_test.go") or (rel.startswith("ios/") and "/Tests/" in rel)


def limit_for(rel: str) -> int:
    if rel in KNOWN:
        return KNOWN[rel]
    return TEST_LIMIT if is_test(rel) else SOURCE_LIMIT


def skipped(rel: str) -> bool:
    parts = rel.split("/")[:-1]
    return rel.endswith(".pb.go") or any(p.startswith(".") or p in SKIPPED_DIRS for p in parts)


def line_count(path: Path) -> int:
    with path.open("rb") as f:
        return sum(1 for _ in f)


def offenders(root: Path) -> list[str]:
    """One "path: N lines (limit L)" message per file over its limit, sorted by path."""
    found = []
    for directory, pattern in SOURCES:
        base = root / directory
        if not base.is_dir():
            continue
        for path in sorted(base.rglob(pattern)):
            rel = path.relative_to(root).as_posix()
            if skipped(rel) or not path.is_file():
                continue
            n, limit = line_count(path), limit_for(rel)
            if n > limit:
                found.append(f"{rel}: {n} lines (limit {limit})")
    return sorted(found)


def main() -> int:
    problems = offenders(ROOT)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print(f"split these files (source limit {SOURCE_LIMIT}, tests {TEST_LIMIT})", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
