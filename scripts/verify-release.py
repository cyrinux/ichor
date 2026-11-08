#!/usr/bin/env python3
"""Rebuilds a tagged release APK and compares it with the published one.

Anyone can check that a release APK on GitHub was built from its tag: this downloads the
published ichor-<TAG>-<abi>.apk, rebuilds the tag from a clean checkout with `./build.sh
release` in the pinned Nix shell (unsigned: no keystore), removes what signing adds from
both (the v1 META-INF signature files and the APK Signing Block of v2/v3) and compares
them entry by entry: name, order, size, CRC-32, compression and timestamp.

Exit 0 and "identical: N entries" when they match; exit 1 with the differing entries (also
written to --report) otherwise; exit 2 when it cannot run (no nix, no release, no build).
--keep keeps both stripped APKs (for diffoscope) and the build checkout. --published uses
an APK already on disk instead of downloading it: CI downloads the draft in a step of its
own, since only a token that can write releases sees a draft, and keeps that token out of
the build.

Usage:
  scripts/verify-release.py verify v1.40.1
  scripts/verify-release.py verify v1.40.1 --abi x86_64 --keep --report diff.txt
  scripts/verify-release.py verify v1.40.1 --published ichor-v1.40.1-arm64-v8a.apk
  scripts/verify-release.py compare PUBLISHED.apk REBUILT.apk   # no download, no build
"""

import argparse
import io
import os
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ABIS = ("arm64-v8a", "armeabi-v7a", "x86_64")
# The repository the release builds take their update source from (BuildConfig.UPDATE_REPO).
DEFAULT_REPO = "cyrinux/ichor"

# What a v1 (JAR) signature adds to an APK.
SIGNATURE_FILE = re.compile(r"^META-INF/([^/]+\.(RSA|DSA|EC|SF)|MANIFEST\.MF)$", re.IGNORECASE)

APK_SIG_MAGIC = b"APK Sig Block 42"
EOCD_MAGIC = b"PK\x05\x06"
EOCD_SIZE = 22


class VerifyError(Exception):
    """The verification could not run (exit 2)."""


def strip_signing_block(data: bytes) -> bytes:
    """data without its APK Signing Block, the central directory offset fixed; as is when it has none."""
    eocd = data.rfind(EOCD_MAGIC, max(0, len(data) - EOCD_SIZE - 0xFFFF))
    if eocd < 0:
        raise VerifyError("not a zip file: no end of central directory")
    cd_offset = struct.unpack_from("<I", data, eocd + 16)[0]
    # The block ends right before the central directory: [size][pairs][size][magic].
    if cd_offset < 24 or data[cd_offset - 16:cd_offset] != APK_SIG_MAGIC:
        return data
    size = struct.unpack_from("<Q", data, cd_offset - 24)[0]
    start = cd_offset - size - 8
    if start < 0 or struct.unpack_from("<Q", data, start)[0] != size:
        raise VerifyError("malformed APK Signing Block")
    eocd_fixed = data[eocd:eocd + 16] + struct.pack("<I", start) + data[eocd + 20:]
    return data[:start] + data[cd_offset:eocd] + eocd_fixed


def entries(path: str) -> list:
    """The APK's entries but the signature files, in order: (name, size, crc, compression, date)."""
    try:
        with open(path, "rb") as f:
            data = strip_signing_block(f.read())
        with zipfile.ZipFile(io.BytesIO(data)) as z:
            return [
                (i.filename, i.file_size, i.CRC, i.compress_type, i.date_time)
                for i in z.infolist()
                if not SIGNATURE_FILE.match(i.filename)
            ]
    except (OSError, zipfile.BadZipFile) as e:
        raise VerifyError(f"cannot read {path}: {e}") from e


def differences(published: list, rebuilt: list) -> list:
    """Lines naming each entry that differs, is missing on one side or moved."""
    out = []
    pub = {e[0]: e for e in published}
    reb = {e[0]: e for e in rebuilt}
    for name in sorted(set(pub) | set(reb)):
        a, b = pub.get(name), reb.get(name)
        if a is None:
            out.append(f"only rebuilt: {name} size={b[1]} crc={b[2]:08x}")
        elif b is None:
            out.append(f"only published: {name} size={a[1]} crc={a[2]:08x}")
        elif a != b:
            out.append(
                f"differs: {name} published size={a[1]} crc={a[2]:08x} method={a[3]} date={a[4]}"
                f" / rebuilt size={b[1]} crc={b[2]:08x} method={b[3]} date={b[4]}"
            )
    if not out and [e[0] for e in published] != [e[0] for e in rebuilt]:
        out.append("same entries in another order")
    return out


