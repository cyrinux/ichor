#!/usr/bin/env python3
"""Fails when a dev shell of flake.nix would not build on one of its Linux hosts.

For each host in HOSTS and each shell in SHELLS it asks Nix what it would have to build
(`nix build --dry-run`: what the binary cache and the local store lack) and lists the
derivations among them whose system that host cannot build. An aarch64 host builds aarch64
and, through binfmt emulation, x86_64 (the Android SDK and NDK only exist for x86_64), never
i686: a nixpkgs bump that pulls an uncached i686 library into the SDK breaks `./build.sh check`
there (2026-10-27, ncurses5). Evaluation itself fails on a package marked broken or unsupported
for the host (swiftc 6.3.3 on aarch64), and that fails the check too. Nothing is built.

Run from anywhere (needs nix with flakes): python3 scripts/check-dev-shells.py [flake-dir]
"""
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SHELLS = ("default", "swift")
# Host → the derivation systems it builds ("builtin": fetchurl and friends, run by Nix itself).
HOSTS = {
    "x86_64-linux": {"x86_64-linux", "i686-linux", "builtin"},
    "aarch64-linux": {"aarch64-linux", "x86_64-linux", "builtin"},
}
DRV = re.compile(r"^\s+(/nix/store/\S+\.drv)$")


def derivations(doc: dict) -> dict:
    """The name → derivation map of `nix derivation show` output: Nix 2.33+ wraps it in
    {"derivations": …, "version": …}, older versions key the derivations by store path."""
    return doc["derivations"] if "derivations" in doc else doc


def foreign(doc: dict, host: str) -> list:
    """(system, derivation) pairs that `host` cannot build, sorted by name."""
    if host not in HOSTS:
        raise ValueError(f"unknown host {host}")
    allowed = HOSTS[host]
    return sorted(((d["system"], name) for name, d in derivations(doc).items() if d["system"] not in allowed), key=lambda p: p[1])


def to_build(dry_run: str) -> list:
    """The derivations a `nix build --dry-run` report says it would build (not those it fetches)."""
    out, listing = [], False
    for line in dry_run.splitlines():
        if not line.startswith(" "):
            listing = line.startswith("this derivation will be built") or (line.startswith("these ") and "will be built" in line)
            continue
        match = DRV.match(line)
        if listing and match:
            out.append(match.group(1))
    return out


def nix(*args: str) -> str:
    out = subprocess.run(["nix", *args], capture_output=True, text=True)
    if out.returncode != 0:
        raise RuntimeError(out.stderr.strip())
    return out.stdout + out.stderr


def check(flake: Path, host: str, shell: str) -> list:
    report = nix("build", "--dry-run", "--no-link", f"{flake}#devShells.{host}.{shell}")
    drvs = to_build(report)
    return foreign(json.loads(nix("derivation", "show", *drvs)), host) if drvs else []


def main(argv: list) -> int:
    flake = Path(argv[1]).resolve() if len(argv) > 1 else ROOT
    failed = False
    for host in HOSTS:
        for shell in SHELLS:
            try:
                bad = check(flake, host, shell)
            except RuntimeError as err:
                print(f"{host} {shell}: evaluation failed\n{err}", file=sys.stderr)
                failed = True
                continue
            for system, name in bad:
                print(f"{host} {shell}: would build {name}, a {system} derivation", file=sys.stderr)
            if not bad:
                print(f"{host} {shell}: OK")
            failed = failed or bool(bad)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
