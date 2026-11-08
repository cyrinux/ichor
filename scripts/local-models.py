#!/usr/bin/env python3
"""Pins the models the app offers to download (go/ichorgo/local_models.json).

Each entry names a Hugging Face repository and a GGUF file. This fills in, from the Hugging
Face API, the repository's current commit (so the download URL never serves other bytes), the
file's size and its SHA-256 (the LFS object id), which both apps check after downloading. An
entry the API does not know fails the run: nothing is pinned to a file that does not exist.

  scripts/local-models.py            # pin the entries that are not pinned yet
  scripts/local-models.py --refresh  # move every entry to its repository's current commit
  scripts/local-models.py --check    # fail when an entry is not pinned (CI before a release)
"""
import argparse
import json
import sys
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CATALOG = ROOT / "go/ichorgo/local_models.json"
API = "https://huggingface.co/api/models/"


def fetch_json(url: str):
    with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "ichor-local-models"}), timeout=60) as r:
        return json.load(r)


def pinned(entry: dict) -> bool:
    return len(entry.get("revision", "")) == 40 and len(entry.get("sha256", "")) == 64 and entry.get("bytes", 0) > 0


def pin(entry: dict, fetch=fetch_json) -> dict:
    """The entry with revision, bytes and sha256 from the repository's current commit."""
    repo = urllib.parse.quote(entry["repo"], safe="/")
    revision = fetch(API + repo + "/revision/main")["sha"]
    directory = entry["file"].rsplit("/", 1)[0] if "/" in entry["file"] else ""
    tree = fetch(API + repo + "/tree/" + revision + ("/" + urllib.parse.quote(directory) if directory else ""))
    for item in tree:
        if item.get("path") == entry["file"]:
            lfs = item.get("lfs") or {}
            if not lfs.get("oid") or not lfs.get("size"):
                raise SystemExit(f"{entry['id']}: {entry['file']} is not an LFS file in {entry['repo']}")
            return {**entry, "revision": revision, "bytes": lfs["size"], "sha256": lfs["oid"]}
    raise SystemExit(f"{entry['id']}: no {entry['file']} in {entry['repo']} at {revision}")


def main(argv=None, fetch=fetch_json, catalog: Path = CATALOG) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--refresh", action="store_true", help="re-pin every entry to its repository's current commit")
    parser.add_argument("--check", action="store_true", help="only check that every entry is pinned")
    args = parser.parse_args(argv)
    entries = json.loads(catalog.read_text())
    if args.check:
        missing = [e["id"] for e in entries if not pinned(e)]
        if missing:
            print("not pinned (run scripts/local-models.py):", *missing, sep="\n  ", file=sys.stderr)
            return 1
        print(f"local models: {len(entries)} pinned")
        return 0
    updated = [pin(e, fetch) if args.refresh or not pinned(e) else e for e in entries]
    catalog.write_text(json.dumps(updated, indent=2, ensure_ascii=False) + "\n")
    for e in updated:
        print(f"{e['id']}: {e['repo']}@{e['revision'][:12]} {e['file']} {e['bytes']} bytes sha256 {e['sha256'][:12]}…")
    return 0


if __name__ == "__main__":
    sys.exit(main())