def write_stripped(src: str, dest: str) -> None:
    """src without its signatures, re-zipped in the original order and compression (for diffoscope)."""
    with zipfile.ZipFile(src) as z, zipfile.ZipFile(dest, "w") as out:
        for info in z.infolist():
            if not SIGNATURE_FILE.match(info.filename):
                out.writestr(info, z.read(info), compress_type=info.compress_type)


def compare(published: str, rebuilt: str, report: str | None) -> int:
    pub = entries(published)
    diff = differences(pub, entries(rebuilt))
    if not diff:
        print(f"identical: {len(pub)} entries ({os.path.basename(published)})")
        return 0
    text = "\n".join(diff) + "\n"
    sys.stdout.write(text)
    if report:
        with open(report, "w") as f:
            f.write(text)
    print(f"not reproducible: {len(diff)} differing entries", file=sys.stderr)
    return 1


def run(cmd: list, **kw) -> None:
    print("+ " + " ".join(cmd), file=sys.stderr)
    if subprocess.run(cmd, **kw).returncode != 0:
        raise VerifyError(f"failed: {' '.join(cmd)}")


def download(tag: str, abi: str, repo: str, dest: str) -> str:
    name = f"ichor-{tag}-{abi}.apk"
    run(["gh", "release", "download", tag, "--repo", repo, "--pattern", name, "--dir", dest])
    path = os.path.join(dest, name)
    if not os.path.exists(path):
        raise VerifyError(f"{name} is not on release {tag}")
    return path


def rebuild(tag: str, abi: str, repo: str, work: str) -> str:
    """The unsigned release APK of abi, built from a clean checkout of tag."""
    if shutil.which("nix") is None:
        raise VerifyError("nix is not installed: only the pinned Nix shell builds comparable APKs")
    src = os.path.join(work, "src")
    run(["git", "-C", ROOT, "worktree", "add", "--detach", src, tag])
    env = {k: v for k, v in os.environ.items() if not k.startswith("ICHOR_KEY")}
    # The repository the release was built in: BuildConfig.UPDATE_REPO comes from it.
    env.update(BUILDER="nix", GITHUB_REPOSITORY=repo, ICHOR_SKIP_TESTS="1")
    run(["./build.sh", "release"], cwd=src, env=env)
    out = os.path.join(src, "app", "build", "outputs", "apk", "release")
    found = sorted(f for f in os.listdir(out) if f.startswith(f"app-{abi}-release") and f.endswith(".apk")) if os.path.isdir(out) else []
    if not found:
        raise VerifyError(f"the build made no {abi} release APK in {out}")
    rebuilt = os.path.join(work, f"rebuilt-{tag}-{abi}.apk")
    shutil.copy(os.path.join(out, found[0]), rebuilt)
    return rebuilt


def verify(args) -> int:
    if args.published and not os.path.isfile(args.published):
        raise VerifyError(f"{args.published} does not exist")
    work = tempfile.mkdtemp(prefix="ichor-verify-")
    try:
        published = args.published or download(args.tag, args.abi, args.repo, work)
        rebuilt = rebuild(args.tag, args.abi, args.repo, work)
        code = compare(published, rebuilt, args.report)
        if args.keep:
            for path in (published, rebuilt):
                write_stripped(path, os.path.join(work, os.path.basename(path).replace(".apk", "-stripped.apk")))
            print(f"kept in {work} (diffoscope {work}/*-stripped.apk)", file=sys.stderr)
        return code
    finally:
        if not args.keep:
            subprocess.run(["git", "-C", ROOT, "worktree", "remove", "--force", os.path.join(work, "src")], capture_output=True)
            shutil.rmtree(work, ignore_errors=True)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    sub = parser.add_subparsers(dest="cmd", required=True)
    v = sub.add_parser("verify", help="download, rebuild and compare a release")
    v.add_argument("tag")
    v.add_argument("--abi", default="arm64-v8a", choices=ABIS)
    v.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY") or DEFAULT_REPO)
    v.add_argument("--keep", action="store_true")
    v.add_argument("--report", help="also write the differing entries here")
    v.add_argument("--published", help="the published APK, already downloaded (no gh release download)")
    c = sub.add_parser("compare", help="compare two APKs already on disk")
    c.add_argument("published")
    c.add_argument("rebuilt")
    c.add_argument("--report")
    args = parser.parse_args(argv)
    try:
        if args.cmd == "compare":
            return compare(args.published, args.rebuilt, args.report)
        return verify(args)
    except VerifyError as e:
        print(f"verify-release: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
