#!/usr/bin/env python3
"""Fails when a translation is missing or its placeholders differ from English.

Covers the Android string resources (app/src/main/res/values*/strings.xml) and the iOS
String Catalogs (ios/**/Localizable.xcstrings). Languages: en (source), fr, es, uk, de, it.
"""
import json
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
LANGUAGES = ["fr", "es", "uk", "de", "it"]
ANDROID_RES = ROOT / "app/src/main/res"
ANDROID_PLACEHOLDER = re.compile(r"%(\d+\$)?[sdf]")
IOS_PLACEHOLDER = re.compile(r"%(\d+\$)?(@|lld|ld|d|lf|f)")
# What a merge leaves when a conflict was resolved by hand but not cleaned (diff3 base included).
CONFLICT_MARKER = re.compile(r"^(<{7}|\|{7}|={7}|>{7})(\s|$)")

errors: list[str] = []


def conflict_markers(text: str) -> list[int]:
    """Line numbers (from 1) of the merge conflict markers left in text."""
    return [n for n, line in enumerate(text.splitlines(), 1) if CONFLICT_MARKER.match(line)]


def check_markers() -> None:
    files = sorted(ANDROID_RES.glob("values*/strings.xml")) + sorted((ROOT / "ios").rglob("Localizable.xcstrings"))
    for path in files:
        for n in conflict_markers(path.read_text()):
            errors.append(f"{path.relative_to(ROOT)}:{n}: merge conflict marker")


def android_entries(path: Path) -> dict[str, list[str]]:
    """name -> list of texts (one for <string>, one per quantity for <plurals>)."""
    entries: dict[str, list[str]] = {}
    for node in ET.parse(path).getroot():
        if node.get("translatable") == "false":
            continue
        if node.tag == "string":
            entries[node.get("name")] = ["".join(node.itertext())]
        elif node.tag == "plurals":
            items = {item.get("quantity"): "".join(item.itertext()) for item in node.findall("item")}
            if "other" not in items:
                errors.append(f"{path.relative_to(ROOT)}: plurals {node.get('name')} has no 'other'")
            entries[node.get("name")] = list(items.values())
    return entries


def placeholders(texts: list[str], pattern: re.Pattern) -> set[str]:
    return {m.group(0) for text in texts for m in pattern.finditer(text)}


def check_android() -> int:
    base_path = ANDROID_RES / "values/strings.xml"
    if not base_path.exists():
        return 0
    base = android_entries(base_path)
    for lang in LANGUAGES:
        path = ANDROID_RES / f"values-{lang}/strings.xml"
        if not path.exists():
            errors.append(f"android: missing {path.relative_to(ROOT)}")
            continue
        translated = android_entries(path)
        for name in sorted(base.keys() - translated.keys()):
            errors.append(f"android {lang}: missing '{name}'")
        for name in sorted(translated.keys() - base.keys()):
            errors.append(f"android {lang}: '{name}' is not in values/strings.xml")
        for name in base.keys() & translated.keys():
            if placeholders(base[name], ANDROID_PLACEHOLDER) != placeholders(translated[name], ANDROID_PLACEHOLDER):
                errors.append(f"android {lang}: placeholders differ for '{name}'")
    return len(base)


def ios_texts(localization: dict) -> list[str]:
    texts = []
    if "stringUnit" in localization:
        texts.append(localization["stringUnit"].get("value", ""))
    for variation in localization.get("variations", {}).values():
        for case in variation.values():
            texts.extend(ios_texts(case))
    return texts


def check_ios() -> int:
    total = 0
    for catalog in sorted((ROOT / "ios").rglob("Localizable.xcstrings")):
        data = json.loads(catalog.read_text())
        rel = catalog.relative_to(ROOT)
        for key, entry in data.get("strings", {}).items():
            if entry.get("shouldTranslate") is False:
                continue
            total += 1
            locs = entry.get("localizations", {})
            english = ios_texts(locs["en"]) if "en" in locs else [key]
            for lang in LANGUAGES:
                if lang not in locs:
                    errors.append(f"{rel} {lang}: missing '{key}'")
                    continue
                if placeholders(english, IOS_PLACEHOLDER) != placeholders(ios_texts(locs[lang]), IOS_PLACEHOLDER):
                    errors.append(f"{rel} {lang}: placeholders differ for '{key}'")
    return total


def main() -> None:
    check_markers()
    android = check_android()
    ios = check_ios()
    if errors:
        print("\n".join(errors), file=sys.stderr)
        print(f"{len(errors)} translation problem(s)", file=sys.stderr)
        sys.exit(1)
    print(f"translations OK: {android} Android keys, {ios} iOS keys, languages en + {', '.join(LANGUAGES)}")


if __name__ == "__main__":
    main()
