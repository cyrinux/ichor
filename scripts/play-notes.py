#!/usr/bin/env python3
"""Writes Google Play's release notes ("What's new") for the newest release.

`fastlane supply` uploads fastlane/metadata/android/<locale>/changelogs/<versionCode>.txt
with the bundle. This renders the newest entry of scripts/changelog.py (whose
build_number is the versionCode scripts/version.sh stamps) as one bullet per user-facing
commit into en-US/changelogs/<build_number>.txt, capped at Play's 500 characters.

A file that already exists is kept: notes written by hand (see `just release-tag`, which
drafts them for you to edit) win over generated ones. Other locales are translated by
hand; Play shows the default language where a locale has none.

Usage:
  scripts/play-notes.py              # notes for the newest release (CI, on a tag)
  scripts/play-notes.py --build 120  # file them under another versionCode
  scripts/play-notes.py --force      # overwrite an existing file
"""

import argparse
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHANGELOGS = os.path.join(ROOT, "fastlane", "metadata", "android", "en-US", "changelogs")
LIMIT = 500  # Play's cap per language
FALLBACK = "Bug fixes and improvements."


def newest_release():
    out = subprocess.run(
        [sys.executable, os.path.join(ROOT, "scripts", "changelog.py"), "--limit", "1"],
        capture_output=True, text=True, check=True,
    ).stdout
    releases = json.loads(out)["releases"]
    if not releases:
        raise SystemExit("No release in the git history (are the tags fetched?).")
    return releases[0]


def render(release):
    """One bullet per item, whole lines only, within LIMIT characters."""
    lines = [f"• {item}" for section in release["sections"] for item in section["items"]]
    if not lines:
        return FALLBACK + "\n"
    kept = []
    for line in lines:
        if len("\n".join([*kept, line])) + 1 > LIMIT:
            break
        kept.append(line)
    if not kept:  # a single line longer than the cap
        kept = [lines[0][: LIMIT - 2] + "…"]
    return "\n".join(kept) + "\n"


def write(path, text, force):
    """Writes text to path unless it exists (and not force); returns whether it wrote."""
    if os.path.exists(path) and not force:
        return False
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as out:
        out.write(text)
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--build", type=int, help="versionCode to file the notes under")
    parser.add_argument("--force", action="store_true", help="overwrite an existing file")
    args = parser.parse_args()

    release = newest_release()
    build = args.build or release["build_number"]
    path = os.path.join(CHANGELOGS, f"{build}.txt")
    wrote = write(path, render(release), args.force)
    print(f"{'Wrote' if wrote else 'Kept'} {os.path.relpath(path, ROOT)}", file=sys.stderr)
    print(path)


if __name__ == "__main__":
    main()
