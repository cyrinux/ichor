#!/usr/bin/env python3
"""Bundles the app-inventory icons from homarr-labs/dashboard-icons (Apache-2.0), plus a
few from selfh.st/icons (CC-BY-4.0) that dashboard-icons lacks.

Reads the curated catalog (go/ichorgo/appcatalog.json), checks it, then:

  app/src/main/assets/appicons/<id>.webp        icon for a light background
  app/src/main/assets/appicons/<id>-night.webp  icon for a dark background, when upstream has one
  go/ichorgo/appicons.txt                   every upstream slug, so the Go core can name an
                                                unknown image's icon (fetched only when opted in)

iOS bundles the same folder (ios/project.yml). Icons are resized to 96 px lossy WebP with
ImageMagick (`magick`); upstream ships 512 px lossless files.

Usage: scripts/sync-app-icons.py [--check]
  --check  validate the catalog and the bundled files without the network
"""
import argparse
import json
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CATALOG = ROOT / "go/ichorgo/appcatalog.json"
SLUGS = ROOT / "go/ichorgo/appicons.txt"
ASSETS = ROOT / "app/src/main/assets/appicons"
CDN = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
# A catalog icon "sh:<ref>" comes from selfh.st/icons (CC-BY-4.0, credited in NOTICE) instead.
SELFHST = "sh:"
SELFHST_CDN = "https://cdn.jsdelivr.net/gh/selfhst/icons"
SIZE = 96
# Logos the cluster selector shows for a managed cluster (model/ClusterLogo.kt, IchorCore
# ClusterLogo.swift), bundled with the app icons: file name -> Dashboard Icons slug. Talos,
# Kubernetes and Rancher come from the catalog.
CLUSTER_ICONS = {
    "aws": "aws",
    "google-cloud": "google-cloud-platform",
    "azure": "azure",
    "digital-ocean": "digital-ocean",
}
CATEGORIES = {
    "system", "networking", "storage", "observability", "security", "database",
    "messaging", "devops", "media", "home", "productivity", "ai", "web",
}


def icon_of(app: dict) -> str:
    """The upstream slug of an app's icon; "" means it has none (a monogram is drawn)."""
    return app.get("icon", app["id"])


def validate(apps: list[dict]) -> list[str]:
    errors, ids, names = [], set(), {}
    for app in apps:
        app_id = app["id"]
        if app_id in ids:
            errors.append(f"duplicate id {app_id}")
        ids.add(app_id)
        if app_id in CLUSTER_ICONS:
            errors.append(f"{app_id}: file name taken by a cluster logo (CLUSTER_ICONS)")
        if app.get("cat") not in CATEGORIES:
            errors.append(f"{app_id}: unknown category {app.get('cat')!r}")
        for name in app.get("match", [app_id]):
            if name in names and names[name] != app_id:
                errors.append(f"match name {name!r} used by {names[name]} and {app_id}")
            names[name] = app_id
    return errors


def fetch(url: str) -> bytes:
    with urllib.request.urlopen(url, timeout=30) as response:
        return response.read()


def resize(data: bytes, out: Path) -> None:
    with tempfile.NamedTemporaryFile(suffix=".webp") as src:
        src.write(data)
        src.flush()
        subprocess.run(
            ["magick", src.name, "-strip", "-resize", f"{SIZE}x{SIZE}", "-quality", "85", str(out)],
            check=True,
        )


def homarr_urls(slug: str, meta: dict) -> tuple[str, str | None] | None:
    """(icon for light backgrounds, icon for dark backgrounds or None). Upstream names a
    dark-coloured variant `colors.dark` and a light-coloured one `colors.light`."""
    if slug not in meta:
        return None
    colors = meta[slug].get("colors", {})
    day = colors.get("dark", slug)
    night = colors.get("light")
    return f"{CDN}/webp/{day}.webp", f"{CDN}/webp/{night}.webp" if night and night != day else None


def selfhst_urls(ref: str, index: dict) -> tuple[str, str | None] | None:
    """selfh.st ships `<ref>-light` as the light-coloured variant (for dark backgrounds)."""
    entry = index.get(ref)
    if entry is None or entry.get("WebP") != "Yes":
        return None
    night = f"{SELFHST_CDN}/webp/{ref}-light.webp" if entry.get("Light") == "Yes" else None
    return f"{SELFHST_CDN}/webp/{ref}.webp", night


def sync(apps: list[dict]) -> list[str]:
    if not shutil.which("magick"):
        return ["ImageMagick (magick) is required"]
    meta = json.loads(fetch(f"{CDN}/metadata.json"))
    selfhst = {e["Reference"]: e for e in json.loads(fetch(f"{SELFHST_CDN}/index.json"))}
    sources = {}
    for app in apps:
        icon = icon_of(app)
        if icon:
            ref = icon.removeprefix(SELFHST)
            sources[app["id"]] = selfhst_urls(ref, selfhst) if icon.startswith(SELFHST) else homarr_urls(icon, meta)
    for name, slug in CLUSTER_ICONS.items():
        sources[name] = homarr_urls(slug, meta)
    errors = [f"{app_id}: no upstream icon" for app_id, urls in sources.items() if urls is None]
    if errors:
        return errors
    shutil.rmtree(ASSETS, ignore_errors=True)
    ASSETS.mkdir(parents=True)
    for app_id, (day, night) in sources.items():
        resize(fetch(day), ASSETS / f"{app_id}.webp")
        if night:
            resize(fetch(night), ASSETS / f"{app_id}-night.webp")
        print(f"  {app_id}", file=sys.stderr)
    SLUGS.write_text("".join(f"{s}\n" for s in sorted(meta)))
    return []


def check_bundle(apps: list[dict]) -> list[str]:
    names = [app["id"] for app in apps if icon_of(app)] + list(CLUSTER_ICONS)
    return [
        f"{name}: missing {ASSETS.relative_to(ROOT)}/{name}.webp"
        for name in names if not (ASSETS / f"{name}.webp").exists()
    ]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--check", action="store_true", help="validate without the network")
    args = parser.parse_args()
    apps = json.loads(CATALOG.read_text())
    errors = validate(apps)
    if not errors:
        errors = check_bundle(apps) if args.check else sync(apps)
    for error in errors:
        print(error, file=sys.stderr)
    if not errors:
        bundled = len(list(ASSETS.glob("*.webp")))
        size = sum(f.stat().st_size for f in ASSETS.glob("*.webp"))
        print(f"{len(apps)} apps, {bundled} icon files, {size // 1024} KiB")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
