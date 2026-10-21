#!/usr/bin/env python3
"""Fails on an Android string key that nothing reads.

Every <string>, <plurals> and <string-array> of app/src/main/res/values/strings.xml must be
referenced somewhere: R.string.<name>, R.plurals.<name> or R.array.<name> in Kotlin or Java,
or @string/<name> (@plurals/, @array/) in a resource or manifest XML, in the app, core-widget
or wear module, tests and previews included. An unread key costs a translation in five
languages at every pass. Keys read by name at runtime (getIdentifier) go in ALLOW.

Run: python3 scripts/check-unused-strings.py
"""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
STRINGS = ROOT / "app/src/main/res/values/strings.xml"
MODULES = [ROOT / "app", ROOT / "core-widget", ROOT / "wear"]
SOURCE_SUFFIXES = {".kt", ".java", ".xml"}

# Keys read by name at runtime (Resources.getIdentifier): none so far.
ALLOW: set[str] = set()

DEFINITION = re.compile(r'<(?:string|plurals|string-array)\s[^>]*\bname="([^"]+)"')
REFERENCE = re.compile(r"\bR\.(?:string|plurals|array)\.(\w+)|@(?:string|plurals|array)/(\w+)")


def defined_keys(xml: str) -> list[str]:
    """The names of the strings, plurals and string arrays defined in a strings.xml."""
    return DEFINITION.findall(xml)


def referenced_names(text: str) -> set[str]:
    """The string resource names a source or resource file refers to."""
    return {code or res for code, res in REFERENCE.findall(text)}


def source_files(modules: list[Path]):
    for module in modules:
        for path in module.rglob("*"):
            if path.suffix in SOURCE_SUFFIXES and "build" not in path.relative_to(module).parts and path.is_file():
                yield path


def unused_keys(defined: list[str], referenced: set[str], allow: set[str]) -> list[str]:
    return [key for key in defined if key not in referenced and key not in allow]


def main() -> int:
    referenced: set[str] = set()
    for path in source_files(MODULES):
        referenced |= referenced_names(path.read_text(encoding="utf-8", errors="replace"))

    unused = unused_keys(defined_keys(STRINGS.read_text(encoding="utf-8")), referenced, ALLOW)
    if not unused:
        return 0

    print(f"{len(unused)} Android string key(s) in {STRINGS.relative_to(ROOT)} are never read:", file=sys.stderr)
    for key in unused:
        print(f"  {key}", file=sys.stderr)
    print("Delete them from values/strings.xml and the five translations, or add them to ALLOW.", file=sys.stderr)

    return 1


if __name__ == "__main__":
    sys.exit(main())
