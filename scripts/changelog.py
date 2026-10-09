#!/usr/bin/env python3
"""Builds the release history as one JSON document, newest first (same shape as lectarr's):

  {
    "generated_at": "2026-10-01T20:00:00+00:00",
    "releases": [
      {
        "version": "0.4.1",
        "build_number": 61,
        "published_at": "2026-10-01T18:25:00+00:00",
        "sections": [{"kind": "new", "title": "New", "items": ["..."]}]
      }
    ]
  }

Each release is the conventional commits between one vX.Y.Z tag and the one before it:
feat -> new, fix -> fixed, perf -> faster, a "!" or BREAKING CHANGE -> breaking. Other
types (chore, ci, docs, test, refactor) are not user-facing and are left out, so a release
may have no sections. `build_number` is the commit count at the tag, the number
scripts/version.sh stamps as Android's versionCode, so the apps can order entries.

When HEAD is past the newest tag, the commits since that tag become a leading entry with
HEAD's version, so a development build also has notes.

The apps bundle this file (what changed since the build you had) and the website
publishes it (what a newer release brings). Prints an empty history, never fails, without
git or tags.

Usage:
  scripts/changelog.py              # the whole history, to stdout
  scripts/changelog.py --limit 20   # only the newest 20 releases
  scripts/changelog.py -o FILE      # write to FILE
  scripts/changelog.py --scrub      # copy stdin to stdout without tracker references

Issue tracker references (CYR-123) are private: they are dropped from every item, and
`just release-tag` runs the tag's commit subjects through --scrub.
"""

import argparse
import datetime
import json
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
SUBJECT = re.compile(r"^(?P<type>[a-z]+)(\([^)]*\))?(?P<breaking>!)?:\s*(?P<text>.+)$")
KINDS = {"feat": "new", "fix": "fixed", "perf": "faster"}
TITLES = {"breaking": "Breaking", "new": "New", "fixed": "Fixed", "faster": "Faster"}
ORDER = ["breaking", "new", "fixed", "faster"]
TRACKER_IDS = r"\bCYR-\d+\b(?:[\s,]+CYR-\d+\b)*"
TRACKER_SCOPE = re.compile(rf"\(\s*{TRACKER_IDS}\s*\)(?=!?:)", re.IGNORECASE)
TRACKER_REF = re.compile(
    rf"\s*(?:\b(?:refs?|closes|fixes|resolves|see)\b\s*)?[\[(]?{TRACKER_IDS}[\])]?(?:\s*:)?",
    re.IGNORECASE,
)


def git(*args):
    try:
        out = subprocess.run(["git", "-C", ROOT, *args], capture_output=True, text=True, check=True)
    except (OSError, subprocess.CalledProcessError):
        return ""
    return out.stdout.strip()


def release_tags():
    tags = [t for t in git("tag", "--merged", "HEAD").splitlines() if TAG.match(t)]
    return sorted(tags, key=lambda t: tuple(int(n) for n in TAG.match(t).groups()), reverse=True)


def scrub(text):
    """Drops issue tracker references (CYR-123), which point to a private tracker."""
    cleaned = TRACKER_REF.sub("", TRACKER_SCOPE.sub("", text))
    if cleaned == text:
        return text
    return re.sub(r"\s{2,}", " ", cleaned).strip(" ,;:-")


def classify(subject, body):
    """Returns (kind, text) for a user-facing commit, else None."""
    match = SUBJECT.match(scrub(subject))
    if not match:
        return None
    kind = KINDS.get(match["type"])
    if match["breaking"] or "BREAKING CHANGE" in body:
        kind = "breaking"
    if kind is None:
        return None
    text = match["text"].strip()
    return kind, text[:1].upper() + text[1:]


def sections(range_spec):
    log = git("log", "--no-merges", "--format=%s%x1f%b%x1e", range_spec)
    items = {}
    for entry in log.split("\x1e"):
        subject, _, body = entry.strip().partition("\x1f")
        found = classify(subject, body)
        if found and found[1] not in items.setdefault(found[0], []):
            items[found[0]].append(found[1])
    return [{"kind": k, "title": TITLES[k], "items": items[k]} for k in ORDER if items.get(k)]


def release(version, ref, range_spec):
    count = git("rev-list", "--count", ref)
    return {
        "version": version,
        "build_number": int(count) if count.isdigit() else 0,
        "published_at": git("log", "-1", "--format=%cI", ref),
        "sections": sections(range_spec),
    }


def history(limit):
    tags = release_tags()
    releases = []
    if tags and git("rev-list", "--count", f"{tags[0]}..HEAD") not in ("", "0"):
        version = git("describe", "--tags", "--match", "v[0-9]*", "HEAD").lstrip("v")
        match = re.match(r"^(\d+\.\d+\.\d+)-(\d+)-g([0-9a-f]+)$", version)
        if match:  # the version scripts/version.sh gives this build
            version = f"{match[1]}+{match[2]}.g{match[3]}"
        releases.append(release(version, "HEAD", f"{tags[0]}..HEAD"))
    for i, tag in enumerate(tags):
        older = tags[i + 1] if i + 1 < len(tags) else None
        releases.append(release(tag.lstrip("v"), tag, f"{older}..{tag}" if older else tag))
    return releases[:limit] if limit else releases


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("-o", "--output")
    parser.add_argument("--scrub", action="store_true", help="filter stdin, see scrub()")
    args = parser.parse_args()

    if args.scrub:
        for line in sys.stdin:
            print(scrub(line.rstrip("\n")))
        return

    document = {
        "generated_at": datetime.datetime.now(datetime.timezone.utc).isoformat(timespec="seconds"),
        "releases": history(args.limit),
    }
    text = json.dumps(document, indent=2, ensure_ascii=False) + "\n"
    if args.output:
        os.makedirs(os.path.dirname(os.path.abspath(args.output)), exist_ok=True)
        with open(args.output, "w", encoding="utf-8") as out:
            out.write(text)
    else:
        sys.stdout.write(text)


if __name__ == "__main__":
    main()
