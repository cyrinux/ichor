#!/usr/bin/env python3
"""Writes the Discord webhook message announcing the newest release.

Renders the newest entry of scripts/changelog.py as one embed: the version as its title
(linked to the GitHub release when --url is given) and one "**Section**" heading per
section with a bullet per user-facing commit, within Discord's 4096 characters per embed
description. release.yml posts it to the #changelog channel's webhook once the release is
published. Mentions are disabled, so a commit subject can never ping anyone.

Usage:
  scripts/discord-notes.py                      # JSON payload for the newest release, to stdout
  scripts/discord-notes.py --url URL            # link the title to the release page
"""

import argparse
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LIMIT = 4096  # Discord's cap on an embed description
COLOR = 0xFF7A45  # the website's accent
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


def describe(release):
    """Sections as bold headings and bullets, whole lines only, within LIMIT characters."""
    lines = []
    for section in (s for s in release["sections"] if s["items"]):
        heading = [f"**{section['title']}**"] if not lines else ["", f"**{section['title']}**"]
        lines += heading + [f"• {item}" for item in section["items"]]
    if not lines:
        return FALLBACK
    kept = []
    for line in lines:
        if len("\n".join([*kept, line, "…"])) > LIMIT:
            return "\n".join([*kept, "…"])
        kept.append(line)
    return "\n".join(kept)


def payload(release, url=None):
    embed = {
        "title": f"Ichor {release['version']}",
        "description": describe(release),
        "color": COLOR,
    }
    if url:
        embed["url"] = url
    if release.get("published_at"):
        embed["timestamp"] = release["published_at"]
    return {"embeds": [embed], "allowed_mentions": {"parse": []}}


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--url", help="release page the title links to")
    args = parser.parse_args()
    print(json.dumps(payload(newest_release(), args.url), ensure_ascii=False))


if __name__ == "__main__":
    main()
