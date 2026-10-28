#!/usr/bin/env python3
"""Fails on a Play Store listing text over Google Play's length limit, in any language.

Play rejects the whole release upload ("The full description for the app is too long for
language ru-RU") only at the end of the release workflow, so this catches it in CI instead.

Run: python3 scripts/check-play-listing.py
"""
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
METADATA = Path("fastlane/metadata/android")

# File name under <locale>/ → Play's limit in characters.
LIMITS = {
    "title.txt": 30,
    "short_description.txt": 80,
    "full_description.txt": 4000,
}
CHANGELOG_LIMIT = 500


def length(path: Path) -> int:
    # supply strips surrounding whitespace before upload (an 80-character short description
    # plus its newline is accepted).
    return len(path.read_text(encoding="utf-8").strip())


def latest_changelog(locale: Path) -> list[Path]:
    """The newest build's notes: supply uploads only the current build's, older ones are history."""
    builds = [p for p in (locale / "changelogs").glob("*.txt") if p.stem.isdigit()]
    return [max(builds, key=lambda p: int(p.stem))] if builds else []


def offenders(root: Path) -> list[str]:
    """One "path: N characters (limit L)" message per text over its limit, sorted by path."""
    base = root / METADATA
    if not base.is_dir():
        return []
    locales = [locale for locale in base.iterdir() if locale.is_dir()]
    checked = [(locale / name, limit) for locale in locales for name, limit in LIMITS.items()]
    checked += [(path, CHANGELOG_LIMIT) for locale in locales for path in latest_changelog(locale)]
    found = []
    for path, limit in checked:
        if path.is_file() and (n := length(path)) > limit:
            found.append(f"{path.relative_to(root).as_posix()}: {n} characters (limit {limit})")
    return sorted(found)


def main() -> int:
    problems = offenders(ROOT)
    for p in problems:
        print(p, file=sys.stderr)
    if problems:
        print("shorten these Play listing texts: Play refuses the release upload", file=sys.stderr)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
