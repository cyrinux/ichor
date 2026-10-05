#!/usr/bin/env python3
"""Prints the next release version, bumped from the conventional commits since the newest tag.

Same reading of the commits as scripts/changelog.py, which writes the release notes:
a "!" or BREAKING CHANGE -> major, feat -> minor, fix or perf -> patch. Other types
(chore, ci, docs, test, refactor) are not user-facing and release nothing, so when the
commits since the tag hold none of the above this prints nothing and exits 1.

--bump forces the bump (a release by hand, from the phone): any commit since the tag is
then enough, but a tagged HEAD still releases nothing.

Usage:
  scripts/next-version.py           # e.g. 1.9.0, or nothing (exit 1) when there is no release
  scripts/next-version.py --bump patch
  scripts/next-version.py --json    # {"current": "1.8.4", "next": "1.9.0", "bump": "minor"}
"""

import argparse
import importlib.util
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("changelog", os.path.join(HERE, "changelog.py"))
changelog = importlib.util.module_from_spec(spec)
spec.loader.exec_module(changelog)

# The changelog section a commit lands in decides how much it bumps.
BUMPS = {"breaking": "major", "new": "minor", "fixed": "patch", "faster": "patch"}
RANK = ["patch", "minor", "major"]


def bump_for(kinds):
    """The largest bump the given changelog kinds call for, or None."""
    bumps = [BUMPS[k] for k in kinds if k in BUMPS]
    return max(bumps, key=RANK.index) if bumps else None


def bumped(version, bump):
    major, minor, patch = (int(n) for n in version.split("."))
    if bump == "major":
        return f"{major + 1}.0.0"
    if bump == "minor":
        return f"{major}.{minor + 1}.0"
    return f"{major}.{minor}.{patch + 1}"


def next_version(forced=None):
    tags = changelog.release_tags()
    current = tags[0].lstrip("v") if tags else "0.0.0"
    since = f"{tags[0]}..HEAD" if tags else "HEAD"
    if forced:
        bump = forced if changelog.git("rev-list", "--count", since) not in ("", "0") else None
    else:
        bump = bump_for(s["kind"] for s in changelog.sections(since))
    return {"current": current, "next": bumped(current, bump) if bump else None, "bump": bump}


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--bump", choices=RANK, help="force this bump instead of the commits'")
    args = parser.parse_args()

    result = next_version(args.bump)
    if args.json:
        print(json.dumps(result))
    elif result["next"]:
        print(result["next"])
    return 0 if result["next"] else 1


if __name__ == "__main__":
    sys.exit(main())
